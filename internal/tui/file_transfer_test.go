package tui

import (
	"context"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTUIFileTransferPreviewBackConfirmAndTask(t *testing.T) {
	for _, test := range []struct {
		lang  language
		width int
		copy  bool
	}{{"en", 128, true}, {"zh", 64, false}} {
		t.Run(string(test.lang), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			screen := tcell.NewSimulationScreen("UTF-8")
			app := tview.NewApplication().SetScreen(screen)
			pages := tview.NewPages()
			previous := tview.NewTable()
			pages.AddPage("main", previous, true, true)
			app.SetRoot(pages, true).SetFocus(previous)
			done := make(chan error, 1)
			go func() { done <- app.Run() }()
			defer func() {
				app.Stop()
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(3 * time.Second):
					t.Error("UI did not stop")
				}
			}()
			type call struct {
				dstDS, dst   string
				dry, confirm bool
			}
			calls := make(chan call, 4)
			submitted := make(chan tasks.Task, 1)
			transfer := func(_ context.Context, dstDS, dst string, dry, confirm bool) (any, error) {
				calls <- call{dstDS, dst, dry, confirm}
				if dry {
					return &esxi.StepResult{DryRun: true, Detail: esxi.FileTransferPlan{Datastore: "ds", Source: "folder/source.txt", Destination: dst, DestinationDatastore: dstDS}}, nil
				}
				return tasks.Task{ID: "task-ui-fixture", Kind: "file.move"}, nil
			}
			app.QueueUpdateDraw(func() {
				screen.SetSize(test.width, 30)
				openTUIFileTransferForm(ctx, app, pages, target.Target{Name: "lab"}, "ds", "folder/source.txt", test.copy, test.lang, transfer, func(job tasks.Task) { submitted <- job; app.SetFocus(previous) }, "other")
			})
			wait := func(predicate func() bool) {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					ok := false
					app.QueueUpdateDraw(func() { ok = predicate() })
					if ok {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatal("UI state not reached")
			}
			key := func(k tcell.Key, r rune) { screen.PostEventWait(tcell.NewEventKey(k, r, tcell.ModNone)) }
			app.QueueUpdateDraw(func() { app.GetFocus().(*tview.DropDown).SetCurrentOption(1) })
			key(tcell.KeyTab, 0)
			wait(func() bool { _, ok := app.GetFocus().(*tview.InputField); return ok })
			app.QueueUpdateDraw(func() { app.GetFocus().(*tview.InputField).SetText("folder/target.txt") })
			key(tcell.KeyTab, 0)
			wait(func() bool { _, ok := app.GetFocus().(*tview.Button); return ok })
			key(tcell.KeyEnter, 0)
			wait(func() bool { name, _ := pages.GetFrontPage(); return name == "file-transfer-confirm" })
			first := <-calls
			if !first.dry || first.confirm || first.dst != "folder/target.txt" || first.dstDS != "other" {
				t.Fatal("preview executed or lost destination", first)
			}
			select {
			case <-submitted:
				t.Fatal("preview submitted task")
			default:
			}
			key(tcell.KeyEscape, 0)
			wait(func() bool { name, _ := pages.GetFrontPage(); return name == "file-transfer" })
			// Returning from confirmation retains edited paths and preview button focus.
			key(tcell.KeyEnter, 0)
			wait(func() bool { name, _ := pages.GetFrontPage(); return name == "file-transfer-confirm" })
			second := <-calls
			if !second.dry || second.dst != first.dst {
				t.Fatal("back lost edited fields")
			}
			key(tcell.KeyRune, 'y')
			var actual call
			select {
			case actual = <-calls:
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation did not execute")
			}
			if actual.dry || !actual.confirm || actual.dst != first.dst || actual.dstDS != first.dstDS {
				t.Fatal("execution lacks explicit confirmation", actual)
			}
			select {
			case job := <-submitted:
				if job.ID != "task-ui-fixture" {
					t.Fatal(job)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("missing task callback")
			}
			wait(func() bool { return !pages.HasPage("file-transfer") })
		})
	}
}
