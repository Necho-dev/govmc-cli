// Package locales loads the shared per-language CLI, TUI and Web catalogs.
package locales

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"
)

//go:embed *.json
var files embed.FS

var catalogs, aliases = load()

func load() (map[string]map[string]string, map[string]string) {
	catalogs := make(map[string]map[string]string)
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		data, err := files.ReadFile(entry.Name())
		if err != nil {
			panic(err)
		}
		var values map[string]string
		if err := json.Unmarshal(data, &values); err != nil {
			panic(err)
		}
		catalogs[strings.TrimSuffix(entry.Name(), ".json")] = values
	}
	if len(catalogs["en"]) == 0 {
		panic("missing default locale en.json")
	}
	data, err := files.ReadFile("_aliases.json")
	if err != nil {
		panic(err)
	}
	var aliases map[string]string
	if err := json.Unmarshal(data, &aliases); err != nil {
		panic(err)
	}
	return catalogs, aliases
}

// Text supports stable message IDs. Legacy literal labels resolve through the
// shared compatibility aliases, so no channel maintains its own translations.
func Text(language, message string) string {
	key := message
	if alias, ok := aliases[key]; ok {
		key = alias
	}
	if value, ok := catalogs[language][key]; ok {
		return value
	}
	if value, ok := catalogs["en"][key]; ok {
		return value
	}
	return message
}
