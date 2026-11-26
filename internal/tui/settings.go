package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type tuiSettings struct {
	mcpProfile      bool
	taskDetails     *tview.TextView
	taskDetailID    string
	taskSearch      string
	taskStatusIndex int
	taskAll         bool
	taskSelected    string
	width           int
	status          string
	ctx             context.Context
	app             *tview.Application
	path            string
	pages           *tview.Pages
	store           *journal.Store
	lang            func() language
	setLanguage     func(language)
	profile         func() target.Target
	manageProfiles  func()
	profileAction   func(string, rune)
	profileSelected string
	root            *tview.Flex
	module, offset  int
	queryUntil      time.Time
	search, source  string
	focus           []tview.Primitive
	searchField     *tview.InputField
	table           *tview.Table
}

var tuiSettingsModules = []string{"Language", "MCP", "Profiles", "Tasks", "Logs", "Audits"}

func newTUISettings(ctx context.Context, app *tview.Application, path string, store *journal.Store, lang func() language, setLang func(language), profile func() target.Target, manage func()) *tuiSettings {
	return &tuiSettings{ctx: ctx, app: app, path: path, store: store, lang: lang, setLanguage: setLang, profile: profile, manageProfiles: manage, root: tview.NewFlex().SetDirection(tview.FlexRow)}
}
func (s *tuiSettings) Render() {
	l := s.lang()
	s.root.SetBackgroundColor(tuiSurface)
	s.root.Clear()
	s.focus = nil
	s.table = nil
	s.searchField = nil
	nav := tview.NewFlex()
	nav.SetBackgroundColor(tuiSurface)
	var rule strings.Builder
	for i, name := range tuiSettingsModules {
		i := i
		label := " " + l.Text(name) + " "
		button := tview.NewButton(label).SetSelectedFunc(func() {
			s.module = i
			s.status = ""
			s.offset = 0
			s.queryUntil = time.Time{}
			s.Render()
			s.app.SetFocus(s.root)
		})
		style := tcell.StyleDefault.Foreground(tuiMuted).Background(tuiSurface)
		if i == s.module {
			style = style.Foreground(tuiText)
		}
		button.SetStyle(style).SetActivatedStyle(style.Underline(true))
		color := "#43474b"
		if i == s.module {
			color = "#007acc"
		}
		rule.WriteString(tuiColor(color, strings.Repeat("─", tview.TaggedStringWidth(label))))
		nav.AddItem(button, tview.TaggedStringWidth(label), 0, false)
		s.focus = append(s.focus, button)
	}
	rule.WriteString(tuiColor("#43474b", strings.Repeat("─", max(0, s.width-tview.TaggedStringWidth(rule.String())))))
	s.root.AddItem(nav, 1, 0, false).AddItem(tuiTextView().SetText(rule.String()).SetWrap(false), 1, 0, false)
	s.root.AddItem(tview.NewBox().SetBackgroundColor(tuiSurface), 1, 0, false)
	cfg, err := target.Load(s.path)
	if err != nil {
		s.root.AddItem(tuiTextView().SetText(l.Text("Could not read local settings")), 0, 1, true)
		return
	}
	switch s.module {
	case 0:
		table := settingsTable(l, []string{"Language", "Status"})
		languages := []language{"en", "zh"}
		names := []string{"English", "简体中文"}
		inspector := settingsInspector()
		show := func(row, _ int) {
			if row < 1 || row > 2 {
				return
			}
			inspector.SetText(tuiInspector(names[row-1], [][2]string{{l.Text("Current language"), map[language]string{"en": "English", "zh": "简体中文"}[l]}}, s.inspectorWidth()) + "\n" + settingsHint("Enter", l.Text("Apply")))
		}
		for i, name := range names {
			status := "—"
			if languages[i] == l {
				status = l.Text("settings.current")
			}
			settingsRow(table, i+1, []string{name, status}, []int{14, 12})
		}
		table.SetSelectionChangedFunc(show).SetSelectedFunc(func(row, _ int) {
			if row < 1 || row > 2 {
				return
			}
			next := languages[row-1]
			err := journal.Run(s.ctx, journal.Event{Source: "tui", Operation: "settings.language"}, func() error {
				return target.Edit(s.ctx, s.path, func(c *target.Config) error { c.Language = string(next); return nil })
			})
			if err != nil {
				inspector.SetText(l.Text("Could not save settings"))
				return
			}
			s.setLanguage(next)
		})
		row := 1
		if l == "zh" {
			row = 2
		}
		table.Select(row, 0)
		show(row, 0)
		s.addSettingsSplit(table, inspector)
	case 1:
		s.renderMCP(cfg)
	case 2:
		table := settingsTable(l, []string{"Name", "Status", "Target"})
		names := sortedProfileNames(cfg)
		inspector := settingsInspector()
		show := func(row, _ int) {
			if row < 1 || row > len(names) {
				return
			}
			name := names[row-1]
			s.profileSelected = name
			p := cfg.Profiles[name]
			credential := l.Text("Not saved")
			if p.Password != "" {
				credential = l.Text("Saved")
			}
			mode := p.MCP.Mode
			if mode == "" {
				mode = l.Text("Inherit global mode")
			}
			effective, _ := target.EffectiveMCPMode(cfg, name)
			inspector.SetText(tuiInspector(name, [][2]string{{l.Text("Target"), "HTTPS  " + tuiEndpoint(target.Target{Profile: p})}, {l.Text("Password"), credential}, {l.Text("Profile mode"), mode}, {l.Text("Effective mode"), effective}}, s.inspectorWidth()) + "\n" + settingsHint("Enter", l.Text("Connect")) + "\n\n" + settingsHint("a", l.Text("Add")) + "  " + settingsHint("e", l.Text("Edit")) + "  " + settingsHint("Del", l.Text("Delete")))
		}
		selected := 1
		for i, name := range names {
			status := "—"
			if name == s.profile().Name {
				status = l.Text("Active")
			}
			settingsRow(table, i+1, []string{name, status, "HTTPS  " + tuiEndpoint(target.Target{Profile: cfg.Profiles[name]})}, []int{20, 10, 45})
			if name == s.profileSelected || s.profileSelected == "" && name == s.profile().Name {
				selected = i + 1
			}
		}
		table.SetSelectionChangedFunc(show).SetSelectedFunc(func(row, _ int) {
			if row > 0 && row <= len(names) {
				s.runProfileAction(names[row-1], '\n')
			}
		})
		if len(names) > 0 {
			table.Select(selected, 0)
			show(selected, 0)
		} else {
			table.SetCell(1, 0, tview.NewTableCell(l.Text("No saved profiles")).SetTextColor(tuiMuted).SetSelectable(false))
			inspector.SetText(settingsHint("a", l.Text("Add environment")))
		}
		s.addSettingsSplit(table, inspector)
	case 3:
		s.renderTasks()
	case 4, 5:
		s.renderJournal()
	}
}
func sortedProfileNames(c target.Config) []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
func (s *tuiSettings) renderJournal() {
	l := s.lang()
	search := tview.NewInputField().SetText(s.search).SetFieldBackgroundColor(tcell.NewHexColor(0x303238)).SetFieldTextColor(tuiText)
	search.SetChangedFunc(func(v string) { s.search = v })
	search.SetDoneFunc(func(k tcell.Key) {
		if k == tcell.KeyTab || k == tcell.KeyBacktab {
			s.cycleFocus(k)
			return
		}
		if k == tcell.KeyEscape {
			s.app.SetFocus(s.table)
			return
		}
		s.offset = 0
		s.queryUntil = time.Time{}
		s.Render()
		s.app.SetFocus(s.table)
	})
	search.SetBackgroundColor(tuiSurface)
	s.searchField = search
	sources := []string{l.Text("All sources"), "cli", "tui", "serve", "mcp"}
	index := 0
	for i, v := range sources {
		if v == s.source {
			index = i
		}
	}
	source := tview.NewDropDown().SetLabelColor(tuiMuted).SetLabel("").SetOptions(sources, func(v string, i int) {
		if i == 0 {
			s.source = ""
		} else {
			s.source = v
		}
	}).SetCurrentOption(index)
	styleSettingsFilterDropdown(source)
	source.SetBackgroundColor(tuiSurface)
	codes := []string{"", "INFO", "WARNING", "ERROR"}
	captionKey := "Level"
	allKey := "All levels"
	if s.module == 5 {
		codes = []string{"", "pending", "success", "failed", "denied", "unknown"}
		captionKey = "Outcome"
		allKey = "All outcomes"
	}
	labels := []string{l.Text(allKey)}
	selected := 0
	for i := 1; i < len(codes); i++ {
		labels = append(labels, l.Text(codes[i]))
		if codes[i] == s.status {
			selected = i
		}
	}
	statusFilter := tview.NewDropDown().SetLabelColor(tuiMuted).SetLabel("").SetOptions(labels, func(_ string, index int) { s.status = codes[index] }).SetCurrentOption(selected)
	statusFilter.SetBackgroundColor(tuiSurface)
	styleSettingsFilterDropdown(statusFilter)
	query := tview.NewButton(l.Text("Query")).SetStyle(tcell.StyleDefault.Foreground(tuiText).Background(tuiSelect)).SetSelectedFunc(func() {
		s.offset = 0
		s.queryUntil = time.Time{}
		s.Render()
		s.app.SetFocus(s.table)
	})
	s.addSettingsFilters(search, source, statusFilter, query, "Source", captionKey)
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	if s.queryUntil.IsZero() {
		s.queryUntil = time.Now().UTC()
	}
	q := journal.Query{Profile: s.profile().Name, Search: s.search, Source: s.source, Limit: 50, Offset: s.offset, Until: s.queryUntil}
	if s.module == 4 {
		q.Level = s.status
	} else {
		q.Outcome = s.status
	}
	var rows journal.Result
	var err error
	if s.module == 4 {
		rows, err = s.store.Logs(ctx, q)
	} else {
		rows, err = s.store.Audits(ctx, q)
	}
	table := settingsTable(l, nil)
	s.table = table
	inspector := settingsInspector()
	columns := []int{0, 1, 2, 3, 4}
	if s.width < 110 {
		columns = []int{0, 1, 3, 4}
	}
	labelsColumns := []string{"Time", "Source", "Profile", "Event / operation", "Outcome"}
	for col, index := range columns {
		label := labelsColumns[index]
		table.SetCell(0, col, tview.NewTableCell(l.Text(label)).SetTextColor(tuiAccent).SetBackgroundColor(tuiHeader).SetSelectable(false))
	}
	for row, e := range rows.Items {
		label := e.Event
		if e.Operation != "" {
			label = e.Operation
		}
		outcome := e.Outcome
		if outcome == "" {
			outcome = e.Level
		}
		values := []string{e.Time.Local().Format("2006-01-02 15:04:05"), e.Source, e.Profile, label, l.Text(outcome)}
		for col, index := range columns {
			value := values[index]
			color := tuiText
			if index == 4 {
				color = tuiAccent
				if outcome == "success" {
					color = tcell.NewHexColor(0x73c936)
				}
				if outcome == "failed" || outcome == "unknown" || outcome == "ERROR" {
					color = tcell.NewHexColor(0xf27878)
				}
			}
			cell := tview.NewTableCell(tview.Escape(value)).SetTextColor(color).SetMaxWidth([]int{19, 6, 12, 24, 10}[index])
			if index == 3 {
				cell.SetExpansion(1)
			}
			if row%2 == 1 {
				cell.SetBackgroundColor(tuiStripe)
			}
			table.SetCell(row+1, col, cell)
		}
	}
	show := func(row, col int) {
		if row < 1 || row > len(rows.Items) {
			return
		}
		e := rows.Items[row-1]
		_, _, width, _ := s.root.GetRect()
		if width < 60 {
			width = 100
		}
		outcome := e.Outcome
		if outcome == "" {
			outcome = e.Level
		}
		inspector.SetText(tuiInspector(l.Text("Record details"), [][2]string{{l.Text("Time"), e.Time.Local().Format("2006-01-02 15:04:05")}, {l.Text("Source"), e.Source}, {l.Text("Profile"), e.Profile}, {l.Text("Original profile"), e.OriginalProfile}, {l.Text("Actor"), e.Actor}, {l.Text("Operation"), e.Operation}, {l.Text("Event"), e.Event}, {l.Text("Outcome"), l.Text(outcome)}, {l.Text("Error code"), e.Code}, {l.Text("Resource"), e.Resource}, {l.Text("Duration"), fmt.Sprintf("%d ms", e.DurationMS)}, {l.Text("Record ID"), e.ID}, {l.Text("Request ID"), e.RequestID}, {"PID", fmt.Sprint(e.PID)}, {l.Text("Session"), e.Session}}, s.inspectorWidth()))
	}
	table.SetSelectionChangedFunc(show)
	if len(rows.Items) > 0 {
		table.Select(1, 0)
		show(1, 0)
	} else {
		table.SetCell(1, 0, tview.NewTableCell(l.Text("No matching records")).SetSelectable(false))
		inspector.SetText("")
	}
	table.SetSelectedFunc(func(row, col int) {
		show(row, col)
		if row > 0 && row <= len(rows.Items) {
			showTUIConfirmation(s.app, s.pages, "settings-record", l.Text("Record details"), inspector.GetText(false), l.Text("Close"), "", l, func(int) { s.app.SetFocus(table) })
		}
	})
	s.addSettingsSplit(table, inspector)
	status := s.store.Status()
	caption := fmt.Sprintf(l.Text("Runtime retention: %d days / %d MiB · Audit retention: %d days"), status.Runtime.Days, status.Runtime.MaxMiB, status.AuditDays)
	if err != nil {
		caption = l.Text("Could not query local records")
	}
	if status.Problem != "" || status.Dropped > 0 {
		caption += " · " + l.Text("Log storage degraded")
	}
	s.root.AddItem(tuiTextView().SetText(tview.Escape(" "+caption)).SetWrap(false), 1, 0, false)
	prev := tview.NewButton(l.Text("Previous page")).SetSelectedFunc(func() { s.offset = max(0, s.offset-50); s.Render(); s.app.SetFocus(s.table) })
	prev.SetDisabled(s.offset == 0)
	next := tview.NewButton(l.Text("Next page")).SetSelectedFunc(func() { s.offset += 50; s.Render(); s.app.SetFocus(s.table) })
	next.SetDisabled(!rows.HasMore)
	pager := tview.NewFlex().AddItem(prev, 14, 0, false).AddItem(tuiTextView().SetText(fmt.Sprintf("  %d", s.offset/50+1)), 6, 0, false).AddItem(next, 14, 0, false)
	for _, button := range []*tview.Button{prev, next} {
		button.SetStyle(tcell.StyleDefault.Foreground(tuiText).Background(tuiRail)).SetDisabledStyle(tcell.StyleDefault.Foreground(tuiMuted).Background(tuiSurface))
	}
	s.root.AddItem(pager, 1, 0, false)
	s.focus = append(s.focus, search, source, statusFilter, query)
	if !prev.IsDisabled() {
		s.focus = append(s.focus, prev)
	}
	if !next.IsDisabled() {
		s.focus = append(s.focus, next)
	}
}
func (s *tuiSettings) Handle(event *tcell.EventKey) bool {
	focus := s.app.GetFocus()
	if _, ok := focus.(*tview.InputField); ok {
		return false
	}
	if dropdown, ok := focus.(*tview.DropDown); ok && dropdown.IsOpen() {
		return false
	}
	if event.Rune() == '[' || event.Rune() == ']' {
		delta := 1
		if event.Rune() == '[' {
			delta = -1
		}
		s.module = (s.module + delta + len(tuiSettingsModules)) % len(tuiSettingsModules)
		s.status = ""
		s.offset = 0
		s.queryUntil = time.Time{}
		s.Render()
		s.app.SetFocus(s.root)
		return true
	}
	if s.module == 1 && event.Rune() == 'g' {
		s.mcpProfile = !s.mcpProfile
		s.Render()
		s.app.SetFocus(s.table)
		return true
	}
	if s.module == 2 && (focus == s.table || focus == s.root) {
		if event.Rune() == 'a' || event.Rune() == 'e' || event.Key() == tcell.KeyDelete {
			action := event.Rune()
			if event.Key() == tcell.KeyDelete {
				action = 'd'
			}
			s.runProfileAction(s.profileSelected, action)
			return true
		}
	}
	switch {
	case event.Rune() == 'r':
		s.queryUntil = time.Time{}
		if s.module != 3 {
			s.offset = 0
		}
		s.Render()
		s.app.SetFocus(s.root)
		return true
	case event.Rune() == 'f' && s.searchField != nil:
		s.app.SetFocus(s.searchField)
		return true
	case event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab:
		for i, p := range s.focus {
			if p == focus {
				delta := 1
				if event.Key() == tcell.KeyBacktab {
					delta = -1
				}
				s.app.SetFocus(s.focus[(i+delta+len(s.focus))%len(s.focus)])
				return true
			}
		}
		if len(s.focus) > 0 {
			s.app.SetFocus(s.focus[0])
			return true
		}
	}
	return false
}
