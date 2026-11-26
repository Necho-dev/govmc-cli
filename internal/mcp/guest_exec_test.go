package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestGuestExecGuard(t *testing.T) {
	for _, tc := range []struct {
		allowed    bool
		args, want string
	}{
		{false, `{"id":"vm","program":"whoami.exe","confirm":true}`, "requires an API token"},
		{true, `{"id":"vm","program":"whoami.exe"}`, "confirm_required"},
		{true, `{"id":"vm","program":"whoami.exe","confirm":true,"password":"test"}`, "unknown field"},
		{true, `{"id":"vm","program":"whoami.exe","confirm":true,"timeoutMs":90001}`, "bad_request"},
	} {
		raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + guestExecTool + `","arguments":` + tc.args + `}}`)
		out, err := Handle(context.Background(), nil, raw, tc.allowed)
		if err != nil || !strings.Contains(string(out), tc.want) || !strings.Contains(string(out), `"isError":true`) {
			t.Fatalf("response = %s, err = %v, want %q", out, err, tc.want)
		}
	}
}
