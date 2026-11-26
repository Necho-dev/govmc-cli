package esxi

import (
	"context"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/vmware/govmomi/object"
	"path"
)

func (c *Client) guardVM(ctx context.Context, vm *ResolvedVM) error {
	manager := tasks.From(ctx)
	if manager == nil {
		return nil
	}
	resources := []string{"vm:" + vm.Info.ID}
	for _, disk := range vm.Info.Disks {
		var backing object.DatastorePath
		if backing.FromString(disk.FileName) {
			resources = append(resources, "datastore:"+backing.Datastore+"/"+path.Dir(backing.Path))
		}
	}
	if e := manager.EnsureIdle(ctx, c.Endpoint(), resources...); e != nil {
		return conflict("%s", e)
	}
	return nil
}
func (c *Client) guardFS(ctx context.Context, ds string, paths ...string) error {
	manager := tasks.From(ctx)
	if manager == nil {
		return nil
	}
	store, err := c.Finder.Datastore(ctx, ds)
	if err != nil {
		return wrapVim(err)
	}
	ds = store.Name()
	resources := make([]string, 0, len(paths))
	for _, p := range paths {
		clean, e := CleanRel(p)
		if e != nil {
			return e
		}
		resources = append(resources, "datastore:"+ds+"/"+clean)
	}
	if e := manager.EnsureIdle(ctx, c.Endpoint(), resources...); e != nil {
		return conflict("%s", e)
	}
	return nil
}
