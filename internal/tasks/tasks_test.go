package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func manager(t *testing.T, path string) *Manager {
	t.Helper()
	m, e := Open(path, "cli")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	return m
}
func waitState(t *testing.T, m *Manager, id, state string) Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v, e := m.Get(id)
		if e != nil {
			t.Fatal(e)
		}
		if v.Status == state {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	v, _ := m.Get(id)
	t.Fatalf("want %s: %+v", state, v)
	return v
}
func TestPersistenceQueueStepsAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	m := manager(t, path)
	job, e := m.Submit(context.Background(), "lab", "https://host", "vm.clone", "new", map[string]string{"src": "base"}, "new", "", func(ctx context.Context, r *Reporter) (any, error) {
		r.Step("copy", "running")
		r.Remote("task-42", nil)
		r.Step("copy", "ok")
		r.Remote("", nil)
		return map[string]string{"name": "new"}, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	v := waitState(t, m, job.ID, "succeeded")
	if len(v.Steps) != 1 || v.Steps[0].EndedAt == nil || v.Percent != nil || v.StartedAt == nil || v.EndedAt == nil {
		t.Fatalf("bad progress: %+v", v)
	}
	if !v.StartedAt.Before(*v.EndedAt) || v.StartedAt.After(v.Steps[0].StartedAt) {
		t.Fatalf("task start was overwritten by completion: %+v", v)
	}
	m.Close()
	reader := manager(t, path)
	p, e := reader.List(Query{Profile: "lab"})
	if e != nil || len(p.Items) != 1 || p.Items[0].ID != job.ID {
		t.Fatal(p, e)
	}
	if _, e = reader.Get(job.ID); e != nil {
		t.Fatal(e)
	}
}
func TestIdempotencyDestinationExclusionCancellationAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	a := manager(t, path)
	b := manager(t, path)
	var called atomic.Int32
	run := func(ctx context.Context, r *Reporter) (any, error) {
		called.Add(1)
		r.Step("copy", "running")
		<-ctx.Done()
		return nil, ctx.Err()
	}
	job, e := a.Submit(context.Background(), "lab", "https://host", "file.copy", "dst", nil, "dst", "key", run)
	if e != nil {
		t.Fatal(e)
	}
	waitState(t, a, job.ID, "running")
	duplicate, e := b.Submit(context.Background(), "lab", "https://host", "file.copy", "dst", nil, "dst", "key", run)
	if e != nil || duplicate.ID != job.ID {
		t.Fatal(duplicate, e)
	}
	if _, e = b.Submit(context.Background(), "lab", "https://host", "file.copy", "dst", nil, "dst", "", run); e == nil {
		t.Fatal("destination conflict permitted")
	}
	if e = b.Cancel(job.ID); e != nil {
		t.Fatal(e)
	}
	v := waitState(t, a, job.ID, "cancelled")
	if !v.CancelRequested || called.Load() != 1 {
		t.Fatal(v, called.Load())
	}
	if e = a.Cancel(job.ID); e == nil {
		t.Fatal("cancelled terminal task")
	}
}
func TestShutdownDoesNotCompleteOrRollbackActiveTasks(t *testing.T) {
	m := manager(t, filepath.Join(t.TempDir(), "config.yaml"))
	started := make(chan struct{})
	job, e := m.Submit(context.Background(), "lab", "host", "vm.clone", "new", nil, "new", "", func(ctx context.Context, r *Reporter) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if e != nil {
		t.Fatal(e)
	}
	<-started
	m.cancel()
	m.wg.Wait()
	v, _ := m.Get(job.ID)
	if v.Status != "interrupted" {
		t.Fatal(v)
	}
	if _, e = m.Wait(context.Background(), job.ID); e == nil {
		t.Fatal("interrupted reported success")
	}
}
func TestListFiltersPaginationAndLiveOwnerIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	a := manager(t, path)
	b := manager(t, path)
	for _, p := range []string{"lab", "prod", "lab"} {
		job, e := a.Submit(context.Background(), p, "host", "file.copy", p+time.Now().String(), nil, time.Now().String(), "", func(context.Context, *Reporter) (any, error) { return nil, errors.New("fixture failure") })
		if e != nil {
			t.Fatal(e)
		}
		waitState(t, a, job.ID, "failed")
	}
	page, e := b.List(Query{Profile: "lab", Status: "failed", Limit: 1})
	if e != nil || len(page.Items) != 1 || !page.HasMore {
		t.Fatal(page, e)
	}
	page, e = b.List(Query{Profile: "lab", Status: "failed", Limit: 1, Offset: 1})
	if e != nil || len(page.Items) != 1 || page.HasMore {
		t.Fatal(page, e)
	}
}

func TestNestedSourceDestinationLocksAndExpiredOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	a := manager(t, path)
	b := manager(t, path)
	job, e := a.Submit(context.Background(), "lab", "host", "file.copy", "folder", nil, "datastore:ds/folder\x00vm:source", "", func(ctx context.Context, r *Reporter) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	if e != nil {
		t.Fatal(e)
	}
	waitState(t, a, job.ID, "running")
	if _, e = b.Submit(context.Background(), "lab", "host", "file.copy", "child", nil, "datastore:ds/folder/child", "", func(context.Context, *Reporter) (any, error) { return nil, nil }); e == nil {
		t.Fatal("nested destination permitted")
	}
	if _, e = b.Submit(context.Background(), "lab", "host", "vm.clone", "other", nil, "vm:source", "", func(context.Context, *Reporter) (any, error) { return nil, nil }); e == nil {
		t.Fatal("same source permitted")
	}
	if e = b.Cancel(job.ID); e != nil {
		t.Fatal(e)
	}
	waitState(t, a, job.ID, "cancelled")
	dead := Task{ID: "dead-task", Owner: "dead-owner", Profile: "lab", Status: "running", Kind: "vm.clone", CreatedAt: time.Now(), UpdatedAt: time.Now(), Steps: []Step{}}
	record, _ := json.Marshal(dead)
	if _, e = b.db.Exec("INSERT INTO task_owners VALUES (?,?)", dead.Owner, time.Now().Add(-time.Minute).UnixMilli()); e != nil {
		t.Fatal(e)
	}
	if _, e = b.db.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", dead.ID, dead.Profile, dead.Status, dead.Kind, "cli", "dead", dead.Owner, time.Now().UnixMilli(), time.Now().UnixMilli(), "dead-lock", nil, record); e != nil {
		t.Fatal(e)
	}
	waitState(t, b, dead.ID, "interrupted")
}

func TestTaskFinalizationStopsCancellationObserver(t *testing.T) {
	m := manager(t, filepath.Join(t.TempDir(), "config.yaml"))
	job, err := m.Submit(context.Background(), "lab", "https://host", "file.copy", "observer", nil, "observer", "", func(ctx context.Context, r *Reporter) (any, error) {
		timer := time.NewTimer(350 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return "done", nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, m, job.ID, "succeeded")
	time.Sleep(350 * time.Millisecond)
	final, err := m.Get(job.ID)
	if err != nil || final.Status != "succeeded" || final.EndedAt == nil {
		t.Fatalf("observer overwrote final state: %+v %v", final, err)
	}
}
