package target

import "fmt"

const MCPReadonly = "readonly"
const MCPReadwrite = "readwrite"

type MCPSettings struct {
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`
}

func ValidateMCPMode(mode string) error {
	if mode != MCPReadonly && mode != MCPReadwrite {
		return fmt.Errorf("MCP mode must be readonly or readwrite")
	}
	return nil
}

// EffectiveMCPMode treats the global setting as a ceiling, never an override.
func EffectiveMCPMode(config Config, name string) (string, error) {
	if err := ValidateMCPMode(config.MCP.Mode); err != nil {
		return "", err
	}
	mode := config.MCP.Mode
	if name != "" {
		profile, ok := config.Profiles[name]
		if !ok {
			return "", fmt.Errorf("unknown profile %q", name)
		}
		if profile.MCP.Mode != "" {
			if err := ValidateMCPMode(profile.MCP.Mode); err != nil {
				return "", err
			}
			if profile.MCP.Mode == MCPReadonly {
				mode = MCPReadonly
			}
		}
	}
	return mode, nil
}

// MCPMode always reloads the saved settings. There is no process mode override.
func MCPMode(path, profile string) (string, error) {
	config, err := Load(path)
	if err != nil {
		return "", err
	}
	if profile == "" {
		profile = config.Active
	}
	return EffectiveMCPMode(config, profile)
}
