package esxi

import (
	"context"
	"strings"

	"github.com/vmware/govmomi/vim25/types"
)

// CounterSamples queries only aggregate counters advertised for this host or VM.
// Small batches keep queries within endpoint performance-query limits.
func (c *Client) CounterSamples(ctx context.Context, vmID string) ([]CounterInfo, *MetricsResponse, error) {
	var ref types.ManagedObjectReference
	kind := "host"
	if vmID == "" {
		host, err := c.Finder.DefaultHostSystem(ctx)
		if err != nil {
			return nil, nil, wrapVim(err)
		}
		ref = host.Reference()
	} else {
		vm, err := c.GetVM(ctx, vmID)
		if err != nil {
			return nil, nil, err
		}
		ref = vm.Obj.Reference()
		kind = "vm"
	}
	available, err := c.Perf.AvailableMetric(ctx, ref, 20)
	if err != nil {
		return nil, nil, wrapVim(err)
	}
	info, err := c.Perf.CounterInfoByKey(ctx)
	if err != nil {
		return nil, nil, wrapVim(err)
	}
	names := map[string]bool{}
	for _, metric := range available {
		if metric.Instance != "" {
			continue
		}
		if counter := info[metric.CounterId]; counter != nil {
			names[counter.Name()] = true
		}
	}
	catalog, err := c.Counters(ctx, kind)
	if err != nil {
		return nil, nil, err
	}
	counters := make([]CounterInfo, 0, len(catalog))
	queryNames := make([]string, 0, len(catalog))
	for _, counter := range catalog {
		if names[counter.Name] {
			counters = append(counters, counter)
			queryNames = append(queryNames, counter.Name)
		}
	}
	merged := &MetricsResponse{Series: []MetricSeriesOut{}}
	for start := 0; start < len(queryNames); start += 32 {
		query := MetricsQuery{Names: queryNames[start:min(start+32, len(queryNames))], MaxSample: 12}
		var samples *MetricsResponse
		if vmID == "" {
			samples, err = c.HostMetrics(ctx, query)
		} else {
			samples, err = c.VMMetrics(ctx, vmID, query)
		}
		if err != nil {
			return counters, merged, err
		}
		merged.Entity, merged.WindowNote, merged.Units = samples.Entity, samples.WindowNote, samples.Units
		for i := range samples.Series {
			series := &samples.Series[i]
			// Existing CPU usage conversion already divides hundredths of a percent.
			if series.RawUnit == "percent" && !strings.HasPrefix(series.Name, "cpu.usage") {
				for j := range series.Points {
					series.Points[j].Value /= 100
				}
			}
		}
		merged.Series = append(merged.Series, samples.Series...)
	}
	return counters, merged, nil
}
