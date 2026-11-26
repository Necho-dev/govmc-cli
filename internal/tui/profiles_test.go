package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func tuiDialogHarness(t *testing.T) (*tview.Application, *tview.Pages, *tview.Table, func(tcell.Key)) {
	t.Helper()
	app, pages, previous := tview.NewApplication(), tview.NewPages(), tview.NewTable()
	pages.AddPage("main", previous, true, true)
	app.SetRoot(pages, true).SetFocus(previous)
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(128, 48)
	pages.SetRect(0, 0, 128, 48)
	t.Cleanup(screen.Fini)
	return app, pages, previous, func(k tcell.Key) {
		pages.Draw(screen)
		pages.InputHandler()(tcell.NewEventKey(k, 0, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	}
}

func TestTUIProfilesPersistsEncryptedPasswordAcrossSessions(t *testing.T) {
	app, pages, _, key := tuiDialogHarness(t)
	path := filepath.Join(t.TempDir(), "profiles.json")
	t.Setenv("GOVMC_TEST_SESSION_SECRET", "")
	legacy := target.Target{Name: "legacy", Profile: target.Profile{URL: "https://legacy.example", Username: "root"}}.WithPassword("legacy-secret")
	sessions := map[string]target.Target{}
	var chosen target.Target
	openTUIProfiles(context.Background(), app, pages, path, legacy, &legacy, sessions, "en", func(next target.Target) { chosen = next })
	picker := app.GetFocus().(*tview.List)
	if picker.GetItemCount() != 1 {
		t.Fatal("legacy connection and add action must be available with no saved profiles")
	}
	picker.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	for _, entry := range [][2]string{{"Environment name", "lab"}, {"ESXi HTTPS URL", "https://lab.example"}, {"Username", "root"}, {"Password", "do-not-save-this"}} {
		input, ok := app.GetFocus().(*tview.InputField)
		if !ok || input.GetLabel() != entry[0] {
			t.Fatalf("field not keyboard reachable: %s (%T)", entry[0], app.GetFocus())
		}
		input.SetText(entry[1])
		key(tcell.KeyTab)
	}
	key(tcell.KeyTab)
	key(tcell.KeyTab)
	key(tcell.KeyEnter)
	if !pages.HasPage("profile-confirm") || chosen.Name != "" {
		t.Fatal("switch from existing environment skipped confirmation")
	}
	key(tcell.KeyTab)
	key(tcell.KeyEnter)
	if chosen.Name != "lab" || !chosen.HasPassword() {
		t.Fatalf("new environment not selected: %s", chosen.Name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "do-not-save-this") || os.Getenv("GOVMC_TEST_SESSION_SECRET") != "" {
		t.Fatal("session password leaked to config or process environment")
	}
	config, err := target.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Active != "" || config.Profiles["lab"].Insecure || config.Profiles["lab"].Password == "" {
		t.Fatal("session switching changed default environment or disabled TLS checks")
	}
	if !sessions["lab"].HasPassword() {
		t.Fatal("session credential was not cached")
	}
	// A new TUI session can reconnect without environment variables or cached secrets.
	sessions = map[string]target.Target{}
	openTUIProfiles(context.Background(), app, pages, path, chosen, &legacy, sessions, "en", func(next target.Target) { chosen = next })
	app.GetFocus().(*tview.List).SetCurrentItem(1)
	key(tcell.KeyEnter)
	if pages.HasPage("profile-password") {
		t.Fatal("saved encrypted credentials ignored")
	}
}

func TestTUIProfileSwitchRequiresConfirmation(t *testing.T) {
	app, pages, _, key := tuiDialogHarness(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := target.Edit(context.Background(), path, func(c *target.Config) error {
		for _, name := range []string{"lab", "prod"} {
			p := target.Profile{URL: "https://" + name + ".example:8443", Username: "root"}
			if err := target.SetPassword(path, name, &p, "test-secret"); err != nil {
				return err
			}
			c.Profiles[name] = p
		}
		c.Active = "lab"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	current, err := target.Named(path, "lab")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	called := 0
	sessions := map[string]target.Target{}
	openTUIProfiles(context.Background(), app, pages, path, current, nil, sessions, "en", func(next target.Target) {
		called++
		if next.Name != "prod" {
			t.Fatal("confirmed wrong target")
		}
	})
	picker := app.GetFocus().(*tview.List)
	picker.SetCurrentItem(1)
	for _, cancel := range []struct {
		key tcell.Key
		r   rune
	}{{tcell.KeyEnter, 0}, {tcell.KeyEscape, 0}, {tcell.KeyRune, 'n'}, {tcell.KeyRune, 'N'}} {
		key(tcell.KeyEnter)
		if !pages.HasPage("profile-confirm") || called != 0 || len(sessions) != 0 {
			t.Fatal("selection connected before confirmation")
		}
		app.GetFocus().InputHandler()(tcell.NewEventKey(cancel.key, cancel.r, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
		if pages.HasPage("profile-confirm") || app.GetFocus() != picker || picker.GetCurrentItem() != 1 {
			t.Fatal("cancel did not preserve picker state")
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("cancel changed saved default or credential")
	}
	key(tcell.KeyEnter)
	app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'Y', tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	if called != 1 || pages.HasPage("profiles") || pages.HasPage("profile-confirm") || !sessions["prod"].HasPassword() {
		t.Fatal("explicit confirmation did not connect exactly once")
	}
}

func TestTUIProfileMissingPasswordPromptsAndCancelRestoresPicker(t *testing.T) {
	app, pages, _, key := tuiDialogHarness(t)
	path := filepath.Join(t.TempDir(), "profiles.json")
	if err := target.Edit(context.Background(), path, func(c *target.Config) error {
		c.Profiles["lab"] = target.Profile{URL: "https://lab.example", Username: "root", PasswordEnv: "GOVMC_TEST_UNSET_PASSWORD"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOVMC_TEST_UNSET_PASSWORD", "")
	called := false
	openTUIProfiles(context.Background(), app, pages, path, target.Target{}, nil, map[string]target.Target{}, "en", func(target.Target) { called = true })
	key(tcell.KeyEnter)
	if !pages.HasPage("profile-password") {
		t.Fatal("missing credential prompt")
	}
	key(tcell.KeyEscape)
	if !pages.HasPage("profiles") || pages.HasPage("profile-password") || called {
		t.Fatal("cancel did not restore chooser")
	}
	key(tcell.KeyEnter)
	app.GetFocus().(*tview.InputField).SetText("session-secret")
	key(tcell.KeyTab)
	key(tcell.KeyTab)
	key(tcell.KeyEnter)
	if !called {
		t.Fatal("keyboard credential submission failed")
	}
}

func TestTUIPowerRequiresExplicitConfirmation(t *testing.T) {
	app, pages, previous, key := tuiDialogHarness(t)
	called := ""
	openTUIPower(app, pages, previous, target.Target{Name: "lab"}, esxi.VMInfo{ID: "vm-1", Name: "test", PowerState: "poweredOn"}, "en", func(action string) { called = action })
	picker := app.GetFocus().(*tview.List)
	if picker.GetItemCount() != 4 {
		t.Fatal("power actions do not match running VM")
	}
	key(tcell.KeyEnter)
	if called != "" || !pages.HasPage("power-confirm") {
		t.Fatal("power action skipped confirmation")
	}
	key(tcell.KeyEscape)
	if app.GetFocus() != picker {
		t.Fatal("Esc did not return to actions")
	}
	key(tcell.KeyEnter)
	key(tcell.KeyEnter)
	if called != "" {
		t.Fatal("Cancel submitted action")
	}
	key(tcell.KeyEnter)
	key(tcell.KeyTab)
	key(tcell.KeyEnter)
	if called != "off" || !pages.HasPage("saving") {
		t.Fatal("explicit confirmation failed")
	}
}

func TestTUIEnvironmentEditorSavesWithoutSwitchingAndKeepsPassword(t *testing.T) {
	app, pages, _, key := tuiDialogHarness(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := target.Edit(context.Background(), path, func(config *target.Config) error {
		for _, name := range []string{"lab", "staging"} {
			p := target.Profile{URL: "https://old.example", Username: "root"}
			if err := target.SetPassword(path, name, &p, "saved-secret"); err != nil {
				return err
			}
			config.Profiles[name] = p
		}
		config.Active = "lab"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	current, err := target.Named(path, "lab")
	if err != nil {
		t.Fatal(err)
	}
	connected := false
	openTUIProfiles(context.Background(), app, pages, path, current, nil, map[string]target.Target{}, "en", func(target.Target) { connected = true })
	picker := app.GetFocus().(*tview.List)
	if picker.GetItemCount() != 2 {
		t.Fatal("actions should not appear as profile rows")
	}
	label, _ := picker.GetItemText(0)
	if !strings.Contains(label, "HTTPS") {
		t.Fatal("profile protocol is missing")
	}
	runeKey := func(r rune) {
		app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	}
	runeKey('e')
	if !pages.HasPage("profile-edit") {
		t.Fatal("edit shortcut did not open form")
	}
	for _, field := range [][2]string{{"Environment name", "renamed"}, {"ESXi HTTPS URL", "https://new.example"}, {"Username", "operator"}, {"New password (blank keeps saved)", ""}} {
		input, ok := app.GetFocus().(*tview.InputField)
		if !ok || input.GetLabel() != field[0] {
			t.Fatalf("field not reachable: %s (%T)", field[0], app.GetFocus())
		}
		input.SetText(field[1])
		key(tcell.KeyTab)
	}
	runeKey(' ')
	key(tcell.KeyTab)
	key(tcell.KeyEnter)
	if connected || pages.HasPage("profile-edit") {
		t.Fatal("Save should return to picker without connecting")
	}
	config, err := target.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p, exists := config.Profiles["renamed"]
	if !exists || p.URL != "https://new.example" || p.Username != "operator" || !p.Insecure || p.Password == "" || config.Active != "renamed" {
		t.Fatal("edited fields were not saved")
	}
	saved, err := target.Named(path, "renamed")
	if err != nil || !saved.HasPassword() {
		t.Fatal("blank password cleared the saved credential")
	}
	// A duplicate name fails inline, and cancelling leaves the saved file intact.
	before, _ := os.ReadFile(path)
	runeKey('e')
	app.GetFocus().(*tview.InputField).SetText("staging")
	for range 5 {
		key(tcell.KeyTab)
	}
	key(tcell.KeyEnter)
	if !pages.HasPage("profile-edit") {
		t.Fatal("duplicate name was accepted")
	}
	key(tcell.KeyEscape)
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed/cancelled edit changed configuration")
	}
	key(tcell.KeyEnter)
	key(tcell.KeyTab)
	key(tcell.KeyEnter)
	if !connected {
		t.Fatal("updated profile could not be selected")
	}
}

func TestTUIAddEnvironmentSaveOnlyDoesNotConnect(t *testing.T) {
	app, pages, _, key := tuiDialogHarness(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	connected := false
	openTUIProfiles(context.Background(), app, pages, path, target.Target{}, nil, map[string]target.Target{}, "en", func(target.Target) { connected = true })
	app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	for _, value := range []string{"new-lab", "https://lab.example", "operator", "saved-secret"} {
		field, ok := app.GetFocus().(*tview.InputField)
		if !ok {
			t.Fatalf("new profile field not focusable: %T", app.GetFocus())
		}
		if field.GetFieldWidth() != 0 {
			t.Fatal("environment field width should fill the form")
		}
		field.SetText(value)
		key(tcell.KeyTab)
	}
	key(tcell.KeyTab)
	if button, ok := app.GetFocus().(*tview.Button); !ok || button.GetLabel() != "Save" {
		t.Fatal("first action must be Save")
	}
	key(tcell.KeyEnter)
	if connected || pages.HasPage("profile-add") {
		t.Fatal("Save should return to picker without connecting")
	}
	stored, err := target.Named(path, "new-lab")
	if err != nil || !stored.HasPassword() {
		t.Fatal("Save did not persist encrypted password")
	}
	config, _ := target.Load(path)
	if config.Active != "" {
		t.Fatal("Save changed the default environment")
	}
	key(tcell.KeyEnter)
	if !connected {
		t.Fatal("newly saved environment could not be connected")
	}
}

func TestTUIProfileDeleteRequiresSecondConfirmation(t *testing.T) {
	app, pages, _, key := tuiDialogHarness(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := target.Edit(context.Background(), path, func(config *target.Config) error {
		for _, name := range []string{"lab", "prod"} {
			p := target.Profile{URL: "https://" + name + ".example", Username: "root"}
			if err := target.SetPassword(path, name, &p, "secret"); err != nil {
				return err
			}
			config.Profiles[name] = p
		}
		config.Active = "lab"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	current, err := target.Named(path, "lab")
	if err != nil {
		t.Fatal(err)
	}
	sessions := map[string]target.Target{"lab": current}
	deleted := ""
	openTUIProfiles(context.Background(), app, pages, path, current, nil, sessions, "en", func(target.Target) { t.Fatal("deletion must not connect") }, func(name string) { deleted = name })
	runeKey := func(r rune) {
		app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	}
	runeKey('d')
	if !pages.HasPage("profile-delete") {
		t.Fatal("delete confirmation did not open")
	}
	config, _ := target.Load(path)
	if len(config.Profiles) != 2 {
		t.Fatal("delete shortcut removed data before confirmation")
	}
	key(tcell.KeyEnter)
	config, _ = target.Load(path)
	if len(config.Profiles) != 2 || deleted != "" {
		t.Fatal("default confirmation action should cancel")
	}
	key(tcell.KeyDelete)
	key(tcell.KeyTab)
	key(tcell.KeyEnter)
	config, _ = target.Load(path)
	if _, exists := config.Profiles["lab"]; exists || config.Active != "" || deleted != "lab" {
		t.Fatal("confirmed deletion did not remove the default profile")
	}
	if _, exists := sessions["lab"]; exists {
		t.Fatal("deleted credential remained in session cache")
	}
	if _, err := target.Named(path, "prod"); err != nil {
		t.Fatal("deletion damaged another profile")
	}
}
