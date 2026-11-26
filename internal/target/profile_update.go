package target

import (
	"context"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/profile/history"
	"time"
)

// UpdateProfile commits an editor snapshot without overwriting a concurrent edit.
// A blank password preserves the stored credential, including across renames.
func UpdateProfile(ctx context.Context, path, name, newName string, expected, replacement Profile, password string) error {
	return Edit(ctx, path, func(config *Config) error {
		current, exists := config.Profiles[name]
		if !exists {
			return fmt.Errorf("unknown profile %q", name)
		}
		if !sameProfileSnapshot(current, expected) {
			return fmt.Errorf("profile %q changed; reopen the editor", name)
		}
		if newName != name {
			if _, exists := config.Profiles[newName]; exists {
				return fmt.Errorf("profile %q already exists", newName)
			}
		}
		replacement.MCP = current.MCP // Connection edits preserve permissions; CLI and TUI Settings.MCP own mode changes.
		replacement.ID, replacement.NameSince = current.ID, current.NameSince
		replacement.Password, replacement.PasswordEnv = current.Password, current.PasswordEnv
		if password == "" && newName != name {
			var err error
			if current.Password != "" {
				password, err = decryptPassword(path, name, current)
			} else {
				password = LegacyEnv(current.PasswordEnv)
			}
			if err != nil {
				return err
			}
			if password == "" {
				return fmt.Errorf("save a password before renaming profile %q", name)
			}
		}
		if password != "" {
			if err := SetPassword(path, newName, &replacement, password); err != nil {
				return err
			}
		}
		if err := Validate(newName, replacement); err != nil {
			return err
		}
		if newName != name {
			now := time.Now().UnixMilli()
			config.ProfileHistory = append(config.ProfileHistory, history.Binding{Name: name, ID: current.ID, From: current.NameSince})
			replacement.NameSince = now
			delete(config.Profiles, name)
			if config.Active == name {
				config.Active = newName
			}
		}
		config.Profiles[newName] = replacement
		return nil
	})
}

func sameProfileSnapshot(current, expected Profile) bool {
	if expected.ID == "" {
		expected.ID, expected.NameSince = current.ID, current.NameSince
	}
	return current == expected
}
