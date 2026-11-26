//go:build !windows

package privatefile

import (
	"errors"
	"os"
)

func checkAccess(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("private state must be accessible only by its owner (mode 0600)")
	}
	return nil
}

// Protect restricts a newly created file before any private data is written.
func Protect(path string) error { return os.Chmod(path, 0600) }

// SyncDirectory persists a completed atomic rename on platforms supporting it.
func SyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
