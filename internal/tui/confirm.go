package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// All confirmation paths share the OpenWrt-style surface and keyboard contract.
// Callers escape user-controlled message text before passing it here.
func showTUIConfirmation(app *tview.Application, pages *tview.Pages, page, title, message, cancelLabel, confirmLabel string, lang language, done func(int)) {
	surface := tcell.NewHexColor(0x12283c)
	view := tuiTextView().SetScrollable(true).SetText(message)
	view.SetBackgroundColor(surface)
	hintText := tuiColor("#79c6f3", "[Esc / N]") + " " + lang.Text("Cancel")
	if confirmLabel != "" {
		hintText = tuiColor("#79c6f3", "[Y]") + " " + lang.Text("Confirm") + "  " + hintText
	}
	hint := tuiTextView().SetText(hintText).SetWrap(false)
	hint.SetBackgroundColor(surface)
	cancel := tview.NewButton(cancelLabel).SetStyle(tcell.StyleDefault.Foreground(tuiText).Background(tcell.NewHexColor(0x262626)))
	confirm := tview.NewButton(confirmLabel).SetStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.NewHexColor(0x48bf75)))
	cancel.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.NewHexColor(0x343434)).Underline(true))
	confirm.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.NewHexColor(0x65d98e)).Bold(true))
	closed := false
	finish := func(index int) {
		if closed {
			return
		}
		closed = true
		pages.RemovePage(page)
		done(index)
	}
	cancel.SetSelectedFunc(func() { finish(0) })
	confirm.SetSelectedFunc(func() { finish(1) })
	input := func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyEscape || event.Rune() == 'n' || event.Rune() == 'N':
			finish(0)
			return nil
		case (event.Rune() == 'y' || event.Rune() == 'Y') && confirmLabel != "":
			finish(1)
			return nil
		case event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab || event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight:
			if confirmLabel != "" && cancel.HasFocus() {
				app.SetFocus(confirm)
			} else {
				app.SetFocus(cancel)
			}
			return nil
		case event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown || event.Key() == tcell.KeyPgUp || event.Key() == tcell.KeyPgDn:
			view.InputHandler()(event, nil)
			return nil
		}
		return event
	}
	cancel.SetInputCapture(input)
	confirm.SetInputCapture(input)
	buttons := tview.NewFlex().AddItem(tview.NewBox().SetBackgroundColor(surface), 0, 1, false).
		AddItem(cancel, max(16, tview.TaggedStringWidth(cancelLabel)+4), 0, true)
	if confirmLabel != "" {
		buttons.AddItem(tview.NewBox().SetBackgroundColor(surface), 1, 0, false).
			AddItem(confirm, max(16, tview.TaggedStringWidth(confirmLabel)+4), 0, false)
	}
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, false).
		AddItem(tview.NewBox().SetBackgroundColor(surface), 1, 0, false).AddItem(hint, 1, 0, false).
		AddItem(tview.NewBox().SetBackgroundColor(surface), 1, 0, false).AddItem(buttons, 3, 0, true)
	rows := 0
	for _, line := range strings.Split(message, "\n") {
		rows += max(1, (tview.TaggedStringWidth(line)+69)/70)
	}
	height := min(28, max(14, rows+12))
	pages.AddPage(page, tuiEnvironmentDialog(content, title, 76, height), true, true)
	app.SetFocus(cancel)
}
