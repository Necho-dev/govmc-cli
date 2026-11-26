package tui

import (
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/rivo/tview"
)

func (s *tuiSettings) renderMCP(cfg target.Config) {
	l := s.lang()
	name := s.profile().Name
	_, exists := cfg.Profiles[name]
	if !exists {
		s.mcpProfile = false
	}
	profileScope := s.mcpProfile
	scope := l.Text("Global mode")
	saved := cfg.MCP.Mode
	modes := []string{target.MCPReadonly, target.MCPReadwrite}
	labels := append([]string(nil), modes...)
	if profileScope {
		scope = l.Text("Profile") + " · " + name
		saved = cfg.Profiles[name].MCP.Mode
		modes = []string{"", target.MCPReadonly, target.MCPReadwrite}
		labels = []string{l.Text("Inherit global mode"), target.MCPReadonly, target.MCPReadwrite}
	}
	table := settingsTable(l, []string{"Mode", "Status"})
	inspector := settingsInspector()
	effective, err := target.EffectiveMCPMode(cfg, name)
	if err != nil {
		effective = "—"
	}
	profileMode := cfg.Profiles[name].MCP.Mode
	if profileMode == "" {
		profileMode = l.Text("Inherit global mode")
	}
	show := func(row, _ int) {
		if row < 1 || row > len(modes) {
			return
		}
		text := tuiInspector(l.Text("MCP permissions"), [][2]string{{l.Text("Scope"), scope}, {l.Text("Selected"), labels[row-1]}, {l.Text("Global mode"), cfg.MCP.Mode}, {l.Text("Profile"), name}, {l.Text("Profile mode"), profileMode}, {l.Text("Effective mode"), effective}}, s.inspectorWidth()) + "\n" + settingsHint("Enter", l.Text("settings.save"))
		if exists {
			text += "\n" + settingsHint("g", l.Text("Global / profile"))
		}
		text += "\n\n " + l.Text("Global mode is the permission ceiling. Readonly profiles remain readonly.")
		inspector.SetText(text)
	}
	selected := 1
	for i, mode := range modes {
		status := "—"
		if mode == saved {
			status = l.Text("Saved")
			selected = i + 1
		}
		settingsRow(table, i+1, []string{labels[i], status}, []int{24, 12})
	}
	table.SetSelectionChangedFunc(show).SetSelectedFunc(func(row, _ int) {
		if row < 1 || row > len(modes) {
			return
		}
		next := modes[row-1]
		if next == saved {
			return
		}
		oldLabel := saved
		if oldLabel == "" {
			oldLabel = l.Text("Inherit global mode")
		}
		message := scope + "\n" + oldLabel + " → " + labels[row-1] + "\n\n" + l.Text("Global mode is the permission ceiling. Readonly profiles remain readonly.")
		showTUIConfirmation(s.app, s.pages, "settings-mcp", l.Text("MCP permissions"), tview.Escape(message), l.Text("Cancel"), l.Text("settings.save"), l, func(answer int) {
			if answer != 1 {
				s.app.SetFocus(table)
				return
			}
			err := journal.Run(s.ctx, journal.Event{Source: "tui", Profile: name, Operation: "settings.mcp"}, func() error {
				return target.Edit(s.ctx, s.path, func(c *target.Config) error {
					if next != "" {
						if err := target.ValidateMCPMode(next); err != nil {
							return err
						}
					}
					if profileScope {
						p, ok := c.Profiles[name]
						if !ok {
							return fmt.Errorf("profile no longer exists")
						}
						if p.MCP.Mode != saved {
							return fmt.Errorf("settings changed; refresh before saving")
						}
						p.MCP.Mode = next
						c.Profiles[name] = p
					} else {
						if c.MCP.Mode != saved {
							return fmt.Errorf("settings changed; refresh before saving")
						}
						c.MCP.Mode = next
					}
					return nil
				})
			})
			if err != nil {
				inspector.SetText(l.Text("Could not save settings"))
				s.app.SetFocus(table)
				return
			}
			s.Render()
			s.app.SetFocus(s.table)
		})
	})
	table.Select(selected, 0)
	show(selected, 0)
	s.addSettingsSplit(table, inspector)
}
