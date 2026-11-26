package command

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func readConsoleActions(r io.Reader) ([]esxi.ConsoleAction, error) {
	const limit = 64 * 1024
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("console actions file exceeds 64 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var actions []esxi.ConsoleAction
	if err := decoder.Decode(&actions); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON action array")
	}
	if err := esxi.ValidateConsoleActions(actions); err != nil {
		return nil, err
	}
	return actions, nil
}

func (o *options) consoleInput() *cobra.Command {
	var file string
	var yes, dryRun bool
	var actions []esxi.ConsoleAction
	cmd := o.remote("input VM", "Send confirmed keyboard and pointer actions to a VM console", func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		actions, err = readConsoleActions(f)
		if err != nil {
			return err
		}
		if !dryRun && !yes && !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("%s", o.language.Text("non-interactive console input requires --yes"))
		}
		return nil
	}, func(ctx context.Context, cmd *cobra.Command, args []string, c *esxi.Client, t target.Target) error {
		vm, err := c.GetVM(ctx, args[0])
		if err != nil {
			return err
		}
		// Preview contains action types, never text or key values (which may be secrets).
		kinds := make([]string, len(actions))
		for i, action := range actions {
			kinds[i] = action.Type
		}
		if dryRun {
			return o.output(cmd, map[string]any{"profile": t.Name, "vmId": vm.Info.ID, "name": vm.Info.Name, "powerState": vm.Info.PowerState, "actions": kinds, "dryRun": true}, nil)
		}
		if !yes {
			fmt.Fprintf(cmd.ErrOrStderr(), o.language.Text("Send %d console actions to %s (%s), profile %s? [y/N] "), len(actions), vm.Info.Name, vm.Info.ID, t.Name)
			answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
				return fmt.Errorf("%s", o.language.Text("console input cancelled"))
			}
		}
		result, err := c.ConsoleInput(ctx, vm.Info.ID, actions)
		if err != nil {
			_ = o.output(cmd, result, nil)
			return err
		}
		return o.output(cmd, result, nil)
	})
	cmd.Flags().StringVar(&file, "actions", "", "JSON file containing console actions")
	_ = cmd.MarkFlagRequired("actions")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm console input")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without acquiring a ticket or sending input")
	return cmd
}
