package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/target"
)

type mcpScriptReader struct {
	steps   []func() string
	pending string
}

func (r *mcpScriptReader) Read(p []byte) (int, error) {
	if r.pending == "" {
		if len(r.steps) == 0 {
			return 0, io.EOF
		}
		r.pending = r.steps[0]() + "\n"
		r.steps = r.steps[1:]
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func TestMCPRunningProcessReloadsCLIPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("GOVMC_CONFIG", path)
	t.Setenv("GOVMC_LANG", "en")
	t.Setenv("GOVMC_PROFILE", "")
	if err := target.Edit(context.Background(), path, func(c *target.Config) error {
		c.Active = "lab"
		c.Profiles["lab"] = target.Profile{URL: "https://lab.example", Username: "root"}
		c.Profiles["prod"] = target.Profile{URL: "https://prod.example", Username: "root"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cli := func(args ...string) string {
		cmd := New(args...)
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out.String()
	}
	call := func(profile string) string {
		args := `"id":"vm-1","action":"off"`
		if profile != "" {
			args += `,"profile":"` + profile + `"`
		}
		return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_vm_power","arguments":{` + args + `}}}`
	}
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	var validConfig []byte
	reader := &mcpScriptReader{steps: []func() string{
		func() string { return list },
		func() string { return call("") },
		func() string { cli("config", "set", "--mcp-mode", "readwrite"); return call("") },
		func() string { cli("profiles", "update", "lab", "--mcp-mode", "readonly"); return call("") },
		func() string { return call("prod") },
		func() string { cli("config", "set", "--mcp-mode", "readonly"); return call("prod") },
		func() string { cli("profiles", "update", "prod", "--mcp-mode", "readwrite"); return call("prod") },
		func() string {
			cli("profiles", "update", "lab", "--mcp-mode", "inherit")
			cli("config", "set", "--mcp-mode", "readwrite")
			return call("")
		},
		func() string { return list },
		func() string {
			var err error
			validConfig, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			invalid := bytes.Replace(validConfig, []byte("mode: readwrite"), []byte("mode: full"), 1)
			if err := os.WriteFile(path, invalid, 0600); err != nil {
				t.Fatal(err)
			}
			return call("")
		},
		func() string {
			if err := os.WriteFile(path, validConfig, 0600); err != nil {
				t.Fatal(err)
			}
			return call("")
		},
		func() string {
			return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_profiles_list","arguments":{}}}`
		},
		func() string {
			return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_config_show","arguments":{"mode":"readwrite"}}}`
		},
	}}
	args := []string{"mcp", "serve"}
	cmd := New(args...)
	cmd.SetArgs(args)
	cmd.SetIn(reader)
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 13 {
		t.Fatalf("expected 13 replies, got %d", len(lines))
	}
	for i, want := range []string{"esxi_vm_power", "mcp_readonly", "confirm=true", "mcp_readonly", "confirm=true", "mcp_readonly", "mcp_readonly", "confirm=true", "esxi_vm_power", "mcp_mode_invalid", "confirm=true", "mcp_effective", "only be changed via CLI"} {
		if !strings.Contains(lines[i], want) {
			t.Fatalf("reply %d expected %s: %s", i, want, lines[i])
		}
	}
	if lines[0] != lines[8] {
		t.Fatal("mode changes altered the stable tool catalog")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(cli("mcp", "privilege", "--json")), &record); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cli("mcp", "json", "--client", "codex"), `"mcp","serve"`) {
		t.Fatal("MCP launch configuration missing")
	}
}
