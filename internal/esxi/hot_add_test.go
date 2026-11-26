package esxi

import (
	"testing"

	"github.com/vmware/govmomi/vim25/types"
)

func TestValidateHotAdd(t *testing.T) {
	on := string(types.VirtualMachinePowerStatePoweredOn)
	cfg := &types.VirtualMachineConfigInfo{
		CpuHotAddEnabled:    types.NewBool(true),
		MemoryHotAddEnabled: types.NewBool(false),
		Hardware:            types.VirtualHardware{NumCPU: 4, MemoryMB: 8192},
	}
	five, four, three, memory := int32(5), int32(4), int32(3), int32(16384)
	for _, tc := range []struct {
		name, power string
		change      VMChange
		valid       bool
	}{
		{"increase CPU", on, VMChange{NumCPU: &five}, true},
		{"unchanged CPU", on, VMChange{NumCPU: &four}, false},
		{"decrease CPU", on, VMChange{NumCPU: &three}, false},
		{"memory hot-add disabled", on, VMChange{MemoryMB: &memory}, false},
		{"unsupported online field", on, VMChange{NumCPU: &five, IOMMU: types.NewBool(false)}, false},
		{"stale power state", "poweredOff", VMChange{NumCPU: &five}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateHotAdd(tc.power, cfg, tc.change); (err == nil) != tc.valid {
				t.Fatalf("valid = %t, error = %v", tc.valid, err)
			}
		})
	}
	cfg.MemoryHotAddEnabled = types.NewBool(true)
	if err := validateHotAdd(on, cfg, VMChange{MemoryMB: &memory}); err != nil {
		t.Fatal(err)
	}
}
