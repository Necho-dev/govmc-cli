package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

const Readonly = "readonly"
const Readwrite = "readwrite"

func allowedTools(profiles bool) []map[string]any {
	var catalog []map[string]any
	for _, tool := range tools() {
		name := tool["name"].(string)
		if name == "esxi_vm_screenshot" {
			tool["description"] = "Capture a PNG. ESXi creates a temporary datastore file and govmc attempts to remove it."
		}
		if profiles {
			tool["inputSchema"].(map[string]any)["properties"].(map[string]any)["profile"] = map[string]string{"type": "string", "description": "Optional saved environment for this call only; does not change the default"}
		}
		tool["annotations"] = map[string]any{"readOnlyHint": readonlyTool(name), "destructiveHint": !readonlyTool(name)}
		catalog = append(catalog, tool)
	}
	if profiles {
		for _, name := range []string{"esxi_profiles_list", "esxi_profiles_current", "esxi_config_show"} {
			catalog = append(catalog, map[string]any{"name": name, "description": "Read local saved environment metadata and effective MCP permissions. No passwords. Permissions can only be changed via CLI or TUI.", "annotations": map[string]any{"readOnlyHint": true}, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}})
		}
	}
	props := map[string]any{}
	if profiles {
		props["profile"] = map[string]string{"type": "string"}
	}
	return append(catalog, map[string]any{"name": "esxi_doctor", "annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false}, "description": "Check authentication, selected endpoint and effective MCP mode. Permissions can only be changed via CLI or TUI.", "inputSchema": map[string]any{"type": "object", "properties": props}})
}

func toolAllowed(name string) bool {
	for _, tool := range allowedTools(true) {
		if tool["name"] == name {
			return true
		}
	}
	return false
}

func needsConfirmation(name string, args map[string]any) bool {
	if readonlyTool(name) {
		return false
	}
	switch name {
	case "esxi_ds_copy", "esxi_ds_move", "esxi_vm_clone":
		if _, set := args["dryRun"]; !set || boolArg(args, "dryRun") {
			return false
		}
	}
	return true
}

func validMode(mode string) error {
	if mode != Readonly && mode != Readwrite {
		return fmt.Errorf("MCP mode must be readonly or readwrite")
	}
	return nil
}

// HandleConfigured enforces mode before opening a profile connection. Mode is a
// server-side CLI setting; no tool argument or tool name can change it.
func HandleConfigured(ctx context.Context, raw []byte, connect Connect, mode string, resolve ...func(string) (string, error)) ([]byte, error) {
	if mode == "" {
		mode = Readonly
	}
	if err := validMode(mode); err != nil {
		return nil, err
	}
	var req rpcReq
	if json.Unmarshal(raw, &req) != nil || (req.Method != "tools/list" && req.Method != "tools/call") {
		return Handle(ctx, nil, raw, false)
	}
	response := rpcResp{JSONRPC: "2.0", ID: req.ID}
	if req.Method == "tools/list" {
		response.Result = map[string]any{"tools": allowedTools(true)}
		return marshal(response)
	}
	fail := func(message string) ([]byte, error) {
		response.Result = map[string]any{"content": text(message), "isError": true}
		return marshal(response)
	}
	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		response.Error = &rpcErr{Code: -32602, Message: err.Error()}
		return marshal(response)
	}
	if _, set := call.Arguments["mode"]; set {
		return fail("MCP permissions can only be changed via CLI or TUI")
	}
	if !toolAllowed(call.Name) {
		return fail("tool unavailable in this transport")
	}

	profile := ""
	if value, set := call.Arguments["profile"]; set {
		var ok bool
		profile, ok = value.(string)
		if !ok || profile == "" {
			return fail("profile must be a nonempty string")
		}
	}
	if len(resolve) > 0 && call.Name != "esxi_tasks_list" && call.Name != "esxi_task_status" {
		var err error
		mode, err = resolve[0](profile)
		if err != nil {
			return fail(asJSON(map[string]string{"error": "mcp_mode_invalid", "message": err.Error()}))
		}
		if err := validMode(mode); err != nil {
			return fail(err.Error())
		}
	}
	if !readonlyTool(call.Name) && needsConfirmation(call.Name, call.Arguments) && mode != Readwrite {
		return fail(asJSON(map[string]string{"error": "mcp_readonly", "mode": mode, "op": call.Name, "hint": "Use govmc config set --mcp-mode readwrite; target profile must also allow readwrite"}))
	}
	if needsConfirmation(call.Name, call.Arguments) && !boolArg(call.Arguments, "confirm") {
		return fail("write operation requires confirm=true")
	}
	if call.Name == "esxi_tasks_list" || call.Name == "esxi_task_status" || call.Name == "esxi_task_cancel" {
		out, e := taskTool(ctx, call.Name, call.Arguments, profile)
		if e != nil {
			return fail(e.Error())
		}
		response.Result = map[string]any{"content": text(asJSON(out)), "isError": false}
		return marshal(response)
	}
	if call.Name == "esxi_logs_list" || call.Name == "esxi_audits_list" {
		out, e := journalTool(ctx, call.Name, call.Arguments, profile)
		if e != nil {
			return fail(e.Error())
		}
		response.Result = map[string]any{"content": text(asJSON(out)), "isError": false}
		return marshal(response)
	}
	client, actual, close, err := connect(ctx, profile)
	if err != nil {
		return fail(err.Error())
	}
	transferred := false
	defer func() {
		if !transferred {
			close()
		}
	}()
	var content []map[string]any
	var failed bool
	if call.Name == "esxi_vm_clone" || call.Name == "esxi_ds_copy" || call.Name == "esxi_ds_move" {
		transferred = true
		content, failed = dispatchWorkflow(ctx, client, actual, call.Name, call.Arguments, close)
	} else if call.Name == "esxi_doctor" {
		content = text(asJSON(client.About(ctx)))
	} else {
		dispatch := make(map[string]any, len(call.Arguments))
		for key, value := range call.Arguments {
			if key != "profile" {
				dispatch[key] = value
			}
		}
		content, failed = callTool(ctx, client, call.Name, dispatch)
	}
	content = append(content, text(asJSON(map[string]string{"profile": actual, "mode": mode}))...)
	response.Result = map[string]any{"content": content, "isError": failed}
	return marshal(response)
}
