package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/Necho-dev/govmc-cli/internal/workflow"
	"time"
)

type profileKey struct{}

func WithProfile(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, profileKey{}, name)
}
func workflowTools() []map[string]any {
	schema := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	strType := map[string]string{"type": "string"}
	boolean := map[string]any{"type": "boolean"}
	integer := map[string]string{"type": "integer"}
	return []map[string]any{
		{"name": "esxi_vm_clone", "description": "Preview a powered-off standalone ESXi clone. Default dryRun=true; execution requires dryRun=false, confirm=true and readwrite. Returns a persistent task ID; cancellation is not rollback.", "inputSchema": schema(map[string]any{"src": strType, "dst": strType, "datastore": strType, "dstDatastore": strType, "portgroup": strType, "numCPU": integer, "memoryMB": integer, "cpuHotAddEnabled": boolean, "memoryHotAddEnabled": boolean, "lockMemory": boolean, "memoryReservationMB": integer, "powerOn": boolean, "dryRun": map[string]any{"type": "boolean", "default": true}, "confirm": boolean, "expandDisks": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"key": integer, "capacityMiB": integer}, "required": []string{"key", "capacityMiB"}}}, "sourceVersion": strType, "requestKey": strType}, "src", "dst")},
		{"name": "esxi_tasks_list", "description": "List persistent tasks without connecting to ESXi. Filter by profile, status, kind, source, search, since, until; paginated.", "inputSchema": schema(map[string]any{"status": strType, "kind": strType, "source": strType, "search": strType, "since": strType, "until": strType, "limit": integer, "offset": integer, "allProfiles": boolean})},
		{"name": "esxi_task_status", "description": "Read task state, execution endpoint, steps, result and failure details from data.db.", "inputSchema": schema(map[string]any{"taskId": strType}, "taskId")},
		{"name": "esxi_task_cancel", "description": "Request cancellation of a task in the selected profile; requires confirm=true and readwrite. Does not roll back completed steps.", "inputSchema": schema(map[string]any{"taskId": strType, "confirm": boolean}, "taskId", "confirm")},
	}
}
func dispatchWorkflow(ctx context.Context, c *esxi.Client, profile, name string, args map[string]any, release func()) ([]map[string]any, bool) {
	var out any
	var err error
	if name == "esxi_vm_clone" {
		raw, _ := json.Marshal(args)
		var spec esxi.CloneSpec
		err = json.Unmarshal(raw, &spec)
		if err == nil {
			out, err = workflow.Clone(ctx, tasks.From(ctx), c, profile, c.Endpoint(), spec, str(args, "requestKey"), release)
		} else {
			release()
		}
	} else {
		dry := true
		if _, ok := args["dryRun"]; ok {
			dry = boolArg(args, "dryRun")
		}
		out, err = workflow.CopyMove(ctx, tasks.From(ctx), c, profile, c.Endpoint(), str(args, "datastore"), str(args, "src"), str(args, "dst"), name == "esxi_ds_copy", dry, boolArg(args, "confirm"), str(args, "requestKey"), release, str(args, "dstDatastore"))
	}
	if err != nil {
		return text(err.Error()), true
	}
	return text(asJSON(out)), false
}
func taskTool(ctx context.Context, name string, args map[string]any, profile string) (any, error) {
	m := tasks.From(ctx)
	if m == nil {
		return nil, fmt.Errorf("task service unavailable")
	}
	if profile == "" {
		profile, _ = ctx.Value(profileKey{}).(string)
	}
	if name == "esxi_tasks_list" {
		q := tasks.Query{Profile: profile, Status: str(args, "status"), Kind: str(args, "kind"), Source: str(args, "source"), Search: str(args, "search"), Limit: int(int32Arg(args, "limit")), Offset: int(int32Arg(args, "offset"))}
		if boolArg(args, "allProfiles") {
			q.Profile = ""
		}
		var e error
		if v := str(args, "since"); v != "" {
			q.Since, e = time.Parse(time.RFC3339, v)
			if e != nil {
				return nil, e
			}
		}
		if v := str(args, "until"); v != "" {
			q.Until, e = time.Parse(time.RFC3339, v)
			if e != nil {
				return nil, e
			}
		}
		return m.List(q)
	}
	t, e := m.Get(str(args, "taskId"))
	if e != nil {
		return nil, e
	}
	if t.Profile != profile {
		return nil, fmt.Errorf("task belongs to another profile")
	}
	if name == "esxi_task_cancel" {
		if e = m.Cancel(t.ID); e != nil {
			return nil, e
		}
		return map[string]string{"taskId": t.ID, "status": "cancel_requested"}, nil
	}
	return t, nil
}
