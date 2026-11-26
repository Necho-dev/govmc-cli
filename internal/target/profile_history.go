package target

import "fmt"

func validateProfileHistory(c Config) error {
	seen := map[string]string{}
	for name, p := range c.Profiles {
		if p.ID == "" || p.NameSince < 0 {
			return fmt.Errorf("invalid identity for profile %q", name)
		}
		if prior, ok := seen[p.ID]; ok {
			return fmt.Errorf("profiles %q and %q share an identity", prior, name)
		}
		seen[p.ID] = name
	}
	for _, b := range c.ProfileHistory {
		if !namePattern.MatchString(b.Name) || b.ID == "" || b.From < 0 || b.Until != 0 && b.Until <= b.From {
			return fmt.Errorf("invalid profile history for %q", b.Name)
		}
		if p, ok := c.Profiles[b.Name]; ok && b.ID != p.ID && (b.Until == 0 || b.Until > p.NameSince) {
			return fmt.Errorf("overlapping profile history for %q", b.Name)
		}
	}
	return nil
}
