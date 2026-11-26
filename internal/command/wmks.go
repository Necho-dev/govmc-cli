package command

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/wmks"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"os"
	"strings"
	"time"
)

func (o *options) wmksConfirm(cmd *cobra.Command, yes bool) error {
	if yes {
		return nil
	}
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return fmt.Errorf("%s", o.language.Text("wmks.confirm_required"))
	}
	fmt.Fprint(cmd.ErrOrStderr(), o.language.Text("wmks.confirm"))
	a, _ := bufio.NewReader(f).ReadString('\n')
	if !strings.EqualFold(strings.TrimSpace(a), "y") {
		return fmt.Errorf("%s", o.language.Text("Cancelled"))
	}
	return nil
}
func (o *options) wmksCommands() *cobra.Command {
	root := group("wmks", o.language.Text("wmks.help"))
	store := func() (*wmks.Store, error) {
		p, e := o.path()
		if e != nil {
			return nil, e
		}
		return wmks.Open(p)
	}
	var file string
	var dry, yes bool
	install := &cobra.Command{Use: "install", Short: o.language.Text("wmks.install"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := store()
		if e != nil {
			return e
		}
		p, e := s.Plan(file)
		if e != nil {
			return e
		}
		if dry {
			return o.output(cmd, p, nil)
		}
		b, _ := json.MarshalIndent(p, "", "  ")
		fmt.Fprintln(cmd.ErrOrStderr(), string(b))
		fmt.Fprintln(cmd.ErrOrStderr(), o.language.Text("wmks.trust"))
		if e = o.wmksConfirm(cmd, yes); e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		v, e := s.Install(ctx, p)
		if e != nil {
			return e
		}
		return o.output(cmd, v, nil)
	}}
	install.Flags().StringVar(&file, "file", "", o.language.Text("wmks.file"))
	_ = install.MarkFlagRequired("file")
	install.Flags().BoolVar(&dry, "dry-run", false, o.language.Text("wmks.preview"))
	install.Flags().BoolVar(&yes, "yes", false, o.language.Text("wmks.confirm_flag"))
	root.AddCommand(install)
	for _, name := range []string{"status", "verify"} {
		name := name
		root.AddCommand(&cobra.Command{Use: name, Short: o.language.Text("wmks." + name), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			s, e := store()
			if e != nil {
				return e
			}
			v, e := s.Status()
			if err := o.output(cmd, v, nil); err != nil {
				return err
			}
			if e != nil {
				return e
			}
			if name == "verify" && !v.Available {
				return fmt.Errorf("wmks_not_installed")
			}
			return nil
		}})
	}
	var confirm bool
	uninstall := &cobra.Command{Use: "uninstall", Short: o.language.Text("wmks.uninstall"), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := store()
		if e != nil {
			return e
		}
		v, e := s.Status()
		if e != nil && v.Digest == "" {
			return e
		}
		if v.Digest == "" {
			return fmt.Errorf("wmks_not_installed")
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Fprintln(cmd.ErrOrStderr(), string(b))
		if e = o.wmksConfirm(cmd, confirm); e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		defer cancel()
		if e = s.Uninstall(ctx); e != nil {
			return e
		}
		return o.output(cmd, map[string]any{"state": "missing", "restartRequired": true}, nil)
	}}
	uninstall.Flags().BoolVar(&confirm, "yes", false, o.language.Text("wmks.confirm_flag"))
	root.AddCommand(uninstall)
	return root
}
