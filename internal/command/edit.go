package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/spf13/cobra"
)

func (o *options) editVM() *cobra.Command {
	var file string
	var yes, hotAdd bool
	cmd := o.remote("edit VM", "Update VM configuration from JSON", func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		if !yes {
			return fmt.Errorf("editing requires --yes to confirm")
		}
		return nil
	}, func(ctx context.Context, cmd *cobra.Command, args []string, c *esxi.Client, _ target.Target) error {
		var input io.Reader = cmd.InOrStdin()
		if file != "-" {
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			defer f.Close()
			input = f
		}
		var patch esxi.VMChange
		dec := json.NewDecoder(io.LimitReader(input, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&patch); err != nil {
			return err
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("expected exactly one configuration object")
		}
		patch.RequirePoweredOff, patch.RequireHotAdd = !hotAdd, hotAdd
		vm, err := c.DescribeVM(ctx, args[0])
		if err != nil {
			return err
		}
		if patch.ChangeVersion == "" && vm.Config != nil {
			patch.ChangeVersion = vm.Config.ChangeVersion
		}
		if err := c.ChangeVM(ctx, vm.ID, patch, true); err != nil {
			return err
		}
		result, err := c.DescribeVM(ctx, vm.ID)
		if err != nil {
			return err
		}
		return o.output(cmd, result, nil)
	})
	cmd.Flags().StringVar(&file, "file", "", "JSON patch file, or - for stdin")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm configuration changes")
	cmd.Flags().BoolVar(&hotAdd, "hot-add", false, "apply supported hardware changes on a running VM")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}
