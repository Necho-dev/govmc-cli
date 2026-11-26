package tui

import (
	"context"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/Necho-dev/govmc-cli/internal/workflow"
	"github.com/rivo/tview"
	"strconv"
	"strings"
	"time"
)

func openTUIClone(ctx context.Context, app *tview.Application, pages *tview.Pages, t target.Target, vm esxi.VMInfo, l language, onSubmitted func(tasks.Task), destinationStores ...string) {
	if vm.PowerState != "poweredOff" || vm.ConnectionState != "connected" {
		showTUIConfirmation(app, pages, "clone-unavailable", l.Text("Clone machine"), l.Text("Clone source must be powered off and connected"), l.Text("Close"), "", l, func(int) {})
		return
	}
	form := tview.NewForm().SetLabelColor(tuiAccent).SetFieldBackgroundColor(tuiHeader).SetFieldTextColor(tuiText).SetButtonBackgroundColor(tuiSelect)
	form.SetBackgroundColor(tuiHeader)
	ds, file := "", ""
	if vm.Config != nil {
		file = vm.Config.VMX
	}
	if file == "" && len(vm.Disks) > 0 {
		file = vm.Disks[0].FileName
	}
	if strings.HasPrefix(file, "[") {
		if i := strings.Index(file, "]"); i > 0 {
			ds = file[1:i]
		}
	}
	names := []string{ds}
	for _, name := range destinationStores {
		if name != ds {
			names = append(names, name)
		}
	}
	form.AddInputField(l.Text("Target name"), vm.Name+"-clone", 32, nil, nil).
		AddTextView(l.Text("Source datastore"), ds, 32, 1, false, false).
		AddDropDown(l.Text("Destination datastore"), names, 0, nil).
		AddInputField("vCPU", fmt.Sprint(vm.NumCPU), 10, tview.InputFieldInteger, nil).
		AddInputField(l.Text("Memory MiB"), fmt.Sprint(vm.MemoryMB), 12, tview.InputFieldInteger, nil).
		AddInputField(l.Text("Portgroup (inherit if empty)"), "", 32, nil, nil).
		AddCheckbox(l.Text("Power on after verification"), false, nil)
	for _, label := range []string{"CPU hot add", "Memory hot add", "Memory reservation lock"} {
		form.AddDropDown(l.Text(label), []string{l.Text("Inherit"), l.Text("Enabled"), l.Text("Disabled")}, 0, nil)
	}
	form.AddInputField(l.Text("Expand disks (key:MiB, ...)"), "", 36, nil, nil)
	styleSettingsFilterDropdown(form.GetFormItem(2).(*tview.DropDown))
	message := tuiTextView().SetText(tview.Escape(t.Name + " / " + vm.Name + " (" + vm.ID + ")"))
	message.SetBackgroundColor(tuiHeader)
	busy := false
	form.AddButton(l.Text("Preview changes"), func() {
		if busy {
			return
		}
		input := func(i int) string { return form.GetFormItem(i).(*tview.InputField).GetText() }
		cpu, e := strconv.ParseInt(input(3), 10, 32)
		if e != nil {
			message.SetText(l.Text("Invalid CPU or memory"))
			return
		}
		mem, e := strconv.ParseInt(input(4), 10, 32)
		if e != nil {
			message.SetText(l.Text("Invalid CPU or memory"))
			return
		}
		cpu32, mem32 := int32(cpu), int32(mem)
		power := form.GetFormItem(6).(*tview.Checkbox).IsChecked()
		dry := true
		targetIndex, _ := form.GetFormItem(2).(*tview.DropDown).GetCurrentOption()
		spec := esxi.CloneSpec{DestinationDatastore: names[targetIndex], Src: vm.ID, Dst: input(0), Datastore: ds, NumCPU: &cpu32, MemoryMB: &mem32, Portgroup: input(5), PowerOn: &power, DryRun: &dry}
		fields := []**bool{&spec.CPUHotAdd, &spec.MemoryHotAdd, &spec.LockMemory}
		for i, p := range fields {
			n, _ := form.GetFormItem(7 + i).(*tview.DropDown).GetCurrentOption()
			if n != 0 {
				x := n == 1
				*p = &x
			}
		}
		if v := strings.TrimSpace(input(10)); v != "" {
			for _, pair := range strings.Split(v, ",") {
				parts := strings.Split(strings.TrimSpace(pair), ":")
				if len(parts) != 2 {
					message.SetText(l.Text("Invalid disk expansion"))
					return
				}
				key, e := strconv.ParseInt(parts[0], 10, 32)
				size, err := strconv.ParseInt(parts[1], 10, 64)
				if e != nil || err != nil {
					message.SetText(l.Text("Invalid disk expansion"))
					return
				}
				spec.ExpandDisks = append(spec.ExpandDisks, esxi.DiskExpansion{Key: int32(key), CapacityMiB: size})
			}
		}
		busy = true
		message.SetText(l.Text("Loading"))
		go func() {
			dial, stop := context.WithTimeout(ctx, 45*time.Second)
			defer stop()
			c, err := t.Dial(dial)
			var plan *esxi.ClonePlan
			if err == nil {
				spec, plan, err = c.PrepareClone(dial, spec)
				target.Close(c)
			}
			if ctx.Err() != nil {
				return
			}
			app.QueueUpdateDraw(func() {
				busy = false
				if err != nil {
					message.SetText(tview.Escape(err.Error()))
					return
				}
				raw := cloneSummary(spec, plan, l)
				showTUIConfirmation(app, pages, "clone-confirm", l.Text("Confirm clone"), tview.Escape(raw), l.Text("Back"), l.Text("Confirm"), l, func(index int) {
					if index == 0 {
						app.SetFocus(form)
						return
					}
					busy = true
					message.SetText(l.Text("Submitting task"))
					go func() {
						c, e := t.Dial(ctx)
						var out any
						if e == nil {
							no := false
							spec.DryRun = &no
							spec.Confirm = true
							out, e = workflow.Clone(ctx, tasks.From(ctx), c, t.Name, c.Endpoint(), spec, "", func() { target.Close(c) })
						}
						if ctx.Err() != nil {
							return
						}
						app.QueueUpdateDraw(func() {
							busy = false
							if e != nil {
								message.SetText(tview.Escape(e.Error()))
								app.SetFocus(form)
								return
							}
							pages.RemovePage("clone-editor")
							onSubmitted(out.(tasks.Task))
						})
					}()
				})
			})
		}()
	})
	form.AddButton(l.Text("Cancel"), func() {
		if !busy {
			pages.RemovePage("clone-editor")
		}
	})
	form.SetCancelFunc(func() {
		if !busy {
			pages.RemovePage("clone-editor")
		}
	})
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(message, 2, 0, false).AddItem(form, 0, 1, true)
	pages.AddPage("clone-editor", tuiEnvironmentDialog(content, l.Text("Clone machine"), 84, 30), true, true)
	app.SetFocus(form)
}

func cloneSummary(s esxi.CloneSpec, p *esxi.ClonePlan, l language) string {
	text := fmt.Sprintf("%s  %s → %s\n%s  [%s] %s\n%s  [%s] %s\nCPU / %s  %d vCPU / %d MiB\n%s  %v\n\n", l.Text("Clone machine"), s.Src, s.Dst, l.Text("Source datastore"), s.Datastore, s.SrcFolder, l.Text("Destination datastore"), s.TargetDatastore(), s.Dst, l.Text("Memory MiB"), *s.NumCPU, *s.MemoryMB, l.Text("Power on after verification"), s.PowerOn != nil && *s.PowerOn)
	for _, pair := range []struct {
		label string
		value *bool
	}{{"CPU hot add", s.CPUHotAdd}, {"Memory hot add", s.MemoryHotAdd}, {"Memory reservation lock", s.LockMemory}} {
		if pair.value != nil {
			text += fmt.Sprintf("%s  %v\n", l.Text(pair.label), *pair.value)
		}
	}
	if s.MemoryReservationMB != nil {
		text += fmt.Sprintf("%s  %d\n", l.Text("memoryReservationMB"), *s.MemoryReservationMB)
	}
	for _, disk := range s.ExpandDisks {
		text += fmt.Sprintf("%s  %d → %d MiB\n", l.Text("Disk MiB"), disk.Key, disk.CapacityMiB)
	}
	for _, st := range p.Steps {
		text += "  • " + l.Text(st.Op) + "\n"
	}
	return text + "\n" + l.Text("New UUID and MAC; guest OS identity remains unchanged")
}
