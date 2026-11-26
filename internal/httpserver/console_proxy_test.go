package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConsoleProxyForwardsInputAndTicketIsSingleUse(t *testing.T) {
	upgrader := websocket.Upgrader{Subprotocols: []string{"binary"}}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		kind, packet, err := c.ReadMessage()
		if err != nil || kind != websocket.BinaryMessage {
			t.Errorf("upstream input: %v", err)
			return
		}
		_ = c.WriteMessage(kind, packet)
		_, _, _ = c.ReadMessage()
	}))
	defer upstream.Close()
	s := &Server{Insecure: true}
	s.storeTicket("fixture", "wss"+strings.TrimPrefix(upstream.URL, "https"))
	proxy := httptest.NewServer(http.HandlerFunc(s.handleWS))
	defer proxy.Close()
	dialer := websocket.Dialer{Subprotocols: []string{"binary"}, HandshakeTimeout: 5 * time.Second}
	address := "ws" + strings.TrimPrefix(proxy.URL, "http") + "/ws/console?id=fixture"
	connection, _, err := dialer.Dial(address, http.Header{"Origin": []string{proxy.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	packet := []byte{127, 0, 0, 8, 0, 28, 1, 0}
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := connection.WriteMessage(websocket.BinaryMessage, packet); err != nil {
		t.Fatal(err)
	}
	_, returned, err := connection.ReadMessage()
	if err != nil || !bytes.Equal(packet, returned) {
		t.Fatalf("WebSocket proxy altered input: %v %v", returned, err)
	}
	reused, response, err := dialer.Dial(address, http.Header{"Origin": []string{proxy.URL}})
	if reused != nil {
		_ = reused.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusGone {
		t.Fatal("consumed ticket was reusable")
	}
	_ = response.Body.Close()
}
