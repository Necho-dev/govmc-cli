package tui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/vmware/govmomi/simulator"
)

// Opt in because vcsim needs a local listening socket. Never contacts a real ESXi.
func TestTUISimulatorPagesAndEnvironment(t *testing.T) {
	if os.Getenv("GOVMC_TUI_SIM_TEST") != "1" {
		t.Skip("set GOVMC_TUI_SIM_TEST=1 to run the local simulator")
	}
	model := simulator.ESX()
	model.Machine = 40
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	defer model.Remove()
	// Use realistic configured resources so simulator metrics have a meaningful denominator.
	for _, entity := range model.Map().All("VirtualMachine") {
		vm := entity.(*simulator.VirtualMachine)
		vm.Config.Hardware.MemoryMB = 8192
		vm.Config.Hardware.NumCPU = 4
		vm.Summary.Config.MemorySizeMB = 8192
		vm.Summary.Config.NumCpu = 4
	}
	model.Service.TLS = &tls.Config{}
	server := model.Service.NewServer()
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	endpoint := *server.URL
	username := endpoint.User.Username()
	password, _ := endpoint.User.Password()
	endpoint.User = nil
	if err := target.Edit(context.Background(), configPath, func(config *target.Config) error {
		for _, name := range []string{"simulator", "other", "broken"} {
			p := target.Profile{URL: endpoint.String(), Username: username, Insecure: true}
			if name == "broken" {
				p.URL = "https://127.0.0.1:1"
			}
			if err := target.SetPassword(configPath, name, &p, password); err != nil {
				return err
			}
			config.Profiles[name] = p
		}
		config.Active = "simulator"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	conn, err := target.Named(configPath, "simulator")
	if err != nil {
		t.Fatal(err)
	}
	browserClient, err := conn.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stores, err := browserClient.Datastores(context.Background())
	target.Close(browserClient)
	if err != nil || len(stores) == 0 {
		t.Fatalf("simulated datastore unavailable: %v", err)
	}
	storeName := stores[0].Name
	fixtureDir := filepath.Join(stores[0].URL, "qa-browse")
	if err := os.MkdirAll(filepath.Join(fixtureDir, "subfolder"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.vmx", "disk.vmdk"} {
		if err := os.WriteFile(filepath.Join(fixtureDir, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	screen := tcell.NewSimulationScreen("UTF-8")
	app := tview.NewApplication().SetScreen(screen)
	done := make(chan error, 1)
	go func() { done <- runTUIApplication(ctx, configPath, conn, "zh", app) }()
	defer func() {
		app.Stop()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("TUI did not stop")
		}
	}()
	read := func() string {
		var out strings.Builder
		app.QueueUpdateDraw(func() {
			cells, w, _ := screen.GetContents()
			skip := false
			for i, c := range cells {
				if i%w == 0 {
					out.WriteByte('\n')
					skip = false
				}
				if skip {
					skip = false
					continue
				}
				out.WriteString(string(c.Runes))
				skip = len(c.Runes) > 0 && c.Runes[0] >= 0x2e80 && c.Runes[0] <= 0xffef
			}
		})
		return out.String()
	}
	wait := func(needle string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(strings.ReplaceAll(read(), " ", ""), strings.ReplaceAll(needle, " ", "")) {
				return
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatalf("screen missing %q:\n%s", needle, read())
	}
	waitFocus := func(input bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			matched := false
			app.QueueUpdateDraw(func() {
				if input {
					_, matched = app.GetFocus().(*tview.InputField)
				} else {
					_, matched = app.GetFocus().(*tview.Table)
				}
			})
			if matched {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("focus did not reach expected control")
	}
	// Application.Init resets the simulation size; resize after Run has started.
	wait("总览")
	app.QueueUpdateDraw(func() { screen.SetSize(128, 48) })
	screen.PostEventWait(tcell.NewEventResize(128, 48))
	wait("localhost")
	capture := func(name string) {
		directory := os.Getenv("GOVMC_TUI_CAPTURE_DIR")
		if directory == "" {
			return
		}
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		app.QueueUpdateDraw(func() {
			type cell struct {
				Text   string
				FG, BG int32
			}
			cells, w, h := screen.GetContents()
			data := struct {
				W, H  int
				Cells []cell
			}{W: w, H: h}
			for _, c := range cells {
				fg, bg, _ := c.Style.Decompose()
				data.Cells = append(data.Cells, cell{string(c.Runes), fg.Hex(), bg.Hex()})
			}
			raw, _ := json.Marshal(data)
			if err := os.WriteFile(filepath.Join(directory, name+".json"), raw, 0600); err != nil {
				t.Error(err)
			}
		})
	}
	capture("overview")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, '2', tcell.ModNone))
	wait("ha-host_VM32")
	if strings.Contains(strings.ReplaceAll(read(), " ", ""), "EntertoloadVMtrends") {
		t.Fatal("unopened trend panel still reserves table space")
	}
	capture("vms")

	// Filtering and scrolling survive an Enter -> detail -> Esc round trip.
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'f', tcell.ModNone))
	waitFocus(true)
	app.QueueUpdateDraw(func() { app.GetFocus().(*tview.InputField).SetText("ha-host_VM") })
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	waitFocus(false)
	var list *tview.Table
	var beforeRow, beforeCol, beforeOffsetRow, beforeOffsetCol int
	app.QueueUpdateDraw(func() { list = app.GetFocus().(*tview.Table) })
	for i := 0; i < 40; i++ {
		screen.PostEventWait(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		atEnd := false
		app.QueueUpdateDraw(func() { row, _ := list.GetSelection(); atEnd = row == list.GetRowCount()-1 })
		if atEnd {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.QueueUpdateDraw(func() {
		beforeRow, beforeCol = list.GetSelection()
		beforeOffsetRow, beforeOffsetCol = list.GetOffset()
		if beforeOffsetRow == 0 {
			t.Error("test did not exercise a scrolled list")
		}
	})
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	wait("系统与状态")
	wait("磁盘吞吐")
	if strings.Contains(read(), "ha-host_VM0 ") {
		t.Fatal("detail page still includes the machine list")
	}
	capture("details")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	wait("预览变更")
	capture("editor")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	wait("系统与状态")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	wait("ha-host_VM32")
	app.QueueUpdateDraw(func() {
		row, col := list.GetSelection()
		offsetRow, offsetCol := list.GetOffset()
		if app.GetFocus() != list || row != beforeRow || col != beforeCol || offsetRow != beforeOffsetRow || offsetCol != beforeOffsetCol {
			t.Errorf("list state lost: selection %d/%d -> %d/%d, offset %d/%d -> %d/%d", beforeRow, beforeCol, row, col, beforeOffsetRow, beforeOffsetCol, offsetRow, offsetCol)
		}
	})
	if !strings.Contains(strings.ReplaceAll(read(), " ", ""), "ha-host_VM") {
		t.Fatal("filter state lost")
	}
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, '3', tcell.ModNone))
	wait(storeName)
	wait("总量 GiB")
	capture("datastores")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'f', tcell.ModNone))
	waitFocus(true)
	app.QueueUpdateDraw(func() { app.GetFocus().(*tview.InputField).SetText(storeName) })
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	waitFocus(false)
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	wait("[DIR]ha-host_VM0/")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'f', tcell.ModNone))
	waitFocus(true)
	app.QueueUpdateDraw(func() { app.GetFocus().(*tview.InputField).SetText("qa-browse") })
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	waitFocus(false)
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	wait("[FILE]config.vmx")
	wait("[DIR]subfolder/")
	capture("datastore-browser")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	wait("筛选：qa-browse")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	wait("总量 GiB")
	wait("筛选：" + storeName)
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, '4', tcell.ModNone))
	wait("cpu.usage.average")
	wait("最新值")
	wait("采样记录")
	capture("metrics")
	if strings.Contains(read(), "Enter to select a sampling target") {
		t.Fatal("metric values require an extra action")
	}
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	wait("ha-host_VM")
	wait("cpu.usage.average")
	capture("samples")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	wait("采样对象")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	wait("Enter 连接")
	capture("environments")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	wait("编辑环境")
	capture("environment-editor")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	wait("Enter 连接")
	// Wrong credentials/endpoint must not change the active environment.
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	wait("确认从 simulator 切换到 broken")
	capture("environment-confirm")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone))
	wait("connect to profile")
	config, _ := target.Load(configPath)
	if config.Active != "simulator" {
		t.Fatal("failed switch changed active environment")
	}
	// Retry must reread corrected credentials/settings for the failed target.
	if err := target.Edit(ctx, configPath, func(config *target.Config) error {
		p := config.Profiles["broken"]
		p.URL = endpoint.String()
		if err := target.SetPassword(configPath, "broken", &p, password); err != nil {
			return err
		}
		config.Profiles["broken"] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		saved, _ := target.Load(configPath)
		if saved.Active == "broken" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	config, _ = target.Load(configPath)
	if config.Active != "broken" {
		t.Fatal("retry refreshed the previous profile instead of reloading the failed profile")
	}
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	wait("Enter 连接")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	wait("切换到 other")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone))
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		saved, _ := target.Load(configPath)
		if saved.Active == "other" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	wait("" + username + "@" + endpoint.Host)
	config, _ = target.Load(configPath)
	if config.Active != "other" {
		t.Fatal("successful switch was not saved")
	}

	// The CLI flag fixes only this MCP process; each tool may override it.
	t.Setenv("GOVMC_CONFIG", configPath)
	t.Setenv("GOVMC_PROFILE", "")
	messages := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_doctor","arguments":{"profile":"other"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"esxi_doctor","arguments":{}}}
`
	command := exec.CommandContext(ctx, cliBinary(t), "--config", configPath, "--profile", "simulator", "mcp", "serve")
	command.Stdin = strings.NewReader(messages)
	var replies bytes.Buffer
	command.Stdout = &replies
	command.Stderr = &replies
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(replies.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 MCP replies, got %d", len(lines))
	}
	for index, line := range lines {
		var response struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatal(err)
		}
		actual := ""
		for _, content := range response.Result.Content {
			var record map[string]any
			if json.Unmarshal([]byte(content.Text), &record) == nil {
				if value, ok := record["profile"].(string); ok {
					actual = value
				}
			}
		}
		expected := []string{"other", "simulator"}[index]
		if actual != expected {
			t.Fatalf("MCP target: got %q, want %q", actual, expected)
		}
	}
	config, _ = target.Load(configPath)
	if config.Active != "other" {
		t.Fatal("MCP temporary profiles changed active environment")
	}

	// Removing the connected profile requires a second action and clears the session.
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	wait("Enter 连接")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	wait("删除已保存的环境 other")
	capture("environment-delete")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	wait("Enter 连接")
	config, _ = target.Load(configPath)
	if _, exists := config.Profiles["other"]; !exists {
		t.Fatal("default cancel removed the profile")
	}
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	wait("删除已保存的环境 other")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	wait("Enter 连接")
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	wait("u 选择环境")
	config, _ = target.Load(configPath)
	if _, exists := config.Profiles["other"]; exists || config.Active != "" {
		t.Fatal("deleting current profile did not clear saved state")
	}

	app.QueueUpdateDraw(func() { screen.SetSize(80, 24) })
	screen.PostEventWait(tcell.NewEventResize(80, 24))
	wait("选择环境")
	capture("narrow")
}
