package webassets

import (
	"embed"
	"io/fs"
)

//go:embed wmks_loader.js index.html settings.html settings.js settings.css tasks.js tasks.css cache.js datastores.html datastores.js datastores.css select.js locales/en.json locales/zh.json locales/_aliases.json app.css theme.css fonts app.js i18n.js console.html console.css console.js vendor
var embeddedWeb embed.FS

func WebAssets() fs.FS {
	return embeddedWeb
}
