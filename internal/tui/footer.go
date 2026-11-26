package tui

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/Necho-dev/govmc-cli"

	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/rivo/tview"
)

func tuiShortcutHint(hint string) string {
	words := strings.Split(hint, " ")
	for i, word := range words {
		switch word {
		case "Enter", "Esc", "[/]", "q", "r", "f", "?", "u", "e", "p", "s", "c", "m":
			words[i] = tuiColor("#f7b65c", word)
		default:
			words[i] = tview.Escape(word)
		}
	}
	return strings.Join(words, " ")
}

func tuiFooterProfileLabel(current target.Target, connected, loading bool, lang language) string {
	color, dot := "#f27878", "●"
	if connected {
		color = "#73c936"
	}
	if loading {
		color, dot = "#f7b65c", "◌"
	}
	endpoint := tuiEndpoint(current)
	if current.Name == "" {
		endpoint = lang.Text("Choose environment")
		color = "#9ba7b0"
	}
	return " " + tuiColor(color, dot) + " " + tview.Escape(endpoint) + " ▾ "
}

// Same Font Awesome / Nerd Font glyph used by openwrt-cli.
const tuiGitHubIcon = "\uf09b"

func tuiDisplayVersion(version string) string {
	return "V" + strings.TrimLeft(version, "vV")
}

func tuiRepositoryCommand(platform string) *exec.Cmd {
	switch platform {
	case "darwin":
		return exec.Command("open", govmc.RepositoryURL)
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", govmc.RepositoryURL)
	default:
		return exec.Command("xdg-open", govmc.RepositoryURL)
	}
}

func openTUIRepository() error {
	cmd := tuiRepositoryCommand(runtime.GOOS)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
