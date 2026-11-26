package httpserver

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/vmware/govmomi/simulator"
)

// Opt in: this test opens only a local simulator socket, never a real ESXi.
func TestEnvironmentSimulatorConnections(t *testing.T) {
	if os.Getenv("GOVMC_WEB_SIM_TEST") != "1" {
		t.Skip("set GOVMC_WEB_SIM_TEST=1 to run the local simulator")
	}
	model := simulator.ESX()
	model.Machine = 1
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	defer model.Remove()
	model.Service.TLS = &tls.Config{}
	service := model.Service.NewServer()
	defer service.Close()
	endpoint := *service.URL
	user := endpoint.User.Username()
	password, _ := endpoint.User.Password()
	endpoint.User = nil
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := target.Edit(context.Background(), path, func(c *target.Config) error {
		c.Active = "prod"
		c.MCP.Mode = target.MCPReadwrite
		for _, name := range []string{"lab", "prod", "broken"} {
			p := target.Profile{URL: endpoint.String(), Username: user, Insecure: true}
			if name == "prod" {
				p.MCP.Mode = target.MCPReadonly
			}
			if name == "broken" {
				p.URL = "https://127.0.0.1:1/sdk"
			}
			if err := target.SetPassword(path, name, &p, password); err != nil {
				return err
			}
			c.Profiles[name] = p
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initial, err := target.Named(path, "lab")
	if err != nil {
		t.Fatal(err)
	}
	client, err := initial.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	base := &Server{ESXi: client, Insecure: true}
	manager := NewEnvironmentManager(path, initial, base)
	base.Profiles = manager
	defer manager.Close()
	handler := base.Handler()
	request := func(method, path, profile, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		req.Header.Set("X-Govmc-Profile", profile)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	listing := request("GET", "/v1/profiles", "", "")
	if listing.Code != 200 || !strings.Contains(listing.Body.String(), `"current":"lab"`) {
		t.Fatal("explicit startup profile did not override active")
	}
	connected := request("POST", "/v1/profiles/connect", "", `{"name":"prod"}`)
	if connected.Code != 200 {
		t.Fatal(connected.Body.String())
	}
	inventory := request("GET", "/v1/vms?view=full", "prod", "")
	var vms []esxi.VMInfo
	if inventory.Code != 200 || inventory.Header().Get("X-Govmc-Profile") != "prod" || json.Unmarshal(inventory.Body.Bytes(), &vms) != nil || len(vms) == 0 {
		t.Fatalf("profile inventory failed: %s", inventory.Body.String())
	}
	before := vms[0].PowerState
	raw := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_vm_power","arguments":{"id":"` + vms[0].ID + `","action":"off","confirm":true}}}`
	denied := request("POST", "/mcp", "prod", raw)
	if denied.Code != http.StatusGone {
		t.Fatalf("profile-specific HTTP MCP permission ignored: %s", denied.Body.String())
	}
	after, err := client.GetVM(context.Background(), vms[0].ID)
	if err != nil || after.Info.PowerState != before {
		t.Fatal("denied operation changed simulated VM")
	}
	failed := request("POST", "/v1/profiles/connect", "prod", `{"name":"broken"}`)
	if failed.Code == 200 {
		t.Fatal("unreachable environment connected")
	}
	config, _ := target.Load(path)
	if config.Active != "prod" {
		t.Fatal("Web changed saved active environment")
	}
	base.storeTicket("lab-ticket", "wss://127.0.0.1/fixture")
	production, err := manager.backend(context.Background(), "prod")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := production.server.tickets.Load("lab-ticket"); exists {
		t.Fatal("console tickets were shared across environments")
	}
	if production.server.ESXi == base.ESXi || !strings.Contains(production.server.consolePath("ticket"), "profile=prod") {
		t.Fatal("profiles did not receive independent connections and ticket routing")
	}
	again := request(http.MethodGet, "/v1/vms", "lab", "")
	if again.Code != 200 {
		t.Fatalf("failed switch destroyed original connection: %s", again.Body.String())
	}
}
