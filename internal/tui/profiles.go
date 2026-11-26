package tui

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func openTUIProfiles(ctx context.Context, app *tview.Application, pages *tview.Pages, configPath string, current target.Target, legacy *target.Target, sessions map[string]target.Target, lang language, switchTarget func(target.Target), onDelete ...func(string)) {
	openTUIProfilesAt(ctx, app, pages, configPath, current, legacy, sessions, lang, "", 0, nil, switchTarget, onDelete...)
}

func openTUIProfilesAt(ctx context.Context, app *tview.Application, pages *tview.Pages, configPath string, current target.Target, legacy *target.Target, sessions map[string]target.Target, lang language, preferred string, action rune, onClosed func(), switchTarget func(target.Target), onDelete ...func(string)) {
	previous := app.GetFocus()
	editConfig := func(ctx context.Context, operation, name string, change func(*target.Config) error) error {
		return journal.Run(ctx, journal.Event{Source: "tui", Operation: operation, Profile: name}, func() error { return target.Edit(ctx, configPath, change) })
	}
	picker := tview.NewList().ShowSecondaryText(false).
		SetMainTextStyle(tcell.StyleDefault.Foreground(tuiText).Background(tcell.NewHexColor(0x19344d))).
		SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.NewHexColor(0x007acc))).SetHighlightFullLine(true)
	picker.SetBackgroundColor(tcell.NewHexColor(0x19344d))
	picker.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		tuiEnvironmentBorder(screen, x, y, w, h, tcell.NewHexColor(0x007acc), tcell.NewHexColor(0x19344d))
		return x + 2, y + 1, max(0, w-4), max(0, h-2)
	})
	var refreshPicker func(string)
	var editProfile func(string)
	profileAt := map[int]string{}
	closePage := func(name string) {
		if pages.HasPage(name) {
			pages.RemovePage(name)
		}
	}
	closePicker := func() {
		closePage("profile-confirm")
		closePage("profile-add")
		closePage("profile-password")
		closePage("profile-edit")
		closePage("profile-delete")
		closePage("profiles")
		app.SetFocus(previous)
		if onClosed != nil {
			onClosed()
		}
	}
	choose := func(next target.Target) {
		connect := func() { sessions[next.Name] = next; closePicker(); switchTarget(next) }
		if current.Name == "" || next == current {
			connect()
			return
		}
		openTUIProfileConfirmation(app, pages, picker, current, next, lang, connect)
	}
	report := func(message string) {
		modal := tview.NewModal().SetText(tview.Escape(message)).AddButtons([]string{lang.Text("Close")})
		modal.SetDoneFunc(func(int, string) {
			closePage("profile-error")
			if pages.HasPage("profiles") {
				app.SetFocus(picker)
			} else {
				app.SetFocus(previous)
			}
		})
		pages.AddPage("profile-error", &tuiModal{modal}, true, true)
		app.SetFocus(modal)
	}
	if _, err := target.Load(configPath); err != nil {
		report(err.Error())
		return
	}
	formStyle := func() *tview.Form {
		f := tview.NewForm().SetItemPadding(1).SetLabelColor(tuiMuted).SetFieldTextColor(tuiText).
			SetFieldBackgroundColor(tcell.NewHexColor(0x19344d)).SetButtonBackgroundColor(tuiSelect).SetButtonTextColor(tuiText)
		f.SetBackgroundColor(tcell.NewHexColor(0x12283c))
		return f
	}
	credentialPrompt := func(next target.Target) {
		form := formStyle()
		password := tview.NewInputField().SetLabel(lang.Text("Password")).SetMaskCharacter('*').SetFieldWidth(0)
		password.SetBackgroundColor(tcell.NewHexColor(0x12283c))
		form.AddFormItem(password)
		errorView := tuiTextView()
		errorView.SetBackgroundColor(tcell.NewHexColor(0x12283c))
		back := func() { password.SetText(""); closePage("profile-password"); app.SetFocus(picker) }
		save := func(connect bool) {
			if password.GetText() == "" {
				return
			}
			saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			err := editConfig(saveCtx, "profile.password", next.Name, func(config *target.Config) error {
				p, ok := config.Profiles[next.Name]
				if !ok {
					return fmt.Errorf("unknown profile %q", next.Name)
				}
				if err := target.SetPassword(configPath, next.Name, &p, password.GetText()); err != nil {
					return err
				}
				config.Profiles[next.Name] = p
				return nil
			})
			if err != nil {
				errorView.SetText(tuiColor("#f07878", err.Error()))
				return
			}
			saved, err := target.Named(configPath, next.Name)
			if err != nil {
				errorView.SetText(tuiColor("#f07878", err.Error()))
				return
			}
			back()
			if connect {
				choose(saved)
			} else {
				refreshPicker(next.Name)
			}
		}
		form.AddButton(lang.Text("Save"), func() { save(false) }).
			AddButton(lang.Text("Save and Connect"), func() { save(true) }).
			AddButton(lang.Text("Cancel"), func() {
				if action != 0 {
					closePicker()
				} else {
					back()
				}
			}).SetCancelFunc(func() {
			if action != 0 {
				closePicker()
			} else {
				back()
			}
		})
		content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(form, 0, 1, true).AddItem(errorView, 2, 0, false)
		pages.AddPage("profile-password", tuiEnvironmentForm(content, lang.Text("Save password")+" · "+next.Name, lang.Text("Password is encrypted and saved for future connections"), 86, 18), true, true)
		app.SetFocus(form)
	}
	add := func() {
		form := formStyle()
		name := tview.NewInputField().SetLabel(lang.Text("Environment name")).SetFieldWidth(0)
		url := tview.NewInputField().SetLabel(lang.Text("ESXi HTTPS URL")).SetText("https://").SetFieldWidth(0)
		username := tview.NewInputField().SetLabel(lang.Text("Username")).SetText("root").SetFieldWidth(0)
		password := tview.NewInputField().SetLabel(lang.Text("Password")).SetMaskCharacter('*').SetFieldWidth(0)
		for _, field := range []*tview.InputField{name, url, username, password} {
			field.SetBackgroundColor(tcell.NewHexColor(0x12283c))
			form.AddFormItem(field)
		}
		insecure := false
		form.AddCheckbox(lang.Text("Skip TLS certificate verification"), false, func(value bool) { insecure = value })
		errorView := tuiTextView()
		errorView.SetBackgroundColor(tcell.NewHexColor(0x12283c))
		back := func() { password.SetText(""); closePage("profile-add"); app.SetFocus(picker) }
		save := func(connect bool) {
			profileName := strings.TrimSpace(name.GetText())
			p := target.Profile{URL: strings.TrimSpace(url.GetText()), Username: strings.TrimSpace(username.GetText()), Insecure: insecure}
			if err := target.Validate(profileName, p); err != nil {
				errorView.SetText(tuiColor("#f07878", err.Error()))
				return
			}
			saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			err := editConfig(saveCtx, "profile.add", profileName, func(config *target.Config) error {
				if _, ok := config.Profiles[profileName]; ok {
					return fmt.Errorf("profile %q already exists", profileName)
				}
				if err := target.SetPassword(configPath, profileName, &p, password.GetText()); err != nil {
					return err
				}
				config.Profiles[profileName] = p
				return nil
			})
			if err != nil {
				errorView.SetText(tuiColor("#f07878", err.Error()))
				return
			}
			next, err := target.Named(configPath, profileName)
			if err != nil {
				errorView.SetText(tuiColor("#f07878", err.Error()))
				return
			}
			back()
			if connect {
				choose(next)
			} else if action != 0 {
				closePicker()
			} else {
				refreshPicker(profileName)
			}
		}
		form.AddButton(lang.Text("Save"), func() { save(false) }).
			AddButton(lang.Text("Save and Connect"), func() { save(true) }).
			AddButton(lang.Text("Cancel"), func() {
				if action != 0 {
					closePicker()
				} else {
					back()
				}
			}).SetCancelFunc(func() {
			if action != 0 {
				closePicker()
			} else {
				back()
			}
		})
		content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(form, 0, 1, true).AddItem(errorView, 2, 0, false)
		pages.AddPage("profile-add", tuiEnvironmentForm(content, lang.Text("Add environment"), lang.Text("Password is encrypted and saved for future connections"), 90, 24), true, true)
		app.SetFocus(form)
	}
	editProfile = func(profileName string) {
		config, err := target.Load(configPath)
		if err != nil {
			report(err.Error())
			return
		}
		original, exists := config.Profiles[profileName]
		if !exists {
			report(fmt.Sprintf("unknown profile %q", profileName))
			return
		}
		form := formStyle()
		name := tview.NewInputField().SetLabel(lang.Text("Environment name")).SetText(profileName).SetFieldWidth(0)
		address := tview.NewInputField().SetLabel(lang.Text("ESXi HTTPS URL")).SetText(original.URL).SetFieldWidth(0)
		user := tview.NewInputField().SetLabel(lang.Text("Username")).SetText(original.Username).SetFieldWidth(0)
		password := tview.NewInputField().SetLabel(lang.Text("New password (blank keeps saved)")).SetMaskCharacter('*').SetFieldWidth(0)
		for _, field := range []*tview.InputField{name, address, user, password} {
			field.SetBackgroundColor(tcell.NewHexColor(0x12283c))
			form.AddFormItem(field)
		}
		insecure := original.Insecure
		form.AddCheckbox(lang.Text("Skip TLS certificate verification"), insecure, func(value bool) { insecure = value })
		errorView := tuiTextView()
		errorView.SetBackgroundColor(tcell.NewHexColor(0x12283c))
		back := func() { password.SetText(""); closePage("profile-edit"); app.SetFocus(picker) }
		save := func(connect bool) {
			newName := strings.TrimSpace(name.GetText())
			replacement := target.Profile{MCP: original.MCP, URL: strings.TrimSpace(address.GetText()), Username: strings.TrimSpace(user.GetText()), Insecure: insecure}
			saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			err := journal.Run(saveCtx, journal.Event{Source: "tui", Operation: "profile.update", Profile: profileName}, func() error {
				return target.UpdateProfile(saveCtx, configPath, profileName, newName, original, replacement, password.GetText())
			})
			if err != nil {
				errorView.SetText(tuiColor("#f07878", err.Error()))
				return
			}
			password.SetText("")
			delete(sessions, profileName)
			back()
			if connect {
				next, err := target.Named(configPath, newName)
				if err != nil {
					report(err.Error())
					return
				}
				choose(next)
			} else if action != 0 {
				closePicker()
			} else {
				refreshPicker(newName)
			}
		}
		form.AddButton(lang.Text("Save"), func() { save(false) }).
			AddButton(lang.Text("Save and Connect"), func() { save(true) }).
			AddButton(lang.Text("Cancel"), func() {
				if action != 0 {
					closePicker()
				} else {
					back()
				}
			}).SetCancelFunc(func() {
			if action != 0 {
				closePicker()
			} else {
				back()
			}
		})
		content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(form, 0, 1, true).AddItem(errorView, 2, 0, false)
		pages.AddPage("profile-edit", tuiEnvironmentForm(content, lang.Text("Edit environment"), lang.Text("Leave password blank to keep it · Save does not reconnect"), 94, 24), true, true)
		app.SetFocus(form)
	}
	refreshPicker = func(preferred string) {
		config, err := target.Load(configPath)
		if err != nil {
			report(err.Error())
			return
		}
		picker.Clear()
		clear(profileAt)
		allNames := make([]string, 0, len(config.Profiles))
		maxName := 12
		for name := range config.Profiles {
			allNames = append(allNames, name)
			maxName = max(maxName, len(name))
		}
		sort.Strings(allNames)
		maxName = min(24, maxName)
		if legacy != nil {
			label := tuiProfileLabel(lang.Text("ESXI_* connection"), legacy.Profile, !current.Saved(), maxName)
			picker.AddItem(label, "", 0, func() { choose(*legacy) })
		}
		selected := -1
		for _, name := range allNames {
			p := config.Profiles[name]
			index := picker.GetItemCount()
			profileAt[index] = name
			picker.AddItem(tuiProfileLabel(name, p, current.Name == name, maxName), "", 0, func() {
				next, err := target.Named(configPath, name)
				if err != nil {
					report(err.Error())
					return
				}
				if !next.HasPassword() {
					credentialPrompt(next)
					return
				}
				choose(next)
			})
			if name == preferred || (preferred == "" && name == current.Name) {
				selected = index
			}
		}
		if picker.GetItemCount() == 0 {
			picker.AddItem("+ "+lang.Text("Add environment"), "", 0, add)
		}
		if selected >= 0 {
			picker.SetCurrentItem(selected)
		}
	}
	deleteProfile := func() {
		name, ok := profileAt[picker.GetCurrentItem()]
		if !ok {
			return
		}
		config, err := target.Load(configPath)
		if err != nil {
			report(err.Error())
			return
		}
		expected, exists := config.Profiles[name]
		if !exists {
			report(fmt.Sprintf("unknown profile %q", name))
			return
		}
		message := fmt.Sprintf(lang.Text("Delete saved profile %s?\n\n%s\n\nOnly this local profile and saved password are removed. Virtual machines are unchanged."), name, tuiEndpoint(target.Target{Profile: expected}))
		if config.Active == name {
			message += "\n\n" + lang.Text("This is the default profile. Select another after deletion.")
		}
		back := func() { closePage("profile-delete"); app.SetFocus(picker) }
		showTUIConfirmation(app, pages, "profile-delete", lang.Text("Delete profile"), tview.Escape(message), lang.Text("Cancel"), lang.Text("Delete profile"), lang, func(index int) {
			if index != 1 {
				back()
				return
			}
			deleteCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			err := journal.Run(deleteCtx, journal.Event{Source: "tui", Operation: "profile.delete", Profile: name}, func() error { return target.DeleteProfile(deleteCtx, configPath, name, expected) })
			if err != nil {
				back()
				report(err.Error())
				return
			}
			delete(sessions, name)
			if current.Name == name {
				current = target.Target{}
			}
			for _, notify := range onDelete {
				notify(name)
			}
			back()
			if action != 0 {
				closePicker()
			} else {
				refreshPicker("")
			}
		})
	}
	picker.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyDelete {
			deleteProfile()
			return nil
		}
		switch event.Rune() {
		case 'd':
			deleteProfile()
			return nil
		case 'a':
			add()
			return nil
		case 'e':
			if name, ok := profileAt[picker.GetCurrentItem()]; ok {
				editProfile(name)
			}
			return nil
		case 'p':
			if name, ok := profileAt[picker.GetCurrentItem()]; ok {
				config, err := target.Load(configPath)
				if err != nil {
					report(err.Error())
				} else {
					credentialPrompt(target.Target{Name: name, Profile: config.Profiles[name]})
				}
			}
			return nil
		}
		return event
	})
	picker.SetDoneFunc(closePicker)
	refreshPicker(preferred)
	actions := tuiTextView().SetWrap(false).SetText(tuiColor("#79c6f3", lang.Text("Enter Connect · a Add · e Edit · d Delete · Esc Cancel")))
	actions.SetBackgroundColor(tcell.NewHexColor(0x12283c))
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(picker, 0, 1, true).
		AddItem(tview.NewBox().SetBackgroundColor(tcell.NewHexColor(0x12283c)), 1, 0, false).
		AddItem(actions, 1, 0, false)
	height := min(23, max(12, picker.GetItemCount()+10))
	pages.AddPage("profiles", tuiEnvironmentDialog(content, lang.Text("Switch environment"), 84, height), true, true)
	app.SetFocus(picker)
	switch action {
	case 'a':
		add()
	case 'e':
		if name, ok := profileAt[picker.GetCurrentItem()]; ok {
			editProfile(name)
		}
	case 'd':
		deleteProfile()
	case '\n':
		picker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
	}
}

func openTUIProfileConfirmation(app *tview.Application, pages *tview.Pages, picker tview.Primitive, current, next target.Target, lang language, connect func()) {
	message := tview.Escape(fmt.Sprintf(lang.Text("Switch profile from %s to %s?\n%s"), current.Name, next.Name, tuiEndpoint(next)))
	showTUIConfirmation(app, pages, "profile-confirm", lang.Text("Switch environment"), message, lang.Text("Cancel"), lang.Text("Confirm"), lang, func(index int) {
		if index == 1 {
			connect()
		} else {
			app.SetFocus(picker)
		}
	})
}

func tuiProfileDialog(content tview.Primitive, title, note string, width, height int) tview.Primitive {
	heading := tuiTextView().SetText(tview.Escape(note))
	heading.SetBackgroundColor(tuiHeader).SetBorderPadding(0, 0, 1, 1)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(heading, 3, 0, false).AddItem(content, 0, 1, true)
	panel.Box = tview.NewBox().SetBackgroundColor(tuiHeader)
	panel.SetBorder(true).SetBorderColor(tuiAccent).SetTitleColor(tuiAccent).SetTitle(" " + tview.Escape(title) + " ")
	grid := tview.NewGrid()
	grid.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		grid.SetColumns(0, min(width, max(1, w-2)), 0).SetRows(0, min(height, max(1, h-2)), 0)
		return x, y, w, h
	})
	grid.AddItem(panel, 1, 1, 1, 1, 0, 0, true)
	return &tuiModal{grid}
}

func tuiProfileLabel(name string, profile target.Profile, active bool, width int) string {
	mark := " "
	if active {
		mark = "●"
	}
	protocol := "HTTPS"
	if endpoint, err := url.Parse(profile.URL); err == nil && endpoint.Scheme != "" {
		protocol = strings.ToUpper(endpoint.Scheme)
	}
	return fmt.Sprintf("%s %-*.*s  %-5s  %s", mark, width, width, name, protocol, tuiEndpoint(target.Target{Profile: profile}))
}

func tuiEnvironmentDialog(content tview.Primitive, title string, width, height int) tview.Primitive {
	surface := tcell.NewHexColor(0x12283c)
	heading := tuiTextView().SetText(tuiColor("#79c6f3", title))
	heading.SetBackgroundColor(surface)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(heading, 2, 0, false).AddItem(content, 0, 1, true)
	panel.Box = tview.NewBox().SetBackgroundColor(surface)
	panel.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		tuiEnvironmentBorder(screen, x, y, w, h, tcell.NewHexColor(0x4c8dff), surface)
		return x + 3, y + 2, max(0, w-6), max(0, h-4)
	})
	grid := tview.NewGrid()
	grid.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		grid.SetColumns(0, min(width, max(1, w-2)), 0).SetRows(0, min(height, max(1, h-2)), 0)
		return x, y, w, h
	})
	grid.AddItem(panel, 1, 1, 1, 1, 0, 0, true)
	return &tuiModal{grid}
}

func tuiEnvironmentBorder(screen tcell.Screen, x, y, w, h int, color, background tcell.Color) {
	if w < 2 || h < 2 {
		return
	}
	style := tcell.StyleDefault.Foreground(color).Background(background)
	for col := 1; col < w-1; col++ {
		screen.SetContent(x+col, y, '─', nil, style)
		screen.SetContent(x+col, y+h-1, '─', nil, style)
	}
	for row := 1; row < h-1; row++ {
		screen.SetContent(x, y+row, '│', nil, style)
		screen.SetContent(x+w-1, y+row, '│', nil, style)
	}
	for _, corner := range []struct {
		dx, dy int
		r      rune
	}{{0, 0, '┌'}, {w - 1, 0, '┐'}, {0, h - 1, '└'}, {w - 1, h - 1, '┘'}} {
		screen.SetContent(x+corner.dx, y+corner.dy, corner.r, nil, style)
	}
}

func tuiEnvironmentForm(content tview.Primitive, title, note string, width, height int) tview.Primitive {
	description := tuiTextView().SetText(tuiColor("#9ba7b0", note))
	description.SetBackgroundColor(tcell.NewHexColor(0x12283c))
	body := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(description, 2, 0, false).AddItem(content, 0, 1, true)
	return tuiEnvironmentDialog(body, title, width, height)
}
