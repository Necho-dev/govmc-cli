package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTUITaskFiltersLanguagesWidthsAndKeyboard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := tasks.Open(path, "tui")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	job, err := manager.Submit(context.Background(), "prod", "https://prod.invalid/sdk", "file.copy", "filter-target", nil, "target", "", func(context.Context, *tasks.Reporter) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v, _ := manager.Get(job.ID)
		if v.Status == "succeeded" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, lang := range []language{"en", "zh"} {
		for _, width := range []int{64, 80, 110, 128} {
			t.Run(fmt.Sprintf("%s-%d", lang, width), func(t *testing.T) {
				screen := tcell.NewSimulationScreen("UTF-8")
				if err := screen.Init(); err != nil {
					t.Fatal(err)
				}
				defer screen.Fini()
				screen.SetSize(width, 36)
				app := tview.NewApplication()
				s := newTUISettings(tasks.With(context.Background(), manager), app, path, nil, func() language { return lang }, func(language) {}, func() target.Target { return target.Target{Name: "lab"} }, func() {})
				s.pages = tview.NewPages()
				s.module = 3
				s.width = width
				s.Render()
				s.root.SetRect(0, 0, width, 36)
				s.root.Draw(screen)
				var menus []*tview.DropDown
				var query *tview.Button
				for _, control := range s.focus {
					if menu, ok := control.(*tview.DropDown); ok {
						menus = append(menus, menu)
					}
					if button, ok := control.(*tview.Button); ok && button.GetLabel() == lang.Text("Query") {
						query = button
					}
				}
				if len(menus) != 2 || query == nil {
					t.Fatal("task filters missing")
				}
				for _, control := range []tview.Primitive{s.searchField, menus[0], menus[1], query} {
					x, _, w, _ := control.GetRect()
					if x < 0 || x+w > width || w < 8 {
						t.Fatalf("filter clipped at %s/%d: %d+%d", lang, width, x, w)
					}
				}
				_, caption := menus[1].GetCurrentOption()
				if caption != lang.Text("Current environment") {
					t.Fatal("scope language mismatch", caption)
				}
				app.SetFocus(s.searchField)
				s.searchField.SetText("filter-target")
				s.searchField.InputHandler()(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
				if app.GetFocus() != menus[0] {
					t.Fatal("Tab did not reach status")
				}
				menus[0].SetCurrentOption(3) // Succeeded.
				menus[1].SetCurrentOption(1) // All environments.
				app.SetFocus(query)
				query.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
				if !s.taskAll || s.taskStatusIndex != 3 || s.table.GetRowCount() != 2 || s.table.GetCell(1, 0).Text != "filter-target" {
					t.Fatal("query did not apply search, status and scope")
				}
				s.root.SetRect(0, 0, width, 36)
				s.root.Draw(screen)
				screen.Show()
				if dir := os.Getenv("GOVMC_TUI_CAPTURE_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
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
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("task-filters-%s-%d.json", lang, width)), raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
