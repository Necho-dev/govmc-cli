package tui

import (
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/rivo/tview"
)

func openTUIPower(app *tview.Application, pages *tview.Pages, previous tview.Primitive, current target.Target, vm esxi.VMInfo, lang language, submit func(string)) {
	picker := tview.NewList().ShowSecondaryText(false).SetMainTextColor(tuiText).SetSelectedBackgroundColor(tuiSelect)
	picker.SetBackgroundColor(tuiHeader)
	closePicker := func() { pages.RemovePage("power"); app.SetFocus(previous) }
	for _, action := range []struct{ key, label string }{{"on", "Power on"}, {"off", "Hard power off"}, {"reset", "Hard reset"}, {"suspend", "Suspend"}} {
		if (action.key == "on" && vm.PowerState == "poweredOn") || (action.key != "on" && vm.PowerState != "poweredOn") {
			continue
		}
		picker.AddItem(lang.Text(action.label), "", 0, func() {
			message := tview.Escape(current.Name + " · " + tuiEndpoint(current) + "\n" + vm.Name + " (" + vm.ID + ")\n" + lang.Text(action.label))
			showTUIConfirmation(app, pages, "power-confirm", lang.Text("Power actions"), message, lang.Text("Cancel"), lang.Text("Confirm"), lang, func(index int) {
				pages.RemovePage("power-confirm")
				if index != 1 {
					app.SetFocus(picker)
					return
				}
				closePicker()
				saving := tview.NewModal().SetText(lang.Text("Applying power action…")).SetBackgroundColor(tuiHeader)
				pages.AddPage("saving", &tuiModal{saving}, true, true)
				app.SetFocus(saving)
				submit(action.key)
			})
		})
	}
	picker.AddItem(lang.Text("Cancel"), "", 0, closePicker).SetDoneFunc(closePicker)
	pages.AddPage("power", tuiProfileDialog(picker, lang.Text("Power actions"), current.Name+" · "+vm.Name+" ("+vm.ID+")", 72, 12), true, true)
	app.SetFocus(picker)
}
