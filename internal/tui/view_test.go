package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/rivo/tview"
)

func TestTUITrendsFitAvailableSpace(t *testing.T) {
	metrics := &esxi.MetricsResponse{}
	start := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	for _, name := range []string{"cpu.usage.average", "mem.active.average", "net.bytesRx.average", "net.bytesTx.average", "virtualDisk.read.average", "virtualDisk.write.average"} {
		series := esxi.MetricSeriesOut{Name: name}
		for i := range 45 {
			series.Points = append(series.Points, esxi.MetricSample{TimeUTC: start.Add(time.Duration(i) * 20 * time.Second), Value: float64(i % 9)})
		}
		metrics.Series = append(metrics.Series, series)
	}
	for _, size := range [][2]int{{120, 37}, {100, 29}, {68, 17}, {120, 19}, {120, 12}} {
		output := tuiTrends(metrics, "", size[0], size[1], "en")
		lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d has %d lines", size[0], size[1], len(lines))
		}
		for _, line := range lines {
			if tview.TaggedStringWidth(line) > size[0] {
				t.Fatalf("%dx%d contains overflowing line %q", size[0], size[1], line)
			}
		}
		for _, label := range []string{"CPU", "Active memory", "↓ Receive", "↑ Transmit", "↓ Read", "↑ Write"} {
			if !strings.Contains(output, label) {
				t.Fatalf("%dx%d omitted %s", size[0], size[1], label)
			}
		}
	}
}

func TestTUITrendsMissingDataPreservesPlots(t *testing.T) {
	now := time.Now()
	for _, metrics := range []*esxi.MetricsResponse{nil, {Series: []esxi.MetricSeriesOut{{Name: "net.bytesRx.average", Points: []esxi.MetricSample{{TimeUTC: now, Value: -1}, {TimeUTC: now, Value: math.NaN()}}}}}} {
		output := tuiTrends(metrics, "", 100, 29, "en")
		if strings.Count(output, "No samples") != 6 {
			t.Fatal("each unavailable series should retain its own legend and plot")
		}
		if strings.Contains(output, "NaN") {
			t.Fatal("invalid samples leaked into the legend")
		}
	}
}

// A module containing only read-only form items must still accept focus.
func TestTUIEditorReadOnlyModuleCanOpen(t *testing.T) {
	app := tview.NewApplication()
	pages := tview.NewPages()
	list := tview.NewTable()
	pages.AddPage("main", list, true, true)
	app.SetRoot(pages, true)
	vm := esxi.VMInfo{ID: "vm-1", Name: "running-vm", PowerState: "poweredOn",
		Config: &esxi.VMConfigState{Editable: true, DeviceEditable: true, Version: "vmx-20", Firmware: "efi"}}
	done := make(chan struct{})
	go func() {
		openTUIEditor(app, pages, list, "lab", vm, nil, "en", func(esxi.VMChange) {})
		close(done)
	}()
	select {
	case <-done:
		name, _ := pages.GetFrontPage()
		if name != "editor" {
			t.Fatalf("expected editor, got %q", name)
		}
	case <-time.After(time.Second):
		t.Fatal("read-only module could not receive focus")
	}
}

func TestTUIVMDetailsIncludeWebFields(t *testing.T) {
	yes, no := true, false
	cores, reservation, limit, unit := int32(2), int64(128), int64(-1), int32(0)
	used, potential, unshared := int64(3<<30), int64(5<<30), int64(2<<30)
	vm := esxi.VMInfo{
		Name: "test-vm", ID: "vm-1", GuestID: "windows11_64Guest", NumCPU: 4, MemoryMB: 8192, PowerState: "poweredOn",
		CommittedBytes: &used, UncommittedBytes: &potential, UnsharedBytes: &unshared, VBSEnabled: &yes,
		QuickStatsReliable: true, QuickStats: &esxi.QuickStats{OverallCPUUsage: 250, HostMemoryUsage: 2048, GuestMemoryUsage: 1024},
		Guest: esxi.GuestInfo{HostName: "test-host", IPAddress: "192.0.2.1", IPAddresses: []string{"192.0.2.1", "2001:db8::1"}, ToolsStatus: "toolsOk", ToolsVersion: "2147483647", ToolsVersionText: "open-vm-tools 13.0.1 build 25056151"},
		Config: &esxi.VMConfigState{Version: "vmx-20", Firmware: "efi", CoresPerSocket: &cores, CPUReservationMHz: &reservation, CPULimitMHz: &limit,
			MemoryReservationMB: &reservation, MemoryLimitMB: &limit, VMX: "[data] test-vm/test-vm.vmx", Annotation: "notes",
			VPMC: &yes, SecureBoot: &yes, MemoryLocked: &no, Devices: []esxi.VMDeviceInfo{{Label: "Video card", Key: 500, Type: "VirtualMachineVideoCard", VideoRAMKB: 16384, Displays: 1, Enable3D: &no}}},
		NICs:  []esxi.NIC{{Label: "NIC 1", Key: 400, MAC: "00:50:56:00:00:01", Portgroup: "VM Network", Connected: &yes, StartConnected: &yes, AddressType: "generated", IPAddresses: []string{"192.0.2.1", "2001:db8::1"}}},
		Disks: []esxi.Disk{{Label: "Disk 1", Key: 300, Capacity: 8 << 20, FileName: "[data] test-vm/disk.vmdk", Thin: &yes, Mode: "persistent", ControllerKey: 1000, UnitNumber: &unit, IOPSLimit: &limit}},
	}
	text := tview.NewTextView().SetDynamicColors(true).SetText(tuiVMDetails(vm, "en", 160, -1)).GetText(true)
	for _, value := range []string{"Microsoft Windows 11 (64-bit)", "test-host", "2001:db8::1", "open-vm-tools 13.0.1 build 25056151", "250 MHz", "3.0 GiB / 8.0 GiB", "Potential additional storage", "Unshared storage", "CPU sockets", "CPU reservation (MHz)", "Memory limit (MiB, -1 unlimited)", "VM configuration file", "[data] test-vm/test-vm.vmx", "Video RAM (MiB)", "Displays", "00:50:56:00:00:01", "Address assignment", "Thin", "persistent", "IOPS limit (-1 unlimited)"} {
		if !strings.Contains(text, value) {
			t.Errorf("detail missing %q", value)
		}
	}
	if strings.Contains(text, "2147483647") {
		t.Fatal("raw Tools version sentinel is displayed")
	}
	vm.QuickStatsReliable = false
	text = tview.NewTextView().SetDynamicColors(true).SetText(tuiVMDetails(vm, "en", 160, -1)).GetText(true)
	if strings.Contains(text, "250 MHz") {
		t.Fatal("unreliable quick stats were displayed")
	}
}

func TestTUIOverviewFitsCompactInspector(t *testing.T) {
	vm := esxi.VMInfo{ID: "vm-42", Name: "windows-example", GuestID: "windows11_64Guest", PowerState: "poweredOn", NumCPU: 8, MemoryMB: 24576,
		Guest:  esxi.GuestInfo{HostName: "windows-example", IPAddress: "192.0.2.42", ToolsStatus: "toolsOk", ToolsVersionText: "VMware Tools 13.0.1 build 25056151"},
		Config: &esxi.VMConfigState{VMX: "[datastore] windows-example/windows-example.vmx"}}
	for _, lang := range []language{"en", "zh"} {
		text := tuiVMDetails(vm, lang, 55, 0)
		if lines := strings.Count(text, "\n"); lines > 16 {
			t.Fatalf("%s overview needs %d lines", lang, lines)
		}
		for _, line := range strings.Split(text, "\n") {
			if tview.TaggedStringWidth(line) > 55 {
				t.Fatalf("%s overflow %q", lang, line)
			}
		}
		if strings.Contains(text, vm.Config.VMX) {
			t.Fatal("advanced configuration leaked into the overview")
		}
		if !strings.Contains(text, "192.0.2.42") {
			t.Fatal("primary address was omitted")
		}
		options := tuiVMDetails(vm, lang, 100, 4)
		plain := tview.NewTextView().SetDynamicColors(true).SetText(options).GetText(true)
		if !strings.Contains(plain, vm.Config.VMX) {
			t.Fatal("VM options are no longer accessible")
		}
	}
}

func TestTUIHeaderAllocationCapacityAndStatus(t *testing.T) {
	uptime := int64(99*86400 + 9*3600 + 6*60)
	data := tuiData{host: &esxi.HostInfo{Name: "localhost.localdomain", NumCpuCores: 96, NumCpuThreads: 192, NumCpuPkgs: 2, MemoryMB: 1536 * 1024, UptimeSeconds: &uptime, ConnectionState: "connected"}, about: esxi.About{APIVersion: "8.0.0.2", Build: "21493926"}, stores: []esxi.DatastoreInfo{{Capacity: 4000 << 30, FreeSpace: 2500 << 30}}}
	for i := 0; i < 10; i++ {
		data.vms = append(data.vms, esxi.VMInfo{PowerState: "poweredOn", NumCPU: 11})
	}
	data.vms = append(data.vms, esxi.VMInfo{PowerState: "poweredOff", NumCPU: 99})
	for _, lang := range []language{"en", "zh"} {
		blocks := tuiHeaderText(data, lang)
		var plain [3]string
		for i, block := range blocks {
			plain[i] = tview.NewTextView().SetDynamicColors(true).SetText(block).GetText(true)
			lines := strings.Split(block, "\n")
			if len(lines) > 6 {
				t.Fatalf("%s column %d exceeds OpenWrt's two three-line metrics", lang, i)
			}
			for _, line := range lines {
				if tview.TaggedStringWidth(line) > 25 {
					t.Fatalf("%s header overflows compact column: %q", lang, line)
				}
			}
		}
		if !strings.Contains(plain[0], "10 / 11") || !strings.Contains(plain[0], "110 vCPU") || strings.Contains(plain[0], "209 vCPU") {
			t.Fatalf("wrong running VM allocation: %s", plain[0])
		}
		if !strings.Contains(plain[0], "build 21493926") || !strings.Contains(plain[1], "1536.0 GiB") || !strings.Contains(plain[2], "1500.0 / 4000.0 GiB") || !strings.Contains(plain[2], "37.5%") {
			t.Fatalf("missing version or capacity context: %v", plain)
		}
		compact := tuiHeaderText(data, lang, true)
		if !strings.Contains(compact[1], "1536.0 GiB") || !strings.Contains(compact[1], "192") {
			t.Fatal("short terminal lost CPU topology or total RAM")
		}
	}
	data.host.ConnectionState = "disconnected"
	if block := tuiHeaderText(data, "en")[2]; !strings.Contains(block, "[#ed5c5c]Disconnected") {
		t.Fatal("disconnected host still appears green")
	}
	data.stores = append(data.stores, esxi.DatastoreInfo{Capacity: 1, FreeSpace: 2})
	if block := tuiHeaderText(data, "en")[2]; strings.Contains(block, "37.5%") || !strings.Contains(block, "Incomplete storage data") {
		t.Fatal("partial datastore data was presented as a complete total")
	}
}
