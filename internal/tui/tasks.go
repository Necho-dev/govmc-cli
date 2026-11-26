package tui

import (
	"encoding/json"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"time"
)

func (s *tuiSettings) renderTasks() {
	l := s.lang()
	m := tasks.From(s.ctx)
	if m == nil {
		s.root.AddItem(tuiTextView().SetText(l.Text("Task service unavailable")), 0, 1, false)
		return
	}
	search := tview.NewInputField().SetText(s.taskSearch)
	search.SetFieldBackgroundColor(tcell.NewHexColor(0x303238)).SetFieldTextColor(tuiText).SetLabelColor(tuiMuted)
	search.SetBackgroundColor(tuiSurface)
	search.SetChangedFunc(func(value string) { s.taskSearch = value })
	s.searchField = search
	filter := tview.NewDropDown().SetOptions([]string{l.Text("All"), l.Text("queued"), l.Text("running"), l.Text("succeeded"), l.Text("failed"), l.Text("cancelled"), l.Text("interrupted"), l.Text("cancelling")}, nil)
	filter.SetCurrentOption(s.taskStatusIndex)
	styleSettingsFilterDropdown(filter)
	filter.SetBackgroundColor(tuiSurface)
	profile := s.profile().Name
	if s.taskAll {
		profile = ""
	}
	states := []string{"", "queued", "running", "succeeded", "failed", "cancelled", "interrupted", "cancelling"}
	page, e := m.List(tasks.Query{Profile: profile, Status: states[s.taskStatusIndex], Search: s.taskSearch, Limit: 50, Offset: s.offset})
	if e != nil {
		s.root.AddItem(tuiTextView().SetText(tview.Escape(e.Error())), 0, 1, false)
		return
	}
	scope := 0
	if s.taskAll {
		scope = 1
	}
	all := tview.NewDropDown().SetOptions([]string{l.Text("Current environment"), l.Text("All environments")}, nil).SetCurrentOption(scope)
	styleSettingsFilterDropdown(all)
	all.SetBackgroundColor(tuiSurface)
	query := tview.NewButton(l.Text("Query")).SetSelectedFunc(func() {
		s.taskSearch = search.GetText()
		s.taskStatusIndex, _ = filter.GetCurrentOption()
		i, _ := all.GetCurrentOption()
		s.taskAll = i == 1
		s.offset = 0
		s.Render()
		s.app.SetFocus(s.table)
	})
	search.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyTab || key == tcell.KeyBacktab {
			s.cycleFocus(key)
			return
		}
		if key == tcell.KeyEscape {
			s.app.SetFocus(s.table)
			return
		}
		s.taskStatusIndex, _ = filter.GetCurrentOption()
		i, _ := all.GetCurrentOption()
		s.taskAll = i == 1
		s.offset = 0
		s.Render()
		s.app.SetFocus(s.table)
	})
	query.SetStyle(tcell.StyleDefault.Foreground(tuiText).Background(tuiSelect))
	s.addSettingsFilters(search, filter, all, query, "Status", "Scope")
	table := settingsTable(l, []string{"Task", "Type", "Status", "Duration"})
	inspector := settingsInspector()
	selected := 1
	show := func(row, _ int) {
		if row < 1 || row > len(page.Items) {
			return
		}
		task := page.Items[row-1]
		s.taskSelected = task.ID
		inspector.SetText(tuiInspector(task.Resource, [][2]string{{l.Text("Task ID"), task.ID}, {l.Text("Profile"), task.Profile}, {l.Text("Type"), l.Text(task.Kind)}, {l.Text("Status"), l.Text(task.Status)}, {l.Text("Current step"), l.Text(task.CurrentStep)}, {l.Text("Duration"), taskDuration(task)}, {l.Text("Created"), task.CreatedAt.Local().Format("2006-01-02 15:04:05")}}, s.inspectorWidth()) + "\n" + settingsHint("Enter", l.Text("Details")))
	}
	for i, task := range page.Items {
		settingsRow(table, i+1, []string{task.Resource, l.Text(task.Kind), l.Text(task.Status), taskDuration(task)}, []int{22, 18, 13, 10})
		color := tuiAccent
		if task.Status == "succeeded" {
			color = tcell.NewHexColor(0x73c936)
		} else if task.Status == "failed" || task.Status == "interrupted" {
			color = tcell.NewHexColor(0xf27878)
		}
		table.GetCell(i+1, 2).SetTextColor(color)
		if task.ID == s.taskSelected {
			selected = i + 1
		}
	}
	table.SetSelectionChangedFunc(show).SetSelectedFunc(func(row, _ int) {
		if row > 0 && row <= len(page.Items) {
			s.showTask(page.Items[row-1].ID)
		}
	})
	if len(page.Items) > 0 {
		table.Select(selected, 0)
		show(selected, 0)
	} else {
		table.SetCell(1, 0, tview.NewTableCell(l.Text("No tasks")).SetSelectable(false).SetTextColor(tuiMuted))
		inspector.SetText(tuiColor("#79c6f3", l.Text("Tasks")) + "\n\n " + l.Text("No tasks"))
	}
	s.addSettingsSplit(table, inspector)
	prev := tview.NewButton(l.Text("Previous")).SetDisabled(s.offset == 0).SetSelectedFunc(func() { s.offset = max(0, s.offset-50); s.Render(); s.app.SetFocus(s.table) })
	next := tview.NewButton(l.Text("Next")).SetDisabled(!page.HasMore).SetSelectedFunc(func() { s.offset += 50; s.Render(); s.app.SetFocus(s.table) })
	for _, button := range []*tview.Button{prev, next} {
		button.SetStyle(tcell.StyleDefault.Foreground(tuiText).Background(tuiRail)).SetDisabledStyle(tcell.StyleDefault.Foreground(tuiMuted).Background(tuiSurface))
	}
	pager := tview.NewFlex().AddItem(prev, 12, 0, false).AddItem(tuiTextView().SetText(fmt.Sprintf(" %d", s.offset/50+1)), 5, 0, false).AddItem(next, 12, 0, false)
	pager.SetBackgroundColor(tuiSurface)
	s.root.AddItem(pager, 1, 0, false)
	s.focus = append(s.focus, search, filter, all, query)
	if !prev.IsDisabled() {
		s.focus = append(s.focus, prev)
	}
	if !next.IsDisabled() {
		s.focus = append(s.focus, next)
	}

}
func (s *tuiSettings) showTask(id string) {
	m := tasks.From(s.ctx)
	t, e := m.Get(id)
	if e != nil {
		return
	}
	l := s.lang()
	raw := taskSummary(t, l)

	view := tuiTextView().SetScrollable(true).SetText(tview.Escape(raw))
	view.SetBackgroundColor(tuiHeader)
	s.taskDetails = view
	s.taskDetailID = id
	close := func() {
		s.pages.RemovePage("task-detail")
		s.taskDetails = nil
		s.taskDetailID = ""
		s.app.SetFocus(s.table)
	}
	cancel := func() {
		latest, e := m.Get(id)
		if e != nil || latest.Status != "queued" && latest.Status != "running" && latest.Status != "cancelling" {
			return
		}
		showTUIConfirmation(s.app, s.pages, "task-cancel", l.Text("Cancel task"), tview.Escape(latest.Profile+" / "+latest.Resource)+"\n"+l.Text("Cancellation does not roll back completed steps"), l.Text("Back"), l.Text("Confirm"), l, func(answer int) {
			if answer == 1 {
				if e := journal.Run(s.ctx, journal.Event{Source: "tui", Profile: latest.Profile, Operation: "task.cancel", Resource: id}, func() error { return m.Cancel(id) }); e != nil {
					s.status = e.Error()
				}
			}
			s.app.SetFocus(view)
		})
	}
	actions := tview.NewFlex().AddItem(tview.NewButton(l.Text("Close")).SetSelectedFunc(close), 16, 0, false).AddItem(tview.NewButton(l.Text("Cancel task")).SetSelectedFunc(cancel), 20, 0, false)
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, true).AddItem(tuiTextView().SetText("Esc "+l.Text("Close")+" · c "+l.Text("Cancel task")), 1, 0, false).AddItem(actions, 1, 0, false)
	view.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		if e.Rune() == 'c' {
			cancel()
			return nil
		}
		if e.Key() == tcell.KeyTab {
			s.app.SetFocus(actions)
			return nil
		}
		return e
	})
	content.SetBackgroundColor(tuiHeader)
	s.pages.AddPage("task-detail", tuiEnvironmentDialog(content, l.Text("Task details"), 86, 30), true, true)
	s.app.SetFocus(view)
}

func taskSummary(t tasks.Task, l language) string {
	text := fmt.Sprintf("%s  %s\n%s  %s\n%s  %s\n%s  %s\n%s  %s\n\n", l.Text("Task ID"), t.ID, l.Text("Profile"), t.Profile, l.Text("Endpoint"), t.Endpoint, l.Text("Status"), l.Text(t.Status), l.Text("Created"), t.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	if t.OriginalProfile != "" {
		text += l.Text("Original profile") + "  " + t.OriginalProfile + "\n"
	}
	for _, step := range t.Steps {
		text += fmt.Sprintf("%-16s %-12s %s\n", l.Text(step.Name), l.Text(step.Status), taskStepTime(step.StartedAt))
	}
	var params map[string]any
	if json.Unmarshal(t.Parameters, &params) == nil {
		text += "\n" + l.Text("Parameters") + "\n"
		for _, key := range []string{"src", "dst", "datastore", "dstDatastore", "numCPU", "memoryMB", "portgroup", "powerOn", "cpuHotAddEnabled", "memoryHotAddEnabled", "lockMemory", "memoryReservationMB", "expandDisks"} {
			if value, ok := params[key]; ok {
				text += fmt.Sprintf("%-20s %v\n", l.Text(key), value)
			}
		}
	}
	if t.Result != nil {
		raw, _ := json.MarshalIndent(t.Result, "", "  ")
		text += "\n" + l.Text("Result") + "\n" + string(raw)
	}
	if t.Error != "" {
		text += "\n" + l.Text(t.Error) + " · " + t.ErrorMessage
	}
	if t.RemoteTask != "" {
		text += "\n" + l.Text("Remote task") + "  " + t.RemoteTask
	}
	return text
}

func taskDuration(t tasks.Task) string {
	start := t.CreatedAt
	if t.StartedAt != nil {
		start = *t.StartedAt
	}
	end := time.Now()
	if t.EndedAt != nil {
		end = *t.EndedAt
	}
	return end.Sub(start).Round(time.Second).String()
}

func taskStepTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Local().Format("15:04:05")
}
