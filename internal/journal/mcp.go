package journal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// MCP observes only tool name, selected profile and effective permission mode.
// Only a taskId resource identifier is retained; other arguments and result text are never persisted.
func MCP(ctx context.Context, raw []byte, profile, mode string, fn func() ([]byte, error)) ([]byte, error) {
	var request struct {
		Method string `json:"method"`
		Params struct {
			Name      string `json:"name"`
			Arguments struct {
				TaskID string `json:"taskId"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &request) != nil || request.Method != "tools/call" {
		return fn()
	}
	var out []byte
	var actual error
	err := Run(ctx, Event{Source: "mcp", Profile: profile, Mode: mode, Operation: request.Params.Name, Resource: request.Params.Arguments.TaskID, RequestID: ID()}, func() error {
		out, actual = fn()
		if actual != nil {
			return actual
		}
		var response struct {
			Error  json.RawMessage `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		if json.Unmarshal(out, &response) == nil && (response.Result.IsError || (len(response.Error) > 0 && string(response.Error) != "null")) {
			if strings.Contains(string(out), "mcp_readonly") || strings.Contains(string(out), "mcp_forbidden") || strings.Contains(string(out), "mcp_mode_invalid") {
				return errors.New("mcp_readonly")
			}
			return errors.New("mcp_tool_failed")
		}
		return nil
	})
	if out != nil {
		return out, actual
	}
	return out, err
}
func (s *Store) Write(p []byte) (int, error) {
	e := Event{Event: "process.diagnostic", Level: "WARNING", Code: "external_diagnostic"}
	text := string(p)
	switch {
	case strings.Contains(text, "re-login failed"):
		e.Event = "connection.reauthentication_failed"
		e.Code = "authentication_failed"
		e.Level = "ERROR"
	case strings.Contains(text, "SOAP session re-authenticated"):
		e.Event = "connection.reauthenticated"
		e.Code = ""
		e.Level = "INFO"
	case strings.Contains(text, "ESXi WebMKS dial failed"):
		e.Event = "console.connection_failed"
		e.Code = "console_unavailable"
		e.Level = "ERROR"
	case strings.Contains(text, "Open http://") || strings.Contains(text, "Listening on"):
		e.Event = "server.listening"
		e.Code = ""
		e.Level = "INFO"
	case strings.Contains(text, "proxy end:"):
		e.Event = "console.closed"
		e.Code = ""
		e.Level = "INFO"
	}
	s.Log(e)
	return len(p), nil
}
