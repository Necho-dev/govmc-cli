package command

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWMKSCommandsAreLocalAndRequireConfirmation(t *testing.T) {
	t.Setenv("GOVMC_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("GOVMC_LANG", "en")
	run := func(args ...string) (string, error) {
		c := New(args...)
		c.SetArgs(args)
		c.SetIn(strings.NewReader(""))
		var b bytes.Buffer
		c.SetOut(&b)
		c.SetErr(io.Discard)
		e := c.Execute()
		return b.String(), e
	}
	out, e := run("wmks", "status", "--json")
	if e != nil || !strings.Contains(out, `"state":"missing"`) {
		t.Fatal(out, e)
	}
	if _, e = run("wmks", "verify"); e == nil {
		t.Fatal("missing SDK verified")
	}
	if _, e = run("wmks", "install", "--file", filepath.Join(os.TempDir(), "does-not-exist-sdk.zip")); e == nil {
		t.Fatal("missing file installed")
	}
}

func TestExplicitConfigAndTemporaryWMKSFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nwmks:\n  directory: ./sdk\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOVMC_CONFIG", filepath.Join(t.TempDir(), "unrelated.yaml"))
	args := []string{"--config", path, "mcp", "config", "--json"}
	cmd := New(args...)
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), path) || strings.Contains(out.String(), "GOVMC_CONFIG") {
		t.Fatal(out.String())
	}
	cmd = New()
	serve, _, err := cmd.Find([]string{"serve"})
	if err != nil {
		t.Fatal(err)
	}
	if err := serve.ParseFlags([]string{"--wmks", "./temporary"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := serve.Flags().GetString("wmks"); got != "./temporary" {
		t.Fatal(got)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "directory: ./sdk") {
		t.Fatal("config mutated", err)
	}
}
