package tui

import (
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/rivo/tview"
)

func TestTUIFooterShortcutHighlights(t *testing.T) {
	for _, hint := range []string{
		" Enter details · e edit · p power · f filter",
		" Esc back · [/] tabs · s screenshot · e edit",
		" q quit   r refresh   f filter   ? help   u environment",
	} {
		for _, lang := range []language{"en", "zh"} {
			translated := lang.Text(hint)
			output := tuiShortcutHint(translated)
			if tview.TaggedStringWidth(output) != tview.TaggedStringWidth(tview.Escape(translated)) {
				t.Fatalf("highlight changed visible width: %q", output)
			}
			for _, key := range []string{"Enter", "Esc", "[/]", "e", "p", "f", "s", "q", "r", "?", "u"} {
				if strings.Contains(translated, " "+key+" ") && !strings.Contains(output, tuiColor("#f7b65c", key)) {
					t.Errorf("missing highlighted %q: %s", key, output)
				}
			}
		}
	}
}

func TestTUIFooterConnectionStates(t *testing.T) {
	current := target.Target{Name: "legacy", Profile: target.Profile{URL: "https://127.0.0.1:8443/sdk", Username: "root", Insecure: true}}
	for _, test := range []struct {
		connected, loading bool
		color, dot         string
	}{{false, false, "#f27878", "●"}, {true, false, "#73c936", "●"}, {true, true, "#f7b65c", "◌"}} {
		label := tuiFooterProfileLabel(current, test.connected, test.loading, "en")
		if !strings.Contains(label, tuiColor(test.color, test.dot)) || !strings.Contains(label, "root@127.0.0.1:8443 ▾") {
			t.Fatalf("incorrect endpoint or connection state: %q", label)
		}
		for _, redundant := range []string{"legacy", "TLS", " u ", "/sdk"} {
			if strings.Contains(label, redundant) {
				t.Fatalf("redundant profile field %q in %q", redundant, label)
			}
		}
	}
}

func TestTUIRepositoryLinkAndVersion(t *testing.T) {
	for _, version := range []string{"1.0.0-dev", "1.2.3", "1.2.3rc1", "v1.2.3", "V1.2.3"} {
		display := tuiDisplayVersion(version)
		if !strings.HasPrefix(display, "V") || strings.Contains(display, "Vv") || strings.Contains(display, "VV") {
			t.Fatal(display)
		}
	}
	for _, platform := range []string{"darwin", "linux", "windows"} {
		cmd := tuiRepositoryCommand(platform)
		if cmd.Args[len(cmd.Args)-1] != govmc.RepositoryURL {
			t.Fatal(cmd.Args)
		}
	}
}
