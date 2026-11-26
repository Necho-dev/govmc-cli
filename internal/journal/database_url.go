package journal

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// DatabaseURL constructs a file URI, never treating a Windows drive as a host.
func DatabaseURL(path string) (url.URL, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return url.URL{}, err
	}
	normalized := filepath.ToSlash(absolute)
	if runtime.GOOS == "windows" && !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return url.URL{Scheme: "file", Path: normalized}, nil
}
