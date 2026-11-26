package command

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	skillassets "github.com/Necho-dev/govmc-cli/skills"
	"github.com/spf13/cobra"
)

const skillPath = skillassets.SkillPath

func clientFolder(client string) (string, error) {
	switch client {
	case "codex":
		return ".agents", nil
	case "cursor":
		return ".cursor", nil
	case "claude-code":
		return ".claude", nil
	}
	return "", fmt.Errorf("--client must be codex, cursor or claude-code")
}

func (o *options) skill() *cobra.Command {
	root := group("skill", "Show or install the esxi-govc Skill")
	root.AddCommand(&cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		data, err := skillassets.Skill.ReadFile(skillPath + "/SKILL.md")
		if err != nil {
			return err
		}
		if o.json {
			return o.output(cmd, map[string]string{"name": skillassets.SkillName, "content": string(data)}, nil)
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}})
	var clientName string
	var force bool
	install := &cobra.Command{Use: "install", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		clientDir, err := clientFolder(clientName)
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		parent := filepath.Join(home, clientDir, "skills")
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}
		dest := filepath.Join(parent, skillassets.SkillName)
		_, err = os.Lstat(dest)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if exists && !force {
			return fmt.Errorf(o.language.Text("%s already exists; use --force to replace it"), dest)
		}
		stage, err := os.MkdirTemp(parent, ".esxi-govc-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		err = fs.WalkDir(skillassets.Skill, skillPath, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == skillPath {
				return nil
			}
			dst := filepath.Join(stage, filepath.FromSlash(strings.TrimPrefix(p, skillPath+"/")))
			if d.IsDir() {
				return os.MkdirAll(dst, 0755)
			}
			data, err := skillassets.Skill.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, 0644)
		})
		if err != nil {
			return err
		}
		if exists {
			backup, err := os.MkdirTemp(parent, ".esxi-govc-backup-")
			if err != nil {
				return err
			}
			old := filepath.Join(backup, "old")
			if err := os.Rename(dest, old); err != nil {
				_ = os.Remove(backup)
				return err
			}
			if err := os.Rename(stage, dest); err != nil {
				if restoreErr := os.Rename(old, dest); restoreErr != nil {
					return fmt.Errorf("%w; original Skill remains at %s: %v", err, old, restoreErr)
				}
				_ = os.Remove(backup)
				return err
			}
			_ = os.RemoveAll(backup)
		} else if err := os.Rename(stage, dest); err != nil {
			return err
		}
		return o.output(cmd, map[string]string{"path": dest}, func(w io.Writer) { printLine(w, o.language.Text("Installed %s"), dest) })
	}}
	install.Flags().StringVar(&clientName, "client", "", "codex, cursor or claude-code")
	install.Flags().BoolVar(&force, "force", false, "replace an existing installation")
	_ = install.MarkFlagRequired("client")
	root.AddCommand(install)
	return root
}
