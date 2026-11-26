package tui

import (
	"context"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"path/filepath"
	"testing"
)

func TestTUICloneSourceGuardAndKeyboardEditing(t *testing.T) {
	app := tview.NewApplication()
	pages := tview.NewPages()
	vm := esxi.VMInfo{ID: "vm-1", Name: "BASE", PowerState: "poweredOn", ConnectionState: "connected", NumCPU: 4, MemoryMB: 8192}
	submitted := false
	done := func(tasks.Task) { submitted = true }
	openTUIClone(context.Background(), app, pages, target.Target{Name: "lab"}, vm, "en", done)
	if !pages.HasPage("clone-unavailable") || pages.HasPage("clone-editor") {
		t.Fatal("running source opened editor")
	}
	vm.PowerState = "poweredOff"
	openTUIClone(context.Background(), app, pages, target.Target{Name: "lab"}, vm, "en", done)
	if !pages.HasPage("clone-editor") {
		t.Fatal("powered-off source editor missing")
	}
	field, ok := app.GetFocus().(*tview.InputField)
	if !ok || field.GetText() != "BASE-clone" {
		t.Fatalf("clone editor focus: %T", app.GetFocus())
	}
	field.SetText("CHILD")
	field.InputHandler()(tcell.NewEventKey(tcell.KeyRune, '2', tcell.ModNone), func(tview.Primitive) {})
	if field.GetText() != "CHILD2" {
		t.Fatal("keyboard editing failed", field.GetText())
	}
	if submitted {
		t.Fatal("editing submitted clone")
	}
}
func TestTUITaskListAndDetailsWithoutESXi(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	m, e := tasks.Open(path, "tui")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	ctx := tasks.With(context.Background(), m)
	app := tview.NewApplication()
	settings := newTUISettings(ctx, app, path, nil, func() language { return "en" }, func(language) {}, func() target.Target { return target.Target{Name: "lab"} }, func() {})
	settings.pages = tview.NewPages()
	settings.module = 3
	settings.Render()
	if settings.table == nil {
		t.Fatal("task list not available offline")
	}
}
