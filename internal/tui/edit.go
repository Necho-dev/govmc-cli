package tui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func openTUIEditor(app *tview.Application, pages *tview.Pages, list tview.Primitive, environment string, vm esxi.VMInfo, portgroups []esxi.Portgroup, lang language, submit func(esxi.VMChange)) {
	config := vm.Config
	offline := vm.PowerState == "poweredOff"
	running := vm.PowerState == "poweredOn"
	closeEditor := func() { pages.RemovePage("editor"); app.SetFocus(list) }
	if config == nil || !config.Editable {
		message := tview.NewModal().SetText(lang.Text("VM configuration is unavailable")).AddButtons([]string{lang.Text("Close")})
		message.SetDoneFunc(func(int, string) { closeEditor() })
		pages.AddPage("editor", &tuiModal{message}, true, true)
		app.SetFocus(message)
		return
	}

	navigation := tview.NewList().ShowSecondaryText(false).SetMainTextColor(tuiText).
		SetMainTextStyle(tcell.StyleDefault.Foreground(tuiText).Background(tuiHeader)).
		SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tuiSelect)).SetHighlightFullLine(true)
	navigation.SetBackgroundColor(tuiHeader)
	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.SetBackgroundColor(tuiHeader)
	var form *tview.Form
	var forms []*tview.Form
	var sectionNames []string
	startSection := func(name string) {
		form = tview.NewForm().SetItemPadding(1).SetFieldBackgroundColor(tuiSelect).SetFieldTextColor(tuiText).
			SetLabelColor(tuiMuted).SetButtonBackgroundColor(tuiSelect).SetButtonTextColor(tuiText)
		form.SetBackgroundColor(tuiHeader).SetBorderPadding(1, 0, 2, 2)
		form.SetCancelFunc(closeEditor)
		forms = append(forms, form)
		sectionNames = append(sectionNames, name)
	}
	startSection(lang.Text("General"))
	form.AddTextView(lang.Text("Operating system"), tuiOSName(vm, lang), 0, 2, false, true).
		AddTextView(lang.Text("Virtual hardware"), config.Version, 24, 1, false, true).
		AddTextView(lang.Text("Firmware"), config.Firmware, 24, 1, false, true).
		AddTextView(lang.Text("VM configuration file"), config.VMX, 0, 2, false, true).
		AddTextView(lang.Text("Suspend directory"), config.SuspendDirectory, 0, 2, false, true).
		AddTextView(lang.Text("Annotation"), config.Annotation, 0, 2, false, true)
	sectionLabel := func(label string) string {
		for index, item := range forms {
			if item == form {
				return sectionNames[index] + " · " + lang.Text(label)
			}
		}
		return lang.Text(label)
	}
	type field struct {
		label, before, after string
		set                  func(*esxi.VMChange, string) error
	}
	var fields []*field
	add := func(label, before string, set func(*esxi.VMChange, string) error) {
		item := &field{label: sectionLabel(label), before: before, after: before, set: set}
		fields = append(fields, item)
		form.AddInputField(lang.Text(label), before, 24, nil, func(value string) { item.after = strings.TrimSpace(value) })
	}
	choice := func(label, before string, options []string, set func(*esxi.VMChange, string) error) {
		item := &field{label: sectionLabel(label), before: before, after: before, set: set}
		fields = append(fields, item)
		form.AddDropDown(label, options, 0, func(option string, index int) {
			item.after = before
			if index > 0 {
				item.after = option
			}
		})
	}
	number := func(label string, before int32, minimum int32, assign func(*esxi.VMChange, *int32)) {
		add(label, strconv.FormatInt(int64(before), 10), func(p *esxi.VMChange, value string) error {
			n, err := strconv.ParseInt(value, 10, 32)
			if err != nil || n < int64(minimum) {
				return fmt.Errorf("%s ≥ %d", lang.Text(label), minimum)
			}
			v := int32(n)
			assign(p, &v)
			return nil
		})
	}
	resource := func(label string, before *int64, limit bool, assign func(*esxi.VMChange, *int64)) {
		if !offline {
			form.AddTextView(lang.Text(label), tuiOptional(before), 24, 1, false, true)
			return
		}
		initial := ""
		if before != nil {
			initial = fmt.Sprint(*before)
		}
		add(label, initial, func(p *esxi.VMChange, value string) error {
			n, err := strconv.ParseInt(value, 10, 64)
			valid := n >= 0
			if limit {
				valid = n == -1 || n > 0
			}
			if err != nil || !valid || n > 2147483647 {
				return fmt.Errorf("%s · %s", lang.Text(label), lang.Text("Invalid resource value"))
			}
			assign(p, &n)
			return nil
		})
	}
	boolean := func(label string, before *bool, assign func(*esxi.VMChange, *bool)) {
		if before == nil {
			choice(lang.Text(label), lang.Text("Unknown"), []string{lang.Text("Unknown"), lang.Text("Enabled"), lang.Text("Disabled")}, func(p *esxi.VMChange, value string) error {
				enabled := value == lang.Text("Enabled")
				assign(p, &enabled)
				return nil
			})
			return
		}
		item := &field{label: sectionLabel(label), before: strconv.FormatBool(*before), after: strconv.FormatBool(*before)}
		item.set = func(p *esxi.VMChange, value string) error {
			enabled := value == "true"
			assign(p, &enabled)
			return nil
		}
		fields = append(fields, item)
		form.AddCheckbox(lang.Text(label), *before, func(value bool) { item.after = strconv.FormatBool(value) })
	}
	if offline {
		add("Name", vm.Name, func(p *esxi.VMChange, value string) error {
			if value == "" || len([]rune(value)) > 80 {
				return fmt.Errorf("%s", lang.Text("VM name cannot be empty"))
			}
			p.Name = &value
			return nil
		})
	}
	startSection("CPU")
	form.AddTextView(lang.Text("Current vCPU"), fmt.Sprint(vm.NumCPU), 24, 1, false, true)
	if offline || running && config.CPUHotAdd != nil && *config.CPUHotAdd {
		minimum := int32(1)
		if running {
			minimum = vm.NumCPU
		}
		number("vCPU", vm.NumCPU, minimum, func(p *esxi.VMChange, v *int32) { p.NumCPU = v })
	}
	startSection(lang.Text("Memory"))
	form.AddTextView(lang.Text("Current memory"), fmt.Sprintf("%.1f GiB", float64(vm.MemoryMB)/1024), 24, 1, false, true)
	if offline || running && config.MemoryHotAdd != nil && *config.MemoryHotAdd {
		minimum := int32(1)
		if running {
			minimum = vm.MemoryMB
		}
		number("Memory (MiB)", vm.MemoryMB, minimum, func(p *esxi.VMChange, v *int32) { p.MemoryMB = v })
	}
	if offline {
		form = forms[1]
		before := ""
		if config.CoresPerSocket != nil {
			before = fmt.Sprint(*config.CoresPerSocket)
		}
		add("Cores per socket", before, func(p *esxi.VMChange, value string) error {
			n, err := strconv.ParseInt(value, 10, 32)
			if err != nil || n < 0 {
				return fmt.Errorf("%s ≥ 0", lang.Text("Cores per socket"))
			}
			v := int32(n)
			p.CoresPerSocket = &v
			return nil
		})
		boolean("CPU hot add", config.CPUHotAdd, func(p *esxi.VMChange, v *bool) { p.CPUHotAdd = v })
		form = forms[2]
		boolean("Memory hot add", config.MemoryHotAdd, func(p *esxi.VMChange, v *bool) { p.MemoryHotAdd = v })
		form = forms[1]
		boolean("Nested virtualization", config.NestedHV, func(p *esxi.VMChange, v *bool) { p.NestedHV = v })
		boolean("Virtual IOMMU", config.IOMMU, func(p *esxi.VMChange, v *bool) { p.IOMMU = v })
	}
	form = forms[1]
	resource("CPU reservation (MHz)", config.CPUReservationMHz, false, func(p *esxi.VMChange, v *int64) { p.CPUReservationMHz = v })
	resource("CPU limit (MHz, -1 unlimited)", config.CPULimitMHz, true, func(p *esxi.VMChange, v *int64) { p.CPULimitMHz = v })
	if offline {
		boolean("CPU performance counters", config.VPMC, func(p *esxi.VMChange, v *bool) { p.VPMC = v })
	}
	form.AddTextView(lang.Text("CPU shares"), config.CPUShares, 24, 1, false, true)
	if !offline {
		form.AddTextView(lang.Text("CPU hot add"), tuiBool(config.CPUHotAdd, lang), 24, 1, false, true).
			AddTextView(lang.Text("Nested virtualization"), tuiBool(config.NestedHV, lang), 24, 1, false, true).
			AddTextView(lang.Text("Virtual IOMMU"), tuiBool(config.IOMMU, lang), 24, 1, false, true).
			AddTextView(lang.Text("CPU performance counters"), tuiBool(config.VPMC, lang), 24, 1, false, true)
	}
	form = forms[2]
	resource("Memory reservation (MiB)", config.MemoryReservationMB, false, func(p *esxi.VMChange, v *int64) { p.MemoryReservationMB = v })
	resource("Memory limit (MiB, -1 unlimited)", config.MemoryLimitMB, true, func(p *esxi.VMChange, v *int64) { p.MemoryLimitMB = v })
	form.AddTextView(lang.Text("Memory shares"), config.MemoryShares, 24, 1, false, true).
		AddTextView(lang.Text("Reserve all memory"), tuiBool(config.MemoryLocked, lang), 24, 1, false, true)
	if !offline {
		form.AddTextView(lang.Text("Memory hot add"), tuiBool(config.MemoryHotAdd, lang), 24, 1, false, true)
	}
	if config.DeviceEditable && (offline || running) {
		capacity := func(label, before string, minimum int64, set func(*esxi.VMChange, int64)) {
			add(label, before, func(p *esxi.VMChange, value string) error {
				n, err := strconv.ParseInt(value, 10, 64)
				if err != nil || n < minimum || n > 64*1024*1024 {
					return fmt.Errorf("%s: %d–%d MiB", label, minimum, 64*1024*1024)
				}
				set(p, n)
				return nil
			})
		}
		for _, disk := range vm.Disks {
			startSection(disk.Label)
			form.AddTextView(lang.Text("Current capacity"), fmt.Sprintf("%.1f GiB", float64(disk.Capacity)/(1<<20)), 28, 1, false, true).
				AddTextView(lang.Text("Disk file"), disk.FileName, 0, 2, false, true).
				AddTextView(lang.Text("Disk mode"), disk.Mode, 28, 1, false, true).
				AddTextView(lang.Text("Thin provisioned"), tuiBool(disk.Thin, lang), 28, 1, false, true).
				AddTextView(lang.Text("Controller / unit"), fmt.Sprint(disk.ControllerKey)+" / "+tuiOptional(disk.UnitNumber), 28, 1, false, true).
				AddTextView(lang.Text("IOPS limit (-1 unlimited)"), tuiOptional(disk.IOPSLimit), 28, 1, false, true)
			if disk.Key == 0 || disk.Capacity <= 0 || running && !tuiOnlineDisk(config, disk) {
				form.AddTextView("", lang.Text("Expansion is unavailable for this controller or power state"), 0, 2, false, true)
				continue
			}
			label := lang.Text("Capacity (MiB)")
			capacity(label, strconv.FormatFloat(float64(disk.Capacity)/1024, 'f', -1, 64), disk.Capacity/1024+1, func(p *esxi.VMChange, n int64) {
				p.ExpandDisks = append(p.ExpandDisks, esxi.DiskExpansion{Key: disk.Key, CapacityMiB: n})
			})
		}
		startSection(lang.Text("Add disk"))
		form.AddTextView("", lang.Text("New disks use the VM datastore and an available SCSI slot"), 0, 2, false, true)
		capacity(lang.Text("Add disk (MiB)"), "", 1, func(p *esxi.VMChange, n int64) { p.AddDiskMiB = &n })
		for _, nic := range vm.NICs {
			if nic.Key == 0 {
				continue
			}
			startSection(nic.Label)
			form.AddTextView("MAC", nic.MAC, 28, 1, false, true).
				AddTextView(lang.Text("Adapter"), nic.Adapter, 28, 1, false, true).
				AddTextView(lang.Text("IP addresses"), strings.Join(nic.IPAddresses, ", "), 0, 2, false, true).
				AddTextView(lang.Text("Address assignment"), nic.AddressType, 28, 1, false, true)
			change := func(p *esxi.VMChange) *esxi.NICChange {
				for i := range p.EditNICs {
					if p.EditNICs[i].Key == nic.Key {
						return &p.EditNICs[i]
					}
				}
				p.EditNICs = append(p.EditNICs, esxi.NICChange{Key: nic.Key})
				return &p.EditNICs[len(p.EditNICs)-1]
			}
			label := nic.Label + " · "
			networks := tuiNetworkOptions(portgroups, nic.Portgroup)
			if len(networks) > 0 {
				current := nic.Portgroup
				if current == "" {
					current = "—"
				}
				choice(label+lang.Text("Portgroup"), current, append([]string{current}, networks...), func(p *esxi.VMChange, value string) error {
					change(p).Portgroup = value
					return nil
				})
			}
			if running && nic.Connected != nil {
				boolean(label+lang.Text("Connected"), nic.Connected, func(p *esxi.VMChange, v *bool) { change(p).Connected = v })
			}
			if nic.StartConnected != nil {
				boolean(label+lang.Text("Connect at power on"), nic.StartConnected, func(p *esxi.VMChange, v *bool) { change(p).StartConnected = v })
			}
		}
		networks := tuiNetworkOptions(portgroups, "")
		if len(networks) > 0 {
			startSection(lang.Text("Add NIC"))
			adapter, start := "vmxnet3", true
			choice(lang.Text("Add NIC"), "", append([]string{lang.Text("Select portgroup")}, networks...), func(p *esxi.VMChange, value string) error {
				p.AddNIC = &esxi.NewNIC{Portgroup: value, Adapter: adapter, StartConnected: start}
				return nil
			})
			form.AddDropDown(lang.Text("Adapter"), []string{"VMXNET 3", "E1000e"}, 0, func(_ string, index int) {
				if index >= 0 {
					adapter = []string{"vmxnet3", "e1000e"}[index]
				}
			})
			form.AddCheckbox(lang.Text("New NIC connects at power on"), start, func(value bool) { start = value })
		}
	}
	startSection(lang.Text("Boot / security"))
	form.AddTextView(lang.Text("Firmware"), config.Firmware, 24, 1, false, true).
		AddTextView(lang.Text("Secure boot"), tuiBool(config.SecureBoot, lang), 24, 1, false, true).
		AddTextView("VBS", tuiBool(config.VBS, lang), 24, 1, false, true).
		AddTextView(lang.Text("Boot delay (ms)"), tuiOptional(config.BootDelayMS), 24, 1, false, true).
		AddTextView(lang.Text("Boot retry"), tuiBool(config.BootRetry, lang), 24, 1, false, true)
	startSection(lang.Text("Console options"))
	form.AddTextView(lang.Text("Console session limit"), fmt.Sprint(config.MaxMksConnections), 24, 1, false, true).
		AddTextView(lang.Text("Lock guest after disconnect"), tuiBool(config.GuestAutoLock, lang), 24, 1, false, true)
	startSection(lang.Text("Tools / power management"))
	form.AddTextView(lang.Text("Tools"), tuiTools(vm.Guest, lang), 0, 2, false, true).
		AddTextView(lang.Text("Tools running"), vm.Guest.ToolsRunningStatus, 28, 1, false, true).
		AddTextView(lang.Text("Tools upgrade policy"), config.ToolsUpgradePolicy, 28, 1, false, true).
		AddTextView(lang.Text("Standby action"), config.StandbyAction, 28, 1, false, true)
	for _, device := range config.Devices {
		found := false
		for _, disk := range vm.Disks {
			if disk.Key == device.Key {
				found = true
			}
		}
		for _, nic := range vm.NICs {
			if nic.Key == device.Key {
				found = true
			}
		}
		if found {
			continue
		}
		startSection(device.Label)
		form.AddTextView(lang.Text("Type"), device.Type, 0, 1, false, true).
			AddTextView(lang.Text("Device key"), fmt.Sprint(device.Key), 24, 1, false, true)
		if device.Type == "VirtualMachineVideoCard" {
			form.AddTextView(lang.Text("Displays"), fmt.Sprint(device.Displays), 24, 1, false, true).
				AddTextView(lang.Text("Video RAM (MiB)"), fmt.Sprintf("%.1f", float64(device.VideoRAMKB)/1024), 24, 1, false, true).
				AddTextView("3D", tuiBool(device.Enable3D, lang), 24, 1, false, true)
		}
	}
	showModal := func(page, message string, buttons []string, done func(int, string)) {
		modal := tview.NewModal().SetText(message).AddButtons(buttons).SetDoneFunc(done).
			SetBackgroundColor(tuiHeader).SetTextColor(tuiText).SetButtonBackgroundColor(tuiSelect)
		pages.AddPage(page, &tuiModal{modal}, true, true)
		app.SetFocus(modal)
	}
	if len(fields) == 0 {
		showModal("editor", lang.Text("Power off the VM or enable hot add before editing"), []string{lang.Text("Close")}, func(int, string) { closeEditor() })
		return
	}
	preview := func() {
		patch := esxi.VMChange{ChangeVersion: config.ChangeVersion, RequirePoweredOff: offline, RequireHotAdd: running}
		lines := []string{tview.Escape(environment + " / " + vm.Name + " (" + vm.ID + ")")}
		for _, item := range fields {
			if item.before == item.after {
				continue
			}
			if err := item.set(&patch, item.after); err != nil {
				lines = []string{err.Error()}
				break
			}
			display := func(value string) string {
				switch value {
				case "":
					return "—"
				case "true":
					return lang.Text("Enabled")
				case "false":
					return lang.Text("Disabled")
				default:
					return value
				}
			}
			lines = append(lines, tview.Escape(fmt.Sprintf("%s  %s → %s", item.label, display(item.before), display(item.after))))
		}
		if patch.AddNIC != nil && len(lines) > 1 {
			lines = append(lines, tview.Escape(fmt.Sprintf("%s · %s: %t", patch.AddNIC.Adapter, lang.Text("Connect at power on"), patch.AddNIC.StartConnected)))
		}
		buttons := []string{lang.Text("Back")}
		if len(lines) > 1 {
			buttons = append(buttons, lang.Text("Apply"))
		} else if lines[0] == tview.Escape(environment+" / "+vm.Name+" ("+vm.ID+")") {
			lines[0] = lang.Text("No changes")
		}
		showTUIPreview(app, pages, lines, buttons, lang, func(index int) {
			pages.RemovePage("preview")
			if index != 1 {
				app.SetFocus(form)
				return
			}
			closeEditor()
			saving := tview.NewModal().SetText(lang.Text("Saving configuration…")).SetBackgroundColor(tuiHeader)
			pages.AddPage("saving", &tuiModal{saving}, true, true)
			app.SetFocus(saving)
			submit(patch)
		})
	}
	actions := tview.NewForm().SetButtonBackgroundColor(tuiSelect).SetButtonTextColor(tuiText).
		AddButton(lang.Text("Preview changes"), preview).AddButton(lang.Text("Cancel"), closeEditor)
	actions.SetBackgroundColor(tuiHeader)
	heading := tuiTextView().SetText(tuiColor("#79c6f3", environment+" / "+vm.Name+" ("+vm.ID+")") + "  " + lang.Power(vm.PowerState))
	heading.SetBackgroundColor(tuiHeader).SetBorderPadding(0, 0, 1, 1)
	hints := tuiTextView().SetText(tuiColor("#9ba7b0", lang.Text("Tab/Shift+Tab Fields · ↑/↓ Move · Space Toggle\nF6 Modules · F8 Actions · Ctrl+S Preview · Esc Cancel")))
	hints.SetBackgroundColor(tuiHeader)
	active := 0
	editable := make([][]int, len(forms))
	for index, itemForm := range forms {
		for itemIndex := range itemForm.GetFormItemCount() {
			switch itemForm.GetFormItem(itemIndex).(type) {
			case *tview.InputField, *tview.Checkbox, *tview.DropDown:
				editable[index] = append(editable[index], itemIndex)
			}
		}
	}
	focusField := func(index int) {
		form = forms[active]
		form.SetFocus(index)
		app.SetFocus(form)
	}
	focusFirst := func() {
		if len(editable[active]) > 0 {
			focusField(editable[active][0])
		} else {
			app.SetFocus(navigation)
		}
	}
	closeDropDown := func() {
		for i := range form.GetFormItemCount() {
			if menu, ok := form.GetFormItem(i).(*tview.DropDown); ok && menu.IsOpen() {
				// The private options list owns focus while a dropdown is open.
				// Send Escape there so Form's cancel handler is not invoked.
				if handler := app.GetFocus().InputHandler(); handler != nil {
					handler(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(p tview.Primitive) { app.SetFocus(p) })
				}
				return
			}
		}
	}
	selectSection := func(index int) {
		if index == active && content.GetItemCount() > 0 {
			return
		}
		active = index
		form = forms[index]
		for i := range form.GetFormItemCount() {
			if item, ok := form.GetFormItem(i).(interface{ SetBackgroundColor(tcell.Color) *tview.Box }); ok {
				item.SetBackgroundColor(tuiHeader)
			}
		}
		content.Clear()
		title := tuiTextView().SetText(tuiColor("#79c6f3", sectionNames[index]))
		title.SetBackgroundColor(tuiHeader).SetBorderPadding(0, 0, 2, 0)
		content.AddItem(title, 1, 0, false).AddItem(form, 0, 1, true)
	}
	for index, name := range sectionNames {
		navigation.AddItem(name, "", 0, func() { selectSection(index); focusFirst() })
	}
	navigation.SetChangedFunc(func(index int, _, _ string, _ rune) { selectSection(index) })
	split := tview.NewFlex().AddItem(navigation, 21, 0, true).AddItem(content, 0, 1, false)
	split.SetBackgroundColor(tuiHeader)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(heading, 2, 0, false).
		AddItem(split, 0, 1, true).AddItem(hints, 2, 0, false).AddItem(actions, 3, 0, false)
	panel.Box = tview.NewBox().SetBackgroundColor(tuiHeader)
	panel.SetBorder(true).SetBorderColor(tuiAccent).SetTitleColor(tuiAccent).SetTitle(" " + lang.Text("Edit configuration") + " ")
	lastWidth := 0
	panel.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		if width != lastWidth {
			lastWidth = width
			split.Clear()
			if width < 75 {
				split.SetDirection(tview.FlexRow).AddItem(navigation, 3, 0, true).AddItem(content, 0, 1, false)
			} else {
				split.SetDirection(tview.FlexColumn).AddItem(navigation, 21, 0, true).AddItem(content, 0, 1, false)
			}
		}
		return x + 1, y + 1, width - 2, height - 2
	})
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := event.Key()
		itemIndex, _ := form.GetFocusedItemIndex()
		var menu *tview.DropDown
		if itemIndex >= 0 {
			menu, _ = form.GetFormItem(itemIndex).(*tview.DropDown)
		}
		if menu != nil && menu.IsOpen() {
			switch key {
			case tcell.KeyEscape:
				closeDropDown()
				return nil
			case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyF6, tcell.KeyF8, tcell.KeyCtrlS:
				closeDropDown()
			default:
				return event
			}
		}
		switch key {
		case tcell.KeyF6:
			if navigation.HasFocus() {
				focusFirst()
			} else {
				app.SetFocus(navigation)
			}
		case tcell.KeyF8:
			app.SetFocus(actions)
		case tcell.KeyCtrlS:
			preview()
		case tcell.KeyEscape:
			closeEditor()
		case tcell.KeyTab, tcell.KeyBacktab:
			if navigation.HasFocus() {
				if key == tcell.KeyBacktab {
					actions.SetFocus(actions.GetButtonCount() - 1)
					app.SetFocus(actions)
				} else if len(editable[active]) == 0 {
					app.SetFocus(actions)
				} else {
					focusFirst()
				}
				return nil
			}
			if actions.HasFocus() {
				_, button := actions.GetFocusedItemIndex()
				if key == tcell.KeyTab && button == actions.GetButtonCount()-1 {
					app.SetFocus(navigation)
					return nil
				}
				if key == tcell.KeyBacktab && button == 0 {
					if fields := editable[active]; len(fields) > 0 {
						focusField(fields[len(fields)-1])
					} else {
						app.SetFocus(navigation)
					}
					return nil
				}
				return event
			}
			fallthrough
		case tcell.KeyUp, tcell.KeyDown:
			if !form.HasFocus() {
				return event
			}
			// Closed dropdowns keep their normal arrow-key selection behavior.
			if menu != nil && (key == tcell.KeyUp || key == tcell.KeyDown) {
				return event
			}
			forward := key == tcell.KeyTab || key == tcell.KeyDown
			fields := editable[active]
			if forward {
				for _, index := range fields {
					if index > itemIndex {
						focusField(index)
						return nil
					}
				}
				app.SetFocus(actions)
			} else {
				for i := len(fields) - 1; i >= 0; i-- {
					if fields[i] < itemIndex {
						focusField(fields[i])
						return nil
					}
				}
				app.SetFocus(navigation)
			}
		default:
			return event
		}
		return nil
	})
	dialog := tview.NewGrid()
	dialog.SetDrawFunc(func(_ tcell.Screen, x, y, width, height int) (int, int, int, int) {
		dialog.SetColumns(0, min(104, max(1, width-4)), 0).SetRows(0, min(28, max(1, height-2)), 0)
		return x, y, width, height
	})
	dialog.AddItem(panel, 1, 1, 1, 1, 0, 0, true)
	selectSection(0)
	pages.AddPage("editor", &tuiModal{dialog}, true, true)
	focusFirst()
}

func tuiOnlineDisk(config *esxi.VMConfigState, disk esxi.Disk) bool {
	for _, device := range config.Devices {
		if device.Key == disk.ControllerKey {
			return disk.Mode == "persistent" && (device.Type == "ParaVirtualSCSIController" || device.Type == "VirtualNVMEController")
		}
	}
	return false
}

func tuiNetworkOptions(portgroups []esxi.Portgroup, exclude string) []string {
	var names []string
	for _, group := range portgroups {
		if group.Name != "" && group.Name != exclude {
			names = append(names, group.Name)
		}
	}
	sort.Strings(names)
	return slices.Compact(names)
}

func showTUIPreview(app *tview.Application, pages *tview.Pages, lines, buttons []string, lang language, done func(int)) {
	confirmLabel := ""
	if len(buttons) > 1 {
		confirmLabel = buttons[1]
	}
	showTUIConfirmation(app, pages, "preview", lang.Text("Preview changes"), strings.Join(lines, "\n"), buttons[0], confirmLabel, lang, done)
}

// Mouse releases and clicks in the modal's padding must not reach the page below.
type tuiModal struct{ tview.Primitive }

func (m *tuiModal) Draw(screen tcell.Screen) {
	x, y, w, h := m.GetRect()
	background := tview.NewBox().SetBackgroundColor(tuiSurface)
	background.SetRect(x, y, w, h)
	background.Draw(screen)
	m.Primitive.Draw(screen)
}

func (m *tuiModal) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(tview.Primitive)) (bool, tview.Primitive) {
		_, capture := m.Primitive.MouseHandler()(action, event, setFocus)
		return true, capture
	}
}
