package journal

import (
	"errors"
	"os"
	"sync"

	"github.com/Necho-dev/govmc-cli/internal/privatefile"
)

var databaseFileMu sync.Mutex

// PrepareDatabase checks the private database without opening an existing file.
// A raw open/close can drop SQLite's process-wide POSIX locks on another live
// connection: https://sqlite.org/howtocorrupt.html#_posix_advisory_locks_canceled_by_a_separate_thread_doing_close_
// Serialize first creation so no caller opens SQLite until the creation fd closes.
func PrepareDatabase(path string) error {
	databaseFileMu.Lock()
	defer databaseFileMu.Unlock()
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		f, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr == nil {
			protectErr := privatefile.Protect(path)
			closeErr := f.Close()
			if err := errors.Join(protectErr, closeErr); err != nil {
				return err
			}
		} else if !errors.Is(createErr, os.ErrExist) {
			return createErr
		}
		_, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	return privatefile.Check(path)
}
