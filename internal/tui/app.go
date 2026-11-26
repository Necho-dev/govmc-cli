package tui

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Necho-dev/govmc-cli"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type tuiRequest struct {
	target                   target.Target
	page                     int
	datastore, path          string
	vmID                     string
	screenshot               bool
	counterEntity, counterVM string
}

type tuiData struct {
	counters                 []esxi.CounterInfo
	counterVM, counterTarget string
	host                     *esxi.HostInfo
	vmDetail                 *esxi.VMInfo
	screenshot               *tuiScreenshot
	screenshotWarning        string
	detailWarning            string
	overviewWarning          string
	about                    esxi.About
	metrics                  *esxi.MetricsResponse
	metricWarning, trendVM   string
	vms                      []esxi.VMInfo
	stores                   []esxi.DatastoreInfo
	files                    []esxi.DSFile
}

// Run starts the terminal workbench with the selected configuration and environment.
func Run(parent context.Context, configPath string, current target.Target, locale string) error {
	return runTUIApplication(parent, configPath, current, language(locale), tview.NewApplication().EnableMouse(true))
}

func runTUIApplication(parent context.Context, configPath string, current target.Target, lang language, app *tview.Application) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	store := journal.From(ctx)
	if store == nil {
		var err error
		store, err = journal.Open(configPath, "tui")
		if err != nil {
			return err
		}
		defer store.Close()
		ctx = journal.With(ctx, store)
	}
	if tasks.From(ctx) == nil {
		manager, err := tasks.Open(configPath, "tui")
		if err != nil {
			return err
		}
		defer manager.Close()
		ctx = tasks.With(ctx, manager)
	}
	pages := tview.NewPages()
	brand := tuiTextView().SetText(tuiWordmark()).SetTextColor(tcell.NewHexColor(0x00d7d7))
	brand.SetWrap(false)
	header := tview.NewFlex()
	header.SetBackgroundColor(tuiSurface)
	headerViews := [3]*tview.TextView{tuiTextView(), tuiTextView(), tuiTextView()}
	for _, view := range headerViews {
		view.SetWrap(false)
	}
	tabs := tview.NewFlex()
	tabs.SetBackgroundColor(tuiSurface)
	tabRule := tuiTextView().SetWrap(false)
	tabNames := []string{"Overview", "Machines", "Datastores", "Metrics", "Settings"}
	tabButtons := make([]*tview.Button, len(tabNames))
	tabWidths := make([]int, len(tabNames))
	info := tuiTextView().SetScrollable(true).SetWrap(false)
	list := tview.NewTable().SetSelectable(true, false).SetFixed(1, 1).
		SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tuiSelect))
	list.SetBackgroundColor(tuiSurface)
	list.SetBorderPadding(0, 0, 2, 2)
	tablePane := tuiTablePane(list)
	info.SetBorderPadding(1, 0, 0, 0)
	detail := tuiTextView().SetScrollable(true)
	detail.SetBorderPadding(1, 0, 1, 1)
	detailPanel := tview.NewFlex().SetDirection(tview.FlexRow)
	detailPanel.SetBackgroundColor(tuiSurface)
	detailTitle := tuiTextView().SetWrap(false)
	backButton := tview.NewButton(lang.Text(" Esc  Back ")).SetStyle(tcell.StyleDefault.Foreground(tuiAccent).Background(tuiHeader))
	detailHeading := tview.NewFlex().AddItem(backButton, 14, 0, false).AddItem(tview.NewBox().SetBackgroundColor(tuiSurface), 3, 0, false).AddItem(detailTitle, 0, 1, false)
	vmCharts := tuiTextView().SetWrap(false).SetScrollable(true)
	preview := newTUIPreview(lang)
	var lastScreen tcell.Screen
	defer func() { preview.clear(lastScreen) }()
	detailGroup := 0
	detailGroups := []string{"Overview", "Resources", "Network adapters", "Disks", "VM options", "Other devices"}
	split := tview.NewFlex()
	divider := tuiTextView().SetWrap(false)
	panel := tview.NewFlex().SetDirection(tview.FlexRow)
	panel.SetBackgroundColor(tuiSurface)
	split.SetBackgroundColor(tuiSurface)
	filter := tview.NewInputField().SetLabel(lang.Text(" Filter: ")).
		SetFieldBackgroundColor(tuiRail).SetFieldTextColor(tuiText)
	footer := tview.NewFlex().SetDirection(tview.FlexRow)
	footerLine := tview.NewFlex()
	notice := tuiTextView().SetWrap(false)
	notice.SetBackgroundColor(tuiRail)
	profile := tview.NewButton("").SetStyle(tcell.StyleDefault.Foreground(tuiAccent).Background(tuiRail))
	repository := tview.NewButton(tuiGitHubIcon).SetStyle(tcell.StyleDefault.Foreground(tuiAccent).Background(tuiRail))
	status := tuiTextView().SetWrap(false)
	status.SetBackgroundColor(tuiRail)
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 8, 0, false).AddItem(tabs, 1, 0, false).AddItem(tabRule, 1, 0, false).
		AddItem(panel, 0, 1, true).AddItem(footer, 1, 0, false)
	layout.Box = tview.NewBox().SetBackgroundColor(tuiSurface)
	pages.AddPage("main", layout, true, true)
	page, width, height := 1, 100, 35
	settingsPreviousPage := 1
	if current.Name == "" {
		page = 5
	}
	var settings *tuiSettings
	narrowDetail := false
	filterVisible := false
	var datastore, directory, trendVM string
	var selectedFile esxi.DSFile
	var browseHistory []tuiBrowseState
	var browseRestore *tuiBrowseState
	var browseUp func()
	var browseEnter func(string, string)
	counterEntity, counterVM := "host", ""
	var data, headerData tuiData
	var generation int
	loading := false
	var switchingTarget *target.Target
	var retrySwitchTarget *target.Target
	var statusMessage string
	connected := false
	profileHovered := false
	repositoryHovered := false
	footerHeight := 1
	var requestCancel context.CancelFunc
	var activeClient *esxi.Client
	var activeTarget target.Target
	var dropClient atomic.Bool
	sessionTargets := map[string]target.Target{}
	var legacyTarget *target.Target
	if !current.Saved() && current.Name != "" {
		initial := current
		legacyTarget = &initial
	} else if current.HasPassword() {
		sessionTargets[current.Name] = current
	}

	requests := make(chan func(), 1)
	updates := make(chan func(), 1)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		defer func() {
			if activeClient != nil {
				target.Close(activeClient)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-requests:
				job()
			}
		}
	}()
	var load func(target.Target, bool)
	var pendingEdit *esxi.VMChange
	var pendingPower string
	var selectedVM, editAfterLoad, displayedDetail string
	var screenshotID, screenshotWarning string
	var screenshotTarget target.Target
	var screenshot *tuiScreenshot
	refreshScreenshot := false
	showTrends := false
	vmDetailOpen := false
	vmListOffsetRow, vmListOffsetColumn := 0, 0
	var openVMDetails func()
	needsScreenshot := func() bool {
		return data.vmDetail != nil && data.vmDetail.ID == selectedVM && data.vmDetail.PowerState == "poweredOn" && (screenshotID != selectedVM || screenshotTarget != current)
	}
	editVM := func(vm esxi.VMInfo) {
		previous := tview.Primitive(list)
		if vmDetailOpen {
			previous = detail
		}
		openTUIEditor(app, pages, previous, current.Name, vm, data.host.Portgroups, lang, func(patch esxi.VMChange) {
			trendVM = vm.ID
			pendingEdit = &patch
			load(current, false)
		})
	}
	renderChrome := func() {
		headerHeight := tuiHeaderHeight(height)
		if height < 32 {
			brand.SetText("\n " + strings.ReplaceAll(govmc.WordmarkCompact, "\n", "\n "))
		} else {
			brand.SetText(tuiWordmark())
		}
		layout.ResizeItem(header, headerHeight, 0)
		header.Clear()
		brandWidth := 35
		if height < 32 {
			brandWidth = 27
		}
		// Keep the complete summary readable instead of squeezing three columns
		// beside a clipped logo on narrow terminals.
		contentWidth := width
		if width >= 120 {
			header.AddItem(brand, brandWidth, 0, false)
			header.AddItem(tview.NewBox().SetBackgroundColor(tuiSurface), 3, 0, false)
			contentWidth -= brandWidth + 3
		}
		columns := min(3, max(1, (contentWidth+2)/27))
		headerText := tuiHeaderText(headerData, lang, height < 32)
		for i := range columns {
			if i > 0 {
				rows := headerHeight - 2
				if height < 32 {
					rows = 3
				}
				header.AddItem(tuiTextView().SetText("\n"+strings.Repeat(tuiColor("#38546a", "│")+"\n", rows)), 2, 0, false)
			}
			text := headerText[i]
			if height < 32 {
				text = strings.Join(strings.Split(text, "\n")[:min(3, len(strings.Split(text, "\n")))], "\n")
			}
			headerViews[i].SetText("\n" + text)
			header.AddItem(headerViews[i], 0, 1, false)
		}
		var rule strings.Builder
		remaining := width
		for i, button := range tabButtons {
			label := " " + lang.Text(tabNames[i]) + " "
			button.SetLabel(label)
			tabWidths[i] = tview.TaggedStringWidth(label)
			tabs.ResizeItem(button, tabWidths[i], 0)
			style := tcell.StyleDefault.Foreground(tuiMuted).Background(tuiSurface)
			if i+1 == page {
				style = style.Foreground(tuiText)
			}
			button.SetStyle(style)
			color := "#43474b"
			if i+1 == page {
				color = "#59a1fa"
			}
			span := min(max(0, remaining), tabWidths[i])
			rule.WriteString(tuiColor(color, strings.Repeat("─", span)))
			remaining -= span
		}
		rule.WriteString(tuiColor("#43474b", strings.Repeat("─", max(0, remaining))))
		tabRule.SetText(rule.String())
		footer.Clear()
		hint := lang.Text(" q quit   r refresh   f filter   ? help   u environment")
		if page == 2 {
			hint = lang.Text(" Enter details · e edit · p power · f filter")
			if vmDetailOpen {
				hint = lang.Text(" Esc back · [/] tabs · s screenshot · e edit")
			}
		}
		if page == 5 {
			hint = lang.Text(" Esc back · [/] tabs · r refresh")
			if settings.module >= 3 {
				hint = lang.Text(" Esc back · [/] tabs · r refresh · f filter")
			}
		}
		if page == 3 {
			hint = lang.Text(" Enter browse · f filter · r refresh")
			if datastore != "" {
				hint = lang.Text(" Enter open · c copy · m move · Esc up · f filter")
			}
		}
		// Transient feedback has its own row; action hints never disappear.
		footerLine.Clear()
		footerHeight = 1
		if statusMessage != "" && !loading {
			notice.SetText(" " + tview.Escape(statusMessage))
			footer.AddItem(notice, 1, 0, false)
			footerHeight++
		}
		layout.ResizeItem(footer, footerHeight, 0)
		status.SetText(tuiShortcutHint(hint))
		footerLine.AddItem(status, 0, 1, false)
		shownTarget := current
		if switchingTarget != nil {
			shownTarget = *switchingTarget
		}
		label := tuiFooterProfileLabel(shownTarget, connected, loading, lang)
		profile.SetLabel(label)
		style := tcell.StyleDefault.Foreground(tuiText).Background(tuiRail)
		if profileHovered {
			style = style.Background(tuiSelect).Underline(true)
		}
		profile.SetStyle(style)
		profile.SetActivatedStyle(style.Background(tuiSelect))
		footerLine.AddItem(profile, min(max(18, width/2), tview.TaggedStringWidth(label)+2), 0, false)
		versionLabel := tuiDisplayVersion(govmc.Version)
		version := tuiTextView().SetWrap(false).SetText(" " + tview.Escape(versionLabel) + " ").SetTextColor(tuiAccent)
		version.SetBackgroundColor(tuiRail)
		footerLine.AddItem(version, tview.TaggedStringWidth(versionLabel)+2, 0, false)
		repositoryStyle := tcell.StyleDefault.Foreground(tuiAccent).Background(tuiRail)
		if repositoryHovered {
			repositoryStyle = repositoryStyle.Foreground(tcell.ColorWhite).Underline(true)
		}
		repository.SetStyle(repositoryStyle).SetActivatedStyle(repositoryStyle.Foreground(tcell.ColorWhite).Underline(true))
		footerLine.AddItem(repository, 3, 0, false)
		footer.AddItem(footerLine, 1, 0, false)
	}
	var render func()
	render = func() {
		renderChrome()
		panel.Clear()
		if page == 5 {
			settings.width = width
			settings.Render()
			panel.AddItem(settings.root, 0, 1, true)
			return
		}
		if page == 1 {
			panel.AddItem(info, 0, 1, true)
			headerHeight := tuiHeaderHeight(height)
			bodyHeight := height - headerHeight - 2 - footerHeight
			warning := data.metricWarning
			if data.overviewWarning != "" {
				warning = strings.TrimSpace(warning + " " + data.overviewWarning)
			}
			info.SetText(tuiTrends(data.metrics, warning, width, bodyHeight-1, lang))
			return
		}
		if page == 4 {
			entity := data.counterTarget
			if entity == "" {
				entity = lang.Text("Host")
			}
			if counterEntity == "vm" && data.counterTarget == "" {
				entity = lang.Text("Virtual machines")
			}
			caption := tuiTextView().SetText("  " + tuiColor("#f7b65c", lang.Text("Live metrics")) + "  " + entity + "  " + tuiColor("#9ba7b0", lang.Text("g Host / VM · Enter Choose VM · auto refresh")))
			panel.AddItem(caption, 1, 0, false)
		}
		if page == 3 && datastore != "" {
			up := tview.NewButton(lang.Text(" Esc  Up ")).SetStyle(tcell.StyleDefault.Foreground(tuiAccent).Background(tuiHeader))
			up.SetSelectedFunc(func() { browseUp() })
			breadcrumb := tuiTextView().SetWrap(false).SetText("  " + tview.Escape("["+datastore+"] /"+directory))
			navigation := tview.NewFlex().AddItem(up, 12, 0, false).AddItem(breadcrumb, 0, 1, false)
			panel.AddItem(navigation, 1, 0, false)
		}
		if (filterVisible || filter.GetText() != "") && !(page == 2 && vmDetailOpen) {
			panel.AddItem(filter, 1, 0, false)
		}
		panel.AddItem(split, 0, 1, true)
		split.Clear()
		tableWidth := width
		headerHeight := tuiHeaderHeight(height)
		bodyHeight := height - headerHeight - 2 - footerHeight
		split.SetDirection(tview.FlexColumn)
		switch {
		case page == 2:
			if vmDetailOpen {
				split.AddItem(detailPanel, 0, 1, true)
			} else {
				split.AddItem(tablePane, 0, 1, true)
			}
		case width < 85:
			if narrowDetail {
				split.AddItem(detailPanel, 0, 1, true)
			} else {
				split.AddItem(tablePane, 0, 1, true)
			}
		default:
			tableWidth = (width - 2) * 3 / 5
			if page == 3 {
				tableWidth = (width - 2) * 2 / 3
			}
			divider.SetText(strings.Repeat(tuiColor("#38546a", "│")+"\n", height))
			split.AddItem(tablePane, tableWidth, 0, true).AddItem(divider, 2, 0, false).AddItem(detailPanel, 0, 1, false)
		}
		detailPanel.Clear()
		inspectorWidth := width - 2
		if page == 2 && vmDetailOpen {
			detailPanel.AddItem(tview.NewBox().SetBackgroundColor(tuiSurface), 1, 0, false)
			detailPanel.AddItem(detailHeading, 1, 0, false)
			detailPanel.AddItem(tview.NewBox().SetBackgroundColor(tuiSurface), 1, 0, false)
			var tabRow *tview.Flex
			used := 0
			for index, name := range detailGroups {
				label := " " + lang.Text(name) + " "
				span := tview.TaggedStringWidth(tview.Escape(label))
				if tabRow == nil || used+span > inspectorWidth {
					tabRow = tview.NewFlex()
					tabRow.SetBackgroundColor(tuiSurface)
					detailPanel.AddItem(tabRow, 1, 0, false)
					used = 0
				}
				button := tview.NewButton(label).SetSelectedFunc(func() {
					detailGroup = index
					render()
					app.SetFocus(detail)
					if index == 0 && !loading && needsScreenshot() {
						load(current, false)
					}
				})
				style := tcell.StyleDefault.Foreground(tuiMuted).Background(tuiSurface)
				if index == detailGroup {
					style = style.Foreground(tuiText)
				}
				button.SetStyle(style)
				tabRow.AddItem(button, span, 0, false)
				used += span
			}
		}
		graphHeight := min(14, max(8, bodyHeight/3))
		detailWidth := width - 2
		if page == 2 && vmDetailOpen {
			if detailGroup == 0 {
				top := tview.NewFlex()
				top.SetBackgroundColor(tuiSurface)
				detailWidth = max(28, (width-2)*2/5)
				top.AddItem(detail, detailWidth, 0, true).
					AddItem(tview.NewBox().SetBackgroundColor(tuiSurface), 2, 0, false).
					AddItem(preview, 0, 1, false)
				top.SetBorderPadding(1, 1, 0, 2)
				detailPanel.AddItem(top, 0, 1, true)
			} else {
				detailPanel.AddItem(detail, 0, 1, true)
			}
			detailPanel.AddItem(vmCharts, graphHeight, 0, false)
		} else {
			detailPanel.AddItem(detail, 0, 1, true)
		}
		selectedRow, _ := list.GetSelection()
		rowOffset, columnOffset := list.GetOffset()
		previousVM := selectedVM
		list.Clear()
		var rows [][]string
		var descriptions, rowIDs []string
		var counterRows []esxi.CounterInfo
		var storeRows []esxi.DatastoreInfo
		var fileRows []esxi.DSFile
		query := strings.ToLower(filter.GetText())
		switch {
		case page == 2:
			labels := []string{lang.Text("Machines"), lang.Text("Power"), lang.Text("Specifications"), "CPU", lang.Text("Active %"), lang.Text("Network (Mbps)"), lang.Text("Disk used / cap (GiB)"), "MAC", lang.Text("Operating system")}
			rows = append(rows, labels)
			for _, vm := range data.vms {
				search := vm.Name + " " + vm.ID + " " + tuiOSName(vm, lang) + " " + lang.Power(vm.PowerState)
				for _, nic := range vm.NICs {
					search += " " + nic.MAC + " " + nic.Portgroup
				}
				if !strings.Contains(strings.ToLower(search), query) {
					continue
				}
				mac := "—"
				if len(vm.NICs) > 0 {
					mac = vm.NICs[0].MAC
					if len(vm.NICs) > 1 {
						mac += fmt.Sprintf(" +%d", len(vm.NICs)-1)
					}
				}
				row := []string{vm.Name, tuiTablePower(vm.PowerState, lang), fmt.Sprintf("%d vCPU / %.1f GiB", vm.NumCPU, float64(vm.MemoryMB)/1024),
					tuiNumber(vm.CPUPct, "%"), tuiNumber(vm.MemActivePct, "%"), tuiNetworkRates(vm), tuiTableDisk(vm), mac, tuiOSName(vm, lang)}
				rows = append(rows, row)
				rowIDs = append(rowIDs, vm.ID)
				descriptions = append(descriptions, tuiInspector(vm.Name, [][2]string{
					{"ID", vm.ID}, {lang.Text("Power"), lang.Power(vm.PowerState)},
					{lang.Text("Operating system"), vm.GuestOS}, {"vCPU", fmt.Sprint(vm.NumCPU)},
					{lang.Text("Memory"), fmt.Sprintf("%.1f GiB", float64(vm.MemoryMB)/1024)},
					{"IP", vm.Guest.IPAddress}, {lang.Text("Tools"), tuiTools(vm.Guest, lang)},
				}, width))
			}
		case page == 4:
			rows = append(rows, []string{lang.Text("Counter"), lang.Text("Latest"), lang.Text("Unit"), lang.Text("Sampled")})
			for _, counter := range data.counters {
				if !strings.Contains(strings.ToLower(counter.Name+" "+counter.Description), query) {
					continue
				}
				value, unit, sampled := tuiCounterLatest(counter, data.metrics)
				rows = append(rows, []string{counter.Name, value, unit, sampled})
				descriptions = append(descriptions, counter.Name)
				counterRows = append(counterRows, counter)
			}
		case datastore == "":
			rows = append(rows, []string{lang.Text("Datastore"), lang.Text("Type"), lang.Text("Total GiB"), lang.Text("Used GiB"), lang.Text("Free GiB"), lang.Text("Used %"), "VMs"})
			for _, ds := range data.stores {
				if !strings.Contains(strings.ToLower(ds.Name+" "+ds.Type), query) {
					continue
				}
				rows = append(rows, tuiDatastoreRow(ds))
				storeRows = append(storeRows, ds)
				descriptions = append(descriptions, tuiDatastoreDetails(ds, max(24, width-tableWidth-3), lang))
			}
		default:
			rows = append(rows, []string{lang.Text("Name"), lang.Text("Type"), lang.Text("Size"), lang.Text("Modified")})
			rows = append(rows, []string{"[UP] ..", lang.Text("Parent"), "—", "—"})
			fileRows = append(fileRows, esxi.DSFile{Path: "..", Folder: true})
			descriptions = append(descriptions, tuiColor("#79c6f3", lang.Text("Return to parent directory")))
			for _, file := range tuiSortedFiles(data.files) {
				if !strings.Contains(strings.ToLower(file.Path+" "+tuiFileKind(file, lang)), query) {
					continue
				}
				label := "[FILE] " + file.Path
				if file.Folder {
					label = "[DIR]  " + strings.TrimSuffix(file.Path, "/") + "/"
				}
				rows = append(rows, []string{label, tuiFileKind(file, lang), tuiFileSize(file), tuiFileTime(file.Modified, false)})
				fileRows = append(fileRows, file)
				descriptions = append(descriptions, tuiFileDetails(file, datastore, directory, max(24, width-tableWidth-3), lang))
			}
		}
		show := func(row, _ int) {
			selectedVM = ""
			selectedFile = esxi.DSFile{}
			if page == 3 && datastore != "" && row > 0 && row <= len(fileRows) && fileRows[row-1].Path != ".." {
				selectedFile = fileRows[row-1]
			}
			if page != 2 || row <= 0 || row > len(descriptions) {
				preview.setFrame(nil, lang.Text("Select a VM to view its screenshot"))
			}
			if row > 0 && row <= len(descriptions) {
				text := descriptions[row-1]
				if page == 4 {

					dw := width
					if width >= 85 {
						dw = width - tableWidth - 3
					}
					text = tuiCounterDetails(counterRows[row-1], data.metrics, data.metricWarning, dw, lang)
				}
				if page == 2 {
					selectedVM = rowIDs[row-1]
					if data.vmDetail == nil || data.vmDetail.ID != selectedVM {
						preview.setFrame(nil, lang.Text("Loading screenshot…"))
					}
					if data.vmDetail != nil && data.vmDetail.ID == selectedVM {
						switch {
						case data.vmDetail.PowerState != "poweredOn":
							preview.setFrame(nil, lang.Text("Power on the VM to view its screenshot"))
						case screenshotID == selectedVM && screenshotTarget == current:
							preview.setFrame(screenshot, screenshotWarning)
						default:
							preview.setFrame(nil, lang.Text("Loading screenshot…"))
						}
						if loading && screenshotID == selectedVM && screenshotTarget == current {
							preview.setFrame(screenshot, lang.Text("Refreshing screenshot…"))
						}
					}
					if vmDetailOpen {
						detailTitle.SetText(" " + tuiColor("#79c6f3", rows[row][0]) + " " + tuiColor("#9ba7b0", rowIDs[row-1]))
						if data.vmDetail != nil && data.vmDetail.ID == rowIDs[row-1] {
							if detailGroup == 0 {
								text = tuiVMSummary(*data.vmDetail, lang, detailWidth-2)
							} else {
								text = tuiVMDetails(*data.vmDetail, lang, detailWidth-2, detailGroup)
							}
						} else {
							text = lang.Text("Loading…") + "\n" + data.detailWarning
						}
						var metrics *esxi.MetricsResponse
						warning := lang.Text("Loading…")
						if data.trendVM == rowIDs[row-1] {
							metrics, warning = data.metrics, data.metricWarning
						}
						vmCharts.SetText(tuiCompactTrends(metrics, warning, width, graphHeight, lang))
					}
				}
				key := fmt.Sprintf("%d/%s/%s/%d/%t", page, datastore, directory, row, showTrends)
				if page == 2 {
					key = fmt.Sprintf("%s/%t/%d", rowIDs[row-1], showTrends, detailGroup)
				}
				scrollRow, scrollColumn := detail.GetScrollOffset()
				detail.SetText(text)
				if displayedDetail == key {
					detail.ScrollTo(scrollRow, scrollColumn)
				} else {
					detail.ScrollToBeginning()
					displayedDetail = key
				}
			}
		}
		list.SetSelectionChangedFunc(show)
		for y, row := range rows {
			for x, v := range row {
				// Full identity and guest OS remain available in the inspector.
				// Avoid a sliver of a MAC/OS column beside the core metrics.
				if page == 2 && tableWidth < 180 && x >= 7 {
					break
				}
				background, foreground := tuiSurface, tuiText
				if y > 0 && page == 2 {
					switch x {
					case 0:
						foreground = tuiAccent
					case 1:
						foreground = tuiMuted
						if v == tuiTablePower("poweredOn", lang) {
							foreground = tcell.NewHexColor(0x67c23a)
						}
						if v == tuiTablePower("suspended", lang) {
							foreground = tcell.NewHexColor(0xf7b65c)
						}
					case 3:
						foreground = tuiMetricColor(v, 0x79c6f3)
					case 4:
						foreground = tuiMetricColor(v, 0xb699ef)
					case 6:
						foreground = tcell.NewHexColor(0xd9bc85)
					case 7, 8:
						foreground = tuiMuted
					}
				}
				if y == 0 {
					background, foreground = tuiHeader, tuiAccent
				} else if y%2 == 0 {
					background = tuiStripe
				}
				value := tview.Escape(v)
				if page == 2 && y > 0 && x == 5 {
					value = v
				}
				cell := tview.NewTableCell(value).SetTextColor(foreground).SetBackgroundColor(background).
					SetExpansion(0).SetSelectable(y > 0)
				if page == 2 {
					widths := []int{18, 9, 18, 6, 9, 17, 21, 22, 32}
					numeric := x == 3 || x == 4 || x == 6
					cell.Text = tuiTableCell(value, widths[x], numeric)
					cell.SetMaxWidth(widths[x] + 2)
				} else if page == 3 {
					widths := []int{16, 5, 9, 9, 9, 7, 4}
					numeric := x >= 2
					if datastore != "" {
						widths = []int{max(16, tableWidth-53), 12, 10, 16}
						numeric = x == 2
					}
					cell.Text = tuiTableCell(value, widths[x], numeric)
					cell.SetMaxWidth(widths[x] + 2)
					if y > 0 {
						if datastore == "" {
							if x == 5 {
								cell.SetTextColor(tuiMetricColor(v, 0x79c6f3))
							}
							if x == 0 && y <= len(storeRows) {
								ds := storeRows[y-1]
								if ds.Accessible != nil && !*ds.Accessible {
									cell.SetTextColor(tcell.NewHexColor(0xf07878))
								}
							}
						} else if y <= len(fileRows) {
							file := fileRows[y-1]
							if x == 0 && file.Folder {
								cell.SetTextColor(tuiAccent)
							}
							if x == 0 && !file.Folder && strings.HasSuffix(strings.ToLower(file.Path), ".vmdk") {
								cell.SetTextColor(tcell.NewHexColor(0xd9bc85))
							}
							if x == 3 {
								cell.SetTextColor(tuiMuted)
							}
						}
					}
				} else if page == 4 {
					widths := []int{29, 11, 9, 8}
					cell.Text = tuiTableCell(value, widths[x], x == 1)
					cell.SetMaxWidth(widths[x] + 2)
					if x == 1 && y > 0 {
						cell.SetTextColor(tuiAccent)
					}
				} else if x == 0 {
					cell.SetExpansion(2).SetMaxWidth(max(12, tableWidth/3))
				}
				list.SetCell(y, x, cell)
			}
		}
		if len(rows) <= 1 {
			selectedVM = ""
			if page == 4 {
				detail.SetText(data.metricWarning)
			} else {
				detail.SetText("")
			}
			displayedDetail = ""
			list.SetCell(1, 0, tview.NewTableCell(lang.Text("No matching entries")).SetTextColor(tuiMuted).SetSelectable(false))
		}
		if len(rows) > 1 {
			for i, id := range rowIDs {
				if id == previousVM {
					selectedRow = i + 1
					break
				}
			}
			if selectedRow < 1 || selectedRow >= len(rows) {
				selectedRow = 1
			}
			list.Select(selectedRow, 0)
			list.SetOffset(rowOffset, columnOffset)
			show(selectedRow, 0)
		}
		list.SetSelectedFunc(func(row, _ int) {
			if row <= 0 || row >= len(rows) {
				return
			}
			if page == 2 {
				openVMDetails()
				return
			}
			if page == 4 {
				if counterEntity == "host" {
					load(current, false)
					return
				}
				picker := tview.NewList().ShowSecondaryText(false).SetMainTextColor(tuiText).SetSelectedBackgroundColor(tuiSelect)
				picker.SetBackgroundColor(tuiHeader)
				back := func() { pages.RemovePage("counter-target"); app.SetFocus(list) }
				choose := func(id string) { back(); counterVM = id; load(current, false) }

				if counterEntity == "vm" {
					for _, vm := range data.vms {
						picker.AddItem(vm.Name+" ("+vm.ID+")", "", 0, func() { choose(vm.ID) })
					}
				}
				picker.AddItem(lang.Text("Cancel"), "", 0, back).SetDoneFunc(back)
				pages.AddPage("counter-target", tuiProfileDialog(picker, lang.Text("Sampling target"), lang.Text("Choose a VM to inspect its live metrics"), 80, 20), true, true)
				app.SetFocus(picker)
				return
			}
			if page != 3 {
				return
			}
			if datastore == "" {
				selected := storeRows[row-1]
				if selected.Accessible != nil && !*selected.Accessible {
					statusMessage = lang.Text("Datastore is inaccessible")
					renderChrome()
					return
				}
				browseEnter(selected.Name, "")
			} else {
				file := fileRows[row-1]
				if file.Path == ".." {
					browseUp()
				} else if file.Folder {
					browseEnter(datastore, path.Join(directory, file.Path))
				} else {
					if width < 85 {
						narrowDetail = true
						render()
					}
					app.SetFocus(detail)
				}
			}
		})
	}
	browseState := func() tuiBrowseState {
		selected, _ := list.GetSelection()
		offsetRow, offsetColumn := list.GetOffset()
		return tuiBrowseState{datastore: datastore, directory: directory, filter: filter.GetText(), selection: selected, offsetRow: offsetRow, offsetColumn: offsetColumn, filterVisible: filterVisible}
	}
	navigateBrowse := func(state tuiBrowseState) {
		data.files, data.stores = nil, nil
		datastore, directory = state.datastore, state.directory
		narrowDetail = false
		filterVisible = state.filterVisible
		browseRestore = &state
		list.Select(0, 0)
		list.SetOffset(0, 0)
		filter.SetText(state.filter)
		render()
		app.SetFocus(list)
		load(current, false)
	}
	browseEnter = func(store, dir string) {
		browseHistory = append(browseHistory, browseState())
		navigateBrowse(tuiBrowseState{datastore: store, directory: dir, selection: 2})
	}
	browseUp = func() {
		if datastore == "" {
			return
		}
		if len(browseHistory) > 0 {
			state := browseHistory[len(browseHistory)-1]
			browseHistory = browseHistory[:len(browseHistory)-1]
			navigateBrowse(state)
			return
		}
		state := tuiBrowseState{datastore: datastore, selection: 1}
		if directory == "" {
			state.datastore = ""
		} else {
			state.directory = path.Dir(directory)
			if state.directory == "." {
				state.directory = ""
			}
		}
		navigateBrowse(state)
	}
	openVMDetails = func() {
		if selectedVM == "" {
			return
		}
		if !vmDetailOpen {
			vmListOffsetRow, vmListOffsetColumn = list.GetOffset()
		}
		vmDetailOpen = true
		trendVM, detailGroup = selectedVM, 0
		showTrends = false
		render()
		app.SetFocus(detail)
		load(current, false)
	}
	backToVMList := func() {
		vmDetailOpen = false
		trendVM = ""
		render()
		list.SetOffset(vmListOffsetRow, vmListOffsetColumn)
		app.SetFocus(list)
	}
	backButton.SetSelectedFunc(backToVMList)
	discardClient := func() {
		if dropClient.Swap(false) {
			if activeClient != nil {
				target.Close(activeClient)
			}
			activeClient = nil
			activeTarget = target.Target{}
		}
	}
	load = func(next target.Target, switching bool) {
		if page == 5 && !switching && switchingTarget == nil {
			render()
			return
		}
		// Navigation and explicit refresh must follow a pending switch. The
		// displayed current target is committed only after connection succeeds.
		if switching {
			requested := next
			switchingTarget = &requested
			retrySwitchTarget = nil
		} else if switchingTarget != nil {
			next, switching = *switchingTarget, true
		}
		if next.Name == "" {
			return
		}
		if requestCancel != nil {
			requestCancel()
		}
		generation++
		id := generation
		patch, editID, powerAction := pendingEdit, editAfterLoad, pendingPower
		pendingEdit, editAfterLoad, pendingPower = nil, "", ""
		req := tuiRequest{target: next, page: page, datastore: datastore, path: directory, vmID: trendVM, counterEntity: counterEntity, counterVM: counterVM}
		req.screenshot = editID == "" && patch == nil && powerAction == "" && page == 2 && trendVM != "" && !showTrends && detailGroup == 0 && (refreshScreenshot || screenshotID != trendVM || screenshotTarget != next)
		refreshScreenshot = false
		if switching {
			req.datastore, req.path, req.vmID = "", "", ""
			req.screenshot = false
			req.counterVM = ""
		}
		jobCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		requestCancel = stop
		loading = true
		statusMessage = ""
		renderChrome()
		job := func() {
			defer stop()
			discardClient()
			c := activeClient
			newClient := c == nil || activeTarget != req.target
			var err error
			if newClient {
				c = nil
				err = journal.Run(jobCtx, journal.Event{Source: "tui", Profile: req.target.Name, Operation: "profile.connect"}, func() error { var dialErr error; c, dialErr = req.target.Dial(jobCtx); return dialErr })
			}
			if err != nil {
				store.Log(journal.Event{Source: "tui", Event: "connection.failed", Profile: req.target.Name, Level: "ERROR", Code: journal.ErrorCode(err)})
			}
			var nextData tuiData
			var editErr error
			if powerAction != "" {
				editErr = err
				if editErr == nil {
					editErr = journal.Run(jobCtx, journal.Event{Source: "tui", Profile: req.target.Name, Operation: "vm.power." + powerAction, Resource: req.vmID}, func() error { return c.Power(jobCtx, req.vmID, powerAction, true) })
				}
			}
			if patch != nil {
				editErr = err
				if editErr == nil {
					editErr = journal.Run(jobCtx, journal.Event{Source: "tui", Profile: req.target.Name, Operation: "vm.edit", Resource: req.vmID}, func() error { return c.ChangeVM(jobCtx, req.vmID, *patch, true) })
				}
			}
			if err == nil {
				readCtx := jobCtx
				stopRead := func() {}
				if powerAction != "" && editErr != nil && jobCtx.Err() != nil {
					readCtx, stopRead = context.WithTimeout(ctx, 8*time.Second)
				}
				nextData, err = loadTUI(readCtx, c, req, lang)
				stopRead()
			}
			if err == nil && jobCtx.Err() != nil && powerAction == "" {
				err = jobCtx.Err()
			}
			var saveDefaultErr error
			if switching && err == nil && req.target.Saved() {
				saveDefaultErr = target.Edit(jobCtx, configPath, func(config *target.Config) error {
					p, exists := config.Profiles[req.target.Name]
					if !exists || p != req.target.Profile {
						return fmt.Errorf("environment changed during connection; default was not updated")
					}
					config.Active = req.target.Name
					return nil
				})
			}
			if newClient && c != nil {
				if err == nil {
					if activeClient != nil {
						target.Close(activeClient)
					}
					activeClient, activeTarget = c, req.target
				} else {
					target.Close(c)
				}
			}
			if ctx.Err() != nil {
				return
			}
			update := func() {
				if generation != id {
					return
				}
				loading = false
				if switching {
					switchingTarget = nil
				}
				if pages.HasPage("saving") {
					pages.RemovePage("saving")
				}
				if patch != nil || powerAction != "" {
					if vmDetailOpen {
						app.SetFocus(detail)
					} else {
						app.SetFocus(list)
					}
				}
				if err != nil {
					if !switching || req.target == current {
						connected = false
					}
					if switching {
						delete(sessionTargets, req.target.Name)
						failed := req.target
						retrySwitchTarget = &failed
					}
					statusMessage = err.Error() + lang.Text("   r retry")
					if patch != nil && editErr == nil {
						statusMessage = lang.Text("Saved, but refresh failed") + " · " + err.Error()
					}
					renderChrome()
					return
				}
				if switching {
					vmDetailOpen = false
					selectedVM, displayedDetail = "", ""
					vmListOffsetRow, vmListOffsetColumn = 0, 0
					list.Select(0, 0)
					list.SetOffset(0, 0)
					browseHistory = nil
					browseRestore = nil
					current = next
					if next.Saved() {
						sessionTargets[next.Name] = next
					}
					datastore, directory, trendVM = "", "", ""
					counterVM = ""
				}
				connected = true
				data = nextData
				if req.page == 4 {
					counterVM = data.counterVM
				}
				if switching {
					screenshotID, screenshotWarning = "", ""
					screenshot = nil
					detailGroup = 0
				}
				if req.screenshot {
					if screenshotTarget != req.target || screenshotID != req.vmID {
						screenshot = nil
					}
					screenshotID, screenshotTarget, screenshotWarning = req.vmID, req.target, data.screenshotWarning
					if data.screenshot != nil {
						screenshot = data.screenshot
					}
					if data.vmDetail != nil && data.vmDetail.PowerState != "poweredOn" {
						screenshot = nil
					}
				}
				if data.vmDetail != nil && data.vmDetail.PowerState != "poweredOn" && screenshotID == data.vmDetail.ID && screenshotTarget == current {
					screenshot = nil
					screenshotID, screenshotWarning = "", ""
				}
				if switching {
					headerData = tuiData{}
				}
				headerData.host, headerData.about = data.host, data.about
				if data.vms != nil {
					headerData.vms = data.vms
				}
				if data.stores != nil {
					headerData.stores = data.stores
				}
				statusMessage = ""
				if switching {
					_ = journal.Run(ctx, journal.Event{Source: "tui", Profile: current.Name, Operation: "profile.switch"}, func() error { return nil })
					statusMessage = fmt.Sprintf(lang.Text("Switched to %s"), current.Name)
				}
				if saveDefaultErr != nil {
					statusMessage = lang.Text("Connected, but default environment was not saved") + " · " + saveDefaultErr.Error()
				}
				if req.page == 4 && data.metricWarning != "" {
					statusMessage = strings.TrimSpace(statusMessage + " " + data.metricWarning)
				}
				if powerAction != "" {
					statusMessage = lang.Text("Power action completed")
					if editErr != nil {
						statusMessage = lang.Text("Power request failed; actual state refreshed") + " · " + editErr.Error()
					}
				}
				if patch != nil {
					statusMessage = lang.Text("Configuration saved")
					if editErr != nil {
						statusMessage = editErr.Error()
					}
				}
				if switching {
					filterVisible = false
					filter.SetText("")
				}
				render()
				if front, _ := pages.GetFrontPage(); page == 5 && front == "main" {
					app.SetFocus(settings.root)
				}
				if req.page == 3 && browseRestore != nil {
					state := browseRestore
					if req.datastore == state.datastore && req.path == state.directory {
						list.Select(min(max(1, state.selection), max(1, list.GetRowCount()-1)), 0)
						list.SetOffset(state.offsetRow, state.offsetColumn)
						browseRestore = nil
					}
				}
				frontPage, _ := pages.GetFrontPage()
				if frontPage == "main" && editID == "" && page == 2 && vmDetailOpen && !showTrends && detailGroup == 0 && data.vmDetail != nil && data.vmDetail.ID == selectedVM && data.vmDetail.PowerState == "poweredOn" && (screenshotID != selectedVM || screenshotTarget != current) {
					load(current, false)
				}
				if editID != "" && editID == selectedVM {
					if data.vmDetail != nil && data.vmDetail.ID == editID {
						editVM(*data.vmDetail)
					} else {
						statusMessage = data.detailWarning
						renderChrome()
					}
				}
			}
			select {
			case <-updates:
			default:
			}
			updates <- update
			app.QueueEvent(tcell.NewEventKey(tcell.KeyF63, 0, tcell.ModNone))
		}
		select {
		case <-requests:
		default:
		}
		requests <- job
	}
	openProfiles := func(preferred string, action rune) {
		var onClosed func()
		if action != 0 {
			onClosed = func() {
				if settings != nil {
					settings.Render()
					app.SetFocus(settings.root)
				}
			}
		}
		openTUIProfilesAt(ctx, app, pages, configPath, current, legacyTarget, sessionTargets, lang, preferred, action, onClosed, func(next target.Target) { load(next, true) }, func(name string) {
			if current.Name != name {
				return
			}
			if requestCancel != nil {
				requestCancel()
			}
			generation++
			loading = false
			switchingTarget = nil
			retrySwitchTarget = nil
			current = target.Target{}
			connected = false
			data, headerData = tuiData{}, tuiData{}
			selectedVM, trendVM, editAfterLoad = "", "", ""
			datastore, directory = "", ""
			browseHistory = nil
			browseRestore = nil
			vmDetailOpen = false
			screenshot = nil
			screenshotID = ""
			preview.setFrame(nil, lang.Text("No screenshot"))
			preview.clear(lastScreen)
			dropClient.Store(true)
			select {
			case <-requests:
			default:
			}
			requests <- discardClient
			statusMessage = lang.Text("Profile deleted · u Choose environment")
			render()
		})
	}
	profile.SetSelectedFunc(func() { openProfiles("", 0) })
	repository.SetSelectedFunc(func() {
		if err := openTUIRepository(); err != nil {
			statusMessage = govmc.RepositoryURL + ": " + err.Error()
			renderChrome()
		}
	})
	switchPage := func(next int) {
		if next == page {
			return
		}
		if next == 5 {
			settingsPreviousPage = page
			page = 5
			render()
			app.SetFocus(settings.root)
			return
		}
		if page == 5 && next == settingsPreviousPage {
			page = next
			render()
			if page == 1 {
				app.SetFocus(info)
			} else {
				app.SetFocus(list)
			}
			load(current, false)
			return
		}
		if page == next {
			return
		}
		page = next
		vmDetailOpen = false
		list.Select(0, 0)
		list.SetOffset(0, 0)
		narrowDetail, filterVisible = false, false
		detailGroup = 0
		trendVM = ""
		data = tuiData{host: data.host, about: data.about}
		filter.SetText("")
		render()
		if page == 2 || page == 3 || page == 4 {
			app.SetFocus(list)
		} else {
			app.SetFocus(info)
		}
		load(current, false)
	}
	settings = newTUISettings(ctx, app, configPath, store, func() language { return lang }, func(next language) {
		lang = next
		backButton.SetLabel(lang.Text(" Esc  Back "))
		render()
		app.SetFocus(settings.root)
	}, func() target.Target { return current }, func() {
		profile.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	})
	settings.pages = pages

	settings.profileAction = openProfiles
	for i, name := range tabNames {
		label := " " + lang.Text(name) + " "
		tabButtons[i] = tview.NewButton(label).SetSelectedFunc(func() { switchPage(i + 1) })
		tabWidths[i] = tview.TaggedStringWidth(label)
		tabs.AddItem(tabButtons[i], tabWidths[i], 0, false)
	}
	filter.SetChangedFunc(func(_ string) {
		if page == 2 || page == 3 || page == 4 {
			render()
		}
	})
	filter.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			filter.SetText("")
		}
		filterVisible = false
		render()
		app.SetFocus(list)
	})
	transferFile := func(copy bool) {
		if page != 3 || datastore == "" || selectedFile.Path == "" || loading || !connected {
			return
		}
		src := path.Join(directory, selectedFile.Path)
		var stores []string
		for _, store := range headerData.stores {
			if store.Accessible == nil || *store.Accessible {
				stores = append(stores, store.Name)
			}
		}
		openTUIFileTransfer(ctx, app, pages, current, datastore, src, copy, lang, func(job tasks.Task) {
			settings.taskSelected = job.ID
			settings.module = 3
			switchPage(5)
			settings.showTask(job.ID)
		}, stores...)
	}
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC || event.Key() == tcell.KeyF61 {
			preview.clear(lastScreen)
			cancel()
			app.Stop()
			return nil
		}
		if event.Key() == tcell.KeyF62 {
			name, _ := pages.GetFrontPage()
			if name == "main" && !loading && app.GetFocus() != filter && (page <= 2 || page == 4) {
				load(current, false)
			}
			return nil
		}
		if event.Key() == tcell.KeyF64 {
			front, _ := pages.GetFrontPage()
			if front == "main" && page == 5 && settings.module == 3 && (app.GetFocus() == settings.table || app.GetFocus() == settings.root) {
				row, col := 0, 0
				if settings.table != nil {
					row, col = settings.table.GetOffset()
				}
				settings.Render()
				settings.table.SetOffset(row, col)
				app.SetFocus(settings.table)
			}
			if front == "task-detail" && settings.taskDetails != nil {
				if value, e := tasks.From(ctx).Get(settings.taskDetailID); e == nil {
					row, col := settings.taskDetails.GetScrollOffset()
					settings.taskDetails.SetText(tview.Escape(taskSummary(value, lang)))
					settings.taskDetails.ScrollTo(row, col)
				}
			}
			return nil
		}
		if event.Key() == tcell.KeyF63 {
			select {
			case update := <-updates:
				update()
			default:
			}
			return nil
		}
		name, _ := pages.GetFrontPage()
		if name != "main" {
			return event
		}
		if page == 5 {
			if settings.Handle(event) {
				return nil
			}
			if _, ok := app.GetFocus().(*tview.InputField); ok {
				return event
			}
			if d, ok := app.GetFocus().(*tview.DropDown); ok && (d.IsOpen() || event.Key() != tcell.KeyEscape) {
				return event
			}
			if event.Key() == tcell.KeyEscape {
				switchPage(settingsPreviousPage)
				return nil
			}
		}
		if app.GetFocus() == filter {
			return event
		}
		// Old inventory stays visible while connecting, but may not be edited
		// or powered through a switch to another environment.
		if switchingTarget != nil && (event.Rune() == 'e' || event.Rune() == 'p' || event.Rune() == 'c') {
			return nil
		}
		if page == 2 && vmDetailOpen && (event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2) {
			backToVMList()
			return nil
		}
		if page == 2 && vmDetailOpen && event.Key() == tcell.KeyTab {
			if app.GetFocus() == vmCharts {
				app.SetFocus(detail)
			} else {
				app.SetFocus(vmCharts)
			}
			return nil
		}
		if event.Key() == tcell.KeyTab && (page == 3 || page == 4) {
			showDetail := app.GetFocus() != detail
			if width < 85 {
				narrowDetail = showDetail
				render()
			}
			if showDetail {
				app.SetFocus(detail)
			} else {
				app.SetFocus(list)
			}
			return nil
		}
		if page == 3 && width < 85 && narrowDetail && (event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2) {
			narrowDetail = false
			render()
			app.SetFocus(list)
			return nil
		}
		if page == 3 && datastore != "" && (event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2) {
			browseUp()
			return nil
		}
		switch key := event.Rune(); key {
		case '1', '2', '3', '4', '5':
			switchPage(int(key - '0'))
		case 'r':
			if retrySwitchTarget != nil {
				// Reload the saved password: it may have been corrected by CLI
				// while the failed switch kept the previous environment active.
				next := *retrySwitchTarget
				if next.Saved() {
					fresh, err := target.Named(configPath, next.Name)
					if err != nil {
						statusMessage = err.Error()
						renderChrome()
						break
					}
					next = fresh
				}
				load(next, true)
			} else {
				load(current, false)
			}
		case 'p':
			if page == 2 && selectedVM != "" {
				for _, vm := range data.vms {
					if vm.ID != selectedVM {
						continue
					}
					previous := tview.Primitive(list)
					if vmDetailOpen {
						previous = detail
					}
					openTUIPower(app, pages, previous, current, vm, lang, func(action string) {
						pendingPower = action
						trendVM = vm.ID
						load(current, false)
					})
					break
				}
			}
		case 'g':
			if page == 4 {
				if counterEntity == "host" {
					counterEntity = "vm"
				} else {
					counterEntity = "host"
				}
				counterVM = ""
				load(current, false)
			}
		case 'f':
			if page == 2 && vmDetailOpen {
				backToVMList()
			}
			if page == 2 || page == 3 || page == 4 {
				filterVisible = true
				narrowDetail = false
				render()
				app.SetFocus(filter)
			}
		case '[', ']':
			if page == 2 && trendVM != "" && !showTrends && data.vmDetail != nil {
				step := 1
				if key == '[' {
					step = -1
				}
				detailGroup = (detailGroup + step + len(detailGroups)) % len(detailGroups)
				render()
				app.SetFocus(detail)
				if detailGroup == 0 && !loading && needsScreenshot() {
					load(current, false)
				}
			}
		case 't':
			if page == 2 {
				openVMDetails()
			}
		case 's':
			if page == 2 && selectedVM != "" && !loading {
				if !vmDetailOpen {
					vmListOffsetRow, vmListOffsetColumn = list.GetOffset()
				}
				vmDetailOpen = true
				trendVM = selectedVM
				showTrends = false
				detailGroup = 0
				refreshScreenshot = true
				if width < 85 {
					narrowDetail = true
				}
				load(current, false)
				render()
			}
		case 'm':
			transferFile(false)
		case 'c':
			if page == 3 {
				transferFile(true)
				break
			}
			if page == 2 && !loading && selectedVM != "" {
				for _, vm := range data.vms {
					if vm.ID == selectedVM {
						var stores []string
						for _, store := range headerData.stores {
							if store.Accessible == nil || *store.Accessible {
								stores = append(stores, store.Name)
							}
						}
						openTUIClone(ctx, app, pages, current, vm, lang, func(job tasks.Task) {
							settings.taskSelected = job.ID
							settings.module = 3
							switchPage(5)
							settings.showTask(job.ID)
						}, stores...)
						break
					}
				}
			}
		case 'e':
			if page == 2 && selectedVM != "" {
				if data.vmDetail == nil || data.vmDetail.ID != selectedVM {
					trendVM, editAfterLoad = selectedVM, selectedVM
					showTrends = false
					load(current, false)
					break
				}
				editVM(*data.vmDetail)
			}
		case 'u':
			profile.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
		case 'G':
			repository.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
		case '?':
			m := tview.NewModal().AddButtons([]string{lang.Text("Close")}).SetText(lang.Text("1–5 Pages · r Refresh · f Filter · u Environment · q Quit\nVMs: Enter Details · Esc Back · e Edit · p Power\n[/] Detail tabs · s Refresh screenshot · Tab Focus · PgUp/PgDn Scroll details\nDatastores: Enter Open · Backspace Up\nMetrics: live values · g Host/VM · Enter Choose VM\nSettings: [/] Modules · r Refresh · f Filter · Esc Back") + "\n" + lang.Text("tui.repository_hint"))
			m.SetBackgroundColor(tuiHeader).SetTextColor(tuiText).SetButtonBackgroundColor(tuiSelect).SetButtonTextColor(tuiText)
			m.SetBorderColor(tuiAccent)
			m.SetDoneFunc(func(_ int, _ string) { pages.RemovePage("help"); app.SetFocus(layout) })
			pages.AddPage("help", &tuiModal{m}, true, true)
			app.SetFocus(m)
		case 'q':
			preview.clear(lastScreen)
			cancel()
			app.Stop()
		default:
			return event
		}
		return nil
	})
	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		front, _ := pages.GetFrontPage()
		hovered := front == "main" && profile.InRect(event.Position())
		repositoryHover := front == "main" && repository.InRect(event.Position())
		if hovered != profileHovered || repositoryHover != repositoryHovered {
			repositoryHovered = repositoryHover
			profileHovered = hovered
			renderChrome()
		}
		return event, action
	})
	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		lastScreen = screen
		preview.drawn = false
		w, h := screen.Size()
		if w != width || h != height {
			width, height = w, h
			if width < 85 && (page == 3 || page == 4) {
				narrowDetail = detail.HasFocus()
			}
			render()
		}
		return false
	})
	app.SetAfterDrawFunc(func(screen tcell.Screen) {
		name, _ := pages.GetFrontPage()
		preview.beforeShow(screen, name == "main")
		if preview.native {
			screen.Show()
		}
		preview.afterDraw(screen, name == "main")
	})
	render()
	load(current, false)
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		taskTicker := time.NewTicker(2 * time.Second)
		defer taskTicker.Stop()
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				app.QueueEvent(tcell.NewEventKey(tcell.KeyF61, 0, tcell.ModNone))
				return
			case <-taskTicker.C:
				app.QueueEvent(tcell.NewEventKey(tcell.KeyF64, 0, tcell.ModNone))
			case <-ticker.C:
				app.QueueEvent(tcell.NewEventKey(tcell.KeyF62, 0, tcell.ModNone))
			}
		}
	}()
	err := app.SetRoot(pages, true).Run()
	cancel()
	select {
	case <-workerDone:
	case <-time.After(4 * time.Second):
	}
	return err
}

func loadTUI(ctx context.Context, c *esxi.Client, req tuiRequest, lang language) (tuiData, error) {
	h, err := c.Host(ctx)
	if err != nil {
		return tuiData{}, err
	}
	data := tuiData{host: h, about: c.About(ctx)}
	query := esxi.MetricsQuery{MaxSample: 45}
	switch req.page {
	case 1:
		data.metrics, err = c.HostMetrics(ctx, query)
		stores, storeErr := c.Datastores(ctx)
		vms, vmErr := c.ListVMs(ctx, false)
		data.stores, data.vms = stores, vms
		if storeErr != nil || vmErr != nil {
			data.overviewWarning = lang.Text("Some inventory data is unavailable")
		}
		if err == nil {
			disk := esxi.MetricsQuery{MaxSample: 45, Names: []string{"disk.read.average", "disk.write.average"}}
			if extra, diskErr := c.HostMetrics(ctx, disk); diskErr == nil {
				data.metrics.Series = append(data.metrics.Series, extra.Series...)
			}
		}
	case 2:
		data.vms, err = c.ListVMs(ctx, true)
		if err != nil {
			return data, err
		}
		if req.vmID != "" {
			data.trendVM = req.vmID
			vm, detailErr := c.DescribeVM(ctx, req.vmID)
			if detailErr == nil {
				for i, row := range data.vms {
					if row.ID == vm.ID {
						vm.CPUPct, vm.MemActivePct, vm.NetRxMbps, vm.NetTxMbps = row.CPUPct, row.MemActivePct, row.NetRxMbps, row.NetTxMbps
						data.vms[i] = vm
						break
					}
				}
				data.vmDetail = &vm
			} else {
				data.detailWarning = detailErr.Error()
				if req.screenshot {
					data.screenshotWarning = detailErr.Error()
				}
			}
			if req.screenshot && detailErr == nil && vm.PowerState == "poweredOn" {
				shotCtx, stop := context.WithTimeout(ctx, 8*time.Second)
				bytes, shotErr := c.Screenshot(shotCtx, req.vmID)
				stop()
				if shotErr == nil {
					data.screenshot, shotErr = decodeTUIScreenshot(bytes)
				}
				if shotErr != nil {
					data.screenshotWarning = lang.Text("Screenshot unavailable") + ": " + shotErr.Error()
				}
			}
			data.metrics, err = c.VMMetrics(ctx, req.vmID, query)
			if err == nil {
				disk := esxi.MetricsQuery{MaxSample: 45, Names: []string{"virtualDisk.read.average", "virtualDisk.write.average"}}
				if extra, diskErr := c.VMMetrics(ctx, req.vmID, disk); diskErr == nil {
					data.metrics.Series = append(data.metrics.Series, extra.Series...)
				}
			}
		}
	case 3:
		if req.datastore != "" {
			data.files, err = c.DatastoreFiles(ctx, req.datastore, req.path)
		} else {
			data.stores, err = c.Datastores(ctx)
		}
		return data, err
	case 4:
		data.counterTarget = lang.Text("Host") + " · " + data.host.Name
		if req.counterEntity == "vm" {
			data.vms, err = c.ListVMs(ctx, false)
			if err != nil {
				return data, err
			}
			for _, vm := range data.vms {
				if vm.ID == req.counterVM || (req.counterVM == "" && (data.counterVM == "" || vm.PowerState == "poweredOn")) {
					data.counterVM = vm.ID
					data.counterTarget = vm.Name + " (" + vm.ID + ")"
					if req.counterVM != "" || vm.PowerState == "poweredOn" {
						break
					}
				}
			}
			if data.counterVM == "" {
				data.metricWarning = lang.Text("No virtual machines available")
				return data, nil
			}
		}
		data.counters, data.metrics, err = c.CounterSamples(ctx, data.counterVM)
	}
	if err != nil {
		data.metricWarning = lang.Text("Metrics unavailable: ") + err.Error()
	}
	return data, nil
}
