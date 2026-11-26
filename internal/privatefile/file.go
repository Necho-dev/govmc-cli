// Package privatefile protects local state using POSIX modes or Windows ACLs.
package privatefile

import (
	"errors"
	"os"
)

// Check rejects links, non-regular files, and access granted to other users.
func Check(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("private state must be a regular file")
	}
	return checkAccess(path, info)
}
