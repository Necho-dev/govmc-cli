package esxi

import (
	"context"
	"io"
	"path"
	"strings"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// PrepareClone performs read-only checks and resolves all inherited values.
// ExecuteClone repeats these checks immediately before any device mutation.
func (c *Client) PrepareClone(ctx context.Context, s CloneSpec) (CloneSpec, *ClonePlan, error) {
	requestedReservation := s.MemoryReservationMB
	reservationProvided := s.MemoryReservationMB != nil
	reservationExplicit := s.MemoryReservationMB != nil
	fail := func(e error) (CloneSpec, *ClonePlan, error) { return s, nil, e }
	if s.Src == "" || s.Dst == "" || strings.ContainsAny(s.Dst, "/\\\x00\r\n") || s.Dst == "." || s.Dst == ".." || strings.TrimSpace(s.Dst) != s.Dst {
		return fail(badRequest("source and target name are required; target must be a single directory name"))
	}
	vm, err := c.GetVM(ctx, s.Src)
	if err != nil {
		return fail(err)
	}
	if vm.Info.PowerState != "poweredOff" || vm.Info.ConnectionState != "connected" {
		return fail(conflict("clone source must be powered off and connected"))
	}
	var full mo.VirtualMachine
	if err = vm.Obj.Properties(ctx, vm.Obj.Reference(), []string{"config", "snapshot"}, &full); err != nil {
		return fail(wrapVim(err))
	}
	cfg := full.Config
	if cfg == nil {
		return fail(conflict("source configuration unavailable"))
	}
	if cfg.KeyId != nil {
		return fail(conflict("encrypted clone sources are not supported"))
	}
	if full.Snapshot != nil && (full.Snapshot.CurrentSnapshot != nil || len(full.Snapshot.RootSnapshotList) > 0) {
		return fail(conflict("clone source must not have snapshots"))
	}
	var home object.DatastorePath
	if !home.FromString(cfg.Files.VmPathName) {
		return fail(conflict("source VMX datastore is unavailable"))
	}
	if s.Datastore == "" {
		s.Datastore = home.Datastore
	}
	ds, err := c.Finder.Datastore(ctx, s.Datastore)
	if err != nil {
		return fail(wrapVim(err))
	}
	s.Datastore = ds.Name()
	if home.Datastore != s.Datastore {
		return fail(conflict("source datastore must match the VMX datastore"))
	}
	targetName := s.TargetDatastore()
	destination, err := c.Finder.Datastore(ctx, targetName)
	if err != nil {
		return fail(wrapVim(err))
	}
	s.DestinationDatastore = destination.Name()
	folder := path.Dir(home.Path)
	prefix := strings.TrimSuffix(path.Base(home.Path), ".vmx")
	if folder == "." || s.Datastore == s.TargetDatastore() && (folder == s.Dst || strings.HasPrefix(s.Dst, folder+"/")) {
		return fail(conflict("source and destination must be separate directories"))
	}
	if s.SourceVersion != "" && s.SourceVersion != cfg.ChangeVersion {
		return fail(conflict("source configuration changed; preview again"))
	}
	s.SourceVersion = cfg.ChangeVersion
	s.Src = vm.Info.ID
	s.SrcFolder = folder
	s.SrcPrefix = prefix
	all, err := c.ListVMs(ctx, false)
	if err != nil {
		return fail(err)
	}
	for _, v := range all {
		if v.Name == s.Dst {
			return fail(conflict("target virtual machine already exists"))
		}
	}
	root, err := c.DatastoreFiles(ctx, s.TargetDatastore(), "")
	if err != nil {
		return fail(err)
	}
	for _, f := range root {
		if strings.TrimSuffix(f.Path, "/") == s.Dst {
			return fail(conflict("destination already exists"))
		}
	}
	files, err := c.DatastoreFiles(ctx, s.Datastore, folder)
	if err != nil {
		return fail(err)
	}
	var bytes int64
	vmxFiles := 0
	for _, f := range files {
		if f.Folder {
			return fail(conflict("source contains nested directories; unsupported clone layout"))
		}
		bytes += f.Size
		if strings.HasSuffix(strings.ToLower(f.Path), ".vmx") {
			vmxFiles++
		}
		if strings.HasSuffix(f.Path, ".vmsn") || strings.Contains(f.Path, "-delta.vmdk") || strings.Contains(f.Path, "-sesparse.vmdk") {
			return fail(conflict("source contains snapshot files"))
		}
	}
	if vmxFiles != 1 {
		return fail(conflict("source must contain exactly one VMX file"))
	}
	stores, err := c.Datastores(ctx)
	if err != nil {
		return fail(err)
	}
	for _, dev := range cfg.Hardware.Device {
		switch dev.(type) {
		case *types.VirtualPCIPassthrough, *types.VirtualSriovEthernetCard:
			return fail(conflict("passthrough devices require a separate clone workflow"))
		}
		if _, ok := dev.(*types.VirtualTPM); ok {
			return fail(conflict("vTPM clone sources require a separate identity workflow"))
		}
		if disk, ok := dev.(*types.VirtualDisk); ok {
			b, ok := disk.Backing.(*types.VirtualDiskFlatVer2BackingInfo)
			if !ok || b.Parent != nil || b.Sharing == string(types.VirtualDiskSharingSharingMultiWriter) {
				return fail(conflict("clone requires non-shared base VMDK disks"))
			}
			var diskPath object.DatastorePath
			if !diskPath.FromString(b.FileName) || diskPath.Datastore != home.Datastore || path.Dir(diskPath.Path) != folder {
				return fail(conflict("external disk references are not supported"))
			}
			rc, _, e := ds.Download(ctx, diskPath.Path, &soap.Download{})
			if e != nil {
				return fail(wrapVim(e))
			}
			raw, e := io.ReadAll(io.LimitReader(rc, (1<<20)+1))
			rc.Close()
			if e != nil {
				return fail(e)
			}
			text := strings.ToLower(string(raw))
			if len(raw) > 1<<20 || !strings.Contains(text, `createtype="vmfs"`) || !strings.Contains(text, "parentcid=ffffffff") || strings.Contains(text, "parentfilenamehint") {
				return fail(conflict("unsupported VMDK descriptor or snapshot chain"))
			}
			extents := parseVmdkExtents(string(raw))
			if len(extents) != 1 || extents[0] != strings.TrimSuffix(path.Base(diskPath.Path), ".vmdk")+"-flat.vmdk" {
				return fail(conflict("only single-extent VMFS base disks are supported"))
			}
			for _, extent := range extents {
				if path.Base(extent) != extent {
					return fail(conflict("external disk extent is unsupported"))
				}
			}
		}
	}
	seenExpansions := map[int32]bool{}
	for _, change := range s.ExpandDisks {
		if seenExpansions[change.Key] || !validDiskMiB(change.CapacityMiB) {
			return fail(badRequest("invalid or duplicate disk expansion"))
		}
		seenExpansions[change.Key] = true
		found := false
		for _, d := range vm.Info.Disks {
			if d.Key == change.Key {
				found = true
				if change.CapacityMiB*1024 <= d.Capacity {
					return fail(badRequest("disk expansion must increase capacity"))
				}
				bytes += (change.CapacityMiB*1024 - d.Capacity) * 1024
			}
		}
		if !found {
			return fail(badRequest("unknown disk expansion key"))
		}
	}
	for _, d := range stores {
		if d.Name == s.Datastore && d.Accessible != nil && !*d.Accessible {
			return fail(conflict("source datastore is inaccessible"))
		}
		if d.Name == s.TargetDatastore() && (d.Accessible != nil && !*d.Accessible || d.FreeSpace < bytes) {
			return fail(conflict("destination datastore is inaccessible or has insufficient space"))
		}
	}
	if s.LockMemory != nil && !*s.LockMemory && s.MemoryReservationMB == nil {
		x := int64(0)
		s.MemoryReservationMB = &x
	}
	if s.NumCPU == nil {
		x := cfg.Hardware.NumCPU
		s.NumCPU = &x
	}
	if s.MemoryMB == nil {
		x := cfg.Hardware.MemoryMB
		s.MemoryMB = &x
	}
	if *s.NumCPU <= 0 || *s.NumCPU > 768 || *s.MemoryMB <= 0 {
		return fail(badRequest("CPU and memory must be positive"))
	}
	if cfg.Hardware.NumCoresPerSocket != nil && *cfg.Hardware.NumCoresPerSocket > 0 && *s.NumCPU%*cfg.Hardware.NumCoresPerSocket != 0 {
		return fail(badRequest("CPU count must be divisible by inherited cores per socket"))
	}
	if s.CPUHotAdd == nil {
		s.CPUHotAdd = cfg.CpuHotAddEnabled
	}
	if s.MemoryHotAdd == nil {
		s.MemoryHotAdd = cfg.MemoryHotAddEnabled
	}
	if s.LockMemory == nil {
		s.LockMemory = cfg.MemoryReservationLockedToMax
	}
	if s.MemoryReservationMB == nil && cfg.MemoryAllocation != nil {
		s.MemoryReservationMB = cfg.MemoryAllocation.Reservation
	}
	if boolDefault(s.LockMemory, false) {
		reservation := int64(*s.MemoryMB)
		if requestedReservation != nil && *requestedReservation != reservation {
			return fail(badRequest("locked memory reservation must equal configured memory"))
		}
		s.MemoryReservationMB = &reservation
	}
	if s.MemoryReservationMB != nil && (*s.MemoryReservationMB < 0 || *s.MemoryReservationMB > int64(*s.MemoryMB)) {
		return fail(badRequest("invalid memory reservation"))
	}
	if boolDefault(s.LockMemory, false) {
		reserve := int64(*s.MemoryMB)
		if reservationProvided && s.MemoryReservationMB != nil && *s.MemoryReservationMB != reserve {
			return fail(badRequest("locked memory requires reservation equal to configured memory"))
		}
		s.MemoryReservationMB = &reserve
	}
	if s.MemoryReservationMB != nil && (*s.MemoryReservationMB < 0 || *s.MemoryReservationMB > int64(*s.MemoryMB)) {
		return fail(badRequest("memory reservation must be between zero and configured memory"))
	}
	if boolDefault(s.LockMemory, false) {
		reserve := int64(*s.MemoryMB)
		if reservationExplicit && s.MemoryReservationMB != nil && *s.MemoryReservationMB != reserve {
			return fail(badRequest("locked memory reservation must equal configured memory"))
		}
		s.MemoryReservationMB = &reserve
	}
	if s.MemoryReservationMB != nil && (*s.MemoryReservationMB < 0 || *s.MemoryReservationMB > int64(*s.MemoryMB)) {
		return fail(badRequest("memory reservation cannot exceed configured memory"))
	}
	if s.PowerOn == nil {
		x := false
		s.PowerOn = &x
	}
	if s.Portgroup != "" {
		if len(vm.Info.NICs) > 1 {
			return fail(conflict("portgroup override requires a single-NIC source; edit individual NICs after cloning"))
		}
		if s.NicAdapter == "" && len(vm.Info.NICs) == 1 {
			s.NicAdapter = strings.ToLower(strings.TrimPrefix(vm.Info.NICs[0].Adapter, "Virtual"))
		}
		networks, e := c.Networks(ctx)
		if e != nil {
			return fail(e)
		}
		found := false
		for _, n := range networks {
			if n.Name == s.Portgroup {
				found = true
			}
		}
		if !found {
			return fail(badRequest("unknown portgroup"))
		}
	}
	plan, err := c.BuildClonePlanFromSpec(ctx, s)
	if err != nil {
		return fail(err)
	}
	plan.Configuration = &s
	plan.Note = "Full standalone ESXi directory clone; inherited configuration; new UUID/MAC; no guest OS identity customization. Cancellation is not rollback."
	return s, plan, nil
}

func (c *Client) VerifyClone(ctx context.Context, s CloneSpec) (*VMInfo, error) {
	value, e := c.DescribeVM(ctx, s.Dst)
	if e != nil {
		return nil, e
	}
	dst := &value
	src, e := c.GetVM(ctx, s.Src)
	if e != nil {
		return dst, e
	}
	if dst.NumCPU != *s.NumCPU || dst.MemoryMB != *s.MemoryMB {
		return dst, conflict("clone CPU/memory verification failed")
	}
	if dst.InstanceUUID == "" || dst.InstanceUUID == src.Info.InstanceUUID {
		return dst, conflict("clone UUID verification failed")
	}
	if dst.Config == nil {
		return dst, conflict("clone configuration verification unavailable")
	}
	var vmx object.DatastorePath
	if !vmx.FromString(dst.Config.VMX) || vmx.Datastore != s.TargetDatastore() || path.Dir(vmx.Path) != s.Dst {
		return dst, conflict("clone VMX backing verification failed")
	}
	for _, pair := range [][2]*bool{{s.CPUHotAdd, dst.Config.CPUHotAdd}, {s.MemoryHotAdd, dst.Config.MemoryHotAdd}, {s.LockMemory, dst.Config.MemoryLocked}} {
		if pair[0] != nil && (pair[1] == nil || *pair[0] != *pair[1]) {
			return dst, conflict("clone hot add or memory lock verification failed")
		}
	}
	if s.MemoryReservationMB != nil && (dst.Config.MemoryReservationMB == nil || *s.MemoryReservationMB != *dst.Config.MemoryReservationMB) {
		return dst, conflict("clone memory reservation verification failed")
	}
	expectedNICs := len(src.Info.NICs)
	if s.Portgroup != "" && s.ReplaceNic != nil && !*s.ReplaceNic {
		expectedNICs++
	}
	if len(dst.NICs) != expectedNICs {
		return dst, conflict("clone network count verification failed")
	}
	if s.Portgroup != "" {
		found := false
		for _, nic := range dst.NICs {
			if nic.Portgroup == s.Portgroup {
				found = true
			}
		}
		if !found {
			return dst, conflict("clone network backing verification failed")
		}
	}
	seen := map[string]bool{}
	all, e := c.ListVMs(ctx, false)
	if e != nil {
		return dst, e
	}
	for _, v := range all {
		if v.ID != dst.ID {
			if v.InstanceUUID != "" && v.InstanceUUID == dst.InstanceUUID || v.BiosUUID != "" && dst.BiosUUID != "" && v.BiosUUID == dst.BiosUUID {
				return dst, conflict("clone UUID collision")
			}
			for _, nic := range v.NICs {
				if nic.MAC != "" {
					seen[strings.ToLower(nic.MAC)] = true
				}
			}
		}
	}
	for _, nic := range dst.NICs {
		mac := strings.ToLower(nic.MAC)
		if mac != "" && seen[mac] {
			return dst, conflict("clone MAC verification failed")
		}
		seen[mac] = true
	}
	if len(dst.Disks) != len(src.Info.Disks) {
		return dst, conflict("clone disk count verification failed")
	}
	for i, disk := range dst.Disks {
		expected := src.Info.Disks[i].Capacity
		for _, growth := range s.ExpandDisks {
			if growth.Key == src.Info.Disks[i].Key {
				expected = growth.CapacityMiB * 1024
			}
		}
		if disk.Thin != nil && src.Info.Disks[i].Thin != nil && *disk.Thin != *src.Info.Disks[i].Thin {
			return dst, conflict("clone disk provisioning verification failed")
		}
		if disk.Capacity != expected {
			return dst, conflict("clone disk capacity verification failed")
		}
		var backing object.DatastorePath
		if !backing.FromString(disk.FileName) || backing.Datastore != s.TargetDatastore() || path.Dir(backing.Path) != s.Dst {
			return dst, conflict("clone disk backing verification failed")
		}
	}
	return dst, nil
}

// The source must remain offline and unchanged until cloning finishes.
func (c *Client) checkCloneSource(ctx context.Context, s CloneSpec) error {
	vm, e := c.GetVM(ctx, s.Src)
	if e != nil {
		return e
	}
	if vm.Info.PowerState != "poweredOff" || vm.Info.ConnectionState != "connected" {
		return conflict("source power or connection state changed during clone")
	}
	var current mo.VirtualMachine
	if e = vm.Obj.Properties(ctx, vm.Obj.Reference(), []string{"config.changeVersion"}, &current); e != nil {
		return wrapVim(e)
	}
	if current.Config == nil || current.Config.ChangeVersion != s.SourceVersion {
		return conflict("source configuration changed during clone")
	}
	return nil
}

// Check target backings before applying any expansion or configuration changes.
func (c *Client) checkCloneBacking(ctx context.Context, s CloneSpec) error {
	vm, e := c.GetVM(ctx, s.Dst)
	if e != nil {
		return e
	}
	for _, disk := range vm.Info.Disks {
		var backing object.DatastorePath
		if !backing.FromString(disk.FileName) || backing.Datastore != s.TargetDatastore() || path.Dir(backing.Path) != s.Dst {
			return conflict("cloned disk still references an unexpected directory")
		}
	}
	return nil
}
