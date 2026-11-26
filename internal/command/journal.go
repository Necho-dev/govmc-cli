package command

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/spf13/cobra"
)

// Wrap only command execution: help, shell completion and version stay read-only.
func (o *options) recordCommands(root *cobra.Command) {
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if run := cmd.RunE; run != nil {
			cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {
				path, err := o.path()
				if err != nil {
					return err
				}
				source := "cli"
				switch {
				case cmd.Name() == "tui":
					source = "tui"
				case cmd.Name() == "serve" && cmd.Parent() != nil && cmd.Parent().Name() == "mcp":
					source = "mcp"
				case cmd.Name() == "serve" || cmd == root:
					source = "serve"
				}
				store, err := journal.Open(path, source)
				if err != nil {
					return err
				}
				defer store.Close()
				previousLog := log.Writer()
				if source == "serve" {
					store.SetMirror(journal.ConsoleOutput(cmd.ErrOrStderr()))
				}
				log.SetOutput(store)
				defer log.SetOutput(previousLog)
				defer func() {
					if err != nil {
						store.Log(journal.Event{Event: "process.failed", Level: "ERROR", Code: journal.ErrorCode(err)})
					}
				}()
				previous := cmd.Context()
				manager, openErr := tasks.Open(path, source)
				if openErr != nil {
					return openErr
				}
				defer manager.Close()
				cmd.SetContext(tasks.With(journal.With(previous, store), manager))
				defer cmd.SetContext(previous)
				if source != "cli" || cmd.Name() == "logs" || cmd.Name() == "audits" {
					return run(cmd, args)
				}
				profile := o.profile
				if cfg, e := target.Load(path); e == nil {
					profile = selectedMCPProfile(cfg, profile)
				}
				operation := strings.TrimPrefix(cmd.CommandPath(), "govmc ")
				if strings.HasPrefix(operation, "wmks ") {
					profile = ""
				}
				resource := ""
				if cmd.Parent() != nil && cmd.Parent().Name() == "profiles" && len(args) > 0 {
					profile = args[0]
				}
				if cmd.Parent() != nil && cmd.Parent().Name() == "tasks" && len(args) > 0 {
					resource = args[0]
				}
				if cmd.Parent() != nil && cmd.Parent().Name() == "vm" && len(args) > 0 {
					resource = args[0]
					if cmd.Name() == "power" && len(args) > 1 {
						resource = args[1]
						switch args[0] {
						case "on", "off", "reset", "suspend":
							operation += "." + args[0]
						}
					}
				}
				if cmd.Parent() != nil && cmd.Parent().Name() == "console" && len(args) > 0 {
					resource = args[0]
				}
				return journal.Run(cmd.Context(), journal.Event{Source: "cli", Operation: operation, Resource: resource, Profile: profile}, func() error { return run(cmd, args) })
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}
func (o *options) journalCommands() []*cobra.Command {
	var commands []*cobra.Command
	for _, name := range []string{"logs", "audits"} {
		name := name
		var source, profile, level, outcome, search, since, until string
		var limit, offset int
		cmd := &cobra.Command{Use: name, Short: map[string]string{"logs": "Query local runtime logs", "audits": "Query local operation audits"}[name], Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			q := journal.Query{Source: source, Profile: profile, Level: level, Outcome: outcome, Search: search, Limit: limit, Offset: offset}
			if q.Profile == "" && o.profile != "" {
				q.Profile = o.profile
			}
			var err error
			if since != "" {
				q.Since, err = time.Parse(time.RFC3339, since)
				if err != nil {
					return err
				}
			}
			if until != "" {
				q.Until, err = time.Parse(time.RFC3339, until)
				if err != nil {
					return err
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			var result journal.Result
			if name == "logs" {
				result, err = journal.From(ctx).Logs(ctx, q)
			} else {
				result, err = journal.From(ctx).Audits(ctx, q)
			}
			if err != nil {
				return err
			}
			return o.output(cmd, result, nil)
		}}
		cmd.Flags().StringVar(&source, "source", "", "filter by entry point: cli, tui, serve or mcp")
		cmd.Flags().StringVar(&profile, "target", "", "filter by profile name")
		cmd.Flags().StringVar(&level, "level", "", "filter by level: INFO, WARNING or ERROR")
		cmd.Flags().StringVar(&outcome, "outcome", "", "filter by outcome")
		cmd.Flags().StringVar(&search, "search", "", "search event metadata")
		cmd.Flags().StringVar(&since, "since", "", "start time (RFC3339)")
		cmd.Flags().StringVar(&until, "until", "", "end time (RFC3339)")
		cmd.Flags().IntVar(&limit, "limit", 100, "maximum records, up to 500")
		cmd.Flags().IntVar(&offset, "offset", 0, "pagination offset")
		commands = append(commands, cmd)
	}
	return commands
}
