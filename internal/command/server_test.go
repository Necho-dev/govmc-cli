package command

import (
	"io/fs"
	"path/filepath"
	"testing"
)

func TestServeFindsSourceWebAssets(t *testing.T) {
	t.Setenv("GOVMC_WEB_DIR", "")
	// The new serve entry must find the same resources as the legacy entry.
	t.Chdir(filepath.Join("..", ".."))
	assets, err := resolveWebAssets("")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "settings.html", "console.js"} {
		if _, err := fs.Stat(assets, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, e := fs.Stat(assets, "wmks/wmks.min.js"); e == nil {
		t.Fatal("SDK embedded in ordinary Web")
	}
}

func TestServeExplicitAssetsDoNotSilentlyFallBack(t *testing.T) {
	t.Setenv("GOVMC_WEB_DIR", "")
	t.Chdir(filepath.Join("..", ".."))
	missing := t.TempDir()
	if _, err := resolveWebAssets(missing); err == nil {
		t.Fatal("explicit incomplete assets accepted")
	}
	t.Setenv("GOVMC_WEB_DIR", missing)
	if _, err := resolveWebAssets(""); err == nil {
		t.Fatal("incomplete environment override ignored")
	}
	// A command-line directory wins over the environment override.
	if _, err := resolveWebAssets("web"); err != nil {
		t.Fatal(err)
	}
}
