package command

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/httpserver"
	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/Necho-dev/govmc-cli/internal/wmks"
	webassets "github.com/Necho-dev/govmc-cli/web"
)

func (o *options) server(ctx context.Context, listen, webDir string, startupOutput io.Writer) error {
	t, err := o.resolve()
	if err != nil {
		return err
	}
	path, err := o.path()
	if err != nil {
		return err
	}
	s := &httpserver.Server{Journal: journal.From(ctx), Tasks: tasks.From(ctx), Token: envSetting(t, "API_TOKEN"), Insecure: t.Insecure, Language: o.preferredLanguage, StartupOutput: startupOutput, StartupLanguage: string(o.language)}
	if listen == "" {
		listen = envSetting(t, "LISTEN")
	}
	if listen == "" {
		listen = "127.0.0.1:8765"
	}
	s.Listen, err = httpserver.ListenAddress(listen)
	if err != nil {
		return err
	}
	s.WebFS, err = resolveWebAssets(webDir)
	if err != nil {
		return err
	}
	sdk, sdkErr := wmks.Open(path)
	if sdkErr != nil {
		return sdkErr
	}
	if o.wmksDirectory != "" {
		sdk.Root, err = filepath.Abs(o.wmksDirectory)
		if err != nil {
			return err
		}
	}
	s.WMKS, err = sdk.SnapshotDirectory(ctx)
	if err != nil {
		if store := journal.From(ctx); store != nil {
			store.Log(journal.Event{Event: "console.sdk.unavailable", Level: "WARNING", Code: "wmks_unavailable"})
		}
		s.WMKS = &wmks.Snapshot{Status: wmks.Status{State: "invalid", Reason: "wmks_unavailable"}}
	}
	defer s.WMKS.Close()
	dial, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s.ESXi, err = t.Dial(dial)
	if err != nil {
		return err
	}

	environments := httpserver.NewEnvironmentManager(path, t, s)
	s.Profiles = environments
	defer environments.Close()
	if s.Tasks != nil {
		defer s.Tasks.Close()
	}
	return s.Serve(ctx)
}

// resolveWebAssets keeps source builds usable without embedding SDK assets in
// public release binaries. Explicit directories take precedence over embedding.
func resolveWebAssets(dir string) (fs.FS, error) {
	if dir == "" {
		dir = os.Getenv("GOVMC_WEB_DIR")
	}
	var assets fs.FS
	if dir != "" {
		assets = os.DirFS(dir)
	} else {
		assets = webassets.WebAssets()
		if assets == nil {
			if local := httpserver.FindWeb(); local != "" {
				assets = os.DirFS(local)
			}
		}
	}
	if assets == nil {
		return nil, fmt.Errorf("Web assets unavailable: specify --web-dir /path/to/web (source checkout: --web-dir ./web), or install govmc-cli or rebuild the native executable")
	}
	for _, name := range []string{"wmks_loader.js", "settings.html", "settings.js", "settings.css", "tasks.js", "tasks.css", "cache.js", "datastores.html", "datastores.js", "datastores.css", "select.js", "locales/en.json", "locales/zh.json", "locales/_aliases.json", "index.html", "theme.css", "i18n.js", "app.js", "app.css", "console.html", "console.js", "console.css"} {
		if _, err := fs.Stat(assets, name); err != nil {
			return nil, fmt.Errorf("Web asset %s is unavailable: %w", name, err)
		}
	}
	return assets, nil
}
