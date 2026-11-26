// Package wmks manages a local browser SDK. It is not a plugin runtime, and is
// independent of GoVMC's native CLI/MCP console input implementation.
package wmks

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/gofrs/flock"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const maxArchive = 32 << 20
const maxFile = 16 << 20
const maxTotal = 128 << 20

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var reserved = regexp.MustCompile(`^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])$`)

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Receipt struct {
	Schema        int       `json:"schema"`
	Digest        string    `json:"digest"`
	ArchiveSHA256 string    `json:"archiveSHA256"`
	SDKVersion    string    `json:"sdkVersion"`
	InstalledAt   time.Time `json:"installedAt"`
	Files         []File    `json:"files"`
}
type state struct {
	Schema int    `json:"schema"`
	Serial uint64 `json:"serial"`
	Digest string `json:"digest"`
}
type Plan struct {
	ArchiveSHA256   string `json:"archiveSHA256"`
	Digest          string `json:"digest"`
	SDKVersion      string `json:"sdkVersion"`
	Files           []File `json:"files"`
	RestartRequired bool   `json:"restartRequired,omitempty"`
	Serial          uint64 `json:"serial"`
	file            string
}
type Status struct {
	Available       bool   `json:"available"`
	State           string `json:"state"`
	Reason          string `json:"reason,omitempty"`
	Digest          string `json:"digest,omitempty"`
	SDKVersion      string `json:"sdkVersion,omitempty"`
	BaseURL         string `json:"baseURL,omitempty"`
	RestartRequired bool   `json:"restartRequired,omitempty"`
}
type Store struct{ Root string }
type Snapshot struct {
	Status Status
	files  map[string][]byte
	lease  *flock.Flock
}

func New(config string) *Store { return &Store{Root: filepath.Join(filepath.Dir(config), "wmks")} }

// Open resolves the configured SDK location relative to its configuration file.
func Open(config string) (*Store, error) {
	c, err := target.Load(config)
	if err != nil {
		return nil, err
	}
	s := New(config)
	if c.WMKS.Directory != "" {
		s.Root = c.WMKS.Directory
		if !filepath.IsAbs(s.Root) {
			s.Root = filepath.Join(filepath.Dir(config), s.Root)
		}
	}
	return s, nil
}
func Missing() *Snapshot {
	return &Snapshot{Status: Status{State: "missing"}, files: map[string][]byte{}}
}
func (s *Snapshot) Close() error {
	if s.lease != nil {
		return s.lease.Unlock()
	}
	return nil
}
func (s *Snapshot) Asset(path string) ([]byte, bool) { b, ok := s.files[path]; return b, ok }
func sum(b []byte) string                            { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func validPath(p string) bool {
	if !fs.ValidPath(p) || p == "." || len(p) > 512 || strings.ContainsAny(p, `\:`) {
		return false
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || reserved.MatchString(strings.ToUpper(strings.SplitN(part, ".", 2)[0])) {
			return false
		}
	}
	return true
}
func readJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func (s *Store) current() (state, error) {
	r := state{Schema: 1}
	b, e := os.ReadFile(filepath.Join(s.Root, "state.json"))
	if errors.Is(e, fs.ErrNotExist) {
		return r, nil
	}
	if e != nil {
		return r, e
	}
	if e = readJSON(b, &r); e != nil || r.Schema != 1 || (r.Digest != "" && !digestPattern.MatchString(r.Digest)) {
		return r, fmt.Errorf("wmks_state_invalid")
	}
	return r, nil
}
func (s *Store) lock(ctx context.Context) (*flock.Flock, error) {
	if e := os.MkdirAll(filepath.Join(s.Root, ".locks"), 0700); e != nil {
		return nil, e
	}
	l := flock.New(filepath.Join(s.Root, ".locks/catalog.lock"))
	ok, e := l.TryLockContext(ctx, 20*time.Millisecond)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, fmt.Errorf("wmks_busy")
	}
	return l, nil
}
func (s *Store) save(r state) error {
	r.Serial++
	b, _ := json.MarshalIndent(r, "", "  ")
	f, e := os.CreateTemp(s.Root, ".state-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(s.Root, "state.json"))
}
func archive(file string) (map[string][]byte, string, error) {
	f, e := os.Open(file)
	if e != nil {
		return nil, "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, maxArchive+1))
	if e != nil {
		return nil, "", e
	}
	if len(b) > maxArchive {
		return nil, "", fmt.Errorf("wmks_archive_too_large")
	}
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil {
		return nil, "", e
	}
	if len(z.File) > 1024 {
		return nil, "", fmt.Errorf("wmks_archive_too_many_files")
	}
	all := map[string][]byte{}
	seen := map[string]bool{}
	total := 0
	for _, f := range z.File {
		if f.Flags&((1<<0)|(1<<6)) != 0 {
			return nil, "", fmt.Errorf("wmks_archive_encrypted")
		}
		name := strings.TrimSuffix(f.Name, "/")
		key := strings.ToLower(name)
		if !validPath(name) || seen[key] {
			return nil, "", fmt.Errorf("wmks_archive_invalid_path")
		}
		seen[key] = true
		if f.Mode()&os.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) {
			return nil, "", fmt.Errorf("wmks_archive_invalid_type")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > maxFile {
			return nil, "", fmt.Errorf("wmks_archive_too_large")
		}
		r, e := f.Open()
		if e != nil {
			return nil, "", e
		}
		data, e := io.ReadAll(io.LimitReader(r, maxFile+1))
		r.Close()
		if e != nil {
			return nil, "", e
		}
		total += len(data)
		if len(data) > maxFile || total > maxTotal {
			return nil, "", fmt.Errorf("wmks_archive_too_large")
		}
		all[name] = data
	}
	roots := []string{}
	for p := range all {
		if p == "wmks.min.js" || strings.HasSuffix(p, "/wmks.min.js") {
			roots = append(roots, strings.TrimSuffix(p, "wmks.min.js"))
		}
	}
	if len(roots) != 1 {
		return nil, "", fmt.Errorf("wmks_sdk_missing_or_ambiguous")
	}
	root := roots[0]
	files := map[string][]byte{}
	for _, p := range []string{"wmks.min.js", "css/wmks-all.css", "img/touch_sprite.png", "img/touch_sprite_feedback.png"} {
		if len(all[root+p]) == 0 {
			return nil, "", fmt.Errorf("wmks_missing_resource: %s", p)
		}
	}
	js := string(all[root+"wmks.min.js"])
	if !strings.Contains(js, "WMKS") || !strings.Contains(js, "wmks") {
		return nil, "", fmt.Errorf("wmks_invalid_sdk")
	}
	for p, b := range all {
		if strings.HasPrefix(p, root) {
			rel := strings.TrimPrefix(p, root)
			if rel == "wmks.min.js" || strings.HasPrefix(rel, "css/") || strings.HasPrefix(rel, "img/") {
				files[rel] = b
			}
		}
		lower := strings.ToLower(p)
		if strings.Contains(lower, "license") || strings.Contains(lower, "notice") {
			files["notices/"+p] = b
		}
	}
	return files, sum(b), nil
}
func fileList(files map[string][]byte) ([]File, string) {
	list := []File{}
	for p, b := range files {
		list = append(list, File{p, sum(b)})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	b, _ := json.Marshal(list)
	return list, sum(b)
}
func (s *Store) Plan(file string) (Plan, error) {
	files, hash, e := archive(file)
	if e != nil {
		return Plan{}, e
	}
	list, digest := fileList(files)
	r, e := s.current()
	if e != nil {
		return Plan{}, e
	}
	return Plan{ArchiveSHA256: hash, Digest: digest, SDKVersion: "unidentified", Files: list, RestartRequired: true, Serial: r.Serial, file: file}, nil
}
func (s *Store) Install(ctx context.Context, p Plan) (Status, error) {
	fresh, e := s.Plan(p.file)
	if e != nil {
		return Status{}, e
	}
	if fresh.ArchiveSHA256 != p.ArchiveSHA256 || fresh.Digest != p.Digest {
		return Status{}, fmt.Errorf("wmks_plan_changed")
	}
	files, hash, e := archive(p.file)
	if e != nil {
		return Status{}, e
	}
	_, digest := fileList(files)
	if hash != p.ArchiveSHA256 || digest != p.Digest {
		return Status{}, fmt.Errorf("wmks_plan_changed")
	}
	l, e := s.lock(ctx)
	if e != nil {
		return Status{}, e
	}
	defer l.Unlock()
	r, e := s.current()
	if e != nil {
		return Status{}, e
	}
	if r.Serial != p.Serial {
		return Status{}, fmt.Errorf("wmks_plan_changed")
	}
	dest := filepath.Join(s.Root, "sdk", p.Digest)
	if _, e = os.Lstat(dest); errors.Is(e, fs.ErrNotExist) {
		stage, e := os.MkdirTemp(s.Root, ".staging-")
		if e != nil {
			return Status{}, e
		}
		defer os.RemoveAll(stage)
		for name, b := range files {
			path := filepath.Join(stage, filepath.FromSlash(name))
			if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				return Status{}, e
			}
			if e = os.WriteFile(path, b, 0600); e != nil {
				return Status{}, e
			}
		}
		receipt := Receipt{Schema: 1, Digest: p.Digest, ArchiveSHA256: p.ArchiveSHA256, SDKVersion: fresh.SDKVersion, Files: fresh.Files, InstalledAt: time.Now().UTC()}
		b, _ := json.MarshalIndent(receipt, "", "  ")
		if e = os.WriteFile(filepath.Join(stage, "receipt.json"), b, 0600); e != nil {
			return Status{}, e
		}
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return Status{}, e
		}
		if e = os.Rename(stage, dest); e != nil {
			return Status{}, e
		}
	} else if e != nil {
		return Status{}, e
	} else if _, _, e = s.load(p.Digest); e != nil {
		return Status{}, e
	}
	r.Digest = p.Digest
	if e = s.save(r); e != nil {
		return Status{}, e
	}
	return Status{Available: true, State: "ready", Digest: p.Digest, SDKVersion: p.SDKVersion, RestartRequired: true}, nil
}
func safeRead(root, name string) ([]byte, error) {
	path := filepath.Join(root, filepath.FromSlash(name))
	for p := path; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if st.Mode()&os.ModeSymlink != 0 || (p == path && !st.Mode().IsRegular()) {
			return nil, fmt.Errorf("wmks_invalid_resource_type")
		}
		if p == root {
			break
		}
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, maxFile+1))
	if len(b) > maxFile {
		return nil, fmt.Errorf("wmks_resource_too_large")
	}
	return b, e
}
func (s *Store) load(digest string) (Receipt, map[string][]byte, error) {
	if !digestPattern.MatchString(digest) {
		return Receipt{}, nil, fmt.Errorf("wmks_invalid_digest")
	}
	root := filepath.Join(s.Root, "sdk", digest)
	for p := root; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return Receipt{}, nil, e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return Receipt{}, nil, fmt.Errorf("wmks_symlink_rejected")
		}
		if p == s.Root {
			break
		}
	}
	b, e := safeRead(root, "receipt.json")
	if e != nil {
		return Receipt{}, nil, e
	}
	var receipt Receipt
	if e = readJSON(b, &receipt); e != nil {
		return receipt, nil, e
	}
	if receipt.Schema != 1 || receipt.Digest != digest || len(receipt.Files) == 0 || len(receipt.Files) > 1024 {
		return receipt, nil, fmt.Errorf("wmks_receipt_invalid")
	}
	files := map[string][]byte{}
	total := 0
	for _, f := range receipt.Files {
		if !validPath(f.Path) || f.Path == "receipt.json" || !digestPattern.MatchString(f.SHA256) {
			return receipt, nil, fmt.Errorf("wmks_receipt_invalid")
		}
		b, e := safeRead(root, f.Path)
		total += len(b)
		if e != nil || sum(b) != f.SHA256 || total > maxTotal {
			return receipt, nil, fmt.Errorf("wmks_integrity_failed")
		}
		files[f.Path] = b
	}
	_, computed := fileList(files)
	if computed != digest {
		return receipt, nil, fmt.Errorf("wmks_integrity_failed")
	}
	return receipt, files, nil
}
func (s *Store) Status() (Status, error) {
	r, e := s.current()
	if e != nil {
		return Status{State: "invalid", Reason: "wmks_state_invalid"}, e
	}
	if r.Digest == "" {
		return Status{State: "missing"}, nil
	}
	receipt, _, e := s.load(r.Digest)
	if e != nil {
		return Status{State: "invalid", Reason: "wmks_integrity_failed", Digest: r.Digest}, e
	}
	return Status{Available: true, State: "ready", Digest: r.Digest, SDKVersion: receipt.SDKVersion}, nil
}
func (s *Store) Snapshot(ctx context.Context) (*Snapshot, error) {
	l, e := s.lock(ctx)
	if e != nil {
		return nil, e
	}
	defer l.Unlock()
	status, e := s.Status()
	if e != nil {
		return &Snapshot{Status: status, files: map[string][]byte{}}, nil
	}
	if !status.Available {
		return Missing(), nil
	}
	lease := flock.New(filepath.Join(s.Root, ".locks", status.Digest+".lock"))
	ok, e := lease.TryRLock()
	if e != nil || !ok {
		return nil, fmt.Errorf("wmks_busy")
	}
	_, files, e := s.load(status.Digest)
	if e != nil {
		lease.Unlock()
		return nil, e
	}
	status.BaseURL = "/wmks/" + status.Digest + "/"
	out := map[string][]byte{}
	for name, b := range files {
		if !strings.HasPrefix(name, "notices/") {
			out[status.BaseURL+name] = b
		}
	}
	return &Snapshot{Status: status, files: out, lease: lease}, nil
}
func (s *Store) Uninstall(ctx context.Context) error {
	l, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer l.Unlock()
	r, e := s.current()
	if e != nil {
		return e
	}
	if r.Digest == "" {
		return fmt.Errorf("wmks_not_installed")
	}
	lease := flock.New(filepath.Join(s.Root, ".locks", r.Digest+".lock"))
	ok, e := lease.TryLock()
	if e != nil {
		return e
	}
	if !ok {
		return fmt.Errorf("wmks_in_use")
	}
	defer lease.Unlock()
	for parent := filepath.Join(s.Root, "sdk"); ; parent = filepath.Dir(parent) {
		st, err := os.Lstat(parent)
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("wmks_invalid_sdk_directory")
		}
		if parent == s.Root {
			break
		}
	}
	digest := r.Digest
	r.Digest = ""
	if e = s.save(r); e != nil {
		return e
	}
	return os.RemoveAll(filepath.Join(s.Root, "sdk", digest))
}

// SnapshotDirectory also accepts an extracted SDK without writing into it.
func (s *Store) SnapshotDirectory(ctx context.Context) (*Snapshot, error) {
	if _, err := os.Lstat(filepath.Join(s.Root, "state.json")); err == nil {
		return s.Snapshot(ctx)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if _, err := os.Lstat(filepath.Join(s.Root, "wmks.min.js")); errors.Is(err, os.ErrNotExist) {
		return Missing(), nil
	} else if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	total, count := 0, 0
	err := filepath.WalkDir(s.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("wmks_symlink_rejected")
		}
		if rel == "." {
			return nil
		}
		count++
		if count > 1024 || !validPath(rel) {
			return fmt.Errorf("wmks_invalid_directory")
		}
		if d.IsDir() {
			if rel == "css" || rel == "img" || strings.HasPrefix(rel, "css/") || strings.HasPrefix(rel, "img/") {
				return nil
			}
			return filepath.SkipDir
		}
		if rel != "wmks.min.js" && !strings.HasPrefix(rel, "css/") && !strings.HasPrefix(rel, "img/") {
			return nil
		}
		b, err := safeRead(s.Root, rel)
		if err != nil {
			return err
		}
		total += len(b)
		if total > maxTotal {
			return fmt.Errorf("wmks_resource_too_large")
		}
		files[rel] = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"wmks.min.js", "css/wmks-all.css", "img/touch_sprite.png", "img/touch_sprite_feedback.png"} {
		if len(files[name]) == 0 {
			return nil, fmt.Errorf("wmks_missing_resource: %s", name)
		}
	}
	js := string(files["wmks.min.js"])
	if !strings.Contains(js, "WMKS") || !strings.Contains(js, "wmks") {
		return nil, fmt.Errorf("wmks_invalid_sdk")
	}
	_, digest := fileList(files)
	base := "/wmks/" + digest + "/"
	out := map[string][]byte{}
	for name, b := range files {
		out[base+name] = b
	}
	return &Snapshot{Status: Status{Available: true, State: "ready", Digest: digest, SDKVersion: "unidentified", BaseURL: base}, files: out}, nil
}
