package tui

import (
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
)

func TestDatastoreCapacityAndUnavailableValues(t *testing.T) {
	accessible := true
	ds := esxi.DatastoreInfo{Name: "store", Type: "VMFS", Capacity: 100 << 30, FreeSpace: 25 << 30, Accessible: &accessible, VMCount: 4}
	row := tuiDatastoreRow(ds)
	if row[2] != "100.0" || row[3] != "75.0" || row[4] != "25.0" || row[5] != "75.0%" || row[6] != "4" {
		t.Fatal(row)
	}
	text := tuiDatastoreDetails(ds, 42, "en")
	if !strings.Contains(text, "Accessible") || !strings.Contains(text, "75.0%") {
		t.Fatal("missing capacity and status")
	}
	accessible = false
	row = tuiDatastoreRow(ds)
	if row[2] != "—" || row[3] != "—" || row[4] != "—" || row[5] != "—" {
		t.Fatal("inaccessible storage displayed stale capacity")
	}
	accessible = true
	ds.FreeSpace = ds.Capacity + 1
	if _, _, valid := tuiDatastoreSpace(ds); valid {
		t.Fatal("invalid capacity accepted")
	}
}

func TestBrowserFoldersFirstAndFileMetadata(t *testing.T) {
	files := []esxi.DSFile{{Path: "z.vmdk", Size: 2 << 30}, {Path: "B", Folder: true}, {Path: "a.vmx", Size: 0}, {Path: "a", Folder: true}}
	sorted := tuiSortedFiles(files)
	if sorted[0].Path != "a" || sorted[1].Path != "B" || sorted[2].Path != "a.vmx" || sorted[3].Path != "z.vmdk" {
		t.Fatal(sorted)
	}
	if files[0].Path != "z.vmdk" {
		t.Fatal("sorting changed inventory data")
	}
	if tuiFileKind(sorted[3], "en") != "Virtual disk" || tuiFileSize(sorted[3]) != "2.0 GiB" {
		t.Fatal("disk type or size missing")
	}
	if tuiFileKind(sorted[0], "en") != "Folder" || tuiFileSize(sorted[0]) != "—" {
		t.Fatal("folder presented as file")
	}
	if tuiFileSize(sorted[2]) != "0 B" {
		t.Fatal("empty files should show zero size")
	}
	if tuiFileTime("", true) != "—" || tuiFileTime("invalid", false) != "—" {
		t.Fatal("missing timestamps fabricated a date")
	}
	if !strings.Contains(tuiFileDetails(sorted[3], "ds", "folder", 40, "en"), "folder/z.vmdk") {
		t.Fatal("full location missing")
	}
}
