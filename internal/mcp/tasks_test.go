package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskToolsAreLocalProfileScopedAndPermissionChecked(t *testing.T) {
	m, e := tasks.Open(filepath.Join(t.TempDir(), "config.yaml"), "mcp")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	ctx := WithProfile(tasks.With(context.Background(), m), "lab")
	job, e := m.Submit(ctx, "lab", "https://host", "file.copy", "target", nil, "target", "", func(ctx context.Context, r *tasks.Reporter) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	if e != nil {
		t.Fatal(e)
	}
	connect := func(context.Context, string) (*esxi.Client, string, func(), error) {
		t.Fatal("task queries must not dial ESXi")
		return nil, "", nil, nil
	}
	call := func(name, mode string, args map[string]any) string {
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 42, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		out, e := HandleConfigured(ctx, raw, connect, mode)
		if e != nil {
			t.Fatal(e)
		}
		return string(out)
	}
	for _, name := range []string{"esxi_tasks_list", "esxi_task_status"} {
		out := call(name, Readonly, map[string]any{"taskId": job.ID})
		if !strings.Contains(out, job.ID) || !strings.Contains(out, `"id":42`) {
			t.Fatal(out)
		}
	}
	if out := call("esxi_task_status", Readonly, map[string]any{"taskId": job.ID, "profile": "prod"}); !strings.Contains(out, "another profile") {
		t.Fatal(out)
	}
	if out := call("esxi_task_cancel", Readonly, map[string]any{"taskId": job.ID, "confirm": true}); !strings.Contains(out, "mcp_readonly") {
		t.Fatal(out)
	}
	if out := call("esxi_task_cancel", Readwrite, map[string]any{"taskId": job.ID}); !strings.Contains(out, "confirm=true") {
		t.Fatal(out)
	}
	if out := call("esxi_task_cancel", Readwrite, map[string]any{"taskId": job.ID, "confirm": true}); !strings.Contains(out, "cancel_requested") {
		t.Fatal(out)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v, _ := m.Get(job.ID)
		if v.Status == "cancelled" {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("cancel not consumed")
}
func TestClonePreviewCanReachReadOnlyConnectorButExecutionCannot(t *testing.T) {
	for _, test := range []struct {
		args  string
		dials bool
	}{{`"dryRun":true`, true}, {``, true}, {`"dryRun":false,"confirm":true`, false}} {
		called := false
		connect := func(context.Context, string) (*esxi.Client, string, func(), error) {
			called = true
			return nil, "", nil, fmt.Errorf("fixture connection")
		}
		comma := ""
		if test.args != "" {
			comma = "," + test.args
		}
		raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_vm_clone","arguments":{"src":"base","dst":"new","datastore":"ds"` + comma + `}}}`)
		out, e := HandleReadonly(context.Background(), raw, connect)
		if e != nil || called != test.dials {
			t.Fatal(string(out), e, called)
		}
	}
}
