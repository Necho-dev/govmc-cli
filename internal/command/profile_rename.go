package command

import (
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/spf13/cobra"
)

func (o *options) renameProfile() *cobra.Command {
	return &cobra.Command{Use: "rename NAME NEW_NAME", Short: "Rename a profile and preserve its history", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		path, e := o.path()
		if e != nil {
			return e
		}
		cfg, e := target.Load(path)
		if e != nil {
			return e
		}
		old, ok := cfg.Profiles[args[0]]
		if !ok {
			return fmt.Errorf("unknown profile %q", args[0])
		}
		if e = target.UpdateProfile(cmd.Context(), path, args[0], args[1], old, old, ""); e != nil {
			return e
		}
		return o.output(cmd, map[string]string{"old": args[0], "profile": args[1]}, nil)
	}}
}
