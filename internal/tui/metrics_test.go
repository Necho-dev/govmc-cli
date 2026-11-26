package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/rivo/tview"
)

func TestCounterLatestUsesNewestValidAggregateSample(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	counter := esxi.CounterInfo{Name: "cpu.usage.average", Unit: "percent"}
	metrics := &esxi.MetricsResponse{Series: []esxi.MetricSeriesOut{
		{Name: counter.Name, Unit: "percent", Points: []esxi.MetricSample{
			{TimeUTC: now, Value: 12.5}, {TimeUTC: now.Add(-time.Minute), Value: 4},
			{TimeUTC: now.Add(time.Minute), Value: -1}, {TimeUTC: now.Add(2 * time.Minute), Value: math.NaN()},
		}},
		{Name: counter.Name, Instance: "0", Unit: "percent", Points: []esxi.MetricSample{{TimeUTC: now.Add(3 * time.Minute), Value: 99}}},
	}}
	value, unit, sampled := tuiCounterLatest(counter, metrics)
	if value != "12.50" || unit != "%" || sampled != now.Local().Format("15:04:05") {
		t.Fatalf("incorrect value: %s %s %s", value, unit, sampled)
	}
	metrics.Series[0].Points = []esxi.MetricSample{{TimeUTC: now, Value: 0}}
	value, _, _ = tuiCounterLatest(counter, metrics)
	if value != "0.00" {
		t.Fatal("zero is a valid sample")
	}
	value, _, sampled = tuiCounterLatest(counter, nil)
	if value != "—" || sampled != "—" {
		t.Fatal("missing samples fabricated values")
	}
	text := tuiCounterDetails(counter, metrics, "", 45, "en")
	if !strings.Contains(text, "0.00") || strings.Contains(text, "Enter to select") {
		t.Fatal("inspector hides values behind another action")
	}
}

func TestCompactVMChartsFitSideBySide(t *testing.T) {
	for _, size := range [][2]int{{128, 12}, {128, 14}, {106, 10}, {80, 12}} {
		text := tuiCompactTrends(nil, "", size[0], size[1], "en")
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		if len(lines) > size[1] {
			t.Fatalf("%v overflowed height: %d", size, len(lines))
		}
		for _, line := range lines {
			if tview.TaggedStringWidth(line) > size[0] {
				t.Fatalf("%v overflowed width: %d", size, tview.TaggedStringWidth(line))
			}
		}
		for _, label := range []string{"CPU", "Active memory", "↓ Receive", "↑ Transmit", "↓ Read", "↑ Write"} {
			if !strings.Contains(text, label) {
				t.Fatalf("%v omitted %s", size, label)
			}
		}
	}
}
