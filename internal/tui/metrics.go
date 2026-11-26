package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/rivo/tview"
)

func tuiCounterLatest(counter esxi.CounterInfo, metrics *esxi.MetricsResponse) (string, string, string) {
	value, unit, sampled := "—", counter.Unit, "—"
	var latest time.Time
	if metrics != nil {
		for _, series := range metrics.Series {
			if series.Name != counter.Name || series.Instance != "" {
				continue
			}
			unit = series.Unit
			for _, point := range series.Points {
				if !tuiValidSample(point) || (!latest.IsZero() && point.TimeUTC.Before(latest)) {
					continue
				}
				latest = point.TimeUTC
				value = fmt.Sprintf("%.2f", point.Value)
				if len(value) > 11 {
					value = fmt.Sprintf("%.3g", point.Value)
				}
				sampled = latest.Local().Format("15:04:05")
			}
		}
	}
	unit = strings.NewReplacer("kiloBytesPerSecond", "KB/s", "millisecond", "ms", "microsecond", "µs", "kiloBytes", "KB", "megaHertz", "MHz", "megaBytes", "MB", "gigaBytes", "GB", "byte", "B", "percent", "%", "number", "count").Replace(unit)
	return value, unit, sampled
}

func tuiValidSample(point esxi.MetricSample) bool {
	return !point.TimeUTC.IsZero() && point.Value >= 0 && !math.IsNaN(point.Value) && !math.IsInf(point.Value, 0)
}

// On wide terminals the six VM plots form three paired columns, leaving room for
// the VM list. Narrow terminals retain a shallow stacked layout.
func tuiCompactTrends(metrics *esxi.MetricsResponse, warning string, width, height int, lang language) string {
	if width < 105 {
		return tuiTrends(metrics, warning, width, height, lang)
	}
	note := ""
	if warning != "" {
		note = "  " + tuiColor("#f7b65c", warning) + "\n"
		height--
	}
	columnWidth := (width - 4) / 3
	columns := [3][]string{}
	for i := range columns {
		columns[i] = strings.Split(strings.TrimSuffix(tuiTrendGroups(metrics, "", columnWidth, height, lang, i), "\n"), "\n")
	}
	var out strings.Builder
	out.WriteString(note)
	for y := 0; y < height; y++ {
		for i := range columns {
			line := ""
			if y < len(columns[i]) {
				line = columns[i][y]
			}
			out.WriteString(line)
			if i < 2 {
				out.WriteString(strings.Repeat(" ", max(0, columnWidth-tview.TaggedStringWidth(line))) + "  ")
			}
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func tuiVMSummary(vm esxi.VMInfo, lang language, width int) string {
	return tuiInspector(lang.Text("System / status"), [][2]string{
		{"ID", vm.ID},
		{lang.Text("Power / connection"), lang.Power(vm.PowerState) + " / " + lang.Text(vm.ConnectionState)},
		{lang.Text("Operating system"), tuiOSName(vm, lang)},
		{"IP", vm.Guest.IPAddress},
		{lang.Text("Tools"), tuiTools(vm.Guest, lang)},
		{lang.Text("Specifications"), fmt.Sprintf("%d vCPU / %.1f GiB", vm.NumCPU, float64(vm.MemoryMB)/1024)},
		{"CPU / " + lang.Text("Active %"), tuiNumber(vm.CPUPct, "%") + " / " + tuiNumber(vm.MemActivePct, "%")},
		{lang.Text("Disk used / cap (GiB)"), tuiTableDisk(vm)},
	}, width)
}
