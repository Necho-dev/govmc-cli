package tui

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	tuiSurface = tcell.NewHexColor(0x1e1e1e)
	tuiRail    = tcell.NewHexColor(0x222c35)
	tuiStripe  = tcell.NewHexColor(0x161616)
	tuiHeader  = tcell.NewHexColor(0x16324d)
	tuiSelect  = tcell.NewHexColor(0x1e4a7a)
	tuiText    = tcell.NewHexColor(0xdce5ed)
	tuiAccent  = tcell.NewHexColor(0x79c6f3)
	tuiMuted   = tcell.NewHexColor(0x9ba7b0)
)

func tuiWordmark() string {
	return "\n" + govmc.Wordmark
}

func tuiTextView() *tview.TextView {
	view := tview.NewTextView().SetDynamicColors(true).SetTextColor(tuiText)
	view.SetBackgroundColor(tuiSurface)
	return view
}

func tuiColor(color, text string) string {
	return "[" + color + "]" + tview.Escape(text) + "[-]"
}

func tuiNumber(value *float64, suffix string) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f%s", *value, suffix)
}

func tuiEndpoint(t target.Target) string {
	if u, err := url.Parse(t.URL); err == nil && u.Host != "" {
		if t.Username != "" {
			return t.Username + "@" + u.Host
		}
		return u.Host
	}
	return t.Name
}

// tuiHeaderText uses OpenWrt's two stacked metrics per column. Labels describe
// the denominator and allocation scope; no static capacity is presented as usage.
func tuiHeaderText(data tuiData, lang language, compact ...bool) [3]string {
	h := data.host
	if h == nil {
		return [3]string{tuiColor("#79c6f3", lang.Text("Host")) + "\n" + lang.Text("Waiting for host data")}
	}
	label := func(text string) string { return tuiColor("#79c6f3", lang.Text(text)) }
	number := func(value int16) string {
		if value <= 0 {
			return "—"
		}
		return strconv.Itoa(int(value))
	}
	uptime := "—"
	if h.UptimeSeconds != nil && *h.UptimeSeconds >= 0 {
		uptime = fmt.Sprintf("%dd %dh %dm", *h.UptimeSeconds/86400, *h.UptimeSeconds/3600%24, *h.UptimeSeconds/60%60)
	}
	running, vcpus := 0, 0
	for _, vm := range data.vms {
		if vm.PowerState == "poweredOn" {
			running++
			vcpus += int(vm.NumCPU)
		}
	}
	workload, allocation := "—", "—"
	if data.vms != nil {
		workload = tuiColor("#67c23a", strconv.Itoa(running)) + " / " + strconv.Itoa(len(data.vms))
		allocation = tuiColor("#b69aef", fmt.Sprintf("%d %s", vcpus, lang.Text("vCPU (running)")))
	}
	memory := "—"
	if h.MemoryMB > 0 {
		memory = fmt.Sprintf("%.1f GiB", float64(h.MemoryMB)/1024)
	}
	storage, storageNote := "—", ""
	if data.stores != nil {
		var capacity, used int64
		complete := true
		for _, store := range data.stores {
			bytes, _, valid := tuiDatastoreSpace(store)
			if !valid {
				complete = false
				continue
			}
			capacity += store.Capacity
			used += bytes
		}
		if capacity > 0 && complete {
			storage = fmt.Sprintf("%.1f / %.1f GiB", float64(used)/(1<<30), float64(capacity)/(1<<30))
			percent := float64(used) / float64(capacity) * 100
			color := "#9ba7b0"
			if percent >= 90 {
				color = "#ed5c5c"
			} else if percent >= 75 {
				color = "#f7b65c"
			}
			storageNote = tuiColor(color, fmt.Sprintf("%.1f%% %s", percent, lang.Text("used")))
		} else if !complete {
			storageNote = tuiColor("#f7b65c", lang.Text("Incomplete storage data"))
		}
	}
	statusColor := "#9ba7b0"
	switch {
	case h.ConnectionState != "" && h.ConnectionState != "connected", h.OverallStatus == "red":
		statusColor = "#ed5c5c"
	case h.MaintenanceMode, h.OverallStatus == "yellow":
		statusColor = "#f7b65c"
	case h.OverallStatus == "green", h.ConnectionState == "connected":
		statusColor = "#67c23a"
	}
	api, build := data.about.APIVersion, data.about.Build
	if api == "" {
		api = "—"
	}
	if build == "" {
		build = "—"
	}
	sockets := ""
	if h.NumCpuPkgs > 0 {
		sockets = fmt.Sprintf(lang.Text("%d sockets"), h.NumCpuPkgs)
	}
	cpu := fmt.Sprintf("%s %s / %s %s", number(h.NumCpuCores), lang.Text("cores"), number(h.NumCpuThreads), lang.Text("threads"))
	result := [3]string{
		strings.Join([]string{label("Host / API"), tview.Escape(h.Name), tview.Escape(fmt.Sprintf("%s (build %s)", api, build)), label("VMs running / total"), workload, allocation}, "\n"),
		strings.Join([]string{label("Physical CPU"), cpu, sockets, label("Host RAM (total)"), memory}, "\n"),
		strings.Join([]string{label("Host state / uptime"), tuiColor(statusColor, tuiHostStatus(h, lang)), uptime, label("Storage used / total"), storage, storageNote}, "\n"),
	}
	if len(compact) > 0 && compact[0] {
		result[1] = strings.Join([]string{label("CPU / RAM total"), cpu, memory}, "\n")
	}
	return result
}

func tuiHostStatus(h *esxi.HostInfo, lang language) string {
	switch {
	case h.ConnectionState != "" && h.ConnectionState != "connected":
		return lang.Text("Disconnected")
	case h.MaintenanceMode:
		return lang.Text("Maintenance")
	case h.OverallStatus == "green":
		return lang.Text("Healthy")
	case h.OverallStatus == "red":
		return lang.Text("Critical")
	case h.OverallStatus == "yellow":
		return lang.Text("Warning")
	case h.ConnectionState == "connected":
		return lang.Text("Connected")
	default:
		return "—"
	}
}

func tuiInspector(title string, fields [][2]string, width int) string {
	var out strings.Builder
	out.WriteString(" " + tuiColor("#79c6f3", title) + "\n")
	labelColumn := 0
	for _, field := range fields {
		labelColumn = max(labelColumn, tview.TaggedStringWidth(tview.Escape(field[0])))
	}
	labelColumn = min(labelColumn, max(12, width/2)) + 2
	for _, field := range fields {
		value := field[1]
		if value == "" {
			value = "—"
		}
		label := tview.Escape(field[0])
		labelWidth := tview.TaggedStringWidth(label)
		if labelWidth >= labelColumn {
			out.WriteString(" " + tuiColor("#9ba7b0", field[0]) + "\n")
			for _, line := range tview.WordWrap(tview.Escape(value), max(1, width-4)) {
				out.WriteString("   " + line + "\n")
			}
			continue
		}
		for i, line := range tview.WordWrap(tview.Escape(value), max(1, width-labelColumn-2)) {
			if i == 0 {
				out.WriteString(" " + tuiColor("#9ba7b0", field[0]) + strings.Repeat(" ", labelColumn-labelWidth))
			} else {
				out.WriteString(strings.Repeat(" ", labelColumn+1))
			}
			out.WriteString(line + "\n")
		}
	}
	return out.String()
}

// Chart blocks follow the OpenWrt workbench: compact resource plots, then
// two upright, independently scaled rate plots with blue/green legends.
func tuiTrends(metrics *esxi.MetricsResponse, warning string, width, height int, lang language) string {
	return tuiTrendGroups(metrics, warning, width, height, lang, -1)
}

func tuiTrendGroups(metrics *esxi.MetricsResponse, warning string, width, height int, lang language, selected int) string {
	width = max(1, width-4)
	series := func(name string) []esxi.MetricSample {
		if metrics != nil {
			for _, item := range metrics.Series {
				if item.Instance == "" && (item.Name == name || item.Name == strings.Replace(name, "disk.", "virtualDisk.", 1)) {
					return item.Points
				}
			}
		}
		return nil
	}
	var first, last time.Time
	for _, name := range []string{"cpu.usage.average", "mem.active.average", "net.bytesRx.average", "net.bytesTx.average", "disk.read.average", "disk.write.average"} {
		for _, point := range series(name) {
			if point.TimeUTC.IsZero() {
				continue
			}
			if first.IsZero() || point.TimeUTC.Before(first) {
				first = point.TimeUTC
			}
			if point.TimeUTC.After(last) {
				last = point.TimeUTC
			}
		}
	}
	var out strings.Builder
	if warning != "" {
		out.WriteString("  " + tuiColor("#f7b65c", warning) + "\n")
		height--
	}
	// Each block reserves its own legends and axes, including empty states.
	compact := height < 24
	tiny := height < 16
	overhead := 6 // One legend for each of the six series.
	if !tiny {
		overhead += 3
	}
	if !compact {
		overhead += 2
	}
	if height >= 13 {
		overhead++
	}
	sectionGap := !compact
	if sectionGap {
		overhead += 2
	}
	plotHeights := tuiPlotHeights(height - overhead)
	if selected >= 0 {
		compact, tiny, sectionGap = false, false, false
		rows := max(2, height-4)
		plotHeights[selected*2] = rows / 2
		plotHeights[selected*2+1] = rows - rows/2
	}
	writePlot := func(name, label, color, dim, unit string, factor, ceiling float64, rows int) {
		points := series(name)
		values := make([]float64, min(45, width))
		for i := range values {
			values[i] = -1
		}
		latest, total, peak, count := 0.0, 0.0, 0.0, 0
		var latestTime time.Time
		for _, point := range points {
			value := point.Value * factor
			if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) || point.TimeUTC.IsZero() {
				continue
			}
			if latestTime.IsZero() || !point.TimeUTC.Before(latestTime) {
				latest, latestTime = value, point.TimeUTC
			}
			total += value
			peak = max(peak, value)
			count++
			index := len(values) - 1
			if last.After(first) {
				index = int(math.Round(float64(point.TimeUTC.Sub(first)) / float64(last.Sub(first)) * float64(len(values)-1)))
			}
			values[index] = max(values[index], value)
		}
		format := func(value float64) string {
			if unit == " MB/s" {
				return fmt.Sprintf("%.3f%s", value, unit)
			}
			return fmt.Sprintf("%.1f%s", value, unit)
		}
		legend := "  " + tuiColor(color, lang.Text(label)) + "  "
		if count == 0 {
			legend += tuiColor("#9ba7b0", lang.Text("No samples"))
		} else {
			legend += tuiColor(color, format(latest))
			if width >= 45 {
				legend += tuiColor("#9ba7b0", "   "+lang.Text("Average")+" ") + tuiColor(color, format(total/float64(count))) +
					tuiColor("#9ba7b0", "   "+lang.Text("Peak")+" ") + tuiColor(color, format(peak))
			}
		}
		out.WriteString(legend + "\n")
		scale := max(peak, ceiling)
		glyphs := []rune(" ▁▂▃▄▅▆▇█")
		for row := rows - 1; row >= 0; row-- {
			out.WriteString("  ")
			for x := range width {
				value := values[x*len(values)/width]
				if value < 0 {
					out.WriteByte(' ')
					continue
				}
				level := 0
				if scale > 0 && value > 0 {
					level = int(math.Ceil(value / scale * float64(rows*8)))
				}
				glyph := glyphs[max(0, min(8, level-row*8))]
				shade := dim
				if scale > 0 && value/scale > 0.5 {
					shade = color
				}
				out.WriteString(tuiColor(shade, string(glyph)))
			}
			out.WriteByte('\n')
		}
	}
	axis := func() {
		if first.IsZero() || last.IsZero() || width < 24 {
			out.WriteByte('\n')
			return
		}
		left, middle, right := first.Local().Format("15:04"), first.Add(last.Sub(first)/2).Local().Format("15:04"), last.Local().Format("15:04")
		gap := max(2, (width-len(left)-len(middle)-len(right))/2)
		line := left + strings.Repeat(" ", gap) + middle
		line += strings.Repeat(" ", max(1, width-len(line)-len(right))) + right
		out.WriteString("  " + tuiColor("#9ba7b0", line) + "\n")
	}

	if selected < 0 || selected == 0 {
		if !tiny {
			out.WriteString("  " + tuiColor("#f7b65c", lang.Text("Compute / memory")) + "\n")
		}
		writePlot("cpu.usage.average", "CPU", "#f7b65c", "#b27e43", "%", 1, 100, plotHeights[0])
		writePlot("mem.active.average", "Active memory", "#b699ef", "#69528d", " GiB", 1.0/(1<<20), 0, plotHeights[1])
		if !compact {
			axis()
		}

	}

	if selected < 0 || selected == 1 {
		if sectionGap {
			out.WriteByte('\n')
		}
		if !tiny {
			out.WriteString("  " + tuiColor("#f7b65c", lang.Text("Network")) + "\n")
		}
		writePlot("net.bytesRx.average", "↓ Receive", "#4c8dff", "#2d5990", " Mbps", 1, 0, plotHeights[2])
		writePlot("net.bytesTx.average", "↑ Transmit", "#67c23a", "#3e7427", " Mbps", 1, 0, plotHeights[3])
		if !compact {
			axis()
		}

	}

	if selected < 0 || selected == 2 {
		if sectionGap {
			out.WriteByte('\n')
		}
		if !tiny {
			out.WriteString("  " + tuiColor("#f7b65c", lang.Text("Disk throughput")) + "\n")
		}
		writePlot("disk.read.average", "↓ Read", "#4c8dff", "#2d5990", " MB/s", .001, 0, plotHeights[4])
		writePlot("disk.write.average", "↑ Write", "#67c23a", "#3e7427", " MB/s", .001, 0, plotHeights[5])
		if height >= 13 || selected >= 0 {
			axis()
		}
	}

	return out.String()
}

func tuiBool(value *bool, lang language) string {
	if value == nil {
		return lang.Text("Unknown")
	}
	if *value {
		return lang.Text("Enabled")
	}
	return lang.Text("Disabled")
}

func tuiOptional[T any](value *T) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprint(*value)
}

func tuiGiB(bytes float64) string {
	if bytes < 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f GiB", bytes/(1<<30))
}

func tuiOSName(vm esxi.VMInfo, lang language) string {
	name := vm.GuestOS
	if name == "" || name == vm.GuestID {
		names := map[string]string{
			"windows11_64Guest": "Microsoft Windows 11 (64-bit)", "windows9_64Guest": "Microsoft Windows 10 (64-bit)",
			"windows9Guest": "Microsoft Windows 10 (32-bit)", "ubuntu64Guest": "Ubuntu Linux (64-bit)",
			"ubuntuGuest": "Ubuntu Linux (32-bit)", "otherLinux64Guest": "Other Linux (64-bit)", "otherLinuxGuest": "Other Linux (32-bit)",
		}
		key := vm.GuestID
		if key == "" {
			key = name
		}
		if known := names[key]; known != "" {
			name = known
		} else if name == "" {
			name = key
		}
	}
	if lang == "zh" {
		name = strings.NewReplacer("64-bit", "64 位", "32-bit", "32 位", "64 bit", "64 位", "32 bit", "32 位", "Other Linux", "其他 Linux").Replace(name)
	}
	if name == "" {
		return "—"
	}
	return name
}

func tuiTools(guest esxi.GuestInfo, lang language) string {
	if guest.ToolsStatus == "toolsNotInstalled" {
		return lang.Text("Not installed")
	}
	installed := guest.ToolsVersionText != "" || (guest.ToolsVersion != "" && guest.ToolsVersion != "0") ||
		guest.ToolsStatus == "toolsOk" || guest.ToolsStatus == "toolsOld" || guest.ToolsStatus == "toolsNotRunning" ||
		guest.ToolsRunningStatus == "guestToolsRunning" || guest.ToolsRunningStatus == "guestToolsExecutingScripts"
	if !installed {
		return lang.Text("Unknown")
	}
	version := guest.ToolsVersionText
	if version == "" {
		version = lang.Text("Version not reported")
	}
	return lang.Text("Installed") + " (" + version + ")"
}

func tuiDiskSummary(vm esxi.VMInfo) string {
	capacity := int64(0)
	for _, disk := range vm.Disks {
		if disk.Capacity <= 0 {
			return "—"
		}
		capacity += disk.Capacity * 1024
	}
	quota, used := "—", "—"
	if len(vm.Disks) > 0 {
		quota = tuiGiB(float64(capacity))
	}
	if vm.CommittedBytes != nil {
		used = tuiGiB(float64(*vm.CommittedBytes))
	}
	return used + " / " + quota
}

func tuiVMDetails(vm esxi.VMInfo, lang language, width, selectedGroup int) string {
	var out strings.Builder
	group := 0
	section := func(title string, fields [][2]string) {
		if selectedGroup >= 0 && selectedGroup != group {
			return
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(tuiInspector(lang.Text(title), fields, width))
	}
	field := func(label, value string) [2]string { return [2]string{lang.Text(label), value} }
	addresses := []string{}
	for _, address := range append([]string{vm.Guest.IPAddress}, vm.Guest.IPAddresses...) {
		if address != "" && !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	vbs := vm.VBSEnabled
	if vbs == nil && vm.Config != nil {
		vbs = vm.Config.VBS
	}
	connection := lang.Text(vm.ConnectionState)
	if connection == "" {
		connection = lang.Text("Unknown")
	}
	section("System / status", [][2]string{
		field("Power / connection", lang.Power(vm.PowerState)+" / "+connection), field("Hostname", vm.Guest.HostName),
		field("Operating system", tuiOSName(vm, lang)), field("Tools", tuiTools(vm.Guest, lang)), field("VBS", tuiBool(vbs, lang)),
	})
	group = 2
	if len(addresses) > 0 {
		ipFields := make([][2]string, 0, len(addresses))
		for i, address := range addresses {
			ipFields = append(ipFields, field(fmt.Sprintf("IP %d", i+1), address))
		}
		section("IP addresses", ipFields)
	}
	group = 0
	if vm.Question != nil {
		section("Pending question", [][2]string{field("Question", vm.Question.Text)})
	}
	cpu, hostMem, active, uptime := "—", "—", "—", "—"
	if vm.QuickStatsReliable && vm.QuickStats != nil {
		stats := vm.QuickStats
		if stats.OverallCPUUsage >= 0 {
			cpu = fmt.Sprintf("%d MHz", stats.OverallCPUUsage)
		}
		if stats.HostMemoryUsage >= 0 {
			hostMem = tuiGiB(float64(stats.HostMemoryUsage) * (1 << 20))
		}
		if stats.GuestMemoryUsage >= 0 {
			active = tuiGiB(float64(stats.GuestMemoryUsage) * (1 << 20))
		}
		if stats.UptimeSeconds >= 0 {
			uptime = fmt.Sprintf("%dd %dh %dm", stats.UptimeSeconds/86400, stats.UptimeSeconds/3600%24, stats.UptimeSeconds/60%60)
		}
	}
	group = 1
	section("Compute / memory", [][2]string{
		field("vCPU", fmt.Sprint(vm.NumCPU)), field("Configured memory", tuiGiB(float64(vm.MemoryMB)*(1<<20))),
		field("Host CPU usage", cpu), field("Host memory usage", hostMem), field("Active memory", active), field("Uptime", uptime),
		field("CPU", tuiNumber(vm.CPUPct, "%")), field("Active memory usage", tuiNumber(vm.MemActivePct, "%")),
		field("Receive", tuiNumber(vm.NetRxMbps, " Mbps")), field("Transmit", tuiNumber(vm.NetTxMbps, " Mbps")),
	})
	group = 0
	ip := "—"
	if len(addresses) > 0 {
		ip = addresses[0]
		if len(addresses) > 1 {
			ip += fmt.Sprintf(" +%d", len(addresses)-1)
		}
	}
	section("Resources", [][2]string{
		field("Specifications", fmt.Sprintf("%d vCPU / %.1f GiB", vm.NumCPU, float64(vm.MemoryMB)/1024)),
		field("IP", ip), field("CPU", tuiNumber(vm.CPUPct, "%")), field("Active memory", active),
		field("Used / configured", tuiDiskSummary(vm)),
		field("Receive / transmit", tuiNumber(vm.NetRxMbps, " Mbps")+" / "+tuiNumber(vm.NetTxMbps, " Mbps")),
	})
	group = 3
	storage := [][2]string{field("Used / configured", tuiDiskSummary(vm)), field("Disks", fmt.Sprint(len(vm.Disks)))}
	for _, item := range []struct {
		label string
		value *int64
	}{{"Potential additional storage", vm.UncommittedBytes}, {"Unshared storage", vm.UnsharedBytes}} {
		value := "—"
		if item.value != nil {
			value = tuiGiB(float64(*item.value))
		}
		storage = append(storage, field(item.label, value))
	}
	section("Storage", storage)
	if cfg := vm.Config; cfg != nil {
		sockets := "—"
		if cfg.CoresPerSocket != nil && *cfg.CoresPerSocket > 0 && vm.NumCPU%*cfg.CoresPerSocket == 0 {
			sockets = fmt.Sprint(vm.NumCPU / *cfg.CoresPerSocket)
		}
		cores := tuiOptional(cfg.CoresPerSocket)
		if cfg.CoresPerSocket != nil && *cfg.CoresPerSocket == 0 {
			cores = lang.Text("Automatic")
		}
		group = 1
		section("CPU configuration", [][2]string{
			field("CPU sockets", sockets), field("Cores per socket", cores),
			field("CPU hot add", tuiBool(cfg.CPUHotAdd, lang)), field("Nested virtualization", tuiBool(cfg.NestedHV, lang)),
			field("Virtual IOMMU", tuiBool(cfg.IOMMU, lang)), field("CPU performance counters", tuiBool(cfg.VPMC, lang)),
			field("CPU reservation (MHz)", tuiOptional(cfg.CPUReservationMHz)), field("CPU limit (MHz, -1 unlimited)", tuiOptional(cfg.CPULimitMHz)), field("CPU shares", cfg.CPUShares),
		})
		section("Memory configuration", [][2]string{
			field("Memory hot add", tuiBool(cfg.MemoryHotAdd, lang)), field("Memory reservation (MiB)", tuiOptional(cfg.MemoryReservationMB)),
			field("Memory limit (MiB, -1 unlimited)", tuiOptional(cfg.MemoryLimitMB)), field("Memory shares", cfg.MemoryShares), field("Reserve all memory", tuiBool(cfg.MemoryLocked, lang)),
		})
		group = 4
		section("VM options", [][2]string{
			field("Virtual hardware", cfg.Version), field("Firmware", cfg.Firmware), field("Secure boot", tuiBool(cfg.SecureBoot, lang)),
			field("VM configuration file", cfg.VMX), field("Suspend directory", cfg.SuspendDirectory), field("Annotation", cfg.Annotation),
			field("Boot delay (ms)", tuiOptional(cfg.BootDelayMS)), field("Boot retry", tuiBool(cfg.BootRetry, lang)),
			field("Console session limit", fmt.Sprint(cfg.MaxMksConnections)), field("Lock guest after disconnect", tuiBool(cfg.GuestAutoLock, lang)),
			field("Tools upgrade policy", cfg.ToolsUpgradePolicy), field("Standby action", cfg.StandbyAction),
		})
		group = 5
		for _, device := range cfg.Devices {
			fields := [][2]string{field("Type", device.Type), field("Device key", fmt.Sprint(device.Key))}
			if device.Type == "VirtualMachineVideoCard" {
				fields = append(fields, field("Video RAM (MiB)", fmt.Sprintf("%.1f", float64(device.VideoRAMKB)/1024)), field("Displays", fmt.Sprint(device.Displays)), field("3D", tuiBool(device.Enable3D, lang)))
			}
			// Disk and NIC configuration is rendered with the additional backing fields below.
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
			if !found {
				section(device.Label, fields)
			}
		}
	}
	group = 2
	for _, nic := range vm.NICs {
		fields := [][2]string{field("Portgroup", nic.Portgroup), field("Adapter", nic.Adapter), field("MAC", nic.MAC),
			field("Connected", tuiBool(nic.Connected, lang)), field("Connect at power on", tuiBool(nic.StartConnected, lang)), field("Address assignment", lang.Text(nic.AddressType))}
		for i, address := range nic.IPAddresses {
			fields = append(fields, field(fmt.Sprintf("IP %d", i+1), address))
		}
		section(nic.Label, fields)
	}
	group = 3
	for _, disk := range vm.Disks {
		provisioning := lang.Text("Unknown")
		if disk.Thin != nil {
			if *disk.Thin {
				provisioning = lang.Text("Thin")
			} else if disk.EagerlyScrub != nil && *disk.EagerlyScrub {
				provisioning = lang.Text("Eager zeroed thick")
			} else {
				provisioning = lang.Text("Thick")
			}
		}
		section(disk.Label, [][2]string{
			field("Configured capacity", tuiGiB(float64(disk.Capacity)*1024)), field("Disk file", disk.FileName), field("Provisioning", provisioning), field("Disk mode", disk.Mode),
			field("Controller / unit", fmt.Sprint(disk.ControllerKey)+" / "+tuiOptional(disk.UnitNumber)), field("IOPS limit (-1 unlimited)", tuiOptional(disk.IOPSLimit)),
		})
	}
	group = 5
	for _, gpu := range vm.VGPU {
		section(gpu.Label, [][2]string{field("vGPU", gpu.Type)})
	}
	group = 4
	section("Tools", [][2]string{field("Tools", tuiTools(vm.Guest, lang)), field("Tools running", lang.Text(vm.Guest.ToolsRunningStatus)), field("Tools installation", vm.Guest.ToolsInstallType)})
	section("Identifiers", [][2]string{field("Name", vm.Name), field("ID", vm.ID), field("MoRef", vm.MoRef), field("UUID", vm.UUID), field("Instance UUID", vm.InstanceUUID), field("BIOS UUID", vm.BiosUUID), field("Guest ID", vm.GuestID)})
	if out.Len() == 0 {
		return " " + tuiColor("#9ba7b0", lang.Text("No records"))
	}
	return out.String()
}

func tuiVMHint(lang language) string {
	return tuiColor("#9ba7b0", lang.Text("t Details/Trends · e Edit configuration · ←/→ Columns"))
}

func tuiNetworkRates(vm esxi.VMInfo) string {
	rate := func(value *float64) string {
		if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
			return fmt.Sprintf("%5s", "—")
		}
		return fmt.Sprintf("%5.2f", *value)
	}
	return tuiColor("#4c8dff", "↓"+rate(vm.NetRxMbps)) + "  " + tuiColor("#67c23a", "↑"+rate(vm.NetTxMbps))
}

func tuiTableDisk(vm esxi.VMInfo) string {
	var capacity int64
	for _, disk := range vm.Disks {
		if disk.Capacity <= 0 {
			return "—"
		}
		capacity += disk.Capacity * 1024
	}
	used, quota := "—", "—"
	if vm.CommittedBytes != nil {
		used = fmt.Sprintf("%.1f", float64(*vm.CommittedBytes)/(1<<30))
	}
	if len(vm.Disks) > 0 {
		quota = fmt.Sprintf("%.1f", float64(capacity)/(1<<30))
	}
	return fmt.Sprintf("%6s / %6s", used, quota)
}

func tuiHeaderHeight(height int) int {
	if height < 32 {
		return 4
	}
	return 8
}

func tuiMetricColor(text string, normal int32) tcell.Color {
	value, err := strconv.ParseFloat(strings.TrimSuffix(text, "%"), 64)
	if err == nil {
		if value >= 95 {
			return tcell.NewHexColor(0xf07878)
		}
		if value >= 80 {
			return tcell.NewHexColor(0xf7b65c)
		}
	}
	return tcell.NewHexColor(normal)
}

func tuiTableCell(text string, width int, numeric bool) string {
	padding := strings.Repeat(" ", max(0, width-tview.TaggedStringWidth(text)))
	if numeric {
		return " " + padding + text + " "
	}
	return " " + text + padding + " "
}

func tuiCounterDetails(counter esxi.CounterInfo, metrics *esxi.MetricsResponse, warning string, width int, lang language) string {
	value, unit, sampled := tuiCounterLatest(counter, metrics)
	text := tuiInspector(counter.Name, [][2]string{
		{lang.Text("Latest"), value}, {lang.Text("Sampled"), sampled},
		{lang.Text("Unit"), unit}, {lang.Text("Group"), counter.Group}, {lang.Text("Rollup"), counter.Rollup}, {lang.Text("Description"), counter.Description},
	}, width)
	if warning != "" {
		text += "\n" + tuiColor("#f7b65c", warning)
	}
	if metrics == nil {
		return text + "\n" + tuiColor("#9ba7b0", lang.Text("No samples"))
	}
	var out strings.Builder
	out.WriteString(text + "\n" + tuiColor("#f7b65c", lang.Text("Samples")) + "\n")
	samples := 0
	for _, series := range metrics.Series {
		if series.Name != counter.Name {
			continue
		}
		out.WriteString(tuiColor("#9ba7b0", series.Unit+" "+series.Instance) + "\n")
		for _, point := range series.Points {
			if !tuiValidSample(point) {
				continue
			}
			fmt.Fprintf(&out, "%s  %.3f\n", point.TimeUTC.Local().Format("15:04:05"), point.Value)
			samples++
		}
	}
	if samples == 0 {
		out.WriteString(tuiColor("#9ba7b0", lang.Text("No samples")))
	}
	return out.String()
}

// Keep fixed-scale compute plots shallow; give paired I/O plots the remaining rows.
func tuiPlotHeights(available int) [6]int {
	heights := [6]int{1, 1, 1, 1, 1, 1}
	remaining := max(0, available-6)
	if remaining >= 6 {
		heights[0]++
		heights[1]++
		remaining -= 2
	}
	for i := 0; i < remaining; i++ {
		heights[2+i%4]++
	}
	return heights
}

func tuiTablePower(state string, lang language) string {
	if lang != "zh" && state == "poweredOff" {
		return "Off"
	}
	return lang.Power(state)
}
