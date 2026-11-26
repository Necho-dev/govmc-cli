package esxi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type ResolvedVM struct {
	Obj  *object.VirtualMachine
	Mo   mo.VirtualMachine
	Info VMInfo
}

func projectVM(vm mo.VirtualMachine) VMInfo {
	ref := vm.Reference()
	info := VMInfo{
		ID:    ref.Value,
		MoRef: ref.Type + "-" + ref.Value,
		Name:  vm.Name,
	}
	if vm.Config != nil {
		info.UUID = strings.TrimSpace(vm.Config.InstanceUuid)
		info.InstanceUUID = vm.Config.InstanceUuid
		info.BiosUUID = vm.Config.Uuid
		info.GuestID = vm.Config.GuestId
		info.GuestOS = vm.Config.GuestFullName
		info.VBSEnabled = vm.Config.Flags.VbsEnabled
		info.NumCPU = vm.Config.Hardware.NumCPU
		info.MemoryMB = vm.Config.Hardware.MemoryMB
		info.NICs, info.Disks, info.VGPU = devicesFrom(vm.Config.Hardware.Device)

	}
	info.PowerState = string(vm.Runtime.PowerState)
	info.ConnectionState = string(vm.Runtime.ConnectionState)
	info.Question = questionFrom(vm.Runtime.Question)
	if vm.Summary.Storage != nil {
		info.CommittedBytes = &vm.Summary.Storage.Committed
		info.UncommittedBytes = &vm.Summary.Storage.Uncommitted
		info.UnsharedBytes = &vm.Summary.Storage.Unshared
	}
	qs := vm.Summary.QuickStats
	info.QuickStats = &QuickStats{
		OverallCPUUsage:  qs.OverallCpuUsage,
		GuestMemoryUsage: qs.GuestMemoryUsage,
		HostMemoryUsage:  qs.HostMemoryUsage,
		UptimeSeconds:    qs.UptimeSeconds,
		ConsumedOverhead: 0,
		PrivateMemory:    qs.PrivateMemory,
	}
	info.QuickStatsReliable = qs.HostMemoryUsage >= 0 && qs.UptimeSeconds > 0 && vm.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn
	if vm.Runtime.PowerState != types.VirtualMachinePowerStatePoweredOn {
		info.QuickStatsReliable = false
	}
	tools := ""
	if vm.Guest != nil {
		tools = string(vm.Guest.ToolsStatus)
		info.Guest.HostName = vm.Guest.HostName
		info.Guest.IPAddress = vm.Guest.IpAddress
		info.Guest.ToolsStatus = tools
	}
	info.Guest.GuestUnavailable = info.Guest.HostName == "" && info.Guest.IPAddress == ""
	return info
}

func questionFrom(q *types.VirtualMachineQuestionInfo) *Question {
	if q == nil {
		return nil
	}
	out := &Question{ID: q.Id, Text: q.Text, Choices: map[string]string{}}
	for _, v := range q.Choice.ChoiceInfo {
		if ed, ok := v.(*types.ElementDescription); ok {
			out.Choices[ed.Key] = ed.Label
		}
	}
	return out
}

func labelOf(d types.BaseVirtualDevice) string {
	info := d.GetVirtualDevice().DeviceInfo
	if info == nil {
		return ""
	}
	return info.GetDescription().Label
}

func devicesFrom(devs []types.BaseVirtualDevice) (nics []NIC, disks []Disk, vgpus []VGPU) {
	for _, b := range devs {
		d := b.GetVirtualDevice()
		switch t := b.(type) {
		case types.BaseVirtualEthernetCard:
			card := t.GetVirtualEthernetCard()
			n := NIC{Label: labelOf(b), Key: d.Key, MAC: card.MacAddress, AddressType: card.AddressType, Adapter: deviceTypeName(b)}
			if d.Connectable != nil {
				n.Connected = types.NewBool(d.Connectable.Connected)
				n.StartConnected = types.NewBool(d.Connectable.StartConnected)
			}
			if backing, ok := d.Backing.(*types.VirtualEthernetCardNetworkBackingInfo); ok {
				n.Portgroup = backing.DeviceName
			}
			nics = append(nics, n)
		case *types.VirtualDisk:
			disk := Disk{Label: labelOf(b), Key: d.Key, Capacity: t.CapacityInKB, ControllerKey: d.ControllerKey, UnitNumber: d.UnitNumber}
			if disk.Capacity <= 0 && t.CapacityInBytes > 0 {
				disk.Capacity = t.CapacityInBytes / 1024
			}
			if backing, ok := d.Backing.(*types.VirtualDiskFlatVer2BackingInfo); ok {
				disk.Mode, disk.Thin, disk.EagerlyScrub = backing.DiskMode, backing.ThinProvisioned, backing.EagerlyScrub
			}
			if t.StorageIOAllocation != nil {
				disk.IOPSLimit = t.StorageIOAllocation.Limit
			}
			if fb, ok := d.Backing.(types.BaseVirtualDeviceFileBackingInfo); ok {
				disk.FileName = fb.GetVirtualDeviceFileBackingInfo().FileName
			}
			disks = append(disks, disk)
		case *types.VirtualPCIPassthrough:
			g := VGPU{Label: labelOf(b)}
			if vb, ok := d.Backing.(*types.VirtualPCIPassthroughVmiopBackingInfo); ok {
				g.Type = vb.Vgpu
			}
			if g.Type != "" {
				vgpus = append(vgpus, g)
			}
		}
	}
	return
}

func deviceTypeName(b types.BaseVirtualDevice) string {
	s := fmt.Sprintf("%T", b)
	s = strings.TrimPrefix(s, "*types.")
	s = strings.TrimPrefix(s, "types.")
	return s
}

func hasUSBTablet(devs []types.BaseVirtualDevice) bool {
	for _, b := range devs {
		switch t := b.(type) {
		case *types.VirtualUSB:
			if strings.Contains(strings.ToLower(t.DeviceInfo.GetDescription().Label), "tablet") {
				return true
			}
		case *types.VirtualPointingDevice:
			if b, ok := t.Backing.(*types.VirtualPointingDeviceDeviceBackingInfo); ok {
				if strings.EqualFold(b.HostPointingDevice, "usb") || strings.Contains(strings.ToLower(b.HostPointingDevice), "tablet") {
					return true
				}
			}
		}
	}
	return false
}

func (c *Client) ListVMs(ctx context.Context, withSpark bool) ([]VMInfo, error) {
	vms, err := c.containerVMs(ctx, vmProps)
	if err != nil {
		return nil, err
	}
	out := make([]VMInfo, 0, len(vms))
	var on []types.ManagedObjectReference
	idx := map[string]int{}
	for _, vm := range vms {
		info := projectVM(vm)
		idx[vm.Reference().Value] = len(out)
		out = append(out, info)
		if vm.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn {
			on = append(on, vm.Reference())
		}
	}
	if withSpark && len(on) > 0 {
		mhz, err := c.HostCPUMhz(ctx)
		if err != nil {
			return out, nil
		}
		latest, err := c.latestVMMetrics(ctx, on)
		if err != nil {
			return out, nil
		}
		for moref, snap := range latest {
			i, ok := idx[moref]
			if !ok {
				continue
			}
			applySpark(&out[i], snap, mhz)
		}
	}
	return out, nil
}

func applySpark(info *VMInfo, snap map[string]int64, hostMhz int32) {
	if v, ok := snap["cpu.usagemhz.average"]; ok && info.NumCPU > 0 && hostMhz > 0 {
		pct := float64(v) / (float64(info.NumCPU) * float64(hostMhz)) * 100
		info.CPUPct = &pct
	}
	if v, ok := snap["mem.active.average"]; ok && info.MemoryMB > 0 {
		pct := float64(v) / (float64(info.MemoryMB) * 1024) * 100
		info.MemActivePct = &pct
	}
	if v, ok := snap["net.bytesRx.average"]; ok {
		m := kbpsToMbps(v)
		info.NetRxMbps = &m
	}
	if v, ok := snap["net.bytesTx.average"]; ok {
		m := kbpsToMbps(v)
		info.NetTxMbps = &m
	}
}

func (c *Client) GetVM(ctx context.Context, id string) (*ResolvedVM, error) {
	vms, err := c.containerVMs(ctx, vmProps)
	if err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	wantMo := normalizeMoRef(id)
	var byMoRef, legacyRef, byName, byUUID []mo.VirtualMachine
	for _, vm := range vms {
		if wantMo != "" && vm.Reference().Value == wantMo {
			byMoRef = append(byMoRef, vm)
		} else if legacy, prefixed := strings.CutPrefix(wantMo, "vm-"); prefixed && legacy != "" && vm.Reference().Value == legacy {
			legacyRef = append(legacyRef, vm)
		}
		if vm.Name == id {
			byName = append(byName, vm)
		}
		if vm.Config != nil && (eqFold(id, vm.Config.InstanceUuid) || eqFold(id, vm.Config.Uuid)) {
			byUUID = append(byUUID, vm)
		}
	}
	if len(byMoRef) == 0 {
		byMoRef = legacyRef
	}
	hit, why := pickVM(byMoRef, byName, byUUID)
	if hit == nil {
		if why == "ambiguous" {
			src := byUUID
			if len(byName) > 1 {
				src = byName
			}
			var refs []string
			for _, vm := range src {
				refs = append(refs, vm.Name+"/"+vm.Reference().Value)
			}
			return nil, &Error{
				Status: http.StatusConflict,
				Code:   "ambiguous",
				Message: fmt.Sprintf("ambiguous vm %q (shared instanceUuid from directory clone). Use display name or moref, e.g. GET /v1/vms/%s. matches: %s",
					id, src[0].Reference().Value, strings.Join(refs, ", ")),
			}
		}
		return nil, notFound("vm %q not found", id)
	}
	return &ResolvedVM{
		Obj:  object.NewVirtualMachine(c.Gov.Client, hit.Reference()),
		Mo:   *hit,
		Info: projectVM(*hit),
	}, nil
}

// DescribeVM reads optional configuration fields for one VM without fetching
// every VM's extraConfig in ordinary inventory and operational lookups.
func (c *Client) DescribeVM(ctx context.Context, id string) (VMInfo, error) {
	vm, err := c.GetVM(ctx, id)
	if err != nil {
		return VMInfo{}, err
	}
	var detail mo.VirtualMachine
	if err := vm.Obj.Properties(ctx, vm.Obj.Reference(), []string{
		"guest.net", "guest.toolsVersion", "guest.toolsInstallType", "guest.toolsRunningStatus",
	}, &detail); err == nil && detail.Guest != nil {
		enrichGuest(&vm.Info, detail.Guest)
	}
	current, err := vm.editConfiguration(ctx)
	if err != nil || current == nil {
		return vm.Info, nil
	}
	vm.Info.Config = projectVMConfig(current)
	if current.GuestFullName != "" {
		vm.Info.GuestOS = current.GuestFullName
	}
	var description, version string
	for _, entry := range current.ExtraConfig {
		if entry == nil {
			continue
		}
		option := entry.GetOptionValue()
		switch option.Key {
		case "guestinfo.vmtools.description":
			description, _ = option.Value.(string)
		case "guestinfo.vmtools.versionString":
			version, _ = option.Value.(string)
		case "vvtd.enable":
			switch value := option.Value.(type) {
			case bool:
				vm.Info.Config.IOMMU = &value
			case string:
				if parsed, err := strconv.ParseBool(value); err == nil {
					vm.Info.Config.IOMMU = &parsed
				}
			}
		}
	}
	vm.Info.Guest.ToolsVersionText = strings.TrimSpace(description)
	if vm.Info.Guest.ToolsVersionText == "" {
		vm.Info.Guest.ToolsVersionText = strings.TrimSpace(version)
	}
	return vm.Info, nil
}

func enrichGuest(info *VMInfo, guest *types.GuestInfo) {
	info.Guest.ToolsVersion = guest.ToolsVersion
	info.Guest.ToolsInstallType = guest.ToolsInstallType
	info.Guest.ToolsRunningStatus = guest.ToolsRunningStatus
	var all []string
	if info.Guest.IPAddress != "" {
		all = append(all, info.Guest.IPAddress)
	}
	for _, nic := range guest.Net {
		var addresses []string
		if nic.IpConfig != nil {
			for _, address := range nic.IpConfig.IpAddress {
				addresses = append(addresses, fmt.Sprintf("%s/%d", address.IpAddress, address.PrefixLength))
				all = append(all, address.IpAddress)
			}
		}
		if len(addresses) == 0 {
			addresses = nic.IpAddress
			all = append(all, addresses...)
		}
		for i := range info.NICs {
			device := &info.NICs[i]
			if nic.DeviceConfigId == device.Key || (nic.MacAddress != "" && strings.EqualFold(nic.MacAddress, device.MAC)) {
				device.IPAddresses = slices.Compact(append(device.IPAddresses, addresses...))
				break
			}
		}
	}
	seen := make(map[string]bool)
	for _, address := range all {
		if address != "" && !seen[address] {
			info.Guest.IPAddresses = append(info.Guest.IPAddresses, address)
			seen[address] = true
		}
	}
}

func projectVMConfig(cfg *types.VirtualMachineConfigInfo) *VMConfigState {
	out := &VMConfigState{
		Editable:       true,
		DeviceEditable: true,
		ChangeVersion:  cfg.ChangeVersion, Version: cfg.Version, CoresPerSocket: cfg.Hardware.NumCoresPerSocket,
		Annotation: cfg.Annotation, Firmware: cfg.Firmware, VMX: cfg.Files.VmPathName, SuspendDirectory: cfg.Files.SuspendDirectory,
		NestedHV: cfg.NestedHVEnabled, CPUHotAdd: cfg.CpuHotAddEnabled, MemoryHotAdd: cfg.MemoryHotAddEnabled,
		MemoryLocked: cfg.MemoryReservationLockedToMax, VPMC: cfg.VPMCEnabled, VBS: cfg.Flags.VbsEnabled,
		MaxMksConnections: cfg.MaxMksConnections, GuestAutoLock: cfg.GuestAutoLockEnabled,
	}
	if cfg.CpuAllocation != nil {
		out.CPUReservationMHz, out.CPULimitMHz = cfg.CpuAllocation.Reservation, cfg.CpuAllocation.Limit
		if shares := cfg.CpuAllocation.Shares; shares != nil {
			out.CPUShares = string(shares.Level)
		}
	}
	if cfg.MemoryAllocation != nil {
		out.MemoryReservationMB, out.MemoryLimitMB = cfg.MemoryAllocation.Reservation, cfg.MemoryAllocation.Limit
		if shares := cfg.MemoryAllocation.Shares; shares != nil {
			out.MemoryShares = string(shares.Level)
		}
	}
	if cfg.BootOptions != nil {
		out.SecureBoot = cfg.BootOptions.EfiSecureBootEnabled
		out.BootDelayMS = &cfg.BootOptions.BootDelay
		out.BootRetry = cfg.BootOptions.BootRetryEnabled
	}
	if cfg.Tools != nil {
		out.ToolsUpgradePolicy = cfg.Tools.ToolsUpgradePolicy
	}
	out.StandbyAction = cfg.DefaultPowerOps.StandbyAction
	for _, device := range cfg.Hardware.Device {
		entry := VMDeviceInfo{Label: labelOf(device), Type: deviceTypeName(device), Key: device.GetVirtualDevice().Key}
		if video, ok := device.(*types.VirtualMachineVideoCard); ok {
			entry.Displays, entry.VideoRAMKB, entry.Enable3D = video.NumDisplays, video.VideoRamSizeInKB, video.Enable3DSupport
		}
		out.Devices = append(out.Devices, entry)
	}
	return out
}

func (vm *ResolvedVM) editConfiguration(ctx context.Context) (*types.VirtualMachineConfigInfo, error) {
	var current mo.VirtualMachine
	err := vm.Obj.Properties(ctx, vm.Obj.Reference(), []string{"config"}, &current)
	return current.Config, err
}

func normalizeMoRef(id string) string {
	s := strings.TrimSpace(id)
	return strings.TrimPrefix(s, "VirtualMachine-")
}

func pickVM(byMoRef, byName, byUUID []mo.VirtualMachine) (*mo.VirtualMachine, string) {
	if len(byMoRef) == 1 {
		return &byMoRef[0], ""
	}
	if len(byName) == 1 {
		return &byName[0], ""
	}
	if len(byName) > 1 {
		return nil, "ambiguous"
	}
	if len(byUUID) == 1 {
		return &byUUID[0], ""
	}
	if len(byUUID) > 1 {
		return nil, "ambiguous"
	}
	return nil, ""
}

func eqFold(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(a, b)
}

func (c *Client) Host(ctx context.Context) (*HostInfo, error) {
	hs, err := c.Finder.DefaultHostSystem(ctx)
	if err != nil {
		return nil, wrapVim(err)
	}
	var moHost mo.HostSystem
	err = hs.Properties(ctx, hs.Reference(), []string{
		"name", "summary.hardware", "config.network.portgroup", "summary.quickStats.uptime", "summary.overallStatus",
		"runtime.bootTime", "runtime.connectionState", "runtime.inMaintenanceMode",
	}, &moHost)
	if err != nil {
		return nil, wrapVim(err)
	}
	out := &HostInfo{
		Name: moHost.Name, MoRef: hs.Reference().Value, OverallStatus: string(moHost.Summary.OverallStatus),
		ConnectionState: string(moHost.Runtime.ConnectionState), MaintenanceMode: moHost.Runtime.InMaintenanceMode,
	}
	if uptime := int64(moHost.Summary.QuickStats.Uptime); uptime > 0 {
		out.UptimeSeconds = &uptime
	} else if boot := moHost.Runtime.BootTime; boot != nil && !boot.IsZero() {
		if elapsed := time.Since(*boot); elapsed >= 0 {
			uptime = int64(elapsed / time.Second)
			out.UptimeSeconds = &uptime
		}
	}
	if hw := moHost.Summary.Hardware; hw != nil {
		out.CPUMhz = hw.CpuMhz
		out.NumCpuCores = hw.NumCpuCores
		out.NumCpuThreads = hw.NumCpuThreads
		out.NumCpuPkgs = hw.NumCpuPkgs
		out.NumCPU = hw.NumCpuThreads
		if out.NumCPU == 0 {
			out.NumCPU = hw.NumCpuCores
		}
		out.MemoryMB = hw.MemorySize / (1024 * 1024)
	}
	if moHost.Config != nil && moHost.Config.Network != nil {
		for _, pg := range moHost.Config.Network.Portgroup {
			out.Portgroups = append(out.Portgroups, Portgroup{
				Name:    pg.Spec.Name,
				VLAN:    pg.Spec.VlanId,
				VSwitch: pg.Spec.VswitchName,
			})
		}
	}
	return out, nil
}

func (c *Client) Networks(ctx context.Context) ([]NetworkInfo, error) {
	h, err := c.Host(ctx)
	if err != nil {
		return nil, err
	}
	var out []NetworkInfo
	for _, pg := range h.Portgroups {
		out = append(out, NetworkInfo{Name: pg.Name, Type: "portgroup"})
	}
	return out, nil
}

func (c *Client) RefreshDatastores(ctx context.Context) error {
	hs, err := c.Finder.DefaultHostSystem(ctx)
	if err != nil {
		return wrapVim(err)
	}
	ss, err := hs.ConfigManager().StorageSystem(ctx)
	if err != nil {
		return wrapVim(err)
	}
	if err := ss.Refresh(ctx); err != nil {
		return wrapVim(err)
	}
	dss, err := c.Finder.DatastoreList(ctx, "*")
	if err != nil {
		return wrapVim(err)
	}
	for _, ds := range dss {
		ref := ds.Reference()
		if _, err := methods.RefreshDatastore(ctx, c.Gov.Client, &types.RefreshDatastore{This: ref}); err != nil {
			return wrapVim(err)
		}
		if _, err := methods.RefreshDatastoreStorageInfo(ctx, c.Gov.Client, &types.RefreshDatastoreStorageInfo{This: ref}); err != nil {
			return wrapVim(err)
		}
	}
	return nil
}

func (c *Client) Datastores(ctx context.Context) ([]DatastoreInfo, error) {
	dss, err := c.Finder.DatastoreList(ctx, "*")
	if err != nil {
		return nil, wrapVim(err)
	}
	var out []DatastoreInfo
	for _, ds := range dss {
		var md mo.Datastore
		if err := ds.Properties(ctx, ds.Reference(), []string{"name", "summary", "info", "vm", "overallStatus"}, &md); err != nil {
			return nil, wrapVim(err)
		}
		accessible := md.Summary.Accessible
		item := DatastoreInfo{
			Name: md.Name, Type: md.Summary.Type, Capacity: md.Summary.Capacity, FreeSpace: md.Summary.FreeSpace,
			URL: md.Summary.Url, Accessible: &accessible, MaintenanceMode: md.Summary.MaintenanceMode,
			Status: string(md.OverallStatus), VMCount: len(md.Vm),
		}
		if md.Summary.Uncommitted > 0 {
			value := md.Summary.Uncommitted
			item.Uncommitted = &value
		}
		if md.Info != nil {
			info := md.Info.GetDatastoreInfo()
			item.MaxFileSize, item.MaxVirtualDiskCapacity = info.MaxFileSize, info.MaxVirtualDiskCapacity
			if info.Timestamp != nil {
				item.UpdatedAt = info.Timestamp.UTC().Format(time.RFC3339)
			}
			if vmfs, ok := md.Info.(*types.VmfsDatastoreInfo); ok && vmfs.Vmfs != nil {
				item.VMFSVersion = vmfs.Vmfs.Version
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (c *Client) DatastoreFiles(ctx context.Context, dsName, path string) ([]DSFile, error) {
	ds, err := c.Finder.Datastore(ctx, dsName)
	if err != nil {
		return nil, wrapVim(err)
	}
	path, err = CleanRel(path)
	if err != nil {
		return nil, err
	}
	b, err := ds.Browser(ctx)
	if err != nil {
		return nil, wrapVim(err)
	}
	spec := &types.HostDatastoreBrowserSearchSpec{
		MatchPattern: []string{"*"},
		Details: &types.FileQueryFlags{
			FileType:     true,
			FileSize:     true,
			Modification: true,
			FileOwner:    false,
		},
	}
	task, err := b.SearchDatastore(ctx, ds.Path(path), spec)
	if err != nil {
		return nil, wrapVim(err)
	}
	info, err := task.WaitForResult(ctx)
	if err != nil {
		return nil, wrapVim(err)
	}
	res, ok := info.Result.(types.HostDatastoreBrowserSearchResults)
	if !ok {
		return nil, errf(http.StatusBadGateway, "vim_fault", "unexpected datastore search result")
	}
	var out []DSFile
	for _, f := range res.File {
		fi := f.GetFileInfo()
		item := DSFile{Path: fi.Path, Size: fi.FileSize}
		if fi.Modification != nil {
			item.Modified = fi.Modification.UTC().Format(time.RFC3339)
		}
		if _, isDir := f.(*types.FolderFileInfo); isDir {
			item.Folder = true
		}
		out = append(out, item)
	}
	return out, nil
}
