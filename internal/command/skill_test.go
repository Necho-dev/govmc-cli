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
)

func TestEmbeddedSkillInstallIsSelfContained(t *testing.T) {
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
			if count < 5 {
				t.Fatal("workflow reference files missing")
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
	if err != nil || !strings.Contains(output, "clone_workflow.md") {
		t.Fatal("skill show does not expose the maintained entrypoint")
	}
}
