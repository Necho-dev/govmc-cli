package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/target"
)

func TestSettingsQueriesAreLocalAuthenticatedAndReadOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	store, err := journal.Open(p, "serve")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.Log(journal.Event{Event: "fixture", Profile: "offline"})
	_ = journal.Run(journal.With(context.Background(), store), journal.Event{Operation: "vm.power.off", Profile: "offline"}, func() error { return errors.New("secret-request-body") })
	s := &Server{Journal: store, Token: "test-token", Listen: "127.0.0.1:8765"}
	// No ESXi client: querying must never try to resolve or dial a profile.
	s.Profiles = NewEnvironmentManager(p, target.Target{Name: "missing"}, s)
	handler := s.Handler()
	for _, path := range []string{"/v1/settings/logs?profile=offline", "/v1/settings/audits?outcome=failed", "/v1/settings/status"} {
		req := httptest.NewRequest("GET", "http://localhost"+path, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("X-Govmc-Profile", "nonexistent")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) || strings.Contains(w.Body.String(), "secret-request-body") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	req := httptest.NewRequest("GET", "http://localhost/v1/settings/audits", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("query bypassed token")
	}
	for _, method := range []string{"POST", "DELETE", "PATCH"} {
		req := httptest.NewRequest(method, "http://localhost/v1/settings/audits", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 405 && w.Code != 404 {
			t.Fatalf("mutation %s: %d", method, w.Code)
		}
	}
	for _, query := range []string{"limit=9999", "offset=-1", "since=invalid", "since=2026-11-25T12:00:00Z&until=2026-11-25T11:00:00Z"} {
		req := httptest.NewRequest("GET", "http://localhost/v1/settings/logs?"+query, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("invalid query accepted: %s", query)
		}
	}
	req = httptest.NewRequest("GET", "http://localhost/v1/settings/logs", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Origin", "https://evil.invalid")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("settings query bypassed origin guard")
	}
}
func TestHTTPAuditPreservesOutcomeAndProtectsDispatch(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	store, err := journal.Open(p, "serve")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{Journal: store}
	called := 0
	handler := s.instrument(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(403) }))
	req := httptest.NewRequest("POST", "http://localhost/v1/vms/vm-1/power?token=secret-query", strings.NewReader("secret-body"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	rows, err := store.Audits(context.Background(), journal.Query{})
	if err != nil || len(rows.Items) != 1 || rows.Items[0].Outcome != "denied" {
		t.Fatal(rows, err)
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "secret-") {
		t.Fatal("raw URL or body was recorded")
	}
	_ = store.Close()
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 503 || called != 1 {
		t.Fatal("request dispatched when audit database unavailable")
	}
}
