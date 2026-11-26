package target

import "testing"

func TestMCPGlobalCeilingAndProfileRestriction(t *testing.T) {
	for _, global := range []string{MCPReadonly, MCPReadwrite} {
		for _, profile := range []string{"", MCPReadonly, MCPReadwrite} {
			c := Config{MCP: MCPSettings{Mode: global}, Profiles: map[string]Profile{"lab": {MCP: MCPSettings{Mode: profile}}}}
			got, err := EffectiveMCPMode(c, "lab")
			want := MCPReadwrite
			if global == MCPReadonly || profile == MCPReadonly {
				want = MCPReadonly
			}
			if err != nil || got != want {
				t.Fatalf("global=%s profile=%s got=%s err=%v", global, profile, got, err)
			}
		}
	}
	for _, c := range []Config{{MCP: MCPSettings{Mode: "full"}}, {MCP: MCPSettings{Mode: MCPReadonly}, Profiles: map[string]Profile{"lab": {MCP: MCPSettings{Mode: "full"}}}}} {
		if _, err := EffectiveMCPMode(c, "lab"); err == nil {
			t.Fatal("invalid permission must fail closed")
		}
	}
}
