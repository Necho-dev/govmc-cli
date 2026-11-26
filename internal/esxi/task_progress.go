package esxi

import (
	"context"
	"errors"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"time"
)

type progressKey struct{}

func WithTaskProgress(ctx context.Context, report func(string, *int)) context.Context {
	return context.WithValue(ctx, progressKey{}, report)
}
func waitFileTask(ctx context.Context, task *object.Task) error {
	report, _ := ctx.Value(progressKey{}).(func(string, *int))
	ref := task.Reference().Value
	if report != nil {
		report(ref, nil)
	}
	monitor, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-monitor.Done():
				return
			case <-ticker.C:
				var value mo.Task
				if task.Properties(monitor, task.Reference(), []string{"info"}, &value) == nil && report != nil {
					p := int(value.Info.Progress)
					if p > 0 {
						report(ref, &p)
					}
				}
			}
		}
	}()
	_, err := task.WaitForResult(ctx)
	stop()
	<-done
	if err != nil && ctx.Err() != nil {
		cancel, finish := context.WithTimeout(context.Background(), 10*time.Second)
		defer finish()
		if e := task.Cancel(cancel); e != nil {
			return errors.New("remote_task_outcome_unknown")
		}
		_, e := task.WaitForResult(cancel)
		if e == nil {
			return nil
		}
		return errors.New("remote_task_outcome_unknown")
	}
	if report != nil {
		report("", nil)
	}
	return err
}
