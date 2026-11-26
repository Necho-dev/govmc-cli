package journal

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatabaseURLDoesNotUsePathAsHost(t *testing.T) {
	for _, path := range []string{filepath.Join(t.TempDir(), "with spaces #", "data.db"), "data.db"} {
		u, err := DatabaseURL(path)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(u.String())
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.Contains(u.String(), "%5C") {
			t.Fatalf("invalid SQLite URI %s", u.String())
		}
		absolute, _ := filepath.Abs(path)
		expected := filepath.ToSlash(absolute)
		if !strings.HasPrefix(expected, "/") {
			expected = "/" + expected
		}
		if parsed.Path != expected {
			t.Fatalf("path changed: %q != %q", parsed.Path, expected)
		}
	}
}
