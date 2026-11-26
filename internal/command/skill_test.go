package command

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	skillassets "github.com/Necho-dev/govmc-cli/skills"
	"gopkg.in/yaml.v3"
)

func TestEmbeddedSkillInstallIsSelfContained(t *testing.T) {
	entry, err := skillassets.Skill.ReadFile(skillPath + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	frontmatter := strings.SplitN(string(entry), "---", 3)
	var identity struct{ Name, Description string }
	if len(frontmatter) != 3 {
		t.Fatal("Skill has no YAML frontmatter")
	}
	if err := yaml.Unmarshal([]byte(frontmatter[1]), &identity); err != nil {
		t.Fatal(err)
	}
	if identity.Name != skillassets.SkillName || identity.Description == "" {
		t.Fatal("Skill discovery identity disagrees with the installed directory")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GOVMC_CONFIG", filepath.Join(home, "config.yaml"))
	t.Setenv("GOVMC_LANG", "en")
	run := func(args ...string) (string, error) {
		cmd := New(args...)
		cmd.SetArgs(args)
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		err := cmd.Execute()
		return output.String(), err
	}
	for _, client := range []string{"codex", "cursor", "claude-code"} {
		t.Run(client, func(t *testing.T) {
			folder, _ := clientFolder(client)
			dest := filepath.Join(home, folder, "skills", skillassets.SkillName)
			if _, err := run("skill", "install", "--client", client); err != nil {
				t.Fatal(err)
			}
			count := 0
			if err := fs.WalkDir(skillassets.Skill, skillassets.SkillPath, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				relative := strings.TrimPrefix(path, skillassets.SkillPath+"/")
				expected, err := skillassets.Skill.ReadFile(path)
				if err != nil {
					return err
				}
				actual, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(relative)))
				if err != nil {
					return err
				}
				if !bytes.Equal(actual, expected) {
					t.Errorf("installed %s differs from embedded source", relative)
				}
				for _, match := range regexp.MustCompile(`\]\(([^)]+\.md)\)`).FindAllSubmatch(actual, -1) {
					if bytes.Contains(match[1], []byte("://")) {
						continue
					}
					if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Join(dest, relative)), string(match[1]))); err != nil {
						t.Errorf("broken installed reference %s: %v", match[1], err)
					}
				}
				count++
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if count == 0 {
				t.Fatal("Skill installation is empty")
			}
			local := filepath.Join(dest, "local-note.txt")
			if err := os.WriteFile(local, []byte("keep until explicitly replaced"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := run("skill", "install", "--client", client); err == nil {
				t.Fatal("silently replaced existing skill")
			}
			if _, err := os.Stat(local); err != nil {
				t.Fatal("refused replacement changed existing files")
			}
			if _, err := run("skill", "install", "--client", client, "--force"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dest, "references", "clone_workflow.md")); err != nil {
				t.Fatal(err)
			}
		})
	}
	output, err := run("skill", "show", "--json")
	if err != nil || !strings.Contains(output, `"name":"govmc"`) || !strings.Contains(output, "clone_workflow.md") {
		t.Fatal("skill show does not expose the maintained entrypoint")
	}
}

// Validate the documented command paths and flags without executing any command
// or opening a profile. This catches drift while preserving the safety of examples.
func TestSkillCLIExamplesMatchCommands(t *testing.T) {
	fences := regexp.MustCompile("(?s)```(?:sh|bash)\\n(.*?)\\n```")
	err := fs.WalkDir(skillassets.Skill, skillPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		body, err := skillassets.Skill.ReadFile(path)
		if err != nil {
			return err
		}
		for _, block := range fences.FindAllSubmatch(body, -1) {
			for _, line := range strings.Split(string(block[1]), "\n") {
				fields := strings.Fields(strings.TrimSpace(line))
				if len(fields) == 0 || fields[0] != "govmc" {
					continue
				}
				root := New()
				cmd, args, err := root.Find(fields[1:])
				if err != nil {
					t.Errorf("%s: %s: %v", path, line, err)
					continue
				}
				if err := cmd.ParseFlags(args); err != nil {
					t.Errorf("%s: %s: %v", path, line, err)
					continue
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
