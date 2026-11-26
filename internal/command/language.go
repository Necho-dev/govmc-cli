package command

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/i18n"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type language = i18n.Language

func normalizeLanguage(value string) language {
	value = strings.ToLower(strings.TrimSpace(value))
	base, _, _ := strings.Cut(strings.NewReplacer("_", "-", ".", "-").Replace(value), "-")
	switch base {
	case "en", "zh":
		return language(base)
	}
	return ""
}

func languages(args []string) (language, string) {
	var explicit language
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--language" && i+1 < len(args) {
			explicit = normalizeLanguage(args[i+1])
		}
		if v, ok := strings.CutPrefix(arg, "--language="); ok {
			explicit = normalizeLanguage(v)
		}
	}
	if explicit != "" {
		return explicit, string(explicit)
	}
	if v := normalizeLanguage(os.Getenv("GOVMC_LANG")); v != "" {
		return v, string(v)
	}
	if p, err := (&options{configPath: configArgument(args)}).path(); err == nil {
		if c, err := target.Load(p); err == nil {
			if v := normalizeLanguage(c.Language); v != "" {
				return v, string(v)
			}
		}
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := normalizeLanguage(os.Getenv(key)); v != "" {
			return v, ""
		}
	}
	return "en", ""
}

func localizeHelp(root *cobra.Command, l language) {
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		cmd.InitDefaultHelpFlag()
		cmd.Short = l.Text(cmd.Short)
		cmd.Long = l.Text(cmd.Long)
		cmd.Flags().VisitAll(func(f *pflag.Flag) { f.Usage = l.Text(f.Usage) })
		cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) { f.Usage = l.Text(f.Usage) })
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
	if l != "zh" {
		return
	}
	usage := root.UsageTemplate()
	for _, entry := range [][2]string{
		{"Usage:", "用法："}, {"Aliases:", "别名："}, {"Examples:", "示例："},
		{"Available Commands:", "可用命令："}, {"Additional Commands:", "其他命令："},
		{"Global Flags:", "全局选项："}, {"Flags:", "选项："}, {"Help Topics:", "帮助主题："},
		{"Use \"{{.CommandPath}} [command] --help\" for more information about a command.", "使用 \"{{.CommandPath}} [command] --help\" 查看命令帮助。"},
	} {
		usage = strings.ReplaceAll(usage, entry[0], entry[1])
	}
	root.SetUsageTemplate(usage)
}

func (o *options) config() *cobra.Command {
	root := group("config", "Manage application preferences")
	root.AddCommand(o.configModeCommands()...)
	root.AddCommand(&cobra.Command{Use: "language [LANGUAGE]", Short: "Show or set the language (en, zh, auto)", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := o.path()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				c, err := target.Load(p)
				if err != nil {
					return err
				}
				return o.output(cmd, map[string]string{"language": string(o.language), "configured": c.Language}, nil)
			}
			value := normalizeLanguage(args[0])
			if value == "" && args[0] != "auto" {
				return fmt.Errorf("%s", o.language.Text("language must be en, zh or auto"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			if err := target.Edit(ctx, p, func(c *target.Config) error {
				c.Language = string(value)
				return nil
			}); err != nil {
				return err
			}
			return o.output(cmd, map[string]string{"configured": string(value)}, nil)
		}})
	root.AddCommand(&cobra.Command{Use: "mcp-mode [MODE]", Short: "Show or set MCP permissions (readonly, readwrite)", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := o.path()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			mode, err := target.MCPMode(path, "")
			if err != nil {
				return err
			}
			return o.output(cmd, map[string]string{"mode": mode}, nil)
		}
		if err := target.ValidateMCPMode(args[0]); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		if err := target.Edit(ctx, path, func(config *target.Config) error { config.MCP.Mode = args[0]; return nil }); err != nil {
			return err
		}
		return o.output(cmd, map[string]string{"mode": args[0]}, nil)
	}})
	return root
}

func configArgument(args []string) string {
	var value string
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--config" && i+1 < len(args) {
			value = args[i+1]
		}
		if v, ok := strings.CutPrefix(arg, "--config="); ok {
			value = v
		}
	}
	return value
}
