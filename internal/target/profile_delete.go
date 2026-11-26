package target

import (
	"context"
	"fmt"
)

// DeleteProfile removes only the exact local record that the user confirmed.
func DeleteProfile(ctx context.Context, path, name string, expected Profile) error {
	return Edit(ctx, path, func(config *Config) error {
		p, exists := config.Profiles[name]
		if !exists {
			return fmt.Errorf("unknown profile %q", name)
		}
		if !sameProfileSnapshot(p, expected) {
			return fmt.Errorf("profile %q changed; confirm deletion again", name)
		}
		delete(config.Profiles, name)
		if config.Active == name {
			config.Active = ""
		}
		config.LegacyImported = true
		return nil
	})
}
