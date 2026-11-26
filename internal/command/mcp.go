package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/mcp"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/spf13/cobra"
)

func (o *options) mcp() *cobra.Command {
	root := group("mcp", "Run or configure the MCP server")
	root.AddCommand(&cobra.Command{Use: "serve", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := o.path()
		if err != nil {
			return err
		}
		if _, err := target.MCPMode(path, o.profile); err != nil {
			return err
		}
		connect := func(ctx context.Context, profile string) (*esxi.Client, string, func(), error) {
			var selected target.Target
			var err error
			if profile != "" {
				selected, err = target.Named(path, profile)
			} else {
				selected, err = target.Resolve(path, o.profile)
			}
			if err != nil {
				return nil, profile, nil, err
			}
			dial, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			client, err := selected.Dial(dial)
			if err != nil {
				return nil, selected.Name, nil, err
			}
			return client, selected.Name, func() { target.Close(client) }, nil
		}
		handle := func(ctx context.Context, msg []byte) ([]byte, error) {
			config, err := target.Load(path)
			if err != nil {
				return mcpConfigFailure(msg, err)
			}
			selectName := func(profile string) string {
				if profile == "" {
					profile = o.profile
				}
				return selectedMCPProfile(config, profile)
			}
			resolveMode := func(profile string) (string, error) {
				return target.EffectiveMCPMode(config, selectName(profile))
			}
			connectSnapshot := func(ctx context.Context, profile string) (*esxi.Client, string, func(), error) {
				name := selectName(profile)
				if name == "" {
					return nil, "", nil, fmt.Errorf("no default environment selected; configure a saved profile via CLI")
				}
				return connect(ctx, name)
			}
			if result, handled, err := mcpLocalRequest(msg, config, o.profile); handled {
				return result, err
			}
			return mcp.HandleConfigured(mcp.WithProfile(ctx, selectName("")), msg, connectSnapshot, config.MCP.Mode, resolveMode)
		}
		return mcp.ServeStream(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), func(ctx context.Context, msg []byte) ([]byte, error) {
			var request struct {
				Params struct {
					Arguments struct {
						Profile string `json:"profile"`
					} `json:"arguments"`
				} `json:"params"`
			}
			_ = json.Unmarshal(msg, &request)
			config, _ := target.Load(path)
			name := selectedMCPProfile(config, o.profile)
			if request.Params.Arguments.Profile != "" {
				name = request.Params.Arguments.Profile
			}
			mode, _ := target.EffectiveMCPMode(config, name)
			return journal.MCP(ctx, msg, name, mode, func() ([]byte, error) { return handle(ctx, msg) })
		})
	}})
	var clientName string
	config := &cobra.Command{Use: "json", Aliases: []string{"config"}, Short: "Print MCP client configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if clientName != "generic" {
			if _, err := clientFolder(clientName); err != nil {
				return err
			}
		}

		bin, err := os.Executable()
		if err != nil {
			return err
		}
		args := []string{"mcp", "serve"}
		if o.profile != "" {
			args = append([]string{"--profile", o.profile}, args...)
		}
		profilePath, err := o.path()
		if err != nil {
			return err
		}
		args = append([]string{"--config", profilePath}, args...)
		if clientName == "codex" && !o.json {
			b, _ := json.Marshal(bin)
			a, _ := json.Marshal(args)
			printLine(cmd.OutOrStdout(), "[mcp_servers.govmc]\ncommand = %s\nargs = %s", b, a)
			return nil
		}
		key := "mcpServers"
		if clientName == "codex" {
			key = "mcp_servers"
		}
		return o.output(cmd, map[string]any{key: map[string]any{"govmc": map[string]any{"command": bin, "args": args}}}, nil)
	}}
	config.Flags().StringVar(&clientName, "client", "generic", "generic, codex, cursor or claude-code")
	root.AddCommand(config, o.mcpPrivilege())
	return root
}
