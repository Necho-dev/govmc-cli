package tui

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/httpserver"
	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/profile/history"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
)

func TestProfileRenameHistoryAcrossSurfaces(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if e := target.Edit(ctx, path, func(c *target.Config) error {
		p := target.Profile{ID: history.LegacyID("legacy", "https://host.invalid/sdk", "root"), URL: "https://host.invalid/sdk", Username: "root"}
		if e := target.SetPassword(path, "legacy", &p, "fixture-secret"); e != nil {
			return e
		}
		c.Profiles["legacy"] = p
		c.Active = "legacy"
		c.MCP.Mode = target.MCPReadwrite
		c.Profiles["other"] = target.Profile{URL: "https://other.invalid/sdk", Username: "root"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	store, e := journal.Open(path, "tui")
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	manager, e := tasks.Open(path, "cli")
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	ctx = tasks.With(journal.With(ctx, store), manager)
	done, e := manager.Submit(ctx, "legacy", "https://host.invalid/sdk", "file.copy", "rename-fixture-complete", nil, "complete", "fixture-key", func(context.Context, *tasks.Reporter) (any, error) { return nil, nil })
	if e != nil {
		t.Fatal(e)
	}
	wait := func(id, status string) {
		t.Helper()
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) {
			v, e := manager.Get(id)
			if e == nil && v.Status == status {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("task did not reach %s", status)
	}
	wait(done.ID, "succeeded")
	active, e := manager.Submit(ctx, "legacy", "https://host.invalid/sdk", "file.copy", "rename-fixture-active", nil, "active", "", func(c context.Context, r *tasks.Reporter) (any, error) {
		r.Step("copy", "running")
		<-c.Done()
		return nil, c.Err()
	})
	if e != nil {
		t.Fatal(e)
	}
	wait(active.ID, "running")
	ev, e := store.Begin(ctx, journal.Event{Profile: "legacy", Operation: "rename-fixture.audit"})
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Finish(ctx, ev, nil); e != nil {
		t.Fatal(e)
	}
	if e = store.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", filepath.Join(filepath.Dir(path), "data.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	// Existing databases have no profileId. Retain original record bytes through queries.
	if _, e = db.Exec("UPDATE tasks SET record=json_remove(record,'$.profileId') WHERE id=?", done.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("UPDATE audit_events SET record=json_remove(record,'$.profileId') WHERE id=?", ev.ID); e != nil {
		t.Fatal(e)
	}
	var rawTask, rawAudit string
	_ = db.QueryRow("SELECT record FROM tasks WHERE id=?", done.ID).Scan(&rawTask)
	_ = db.QueryRow("SELECT record FROM audit_events WHERE id=?", ev.ID).Scan(&rawAudit)
	dir := filepath.Join(filepath.Dir(path), "logs", "cli-"+strings.Repeat("a", 32))
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	rawLog, _ := json.Marshal(journal.Event{ID: "raw-runtime-fixture", Time: time.Now().Add(-time.Second).UTC(), Source: "cli", Level: "INFO", Event: "rename-fixture.runtime", Profile: "legacy"})
	logPath := filepath.Join(dir, "fixture.jsonl")
	if e = os.WriteFile(logPath, append(rawLog, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	process := func(input string, args ...string) []byte {
		t.Helper()
		c, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		cmd := exec.CommandContext(c, cliBinary(t), args...)
		cmd.Env = append(os.Environ(), "GOVMC_TASK_QUERY_HELPER=1", "GOVMC_CONFIG="+path, "GOVMC_PROFILE=", "GOVMC_LANG=en")
		cmd.Stdin = strings.NewReader(input)
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		if e := cmd.Run(); e != nil {
			t.Fatalf("%v: %s %v", args, &stderr, e)
		}
		return out.Bytes()
	}
	cli := func(args ...string) []byte { return process("", append(args, "--json")...) }
	rpc := func(name string, args map[string]any) []byte {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		raw := process(string(b)+"\n", "mcp", "serve")
		var r struct {
			Result struct {
				IsError bool                    `json:"isError"`
				Content []struct{ Text string } `json:"content"`
			}
		}
		if e := json.Unmarshal(raw, &r); e != nil || r.Result.IsError || len(r.Result.Content) != 1 {
			t.Fatalf("MCP: %s %v", raw, e)
		}
		return []byte(r.Result.Content[0].Text)
	}
	cli("profiles", "rename", "legacy", "hongkong")
	cfg, _ := target.Load(path)
	if cfg.Active != "hongkong" || len(cfg.ProfileHistory) != 1 {
		t.Fatal("rename metadata missing")
	}
	server := &httpserver.Server{Tasks: manager, Journal: store, Listen: "127.0.0.1:8765", Token: "fixture-token"}
	server.Profiles = httpserver.NewEnvironmentManager(path, target.Target{Name: "hongkong"}, server)
	handler := server.Handler()
	web := func(url string) []byte {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+url, nil)
		r.Header.Set("X-API-Token", "fixture-token")
		r.Header.Set("X-Govmc-Profile", "hongkong")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("Web %s: %d %s", url, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	var canonical tasks.Page
	for channel, raw := range map[string][]byte{"CLI": cli("--profile", "hongkong", "tasks", "list", "--search", "rename-fixture", "--limit", "50"), "MCP": rpc("esxi_tasks_list", map[string]any{"profile": "hongkong", "search": "rename-fixture", "limit": 50}), "Web": web("/v1/tasks?search=rename-fixture&limit=50")} {
		var p tasks.Page
		if e := json.Unmarshal(raw, &p); e != nil || len(p.Items) != 2 || p.Active != 1 {
			t.Fatalf("%s tasks %s %v", channel, raw, e)
		}
		for _, v := range p.Items {
			if v.Profile != "hongkong" || v.OriginalProfile != "legacy" || v.ProfileID == "" {
				t.Fatalf("%s lost ownership %+v", channel, v)
			}
		}
		if canonical.Items != nil && !reflect.DeepEqual(canonical, p) {
			t.Fatalf("%s differs", channel)
		}
		canonical = p
	}
	for _, id := range []string{done.ID, active.ID} {
		var canonical tasks.Task
		for channel, raw := range map[string][]byte{"CLI": cli("--profile", "hongkong", "tasks", "status", id), "MCP": rpc("esxi_task_status", map[string]any{"profile": "hongkong", "taskId": id}), "Web": web("/v1/tasks/" + id)} {
			var v tasks.Task
			if e := json.Unmarshal(raw, &v); e != nil || v.Profile != "hongkong" {
				t.Fatalf("%s detail %s %v", channel, raw, e)
			}
			if canonical.ID != "" && !reflect.DeepEqual(canonical, v) {
				t.Fatalf("%s details differ", channel)
			}
			canonical = v
		}
	}
	app, pages, _, _ := tuiDialogHarness(t)
	settings := newTUISettings(ctx, app, path, store, func() language { return "en" }, func(language) {}, func() target.Target { return target.Target{Name: "hongkong"} }, func() {})
	settings.pages = pages
	settings.width = 128
	settings.module = 3
	settings.taskSearch = "rename-fixture"
	settings.Render()
	if settings.table.GetRowCount() != 3 {
		t.Fatal("TUI tasks missing")
	}
	settings.showTask(done.ID)
	if !strings.Contains(settings.taskDetails.GetText(false), "hongkong") || !strings.Contains(settings.taskDetails.GetText(false), "legacy") {
		t.Fatal("TUI task details lost history")
	}
	pages.RemovePage("task-detail")
	for _, kind := range []string{"logs", "audits"} {
		search := "rename-fixture.runtime"
		tool := "esxi_logs_list"
		module := 4
		if kind == "audits" {
			search = "rename-fixture.audit"
			tool = "esxi_audits_list"
			module = 5
		}
		var canonical journal.Result
		for channel, raw := range map[string][]byte{"CLI": cli("--profile", "hongkong", kind, "--search", search, "--limit", "50"), "MCP": rpc(tool, map[string]any{"profile": "hongkong", "search": search, "limit": 50}), "Web": web("/v1/settings/" + kind + "?profile=hongkong&search=" + search + "&limit=50")} {
			var v journal.Result
			if e := json.Unmarshal(raw, &v); e != nil || len(v.Items) != 1 || v.Items[0].Profile != "hongkong" || v.Items[0].OriginalProfile != "legacy" {
				t.Fatalf("%s %s: %s %v", channel, kind, raw, e)
			}
			if canonical.Items != nil && !reflect.DeepEqual(canonical, v) {
				t.Fatalf("%s %s differs", channel, kind)
			}
			canonical = v
		}
		settings.module = module
		settings.search = search
		settings.queryUntil = time.Time{}
		settings.Render()
		if settings.table.GetRowCount() != 2 {
			t.Fatalf("TUI %s missing", kind)
		}
	}
	var afterTask, afterAudit string
	_ = db.QueryRow("SELECT record FROM tasks WHERE id=?", done.ID).Scan(&afterTask)
	_ = db.QueryRow("SELECT record FROM audit_events WHERE id=?", ev.ID).Scan(&afterAudit)
	afterLog, _ := os.ReadFile(logPath)
	if rawTask != afterTask || rawAudit != afterAudit || !bytes.Equal(afterLog, append(rawLog, '\n')) {
		t.Fatal("queries rewrote original history")
	}
	if v, found, e := manager.Replay(ctx, "hongkong", "https://host.invalid/sdk", "file.copy", "fixture-key"); e != nil || !found || v.ID != done.ID {
		t.Fatalf("rename broke idempotency: %+v %v", v, e)
	}
	rpc("esxi_task_cancel", map[string]any{"profile": "hongkong", "taskId": active.ID, "confirm": true})
	wait(active.ID, "cancelled")
	if v, _ := manager.Get(active.ID); v.Profile != "hongkong" || v.OriginalProfile != "legacy" {
		t.Fatal("live writer lost identity after rename")
	}
	// Chained rename and reusing the old name must not give the new environment old history.
	cli("profiles", "rename", "hongkong", "hk")
	if e := target.Edit(context.Background(), path, func(c *target.Config) error {
		c.Profiles["legacy"] = target.Profile{URL: "https://new.invalid/sdk", Username: "new"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if p, e := manager.List(tasks.Query{Profile: "hk", Search: "rename-fixture"}); e != nil || len(p.Items) != 2 {
		t.Fatal("chained rename lost tasks", e)
	}
	if p, e := manager.List(tasks.Query{Profile: "legacy"}); e != nil || len(p.Items) != 0 {
		t.Fatal("reused name exposed old tasks", e)
	}
	for _, query := range []func(context.Context, journal.Query) (journal.Result, error){store.Logs, store.Audits} {
		if p, e := query(context.Background(), journal.Query{Profile: "legacy", Search: "rename-fixture"}); e != nil || len(p.Items) != 0 {
			t.Fatal("reused name exposed old journal", e)
		}
	}
	// Reopened stores have identical ownership, without any in-memory rename state.
	reader, e := tasks.Open(path, "mcp")
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Close()
	if p, e := reader.List(tasks.Query{Profile: "hk", Search: "rename-fixture"}); e != nil || len(p.Items) != 2 {
		t.Fatal("restart lost renamed history", e)
	}
}
