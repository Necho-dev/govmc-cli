package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
)

func TestMCPModeCatalogAndDispatchAreEnforced(t *testing.T) {
	list := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	connect := func(context.Context, string) (*esxi.Client, string, func(), error) {
		t.Fatal("denied calls must not dial ESXi")
		return nil, "", nil, nil
	}
	for _, mode := range []string{Readonly, Readwrite} {
		out, err := HandleConfigured(context.Background(), list, connect, mode)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "esxi_vm_power") {
			t.Fatalf("incorrect %s catalog: %s", mode, out)
		}
		if strings.Contains(string(out), "esxi_clone\"") || strings.Contains(string(out), "esxi_vm_console_ticket") {
			t.Fatal("catalog advertised unavailable transport services")
		}
		if strings.Contains(string(out), "esxi_config_set") {
			t.Fatal("MCP exposed a permission mutation tool")
		}
	}
	for _, name := range []string{"esxi_vm_power", "esxi_ds_folder_create", "esxi_ds_folder_delete", "esxi_ds_copy", "esxi_ds_move", "esxi_vm_console_input", guestExecTool, "esxi_vm_clone"} {
		raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":{"confirm":true,"dryRun":false}}}`)
		out, err := HandleConfigured(context.Background(), raw, connect, Readonly)
		if err != nil || !strings.Contains(string(out), `"isError":true`) {
			t.Fatalf("readonly allowed %s: %s %v", name, out, err)
		}

	}
	raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_vm_power","arguments":{"id":"vm-1","action":"off"}}}`)
	out, err := HandleConfigured(context.Background(), raw, connect, Readwrite)
	if err != nil || !strings.Contains(string(out), "confirm=true") {
		t.Fatal("readwrite dropped write confirmation")
	}
	raw = []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_vm_list","arguments":{"mode":"readwrite"}}}`)
	out, err = HandleConfigured(context.Background(), raw, connect, Readonly)
	if err != nil || !strings.Contains(string(out), "only be changed via CLI") {
		t.Fatal("MCP request changed permission mode")
	}
	raw = []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_vm_power","arguments":{"id":"vm-1","action":"off","confirm":true,"profile":"lab"}}}`)
	selected := ""
	permitted := func(_ context.Context, profile string) (*esxi.Client, string, func(), error) {
		selected = profile
		return nil, profile, nil, errors.New("fixture: dispatch allowed")
	}
	out, err = HandleConfigured(context.Background(), raw, permitted, Readwrite)
	if err != nil || selected != "lab" || !strings.Contains(string(out), "dispatch allowed") {
		t.Fatal("explicitly enabled readwrite was not dispatched")
	}
	if _, err := HandleConfigured(context.Background(), list, connect, "invalid"); err == nil {
		t.Fatal("unknown permission mode should fail closed")
	}
}
