package httpserver

import (
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/wmks"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWMKSCapabilitiesAreLocalAndAuthenticated(t *testing.T) {
	s := &Server{Token: "fixture", WMKS: wmks.Missing()}
	s.Profiles = NewEnvironmentManager("unused", target.Target{Name: "offline"}, s)
	h := s.Handler()
	for _, token := range []string{"", "fixture"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8765/v1/console/capabilities", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Govmc-Profile", "not-a-configured-profile")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if token == "" {
			if w.Code != 401 {
				t.Fatal(w.Code)
			}
		} else if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"missing"`) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, p := range []string{"/wmks/receipt.json", "/wmks/config.key", "/wmks/wmks.min.js"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8765"+p, nil))
		if w.Code != 404 {
			t.Fatal(p, w.Code)
		}
	}
}
