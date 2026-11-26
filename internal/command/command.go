package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Necho-dev/govmc-cli"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/spf13/cobra"
)

type options struct {
	configPath        string
	wmksDirectory     string
	profile           string
	json              bool
	language          language
	preferredLanguage string
}

func New(args ...string) *cobra.Command {
	lang, preferred := languages(args)
	o := &options{language: lang, preferredLanguage: preferred}
	var languageFlag string
	var bareListen, bareWeb string
	root := &cobra.Command{
		Use: "govmc", Short: "Manage VMware hosts and virtual machines",
		Long:    govmc.Wordmark + "\n\n" + lang.Text("Manage VMware hosts and virtual machines"),
		Version: govmc.Version, SilenceUsage: true, SilenceErrors: true,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.server(cmd.Context(), bareListen, bareWeb, cmd.ErrOrStderr())
		},
	}
	root.PersistentFlags().StringVar(&o.configPath, "config", "", "configuration YAML file (default ~/.govmc/config.yaml)")
	root.PersistentFlags().StringVar(&o.profile, "profile", "", "target profile for this command")
	root.PersistentFlags().StringVar(&languageFlag, "language", "", "interface language: en or zh")
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if languageFlag != "" && normalizeLanguage(languageFlag) == "" {
			return fmt.Errorf("%s", o.language.Text("language must be en or zh"))
		}
		return nil
	}
	root.PersistentFlags().BoolVar(&o.json, "json", false, "write machine-readable JSON")
	root.Flags().StringVar(&bareListen, "listen", "", "listen IP and port")
	root.Flags().StringVar(&bareWeb, "web-dir", "", "Web UI asset directory")
	serve := &cobra.Command{Use: "serve", Short: "Serve WebUI and APIs on this device", Args: cobra.NoArgs}
	var listen, webDir string
	serve.Flags().StringVar(&listen, "listen", "", "listen IP and port (default 127.0.0.1:8765)")
	serve.Flags().StringVar(&webDir, "web-dir", "", "Web UI asset directory")
	serve.Flags().StringVar(&o.wmksDirectory, "wmks", "", "SDK directory for this process only")
	root.Flags().StringVar(&o.wmksDirectory, "wmks", "", "SDK directory for this process only")
	serve.RunE = func(cmd *cobra.Command, _ []string) error {
		return o.server(cmd.Context(), listen, webDir, cmd.ErrOrStderr())
	}
	root.AddCommand(o.profiles(), serve, o.mcp(), o.skill(), o.tui(), o.config(), o.wmksCommands())
	o.addOperations(root)
	root.AddCommand(o.journalCommands()...)
	o.recordCommands(root)
	localizeHelp(root, lang)
	return root
}

func (o *options) output(cmd *cobra.Command, v any, human func(io.Writer)) error {
	if !o.json && human != nil {
		human(cmd.OutOrStdout())
		return nil
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	if !o.json {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

func (o *options) resolve() (target.Target, error) {
	path, err := o.path()
	if err != nil {
		return target.Target{}, err
	}
	return target.Resolve(path, o.profile)
}

func (o *options) remote(use, short string, args cobra.PositionalArgs, run func(context.Context, *cobra.Command, []string, *esxi.Client, target.Target) error) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: args, RunE: func(cmd *cobra.Command, args []string) error {
		t, err := o.resolve()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 90*time.Second)
		defer cancel()
		c, err := t.Dial(ctx)
		if err != nil {
			return err
		}
		defer target.Close(c)
		return run(ctx, cmd, args, c, t)
	}}
}

func envSetting(t target.Target, key string) string {
	if !t.Saved() {
		return target.LegacyEnv(key)
	}
	return os.Getenv(key)
}

func result[T any](o *options, cmd *cobra.Command, v T, err error) error {
	if err != nil {
		return err
	}
	return o.output(cmd, v, nil)
}

func group(use, short string) *cobra.Command { return &cobra.Command{Use: use, Short: short} }

func printLine(w io.Writer, format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }

func (o *options) path() (string, error) {
	if o.configPath != "" {
		return filepath.Abs(o.configPath)
	}
	return target.Path()
}
