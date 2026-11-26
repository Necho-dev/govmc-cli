package esxi

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vmware/govmomi/vim25/types"
)

func TestConsoleWireHandshakeAndActualInput(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "rfb38"}[negotiated], func(t *testing.T) {
			packets := make(chan [][]byte, 1)
			upgrader := websocket.Upgrader{Subprotocols: []string{"binary"}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				if negotiated {
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte("RFB 003.008\n"))
					_, version, err := conn.ReadMessage()
					if err != nil || string(version) != "RFB 003.008\n" {
						t.Errorf("client version: %q %v", version, err)
						return
					}
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1, 1})
					_, auth, _ := conn.ReadMessage()
					if !bytes.Equal(auth, []byte{1}) {
						t.Error("client did not select ticket-authenticated None security")
						return
					}
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0, 0, 0, 0})
					_, shared, _ := conn.ReadMessage()
					if !bytes.Equal(shared, []byte{1}) {
						t.Error("missing shared desktop request")
						return
					}
				}
				header := make([]byte, 24)
				binary.BigEndian.PutUint16(header, 1920)
				binary.BigEndian.PutUint16(header[2:], 1080)
				binary.BigEndian.PutUint32(header[20:], 4)
				// Split ServerInit across messages: WebSocket frames are not RFB messages.
				_ = conn.WriteMessage(websocket.BinaryMessage, header[:7])
				_ = conn.WriteMessage(websocket.BinaryMessage, append(header[7:], []byte("test")...))
				var received [][]byte
				for i := 0; i < 8; i++ {
					kind, packet, err := conn.ReadMessage()
					if err != nil || kind != websocket.BinaryMessage {
						t.Errorf("input packet %d: %v", i, err)
						return
					}
					received = append(received, packet)
				}
				packets <- received
				// Keep the server alive through the input consumption grace period.
				_, _, _ = conn.ReadMessage()
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			wire, err := dialConsole(ctx, websocket.Dialer{Subprotocols: []string{"binary"}}, "ws"+strings.TrimPrefix(server.URL, "http"))
			if err != nil {
				t.Fatal(err)
			}
			defer wire.conn.Close()
			actions, err := compileActions([]ConsoleAction{{Type: "key", Key: "r", Modifiers: []string{"meta"}}, {Type: "text", Text: "a"}, {Type: "key", Key: "enter"}}, wire.width, wire.height)
			if err != nil {
				t.Fatal(err)
			}
			result := ConsoleResult{}
			if err := wire.runActions(ctx, actions, &result); err != nil || result.Completed != 3 {
				t.Fatalf("input: %+v %v", result, err)
			}
			got := <-packets
			want := [][]byte{keyPacket(0x15b, true), keyPacket(19, true), keyPacket(19, false), keyPacket(0x15b, false), keyPacket(30, true), keyPacket(30, false), keyPacket(28, true), keyPacket(28, false)}
			for i := range want {
				if !bytes.Equal(got[i], want[i]) {
					t.Errorf("packet %d: %v, want %v", i, got[i], want[i])
				}
			}
		})
	}
}

func TestConsoleTicketRoutingUsesReachableEndpoint(t *testing.T) {
	reach, _ := url.Parse("https://root:secret@public.example:8443/sdk")
	ticket := &types.VirtualMachineTicket{Ticket: "secret-ticket", Host: "internal.invalid", Port: 443}
	if got := ConsoleURL(ticket, reach); got != "wss://public.example:8443/ticket/secret-ticket" {
		t.Fatal("console did not retain the configured NAT endpoint")
	}
	ticket.Url = "wss://internal.invalid/ticket/other?x=1"
	if got := ConsoleURL(ticket, reach); got != "wss://public.example:8443/ticket/other?x=1" {
		t.Fatal("server ticket path was lost or internal host leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := dialConsole(ctx, websocket.Dialer{}, "ws://127.0.0.1:1/ticket/secret-ticket")
	if err == nil || strings.Contains(err.Error(), "secret-ticket") {
		t.Fatal("dial error exposed the bearer ticket")
	}
}
