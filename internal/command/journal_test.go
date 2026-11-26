package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/journal"
)

func TestLocalJournalCLIQueriesDoNotNeedProfileConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("GOVMC_CONFIG", path)
	store, err := journal.Open(path, "tui")
	if err != nil {
		t.Fatal(err)
	}
	store.Log(journal.Event{Event: "fixture", Profile: "deleted"})
	_ = journal.Run(journal.With(context.Background(), store), journal.Event{Profile: "deleted", Operation: "vm.edit"}, func() error { return errors.New("private-error") })
	_ = store.Close()
	for _, args := range [][]string{{"logs", "--target", "deleted", "--source", "tui", "--json"}, {"--profile", "deleted", "audits", "--outcome", "failed", "--json"}} {
		command := New(args...)
		command.SetArgs(args)
		var out bytes.Buffer
		command.SetOut(&out)
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		var result journal.Result
		if err := json.Unmarshal(out.Bytes(), &result); err != nil || len(result.Items) == 0 {
			t.Fatal("query output is not JSON or needs a live target", out.String(), err)
		}
		if bytes.Contains(out.Bytes(), []byte("private-error")) {
			t.Fatal("error body leaked")
		}
	}
}
