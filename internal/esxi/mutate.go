package esxi

import (
	"context"
	"strings"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

func RequireConfirm(confirm bool) error {
	if confirm {
		return nil
	}
	return forbidden("destructive action requires confirm=true or header X-Confirm: true")
}

func (c *Client) Power(ctx context.Context, id, action string, confirm bool) error {
	if err := RequireConfirm(confirm); err != nil {
		return err
	}
	vm, err := c.GetVM(ctx, id)
	if err != nil {
		return err
	}
	if err := c.guardVM(ctx, vm); err != nil {
		return err
	}
	var task *object.Task
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "on":
		task, err = vm.Obj.PowerOn(ctx)
	case "off":
		task, err = vm.Obj.PowerOff(ctx)
	case "reset":
		task, err = vm.Obj.Reset(ctx)
	case "suspend", "s":
		task, err = vm.Obj.Suspend(ctx)
	default:
		return badRequest("action must be on|off|reset|suspend")
	}
	if err != nil {
		return wrapVim(err)
	}
	return wrapVim(task.Wait(ctx))
}

func (c *Client) GetQuestion(ctx context.Context, id string) (*Question, error) {
	vm, err := c.GetVM(ctx, id)
	if err != nil {
		return nil, err
	}
	return vm.Info.Question, nil
}

func (c *Client) AnswerQuestion(ctx context.Context, id, questionID, choice string, confirm bool) error {
	if err := RequireConfirm(confirm); err != nil {
		return err
	}
	vm, err := c.GetVM(ctx, id)
	if err != nil {
		return err
	}
	if err := c.guardVM(ctx, vm); err != nil {
		return err
	}
	q := vm.Info.Question
	if q == nil {
		return conflict("no pending question")
	}
	if questionID == "" {
		questionID = q.ID
	}
	if choice == "" {
		return badRequest("choice is required")
	}
	return wrapVim(vm.Obj.Answer(ctx, questionID, choice))
}

type VMChange struct {
	ExpandDisks         []DiskExpansion `json:"expandDisks,omitempty"`
	AddDiskMiB          *int64          `json:"addDiskMiB,omitempty"`
	EditNICs            []NICChange     `json:"editNICs,omitempty"`
	AddNIC              *NewNIC         `json:"addNIC,omitempty"`
	NumCPU              *int32          `json:"numCPU,omitempty"`
	MemoryMB            *int32          `json:"memoryMB,omitempty"`
	NestedHV            *bool           `json:"nestedHV,omitempty"`
	IOMMU               *bool           `json:"iommu,omitempty"`
	MemoryLocked        *bool           `json:"memoryLocked,omitempty"`
	LockMemory          bool            `json:"-"`
	RequirePoweredOff   bool            `json:"requirePoweredOff,omitempty"`
	RequireHotAdd       bool            `json:"requireHotAdd,omitempty"`
	ChangeVersion       string          `json:"changeVersion,omitempty"`
	Name                *string         `json:"name,omitempty"`
	CoresPerSocket      *int32          `json:"coresPerSocket,omitempty"`
	CPUHotAdd           *bool           `json:"cpuHotAddEnabled,omitempty"`
	MemoryHotAdd        *bool           `json:"memoryHotAddEnabled,omitempty"`
	VPMC                *bool           `json:"vpmc,omitempty"`
	CPUReservationMHz   *int64          `json:"cpuReservationMHz,omitempty"`
	CPULimitMHz         *int64          `json:"cpuLimitMHz,omitempty"`
	MemoryReservationMB *int64          `json:"memoryReservationMB,omitempty"`
	MemoryLimitMB       *int64          `json:"memoryLimitMB,omitempty"`
}

func (ch VMChange) offlineSettings() bool {
	return ch.MemoryLocked != nil || ch.Name != nil || ch.CoresPerSocket != nil || ch.CPUHotAdd != nil || ch.MemoryHotAdd != nil ||
		ch.VPMC != nil || ch.CPUReservationMHz != nil || ch.CPULimitMHz != nil ||
		ch.MemoryReservationMB != nil || ch.MemoryLimitMB != nil
}

func (c *Client) ChangeVM(ctx context.Context, id string, ch VMChange, confirm bool) error {
	if err := RequireConfirm(confirm); err != nil {
		return err
	}
	if !ch.deviceChanges() && !ch.offlineSettings() && ch.NumCPU == nil && ch.MemoryMB == nil && ch.NestedHV == nil && ch.IOMMU == nil {
		return badRequest("no configuration changes supplied")
	}
	vm, err := c.GetVM(ctx, id)
	if err != nil {
		return err
	}
	if err := c.guardVM(ctx, vm); err != nil {
		return err
	}
	if (ch.RequirePoweredOff || ch.offlineSettings()) && vm.Info.PowerState != string(types.VirtualMachinePowerStatePoweredOff) {
		return conflict("power off the virtual machine before editing its configuration")
	}
	spec := types.VirtualMachineConfigSpec{ChangeVersion: ch.ChangeVersion, MemoryReservationLockedToMax: ch.MemoryLocked}
	if ch.Name != nil {
		spec.Name = strings.TrimSpace(*ch.Name)
		if spec.Name == "" {
			return badRequest("VM name cannot be empty")
		}
	}
	if ch.NumCPU != nil && *ch.NumCPU <= 0 || ch.MemoryMB != nil && *ch.MemoryMB <= 0 {
		return badRequest("vCPU and memory must be positive")
	}
	if ch.CoresPerSocket != nil {
		cpu := vm.Info.NumCPU
		if ch.NumCPU != nil {
			cpu = *ch.NumCPU
		}
		cores := *ch.CoresPerSocket
		if cores < 0 || (cores > 0 && (cpu < cores || cpu%cores != 0)) {
			return badRequest("cores per socket must be automatic (0) or divide the vCPU count")
		}
		spec.NumCoresPerSocket = ch.CoresPerSocket
	}
	for _, reservation := range []*int64{ch.CPUReservationMHz, ch.MemoryReservationMB} {
		if reservation != nil && *reservation < 0 {
			return badRequest("reservations must be non-negative")
		}
	}
	for _, limit := range []*int64{ch.CPULimitMHz, ch.MemoryLimitMB} {
		if limit != nil && (*limit < -1 || *limit == 0) {
			return badRequest("limits must be positive or -1 (unlimited)")
		}
	}
	if ch.CPUReservationMHz != nil || ch.CPULimitMHz != nil {
		spec.CpuAllocation = &types.ResourceAllocationInfo{Reservation: ch.CPUReservationMHz, Limit: ch.CPULimitMHz}
	}
	if ch.MemoryReservationMB != nil || ch.MemoryLimitMB != nil {
		spec.MemoryAllocation = &types.ResourceAllocationInfo{Reservation: ch.MemoryReservationMB, Limit: ch.MemoryLimitMB}
	}
	spec.CpuHotAddEnabled, spec.MemoryHotAddEnabled, spec.VPMCEnabled = ch.CPUHotAdd, ch.MemoryHotAdd, ch.VPMC
	if ch.RequireHotAdd || ch.deviceChanges() {
		cfg, err := vm.editConfiguration(ctx)
		if err != nil {
			return wrapVim(err)
		}
		if cfg == nil {
			return conflict("VM configuration is unavailable")
		}
		if ch.RequireHotAdd || (ch.deviceChanges() && vm.Info.PowerState == string(types.VirtualMachinePowerStatePoweredOn) && (ch.NumCPU != nil || ch.MemoryMB != nil)) {
			if err := validateHotAdd(vm.Info.PowerState, cfg, ch); err != nil {
				return err
			}
		}
		if ch.ChangeVersion != "" && ch.ChangeVersion != cfg.ChangeVersion {
			return conflict("VM configuration changed; reload before editing")
		}
		spec.ChangeVersion = cfg.ChangeVersion
		if ch.deviceChanges() {
			spec.DeviceChange, err = c.buildDeviceChanges(ctx, vm, cfg, ch)
			if err != nil {
				return err
			}
		}
	}
	if ch.NumCPU != nil {
		spec.NumCPUs = *ch.NumCPU
	}
	if ch.MemoryMB != nil {
		spec.MemoryMB = int64(*ch.MemoryMB)
		if ch.LockMemory {
			mb := int64(*ch.MemoryMB)
			spec.MemoryReservationLockedToMax = types.NewBool(true)
			spec.MemoryAllocation = &types.ResourceAllocationInfo{Reservation: types.NewInt64(mb)}
		}
	}
	if ch.NestedHV != nil {
		spec.NestedHVEnabled = types.NewBool(*ch.NestedHV)
	}
	if ch.IOMMU != nil {
		val := "FALSE"
		if *ch.IOMMU {
			val = "TRUE"
		}
		spec.ExtraConfig = append(spec.ExtraConfig, &types.OptionValue{Key: "vvtd.enable", Value: val})
	}
	task, err := vm.Obj.Reconfigure(ctx, spec)
	if err != nil {
		return wrapVim(err)
	}
	return wrapVim(task.Wait(ctx))
}

func validateHotAdd(power string, cfg *types.VirtualMachineConfigInfo, ch VMChange) error {
	if power != string(types.VirtualMachinePowerStatePoweredOn) || cfg == nil {
		return conflict("online reconfiguration requires a running VM with readable configuration")
	}
	if ch.offlineSettings() || ch.NestedHV != nil || ch.IOMMU != nil || ch.LockMemory || (ch.NumCPU == nil && ch.MemoryMB == nil && !ch.deviceChanges()) {
		return badRequest("online reconfiguration only supports CPU or memory hot-add and supported device changes")
	}
	if ch.NumCPU != nil && (cfg.CpuHotAddEnabled == nil || !*cfg.CpuHotAddEnabled || cfg.Hardware.NumCPU <= 0 || *ch.NumCPU <= cfg.Hardware.NumCPU) {
		return conflict("CPU hot-add must be enabled and the requested vCPU count must be higher")
	}
	if ch.MemoryMB != nil && (cfg.MemoryHotAddEnabled == nil || !*cfg.MemoryHotAddEnabled || cfg.Hardware.MemoryMB <= 0 || *ch.MemoryMB <= cfg.Hardware.MemoryMB) {
		return conflict("memory hot-add must be enabled and the requested memory must be higher")
	}
	return nil
}

func (c *Client) AddNIC(ctx context.Context, id, portgroup, adapter string, replace, confirm bool) error {
	if err := RequireConfirm(confirm); err != nil {
		return err
	}
	if portgroup == "" {
		return badRequest("portgroup is required")
	}
	if adapter == "" {
		adapter = "e1000e"
	}
	vm, err := c.GetVM(ctx, id)
	if err != nil {
		return err
	}
	if err := c.guardVM(ctx, vm); err != nil {
		return err
	}
	if replace {
		devs, err := vm.Obj.Device(ctx)
		if err != nil {
			return wrapVim(err)
		}
		var remove []types.BaseVirtualDevice
		for _, d := range devs {
			if _, ok := d.(types.BaseVirtualEthernetCard); ok {
				remove = append(remove, d)
			}
		}
		if len(remove) > 0 {
			if err := vm.Obj.RemoveDevice(ctx, false, remove...); err != nil {
				return wrapVim(err)
			}
		}
	}
	net, err := c.Finder.Network(ctx, portgroup)
	if err != nil {
		return wrapVim(err)
	}
	backing, err := net.EthernetCardBackingInfo(ctx)
	if err != nil {
		return wrapVim(err)
	}
	card, err := object.EthernetCardTypes().CreateEthernetCard(adapter, backing)
	if err != nil {
		return wrapVim(err)
	}
	return wrapVim(vm.Obj.AddDevice(ctx, card))
}

func (c *Client) Unregister(ctx context.Context, id string, confirm bool) error {
	if err := RequireConfirm(confirm); err != nil {
		return err
	}
	vm, err := c.GetVM(ctx, id)
	if err != nil {
		return err
	}
	if err := c.guardVM(ctx, vm); err != nil {
		return err
	}
	return wrapVim(vm.Obj.Unregister(ctx))
}

func (c *Client) Register(ctx context.Context, datastore, path, name string, confirm bool) (*VMInfo, error) {
	if err := RequireConfirm(confirm); err != nil {
		return nil, err
	}
	if datastore == "" || path == "" {
		return nil, badRequest("datastore and path are required")
	}
	if name == "" {
		return nil, badRequest("name is required")
	}
	dsPath := path
	if !strings.HasPrefix(strings.TrimSpace(path), "[") {
		p := object.DatastorePath{Datastore: datastore, Path: path}
		dsPath = p.String()
	}
	var parsed object.DatastorePath
	if parsed.FromString(dsPath) {
		if err := c.guardFS(ctx, parsed.Datastore, parsed.Path); err != nil {
			return nil, err
		}
	}
	dc, err := c.Finder.DefaultDatacenter(ctx)
	if err != nil {
		return nil, wrapVim(err)
	}
	folders, err := dc.Folders(ctx)
	if err != nil {
		return nil, wrapVim(err)
	}
	host, err := c.Finder.DefaultHostSystem(ctx)
	if err != nil {
		return nil, wrapVim(err)
	}
	pool, err := host.ResourcePool(ctx)
	if err != nil {
		return nil, wrapVim(err)
	}
	task, err := folders.VmFolder.RegisterVM(ctx, dsPath, name, false, pool, host)
	if err != nil {
		return nil, wrapVim(err)
	}
	info, err := task.WaitForResult(ctx)
	if err != nil {
		return nil, wrapVim(err)
	}
	ref, ok := info.Result.(types.ManagedObjectReference)
	if !ok {
		return nil, errf(502, "vim_fault", "register returned no VM")
	}
	resolved, err := c.GetVM(ctx, ref.Value)
	if err != nil {
		return nil, err
	}
	return &resolved.Info, nil
}

func (c *Client) CopyDatastoreDir(ctx context.Context, dsName, src, dst string, confirm, dryRun bool) (*StepResult, error) {
	if src == "" || dst == "" {
		return nil, badRequest("src and dst are required")
	}
	layout := CloneLayout{Folder: src, Prefix: src}
	if got, err := c.ResolveCloneLayout(ctx, dsName, src); err == nil {
		layout = got
		if layout.Folder != "" {
			src = layout.Folder
		}
	}
	if dryRun {
		return &StepResult{Op: "copy", DryRun: true, Message: "copy [" + dsName + "] " + src + " -> " + dst, Detail: layout}, nil
	}
	out, err := c.FSCopy(ctx, dsName, src, dst, confirm, false)
	if err != nil {
		return nil, err
	}
	out.Detail = layout
	return out, nil
}

func (c *Client) DeleteDatastoreDir(ctx context.Context, dsName, rel string, confirm bool) (*StepResult, error) {
	return c.FSDelete(ctx, dsName, rel, confirm)
}
