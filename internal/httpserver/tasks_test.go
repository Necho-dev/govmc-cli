package httpserver

import (
	"context"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskQueriesDoNotDialSelectedProfileAndHTTPMCPIsRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	m, e := tasks.Open(path, "serve")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	job, e := m.Submit(context.Background(), "offline", "https://old-endpoint", "vm.clone", "clone-1", nil, "clone-1", "", func(context.Context, *tasks.Reporter) (any, error) { return nil, nil })
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{Tasks: m, Token: "token", Listen: "127.0.0.1:8765"}
	s.Profiles = NewEnvironmentManager(path, target.Target{Name: "offline"}, s)
	handler := s.Handler()
	for _, url := range []string{"/v1/tasks", "/v1/tasks/" + job.ID} {
		r := httptest.NewRequest("GET", "http://127.0.0.1"+url, nil)
		r.Header.Set("X-API-Token", "token")
		r.Header.Set("X-Govmc-Profile", "offline")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), job.ID) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1/mcp", strings.NewReader(`{"method":"tools/list"}`))
	r.Header.Set("X-API-Token", "token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 410 {
		t.Fatal(w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1/v1/tasks", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated task query", w.Code)
	}
}
