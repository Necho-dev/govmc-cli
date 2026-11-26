package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleActionsRejectMalformedInputBeforeDial(t *testing.T) {
	for _, value := range []string{
		`[]`, `null`, `[ {"type":"text","text":"中文"} ]`,
		`[{"type":"text","text":"ok","secret":"typo"}]`,
		`[{"type":"key","key":"a","modifiers":["ctrl","ctrl"]}]`,
		`[{"type":"wait","ms":2001}]`, `[{"type":"wait","ms":1}] []`,
		`[{"type":"click","x":-1,"y":0}]`, strings.Repeat(" ", 65537),
	} {
		if _, err := readConsoleActions(strings.NewReader(value)); err == nil {
			t.Fatalf("accepted malformed actions: %.100s", value)
		}
	}
	got, err := readConsoleActions(strings.NewReader(`[{"type":"key","key":"r","modifiers":["meta"]},{"type":"text","text":"hostname"},{"type":"key","key":"enter"}]`))
	if err != nil || len(got) != 3 {
		t.Fatalf("valid actions: %+v %v", got, err)
	}
}

func TestConsoleInputNonInteractiveRequiresConfirmationBeforeDial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions.json")
	if err := os.WriteFile(path, []byte(`[{"type":"key","key":"enter"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := New("--language", "en")
	cmd.SetArgs([]string{"vm", "console", "input", "fixture", "--actions", path})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	// Test runners have non-TTY stdin. No config, journal or ESXi connection may be opened.
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("missing pre-connection confirmation: %v", err)
	}
}
