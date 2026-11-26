package locales

import "testing"

func TestCatalogParityAndAliases(t *testing.T) {
	for language, catalog := range catalogs {
		if len(catalog) != len(catalogs["en"]) {
			t.Errorf("%s catalog key count differs", language)
		}
		for key := range catalogs["en"] {
			if _, ok := catalog[key]; !ok {
				t.Errorf("%s is missing %s", language, key)
			}
		}
	}
	for alias, key := range aliases {
		if _, ok := catalogs["en"][key]; !ok {
			t.Errorf("alias %q has missing key %s", alias, key)
		}
	}
	for _, key := range []string{"tab.settings", "label.language", "action.cancel", "action.close", "action.search", "action.query"} {
		if Text("en", key) == key || Text("zh", key) == Text("en", key) {
			t.Errorf("missing translations: %s", key)
		}
	}
	if Text("fr", "action.cancel") != "Cancel" {
		t.Fatal("English fallback missing")
	}
	if Text("zh", "Cancel") != "取消" || Text("en", "取消") != "Cancel" {
		t.Fatal("legacy alias mismatch")
	}
	if Text("en", "unknown message") != "unknown message" {
		t.Fatal("unknown key fallback")
	}
}
