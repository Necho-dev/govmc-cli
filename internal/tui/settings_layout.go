package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Settings follow OpenWrt's quiet tabs, full-height list and padded inspector.
func settingsTable(l language, labels []string) *tview.Table {
	table := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tuiSelect).Bold(true))
	table.SetBackgroundColor(tcell.NewHexColor(0x262626))
	for col, label := range labels {
		table.SetCell(0, col, tview.NewTableCell(l.Text(label)).SetSelectable(false).SetTextColor(tuiAccent).SetBackgroundColor(tuiHeader).SetExpansion(0))
	}
	if len(labels) > 0 {
		table.GetCell(0, len(labels)-1).SetExpansion(1)
	}
	return table
}
func settingsRow(table *tview.Table, row int, values []string, widths []int) {
	for col, value := range values {
		cell := tview.NewTableCell(tview.Escape(value)).SetTextColor(tuiText).SetMaxWidth(widths[col])
		if col == len(values)-1 {
			cell.SetExpansion(1)
		}
		if row%2 == 0 {
			cell.SetBackgroundColor(tuiStripe)
		}
		table.SetCell(row, col, cell)
	}
}
func settingsInspector() *tview.TextView {
	v := tuiTextView().SetWrap(true).SetScrollable(true)
	v.SetBorderPadding(1, 0, 1, 1)
	return v
}
func settingsHint(key, label string) string {
	return " " + tuiColor("#79c6f3", key) + tuiColor("#9ba7b0", " · "+label)
}
func (s *tuiSettings) inspectorWidth() int {
	if s.width < 90 {
		return max(24, s.width-4)
	}
	return max(26, (s.width-3)*2/5-3)
}
func (s *tuiSettings) addSettingsSplit(table *tview.Table, inspector *tview.TextView) {
	s.table = table
	body := tview.NewFlex()
	divider := tview.NewBox().SetBackgroundColor(tuiSurface)
	divider.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		for row := 0; row < h; row++ {
			screen.SetContent(x, y+row, '│', nil, tcell.StyleDefault.Foreground(tcell.NewHexColor(0x38546a)).Background(tuiSurface))
		}
		return x, y, w, h
	})
	if s.width < 90 {
		body.SetDirection(tview.FlexRow).AddItem(table, 0, 1, true).AddItem(inspector, 0, 1, false)
	} else {
		body.AddItem(table, 0, 3, true).AddItem(divider, 1, 0, false).AddItem(inspector, 0, 2, false)
	}
	body.SetBackgroundColor(tuiSurface)
	s.root.AddItem(body, 0, 1, true)
	s.focus = append(s.focus, table, inspector)
}
func (s *tuiSettings) runProfileAction(name string, action rune) {
	if s.profileAction != nil {
		s.profileAction(name, action)
	} else {
		s.manageProfiles()
	}
}

func styleSettingsFilterDropdown(field *tview.DropDown) {
	neutral := tcell.StyleDefault.Foreground(tuiText).Background(tcell.NewHexColor(0x303238))
	field.SetFieldStyle(neutral).SetFocusedStyle(neutral.Foreground(tuiAccent).Underline(true)).
		SetListStyles(neutral, tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tuiSelect)).
		SetTextOptions(" ", " ", " ", " ▾", "").SetFieldWidth(22)
}
func (s *tuiSettings) addSettingsFilters(search *tview.InputField, source, status *tview.DropDown, query *tview.Button, firstLabel, caption string) {
	l := s.lang()
	cell := func(label string, control tview.Primitive) *tview.Flex {
		title := tuiTextView().SetText(tview.Escape(label)).SetTextColor(tuiMuted).SetWrap(false)
		box := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(title, 1, 0, false).AddItem(control, 1, 0, false)
		box.SetBackgroundColor(tuiSurface)
		return box
	}
	gap := func() *tview.Box { return tview.NewBox().SetBackgroundColor(tuiSurface) }
	query.SetStyle(tcell.StyleDefault.Foreground(tuiText).Background(tcell.NewHexColor(0x303238))).
		SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tuiSelect))
	sourceCell, statusCell, queryCell := cell(l.Text(firstLabel), source), cell(l.Text(caption), status), cell("", query)
	row := tview.NewFlex()
	row.SetBackgroundColor(tuiSurface)
	if s.width >= 110 {
		row.AddItem(cell(l.Text("Search"), search), 0, 1, false).AddItem(gap(), 2, 0, false).
			AddItem(sourceCell, 24, 0, false).AddItem(gap(), 2, 0, false).
			AddItem(statusCell, 24, 0, false).AddItem(gap(), 2, 0, false).AddItem(queryCell, 10, 0, false)
		s.root.AddItem(row, 2, 0, false)
	} else {
		s.root.AddItem(cell(l.Text("Search"), search), 2, 0, false).AddItem(gap(), 1, 0, false)
		row.AddItem(sourceCell, 0, 1, false).AddItem(gap(), 2, 0, false).
			AddItem(statusCell, 0, 1, false).AddItem(gap(), 2, 0, false).AddItem(queryCell, 10, 0, false)
		s.root.AddItem(row, 2, 0, false)
	}
	s.root.AddItem(gap(), 1, 0, false)
}
func (s *tuiSettings) cycleFocus(key tcell.Key) {
	for i, p := range s.focus {
		if p == s.app.GetFocus() {
			delta := 1
			if key == tcell.KeyBacktab {
				delta = -1
			}
			s.app.SetFocus(s.focus[(i+delta+len(s.focus))%len(s.focus)])
			return
		}
	}
}
