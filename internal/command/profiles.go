package command

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (o *options) profiles() *cobra.Command {
	root := group("profiles", "Manage saved target environments")
	root.AddCommand(o.renameProfile())
	root.AddCommand(&cobra.Command{Use: "list", Short: "List saved profiles", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := o.path()
		if err != nil {
			return err
		}
		c, err := target.Load(path)
		if err != nil {
			return err
		}
		permissions := map[string]string{}
		for name := range c.Profiles {
			permissions[name], _ = target.EffectiveMCPMode(c, name)
		}
		return o.output(cmd, struct {
			MCPEffective map[string]string         `json:"mcpEffective"`
			Active       string                    `json:"active"`
			Profiles     map[string]target.Profile `json:"profiles"`
		}{permissions, c.Active, c.Profiles}, func(w io.Writer) {
			names := make([]string, 0, len(c.Profiles))
			for name := range c.Profiles {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				mark := " "
				if name == c.Active {
					mark = "*"
				}
				printLine(w, "%s %-18s %-10s %s", mark, name, permissions[name], c.Profiles[name].URL)
			}
		})
	}})
	for _, use := range []string{"show [NAME]", "current"} {
		command := &cobra.Command{Use: use, Short: "Show a saved profile", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			path, err := o.path()
			if err != nil {
				return err
			}
			c, err := target.Load(path)
			if err != nil {
				return err
			}
			name := o.profile
			if name == "" {
				name = os.Getenv("GOVMC_PROFILE")
			}
			if name == "" {
				name = c.Active
			}
			if len(args) > 0 {
				name = args[0]
			}
			p, ok := c.Profiles[name]
			if !ok {
				return fmt.Errorf("no saved profile %q", name)
			}
			effective, err := target.EffectiveMCPMode(c, name)
			if err != nil {
				return err
			}
			return o.output(cmd, struct {
				MCPEffective string `json:"mcpEffective"`
				Name         string `json:"name"`
				target.Profile
				PasswordStored bool `json:"passwordStored"`
			}{effective, name, p, p.Password != ""}, nil)
		}}
		if use == "current" {
			command.Args = cobra.NoArgs
		}
		root.AddCommand(command)
	}
	for _, action := range []string{"add", "update", "use", "delete"} {
		var p target.Profile
		var passwordStdin, activate bool
		var profileMode string
		descriptions := map[string]string{"add": "Add a profile", "update": "Update a profile", "use": "Set the active profile", "delete": "Delete a profile"}
		cmd := &cobra.Command{Use: action + " NAME", Short: descriptions[action], Args: cobra.ExactArgs(1)}
		if action == "add" {
			cmd.Use = "add [NAME]"
			cmd.Args = cobra.MaximumNArgs(1)
		}
		if action == "delete" {
			cmd.Aliases = []string{"del"}
		}
		if action == "add" || action == "update" {
			cmd.Flags().StringVar(&profileMode, "mcp-mode", "", "profile MCP permission: inherit, readonly or readwrite")
			cmd.Flags().StringVar(&p.URL, "url", "", "ESXi HTTPS URL")
			cmd.Flags().StringVar(&p.Username, "username", "", "ESXi username")
			cmd.Flags().BoolVar(&p.Insecure, "insecure", false, "skip certificate validation for this profile")
			cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from standard input and store it encrypted")
			cmd.Flags().BoolVar(&activate, "activate", false, "make this profile the default")
			cmd.Flags().StringVar(&p.PasswordEnv, "password-env", "", "legacy password environment variable")
			_ = cmd.Flags().MarkHidden("password-env")
			cmd.MarkFlagsMutuallyExclusive("password-stdin", "password-env")
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			path, err := o.path()
			if err != nil {
				return err
			}
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			if action == "add" {
				if name == "" {
					name, err = readProfileField(cmd, o.language.Text("Environment name"), "")
					if err != nil {
						return err
					}
				}
				if !cmd.Flags().Changed("url") {
					value, err := readProfileField(cmd, o.language.Text("ESXi HTTPS URL"), "")
					if err != nil {
						return err
					}
					if err := cmd.Flags().Set("url", value); err != nil {
						return err
					}
				}
				if !cmd.Flags().Changed("username") {
					value, err := readProfileField(cmd, o.language.Text("Username"), "root")
					if err != nil {
						return err
					}
					if err := cmd.Flags().Set("username", value); err != nil {
						return err
					}
				}
			}
			secret := ""
			if action == "add" || action == "update" {
				config, err := target.Load(path)
				if err != nil {
					return err
				}
				current, exists := config.Profiles[name]
				if action == "add" && exists {
					return fmt.Errorf("profile %q already exists", name)
				}
				if action == "update" && !exists {
					return fmt.Errorf("unknown profile %q", name)
				}
				changed := false
				if cmd.Flags().Changed("mcp-mode") {
					if profileMode != "inherit" {
						if err := target.ValidateMCPMode(profileMode); err != nil {
							return err
						}
					}
					changed = true
				}
				if cmd.Flags().Changed("url") {
					current.URL = p.URL
					changed = true
				}
				if cmd.Flags().Changed("username") {
					current.Username = p.Username
					changed = true
				}
				if cmd.Flags().Changed("insecure") {
					current.Insecure = p.Insecure
					changed = true
				}
				check := current
				check.Password, check.PasswordEnv = "", ""
				if err := target.Validate(name, check); err != nil {
					return err
				}
				if p.PasswordEnv != "" {
					secret = target.LegacyEnv(p.PasswordEnv)
					if secret == "" {
						return errors.New("legacy password variable is empty; use --password-stdin")
					}
				}
				if passwordStdin || (secret == "" && (action == "add" || !changed)) {
					secret, err = readProfilePassword(cmd, passwordStdin, o.language)
					if err != nil {
						return err
					}
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			err = target.Edit(ctx, path, func(c *target.Config) error {
				current, exists := c.Profiles[name]
				if action == "add" && exists {
					return fmt.Errorf("profile %q already exists", name)
				}
				if action != "add" && !exists {
					return fmt.Errorf("unknown profile %q", name)
				}
				switch action {
				case "use":
					if err := target.Validate(name, current); err != nil {
						return err
					}
					c.Active = name
				case "delete":
					c.LegacyImported = true
					delete(c.Profiles, name)
					if c.Active == name {
						c.Active = ""
					}
				default:
					if cmd.Flags().Changed("mcp-mode") {
						current.MCP.Mode = profileMode
						if profileMode == "inherit" {
							current.MCP.Mode = ""
						}
					}
					if cmd.Flags().Changed("url") {
						current.URL = p.URL
					}
					if cmd.Flags().Changed("username") {
						current.Username = p.Username
					}
					if cmd.Flags().Changed("insecure") {
						current.Insecure = p.Insecure
					}
					if secret != "" {
						if err := target.SetPassword(path, name, &current, secret); err != nil {
							return err
						}
					}
					if err := target.Validate(name, current); err != nil {
						return err
					}
					c.Profiles[name] = current
					if activate || (action == "add" && c.Active == "") {
						c.Active = name
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			return o.output(cmd, map[string]string{"profile": name, "action": action}, func(w io.Writer) { printLine(w, "%s %s", action, name) })
		}
		root.AddCommand(cmd)
	}
	return root
}

func readProfilePassword(cmd *cobra.Command, fromStdin bool, lang language) (string, error) {
	if fromStdin {
		data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 65537))
		if err != nil {
			return "", err
		}
		if len(data) > 65536 {
			return "", errors.New("password is too long")
		}
		secret := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		if secret == "" {
			return "", errors.New("password must not be empty")
		}
		return secret, nil
	}
	input, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return "", errors.New("non-interactive profile setup requires --password-stdin")
	}
	fmt.Fprint(cmd.ErrOrStderr(), lang.Text("Password: "))
	data, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", errors.New("could not read password")
	}
	secret := string(data)
	if secret == "" {
		return "", errors.New("password must not be empty")
	}
	return secret, nil
}

func readProfileField(cmd *cobra.Command, label, defaultValue string) (string, error) {
	input, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return "", fmt.Errorf("%s is required; supply profile name, --url and --username in non-interactive mode", label)
	}
	if defaultValue != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s: ", label)
	}
	value, err := bufio.NewReader(input).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("could not read %s", label)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	if value == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return value, nil
}
