package esxi

import (
	"errors"
	"testing"

	"github.com/vmware/govmomi/vim25/types"
)

func TestGuestExecReadiness(t *testing.T) {
	on := types.VirtualMachinePowerStatePoweredOn
	missing := &types.GuestInfo{ToolsStatus: types.VirtualMachineToolsStatusToolsNotInstalled}
	ready := true
	notReady := false
	for _, tc := range []struct {
		power types.VirtualMachinePowerState
		info  *types.GuestInfo
		code  string
	}{
		{types.VirtualMachinePowerStatePoweredOff, missing, "guest_vm_not_powered_on"},
		{on, missing, "vmware_tools_not_installed"},
		{on, &types.GuestInfo{ToolsRunningStatus: "guestToolsNotRunning"}, "vmware_tools_not_running"},
		{on, nil, "guest_operations_not_ready"},
		{on, &types.GuestInfo{GuestOperationsReady: &notReady}, "guest_operations_not_ready"},
		{on, &types.GuestInfo{GuestOperationsReady: &ready}, ""},
	} {
		err := guestExecReady(tc.power, tc.info)
		if tc.code == "" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		var api *Error
		if !errors.As(err, &api) || api.Code != tc.code {
			t.Errorf("got %v, want %s", err, tc.code)
		}
	}
}

func TestQuoteWindowsArg(t *testing.T) {
	for _, tc := range []struct{ arg, want string }{
		{``, `""`},
		{`a b`, `"a b"`},
		{`a"b`, `"a\"b"`},
		{`a\"b`, `"a\\\"b"`},
		{`C:\a b\`, `"C:\a b\\"`},
	} {
		if got := quoteWindowsArg(tc.arg); got != tc.want {
			t.Errorf("quoteWindowsArg(%q) = %q, want %q", tc.arg, got, tc.want)
		}
	}
}
