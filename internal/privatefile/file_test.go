package privatefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Protect(path); err != nil {
		t.Fatal(err)
	}
	if err := Check(path); err != nil {
		t.Fatal(err)
	}
	if err := Check(filepath.Dir(path)); err == nil {
		t.Fatal("accepted directory")
	}
}
