package wmks

import (
	"archive/zip"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sdkFiles() map[string][]byte {
	return map[string][]byte{"sdk/wmks.min.js": []byte("/* WMKS */ window.wmks={};"), "sdk/css/wmks-all.css": []byte("body{}"), "sdk/img/touch_sprite.png": []byte("png"), "sdk/img/touch_sprite_feedback.png": []byte("png"), "LICENSE.txt": []byte("fixture notice")}
}
func zipSDK(t *testing.T, files map[string][]byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sdk.zip")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	for p, b := range files {
		w, e := z.Create(p)
		if e != nil {
			t.Fatal(e)
		}
		w.Write(b)
	}
	z.Close()
	f.Close()
	return p
}
func installSDK(t *testing.T, s *Store, file string) Status {
	t.Helper()
	p, e := s.Plan(file)
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.Install(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestSDKInstallSnapshotUpdateAndUninstall(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "config.yaml"))
	ctx := context.Background()
	file := zipSDK(t, sdkFiles())
	p, e := s.Plan(file)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(s.Root); !os.IsNotExist(e) {
		t.Fatal("preview wrote state")
	}
	v := installSDK(t, s, file)
	installSDK(t, s, file)
	if v.SDKVersion != "unidentified" {
		t.Fatal("invented SDK version")
	}
	snap, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer snap.Close()
	if !snap.Status.Available {
		t.Fatal(snap.Status)
	}
	if _, ok := snap.Asset(snap.Status.BaseURL + "wmks.min.js"); !ok {
		t.Fatal("JS unavailable")
	}
	if _, ok := snap.Asset(snap.Status.BaseURL + "receipt.json"); ok {
		t.Fatal("receipt exposed")
	}
	if _, ok := snap.Asset(snap.Status.BaseURL + "notices/LICENSE.txt"); ok {
		t.Fatal("unexpected notice route")
	}
	if e = s.Uninstall(ctx); e == nil || !strings.Contains(e.Error(), "wmks_in_use") {
		t.Fatal(e)
	}
	files := sdkFiles()
	files["sdk/wmks.min.js"] = []byte("/* WMKS updated */ window.wmks={};")
	newStatus := installSDK(t, s, zipSDK(t, files))
	if newStatus.Digest == v.Digest {
		t.Fatal("update did not get a new digest")
	}
	if snap.Status.Digest != v.Digest {
		t.Fatal("live snapshot switched SDK")
	}
	snap.Close()
	snap2, e := s.Snapshot(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if snap2.Status.Digest != newStatus.Digest {
		t.Fatal("restart did not adopt SDK")
	}
	snap2.Close()
	if e = s.Uninstall(ctx); e != nil {
		t.Fatal(e)
	}
	status, e := s.Status()
	if e != nil || status.Available {
		t.Fatal(status, e)
	}
	_ = p
}
func TestSDKChangedPlanAndTampering(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "config.yaml"))
	file := zipSDK(t, sdkFiles())
	p, _ := s.Plan(file)
	changed := sdkFiles()
	changed["sdk/wmks.min.js"] = []byte("WMKS wmks changed")
	b, _ := os.ReadFile(zipSDK(t, changed))
	os.WriteFile(file, b, 0600)
	if _, e := s.Install(context.Background(), p); e == nil {
		t.Fatal("changed file accepted")
	}
	v := installSDK(t, s, zipSDK(t, sdkFiles()))
	os.WriteFile(filepath.Join(s.Root, "sdk", v.Digest, "wmks.min.js"), []byte("tampered"), 0600)
	status, e := s.Status()
	if e == nil || status.State != "invalid" {
		t.Fatal(status, e)
	}
	snap, e := s.Snapshot(context.Background())
	if e != nil || snap.Status.Available {
		t.Fatal(e)
	}
	snap.Close()
	if e = s.Uninstall(context.Background()); e != nil {
		t.Fatal("cannot remove corrupt SDK", e)
	}
}
func TestSDKUnsafeArchives(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "config.yaml"))
	for _, name := range []string{"../outside", "/absolute", `C:/outside`, `a\b`, "a/../../escape", "CON.txt"} {
		if _, e := s.Plan(zipSDK(t, map[string][]byte{name: []byte("x")})); e == nil {
			t.Fatal("unsafe archive accepted", name)
		}
	}
	files := sdkFiles()
	delete(files, "sdk/css/wmks-all.css")
	if _, e := s.Plan(zipSDK(t, files)); e == nil {
		t.Fatal("incomplete SDK accepted")
	}
	files = sdkFiles()
	files["other/wmks.min.js"] = []byte("WMKS wmks")
	if _, e := s.Plan(zipSDK(t, files)); e == nil {
		t.Fatal("ambiguous root accepted")
	}
	p := filepath.Join(t.TempDir(), "link.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	header := &zip.FileHeader{Name: "link"}
	header.SetMode(os.ModeSymlink | 0777)
	w, _ := z.CreateHeader(header)
	w.Write([]byte("../outside"))
	z.Close()
	f.Close()
	if _, e := s.Plan(p); e == nil {
		t.Fatal("symlink accepted")
	}
}

func TestSDKZIPLayoutsAndRetainedFiles(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "config.yaml"))
	for _, prefix := range []string{"", "sdk/", "release/HTML Console SDK/sdk/"} {
		t.Run(prefix, func(t *testing.T) {
			files := map[string][]byte{}
			for p, b := range sdkFiles() {
				if strings.HasPrefix(p, "sdk/") {
					files[prefix+strings.TrimPrefix(p, "sdk/")] = b
				} else {
					files[p] = b
				}
			}
			files[prefix+"wmks.js"] = []byte("not a required runtime resource")
			files[prefix+"css/extra.css"] = []byte("body{}")
			file := zipSDK(t, files)
			renamed := strings.TrimSuffix(file, ".zip") + ".data"
			if e := os.Rename(file, renamed); e != nil {
				t.Fatal(e)
			}
			p, e := s.Plan(renamed)
			if e != nil {
				t.Fatal("valid SDK nesting rejected", e)
			}
			paths := map[string]bool{}
			for _, f := range p.Files {
				paths[f.Path] = true
			}
			if !paths["wmks.min.js"] || !paths["css/extra.css"] || !paths["notices/LICENSE.txt"] || paths["wmks.js"] {
				t.Fatal(paths)
			}
		})
	}
}
func TestSDKZIPRejectsMalformedDuplicatesAndSizeLimits(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "config.yaml"))
	p := filepath.Join(t.TempDir(), "broken.zip")
	os.WriteFile(p, []byte("not ZIP"), 0600)
	if _, e := s.Plan(p); e == nil {
		t.Fatal("non-ZIP accepted")
	}
	for _, names := range [][]string{{"same", "same"}, {"file", "FILE"}} {
		p = filepath.Join(t.TempDir(), "duplicate.zip")
		f, _ := os.Create(p)
		z := zip.NewWriter(f)
		for _, name := range names {
			w, _ := z.Create(name)
			w.Write([]byte("x"))
		}
		z.Close()
		f.Close()
		if _, e := s.Plan(p); e == nil || !strings.Contains(e.Error(), "wmks_archive_invalid_path") {
			t.Fatal("duplicate path accepted", e)
		}
	}
	p = filepath.Join(t.TempDir(), "oversize.zip")
	f, _ := os.Create(p)
	if e := f.Truncate(maxArchive + 1); e != nil {
		t.Fatal(e)
	}
	f.Close()
	if _, e := s.Plan(p); e == nil || !strings.Contains(e.Error(), "wmks_archive_too_large") {
		t.Fatal("oversized compressed input accepted", e)
	}
	p = filepath.Join(t.TempDir(), "header.zip")
	f, _ = os.Create(p)
	z := zip.NewWriter(f)
	if _, e := z.CreateRaw(&zip.FileHeader{Name: "large", Method: zip.Store, UncompressedSize64: maxFile + 1}); e != nil {
		t.Fatal(e)
	}
	z.Close()
	f.Close()
	if _, e := s.Plan(p); e == nil || !strings.Contains(e.Error(), "wmks_archive_too_large") {
		t.Fatal("oversized file header accepted", e)
	}
	p = filepath.Join(t.TempDir(), "entries.zip")
	f, _ = os.Create(p)
	z = zip.NewWriter(f)
	for i := 0; i < 1025; i++ {
		if _, e := z.Create(fmt.Sprintf("file-%04d", i)); e != nil {
			t.Fatal(e)
		}
	}
	z.Close()
	f.Close()
	if _, e := s.Plan(p); e == nil || !strings.Contains(e.Error(), "wmks_archive_too_many_files") {
		t.Fatal("entry limit not enforced", e)
	}
}

type sdkZeroReader struct{}

func (sdkZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
func TestSDKZIPActualExpansionLimit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "expansion.zip")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	for i := 0; i < 9; i++ {
		w, e := z.Create(fmt.Sprintf("padding-%d", i))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = io.CopyN(w, sdkZeroReader{}, maxFile); e != nil {
			t.Fatal(e)
		}
	}
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	f.Close()
	s := New(filepath.Join(t.TempDir(), "config.yaml"))
	if _, e = s.Plan(p); e == nil || !strings.Contains(e.Error(), "wmks_archive_too_large") {
		t.Fatal("actual total expansion limit not enforced", e)
	}
}

func TestSDKZIPRejectsEncryptionFlags(t *testing.T) {
	for _, flag := range []uint16{1, 64} {
		t.Run(fmt.Sprint(flag), func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "encrypted.zip")
			f, e := os.Create(p)
			if e != nil {
				t.Fatal(e)
			}
			z := zip.NewWriter(f)
			for name, b := range sdkFiles() {
				header := &zip.FileHeader{Name: name, Method: zip.Store, CRC32: crc32.ChecksumIEEE(b), UncompressedSize64: uint64(len(b)), CompressedSize64: uint64(len(b))}
				if strings.HasSuffix(name, "wmks.min.js") {
					header.Flags = flag
				}
				w, e := z.CreateRaw(header)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = w.Write(b); e != nil {
					t.Fatal(e)
				}
			}
			z.Close()
			f.Close()
			s := New(filepath.Join(t.TempDir(), "config.yaml"))
			if _, e = s.Plan(p); e == nil {
				t.Fatal("encryption flag accepted as plain SDK")
			}
		})
	}
}

func TestSDKZIPRejectsEmptyRequiredFilesAndWrongJavaScript(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "config.yaml"))
	for _, name := range []string{"sdk/wmks.min.js", "sdk/css/wmks-all.css", "sdk/img/touch_sprite.png", "sdk/img/touch_sprite_feedback.png"} {
		files := sdkFiles()
		files[name] = nil
		if _, e := s.Plan(zipSDK(t, files)); e == nil || !strings.Contains(e.Error(), "wmks_missing_resource") {
			t.Fatal("empty required file accepted", name, e)
		}
	}
	files := sdkFiles()
	files["sdk/wmks.min.js"] = []byte("window.unrelated = true;")
	if _, e := s.Plan(zipSDK(t, files)); e == nil || !strings.Contains(e.Error(), "wmks_invalid_sdk") {
		t.Fatal("unrelated JS accepted", e)
	}
}

func TestConfiguredAndExtractedDirectory(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(config, []byte("version: 1\nwmks:\n  directory: custom/sdk\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	if s.Root != filepath.Join(base, "custom/sdk") {
		t.Fatal(s.Root)
	}
	for name, b := range sdkFiles() {
		if !strings.HasPrefix(name, "sdk/") {
			continue
		}
		p := filepath.Join(s.Root, strings.TrimPrefix(name, "sdk/"))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := s.SnapshotDirectory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	if !snap.Status.Available {
		t.Fatal(snap.Status)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "state.json")); !os.IsNotExist(err) {
		t.Fatal("extracted directory mutated", err)
	}
	if _, ok := snap.Asset(snap.Status.BaseURL + "wmks.min.js"); !ok {
		t.Fatal("missing JS")
	}
	if err := os.Remove(filepath.Join(s.Root, "css/wmks-all.css")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SnapshotDirectory(context.Background()); err == nil {
		t.Fatal("accepted incomplete SDK")
	}
}
