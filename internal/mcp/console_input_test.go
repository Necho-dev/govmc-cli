package mcp

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestConsoleInputRequiresAuthorizationAndConfirmation(t *testing.T) {
	for _, tc := range []struct {
		allowed    bool
		args, want string
	}{
		{false, `{"id":"vm","confirm":true}`, "requires an API token"},
		{true, `{"id":"vm"}`, "requires confirm=true"},
		{true, `{"id":"vm","confirm":true,"actions":[{"type":"click","typo":1}]}`, "unknown field"},
	} {
		raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"esxi_vm_console_input","arguments":` + tc.args + `}}`)
		out, err := Handle(context.Background(), nil, raw, tc.allowed)
		if err != nil || !strings.Contains(string(out), tc.want) || !strings.Contains(string(out), `"isError":true`) {
			t.Fatalf("response = %s, err = %v, want %q", out, err, tc.want)
		}
	}
}

func TestStdioMatchesRequestFraming(t *testing.T) {
	for _, input := range []string{"{}\n", "Content-Length: 2\r\n\r\n{}"} {
		payload, framed, err := readMCP(bufio.NewReader(strings.NewReader(input)))
		var output bytes.Buffer
		if err != nil {
			t.Fatal(err)
		}
		if err := writeMCP(&output, payload, framed); err != nil {
			t.Fatal(err)
		}
		if output.String() != input {
			t.Fatalf("got %q, want %q", &output, input)
		}
	}
}

func TestConsoleToolsUseSameAuthorizationAndConfirmation(t *testing.T) {
	for _, tc := range []struct{ name, args string }{
		{consoleBatch, `"actions":[{"type":"key","key":"enter"}]`},
		{"esxi_vm_console_press_key", `"key":"enter"`},
		{"esxi_vm_console_type_text", `"text":"clear"`},
		{"esxi_vm_console_pointer", `"type":"click","x":0,"y":0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, check := range []struct {
				allowed bool
				want    string
			}{{false, "requires an API token"}, {true, "requires confirm=true"}} {
				raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tc.name + `","arguments":{"id":"vm",` + tc.args + `}}}`)
				out, err := Handle(context.Background(), nil, raw, check.allowed)
				if err != nil || !strings.Contains(string(out), check.want) || !strings.Contains(string(out), `"isError":true`) {
					t.Fatalf("response = %s, err = %v, want %q", out, err, check.want)
				}
			}
		})
	}
}

func TestSingleConsoleToolActionIsolation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"esxi_vm_console_pointer", map[string]any{"type": "key", "key": "enter"}, "unsupported pointer action"},
		{"esxi_vm_console_pointer", map[string]any{"type": "drag", "x": 0, "y": 0}, "drag requires toX"},
		{"esxi_vm_console_pointer", map[string]any{"type": "relativeClick", "x": 0}, "unknown field"},
		{"esxi_vm_console_type_text", map[string]any{"text": "clear", "actions": []any{}}, "unknown field"},
		{"esxi_vm_console_press_key", map[string]any{"key": "enter", "type": "text"}, "unknown field"},
	} {
		_, err := parseConsoleTool(tc.name, tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.name, err, tc.want)
		}
	}

	req, err := parseConsoleTool("esxi_vm_console_pointer", map[string]any{
		"id": "vm", "confirm": true, "screenshot": true, "type": "click", "x": 0, "y": 0,
	})
	if err != nil || !req.Screenshot || len(req.Actions) != 1 || req.Actions[0].Type != "click" ||
		req.Actions[0].X == nil || *req.Actions[0].X != 0 || req.Actions[0].Y == nil || *req.Actions[0].Y != 0 {
		t.Fatalf("got %+v, err = %v; want click at origin with screenshot", req, err)
	}
}
