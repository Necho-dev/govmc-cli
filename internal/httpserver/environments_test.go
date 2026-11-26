package httpserver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/target"
)

func TestEnvironmentRoutingPinsRequestsAndDoesNotChangeDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := target.Edit(context.Background(), path, func(c *target.Config) error {
		c.Active = "lab"
		for _, name := range []string{"lab", "prod"} {
			p := target.Profile{URL: "https://" + name + ".example", Username: "root"}
			if err := target.SetPassword(path, name, &p, "fixture-secret"); err != nil {
				return err
			}
			c.Profiles[name] = p
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lab, _ := target.Named(path, "lab")
	prod, _ := target.Named(path, "prod")
	base := &Server{Token: "fixture-token"}
	manager := NewEnvironmentManager(path, lab, base)
	defer manager.Close()
	var mu sync.Mutex
	seen := map[string]int{}
	backend := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen[name]++
			mu.Unlock()
			fmt.Fprintf(w, `{"environment":%q,"vm":"same-id"}`, name)
		})
	}
	manager.backends[prod] = &environmentBackend{server: &Server{}, handler: backend("prod")}
	handler := requestsForListen("", base.auth(manager.wrap(backend("lab"))))
	request := func(method, path, profile, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		r.Header.Set("X-Govmc-Profile", profile)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if got := request("GET", "/v1/profiles", "", "", ""); got.Code != 401 {
		t.Fatal("environment list bypassed API authentication")
	}
	listed := request("GET", "/v1/profiles", "", "", "fixture-token")
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), `"current":"lab"`) || strings.Contains(listed.Body.String(), "fixture-secret") || strings.Contains(listed.Body.String(), "password") {
		t.Fatalf("unsafe profile list: %s", listed.Body.String())
	}
	if got := request("POST", "/v1/profiles/connect", "", `{"name":"prod"}`, "fixture-token"); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	// The same VM ID must be resolved on the selected environment, including writes.
	for _, name := range []string{"lab", "prod", "lab", "prod"} {
		got := request("POST", "/v1/vms/same-id/power", name, `{"confirm":true}`, "fixture-token")
		if got.Code != 200 || !strings.Contains(got.Body.String(), `"environment":"`+name+`"`) {
			t.Fatalf("cross-environment routing: %s", got.Body.String())
		}
	}
	for _, body := range []string{`{"name":"missing"}`, `{"name":"prod","mode":"readwrite"}`, `{"name":"prod"} {}`} {
		if got := request("POST", "/v1/profiles/connect", "", body, "fixture-token"); got.Code == 200 {
			t.Fatalf("invalid selection accepted: %s", body)
		}
	}
	if got := request("GET", "/v1/vms", "missing", "", "fixture-token"); got.Code == 200 {
		t.Fatal("unknown profile silently fell back")
	}
	if got := request("GET", "/v1/vms", "", "", "fixture-token"); !strings.Contains(got.Body.String(), `"lab"`) {
		t.Fatal("tab selection changed server default")
	}
	config, _ := target.Load(path)
	if config.Active != "lab" || config.MCP.Mode != target.MCPReadonly {
		t.Fatal("Web selection changed saved defaults or permissions")
	}
	if base.ProfileName != "lab" || !strings.Contains(base.consolePath("ticket"), "profile=lab") {
		t.Fatal("console ticket omitted its originating environment")
	}
	// Concurrent tabs cannot swap a shared client out from under one another.
	var workers sync.WaitGroup
	for _, name := range []string{"lab", "prod"} {
		workers.Add(1)
		go func(name string) {
			defer workers.Done()
			for i := 0; i < 15; i++ {
				got := request("GET", "/v1/vms/same-id", name, "", "fixture-token")
				if !strings.Contains(got.Body.String(), `"environment":"`+name+`"`) {
					t.Errorf("concurrent profile crossed: %s", got.Body.String())
				}
			}
		}(name)
	}
	workers.Wait()
}

func TestWebCannotCreateEditOrDeleteProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := target.Edit(context.Background(), path, func(c *target.Config) error {
		c.Active = "lab"
		c.Profiles["lab"] = target.Profile{URL: "https://lab.example", Username: "root"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initial, err := target.Named(path, "lab")
	if err != nil {
		t.Fatal(err)
	}
	base := &Server{Listen: "0.0.0.0:8765"}
	manager := NewEnvironmentManager(path, initial, base)
	base.Profiles = manager
	defer manager.Close()
	handler := base.Handler()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct{ method, path string }{{"POST", "/v1/profiles"}, {"PATCH", "/v1/profiles/lab"}, {"PUT", "/v1/profiles/lab"}, {"DELETE", "/v1/profiles/lab"}} {
		req := httptest.NewRequest(action.method, "http://192.0.2.10:8765"+action.path, strings.NewReader(`{"name":"changed","url":"https://changed.example","username":"root","password":"must-not-be-stored"}`))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("profile management route exposed: %s %s returned %d", action.method, action.path, response.Code)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Web management requests modified saved profiles")
	}
}
