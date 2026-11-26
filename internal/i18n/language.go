package i18n

import "github.com/Necho-dev/govmc-cli/web/locales"

// Language selects the shared English or Chinese application dictionary.
type Language string

// Text resolves a dictionary key or its compatibility message alias.
func (l Language) Text(message string) string {
	return locales.Text(string(l), message)
}

// Power translates an ESXi power state for display.
func (l Language) Power(raw string) string {
	switch raw {
	case "poweredOn":
		return l.Text("Running")
	case "poweredOff":
		return l.Text("Powered off")
	case "suspended":
		return l.Text("Suspended")
	}
	return raw
}
