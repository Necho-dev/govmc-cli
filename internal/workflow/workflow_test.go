package workflow

import (
	"context"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneReplayDoesNotRevalidateOrCreateAnotherTask(t *testing.T) {
	m, e := tasks.Open(filepath.Join(t.TempDir(), "config.yaml"), "cli")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	no := false
	spec := esxi.CloneSpec{Src: "base", Dst: "target", Datastore: "ds", DryRun: &no, Confirm: true}
	ctx := tasks.WithRequest(context.Background(), spec)
	job, e := m.Submit(ctx, "lab", "endpoint", "vm.clone", "target", spec, "target", "key", func(context.Context, *tasks.Reporter) (any, error) { return nil, nil })
	if e != nil {
		t.Fatal(e)
	}
	released := 0
	out, e := Clone(context.Background(), m, &esxi.Client{}, "lab", "endpoint", spec, "key", func() { released++ })
	if e != nil || out.(tasks.Task).ID != job.ID || released != 1 {
		t.Fatal(out, e, released)
	}
	spec.Dst = "different"
	if _, e = Clone(context.Background(), m, &esxi.Client{}, "lab", "endpoint", spec, "key", nil); e == nil {
		t.Fatal("same key accepted changed target")
	}
}
func TestTaskErrorMessageStripsCredentials(t *testing.T) {
	u, _ := url.Parse("https://root:secret@host/sdk")
	err := publicFailure(&esxi.Client{Reach: u}, esxi.ErrBad("failed https://root:secret@host/sdk secret\nother"))
	var public interface{ TaskFailure() (string, string) }
	public = err.(interface{ TaskFailure() (string, string) })
	_, message := public.TaskFailure()
	if strings.Contains(message, "secret") || strings.Contains(message, "root:") || strings.Contains(message, "\n") {
		t.Fatal(message)
	}
}
