package mcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/gorilla/websocket"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

type consoleTicketRoundTripper struct {
	soap.RoundTripper
	issued atomic.Int32
}

func (r *consoleTicketRoundTripper) RoundTrip(ctx context.Context, req, res soap.HasFault) error {
	if b, ok := req.(*methods.AcquireTicketBody); ok {
		if b.Req.TicketType != "webmks" {
			return context.Canceled
		}
		r.issued.Add(1)
		res.(*methods.AcquireTicketBody).Res = &types.AcquireTicketResponse{Returnval: types.VirtualMachineTicket{Ticket: "fixture-secret"}}
		return nil
	}
	return r.RoundTripper.RoundTrip(ctx, req, res)
}

func TestStdioReadwriteConsoleObtainsTicketAndSendsInput(t *testing.T) {
	if os.Getenv("GOVMC_WEB_SIM_TEST") != "1" {
		t.Skip("local simulator opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	model := simulator.ESX()
	model.Machine = 1
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	defer model.Remove()
	model.Service.TLS = &tls.Config{}
	sdk := model.Service.NewServer()
	defer sdk.Close()
	username := sdk.URL.User.Username()
	password, _ := sdk.URL.User.Password()
	c, err := esxi.Dial(ctx, sdk.URL.String(), username, password, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Gov.Logout(context.Background())
	vms, err := c.ListVMs(ctx, false)
	if err != nil || len(vms) != 1 {
		t.Fatalf("inventory: %v %v", vms, err)
	}
	var received atomic.Int32
	upgrader := websocket.Upgrader{Subprotocols: []string{"binary"}}
	console := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ticket/fixture-secret" {
			t.Error("ticket was not used for the WebSocket")
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		header := make([]byte, 24)
		binary.BigEndian.PutUint16(header, 800)
		binary.BigEndian.PutUint16(header[2:], 600)
		_ = conn.WriteMessage(websocket.BinaryMessage, header)
		for _, down := range []byte{1, 0} {
			_, packet, err := conn.ReadMessage()
			if err != nil || !bytes.Equal(packet, []byte{127, 0, 0, 8, 0, 28, down, 0}) {
				t.Errorf("expected Enter press/release, got %v %v", packet, err)
				return
			}
			received.Add(1)
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer console.Close()
	c.Reach, _ = url.Parse(console.URL)
	rt := &consoleTicketRoundTripper{RoundTripper: c.Gov.Client.RoundTripper}
	c.Gov.Client.RoundTripper = rt
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_vm_console_press_key","arguments":{"id":"` + vms[0].ID + `","key":"enter","profile":"fixture"}}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"esxi_vm_console_press_key","arguments":{"id":"` + vms[0].ID + `","key":"enter","profile":"fixture","confirm":true}}}` + "\n")
	connects := 0
	var output bytes.Buffer
	err = ServeStream(ctx, input, &output, func(ctx context.Context, raw []byte) ([]byte, error) {
		return HandleConfigured(ctx, raw, func(_ context.Context, profile string) (*esxi.Client, string, func(), error) {
			connects++
			if profile != "fixture" {
				t.Error("explicit profile lost")
			}
			return c, profile, func() {}, nil
		}, Readwrite)
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "confirm=true") {
		t.Fatalf("confirmation framing: %s", &output)
	}
	var response struct {
		Result struct {
			IsError bool                    `json:"isError"`
			Content []struct{ Text string } `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &response); err != nil || response.Result.IsError || len(response.Result.Content) < 1 {
		t.Fatalf("stdio response: %s %v", lines[1], err)
	}
	var result esxi.ConsoleResult
	if err := json.Unmarshal([]byte(response.Result.Content[0].Text), &result); err != nil || result.Completed != 1 || result.Width != 800 || result.VMID != vms[0].ID {
		t.Fatalf("console result: %+v %v", result, err)
	}
	if connects != 1 || rt.issued.Load() != 1 || received.Load() != 2 || strings.Contains(output.String(), "fixture-secret") {
		t.Fatal("confirmation, ticket consumption, actual input or credential redaction failed")
	}
}
