//go:build !windows

package target

import "os"

func makeKeyShared(path string) error { return os.Chmod(path, 0644) }
