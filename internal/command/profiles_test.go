package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/target"
)

func TestCLIEncryptedProfilesAndTemporarySelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("GOVMC_CONFIG", path)
	t.Setenv("GOVMC_PROFILE", "")
	t.Setenv("GOVMC_LANG", "en")
	run := func(input string, args ...string) (string, error) {
		cmd := New(args...)
		cmd.SetArgs(args)
		cmd.SetIn(strings.NewReader(input))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		err := cmd.Execute()
		return out.String(), err
	}
	for _, name := range []string{"lab", "prod"} {
		if _, err := run(name+"-secret\n", "profiles", "add", name, "--url", "https://"+name+".example", "--username", "root", "--password-stdin"); err != nil {
			t.Fatal(err)
		}
	}
	config, err := target.Load(path)
	if err != nil || config.Active != "lab" {
		t.Fatal("first profile should become the default")
	}
	for _, args := range [][]string{{"profiles", "list", "--json"}, {"profiles", "show", "lab", "--json"}} {
		output, err := run("", args...)
		if err != nil || strings.Contains(output, "secret") || strings.Contains(output, "enc:v1:") {
			t.Fatalf("credential exposed during inspection: %s %v", output, err)
		}
	}
	if _, err := run("", "profiles", "use", "prod"); err != nil {
		t.Fatal(err)
	}
	output, err := run("", "--profile", "lab", "profiles", "current", "--json")
	if err != nil || !strings.Contains(output, `"name":"lab"`) {
		t.Fatalf("temporary target ignored: %s %v", output, err)
	}
	config, _ = target.Load(path)
	if config.Active != "prod" {
		t.Fatal("--profile changed saved default")
	}
	if _, err := run("rotated-secret\n", "profiles", "update", "lab", "--password-stdin"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("", "profiles", "update", "lab", "--url", "https://new.example"); err != nil {
		t.Fatal(err)
	}
	lab, err := target.Named(path, "lab")
	if err != nil || !lab.HasPassword() || lab.URL != "https://new.example" {
		t.Fatal("profile edit lost stored password")
	}
	output, err = run("", "mcp", "config", "--client", "codex", "--json")
	if err != nil || strings.Contains(output, "--profile") {
		t.Fatalf("default MCP configuration should follow active profile: %s %v", output, err)
	}
	output, err = run("", "--profile", "lab", "mcp", "config", "--client", "codex", "--json")
	if err != nil || !strings.Contains(output, `"--profile","lab"`) || strings.Contains(output, "secret") {
		t.Fatalf("fixed MCP configuration not generated: %s %v", output, err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "rotated-secret") || strings.Contains(string(raw), "lab-secret") {
		t.Fatal("CLI saved plaintext")
	}
	if _, err := run("", "profiles", "add", "missing", "--url", "https://missing.example", "--username", "root"); err == nil || !strings.Contains(err.Error(), "password-stdin") {
		t.Fatal("noninteractive missing password should be actionable")
	}
}

func TestMCPPermissionsAreConfiguredOnlyByCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("GOVMC_CONFIG", path)
	t.Setenv("GOVMC_LANG", "en")
	run := func(input string, args ...string) (string, error) {
		cmd := New(args...)
		cmd.SetArgs(args)
		cmd.SetIn(strings.NewReader(input))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		err := cmd.Execute()
		return out.String(), err
	}
	output, err := run("", "config", "mcp-mode")
	if err != nil || !strings.Contains(output, "readonly") {
		t.Fatal("default MCP mode is not readonly")
	}
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	output, err = run(list, "mcp", "serve")
	if err != nil || !strings.Contains(output, "esxi_vm_power") {
		t.Fatal("stable tool catalog missing writes")
	}
	if _, err := run("", "config", "set", "--mcp-mode", "readwrite"); err != nil {
		t.Fatal(err)
	}
	output, err = run(list, "mcp", "serve")
	if err != nil || !strings.Contains(output, "esxi_vm_power") {
		t.Fatal("CLI mode change did not enable write tools")
	}
	if _, err := run(list, "mcp", "serve", "--mode", "readwrite"); err == nil {
		t.Fatal("process mode override must not exist")
	}
	config, _ := target.Load(path)
	if config.MCP.Mode != "readwrite" {
		t.Fatal("process changed global mode")
	}
	if _, err := run("", "config", "mcp-mode", "readonly"); err != nil {
		t.Fatal(err)
	}
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_vm_list","arguments":{"mode":"readwrite"}}}` + "\n"
	output, err = run(request, "mcp", "serve")
	if err != nil || !strings.Contains(output, "only be changed via CLI") {
		t.Fatal("MCP mode injection was not rejected")
	}
	config, _ = target.Load(path)
	if config.MCP.Mode != "readonly" {
		t.Fatal("MCP changed saved permissions")
	}
	if _, err := run("", "config", "mcp-mode", "invalid"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
