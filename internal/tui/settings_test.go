package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTUISettingsWithoutConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	store, err := journal.Open(path, "cli")
	if err != nil {
		t.Fatal(err)
	}
	store.Log(journal.Event{Event: "fixture.runtime"})
	_ = journal.Run(journal.With(context.Background(), store), journal.Event{Operation: "fixture.operation"}, func() error { return nil })
	_ = store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	screen := tcell.NewSimulationScreen("UTF-8")
	app := tview.NewApplication().SetScreen(screen)
	done := make(chan error, 1)
	go func() { done <- runTUIApplication(ctx, path, target.Target{}, "zh", app) }()
	defer func() {
		app.Stop()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("settings did not stop")
		}
	}()
	read := func() string {
		var text strings.Builder
		app.QueueUpdateDraw(func() {
			cells, w, _ := screen.GetContents()
			skip := false
			for i, c := range cells {
				if i%w == 0 {
					text.WriteByte('\n')
					skip = false
				}
				if skip {
					skip = false
					continue
				}
				text.WriteString(string(c.Runes))
				skip = len(c.Runes) > 0 && c.Runes[0] >= 0x2e80 && c.Runes[0] <= 0xffef
			}
		})
		return strings.ReplaceAll(text.String(), " ", "")
	}
	wait := func(text string) {
		t.Helper()
		until := time.Now().Add(8 * time.Second)
		for time.Now().Before(until) {
			if strings.Contains(read(), strings.ReplaceAll(text, " ", "")) {
				return
			}
			time.Sleep(15 * time.Millisecond)
		}
		t.Fatalf("missing %q in %s", text, read())
	}
	waitFocus := func(input bool) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			matched := false
			app.QueueUpdateDraw(func() {
				if input {
					_, matched = app.GetFocus().(*tview.InputField)
				} else {
					_, matched = app.GetFocus().(*tview.Table)
				}
			})
			if matched {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("settings focus did not reach expected control")
	}
	key := func(k tcell.Key, r rune) { screen.PostEventWait(tcell.NewEventKey(k, r, tcell.ModNone)) }
	capture := func(name string) {
		if dir := os.Getenv("GOVMC_TUI_CAPTURE_DIR"); dir != "" {
			_ = os.MkdirAll(dir, 0700)
			app.QueueUpdateDraw(func() {
				type cell struct {
					Text   string
					FG, BG int32
				}
				cells, w, h := screen.GetContents()
				out := struct {
					W, H  int
					Cells []cell
				}{W: w, H: h}
				for _, c := range cells {
					fg, bg, _ := c.Style.Decompose()
					out.Cells = append(out.Cells, cell{string(c.Runes), fg.Hex(), bg.Hex()})
				}
				raw, _ := json.Marshal(out)
				_ = os.WriteFile(filepath.Join(dir, name+".json"), raw, 0600)
			})
		}
	}
	wait("当前语言")
	app.QueueUpdateDraw(func() { screen.SetSize(128, 42) })
	key(tcell.KeyRune, ']')
	wait("MCP 权限")
	capture("settings-mcp")
	waitFocus(false)
	key(tcell.KeyDown, 0)
	key(tcell.KeyEnter, 0)
	wait("readonly → readwrite")
	cfgBefore, _ := target.Load(path)
	if cfgBefore.MCP.Mode != target.MCPReadonly {
		t.Fatal("mode changed before confirmation")
	}
	key(tcell.KeyEscape, 0)
	waitFocus(false)
	key(tcell.KeyEnter, 0)
	wait("readonly → readwrite")
	key(tcell.KeyRune, 'y')
	wait("readwrite")
	waitFocus(false)
	key(tcell.KeyUp, 0)
	key(tcell.KeyEnter, 0)
	wait("readwrite → readonly")
	key(tcell.KeyRune, 'y')
	waitFocus(false)
	key(tcell.KeyRune, ']')
	wait("暂无已保存环境")
	capture("settings-profiles")
	key(tcell.KeyRune, ']')
	wait("暂无任务")
	capture("settings-tasks")
	key(tcell.KeyRune, ']')
	wait("fixture.runtime")
	capture("settings-logs")
	key(tcell.KeyRune, 'f')
	waitFocus(true)
	wait("搜索")
	app.QueueUpdateDraw(func() {
		input, ok := app.GetFocus().(*tview.InputField)
		if !ok {
			t.Errorf("filter not focusable: %T", app.GetFocus())
			return
		}
		input.SetText("fixture.runtime")
	})
	key(tcell.KeyTab, 0)
	deadline := time.Now().Add(3 * time.Second)
	reached := false
	for time.Now().Before(deadline) {
		app.QueueUpdateDraw(func() { _, reached = app.GetFocus().(*tview.DropDown) })
		if reached {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !reached {
		t.Fatal("Tab did not reach source filter")
	}
	key(tcell.KeyEnter, 0)
	capture("settings-source-menu")
	key(tcell.KeyDown, 0) // All sources -> cli.
	key(tcell.KeyEnter, 0)
	key(tcell.KeyTab, 0) // Level.
	key(tcell.KeyTab, 0) // Query.
	key(tcell.KeyEnter, 0)
	waitFocus(false)
	wait("fixture.runtime")
	key(tcell.KeyRune, ']') // Audit search is scoped to the module; clear filter first.
	wait("暂无匹配记录")
	waitFocus(false)
	// Shared metadata search is intentionally retained between Logs and Audits.
	key(tcell.KeyRune, 'f')
	waitFocus(true)
	app.QueueUpdateDraw(func() {
		if input, ok := app.GetFocus().(*tview.InputField); ok {
			input.SetText("")
		}
	})
	key(tcell.KeyEnter, 0)
	waitFocus(false)
	wait("fixture.operation")
	capture("settings-audits")
	for i := 0; i < 5; i++ {
		key(tcell.KeyRune, '[')
	}
	wait("当前语言")
	waitFocus(false)
	key(tcell.KeyUp, 0)
	key(tcell.KeyEnter, 0)
	wait("Current language")
	capture("settings-language")
	cfg, err := target.Load(path)
	if err != nil || cfg.Language != "en" || cfg.MCP.Mode != target.MCPReadonly {
		t.Fatal("language failed to persist or changed MCP mode", err)
	}
	for _, width := range []int{128, 80} {
		app.QueueUpdateDraw(func() { screen.SetSize(width, 42) })
		for module, label := range []string{"Current language", "MCP permissions", "No saved profiles", "No tasks", "fixture.runtime", "fixture.operation"} {
			if module > 0 {
				key(tcell.KeyRune, ']')
			}
			if module == 4 {
				key(tcell.KeyRune, 'f')
				waitFocus(true)
				app.QueueUpdateDraw(func() { app.GetFocus().(*tview.InputField).SetText("") })
				key(tcell.KeyEnter, 0)
			}
			wait(label)
			capture(fmt.Sprintf("settings-%d-%d", width, module))
		}
		for i := 0; i < 5; i++ {
			key(tcell.KeyRune, '[')
		}
	}
	key(tcell.KeyEscape, 0)
	wait("Overview")
}

func TestTUISettingsMCPProfileModeConfirmation(t *testing.T) {
	app, pages, _, key := tuiDialogHarness(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	ctx := context.Background()
	err := target.Edit(ctx, path, func(c *target.Config) error {
		c.MCP.Mode = target.MCPReadonly
		c.Profiles["lab"] = target.Profile{URL: "https://lab.example/sdk", Username: "root"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newTUISettings(ctx, app, path, nil, func() language { return "en" }, func(language) {}, func() target.Target { return target.Target{Name: "lab"} }, func() {})
	s.pages = pages
	s.width = 128
	s.module = 1
	s.Render()
	pages.AddPage("main", s.root, true, true)
	app.SetFocus(s.table)
	s.Handle(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	if s.table.GetRowCount() != 4 {
		t.Fatal("profile mode must include inherit")
	}
	s.table.Select(3, 0)
	key(tcell.KeyEnter)
	cfg, _ := target.Load(path)
	if cfg.Profiles["lab"].MCP.Mode != "" {
		t.Fatal("mode changed before confirmation")
	}
	key(tcell.KeyEscape)
	cfg, _ = target.Load(path)
	if cfg.Profiles["lab"].MCP.Mode != "" {
		t.Fatal("cancel changed mode")
	}
	key(tcell.KeyEnter)
	app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	cfg, err = target.Load(path)
	if err != nil || cfg.Profiles["lab"].MCP.Mode != target.MCPReadwrite {
		t.Fatal("confirmed profile mode not saved", err)
	}
	effective, err := target.EffectiveMCPMode(cfg, "lab")
	if err != nil || effective != target.MCPReadonly {
		t.Fatal("profile mode bypassed global ceiling", err)
	}
	// CLI and a running MCP read the same saved policy without restarting.
	mode, err := target.MCPMode(path, "lab")
	if err != nil || mode != target.MCPReadonly {
		t.Fatal("saved mode differs from effective MCP mode", err)
	}
	s.table.Select(1, 0)
	key(tcell.KeyEnter)
	app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	cfg, _ = target.Load(path)
	if cfg.Profiles["lab"].MCP.Mode != "" {
		t.Fatal("inherit mode did not clear profile override")
	}
}
