package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	pngformat "image/png"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
)

const MaxRequestBytes = 1 << 20

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Handle(ctx context.Context, client *esxi.Client, raw []byte, allowConsoleInput bool) ([]byte, error) {
	var req rpcReq
	if err := json.Unmarshal(raw, &req); err != nil {
		return marshal(rpcResp{JSONRPC: "2.0", Error: &rpcErr{Code: -32700, Message: err.Error()}})
	}
	if strings.HasPrefix(req.Method, "notifications/") || req.Method == "initialized" {
		if len(req.ID) == 0 || string(req.ID) == "null" {
			return nil, nil
		}
	}
	resp := rpcResp{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]string{"name": "govmc", "version": govmc.Version},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "notifications/initialized", "initialized":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": tools()}
	case "resources/list":
		resp.Result = map[string]any{"resources": []any{}}
	case "prompts/list":
		resp.Result = map[string]any{"prompts": []any{}}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcErr{Code: -32602, Message: err.Error()}
			break
		}
		if p.Name == guestExecTool && !allowConsoleInput {
			resp.Result = map[string]any{"content": text("remote guest execution requires an API token"), "isError": true}
			break
		}
		if isConsoleInputTool(p.Name) && !allowConsoleInput {
			resp.Result = map[string]any{"content": text("remote console input requires an API token"), "isError": true}
			break
		}
		content, isErr := callTool(ctx, client, p.Name, p.Arguments)
		resp.Result = map[string]any{"content": content, "isError": isErr}
	default:
		resp.Error = &rpcErr{Code: -32601, Message: "method not found: " + req.Method}
	}
	return marshal(resp)
}

func marshal(v rpcResp) ([]byte, error) {
	return json.Marshal(v)
}

func tools() []map[string]any {
	note := "Standalone ESXi: directory cloning; no vCenter CloneVM dependency. Metrics=QueryPerf (not QuickStats). Guest hostname empty without Tools. Sum of vNIC != host vmnic. Instance default empty string. Destructive tools need confirm=true."
	schema := func(props map[string]any, required []string) map[string]any {
		s := map[string]any{"type": "object", "properties": props, "additionalProperties": true}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	id := map[string]any{"type": "string", "description": "VM display name or instanceUuid (or bios uuid)"}
	catalog := []map[string]any{
		{"name": "esxi_vm_list", "description": "List VMs with power and latest cpu/mem/net. " + note, "inputSchema": schema(map[string]any{}, nil)},
		{"name": "esxi_vm_get", "description": "One VM identity, networks (vNICs), pending question. " + note, "inputSchema": schema(map[string]any{"id": id}, []string{"id"})},
		{"name": "esxi_metrics_query", "description": "QueryPerf time series (GET /v1/vms/{id}/metrics or /v1/host/metrics). Counter catalog is GET /v1/metrics/counters. Default VM aggregate instance; net in Mbps. " + note, "inputSchema": schema(map[string]any{
			"id":        id,
			"host":      map[string]any{"type": "boolean"},
			"names":     map[string]any{"type": "string"},
			"instance":  map[string]any{"type": "string"},
			"maxSample": map[string]any{"type": "integer"},
			"start":     map[string]any{"type": "string"},
			"end":       map[string]any{"type": "string"},
		}, nil)},
		{"name": guestExecTool, "description": "Run a Windows program in the background via VMware Guest Operations. Requires VMware Tools, server-side credentials for the target VM, and confirm=true. Returns exit code and captured output.", "inputSchema": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"id", "program", "confirm"},
			"properties": map[string]any{
				"id": id, "program": map[string]any{"type": "string", "minLength": 1},
				"args":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 128},
				"workingDir": map[string]any{"type": "string"},
				"timeoutMs":  map[string]any{"type": "integer", "minimum": 0, "maximum": 90000, "default": 30000},
				"confirm":    map[string]any{"type": "boolean"},
			},
		}},
		{"name": "esxi_vm_screenshot", "description": "PNG screenshot; 409 if powered off.", "inputSchema": schema(map[string]any{"id": id}, []string{"id"})},
		{"name": "esxi_host_get", "description": "Host inventory (cpuMhz, memory, portgroups). Not time series; use esxi_metrics_query host=true.", "inputSchema": schema(map[string]any{}, nil)},
		{"name": "esxi_ds_ls", "description": "List datastore entries. kind=folder|file|all.", "inputSchema": schema(map[string]any{
			"datastore": map[string]any{"type": "string"},
			"path":      map[string]any{"type": "string"},
			"kind":      map[string]any{"type": "string"},
		}, []string{"datastore"})},
		{"name": "esxi_ds_folder_create", "description": "Create datastore folder. confirm=true.", "inputSchema": schema(map[string]any{
			"datastore": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"}, "confirm": map[string]any{"type": "boolean"},
		}, []string{"datastore", "path", "confirm"})},
		{"name": "esxi_ds_folder_delete", "description": "Delete datastore folder. confirm=true.", "inputSchema": schema(map[string]any{
			"datastore": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"}, "confirm": map[string]any{"type": "boolean"},
		}, []string{"datastore", "path", "confirm"})},
		{"name": "esxi_ds_copy", "description": "Copy file or folder. Default dryRun=true.", "inputSchema": schema(map[string]any{
			"dstDatastore": map[string]any{"type": "string", "description": "Destination datastore; defaults to source"}, "datastore": map[string]any{"type": "string"}, "src": map[string]any{"type": "string"}, "dst": map[string]any{"type": "string"},
			"requestKey": map[string]any{"type": "string"}, "dryRun": map[string]any{"type": "boolean"}, "confirm": map[string]any{"type": "boolean"},
		}, []string{"datastore", "src", "dst"})},
		{"name": "esxi_ds_move", "description": "Move/rename file or folder. Default dryRun=true.", "inputSchema": schema(map[string]any{
			"dstDatastore": map[string]any{"type": "string", "description": "Destination datastore; defaults to source"}, "datastore": map[string]any{"type": "string"}, "src": map[string]any{"type": "string"}, "dst": map[string]any{"type": "string"},
			"requestKey": map[string]any{"type": "string"}, "dryRun": map[string]any{"type": "boolean"}, "confirm": map[string]any{"type": "boolean"},
		}, []string{"datastore", "src", "dst"})},
		{"name": "esxi_vm_power", "description": "Power on/off/reset/suspend. Requires confirm=true. Hard off without Tools.", "inputSchema": schema(map[string]any{
			"id":      id,
			"action":  map[string]any{"type": "string", "enum": []string{"on", "off", "reset", "suspend"}},
			"confirm": map[string]any{"type": "boolean"},
		}, []string{"id", "action", "confirm"})},
		{"name": "esxi_vm_answer_question", "description": "Answer pending VM question (e.g. I copied it). Requires confirm=true.", "inputSchema": schema(map[string]any{
			"id":      id,
			"choice":  map[string]any{"type": "string"},
			"qid":     map[string]any{"type": "string"},
			"confirm": map[string]any{"type": "boolean"},
		}, []string{"id", "choice", "confirm"})},
	}
	catalog = append(catalog, workflowTools()...)
	catalog = append(catalog, journalTools()...)
	return append(catalog, consoleInputTools(id)...)
}

func consoleActionProperties() map[string]any {
	integer := map[string]any{"type": "integer"}
	stringValue := map[string]any{"type": "string"}
	return map[string]any{
		"x": integer, "y": integer, "toX": integer, "toY": integer, "dx": integer, "dy": integer,
		"button": map[string]any{"type": "string", "enum": []string{"left", "middle", "right"}},
		"key":    stringValue, "text": stringValue,
		"modifiers": map[string]any{"type": "array", "maxItems": 4, "items": map[string]any{"type": "string", "enum": []string{"ctrl", "shift", "alt", "meta"}}},
	}
}

func consoleInputSchema(id map[string]any) map[string]any {
	boolean := map[string]any{"type": "boolean"}
	props := consoleActionProperties()
	props["ms"] = map[string]any{"type": "integer"}
	kinds := []string{"key", "text", "wait"}
	for _, shape := range consolePointerShapes {
		kinds = append(kinds, shape.kind)
	}
	props["type"] = map[string]any{"type": "string", "enum": kinds}
	action := map[string]any{
		"type": "object", "required": []string{"type"}, "additionalProperties": false,
		"properties": props,
	}
	return map[string]any{
		"type": "object", "required": []string{"id", "confirm", "actions"}, "additionalProperties": false,
		"properties": map[string]any{
			"id": id, "confirm": boolean, "screenshot": boolean,
			"actions": map[string]any{"type": "array", "items": action, "minItems": 1, "maxItems": 32},
		},
	}
}

func imageContent(png []byte) map[string]any {
	return map[string]any{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(png)}
}

type consoleInputRequest struct {
	ID         string               `json:"id"`
	Confirm    bool                 `json:"confirm"`
	Screenshot bool                 `json:"screenshot"`
	Actions    []esxi.ConsoleAction `json:"actions"`
}

func parseConsoleInput(args map[string]any) (consoleInputRequest, error) {
	var req consoleInputRequest
	b, err := json.Marshal(args)
	if err != nil {
		return req, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		return req, err
	}
	if !req.Confirm {
		return req, fmt.Errorf("console input requires confirm=true")
	}
	return req, nil
}

func text(s string) []map[string]any {
	return []map[string]any{{"type": "text", "text": s}}
}

func asJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	v, _ := m[k].(string)
	return v
}

func boolArg(m map[string]any, k string) bool {
	switch v := m[k].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	default:
		return false
	}
}

func int32Arg(m map[string]any, k string) int32 {
	switch v := m[k].(type) {
	case float64:
		return int32(v)
	case json.Number:
		n, _ := v.Int64()
		return int32(n)
	case string:
		n, _ := strconv.Atoi(v)
		return int32(n)
	default:
		return 0
	}
}

func callTool(ctx context.Context, client *esxi.Client, name string, args map[string]any) ([]map[string]any, bool) {
	if args == nil {
		args = map[string]any{}
	}
	timeout := 120 * time.Second
	if name == "esxi_ds_copy" {
		dry := true
		if _, ok := args["dryRun"]; ok {
			dry = boolArg(args, "dryRun")
		}
		if !dry {
			timeout = 30 * time.Minute
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if isConsoleInputTool(name) {
		return callConsoleInput(ctx, client, name, args)
	}
	switch name {
	case "esxi_vm_list":
		list, err := client.ListVMs(ctx, true)
		if err != nil {
			return text(err.Error()), true
		}
		return text(asJSON(list)), false
	case "esxi_vm_get":
		vm, err := client.GetVM(ctx, str(args, "id"))
		if err != nil {
			return text(err.Error()), true
		}
		return text(asJSON(vm.Info)), false
	case "esxi_metrics_query":
		q := esxi.MetricsQuery{Instance: str(args, "instance"), MaxSample: int32Arg(args, "maxSample")}
		if n := str(args, "names"); n != "" {
			q.Names = strings.Split(n, ",")
		}
		if v := str(args, "start"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err == nil {
				q.Start = &t
			}
		}
		if v := str(args, "end"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err == nil {
				q.End = &t
			}
		}
		var (
			out *esxi.MetricsResponse
			err error
		)
		if boolArg(args, "host") {
			out, err = client.HostMetrics(ctx, q)
		} else {
			out, err = client.VMMetrics(ctx, str(args, "id"), q)
		}
		if err != nil {
			return text(err.Error()), true
		}
		return text(asJSON(out)), false
	case guestExecTool:
		return callGuestExec(ctx, client, args)
	case "esxi_vm_screenshot":
		png, err := client.Screenshot(ctx, str(args, "id"))
		if err != nil {
			return text(err.Error()), true
		}
		content := []map[string]any{imageContent(png)}
		if size, err := pngformat.DecodeConfig(bytes.NewReader(png)); err == nil {
			content = append(content, text(asJSON(map[string]int{"width": size.Width, "height": size.Height}))...)
		}
		return content, false
	case "esxi_host_get":
		h, err := client.Host(ctx)
		if err != nil {
			return text(err.Error()), true
		}
		return text(asJSON(h)), false
	case "esxi_ds_ls":
		kind := str(args, "kind")
		if kind == "" {
			kind = "all"
		}
		files, err := client.FSList(ctx, str(args, "datastore"), str(args, "path"), kind)
		if err != nil {
			return text(err.Error()), true
		}
		return text(asJSON(files)), false
	case "esxi_ds_folder_create":
		out, err := client.FSMkdir(ctx, str(args, "datastore"), str(args, "path"), boolArg(args, "confirm"))
		if err != nil {
			return text(err.Error()), true
		}
		return text(asJSON(out)), false
	case "esxi_ds_folder_delete":
		out, err := client.FSDelete(ctx, str(args, "datastore"), str(args, "path"), boolArg(args, "confirm"))
		if err != nil {
			return text(err.Error()), true
		}
		return text(asJSON(out)), false
	case "esxi_ds_copy":
		dry := true
		if _, ok := args["dryRun"]; ok {
			dry = boolArg(args, "dryRun")
		}
		out, err := client.FSCopy(ctx, str(args, "datastore"), str(args, "src"), str(args, "dst"), boolArg(args, "confirm"), dry)
		if err != nil {
			return text(err.Error()), true
		}
		return text(asJSON(out)), false
	case "esxi_ds_move":
		dry := true
		if _, ok := args["dryRun"]; ok {
			dry = boolArg(args, "dryRun")
		}
		out, err := client.FSMove(ctx, str(args, "datastore"), str(args, "src"), str(args, "dst"), boolArg(args, "confirm"), dry)
		if err != nil {
			return text(err.Error()), true
		}
		return text(asJSON(out)), false

	case "esxi_vm_power":
		if err := client.Power(ctx, str(args, "id"), str(args, "action"), boolArg(args, "confirm")); err != nil {
			return text(err.Error()), true
		}
		return text("ok"), false
	case "esxi_vm_answer_question":
		if err := client.AnswerQuestion(ctx, str(args, "id"), str(args, "qid"), str(args, "choice"), boolArg(args, "confirm")); err != nil {
			return text(err.Error()), true
		}
		return text("ok"), false

	default:
		return text("unknown tool " + name), true
	}
}

func ServeStream(ctx context.Context, input io.Reader, output io.Writer, handle func(context.Context, []byte) ([]byte, error)) error {
	if file, ok := input.(*os.File); ok {
		stop := context.AfterFunc(ctx, func() { _ = file.Close() })
		defer stop()
	}
	in := bufio.NewReaderSize(input, MaxRequestBytes+1)
	for {
		msg, framed, err := readMCP(in)
		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return nil
			}
			return err
		}
		if len(bytesTrim(msg)) == 0 {
			continue
		}
		out, err := handle(ctx, msg)
		if err != nil {
			return err
		}
		if len(out) == 0 {
			continue
		}
		if err := writeMCP(output, out, framed); err != nil {
			return err
		}
	}
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func readMCP(r *bufio.Reader) ([]byte, bool, error) {
	var contentLen int
	sawHeader := false
	for {
		line, err := r.ReadSlice('\n')
		if err != nil {
			return nil, sawHeader, err
		}
		trim := strings.TrimRight(string(line), "\r\n")
		if strings.HasPrefix(strings.ToLower(trim), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(trim[len("content-length:"):]))
			if err != nil || n < 1 || n > MaxRequestBytes {
				return nil, true, fmt.Errorf("invalid MCP Content-Length")
			}
			contentLen = n
			sawHeader = true
			continue
		}
		if sawHeader && trim == "" {
			buf := make([]byte, contentLen)
			_, err := io.ReadFull(r, buf)
			return buf, true, err
		}
		if !sawHeader && strings.TrimSpace(trim) != "" {
			return []byte(strings.TrimSpace(trim)), false, nil
		}
	}
}

func writeMCP(w io.Writer, payload []byte, framed bool) error {
	if !framed {
		_, err := fmt.Fprintln(w, string(payload))
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
