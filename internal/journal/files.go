package journal

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofrs/flock"
)

var sessionName = regexp.MustCompile(`^(cli|tui|serve|mcp)-[a-f0-9]{32}$`)

func validSegment(path string) bool {
	if !sessionName.MatchString(filepath.Base(filepath.Dir(path))) {
		return false
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() {
		return false
	}
	info, err = os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// Recover crashed writers without ever deleting another process's active file.
func (s *Store) recoverSegments() {
	paths, _ := filepath.Glob(filepath.Join(s.root, "logs", "*", "*.active"))
	for _, path := range paths {
		if !validSegment(path) {
			continue
		}
		lock := flock.New(filepath.Join(filepath.Dir(path), "writer.lock"))
		locked, err := lock.TryLock()
		if err == nil && locked {
			_ = os.Rename(path, strings.TrimSuffix(path, ".active")+".jsonl")
			_ = lock.Unlock()
		}
		_ = lock.Close()
	}
}

func (s *Store) removeEmptySessions() {
	dirs, _ := os.ReadDir(filepath.Join(s.root, "logs"))
	for _, entry := range dirs {
		if !entry.IsDir() || !sessionName.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(s.root, "logs", entry.Name())
		lock := flock.New(filepath.Join(dir, "writer.lock"))
		locked, err := lock.TryLock()
		if err != nil || !locked {
			_ = lock.Close()
			continue
		}
		files, readErr := os.ReadDir(dir)
		empty := readErr == nil
		for _, file := range files {
			if file.Name() != "writer.lock" {
				empty = false
			}
		}
		_ = lock.Close()
		if empty {
			_ = os.Remove(filepath.Join(dir, "writer.lock"))
			_ = os.Remove(dir)
		}
	}
}
