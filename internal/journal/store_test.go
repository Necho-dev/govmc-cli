package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	s, err := Open(p, "tui")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, p
}
func TestAuditPersistenceAndMetadataOnly(t *testing.T) {
	s, p := openTestStore(t)
	ctx := With(context.Background(), s)
	secret := "never-record-this-password-token"
	if err := Run(ctx, Event{Operation: "vm.power.off", Profile: "lab", Resource: "vm-1"}, func() error { return errors.New(secret) }); err == nil {
		t.Fatal("operation error disappeared")
	}
	if err := Run(ctx, Event{Operation: "vm.edit", Profile: "prod"}, func() error { return context.DeadlineExceeded }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := Run(ctx, Event{Operation: "tool", Profile: "lab"}, func() error { return errors.New("mcp_readonly") }); err == nil {
		t.Fatal("denial disappeared")
	}
	result, err := s.Audits(ctx, Query{Limit: 1})
	if err != nil || !result.HasMore || len(result.Items) != 1 {
		t.Fatalf("pagination: %#v %v", result, err)
	}
	cases := []struct {
		query   Query
		outcome string
	}{{Query{Profile: "lab", Outcome: "failed"}, "failed"}, {Query{Outcome: "unknown"}, "unknown"}, {Query{Outcome: "denied"}, "denied"}}
	for _, tc := range cases {
		r, e := s.Audits(ctx, tc.query)
		if e != nil || len(r.Items) != 1 || r.Items[0].Outcome != tc.outcome {
			t.Fatalf("outcome %s: %#v %v", tc.outcome, r, e)
		}
	}
	logs, err := s.Logs(ctx, Query{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(logs)
	if strings.Contains(string(raw), secret) {
		t.Fatal("runtime leaked error text")
	}
	r, err := s.Audits(ctx, Query{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(r)
	if strings.Contains(string(raw), secret) {
		t.Fatal("audit leaked error text")
	}
	second, err := Open(p, "cli")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	persisted, err := second.Audits(ctx, Query{Profile: "prod"})
	if err != nil || len(persisted.Items) != 1 {
		t.Fatal("second process cannot query durable audit")
	}
	info, _ := os.Stat(filepath.Join(filepath.Dir(p), "data.db"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("database permissions")
	}
}
func TestAuditUnavailablePreventsOperation(t *testing.T) {
	s, _ := openTestStore(t)
	_ = s.Close()
	called := false
	err := Run(With(context.Background(), s), Event{Operation: "vm.edit"}, func() error { called = true; return nil })
	if err == nil || called {
		t.Fatal("operation dispatched without audit registration")
	}
}
func TestRuntimeSourcesFilteringAndNoSymlinkTraversal(t *testing.T) {
	s, p := openTestStore(t)
	for i := 0; i < 3; i++ {
		s.Log(Event{Event: fmt.Sprintf("test.%d", i), Profile: "lab", Level: "ERROR"})
	}
	logs, err := s.Logs(context.Background(), Query{Profile: "lab", Level: "ERROR", Limit: 2})
	if err != nil || len(logs.Items) != 2 || !logs.HasMore {
		t.Fatal(logs, err)
	}
	outside := t.TempDir()
	raw, _ := json.Marshal(Event{ID: "external", Time: time.Now(), Event: "external-secret"})
	_ = os.WriteFile(filepath.Join(outside, "000001.jsonl"), raw, 0600)
	if err := os.Symlink(outside, filepath.Join(filepath.Dir(p), "logs", "cli-"+strings.Repeat("a", 32))); err != nil {
		t.Skip("symlinks unavailable")
	}
	logs, err = s.Logs(context.Background(), Query{Search: "external-secret"})
	if err != nil || len(logs.Items) != 0 {
		t.Fatal("query followed external log directory symlink")
	}
}
func TestRetentionPreservesActiveWriterAndRecoversCrash(t *testing.T) {
	s, p := openTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(filepath.Dir(p), "logs")
	dir := filepath.Join(root, "mcp-"+strings.Repeat("b", 32))
	// Publish the fixture only after its writer lock is held: Open starts a
	// background prune which must not mistake an unfinished fixture for a crash.
	staged := filepath.Join(t.TempDir(), "writer")
	if err := os.MkdirAll(staged, 0700); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(staged, "000001.active")
	_ = os.WriteFile(active, []byte("{}\n"), 0600)
	old := time.Now().Add(-10 * 24 * time.Hour)
	_ = os.Chtimes(active, old, old)
	lock := flock.New(filepath.Join(staged, "writer.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	if err := os.Rename(staged, dir); err != nil {
		t.Fatal(err)
	}
	active = filepath.Join(dir, "000001.active")
	s.prune()
	if _, err := os.Stat(active); err != nil {
		t.Fatal("deleted live writer segment")
	}
	_ = lock.Close()
	s.prune()
	if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("crashed active segment was not recovered")
	}
	if _, err := os.Stat(strings.TrimSuffix(active, ".active") + ".jsonl"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired recovered segment was retained")
	}
}
func TestMCPMetadataDoesNotRecordArgumentsOrResponses(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := With(context.Background(), s)
	request := []byte(`{"method":"tools/call","params":{"name":"esxi_vm_power","arguments":{"password":"request-secret","profile":"lab"}}}`)
	response := []byte(`{"result":{"isError":true,"content":[{"text":"mcp_readonly response-secret"}]}}`)
	out, err := MCP(ctx, request, "lab", "readonly", func() ([]byte, error) { return response, nil })
	if err != nil || string(out) != string(response) {
		t.Fatal("MCP response changed")
	}
	rows, err := s.Audits(ctx, Query{})
	if err != nil || len(rows.Items) != 1 || rows.Items[0].Outcome != "denied" || rows.Items[0].Mode != "readonly" {
		t.Fatal(rows, err)
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "request-secret") || strings.Contains(string(raw), "response-secret") {
		t.Fatal("MCP payload persisted")
	}
}
