package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
)

const guestExecTool = "esxi_vm_guest_exec"

func callGuestExec(ctx context.Context, client *esxi.Client, args map[string]any) ([]map[string]any, bool) {
	var req esxi.GuestExecRequest
	raw, err := json.Marshal(args)
	if err != nil {
		return text(err.Error()), true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return text(err.Error()), true
	}
	if err := req.Validate(); err != nil {
		return guestExecError(esxi.GuestExecResult{}, err), true
	}
	result, err := client.GuestExec(ctx, req)
	if err != nil {
		return guestExecError(result, err), true
	}
	return text(asJSON(result)), false
}

func guestExecError(result esxi.GuestExecResult, err error) []map[string]any {
	code := "guest_exec_failed"
	var api *esxi.Error
	if errors.As(err, &api) {
		code = api.Code
	}
	return text(asJSON(map[string]any{"code": code, "error": err.Error(), "result": result}))
}
