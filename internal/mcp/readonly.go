package mcp

import (
	"context"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
)

type Connect func(context.Context, string) (*esxi.Client, string, func(), error)

func readonlyTools() []map[string]any { return allowedTools(true) }

func readonlyTool(name string) bool {
	switch name {
	case "esxi_logs_list", "esxi_audits_list", "esxi_profiles_list", "esxi_profiles_current", "esxi_config_show", "esxi_doctor", "esxi_vm_list", "esxi_vm_get", "esxi_metrics_query", "esxi_vm_screenshot", "esxi_host_get", "esxi_ds_ls", "esxi_tasks_list", "esxi_task_status":
		return true
	}
	return false
}

func HandleReadonly(ctx context.Context, raw []byte, connect Connect) ([]byte, error) {
	return HandleConfigured(ctx, raw, connect, Readonly)
}
