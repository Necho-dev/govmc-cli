package mcp

import (
	"context"
	"fmt"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
)

const consoleBatch = "esxi_vm_console_input"

type consoleActionShape struct {
	kind     string
	required []string
	optional []string
}

var consolePointerShapes = []consoleActionShape{
	{kind: "move", required: []string{"x", "y"}},
	{kind: "click", required: []string{"x", "y"}, optional: []string{"button"}},
	{kind: "doubleClick", required: []string{"x", "y"}, optional: []string{"button"}},
	{kind: "drag", required: []string{"x", "y", "toX", "toY"}, optional: []string{"button"}},
	{kind: "scroll", required: []string{"x", "y"}, optional: []string{"dx", "dy"}},
	{kind: "relativeMove", optional: []string{"dx", "dy"}},
	{kind: "relativeClick", optional: []string{"button"}},
	{kind: "relativeDrag", optional: []string{"dx", "dy", "button"}},
	{kind: "relativeScroll", optional: []string{"dx", "dy"}},
}

type consoleTool struct {
	name, description string
	shapes            []consoleActionShape
}

var consoleSingleTools = []consoleTool{
	{"esxi_vm_console_press_key", "Press one key or key chord in the VM's focused window.", []consoleActionShape{
		{kind: "key", required: []string{"key"}, optional: []string{"modifiers"}},
	}},
	{"esxi_vm_console_type_text", "Type 1–256 printable ASCII characters using a US keyboard layout. No Enter is sent.", []consoleActionShape{
		{kind: "text", required: []string{"text"}},
	}},
	{"esxi_vm_console_pointer", "Send one pointer action. Absolute coordinates are screenshot pixels from the top-left; relative actions operate from the current pointer position.", consolePointerShapes},
}

func isConsoleInputTool(name string) bool {
	if name == consoleBatch {
		return true
	}
	for _, tool := range consoleSingleTools {
		if name == tool.name {
			return true
		}
	}
	return false
}

func consoleInputTools(id map[string]any) []map[string]any {
	const common = " Requires confirm=true. Optionally returns a fresh screenshot."
	batchSchema := consoleInputSchema(id)
	actionProps := consoleActionProperties()
	boolean := map[string]any{"type": "boolean"}
	tools := []map[string]any{}
	for _, tool := range consoleSingleTools {
		props := map[string]any{"id": id, "confirm": boolean, "screenshot": boolean}
		required := []string{"id", "confirm"}
		if len(tool.shapes) == 1 {
			required = append(required, tool.shapes[0].required...)
		} else {
			kinds := make([]string, 0, len(tool.shapes))
			for _, shape := range tool.shapes {
				kinds = append(kinds, shape.kind)
			}
			props["type"] = map[string]any{"type": "string", "enum": kinds}
			required = append(required, "type")
		}
		for _, shape := range tool.shapes {
			for _, field := range shape.required {
				props[field] = actionProps[field]
			}
			for _, field := range shape.optional {
				props[field] = actionProps[field]
			}
		}
		tools = append(tools, map[string]any{"name": tool.name, "description": tool.description + common, "inputSchema": map[string]any{
			"type": "object", "required": required, "additionalProperties": false, "properties": props,
		}})
	}
	return append(tools, map[string]any{"name": consoleBatch, "description": "Send a batch of keyboard, pointer and wait actions over one ESXi WebMKS connection. Prefer the dedicated tools for single actions." + common, "inputSchema": batchSchema})
}

func parseConsoleTool(name string, args map[string]any) (consoleInputRequest, error) {
	if name == consoleBatch {
		return parseConsoleInput(args)
	}
	for _, tool := range consoleSingleTools {
		if tool.name != name {
			continue
		}
		if len(tool.shapes) == 1 {
			return parseSingleConsoleAction(args, tool.shapes[0], false)
		}
		for _, shape := range tool.shapes {
			if kind, _ := args["type"].(string); kind == shape.kind {
				return parseSingleConsoleAction(args, shape, true)
			}
		}
		return consoleInputRequest{}, fmt.Errorf("unsupported pointer action type")
	}
	return consoleInputRequest{}, fmt.Errorf("unknown console input tool %q", name)
}

func parseSingleConsoleAction(args map[string]any, shape consoleActionShape, hasType bool) (consoleInputRequest, error) {
	action := map[string]any{"type": shape.kind}
	request := map[string]any{"actions": []map[string]any{action}}
	for _, field := range shape.required {
		value, ok := args[field]
		if !ok || value == nil {
			return consoleInputRequest{}, fmt.Errorf("%s requires %s", shape.kind, field)
		}
		action[field] = value
	}
	for _, field := range shape.optional {
		if value, ok := args[field]; ok {
			action[field] = value
		}
	}
	for field, value := range args {
		switch field {
		case "id", "confirm", "screenshot":
			request[field] = value
		case "type":
			if hasType {
				continue
			}
			fallthrough
		default:
			if _, ok := action[field]; !ok || field == "type" {
				return consoleInputRequest{}, fmt.Errorf("unknown field %q for %s", field, shape.kind)
			}
		}
	}
	return parseConsoleInput(request)
}
func callConsoleInput(ctx context.Context, client *esxi.Client, name string, args map[string]any) ([]map[string]any, bool) {
	req, err := parseConsoleTool(name, args)
	if err != nil {
		return text(err.Error()), true
	}
	result, err := client.ConsoleInput(ctx, req.ID, req.Actions)
	if err != nil {
		return text(asJSON(map[string]any{"result": result, "error": err.Error()})), true
	}
	content := text(asJSON(result))
	if req.Screenshot {
		frame, err := client.Screenshot(ctx, result.VMID)
		if err != nil {
			// Input has already been sent. Keep success to discourage retries.
			return append(content, text("input sent; follow-up screenshot failed: "+err.Error())...), false
		}
		content = append(content, imageContent(frame))
	}
	return content, false
}
