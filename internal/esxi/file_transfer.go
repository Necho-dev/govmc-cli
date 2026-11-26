package esxi

import (
	"context"
	"errors"
	"fmt"
	"github.com/vmware/govmomi/vim25/mo"
	"path"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// FileTransferPlan is the same read-only preview on every frontend.
// Destination is an exact datastore-relative path, never an overwrite or a directory hint.
type FileTransferPlan struct {
	Datastore            string `json:"datastore"`
	DestinationDatastore string `json:"dstDatastore"`
	Source               string `json:"src"`
	Destination          string `json:"dst"`
	Folder               bool   `json:"folder"`
	SizeBytes            int64  `json:"sizeBytes"`
	Overwrite            bool   `json:"overwrite"`
}

func NormalizeTransferPaths(src, dst string, across ...bool) (string, string, error) {
	var err error
	src, err = CleanRel(src)
	if err != nil {
		return "", "", err
	}
	dst, err = CleanRel(dst)
	if err != nil {
		return "", "", err
	}
	if src == "" || dst == "" || path.IsAbs(src) || path.IsAbs(dst) {
		return "", "", badRequest("src and dst are required")
	}
	src, dst = path.Clean(src), path.Clean(dst)
	if src == "." || dst == "." || (len(across) == 0 || !across[0]) && (src == dst || len(dst) > len(src) && dst[:len(src)+1] == src+"/") {
		return "", "", badRequest("destination must differ from source and must not be inside it")
	}
	return src, dst, nil
}

func (c *Client) PrepareFileTransfer(ctx context.Context, dsName, src, dst string, copy bool, targetDatastore ...string) (*StepResult, error) {
	src, dst, err := NormalizeTransferPaths(src, dst, true)
	if err != nil {
		return nil, err
	}
	ds, err := c.Finder.Datastore(ctx, dsName)
	if err != nil {
		return nil, wrapVim(err)
	}
	destinationName := dsName
	if len(targetDatastore) > 0 && targetDatastore[0] != "" {
		destinationName = targetDatastore[0]
	}
	destination, err := c.Finder.Datastore(ctx, destinationName)
	if err != nil {
		return nil, wrapVim(err)
	}
	src, dst, err = NormalizeTransferPaths(src, dst, ds.Name() != destination.Name())
	if err != nil {
		return nil, err
	}
	source, err := ds.Stat(ctx, src)
	if err != nil {
		var f object.DatastoreNoSuchFileError
		var d object.DatastoreNoSuchDirectoryError
		if errors.As(err, &f) || errors.As(err, &d) {
			return nil, notFound("source path does not exist")
		}
		return nil, wrapVim(err)
	}
	if _, err = destination.Stat(ctx, dst); err == nil {
		return nil, conflict("destination already exists")
	} else {
		var missing object.DatastoreNoSuchFileError
		var missingParent object.DatastoreNoSuchDirectoryError
		if errors.As(err, &missingParent) {
			return nil, badRequest("destination parent does not exist")
		}
		if !errors.As(err, &missing) {
			return nil, wrapVim(err)
		}
	}
	parent := path.Dir(dst)
	if parent != "." {
		info, e := destination.Stat(ctx, parent)
		if e != nil {
			return nil, wrapVim(e)
		}
		if _, ok := info.(*types.FolderFileInfo); !ok {
			return nil, badRequest("destination parent must be a directory")
		}
	}
	_, folder := source.(*types.FolderFileInfo)
	kind := "copy"
	if !copy {
		kind = "move"
	}
	var info mo.Datastore
	if err = destination.Properties(ctx, destination.Reference(), []string{"summary"}, &info); err != nil {
		return nil, wrapVim(err)
	}
	if !info.Summary.Accessible {
		return nil, conflict("destination datastore is inaccessible")
	}
	if !folder && (copy || ds.Name() != destination.Name()) && source.GetFileInfo().FileSize > info.Summary.FreeSpace {
		return nil, conflict("destination datastore has insufficient free space")
	}
	plan := FileTransferPlan{Datastore: ds.Name(), DestinationDatastore: destination.Name(), Source: src, Destination: dst, Folder: folder, SizeBytes: source.GetFileInfo().FileSize, Overwrite: false}
	return &StepResult{Op: kind, DryRun: true, Message: fmt.Sprintf("%s [%s] %s -> [%s] %s", kind, plan.Datastore, src, plan.DestinationDatastore, dst), Detail: plan}, nil
}

// FSTransfer uses VMware's native task with explicit source and destination stores.
func (c *Client) FSTransfer(ctx context.Context, ds, target, src, dst string, copy bool) (*StepResult, error) {
	if ds == target {
		if copy {
			return c.FSCopy(ctx, ds, src, dst, true, false)
		}
		return c.FSMove(ctx, ds, src, dst, true, false)
	}
	if err := c.guardFS(ctx, ds, src); err != nil {
		return nil, err
	}
	if err := c.guardFS(ctx, target, dst); err != nil {
		return nil, err
	}
	source, err := c.Finder.Datastore(ctx, ds)
	if err != nil {
		return nil, wrapVim(err)
	}
	destination, err := c.Finder.Datastore(ctx, target)
	if err != nil {
		return nil, wrapVim(err)
	}
	dc, err := c.Finder.DefaultDatacenter(ctx)
	if err != nil {
		return nil, wrapVim(err)
	}
	fm := object.NewFileManager(c.Gov.Client)
	var task *object.Task
	kind := "move"
	if copy {
		kind = "copy"
		task, err = fm.CopyDatastoreFile(ctx, source.Path(src), dc, destination.Path(dst), dc, false)
	} else {
		task, err = fm.MoveDatastoreFile(ctx, source.Path(src), dc, destination.Path(dst), dc, false)
	}
	if err == nil {
		err = waitFileTask(ctx, task)
	}
	if err != nil {
		return nil, wrapVim(err)
	}
	return &StepResult{Op: kind, Message: fmt.Sprintf("%s [%s] %s -> [%s] %s", kind, ds, src, target, dst)}, nil
}
