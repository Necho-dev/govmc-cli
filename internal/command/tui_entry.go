package command

import (
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tui"
	"github.com/spf13/cobra"
)

func (o *options) tui() *cobra.Command {
	return &cobra.Command{Use: "tui", Short: "Open the terminal workbench", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		configPath, err := o.path()
		if err != nil {
			return err
		}
		t, err := o.resolve()
		if err != nil {
			if o.profile != "" {
				return err
			}
			t = target.Target{}
		}
		return tui.Run(cmd.Context(), configPath, t, string(o.language))
	}}
}
