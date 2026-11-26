//go:build !windows

package privatefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedFileRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared")
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(path); err == nil {
		t.Fatal("accepted shared file")
	}
	if err := Protect(path); err != nil {
		t.Fatal(err)
	}
	if err := Check(path); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := Check(link); err == nil {
		t.Fatal("accepted symlink")
	}
}
