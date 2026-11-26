package workflow

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/soap"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real VMFS browsers hide extents. An ordinary descriptor copy must never be
// used for cloning even when vcsim happens to expose the extent as a file.
type rejectDescriptorFileCopy struct{ next soap.RoundTripper }

func (r rejectDescriptorFileCopy) RoundTrip(ctx context.Context, req, res soap.HasFault) error {
	if body, ok := req.(*methods.CopyDatastoreFile_TaskBody); ok && strings.HasSuffix(body.Req.SourceName, ".vmdk") {
		return fmt.Errorf("descriptor-only file copy would lose hidden VMFS extents")
	}
	return r.next.RoundTrip(ctx, req, res)
}

func TestSimulatorClonePreflightAndPersistentFileJobs(t *testing.T) {
	if os.Getenv("GOVMC_WEB_SIM_TEST") != "1" {
		t.Skip("local ESXi simulator opt-in")
	}
	model := simulator.ESX()
	model.Machine = 1
	model.Datastore = 2
	if e := model.Create(); e != nil {
		t.Fatal(e)
	}
	defer model.Remove()
	model.Service.TLS = &tls.Config{}
	server := model.Service.NewServer()
	defer server.Close()
	u := *server.URL
	user := u.User.Username()
	password, _ := u.User.Password()
	u.User = nil
	c, e := esxi.Dial(context.Background(), u.String(), user, password, true)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Gov.Logout(context.Background())
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	vms, e := c.ListVMs(ctx, false)
	if e != nil || len(vms) == 0 {
		t.Fatal(e)
	}
	stores, e := c.Datastores(ctx)
	if e != nil || len(stores) < 2 {
		t.Fatal(e)
	}
	vm := vms[0]
	dry := true
	s := esxi.CloneSpec{Src: vm.ID, Dst: "clone-target", Datastore: stores[0].Name, DryRun: &dry}
	if _, _, e = c.PrepareClone(ctx, s); e == nil {
		t.Fatal("powered-on source accepted")
	}
	if e = c.Power(ctx, vm.ID, "off", true); e != nil {
		t.Fatal(e)
	}
	resolved, e := c.GetVM(ctx, vm.ID)
	if e != nil {
		t.Fatal(e)
	}
	var cfgPath string
	for _, disk := range resolved.Info.Disks {
		var dp object.DatastorePath
		if !dp.FromString(disk.FileName) {
			t.Fatal(disk.FileName)
		}
		dir := filepath.Join(stores[0].URL, path.Dir(dp.Path))
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		base := strings.TrimSuffix(path.Base(dp.Path), ".vmdk")
		if e = os.WriteFile(filepath.Join(dir, base+"-flat.vmdk"), make([]byte, 512), 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, base+".vmdk"), []byte(esxi.VmdkDescriptor(base+"-flat.vmdk", 512)), 0600); e != nil {
			t.Fatal(e)
		}
		cfgPath = dir
	}
	// Simulator inventory creates a VMX; preflight reads physical descriptors only.
	if cfgPath == "" {
		t.Fatal("fixture has no disk")
	}
	s, plan, e := c.PrepareClone(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	if *s.NumCPU != vm.NumCPU || *s.MemoryMB != vm.MemoryMB || *s.PowerOn || !plan.DryRun {
		t.Fatal("configuration was not inherited", s)
	}
	if _, e = os.Stat(filepath.Join(stores[0].URL, s.Dst)); !os.IsNotExist(e) {
		t.Fatal("preview created destination", e)
	}
	if _, _, e = c.PrepareClone(ctx, esxi.CloneSpec{Src: vm.ID, Dst: "../escape", Datastore: s.Datastore}); e == nil {
		t.Fatal("invalid destination accepted")
	}
	if binary := os.Getenv("GOVMC_SERVE_BINARY"); binary != "" {
		configPath := filepath.Join(t.TempDir(), "config.yaml")
		if e = target.Edit(ctx, configPath, func(cfg *target.Config) error {
			p := target.Profile{URL: u.String(), Username: user, Insecure: true}
			if e := target.SetPassword(configPath, "lab", &p, password); e != nil {
				return e
			}
			cfg.Profiles["lab"] = p
			cfg.Active = "lab"
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		cli := exec.CommandContext(ctx, binary, "--profile", "lab", "vm", "clone", vm.ID, "cli-preview", "--datastore", s.Datastore, "--dry-run", "--json")
		cli.Env = append(os.Environ(), "GOVMC_CONFIG="+configPath)
		output, e := cli.Output()
		if e != nil {
			t.Fatal("CLI clone preview", e)
		}
		var preview esxi.ClonePlan
		if json.Unmarshal(output, &preview) != nil || !preview.DryRun {
			t.Fatal(string(output))
		}
		if _, e = os.Stat(filepath.Join(stores[0].URL, "cli-preview")); !os.IsNotExist(e) {
			t.Fatal("CLI preview created a destination")
		}
		cli = exec.CommandContext(ctx, binary, "--profile", "lab", "tasks", "list", "--json")
		cli.Env = append(os.Environ(), "GOVMC_CONFIG="+configPath)
		output, e = cli.Output()
		var page tasks.Page
		if e != nil || json.Unmarshal(output, &page) != nil || len(page.Items) != 0 {
			t.Fatal("preview created an execution task", string(output), e)
		}
	}
	c.Gov.Client.RoundTripper = rejectDescriptorFileCopy{next: c.Gov.Client.RoundTripper}
	// Exercise actual folder copy, descriptor patch and identity patch, then
	// cancel at the registration boundary. vcsim does not load real VMX hardware.
	fixtureVMX := filepath.Join(stores[0].URL, s.SrcFolder, s.SrcPrefix+".vmx")
	originalVMX := fmt.Sprintf("displayName = %q\nvc.uuid = %q\nuuid.bios = %q\nethernet12.addressType = \"static\"\nethernet12.address = \"00:00:00:00:00:12\"\n", vm.Name, vm.InstanceUUID, vm.BiosUUID)
	if e = os.WriteFile(fixtureVMX, []byte(originalVMX), 0600); e != nil {
		t.Fatal(e)
	}
	no := false
	execute := s
	execute.DryRun = &no
	execute.Confirm = true
	run, cancelRun := context.WithCancel(ctx)
	_, e = c.ExecuteClone(run, execute, func(op, status, message string) {
		if op == "register" && status == "running" {
			cancelRun()
		}
	})
	cancelRun()
	if !errors.Is(e, context.Canceled) {
		t.Fatal("clone did not stop before registration", e)
	}
	patched, e := os.ReadFile(filepath.Join(stores[0].URL, s.Dst, s.Dst+".vmx"))
	if e != nil || !strings.Contains(string(patched), "vc.uuid") || strings.Contains(string(patched), vm.InstanceUUID) || strings.Contains(string(patched), "00:00:00:00:00:12") {
		t.Fatal("clone identity patch failed", string(patched), e)
	}
	unchanged, e := os.ReadFile(fixtureVMX)
	if e != nil || string(unchanged) != originalVMX {
		t.Fatal("clone changed mother VMX", e)
	}
	list, e := c.ListVMs(ctx, false)
	if e != nil || len(list) != len(vms) {
		t.Fatal("cancelled clone was registered", e)
	}
	// Cross-store clone: inspect the actual copied/rewritten files and register
	// the VMX on the target, stopping before hardware checks (vcsim does not load VMX hardware).
	cross := s
	cross.Dst = "cross-clone"
	cross.Datastore = ""
	cross.DestinationDatastore = stores[1].Name
	if e = os.MkdirAll(filepath.Join(stores[0].URL, cross.Dst), 0700); e != nil {
		t.Fatal(e)
	}
	cross, crossPlan, e := c.PrepareClone(ctx, cross)
	if e != nil || cross.Datastore != stores[0].Name || cross.TargetDatastore() != stores[1].Name || crossPlan.DestinationDatastore != stores[1].Name {
		t.Fatal("cross-store preview", crossPlan, e)
	}
	if _, e = os.Stat(filepath.Join(stores[1].URL, cross.Dst)); !os.IsNotExist(e) {
		t.Fatal("cross preview created target")
	}
	wrong := cross
	wrong.Datastore = stores[1].Name
	if _, _, e = c.PrepareClone(ctx, wrong); e == nil {
		t.Fatal("wrong source store accepted")
	}
	large := cross
	large.ExpandDisks = []esxi.DiskExpansion{{Key: resolved.Info.Disks[0].Key, CapacityMiB: stores[1].FreeSpace/(1<<20) + resolved.Info.Disks[0].Capacity/1024 + 1}}
	if _, _, e = c.PrepareClone(ctx, large); e == nil || !strings.Contains(e.Error(), "insufficient") {
		t.Fatal("target capacity did not include disk expansion", e)
	}
	cross.DryRun = &no
	cross.Confirm = true
	crossRun, crossCancel := context.WithCancel(ctx)
	registered := false
	_, e = c.ExecuteClone(crossRun, cross, func(op, status, message string) {
		if op == "register" && status == "ok" {
			registered = true
			crossCancel()
		}
	})
	crossCancel()
	if !registered || !errors.Is(e, context.Canceled) {
		t.Fatal("cross clone did not register at target", e)
	}
	child, e := c.DescribeVM(ctx, cross.Dst)
	if e != nil || child.Config == nil || child.Config.VMX != "["+stores[1].Name+"] "+cross.Dst+"/"+cross.Dst+".vmx" {
		t.Fatal("registered on wrong datastore", child, e)
	}
	copiedVMX, e := os.ReadFile(filepath.Join(stores[1].URL, cross.Dst, cross.Dst+".vmx"))
	if e != nil || strings.Contains(string(copiedVMX), vm.InstanceUUID) || strings.Contains(string(copiedVMX), "00:00:00:00:00:12") {
		t.Fatal("cross-store identity patch", e)
	}
	sourceVMX, e := os.ReadFile(fixtureVMX)
	if e != nil || string(sourceVMX) != originalVMX {
		t.Fatal("cross clone changed source", e)
	}
	if e = c.Unregister(ctx, cross.Dst, true); e != nil {
		t.Fatal(e)
	}
	// Keep both workers occupied to inspect cross-store task submission, resource
	// reservations, idempotency and cancellation before any clone mutation occurs.
	cloneManager, e := tasks.Open(filepath.Join(t.TempDir(), "config.yaml"), "mcp")
	if e != nil {
		t.Fatal(e)
	}
	defer cloneManager.Close()
	gate := make(chan struct{})
	started := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		_, e = cloneManager.Submit(ctx, "lab", "fixture", "fixture", "blocker", nil, fmt.Sprintf("blocker-%d", i), "", func(run context.Context, _ *tasks.Reporter) (any, error) {
			started <- struct{}{}
			select {
			case <-gate:
				return nil, nil
			case <-run.Done():
				return nil, run.Err()
			}
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	<-started
	<-started
	queued := cross
	queued.Dst = "queued-cross-clone"
	queued.Confirm = false
	if _, e = Clone(ctx, cloneManager, c, "lab", c.Endpoint(), queued, "clone-key", nil); e == nil {
		t.Fatal("clone executed without confirmation")
	}
	queued.Confirm = true
	submitted, e := Clone(ctx, cloneManager, c, "lab", c.Endpoint(), queued, "clone-key", nil)
	if e != nil {
		t.Fatal(e)
	}
	queuedTask := submitted.(tasks.Task)
	var recorded esxi.CloneSpec
	if json.Unmarshal(queuedTask.Parameters, &recorded) != nil || recorded.Datastore != stores[0].Name || recorded.TargetDatastore() != stores[1].Name || queuedTask.Kind != "vm.clone" || queuedTask.Status != "queued" {
		t.Fatal("incorrect cross-store task record", queuedTask)
	}
	again, e := Clone(ctx, cloneManager, c, "lab", c.Endpoint(), queued, "clone-key", nil)
	if e != nil || again.(tasks.Task).ID != queuedTask.ID {
		t.Fatal("clone submission not idempotent", e)
	}
	if e = cloneManager.EnsureIdle(ctx, c.Endpoint(), "datastore:"+stores[0].Name+"/"+queued.SrcFolder+"/disk"); e == nil {
		t.Fatal("source folder not reserved")
	}
	if e = cloneManager.EnsureIdle(ctx, c.Endpoint(), "datastore:"+stores[1].Name+"/"+queued.Dst+"/disk"); e == nil {
		t.Fatal("target folder not reserved")
	}
	if e = cloneManager.EnsureIdle(ctx, c.Endpoint(), "datastore:"+stores[0].Name+"/"+queued.Dst+"/disk"); e != nil {
		t.Fatal("target locked on source store", e)
	}
	if e = cloneManager.Cancel(queuedTask.ID); e != nil {
		t.Fatal(e)
	}
	close(gate)
	cancelled, _ := cloneManager.Wait(ctx, queuedTask.ID)
	if cancelled.Status != "cancelled" {
		t.Fatal("queued clone cancellation failed", cancelled)
	}
	if _, e = os.Stat(filepath.Join(stores[1].URL, queued.Dst)); !os.IsNotExist(e) {
		t.Fatal("cancelled queued clone wrote files", e)
	}
	c.Gov.Client.RoundTripper = c.Gov.Client.RoundTripper.(rejectDescriptorFileCopy).next
	source := filepath.Join(stores[0].URL, "source.txt")
	if e = os.WriteFile(source, []byte("fixture-copy"), 0600); e != nil {
		t.Fatal(e)
	}
	m, e := tasks.Open(filepath.Join(t.TempDir(), "config.yaml"), "serve")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	out, e := CopyMove(ctx, m, c, "lab", c.Endpoint(), s.Datastore, "source.txt", "copied.txt", true, false, true, "copy-key", nil)
	if e != nil {
		t.Fatal(e)
	}
	job := out.(tasks.Task)
	if _, e = m.Wait(ctx, job.ID); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(stores[0].URL, "copied.txt"))
	if e != nil || string(data) != "fixture-copy" {
		t.Fatal(string(data), e)
	}
	out, e = CopyMove(ctx, m, c, "lab", c.Endpoint(), s.Datastore, "copied.txt", "moved.txt", false, false, true, "move-key", nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Wait(ctx, out.(tasks.Task).ID); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(stores[0].URL, "copied.txt")); !os.IsNotExist(e) {
		t.Fatal("move did not remove source")
	}
	if _, e = CopyMove(ctx, m, c, "lab", c.Endpoint(), s.Datastore, "source.txt", "moved.txt", true, false, true, "", nil); e == nil {
		t.Fatal("existing destination passed preflight")
	}
	replay, e := CopyMove(ctx, m, c, "lab", c.Endpoint(), s.Datastore, "copied.txt", "moved.txt", false, false, true, "move-key", nil)
	if e != nil || replay.(tasks.Task).Kind != "file.move" {
		t.Fatal("completed move replay failed", e)
	}
	data, e = os.ReadFile(filepath.Join(stores[0].URL, "moved.txt"))
	if e != nil || string(data) != "fixture-copy" {
		t.Fatal("destination was overwritten", e)
	}
	page, e := m.List(tasks.Query{Kind: "file.copy"})
	if e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
}
