package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var cliOnce sync.Once
var cliPath string
var cliError error
var cliOutput []byte

func cliBinary(t *testing.T) string {
	t.Helper()
	cliOnce.Do(func() {
		var dir string
		dir, cliError = os.MkdirTemp("", "govmc_cli_test_")
		if cliError != nil {
			return
		}
		cliPath = filepath.Join(dir, "govmc")
		if os.PathSeparator == '\\' {
			cliPath += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", cliPath, "../../cmd/govmc")
		cliOutput, cliError = cmd.CombinedOutput()
	})
	if cliError != nil {
		t.Fatalf("build CLI: %v %s", cliError, cliOutput)
	}
	return cliPath
}
func TestMain(m *testing.M) {
	code := m.Run()
	if cliPath != "" {
		os.RemoveAll(filepath.Dir(cliPath))
	}
	os.Exit(code)
}
