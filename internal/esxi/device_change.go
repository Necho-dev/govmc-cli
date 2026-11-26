package esxi

import (
	"context"
	"strings"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type DiskExpansion struct {
	Key         int32 `json:"key"`
	CapacityMiB int64 `json:"capacityMiB"`
}

type NICChange struct {
	Key            int32  `json:"key"`
	Portgroup      string `json:"portgroup,omitempty"`
	Connected      *bool  `json:"connected,omitempty"`
	StartConnected *bool  `json:"startConnected,omitempty"`
}

type NewNIC struct {
	Portgroup      string `json:"portgroup"`
	Adapter        string `json:"adapter"`
	StartConnected bool   `json:"startConnected"`
}

func (ch VMChange) deviceChanges() bool {
	return len(ch.ExpandDisks) > 0 || ch.AddDiskMiB != nil || len(ch.EditNICs) > 0 || ch.AddNIC != nil
}

func validDiskMiB(mib int64) bool { return mib > 0 && mib <= 64*1024*1024 }

func (c *Client) buildDeviceChanges(ctx context.Context, vm *ResolvedVM, cfg *types.VirtualMachineConfigInfo, ch VMChange) ([]types.BaseVirtualDeviceConfigSpec, error) {
	online := vm.Info.PowerState == string(types.VirtualMachinePowerStatePoweredOn)
	if !online && vm.Info.PowerState != string(types.VirtualMachinePowerStatePoweredOff) {
		return nil, conflict("device configuration requires a powered-on or powered-off VM")
	}
	devices := object.VirtualDeviceList(cfg.Hardware.Device)
	changes := make([]types.BaseVirtualDeviceConfigSpec, 0)
	edit := func(device types.BaseVirtualDevice) {
		changes = append(changes, &types.VirtualDeviceConfigSpec{Operation: types.VirtualDeviceConfigSpecOperationEdit, Device: device})
	}
	seen := make(map[int32]bool)
	if len(ch.ExpandDisks) > 0 {
		var snapshot mo.VirtualMachine
		if err := vm.Obj.Properties(ctx, vm.Obj.Reference(), []string{"snapshot"}, &snapshot); err != nil {
			return nil, wrapVim(err)
		}
		if snapshot.Snapshot != nil && (snapshot.Snapshot.CurrentSnapshot != nil || len(snapshot.Snapshot.RootSnapshotList) > 0) {
			return nil, conflict("disk expansion requires a VM without snapshots")
		}
	}
	for _, change := range ch.ExpandDisks {
		disk, ok := devices.FindByKey(change.Key).(*types.VirtualDisk)
		if !ok || seen[change.Key] || !validDiskMiB(change.CapacityMiB) {
			return nil, badRequest("invalid disk expansion")
		}
		seen[change.Key] = true
		backing, ok := disk.Backing.(*types.VirtualDiskFlatVer2BackingInfo)
		if !ok || backing.Parent != nil || backing.Sharing == string(types.VirtualDiskSharingSharingMultiWriter) {
			return nil, conflict("disk expansion requires a non-shared base VMDK")
		}
		controller := devices.FindByKey(disk.ControllerKey)
		if scsi, ok := controller.(types.BaseVirtualSCSIController); ok {
			bus := scsi.GetVirtualSCSIController().SharedBus
			if bus != "" && bus != types.VirtualSCSISharingNoSharing {
				return nil, conflict("disk expansion on a shared SCSI bus is unavailable")
			}
		}
		if online {
			switch controller.(type) {
			case *types.ParaVirtualSCSIController, *types.VirtualNVMEController:
			default:
				return nil, conflict("online disk expansion requires a PVSCSI or NVMe controller")
			}
			if backing.DiskMode != string(types.VirtualDiskModePersistent) {
				return nil, conflict("online disk expansion requires a persistent VMDK")
			}
		}
		bytes := change.CapacityMiB * 1024 * 1024
		if bytes <= max(disk.CapacityInBytes, disk.CapacityInKB*1024) {
			return nil, badRequest("new disk capacity must exceed its current capacity")
		}
		disk.CapacityInBytes, disk.CapacityInKB = bytes, bytes/1024
		edit(disk)
	}
	if ch.AddDiskMiB != nil {
		if !validDiskMiB(*ch.AddDiskMiB) {
			return nil, badRequest("invalid capacity for new disk")
		}
		controller, unit, ok := freeSCSIUnit(devices)
		if !ok {
			return nil, conflict("adding a disk requires a free slot on an existing non-shared SCSI controller")
		}
		var home object.DatastorePath
		if !home.FromString(cfg.Files.VmPathName) {
			return nil, conflict("VM datastore is unavailable")
		}
		ds, err := c.Finder.Datastore(ctx, home.Datastore)
		if err != nil {
			return nil, wrapVim(err)
		}
		disk := devices.CreateDisk(nil, ds.Reference(), "")
		disk.Key, disk.ControllerKey, disk.UnitNumber = devices.NewKey(), controller, &unit
		disk.CapacityInBytes = *ch.AddDiskMiB * 1024 * 1024
		disk.CapacityInKB = disk.CapacityInBytes / 1024
		changes = append(changes, &types.VirtualDeviceConfigSpec{
			Operation: types.VirtualDeviceConfigSpecOperationAdd, FileOperation: types.VirtualDeviceConfigSpecFileOperationCreate, Device: disk,
		})
		devices = append(devices, disk)
	}
	for _, change := range ch.EditNICs {
		card := devices.FindByKey(change.Key)
		_, ok := card.(types.BaseVirtualEthernetCard)
		if !ok || seen[change.Key] || (change.Portgroup == "" && change.Connected == nil && change.StartConnected == nil) {
			return nil, badRequest("invalid network adapter change")
		}
		seen[change.Key] = true
		device := card.GetVirtualDevice()
		if change.Portgroup != "" {
			net, err := c.Finder.Network(ctx, change.Portgroup)
			if err != nil {
				return nil, wrapVim(err)
			}
			backing, err := net.EthernetCardBackingInfo(ctx)
			if err != nil {
				return nil, wrapVim(err)
			}
			device.Backing = backing
		}
		if change.Connected != nil || change.StartConnected != nil {
			if device.Connectable == nil || (change.Connected != nil && !online) {
				return nil, conflict("requested network adapter connection setting is unavailable")
			}
			if change.Connected != nil {
				device.Connectable.Connected = *change.Connected
			}
			if change.StartConnected != nil {
				device.Connectable.StartConnected = *change.StartConnected
			}
		}
		edit(card)
	}
	if ch.AddNIC != nil {
		if strings.TrimSpace(ch.AddNIC.Portgroup) == "" || (ch.AddNIC.Adapter != "vmxnet3" && ch.AddNIC.Adapter != "e1000e") {
			return nil, badRequest("select a portgroup and a VMXNET 3 or E1000e adapter")
		}
		net, err := c.Finder.Network(ctx, ch.AddNIC.Portgroup)
		if err != nil {
			return nil, wrapVim(err)
		}
		backing, err := net.EthernetCardBackingInfo(ctx)
		if err != nil {
			return nil, wrapVim(err)
		}
		card, err := object.EthernetCardTypes().CreateEthernetCard(ch.AddNIC.Adapter, backing)
		if err != nil {
			return nil, wrapVim(err)
		}
		device := card.GetVirtualDevice()
		device.Key = devices.NewKey()
		device.Connectable = &types.VirtualDeviceConnectInfo{
			StartConnected: ch.AddNIC.StartConnected, Connected: online && ch.AddNIC.StartConnected, AllowGuestControl: true,
		}
		changes = append(changes, &types.VirtualDeviceConfigSpec{Operation: types.VirtualDeviceConfigSpecOperationAdd, Device: card})
	}
	return changes, nil
}

func freeSCSIUnit(devices object.VirtualDeviceList) (int32, int32, bool) {
	for _, device := range devices {
		controller, ok := device.(types.BaseVirtualSCSIController)
		if !ok {
			continue
		}
		scsi := controller.GetVirtualSCSIController()
		if scsi.SharedBus != "" && scsi.SharedBus != types.VirtualSCSISharingNoSharing {
			continue
		}
		used := map[int32]bool{scsi.ScsiCtlrUnitNumber: true}
		for _, device := range devices {
			child := device.GetVirtualDevice()
			if child.ControllerKey == scsi.Key && child.UnitNumber != nil {
				used[*child.UnitNumber] = true
			}
		}
		for unit := int32(0); unit < 16; unit++ {
			if !used[unit] {
				return scsi.Key, unit, true
			}
		}
	}
	return 0, 0, false
}
