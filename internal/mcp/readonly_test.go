package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
)

func TestReadonlyCatalogAndDispatch(t *testing.T) {
	connect := func(context.Context, string) (*esxi.Client, string, func(), error) {
		t.Fatal("no connection should be opened for this request")
		return nil, "", nil, nil
	}
	for _, tc := range []struct{ method, args, want, forbidden string }{
		{"tools/list", "", "esxi_vm_power", ""},
		{"tools/call", `,"params":{"name":"esxi_vm_power","arguments":{"confirm":true}}`, "mcp_readonly", ""},
		{"tools/call", `,"params":{"name":"esxi_vm_screenshot","arguments":{"id":"vm-1","profile":5}}`, "nonempty string", ""},
	} {
		raw := `{"jsonrpc":"2.0","id":1,"method":"` + tc.method + `"` + tc.args + `}`
		result, err := HandleReadonly(context.Background(), []byte(raw), connect)
		if err != nil || !strings.Contains(string(result), tc.want) || (tc.forbidden != "" && strings.Contains(string(result), tc.forbidden)) {
			t.Fatalf("%s: %s (%v)", tc.method, result, err)
		}
	}
}

func TestReadonlyProfileOverrideReachesConnector(t *testing.T) {
	for _, name := range []string{"esxi_vm_list", "esxi_vm_screenshot", "esxi_doctor"} {
		profile := ""
		connect := func(_ context.Context, value string) (*esxi.Client, string, func(), error) {
			profile = value
			return nil, value, nil, errors.New("fixture: connection not needed")
		}
		raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":{"id":"vm-1","profile":"lab"}}}`)
		result, err := HandleReadonly(context.Background(), raw, connect)
		if err != nil || profile != "lab" || !strings.Contains(string(result), "fixture") {
			t.Fatalf("%s ignored temporary profile: %s (%v)", name, result, err)
		}
	}
}
