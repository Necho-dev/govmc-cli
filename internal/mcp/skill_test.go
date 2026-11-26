package mcp

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	skillassets "github.com/Necho-dev/govmc-cli/skills"
)

// Skill examples must refer to the real tool catalog. Validate names, argument
// names and required fields without dispatching calls or connecting to ESXi.
func TestSkillMCPExamplesMatchCatalog(t *testing.T) {
	catalog := map[string]map[string]any{}
	for _, tool := range allowedTools(true) {
		catalog[tool["name"].(string)] = tool["inputSchema"].(map[string]any)
	}
	fences := regexp.MustCompile("(?s)```json\\n(.*?)\\n```")
	examples := 0
	err := fs.WalkDir(skillassets.Skill, skillassets.SkillPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		body, err := skillassets.Skill.ReadFile(path)
		if err != nil {
			return err
		}
		for _, block := range fences.FindAllSubmatch(body, -1) {
			var value any
			if err := json.Unmarshal(block[1], &value); err != nil {
				t.Errorf("%s: invalid JSON example: %v", path, err)
				continue
			}
			call, ok := value.(map[string]any)
			if !ok { // Standalone action arrays are not tools/call objects.
				continue
			}
			name, _ := call["name"].(string)
			args, _ := call["arguments"].(map[string]any)
			shape, exists := catalog[name]
			if !exists || args == nil {
				t.Errorf("%s: example does not identify an available tool: %s", path, name)
				continue
			}
			// Normalize []string/map[string]string schema values to JSON types.
			raw, _ := json.Marshal(shape)
			var schema map[string]any
			if err := json.Unmarshal(raw, &schema); err != nil {
				return err
			}
			props, _ := schema["properties"].(map[string]any)
			for field := range args {
				if _, exists := props[field]; !exists {
					t.Errorf("%s: %s has no argument %s", path, name, field)
				}
			}
			required, _ := schema["required"].([]any)
			for _, field := range required {
				if _, exists := args[field.(string)]; !exists {
					t.Errorf("%s: %s is missing %s", path, name, field)
				}
			}
			if isConsoleInputTool(name) {
				delete(args, "profile") // Consumed by the configured MCP router.
				if _, err := parseConsoleTool(name, args); err != nil {
					t.Errorf("%s: console action example rejected: %v", path, err)
				}
			}
			examples++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if examples == 0 {
		t.Fatal("no runnable MCP examples found in the embedded Skill")
	}
}
