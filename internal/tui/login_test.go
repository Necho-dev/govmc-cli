package tui

import (
	"context"
	"crypto/tls"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/vmware/govmomi/simulator"
)

// Strict simulator authentication checks the actual SOAP username/password,
// rather than accepting any nonempty credential as default vcsim does.
func TestTUIPasswordEncryptionAndLogin(t *testing.T) {
	if os.Getenv("GOVMC_TUI_SIM_TEST") != "1" {
		t.Skip("set GOVMC_TUI_SIM_TEST=1 to run the local simulator")
	}
	for _, secret := range []string{"p@ss:%40#$?&+\\\"'<>", " 密码 with spaces ", "literal%2F%40%25"} {
		t.Run("credential", func(t *testing.T) {
			model := simulator.ESX()
			model.Machine = 1
			if err := model.Create(); err != nil {
				t.Fatal(err)
			}
			defer model.Remove()
			model.Service.TLS = &tls.Config{}
			model.Service.Listen = &url.URL{User: url.UserPassword("root", secret)}
			server := model.Service.NewServer()
			defer server.Close()
			endpoint := *server.URL
			endpoint.User = nil
			app, pages, _, key := tuiDialogHarness(t)
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			var chosen target.Target
			openTUIProfiles(context.Background(), app, pages, configPath, target.Target{}, nil, map[string]target.Target{}, "en", func(next target.Target) { chosen = next })
			key(tcell.KeyEnter) // Empty picker offers Add environment.
			for _, value := range []string{"lab", endpoint.String(), "root", secret} {
				input, ok := app.GetFocus().(*tview.InputField)
				if !ok {
					t.Fatalf("expected input field, got %T", app.GetFocus())
				}
				input.SetText("")
				for _, r := range value {
					input.InputHandler()(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
				}
				key(tcell.KeyTab)
			}
			app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
			key(tcell.KeyTab) // Save.
			key(tcell.KeyTab) // Save and Connect.
			key(tcell.KeyEnter)
			if chosen.Name != "lab" {
				t.Fatal("TUI did not save and select profile")
			}
			checkLogin := func(connection target.Target) {
				t.Helper()
				ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				client, err := connection.Dial(ctx)
				if err != nil {
					t.Fatalf("saved TUI credential was rejected: %v", err)
				}
				target.Close(client)
			}
			checkLogin(chosen)
			reloaded, err := target.Named(configPath, "lab")
			if err != nil {
				t.Fatal(err)
			}
			checkLogin(reloaded)
			config, _ := target.Load(configPath)
			original := config.Profiles["lab"]
			if err := target.UpdateProfile(context.Background(), configPath, "lab", "renamed", original, original, ""); err != nil {
				t.Fatal(err)
			}
			renamed, err := target.Named(configPath, "renamed")
			if err != nil {
				t.Fatal(err)
			}
			checkLogin(renamed)
			// Edit through the same TUI form used in a failed-login recovery.
			openTUIProfiles(context.Background(), app, pages, configPath, renamed, nil, map[string]target.Target{}, "en", func(next target.Target) { chosen = next })
			app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
			for i := 0; i < 3; i++ {
				key(tcell.KeyTab)
			}
			rotated := secret + " updated"
			app.GetFocus().(*tview.InputField).SetText(rotated)
			key(tcell.KeyTab) // TLS checkbox.
			key(tcell.KeyTab) // Save.
			key(tcell.KeyTab) // Save and Connect.
			key(tcell.KeyEnter)
			if pages.HasPage("profile-confirm") {
				key(tcell.KeyTab)
				key(tcell.KeyEnter)
			}
			model.Service.Listen.User = url.UserPassword("root", rotated)
			checkLogin(chosen)
			renamed, err = target.Named(configPath, "renamed")
			if err != nil {
				t.Fatal(err)
			}
			checkLogin(renamed)
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			client, err := renamed.WithPassword("deliberately-incorrect").Dial(ctx)
			if err == nil {
				target.Close(client)
				t.Fatal("strict simulator accepted wrong password")
			}
			if !strings.Contains(err.Error(), "authenticate ESXi:") {
				t.Fatalf("authentication failure lost login stage: %v", err)
			}
		})
	}
}
