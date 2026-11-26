package command

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (o *options) addOperations(root *cobra.Command) {
	root.AddCommand(o.remote("doctor", "Check connection, authentication and TLS", cobra.NoArgs,
		func(ctx context.Context, cmd *cobra.Command, _ []string, c *esxi.Client, t target.Target) error {
			about := c.About(ctx)
			return o.output(cmd, map[string]any{"profile": t.Name, "authenticated": true, "verifyTLS": !t.Insecure, "target": about}, nil)
		}))
	host := group("host", "Host inventory")
	host.AddCommand(o.remote("info", "Show host information", cobra.NoArgs,
		func(ctx context.Context, cmd *cobra.Command, _ []string, c *esxi.Client, _ target.Target) error {
			v, e := c.Host(ctx)
			return result(o, cmd, v, e)
		}))
	vm := group("vm", "Virtual machine operations")
	vm.AddCommand(o.remote("list", "List virtual machines", cobra.NoArgs,
		func(ctx context.Context, cmd *cobra.Command, _ []string, c *esxi.Client, _ target.Target) error {
			v, err := c.ListVMs(ctx, true)
			if err != nil {
				return err
			}
			return o.output(cmd, v, func(w io.Writer) {
				printLine(w, "%-16s %-15s %-24s %s", "ID", o.language.Text("Power"), o.language.Text("Name"), o.language.Text("Operating system"))
				for _, vm := range v {
					osName := vm.GuestOS
					if osName == "" {
						osName = vm.GuestID
					}
					printLine(w, "%-16s %-15s %-24s %s", vm.ID, o.language.Power(vm.PowerState), vm.Name, osName)
				}
			})
		}),
		o.remote("info VM", "Show virtual machine details", cobra.ExactArgs(1),
			func(ctx context.Context, cmd *cobra.Command, args []string, c *esxi.Client, _ target.Target) error {
				v, e := c.DescribeVM(ctx, args[0])
				if e != nil {
					return e
				}
				return o.output(cmd, v, nil)
			}))
	console := group("console", "Remote console operations")
	console.AddCommand(o.consoleInput())
	vm.AddCommand(o.editVM(), o.cloneVM(), console)

	var out string
	shot := o.remote("screenshot VM", "Save a PNG screenshot", cobra.ExactArgs(1),
		func(ctx context.Context, cmd *cobra.Command, args []string, c *esxi.Client, _ target.Target) error {
			path, err := filepath.Abs(out)
			if err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			data, err := c.Screenshot(ctx, args[0])
			if err == nil {
				_, err = f.Write(data)
			}
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				_ = os.Remove(path)
				return err
			}
			return o.output(cmd, map[string]string{"path": path}, func(w io.Writer) { printLine(w, "%s", path) })
		})
	shot.Flags().StringVar(&out, "out", "", "output PNG path")
	_ = shot.MarkFlagRequired("out")
	var yes bool
	power := o.remote("power ACTION VM", "Power on, hard off, hard reset or suspend", func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(2)(cmd, args); err != nil {
			return err
		}
		switch args[0] {
		case "on", "off", "reset", "suspend":
		default:
			return fmt.Errorf("%s", o.language.Text("action must be on, off, reset or suspend"))
		}
		if !yes && !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("%s", o.language.Text("non-interactive power requires --yes"))
		}
		return nil
	}, func(ctx context.Context, cmd *cobra.Command, args []string, c *esxi.Client, t target.Target) error {
		vm, err := c.GetVM(ctx, args[1])
		if err != nil {
			return err
		}
		if !yes {
			fmt.Fprintf(cmd.ErrOrStderr(), o.language.Text("Run %s on %s (%s), profile %s at %s? [y/N] "), args[0], vm.Info.Name, vm.Info.ID, t.Name, t.URL)
			answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
				return fmt.Errorf("%s", o.language.Text("power action cancelled"))
			}
		}
		if err := c.Power(ctx, vm.Info.ID, args[0], true); err != nil {
			return err
		}
		return o.output(cmd, map[string]string{"profile": t.Name, "vm": vm.Info.ID, "action": args[0], "status": "ok"}, nil)
	})
	power.Flags().BoolVar(&yes, "yes", false, "confirm the power action")
	vm.AddCommand(shot, power)
	metrics := group("metrics", "Query performance metrics")
	for _, entity := range []string{"vm", "host"} {
		use, args := entity, cobra.NoArgs
		if entity == "vm" {
			use, args = "vm VM", cobra.ExactArgs(1)
		}
		var q esxi.MetricsQuery
		m := o.remote(use, "Show QueryPerf samples", args, func(ctx context.Context, cmd *cobra.Command, args []string, c *esxi.Client, _ target.Target) error {
			if entity == "vm" {
				v, e := c.VMMetrics(ctx, args[0], q)
				return result(o, cmd, v, e)
			}
			v, e := c.HostMetrics(ctx, q)
			return result(o, cmd, v, e)
		})
		m.Flags().StringSliceVar(&q.Names, "names", nil, "comma-separated counter names")
		m.Flags().StringVar(&q.Instance, "instance", "", "counter instance")
		m.Flags().Int32Var(&q.MaxSample, "max-sample", 180, "maximum number of samples")
		metrics.AddCommand(m)
	}
	var entity string
	counters := o.remote("counters", "List performance counters", cobra.NoArgs, func(ctx context.Context, cmd *cobra.Command, _ []string, c *esxi.Client, _ target.Target) error {
		if entity != "vm" && entity != "host" {
			return fmt.Errorf("%s", o.language.Text("--entity must be vm or host"))
		}
		v, e := c.Counters(ctx, entity)
		return result(o, cmd, v, e)
	})
	counters.Flags().StringVar(&entity, "entity", "vm", "counter entity: vm or host")
	metrics.AddCommand(counters)
	ds := group("datastore", "Browse data stores")
	ds.AddCommand(o.remote("list", "List data stores", cobra.NoArgs, func(ctx context.Context, cmd *cobra.Command, _ []string, c *esxi.Client, _ target.Target) error {
		v, e := c.Datastores(ctx)
		return result(o, cmd, v, e)
	}), o.remote("files DATASTORE [PATH]", "List datastore directory entries", cobra.RangeArgs(1, 2), func(ctx context.Context, cmd *cobra.Command, args []string, c *esxi.Client, _ target.Target) error {
		var path string
		if len(args) > 1 {
			path = args[1]
		}
		v, e := c.DatastoreFiles(ctx, args[0], path)
		return result(o, cmd, v, e)
	}))
	ds.AddCommand(o.fileTransfer(true), o.fileTransfer(false))
	root.AddCommand(host, vm, metrics, ds, o.taskCommands())
}
