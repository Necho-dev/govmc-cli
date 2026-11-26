package command

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/Necho-dev/govmc-cli/internal/workflow"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (o *options) fileTransfer(copy bool) *cobra.Command {
	name := "move"
	if copy {
		name = "copy"
	}
	var dry, yes bool
	var dstDatastore string
	cmd := &cobra.Command{Use: name + " DATASTORE SOURCE DESTINATION", Short: "Preview and submit a datastore file transfer as a persistent task", Args: cobra.ExactArgs(3)}
	cmd.Flags().StringVar(&dstDatastore, "dst-datastore", "", "destination datastore (default source datastore)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "validate and preview without creating an execution task")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the previewed file operation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		t, err := o.resolve()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Minute)
		defer cancel()
		c, err := t.Dial(ctx)
		if err != nil {
			return err
		}
		defer target.Close(c)
		manager := tasks.From(ctx)
		preview, err := workflow.CopyMove(ctx, manager, c, t.Name, c.Endpoint(), args[0], args[1], args[2], copy, true, false, "", nil, dstDatastore)
		if err != nil {
			return err
		}
		if dry {
			return o.output(cmd, preview, nil)
		}
		raw, _ := json.MarshalIndent(preview, "", "  ")
		fmt.Fprintln(cmd.ErrOrStderr(), string(raw))
		if !yes {
			input, ok := cmd.InOrStdin().(*os.File)
			if !ok || !term.IsTerminal(int(input.Fd())) {
				return fmt.Errorf("%s", o.language.Text("File transfer requires --yes; use --dry-run to preview"))
			}
			fmt.Fprint(cmd.ErrOrStderr(), o.language.Text("Confirm file transfer? [y/N] "))
			answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if !strings.EqualFold(strings.TrimSpace(answer), "y") {
				return fmt.Errorf("%s", o.language.Text("Cancelled"))
			}
		}
		out, err := workflow.CopyMove(ctx, manager, c, t.Name, c.Endpoint(), args[0], args[1], args[2], copy, false, true, "", nil, dstDatastore)
		if err != nil {
			return err
		}
		job := out.(tasks.Task)
		fmt.Fprintln(cmd.ErrOrStderr(), o.language.Text("Task submitted")+": "+job.ID)
		result, err := manager.Wait(ctx, job.ID)
		if outputErr := o.output(cmd, result, nil); outputErr != nil {
			return outputErr
		}
		return err
	}
	return cmd
}
