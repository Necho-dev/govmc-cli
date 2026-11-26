package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/mcp"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/spf13/cobra"
)

func (o *options) configModeCommands() []*cobra.Command {
	var mode string
	set := &cobra.Command{Use: "set", Short: "Set application preferences", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("mcp-mode") {
			return fmt.Errorf("specify --mcp-mode readonly or readwrite")
		}
		if err := target.ValidateMCPMode(mode); err != nil {
			return err
		}
		path, err := o.path()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		if err := target.Edit(ctx, path, func(config *target.Config) error { config.MCP.Mode = mode; return nil }); err != nil {
			return err
		}
		return o.output(cmd, map[string]string{"mode": mode}, nil)
	}}
	set.Flags().StringVar(&mode, "mcp-mode", "", "global MCP permission limit: readonly or readwrite")
	show := &cobra.Command{Use: "show", Short: "Show application preferences and effective MCP permissions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := o.path()
		if err != nil {
			return err
		}
		config, err := target.Load(path)
		if err != nil {
			return err
		}
		data, err := mcpConfigView(config, o.profile)
		if err != nil {
			return err
		}
		return o.output(cmd, data, nil)
	}}
	return []*cobra.Command{set, show}
}

func selectedMCPProfile(config target.Config, requested string) string {
	if requested != "" {
		return requested
	}
	if env := os.Getenv("GOVMC_PROFILE"); env != "" {
		return env
	}
	return config.Active
}

func mcpConfigView(config target.Config, requested string) (map[string]any, error) {
	name := selectedMCPProfile(config, requested)
	mode, err := target.EffectiveMCPMode(config, name)
	if err != nil {
		return nil, err
	}
	return map[string]any{"language": config.Language, "active": config.Active, "profile": name, "mcp": map[string]string{"mode": config.MCP.Mode, "effective": mode}}, nil
}

func mcpProfileViews(config target.Config) []map[string]any {
	names := make([]string, 0, len(config.Profiles))
	for name := range config.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]map[string]any, 0, len(names))
	for _, name := range names {
		p := config.Profiles[name]
		effective, _ := target.EffectiveMCPMode(config, name)
		mode := p.MCP.Mode
		if mode == "" {
			mode = "inherit"
		}
		rows = append(rows, map[string]any{"name": name, "url": p.URL, "username": p.Username, "insecure": p.Insecure, "active": name == config.Active, "mcp_mode": mode, "mcp_effective": effective})
	}
	return rows
}

// Local inspection is available without opening a network connection or decrypting passwords.
func mcpLocalRequest(raw []byte, config target.Config, requested string) ([]byte, bool, error) {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &req) != nil || req.Method != "tools/call" {
		return nil, false, nil
	}
	name := req.Params.Name
	if name != "esxi_profiles_list" && name != "esxi_profiles_current" && name != "esxi_config_show" {
		return nil, false, nil
	}
	var data any
	var err error
	if len(req.Params.Arguments) > 0 {
		err = fmt.Errorf("local inspection tools do not accept arguments; permissions can only be changed via CLI or TUI")
	} else {
		switch name {
		case "esxi_profiles_list":
			data = map[string]any{"active": config.Active, "profiles": mcpProfileViews(config)}
		case "esxi_profiles_current":
			// Current means saved active, not the process's temporary profile.
			for _, row := range mcpProfileViews(config) {
				if row["active"] == true {
					data = row
					break
				}
			}
		case "esxi_config_show":
			data, err = mcpConfigView(config, requested)
		}
	}
	payload, _ := json.Marshal(data)
	if err != nil {
		payload = []byte(err.Error())
	}
	result := map[string]any{"content": []map[string]string{{"type": "text", "text": string(payload)}}, "isError": err != nil}
	out, marshalErr := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	return out, true, marshalErr
}

func (o *options) mcpPrivilege() *cobra.Command {
	return &cobra.Command{Use: "privilege", Short: "Show MCP permission matrix and effective profile mode", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := o.path()
		if err != nil {
			return err
		}
		config, err := target.Load(path)
		if err != nil {
			return err
		}
		view, err := mcpConfigView(config, o.profile)
		if err != nil {
			return err
		}
		var catalog struct {
			Result struct {
				Tools []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Annotations struct {
						ReadOnly bool `json:"readOnlyHint"`
					} `json:"annotations"`
				} `json:"tools"`
			} `json:"result"`
		}
		raw, err := mcp.HandleConfigured(cmd.Context(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), nil, mcp.Readonly)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			return err
		}
		rows := make([]map[string]any, 0, len(catalog.Result.Tools))
		for _, tool := range catalog.Result.Tools {
			read := tool.Annotations.ReadOnly || tool.Name == "esxi_doctor"
			level := "write"
			if read {
				level = "read"
			}
			rows = append(rows, map[string]any{"tool": tool.Name, "level": level, "readonly": read, "readwrite": true})
		}
		view["modes"] = []string{target.MCPReadonly, target.MCPReadwrite}
		view["tools"] = rows
		return o.output(cmd, view, func(w io.Writer) {
			mode := view["mcp"].(map[string]string)
			printLine(w, "Global: %s  Profile: %s  Effective: %s", mode["mode"], view["profile"], mode["effective"])
			printLine(w, "%-30s %-8s %-10s %-10s", "Tool", "Level", "readonly", "readwrite")
			for _, row := range rows {
				printLine(w, "%-30s %-8s %-10v %-10v", row["tool"], row["level"], row["readonly"], row["readwrite"])
			}
		})
	}}
}

// A malformed live configuration fails closed without killing the MCP process.
func mcpConfigFailure(raw []byte, cause error) ([]byte, error) {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return nil, cause
	}
	if len(request.ID) == 0 || string(request.ID) == "null" {
		return nil, nil
	}
	response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
	if request.Method == "tools/call" {
		payload, _ := json.Marshal(map[string]string{"error": "mcp_mode_invalid", "message": cause.Error()})
		response["result"] = map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": string(payload)}}}
	} else {
		response["error"] = map[string]any{"code": -32000, "message": cause.Error()}
	}
	return json.Marshal(response)
}
