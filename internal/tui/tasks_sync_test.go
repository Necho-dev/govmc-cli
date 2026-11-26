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

	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/httpserver"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
)

func TestTaskQueriesSynchronizeAcrossWebCLITUIAndMCP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	ctx := context.Background()
	if err := target.Edit(ctx, path, func(c *target.Config) error {
		c.Active = "lab"
		for _, name := range []string{"lab", "prod"} {
			c.Profiles[name] = target.Profile{URL: "https://" + name + ".invalid/sdk", Username: "root"}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	owners := map[string]*tasks.Manager{}
	for _, source := range []string{"cli", "serve", "tui", "mcp"} {
		m, err := tasks.Open(path, source)
		if err != nil {
			t.Fatal(err)
		}
		owners[source] = m
		t.Cleanup(m.Close)
	}
	releases := map[string]chan struct{}{}
	jobs := map[string]tasks.Task{}
	for source, m := range owners {
		release := make(chan struct{})
		releases[source] = release
		job, err := m.Submit(ctx, "lab", "https://lab.invalid/sdk", "file.copy", "sync-"+source, nil, "sync-"+source, "", func(ctx context.Context, r *tasks.Reporter) (any, error) {
			r.Step("copy", "running")
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
			}
			r.Step("copy", "ok")
			return map[string]bool{"copied": true}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		jobs[source] = job
	}
	hidden, err := owners["cli"].Submit(ctx, "prod", "https://prod.invalid/sdk", "file.move", "private-prod", nil, "private-prod", "", func(context.Context, *tasks.Reporter) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	wait := func(id, state string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			v, e := owners["serve"].Get(id)
			if e == nil && v.Status == state {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}

		for owner, m := range owners {
			v, e := m.Get(id)
			t.Logf("reader %s state=%s cancel=%v err=%v", owner, v.Status, v.CancelRequested, e)
		}
		latest, readErr := owners["serve"].Get(id)
		t.Fatalf("task %s did not reach %s: %+v read=%v", id, state, latest, readErr)
	}
	for _, job := range jobs {
		wait(job.ID, "running")
	}
	wait(hidden.ID, "succeeded")
	process := func(input string, args ...string) []byte {
		t.Helper()
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(runCtx, cliBinary(t), args...)
		cmd.Env = append(os.Environ(), "GOVMC_TASK_QUERY_HELPER=1", "GOVMC_CONFIG="+path, "GOVMC_PROFILE=", "GOVMC_LANG=en")
		cmd.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if e := cmd.Run(); e != nil {
			t.Fatalf("%v: %s %v", args, stderr.String(), e)
		}
		return stdout.Bytes()
	}
	cli := func(args ...string) []byte { return process("", append(args, "--json")...) }
	rpc := func(name string, args map[string]any) []byte {
		request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 17, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		out := process(string(request)+"\n", "mcp", "serve")
		var response struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if e := json.Unmarshal(out, &response); e != nil || response.Result.IsError || len(response.Result.Content) != 1 {
			t.Fatalf("MCP response: %s %v", out, e)
		}
		return []byte(response.Result.Content[0].Text)
	}
	server := &httpserver.Server{Tasks: owners["serve"], Token: "test-token", Listen: "127.0.0.1:8765"}
	server.Profiles = httpserver.NewEnvironmentManager(path, target.Target{Name: "lab"}, server)
	handler := server.Handler()
	web := func(method, url, profile, body string) []byte {
		t.Helper()
		request := httptest.NewRequest(method, "http://127.0.0.1"+url, strings.NewReader(body))
		request.Header.Set("X-API-Token", "test-token")
		request.Header.Set("X-Govmc-Profile", profile)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, request)
		if result.Code != http.StatusOK {
			t.Fatalf("Web %s: %d %s", url, result.Code, result.Body.String())
		}
		return result.Body.Bytes()
	}
	app, pages, _, _ := tuiDialogHarness(t)
	s := newTUISettings(tasks.With(ctx, owners["tui"]), app, path, nil, func() language { return "en" }, func(language) {}, func() target.Target { return target.Target{Name: "lab"} }, func() {})
	s.pages = pages
	s.module = 3
	s.width = 128
	compare := func() {
		t.Helper()
		for name, raw := range map[string][]byte{"CLI": cli("--profile", "lab", "tasks", "list"), "Web": web("GET", "/v1/tasks", "lab", ""), "MCP": rpc("esxi_tasks_list", map[string]any{"profile": "lab"})} {
			var page tasks.Page
			if err := json.Unmarshal(raw, &page); err != nil {
				t.Fatalf("%s: %s %v", name, raw, err)
			}
			if len(page.Items) != len(jobs) {
				t.Fatalf("%s task count %d", name, len(page.Items))
			}
			seen := map[string]bool{}
			for _, item := range page.Items {
				live, _ := owners["serve"].Get(item.ID)
				if item.Status != live.Status || item.Profile != "lab" {
					t.Fatalf("%s stale or wrong profile: %+v", name, item)
				}
				seen[item.ID] = true
			}
			for _, job := range jobs {
				if !seen[job.ID] {
					t.Fatalf("%s lost task %s", name, job.ID)
				}
			}
		}
		s.Render()
		if s.table.GetRowCount() != len(jobs)+1 {
			t.Fatal("TUI task count", s.table.GetRowCount())
		}
		for row := 1; row < s.table.GetRowCount(); row++ {
			s.table.Select(row, 0)
			item, err := owners["serve"].Get(s.taskSelected)
			if err != nil || s.table.GetCell(row, 2).Text != item.Status {
				t.Fatal("TUI stale state", err)
			}
			s.showTask(item.ID)
			if !strings.Contains(s.taskDetails.GetText(false), item.ID) || !strings.Contains(s.taskDetails.GetText(false), item.Status) {
				t.Fatal("TUI missing task detail")
			}
			pages.RemovePage("task-detail")
		}
	}
	compare()
	for _, job := range jobs {
		for channel, raw := range map[string][]byte{"CLI": cli("--profile", "lab", "tasks", "status", job.ID), "Web": web("GET", "/v1/tasks/"+job.ID, "lab", ""), "MCP": rpc("esxi_task_status", map[string]any{"profile": "lab", "taskId": job.ID})} {
			var item tasks.Task
			if e := json.Unmarshal(raw, &item); e != nil || item.ID != job.ID || item.Status != "running" || len(item.Steps) != 1 {
				t.Fatalf("%s details differ: %s %v", channel, raw, e)
			}
		}
	}
	// Cancel a CLI-owned task from Web, then observe the owner consume that request.
	web("POST", "/v1/tasks/"+jobs["cli"].ID+"/cancel", "lab", `{"confirm":true}`)
	wait(jobs["cli"].ID, "cancelled")
	compare()
	for source, ch := range releases {
		close(ch)
		if source != "cli" {
			wait(jobs[source].ID, "succeeded")
		}
	}
	compare()
	// Environment scopes isolate history; all-environments explicitly includes prod.
	for _, raw := range [][]byte{cli("--profile", "prod", "tasks", "list"), web("GET", "/v1/tasks", "prod", ""), rpc("esxi_tasks_list", map[string]any{"profile": "prod"})} {
		var page tasks.Page
		if e := json.Unmarshal(raw, &page); e != nil || len(page.Items) != 1 || page.Items[0].ID != hidden.ID {
			t.Fatalf("profile scope failed: %s %v", raw, e)
		}
	}
	for _, raw := range [][]byte{cli("tasks", "list", "--all-profiles"), web("GET", "/v1/tasks?allProfiles=true", "lab", ""), rpc("esxi_tasks_list", map[string]any{"allProfiles": true})} {
		var page tasks.Page
		if e := json.Unmarshal(raw, &page); e != nil || len(page.Items) != 5 {
			t.Fatalf("all profiles failed: %s %v", raw, e)
		}
	}
	s.taskAll = true
	s.Render()
	if s.table.GetRowCount() != 6 {
		t.Fatal("TUI all environments failed")
	}
	for _, owner := range owners {
		owner.Close()
	}
	// New OS processes still read the completed records after every owner exits.
	for _, raw := range [][]byte{cli("tasks", "list", "--all-profiles"), rpc("esxi_tasks_list", map[string]any{"allProfiles": true})} {
		var page tasks.Page
		if e := json.Unmarshal(raw, &page); e != nil || len(page.Items) != 5 {
			t.Fatalf("history lost after restart: %s %v", raw, e)
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(filepath.Dir(path), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal("shared task/audit database integrity failed", integrity, err)
	}

}
