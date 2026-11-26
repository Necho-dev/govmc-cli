// Package workflow contains device workflows shared by CLI, MCP, TUI and REST.
package workflow

import (
	"context"
	"errors"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"regexp"
	"strings"
	"unicode"
)

func Clone(ctx context.Context, m *tasks.Manager, c *esxi.Client, profile, endpoint string, spec esxi.CloneSpec, key string, release func()) (any, error) {
	if release == nil {
		release = func() {}
	}
	ctx = tasks.WithRequest(ctx, spec)
	if !spec.DryRunValue() {
		if !spec.Confirm {
			release()
			return nil, esxi.RequireConfirm(false)
		}
		if m != nil && key != "" {
			old, found, e := m.Replay(ctx, profile, endpoint, "vm.clone", key)
			if e != nil {
				release()
				return nil, e
			}
			if found {
				release()
				return old, nil
			}
		}
	}
	spec, plan, err := c.PrepareClone(ctx, spec)
	if err != nil {
		release()
		return nil, err
	}
	if spec.DryRunValue() {
		release()
		return plan, nil
	}
	if !spec.Confirm {
		release()
		return nil, esxi.RequireConfirm(false)
	}
	if m == nil {
		release()
		return nil, errors.New("task service unavailable")
	}
	task, err := m.Submit(ctx, profile, endpoint, "vm.clone", spec.Dst, spec, "datastore:"+spec.TargetDatastore()+"/"+spec.Dst+"\x00datastore:"+spec.Datastore+"/"+spec.SrcFolder+"\x00vm:"+spec.Src, key, func(run context.Context, r *tasks.Reporter) (any, error) {
		run = esxi.WithTaskProgress(run, func(ref string, percent *int) { r.Remote(ref, nil) })
		result, err := c.ExecuteClone(run, spec, func(op, status, message string) { r.Step(op, status) })
		return result, publicFailure(c, err)
	}, release)
	if err != nil {
		release()
	}
	return task, err
}
func CopyMove(ctx context.Context, m *tasks.Manager, c *esxi.Client, profile, endpoint, ds, src, dst string, copy, dry, confirm bool, key string, release func(), targetDatastore ...string) (any, error) {
	if release == nil {
		release = func() {}
	}

	src, dst, err := esxi.NormalizeTransferPaths(src, dst, true)
	if err != nil {
		release()
		return nil, err
	}
	store, err := c.Finder.Datastore(ctx, ds)
	if err != nil {
		release()
		return nil, err
	}
	ds = store.Name()
	dstDS := ds
	if len(targetDatastore) > 0 && targetDatastore[0] != "" {
		dstDS = targetDatastore[0]
	}
	destination, err := c.Finder.Datastore(ctx, dstDS)
	if err != nil {
		release()
		return nil, err
	}
	dstDS = destination.Name()
	src, dst, err = esxi.NormalizeTransferPaths(src, dst, ds != dstDS)
	if err != nil {
		release()
		return nil, err
	}
	kind := "file.move"
	if copy {
		kind = "file.copy"
	}
	params := map[string]string{"datastore": ds, "src": src, "dst": dst}
	if ds != dstDS {
		params["dstDatastore"] = dstDS
	}
	ctx = tasks.WithRequest(ctx, params)
	if !dry {
		if !confirm {
			release()
			return nil, esxi.RequireConfirm(false)
		}
		if m == nil {
			release()
			return nil, errors.New("task service unavailable")
		}
		if key != "" {
			old, found, e := m.Replay(ctx, profile, endpoint, kind, key)
			if e != nil {
				release()
				return nil, e
			}
			if found {
				release()
				return old, nil
			}
		}
	}
	plan, err := c.PrepareFileTransfer(ctx, ds, src, dst, copy, dstDS)
	if err == nil && m != nil {
		err = m.EnsureIdle(ctx, endpoint, "datastore:"+ds+"/"+src, "datastore:"+dstDS+"/"+dst)
	}
	if err != nil || dry {
		release()
		return plan, err
	}
	task, err := m.Submit(ctx, profile, endpoint, kind, dst, params, "datastore:"+dstDS+"/"+dst+"\x00datastore:"+ds+"/"+src, key, func(run context.Context, r *tasks.Reporter) (any, error) {
		run = esxi.WithTaskProgress(run, r.Remote)
		r.Step(kind, "running")
		if _, e := c.PrepareFileTransfer(run, ds, src, dst, copy, dstDS); e != nil {
			r.Step(kind, "error")
			return nil, publicFailure(c, e)
		}
		out, e := c.FSTransfer(run, ds, dstDS, src, dst, copy)
		status := "ok"
		if e != nil {
			status = "error"
		}
		r.Step(kind, status)
		return out, publicFailure(c, e)
	}, release)
	if err != nil {
		release()
	}
	return task, err
}

type publicError struct {
	error
	code, message string
}

func (e *publicError) Unwrap() error                 { return e.error }
func (e *publicError) TaskFailure() (string, string) { return e.code, e.message }
func publicFailure(c *esxi.Client, err error) error {
	if err == nil {
		return nil
	}
	var api *esxi.Error
	if !errors.As(err, &api) {
		return err
	}
	message := api.Message
	if c.Reach != nil && c.Reach.User != nil {
		if password, ok := c.Reach.User.Password(); ok && password != "" {
			message = strings.ReplaceAll(message, password, "[redacted]")
		}
	}
	message = regexp.MustCompile(`https?://[^/@\s]+@`).ReplaceAllString(message, "https://[redacted]@")
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	if len(message) > 512 {
		message = message[:512]
	}
	return &publicError{error: err, code: api.Code, message: message}
}
