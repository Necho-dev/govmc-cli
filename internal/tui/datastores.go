package tui

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
)

type tuiBrowseState struct {
	datastore, directory, filter       string
	selection, offsetRow, offsetColumn int
	filterVisible                      bool
}

func tuiDatastoreSpace(ds esxi.DatastoreInfo) (used int64, percent float64, valid bool) {
	if (ds.Accessible != nil && !*ds.Accessible) || ds.Capacity <= 0 || ds.FreeSpace < 0 || ds.FreeSpace > ds.Capacity {
		return 0, 0, false
	}
	used = ds.Capacity - ds.FreeSpace
	return used, float64(used) / float64(ds.Capacity) * 100, true
}

func tuiDatastoreRow(ds esxi.DatastoreInfo) []string {
	total, used, free, percent := "—", "—", "—", "—"
	if bytes, pct, ok := tuiDatastoreSpace(ds); ok {
		total = fmt.Sprintf("%.1f", float64(ds.Capacity)/(1<<30))
		used = fmt.Sprintf("%.1f", float64(bytes)/(1<<30))
		free = fmt.Sprintf("%.1f", float64(ds.FreeSpace)/(1<<30))
		percent = fmt.Sprintf("%.1f%%", pct)
	}
	return []string{ds.Name, ds.Type, total, used, free, percent, fmt.Sprint(ds.VMCount)}
}

func tuiDatastoreDetails(ds esxi.DatastoreInfo, width int, lang language) string {
	row := tuiDatastoreRow(ds)
	accessible := lang.Text("Unknown")
	if ds.Accessible != nil {
		accessible = lang.Text("Accessible")
		if !*ds.Accessible {
			accessible = lang.Text("Inaccessible")
		}
	}
	maintenance := ds.MaintenanceMode
	if maintenance == "" {
		maintenance = "—"
	} else {
		if label, ok := map[string]string{"normal": "Normal", "enteringMaintenance": "Entering maintenance", "inMaintenance": "In maintenance"}[maintenance]; ok {
			maintenance = lang.Text(label)
		}
	}
	extra, maxFile, maxDisk := "—", "—", "—"
	if ds.Accessible == nil || *ds.Accessible {
		if ds.Uncommitted != nil {
			extra = tuiGiB(float64(*ds.Uncommitted))
		}
		if ds.MaxFileSize > 0 {
			maxFile = tuiGiB(float64(ds.MaxFileSize))
		}
		if ds.MaxVirtualDiskCapacity > 0 {
			maxDisk = tuiGiB(float64(ds.MaxVirtualDiskCapacity))
		}
	}
	location := ds.URL
	if ds.Accessible != nil && !*ds.Accessible {
		location = "—"
	}
	health := map[string]string{"green": "Healthy", "yellow": "Warning", "red": "Critical", "gray": "Unknown"}[ds.Status]
	if health == "" {
		health = "Unknown"
	}
	text := tuiInspector(ds.Name, [][2]string{
		{lang.Text("Type"), ds.Type}, {lang.Text("Accessibility"), accessible},
		{lang.Text("Health"), lang.Text(health)}, {lang.Text("Maintenance state"), maintenance},
		{lang.Text("Capacity (GiB)"), row[2]}, {lang.Text("Used (GiB)"), row[3]}, {lang.Text("Free (GiB)"), row[4]},
		{lang.Text("Usage"), row[5]}, {lang.Text("Machines"), row[6]},
		{lang.Text("Potential growth"), extra}, {lang.Text("Max file"), maxFile}, {lang.Text("Max virtual disk"), maxDisk},
		{lang.Text("VMFS version"), ds.VMFSVersion}, {lang.Text("Updated"), tuiFileTime(ds.UpdatedAt, true)},
		{lang.Text("Location"), location},
	}, width)
	return text + "\n" + tuiColor("#9ba7b0", lang.Text("Enter to browse"))
}

func tuiSortedFiles(files []esxi.DSFile) []esxi.DSFile {
	sorted := slices.Clone(files)
	slices.SortStableFunc(sorted, func(a, b esxi.DSFile) int {
		if a.Folder != b.Folder {
			if a.Folder {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
	})
	return sorted
}

func tuiFileKind(file esxi.DSFile, lang language) string {
	if file.Folder {
		return lang.Text("Folder")
	}
	switch strings.ToLower(path.Ext(file.Path)) {
	case ".vmdk":
		return lang.Text("Virtual disk")
	case ".vmx":
		return lang.Text("VM config")
	case ".iso":
		return lang.Text("ISO image")
	case ".log":
		return lang.Text("Log file")
	case ".nvram":
		return lang.Text("Firmware")
	case ".vmsn", ".vmsd":
		return lang.Text("Snapshot")
	}
	return lang.Text("File")
}

func tuiFileSize(file esxi.DSFile) string {
	if file.Folder || file.Size < 0 {
		return "—"
	}
	value := float64(file.Size)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d B", file.Size)
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func tuiFileTime(value string, full bool) string {
	if value == "" {
		return "—"
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "—"
	}
	if full {
		return parsed.Local().Format("2006-01-02 15:04:05")
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

func tuiFileDetails(file esxi.DSFile, datastore, directory string, width int, lang language) string {
	return tuiInspector(file.Path, [][2]string{
		{lang.Text("Type"), tuiFileKind(file, lang)}, {lang.Text("Size"), tuiFileSize(file)},
		{lang.Text("Modified"), tuiFileTime(file.Modified, true)},
		{lang.Text("Location"), "[" + datastore + "] " + path.Join(directory, file.Path)},
	}, width)
}
