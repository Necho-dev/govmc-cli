package httpserver

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// This listener serves fixtures only; no ESXi credentials or clients are used.
func TestWildcardListenHTTPAndConsoleOrigin(t *testing.T) {
	if os.Getenv("GOVMC_WEB_SIM_TEST") != "1" {
		t.Skip("set GOVMC_WEB_SIM_TEST=1 to allow local fixture listeners")
	}
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Listen: "0.0.0.0:0", WebFS: fstest.MapFS{"index.html": {Data: []byte("LAN fixture")}}}
	server := httptest.NewUnstartedServer(s.Handler())
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	origin := "http://192.0.2.10:" + port
	request := func(path, host, requestOrigin string, upgrade bool) (int, string) {
		req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		req.Header.Set("Origin", requestOrigin)
		if upgrade {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(body)
	}
	host := "192.0.2.10:" + port
	if code, body := request("/", host, origin, false); code != 200 || !strings.Contains(body, "LAN fixture") {
		t.Fatalf("LAN UI: %d %s", code, body)
	}
	s.tickets.Store("ticket", consoleTicket{ExpiresAt: time.Now().Add(-time.Second)})
	if code, _ := request("/ws/console?id=ticket", host, "http://evil.test", true); code != 403 {
		t.Fatal("cross-origin console request accepted")
	}
	if _, exists := s.tickets.Load("ticket"); !exists {
		t.Fatal("rejected request consumed console ticket")
	}
	if code, _ := request("/ws/console?id=ticket", host, origin, true); code != 410 {
		t.Fatalf("same-origin LAN console rejected before ticket check: %d", code)
	}
	if code, _ := request("/", "evil.test:"+port, "", false); code != 403 {
		t.Fatal("arbitrary DNS Host accepted")
	}
}
