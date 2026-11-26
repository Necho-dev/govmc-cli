package command

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestLanguagePrecedence(t *testing.T) {
	t.Setenv("GOVMC_CONFIG", filepath.Join(t.TempDir(), "profiles.json"))
	t.Setenv("GOVMC_LANG", "")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	execute := func(args ...string) string {
		t.Helper()
		cmd := New(args...)
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	execute("config", "language", "zh")
	if !strings.Contains(execute("vm", "--help"), "虚拟机操作") {
		t.Fatal("saved language was not used")
	}
	t.Setenv("GOVMC_LANG", "en")
	if !strings.Contains(execute("vm", "--help"), "Virtual machine operations") {
		t.Fatal("environment did not override saved language")
	}
	if !strings.Contains(execute("vm", "--language=zh-CN", "--help"), "虚拟机操作") {
		t.Fatal("flag did not override environment")
	}
}
