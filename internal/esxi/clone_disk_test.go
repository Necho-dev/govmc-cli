package esxi

import (
	"github.com/vmware/govmomi/vim25/types"
	"testing"
)

func TestCloneDiskProvisioningSpec(t *testing.T) {
	for _, tc := range []struct {
		name        string
		thin, eager *bool
		kind        string
	}{
		{"thin", types.NewBool(true), types.NewBool(false), "thin"},
		{"lazy", types.NewBool(false), types.NewBool(false), "preallocated"},
		{"eager", types.NewBool(false), types.NewBool(true), "eagerZeroedThick"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Disk{Thin: tc.thin, EagerlyScrub: tc.eager, ControllerKey: 200}
			spec := cloneDiskSpec(d, []types.BaseVirtualDevice{&types.VirtualIDEController{VirtualController: types.VirtualController{VirtualDevice: types.VirtualDevice{Key: 200}}}})
			if spec.DiskType != tc.kind || spec.AdapterType != "ide" {
				t.Fatalf("unexpected disk copy spec: %+v", spec)
			}
		})
	}
}
