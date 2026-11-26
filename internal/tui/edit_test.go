package tui

import (
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTUIEditorResourceChangesRequireConfirmation(t *testing.T) {
	zero, unlimited, cores, no := int64(0), int64(-1), int32(2), false
	vm := esxi.VMInfo{ID: "vm-1", Name: "test-vm", PowerState: "poweredOff", NumCPU: 4, MemoryMB: 4096, Config: &esxi.VMConfigState{
		Editable: true, DeviceEditable: true, ChangeVersion: "v1", CoresPerSocket: &cores,
		CPUReservationMHz: &zero, CPULimitMHz: &unlimited, MemoryReservationMB: &zero, MemoryLimitMB: &unlimited,
		CPUHotAdd: &no, MemoryHotAdd: &no, NestedHV: &no, IOMMU: &no, VPMC: &no,
	}}
	app := tview.NewApplication()
	pages := tview.NewPages()
	table := tview.NewTable()
	pages.AddPage("main", table, true, true)
	app.SetRoot(pages, true)
	var submitted *esxi.VMChange
	openTUIEditor(app, pages, table, "lab", vm, nil, "en", func(patch esxi.VMChange) { submitted = &patch })
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(120, 48)
	defer screen.Fini()
	pages.SetRect(0, 0, 120, 48)
	key := func(key tcell.Key) {
		pages.Draw(screen)
		pages.InputHandler()(tcell.NewEventKey(key, 0, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	}
	findInput := func(label, value string) {
		t.Helper()
		for range 24 {
			if input, ok := app.GetFocus().(*tview.InputField); ok && input.GetLabel() == label {
				input.SetText(value)
				return
			}
			key(tcell.KeyTab)
		}
		t.Fatalf("input %s was not reachable", label)
	}
	key(tcell.KeyF6)
	nav, ok := app.GetFocus().(*tview.List)
	if !ok {
		t.Fatalf("modules were not focusable")
	}
	nav.SetCurrentItem(1)
	key(tcell.KeyEnter)
	findInput("CPU reservation (MHz)", "500")
	findInput("CPU limit (MHz, -1 unlimited)", "2000")
	key(tcell.KeyF6)
	nav.SetCurrentItem(2)
	key(tcell.KeyEnter)
	findInput("Memory reservation (MiB)", "1024")
	findInput("Memory limit (MiB, -1 unlimited)", "8192")
	key(tcell.KeyCtrlS)
	if submitted != nil {
		t.Fatal("preview submitted the patch")
	}
	name, _ := pages.GetFrontPage()
	if name != "preview" {
		t.Fatalf("expected preview, got %s", name)
	}
	key(tcell.KeyTab)
	key(tcell.KeyEnter)
	if submitted == nil {
		t.Fatal("confirmation did not submit")
	}
	if submitted.CPUReservationMHz == nil || *submitted.CPUReservationMHz != 500 ||
		submitted.CPULimitMHz == nil || *submitted.CPULimitMHz != 2000 ||
		submitted.MemoryReservationMB == nil || *submitted.MemoryReservationMB != 1024 ||
		submitted.MemoryLimitMB == nil || *submitted.MemoryLimitMB != 8192 {
		t.Fatalf("cross-module changes were lost: %#v", submitted)
	}
	if submitted.ChangeVersion != "v1" || !submitted.RequirePoweredOff {
		t.Fatal("configuration preconditions were lost")
	}
	if submitted.NumCPU != nil || submitted.MemoryMB != nil || submitted.Name != nil {
		t.Fatal("unchanged fields were included")
	}
}

func TestTUIEditorMouseAndKeyboardStayInsideDialog(t *testing.T) {
	yes := true
	vm := esxi.VMInfo{ID: "vm-1", Name: "running-vm", PowerState: "poweredOn", Config: &esxi.VMConfigState{Editable: true, DeviceEditable: true},
		NICs: []esxi.NIC{{Key: 4000, Label: "NIC 1", Portgroup: "VM Network", Connected: &yes, StartConnected: &yes}}}
	app := tview.NewApplication()
	pages := tview.NewPages()
	behind := tview.NewBox()
	behindClicks := 0
	behind.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		behindClicks++
		return action, event
	})
	pages.AddPage("main", behind, true, true)
	app.SetRoot(pages, true)
	openTUIEditor(app, pages, tview.NewTable(), "lab", vm, []esxi.Portgroup{{Name: "VM Network"}, {Name: "Other network"}}, "en", func(esxi.VMChange) {})
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(120, 48)
	defer screen.Fini()
	pages.SetRect(0, 0, 120, 48)
	draw := func() { pages.Draw(screen) }
	key := func(k tcell.Key, r rune) {
		draw()
		pages.InputHandler()(tcell.NewEventKey(k, r, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	}
	draw()
	key(tcell.KeyF6, 0)
	nav, ok := app.GetFocus().(*tview.List)
	if !ok {
		t.Fatal("modules are not focusable")
	}
	index := -1
	for i := 0; i < nav.GetItemCount(); i++ {
		name, _ := nav.GetItemText(i)
		if name == "NIC 1" {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("NIC module is missing")
	}
	x, y, _, _ := nav.GetInnerRect()
	click := func(action tview.MouseAction, x, y int) {
		draw()
		pages.MouseHandler()(action, tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	}
	click(tview.MouseLeftClick, x+1, y+index)
	dropdown, ok := app.GetFocus().(*tview.DropDown)
	if !ok {
		t.Fatalf("module click focused %T instead of its first editable field", app.GetFocus())
	}
	key(tcell.KeyEnter, 0)
	if !dropdown.IsOpen() {
		t.Fatal("Enter did not open the portgroup menu")
	}
	key(tcell.KeyDown, 0)
	key(tcell.KeyEscape, 0)
	if page, _ := pages.GetFrontPage(); page != "editor" || dropdown.IsOpen() {
		t.Fatal("Escape should close only the open portgroup menu")
	}
	key(tcell.KeyTab, 0)
	check, ok := app.GetFocus().(*tview.Checkbox)
	if !ok {
		t.Fatalf("Tab did not reach a checkbox, got %T", app.GetFocus())
	}
	checked := check.IsChecked()
	key(tcell.KeyRune, ' ')
	if check.IsChecked() == checked {
		t.Fatal("Space did not toggle the focused checkbox")
	}
	cx, cy, _, _ := check.GetInnerRect()
	click(tview.MouseLeftDown, cx+1, cy)
	click(tview.MouseLeftClick, cx+1, cy)
	if app.GetFocus() != check || check.IsChecked() != checked {
		t.Fatal("checkbox click lost focus or failed to toggle")
	}
	key(tcell.KeyRune, ' ')
	key(tcell.KeyBacktab, 0)
	if app.GetFocus() != dropdown {
		t.Fatal("Shift+Tab did not return to the dropdown")
	}
	click(tview.MouseLeftDown, 0, 0)
	click(tview.MouseLeftClick, 0, 0)
	if behindClicks != 0 {
		t.Fatalf("%d mouse events escaped to the background", behindClicks)
	}
	if app.GetFocus() != dropdown {
		t.Fatal("an outside click stole the editing focus")
	}
	screen.SetSize(100, 40)
	pages.SetRect(0, 0, 100, 40)
	draw()
	if app.GetFocus() != dropdown {
		t.Fatal("resize changed the active field")
	}
}

func TestTUIEditorTypingAndFocusRing(t *testing.T) {
	no := false
	cores := int32(2)
	vm := esxi.VMInfo{ID: "vm-1", Name: "old", NumCPU: 4, MemoryMB: 4096, PowerState: "poweredOff",
		Config: &esxi.VMConfigState{Editable: true, CoresPerSocket: &cores, CPUHotAdd: &no, NestedHV: &no, IOMMU: &no, VPMC: &no}}
	app := tview.NewApplication()
	pages := tview.NewPages()
	list := tview.NewTable()
	pages.AddPage("main", list, true, true)
	app.SetRoot(pages, true)
	var submitted *esxi.VMChange
	openTUIEditor(app, pages, list, "lab", vm, nil, "en", func(p esxi.VMChange) { submitted = &p })
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(120, 48)
	defer screen.Fini()
	pages.SetRect(0, 0, 120, 48)
	key := func(k tcell.Key, r rune) {
		pages.Draw(screen)
		pages.InputHandler()(tcell.NewEventKey(k, r, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	}
	input, ok := app.GetFocus().(*tview.InputField)
	if !ok || input.GetLabel() != "Name" {
		t.Fatal("initial focus should select the name field")
	}
	key(tcell.KeyCtrlA, 0)
	key(tcell.KeyCtrlK, 0)
	for _, r := range "renamed" {
		key(tcell.KeyRune, r)
	}
	if input.GetText() != "renamed" {
		t.Fatalf("typing failed: %q", input.GetText())
	}
	key(tcell.KeyTab, 0)
	if button, ok := app.GetFocus().(*tview.Button); !ok || button.GetLabel() != "Preview changes" {
		t.Fatal("Tab did not move from the last field to actions")
	}
	key(tcell.KeyBacktab, 0)
	if app.GetFocus() != input {
		t.Fatal("Shift+Tab did not return from actions to the last field")
	}
	key(tcell.KeyF6, 0)
	key(tcell.KeyDown, 0)
	key(tcell.KeyEnter, 0)
	cpu, ok := app.GetFocus().(*tview.InputField)
	if !ok || cpu.GetLabel() != "vCPU" {
		t.Fatal("CPU module did not focus its first editable field")
	}
	key(tcell.KeyCtrlA, 0)
	key(tcell.KeyCtrlK, 0)
	key(tcell.KeyRune, '6')
	key(tcell.KeyDown, 0)
	if next, ok := app.GetFocus().(*tview.InputField); !ok || next.GetLabel() != "Cores per socket" {
		t.Fatal("Down did not navigate to the next field")
	}
	key(tcell.KeyDown, 0)
	check, ok := app.GetFocus().(*tview.Checkbox)
	if !ok {
		t.Fatal("Down did not reach CPU hot add")
	}
	key(tcell.KeyRune, ' ')
	if !check.IsChecked() {
		t.Fatal("Space did not edit CPU hot add")
	}
	key(tcell.KeyCtrlS, 0)
	if submitted != nil {
		t.Fatal("preview submitted changes")
	}
	key(tcell.KeyEscape, 0)
	if name, _ := pages.GetFrontPage(); name != "editor" {
		t.Fatal("Escape did not return from preview")
	}
	if input.GetText() != "renamed" || cpu.GetText() != "6" || !check.IsChecked() {
		t.Fatal("preview cancellation lost the edits")
	}
	key(tcell.KeyCtrlS, 0)
	key(tcell.KeyTab, 0)
	key(tcell.KeyEnter, 0)
	if submitted == nil || submitted.Name == nil || *submitted.Name != "renamed" || submitted.NumCPU == nil || *submitted.NumCPU != 6 || submitted.CPUHotAdd == nil || !*submitted.CPUHotAdd {
		t.Fatalf("keyboard changes were not submitted: %#v", submitted)
	}
}
