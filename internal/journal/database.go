package journal

import (
	"errors"
	"os"
	"sync"
)

var databaseFileMu sync.Mutex

// PrepareDatabase checks the private database without opening an existing file.
// A raw open/close can drop SQLite's process-wide POSIX locks on another live
// connection: https://sqlite.org/howtocorrupt.html#_posix_advisory_locks_canceled_by_a_separate_thread_doing_close_
// Serialize first creation so no caller opens SQLite until the creation fd closes.
func PrepareDatabase(path string) error {
	databaseFileMu.Lock()
	defer databaseFileMu.Unlock()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		f, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr == nil {
			if err = f.Close(); err != nil {
				return err
			}
		} else if !errors.Is(createErr, os.ErrExist) {
			return createErr
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("data.db must be an owner-only regular file (0600)")
	}
	return nil
}
