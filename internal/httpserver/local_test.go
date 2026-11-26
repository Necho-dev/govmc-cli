package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListenAddressAndRequestScope(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8765", "192.168.1.2:8765", "127.0.0.1:9000", "[::]:8765", "127.0.0.1:0"} {
		if _, err := ListenAddress(address); err != nil {
			t.Fatalf("rejected %s: %v", address, err)
		}
	}
	for _, address := range []string{":8765", "evil.test:8765", "127.0.0.1:not-a-port", "0.0.0.0:65536", "127.0.0.1:-1"} {
		if _, err := ListenAddress(address); err == nil {
			t.Fatalf("accepted invalid address %s", address)
		}
	}
	if got, err := ListenAddress("localhost:8765"); err != nil || got != "127.0.0.1:8765" {
		t.Fatalf("localhost: %s %v", got, err)
	}
	for _, tc := range []struct {
		listen, host, origin, site string
		code                       int
	}{
		{"", "localhost:8765", "http://localhost:8765", "", 204},
		{"127.0.0.1:8765", "127.0.0.1:8765", "", "", 204},
		{"127.0.0.1:8765", "192.168.1.2:8765", "", "", 403},
		{"0.0.0.0:8765", "192.168.1.2:8765", "http://192.168.1.2:8765", "", 204},
		{"0.0.0.0:8765", "127.0.0.1:8765", "", "", 204},
		{"0.0.0.0:8765", "192.168.1.2:8765", "http://evil.test", "", 403},
		{"0.0.0.0:8765", "192.168.1.2:8765", "http://192.168.1.2:9000", "", 403},
		{"0.0.0.0:8765", "192.168.1.2:8765", "", "cross-site", 403},
		{"0.0.0.0:8765", "evil.test:8765", "", "", 403},
		{"0.0.0.0:8765", "0.0.0.0:8765", "", "", 403},
		{"192.168.1.2:8765", "192.168.1.2:8765", "", "", 204},
		{"192.168.1.2:8765", "192.168.1.3:8765", "", "", 403},
	} {
		handler := requestsForListen(tc.listen, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		r := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/v1/vms/1/power", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%+v got %d", tc, w.Code)
		}
	}
}

func TestConsoleTicketCannotBeConsumedCrossOrigin(t *testing.T) {
	s := &Server{}
	s.tickets.Store("ticket", consoleTicket{ExpiresAt: time.Now().Add(-time.Minute)})
	request := func(origin string) int {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/ws/console?id=ticket", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Connection", "Upgrade")
		w := httptest.NewRecorder()
		s.handleWS(w, r)
		return w.Code
	}
	if got := request("http://evil.test"); got != 403 {
		t.Fatalf("cross-origin status: %d", got)
	}
	if _, ok := s.tickets.Load("ticket"); !ok {
		t.Fatal("cross-origin request consumed the ticket")
	}
	if got := request("http://127.0.0.1:8765"); got != 410 {
		t.Fatalf("expired ticket status: %d", got)
	}
	if _, ok := s.tickets.Load("ticket"); ok {
		t.Fatal("expired ticket retained")
	}
}
