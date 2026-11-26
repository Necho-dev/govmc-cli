package tui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/httpserver"
	"github.com/Necho-dev/govmc-cli/internal/mcp"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"net/http/httptest"
)

func TestSimulatorCloneDestinationPreviewAcrossFrontends(t *testing.T) {
	if os.Getenv("GOVMC_WEB_SIM_TEST") != "1" {
		t.Skip("local simulator opt-in")
	}
	model := simulator.ESX()
	model.Machine = 1
	model.Datastore = 2
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	defer model.Remove()
	model.Service.TLS = &tls.Config{}
	server := model.Service.NewServer()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	u := *server.URL
	username := u.User.Username()
	password, _ := u.User.Password()
	u.User = nil
	if err := target.Edit(ctx, configPath, func(cfg *target.Config) error {
		p := target.Profile{URL: u.String(), Username: username, Insecure: true}
		if e := target.SetPassword(configPath, "lab", &p, password); e != nil {
			return e
		}
		cfg.Profiles["lab"] = p
		cfg.Active = "lab"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	conn, err := target.Named(configPath, "lab")
	if err != nil {
		t.Fatal(err)
	}
	c, err := conn.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(c)
	stores, err := c.Datastores(ctx)
	if err != nil || len(stores) != 2 {
		t.Fatal(err)
	}
	vms, err := c.ListVMs(ctx, false)
	if err != nil || len(vms) != 1 {
		t.Fatal(err)
	}
	vm := vms[0]
	if err = c.Power(ctx, vm.ID, "off", true); err != nil {
		t.Fatal(err)
	}
	vm, err = c.DescribeVM(ctx, vm.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, disk := range vm.Disks {
		var backing object.DatastorePath
		if !backing.FromString(disk.FileName) {
			t.Fatal(disk)
		}
		folder := filepath.Join(stores[0].URL, path.Dir(backing.Path))
		if err = os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
		base := strings.TrimSuffix(path.Base(backing.Path), ".vmdk")
		if err = os.WriteFile(filepath.Join(folder, base+"-flat.vmdk"), make([]byte, 512), 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(folder, base+".vmdk"), []byte(esxi.VmdkDescriptor(base+"-flat.vmdk", 512)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manager, err := tasks.Open(configPath, "serve")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	spec := esxi.CloneSpec{Src: vm.ID, Dst: "cross-preview", DestinationDatastore: stores[1].Name}
	check := func(name string, raw []byte) {
		t.Helper()
		var plan esxi.ClonePlan
		if e := json.Unmarshal(raw, &plan); e != nil || plan.Configuration == nil || plan.SourceDatastore != stores[0].Name || plan.DestinationDatastore != stores[1].Name || plan.Configuration.TargetDatastore() != stores[1].Name || !plan.DryRun {
			t.Fatalf("%s bad preview: %s %v", name, raw, e)
		}
	}
	args := []string{"--profile", "lab", "vm", "clone", vm.ID, spec.Dst, "--dst-datastore", stores[1].Name, "--dry-run", "--json"}
	cmd := exec.CommandContext(ctx, cliBinary(t), args...)
	cmd.Env = append(os.Environ(), "GOVMC_TASK_QUERY_HELPER=1", "GOVMC_CONFIG="+configPath, "GOVMC_LANG=en")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(stderr.String(), err)
	}
	check("CLI", raw)
	api := &httpserver.Server{ESXi: c, Tasks: manager, ProfileName: "lab", Token: "token", Listen: "127.0.0.1:8765"}
	payload, _ := json.Marshal(spec)
	request := httptest.NewRequest("POST", "http://127.0.0.1/v1/custom/clone", bytes.NewReader(payload))
	request.Header.Set("X-API-Token", "token")
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, request)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	check("Web", w.Body.Bytes())
	rpc, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "esxi_vm_clone", "arguments": spec}})
	raw, err = mcp.HandleConfigured(mcp.WithProfile(tasks.With(ctx, manager), "lab"), rpc, func(context.Context, string) (*esxi.Client, string, func(), error) { return c, "lab", func() {}, nil }, mcp.Readonly)
	var response struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err != nil || json.Unmarshal(raw, &response) != nil || response.Result.IsError || len(response.Result.Content) == 0 {
		t.Fatal(string(raw), err)
	}
	check("MCP", []byte(response.Result.Content[0].Text))
	// Exercise the actual TUI form, target selection and readonly confirmation.
	screen := tcell.NewSimulationScreen("UTF-8")
	app := tview.NewApplication().SetScreen(screen)
	pages := tview.NewPages()
	previous := tview.NewTable()
	pages.AddPage("main", previous, true, true)
	app.SetRoot(pages, true).SetFocus(previous)
	stopped := make(chan error, 1)
	go func() { stopped <- app.Run() }()
	defer func() {
		app.Stop()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Error("TUI did not stop")
		}
	}()
	app.QueueUpdateDraw(func() {
		screen.SetSize(128, 45)
		openTUIClone(tasks.With(ctx, manager), app, pages, conn, vm, "zh", func(tasks.Task) { t.Error("preview submitted a task") }, stores[1].Name)
		app.GetFocus().(*tview.InputField).SetText("tui-cross-preview")
	})
	key := func(k tcell.Key, r rune) { screen.PostEventWait(tcell.NewEventKey(k, r, tcell.ModNone)) }
	wait := func(f func() bool) {
		t.Helper()
		until := time.Now().Add(8 * time.Second)
		for time.Now().Before(until) {
			ok := false
			app.QueueUpdateDraw(func() { ok = f() })
			if ok {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("TUI preview state not reached")
	}
	key(tcell.KeyTab, 0)
	wait(func() bool {
		d, ok := app.GetFocus().(*tview.DropDown)
		return ok && d.GetLabel() == "目标数据存储"
	})
	app.QueueUpdateDraw(func() { app.GetFocus().(*tview.DropDown).SetCurrentOption(1) })
	// Tab through the remaining form fields to the Preview button.
	for i := 0; i < 9; i++ {
		key(tcell.KeyTab, 0)
	}
	wait(func() bool { b, ok := app.GetFocus().(*tview.Button); return ok && b.GetLabel() == "预览变更" })
	key(tcell.KeyEnter, 0)
	wait(func() bool { name, _ := pages.GetFrontPage(); return name == "clone-confirm" })
	var text strings.Builder
	app.QueueUpdateDraw(func() {
		cells, _, _ := screen.GetContents()
		for _, cell := range cells {
			text.WriteString(string(cell.Runes))
		}
	})
	if !strings.Contains(text.String(), stores[1].Name) {
		t.Fatal("TUI preview lost destination datastore")
	}
	key(tcell.KeyEscape, 0)
	wait(func() bool { name, _ := pages.GetFrontPage(); return name == "clone-editor" })
	page, err := manager.List(tasks.Query{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("previews created execution tasks", err)
	}
	for _, name := range []string{spec.Dst, "tui-cross-preview"} {
		if _, err = os.Stat(filepath.Join(stores[1].URL, name)); !os.IsNotExist(err) {
			t.Fatal("preview created destination", err)
		}
	}
}
