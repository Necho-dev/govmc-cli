package command

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/Necho-dev/govmc-cli/internal/workflow"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func (o *options) cloneVM() *cobra.Command {
	var ds, dstDS, pg, file string
	var expansion []string
	var hotCPU, hotMemory, locked bool
	var reservation int64
	var cpu, mem int32
	var dry, yes, power bool
	cmd := &cobra.Command{Use: "clone SOURCE TARGET", Short: "Clone a powered-off machine; preview with --dry-run", Args: cobra.ExactArgs(2)}
	cmd.Flags().StringVar(&ds, "datastore", "", "source datastore (default inferred from VMX)")
	cmd.Flags().StringVar(&dstDS, "dst-datastore", "", "destination datastore (default source datastore)")
	cmd.Flags().StringVar(&file, "config", "", "optional clone configuration JSON file")
	cmd.Flags().StringSliceVar(&expansion, "expand-disk", nil, "disk expansion key:MiB (repeatable)")
	cmd.Flags().BoolVar(&hotCPU, "cpu-hot-add", false, "override CPU hot add (default inherit)")
	cmd.Flags().BoolVar(&hotMemory, "memory-hot-add", false, "override memory hot add (default inherit)")
	cmd.Flags().BoolVar(&locked, "lock-memory", false, "override full memory reservation lock")
	cmd.Flags().Int64Var(&reservation, "memory-reservation-mb", 0, "override memory reservation MiB")
	cmd.Flags().StringVar(&pg, "portgroup", "", "target portgroup (default inherit)")
	cmd.Flags().Int32Var(&cpu, "cpu", 0, "target vCPU count (default inherit)")
	cmd.Flags().Int32Var(&mem, "memory-mb", 0, "target memory MiB (default inherit)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "only validate and preview; no device writes")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm execution")
	cmd.Flags().BoolVar(&power, "power-on", false, "power on after verification")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		t, e := o.resolve()
		if e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 45*time.Minute)
		defer cancel()
		c, e := t.Dial(ctx)
		if e != nil {
			return e
		}
		defer target.Close(c)
		var spec esxi.CloneSpec
		if file != "" {
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			defer f.Close()
			dec := json.NewDecoder(io.LimitReader(f, 1<<20))
			dec.DisallowUnknownFields()
			if err = dec.Decode(&spec); err != nil {
				return err
			}
			if err = dec.Decode(new(any)); err != io.EOF {
				return fmt.Errorf("expected one clone configuration object")
			}
		}
		spec.Src = args[0]
		spec.Dst = args[1]
		if cmd.Flags().Changed("dst-datastore") {
			spec.DestinationDatastore = dstDS
		}
		spec.Confirm = false
		spec.DryRun = &dry
		if cmd.Flags().Changed("datastore") {
			spec.Datastore = ds
		}
		if cmd.Flags().Changed("portgroup") {
			spec.Portgroup = pg
		}
		if cmd.Flags().Changed("power-on") {
			spec.PowerOn = &power
		}
		if cmd.Flags().Changed("cpu-hot-add") {
			spec.CPUHotAdd = &hotCPU
		}
		if cmd.Flags().Changed("memory-hot-add") {
			spec.MemoryHotAdd = &hotMemory
		}
		if cmd.Flags().Changed("lock-memory") {
			spec.LockMemory = &locked
		}
		if cmd.Flags().Changed("memory-reservation-mb") {
			spec.MemoryReservationMB = &reservation
		}
		for _, value := range expansion {
			pair := strings.Split(value, ":")
			if len(pair) != 2 {
				return fmt.Errorf("expected --expand-disk key:MiB")
			}
			key, err := strconv.ParseInt(pair[0], 10, 32)
			size, e := strconv.ParseInt(pair[1], 10, 64)
			if err != nil || e != nil {
				return fmt.Errorf("invalid disk expansion")
			}
			spec.ExpandDisks = append(spec.ExpandDisks, esxi.DiskExpansion{Key: int32(key), CapacityMiB: size})
		}

		if cmd.Flags().Changed("cpu") {
			spec.NumCPU = &cpu
		}
		if cmd.Flags().Changed("memory-mb") {
			spec.MemoryMB = &mem
		}
		spec, plan, e := c.PrepareClone(ctx, spec)
		if e != nil {
			return e
		}
		if dry {
			return o.output(cmd, plan, nil)
		}
		if !yes {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("%s", o.language.Text("Clone requires --yes; use --dry-run to preview"))
			}
			b, _ := json.MarshalIndent(plan, "", "  ")
			fmt.Fprintln(cmd.ErrOrStderr(), string(b))
			fmt.Fprint(cmd.ErrOrStderr(), o.language.Text("Confirm clone? [y/N] "))
			answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if !strings.EqualFold(strings.TrimSpace(answer), "y") {
				return fmt.Errorf("%s", o.language.Text("Cancelled"))
			}
		}
		spec.Confirm = true
		out, e := workflow.Clone(ctx, tasks.From(ctx), c, t.Name, c.Endpoint(), spec, "", nil)
		if e != nil {
			return e
		}
		job := out.(tasks.Task)
		fmt.Fprintln(cmd.ErrOrStderr(), o.language.Text("Task submitted")+": "+job.ID)
		view, e := tasks.From(ctx).Wait(ctx, job.ID)
		_ = o.output(cmd, view, nil)
		return e
	}
	return cmd
}
func (o *options) taskCommands() *cobra.Command {
	root := group("tasks", "List, inspect or cancel persistent tasks")
	var q tasks.Query
	var all bool
	var since, until string
	list := &cobra.Command{Use: "list", Short: "List persisted tasks", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !all {
			if o.profile != "" {
				q.Profile = o.profile
			} else if p, e := o.path(); e == nil {
				if cfg, e := target.Load(p); e == nil {
					q.Profile = cfg.Active
				}
			}
		}
		var e error
		if since != "" {
			q.Since, e = time.Parse(time.RFC3339, since)
			if e != nil {
				return e
			}
		}
		if until != "" {
			q.Until, e = time.Parse(time.RFC3339, until)
			if e != nil {
				return e
			}
		}
		page, e := tasks.From(cmd.Context()).List(q)
		if e != nil {
			return e
		}
		return o.output(cmd, page, nil)
	}}
	list.Flags().StringVar(&q.Status, "status", "", "task status")
	list.Flags().StringVar(&q.Kind, "type", "", "task type")
	list.Flags().StringVar(&q.Source, "source", "", "cli, mcp, tui or serve")
	list.Flags().StringVar(&q.Search, "search", "", "task ID or resource")
	list.Flags().IntVar(&q.Limit, "limit", 50, "page size, up to 500")
	list.Flags().IntVar(&q.Offset, "offset", 0, "pagination offset")
	list.Flags().BoolVar(&all, "all-profiles", false, "include all environments")
	list.Flags().StringVar(&since, "since", "", "start time RFC3339")
	list.Flags().StringVar(&until, "until", "", "end time RFC3339")
	var watch bool
	var timeout time.Duration
	status := &cobra.Command{Use: "status TASK_ID", Short: "Show task details and steps", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		m := tasks.From(cmd.Context())
		t, e := m.Get(args[0])
		if e != nil {
			return e
		}
		if o.profile != "" && t.Profile != o.profile {
			return fmt.Errorf("task belongs to another profile")
		}
		if !watch {
			return o.output(cmd, t, nil)
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		for {
			t, e = m.Get(args[0])
			if e != nil {
				return e
			}
			if e = o.output(cmd, t, nil); e != nil {
				return e
			}
			if t.Status != "queued" && t.Status != "running" && t.Status != "cancelling" {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}}
	status.Flags().BoolVar(&watch, "watch", false, "watch state changes (JSON snapshots)")
	status.Flags().DurationVar(&timeout, "timeout", 45*time.Minute, "watch timeout")
	var yes bool
	cancel := &cobra.Command{Use: "cancel TASK_ID", Short: "Request cancellation; does not roll back", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !yes {
			return fmt.Errorf("cancelling requires --yes")
		}
		m := tasks.From(cmd.Context())
		t, e := m.Get(args[0])
		if e != nil {
			return e
		}
		if o.profile != "" && t.Profile != o.profile {
			return fmt.Errorf("task belongs to another profile")
		}
		if e = m.Cancel(t.ID); e != nil {
			return e
		}
		return o.output(cmd, map[string]string{"taskId": t.ID, "status": "cancel_requested"}, nil)
	}}
	cancel.Flags().BoolVar(&yes, "yes", false, "confirm cancellation")
	root.AddCommand(list, status, cancel)
	return root
}
