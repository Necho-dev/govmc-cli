package command

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/httpserver"
	"github.com/Necho-dev/govmc-cli/internal/mcp"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/Necho-dev/govmc-cli/internal/workflow"
	"github.com/vmware/govmomi/simulator"
)

func TestSimulatorFileTransfersAcrossFrontends(t *testing.T) {
	if os.Getenv("GOVMC_WEB_SIM_TEST") != "1" {
		t.Skip("local simulator opt-in")
	}
	model := simulator.ESX()
	model.Machine = 0
	model.Datastore = 2
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	defer model.Remove()
	model.Service.TLS = &tls.Config{}
	server := model.Service.NewServer()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	endpoint := *server.URL
	username := endpoint.User.Username()
	password, _ := endpoint.User.Password()
	endpoint.User = nil
	if err := target.Edit(ctx, path, func(cfg *target.Config) error {
		p := target.Profile{URL: endpoint.String(), Username: username, Insecure: true}
		if e := target.SetPassword(path, "lab", &p, password); e != nil {
			return e
		}
		cfg.Profiles["lab"] = p
		cfg.Active = "lab"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	conn, err := target.Named(path, "lab")
	if err != nil {
		t.Fatal(err)
	}
	client, err := conn.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(client)
	stores, err := client.Datastores(ctx)
	if err != nil || len(stores) < 2 {
		t.Fatal(err)
	}
	ds := stores[0]
	targetDS := ds.Name
	managers := map[string]*tasks.Manager{}
	for _, source := range []string{"cli", "serve", "mcp", "tui"} {
		m, e := tasks.Open(path, source)
		if e != nil {
			t.Fatal(e)
		}
		managers[source] = m
		defer m.Close()
	}
	webServer := &httpserver.Server{ESXi: client, Tasks: managers["serve"], ProfileName: "lab", Token: "fixture", Listen: "127.0.0.1:8765"}
	webHandler := webServer.Handler()
	type runner func(bool, bool, bool, string, string) ([]byte, error)
	runners := map[string]runner{}
	runners["cli"] = func(copy, dry, confirm bool, src, dst string) ([]byte, error) {
		op := "move"
		if copy {
			op = "copy"
		}
		args := []string{"-test.run=^TestTaskQueryHelperProcess$", "--", "--profile", "lab", "datastore", op, ds.Name, src, dst, "--json"}
		if targetDS != ds.Name {
			args = append(args, "--dst-datastore", targetDS)
		}
		if dry {
			args = append(args, "--dry-run")
		}
		if confirm {
			args = append(args, "--yes")
		}
		cmd := exec.CommandContext(ctx, os.Args[0], args...)
		cmd.Env = append(os.Environ(), "GOVMC_TASK_QUERY_HELPER=1", "GOVMC_CONFIG="+path, "GOVMC_LANG=en")
		cmd.Stdin = strings.NewReader("")
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		e := cmd.Run()
		if e != nil {
			return nil, fmt.Errorf("CLI: %s %w", stderr.String(), e)
		}
		return out.Bytes(), nil
	}
	runners["serve"] = func(copy, dry, confirm bool, src, dst string) ([]byte, error) {
		op := "move"
		if copy {
			op = "copy"
		}
		raw, _ := json.Marshal(map[string]any{"ds": ds.Name, "dstDatastore": targetDS, "src": src, "dst": dst, "dryRun": dry, "confirm": confirm})
		r := httptest.NewRequest("POST", "http://127.0.0.1/v1/datastores/file/"+op, bytes.NewReader(raw))
		r.Header.Set("X-API-Token", "fixture")
		w := httptest.NewRecorder()
		webHandler.ServeHTTP(w, r)
		if w.Code != 200 && w.Code != 202 {
			return nil, fmt.Errorf("Web %d: %s", w.Code, w.Body.String())
		}
		return w.Body.Bytes(), nil
	}
	runners["mcp"] = func(copy, dry, confirm bool, src, dst string) ([]byte, error) {
		op := "esxi_ds_move"
		if copy {
			op = "esxi_ds_copy"
		}
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": op, "arguments": map[string]any{"datastore": ds.Name, "dstDatastore": targetDS, "src": src, "dst": dst, "dryRun": dry, "confirm": confirm}}})
		out, e := mcp.HandleConfigured(mcp.WithProfile(tasks.With(ctx, managers["mcp"]), "lab"), raw, func(context.Context, string) (*esxi.Client, string, func(), error) {
			return client, "lab", func() {}, nil
		}, mcp.Readwrite)
		if e != nil {
			return nil, e
		}
		var response struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if e = json.Unmarshal(out, &response); e != nil {
			return nil, e
		}
		if len(response.Result.Content) < 1 {
			return nil, fmt.Errorf("invalid MCP response: %s", out)
		}
		if response.Result.IsError {
			return nil, fmt.Errorf("MCP: %s", response.Result.Content[0].Text)
		}
		return []byte(response.Result.Content[0].Text), nil
	}
	// The TUI adapter calls this exact shared operation; keyboard confirmation is tested separately.
	runners["tui"] = func(copy, dry, confirm bool, src, dst string) ([]byte, error) {
		out, e := workflow.CopyMove(tasks.With(ctx, managers["tui"]), managers["tui"], client, "lab", client.Endpoint(), ds.Name, src, dst, copy, dry, confirm, "", nil, targetDS)
		if e != nil {
			return nil, e
		}
		return json.Marshal(out)
	}
	count := func() int {
		page, e := managers["serve"].List(tasks.Query{})
		if e != nil {
			t.Fatal(e)
		}
		return len(page.Items)
	}
	for _, cross := range []bool{false, true} {
		for source, run := range runners {
			t.Run(fmt.Sprintf("%s/cross=%t", source, cross), func(t *testing.T) {
				prefix := source
				if cross {
					prefix += "-cross"
				}
				src, dst, moved := prefix+"-source.txt", prefix+"-copy.txt", prefix+"-moved.txt"
				destination := ds
				targetDS = ds.Name
				if cross {
					destination = stores[1]
					targetDS = destination.Name
					dst = src
				}
				moveSource := dst
				if cross {
					moveSource = src
				}
				data := []byte("fixture-" + source)
				if e := os.WriteFile(filepath.Join(ds.URL, src), data, 0600); e != nil {
					t.Fatal(e)
				}
				before := count()
				raw, e := run(true, true, false, "./"+src, dst)
				if e != nil {
					t.Fatal(e)
				}
				var plan struct {
					DryRun bool                  `json:"dryRun"`
					Detail esxi.FileTransferPlan `json:"detail"`
				}
				if e = json.Unmarshal(raw, &plan); e != nil || !plan.DryRun || plan.Detail.Source != src || plan.Detail.Destination != dst || plan.Detail.Overwrite || plan.Detail.DestinationDatastore != targetDS {
					t.Fatalf("bad preview %s %v", raw, e)
				}
				if count() != before {
					t.Fatal("preview created task")
				}
				if _, e = os.Stat(filepath.Join(destination.URL, dst)); !os.IsNotExist(e) {
					t.Fatal("preview copied file")
				}
				if _, e = run(true, false, false, src, dst); e == nil || count() != before {
					t.Fatal("execution without confirmation accepted")
				}
				raw, e = run(true, false, true, src, dst)
				if e != nil {
					t.Fatal(e)
				}
				var job tasks.Task
				if e = json.Unmarshal(raw, &job); e != nil || job.ID == "" {
					t.Fatalf("missing task: %s %v", raw, e)
				}
				done, e := managers["serve"].Wait(ctx, job.ID)
				if e != nil || done.Status != "succeeded" || done.Source != source || done.Kind != "file.copy" || len(done.Steps) != 1 {
					t.Fatal(done, e)
				}
				copied, e := os.ReadFile(filepath.Join(destination.URL, dst))
				if e != nil || !bytes.Equal(copied, data) {
					t.Fatal("copy contents", e)
				}
				if _, e = os.Stat(filepath.Join(ds.URL, src)); e != nil {
					t.Fatal("copy removed source", e)
				}
				before = count()
				if _, e = run(true, true, false, src, dst); e == nil || count() != before {
					t.Fatal("overwrite passed preview")
				}
				if _, e = run(true, true, false, "missing.txt", source+"-other.txt"); e == nil {
					t.Fatal("missing source passed preview")
				}
				if _, e = run(true, true, false, src, "missing-parent/child.txt"); e == nil {
					t.Fatal("missing parent passed preview")
				}
				if _, e = run(true, true, false, src, src+"/child"); e == nil {
					t.Fatal("descendant destination passed preview")
				}
				if _, e = run(false, true, false, moveSource, moved); e != nil || count() != before {
					t.Fatal("move preview", e)
				}
				raw, e = run(false, false, true, moveSource, moved)
				if e != nil {
					t.Fatal(e)
				}
				if e = json.Unmarshal(raw, &job); e != nil {
					t.Fatal(e)
				}
				done, e = managers["serve"].Wait(ctx, job.ID)
				if e != nil || done.Kind != "file.move" || done.Source != source {
					t.Fatal(done, e)
				}
				movedBytes, e := os.ReadFile(filepath.Join(destination.URL, moved))
				if e != nil || !bytes.Equal(movedBytes, data) {
					t.Fatal("move contents", e)
				}
				if _, e = os.Stat(filepath.Join(ds.URL, moveSource)); !os.IsNotExist(e) {
					t.Fatal("move retained source")
				}
			})
		}
	}
}
