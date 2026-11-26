package esxi

import (
	"context"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"strings"
)

// CopyCloneDisk copies a complete base virtual disk including hidden extents.
// Native no-overwrite semantics protect existing disks on both datastores.
func (c *Client) CopyCloneDisk(ctx context.Context, sourceStore, targetStore, sourcePath, targetPath string, confirm bool, spec *types.VirtualDiskSpec) error {
	if err := RequireConfirm(confirm); err != nil {
		return err
	}
	src, err := CleanRel(sourcePath)
	if err != nil {
		return err
	}
	dst, err := CleanRel(targetPath)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(src, ".vmdk") || !strings.HasSuffix(dst, ".vmdk") || sourceStore == targetStore && src == dst {
		return badRequest("distinct VMDK paths are required")
	}
	if err = c.guardFS(ctx, sourceStore, src); err != nil {
		return err
	}
	if err = c.guardFS(ctx, targetStore, dst); err != nil {
		return err
	}
	source, err := c.Finder.Datastore(ctx, sourceStore)
	if err != nil {
		return wrapVim(err)
	}
	target, err := c.Finder.Datastore(ctx, targetStore)
	if err != nil {
		return wrapVim(err)
	}
	if c.Gov.ServiceContent.VirtualDiskManager == nil {
		return conflict("native virtual disk copy is unavailable")
	}
	manager := object.NewVirtualDiskManager(c.Gov.Client)
	task, err := manager.CopyVirtualDisk(ctx, source.Path(src), nil, target.Path(dst), nil, spec, false)
	if err == nil {
		err = waitFileTask(ctx, task)
	}
	return wrapVim(err)
}

func cloneDiskSpec(disk Disk, devices []types.BaseVirtualDevice) *types.VirtualDiskSpec {
	spec := &types.VirtualDiskSpec{DiskType: string(types.VirtualDiskTypePreallocated), AdapterType: string(types.VirtualDiskAdapterTypeLsiLogic)}
	if disk.Thin != nil && *disk.Thin {
		spec.DiskType = string(types.VirtualDiskTypeThin)
	} else if disk.EagerlyScrub != nil && *disk.EagerlyScrub {
		spec.DiskType = string(types.VirtualDiskTypeEagerZeroedThick)
	}
	for _, device := range devices {
		if device.GetVirtualDevice().Key != disk.ControllerKey {
			continue
		}
		switch device.(type) {
		case *types.VirtualIDEController:
			spec.AdapterType = string(types.VirtualDiskAdapterTypeIde)
		case *types.VirtualBusLogicController:
			spec.AdapterType = string(types.VirtualDiskAdapterTypeBusLogic)
		}
	}
	return spec
}
