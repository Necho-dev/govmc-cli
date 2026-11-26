package esxi

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/vmware/govmomi/guest"
	"github.com/vmware/govmomi/guest/toolbox"
	"github.com/vmware/govmomi/vim25/types"
)

type GuestExecRequest struct {
	ID         string   `json:"id"`
	Program    string   `json:"program"`
	Args       []string `json:"args"`
	WorkingDir string   `json:"workingDir"`
	TimeoutMS  int      `json:"timeoutMs"`
	Confirm    bool     `json:"confirm"`
}

type GuestExecResult struct {
	VMID      string `json:"vmId,omitempty"`
	PID       int64  `json:"pid,omitempty"`
	ExitCode  *int32 `json:"exitCode,omitempty"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
}

func guestExecReady(power types.VirtualMachinePowerState, info *types.GuestInfo) error {
	switch {
	case power != types.VirtualMachinePowerStatePoweredOn:
		return errf(http.StatusConflict, "guest_vm_not_powered_on", "VM is not powered on")
	case info != nil && info.ToolsStatus == types.VirtualMachineToolsStatusToolsNotInstalled:
		return errf(http.StatusConflict, "vmware_tools_not_installed", "ESXi reports that VMware Tools is not installed; guest execution is unavailable")
	case info != nil && (info.ToolsStatus == types.VirtualMachineToolsStatusToolsNotRunning ||
		info.ToolsRunningStatus == string(types.VirtualMachineToolsRunningStatusGuestToolsNotRunning)):
		return errf(http.StatusConflict, "vmware_tools_not_running", "VMware Tools is not running; guest execution is unavailable")
	case info == nil || info.GuestOperationsReady == nil || !*info.GuestOperationsReady:
		return errf(http.StatusConflict, "guest_operations_not_ready", "Guest Operations is not ready; check VMware Tools and its Guest Operations service")
	}
	return nil
}

func (r GuestExecRequest) Validate() error {
	if !r.Confirm {
		return forbidden("guest execution requires confirm=true")
	}
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Program) == "" {
		return badRequest("id and program are required")
	}
	if r.TimeoutMS < 0 || r.TimeoutMS > 90000 {
		return badRequest("timeoutMs must be 0–90000 (0 uses the 30000 ms default)")
	}
	size := 0
	for _, value := range append([]string{r.Program, r.WorkingDir}, r.Args...) {
		if strings.ContainsRune(value, 0) {
			return badRequest("program, args and workingDir cannot contain NUL")
		}
		size += len(value)
	}
	if size > 3000 || len(r.Args) > 128 {
		return badRequest("program, args and workingDir exceed the Windows command size limit")
	}
	return nil
}

func (c *Client) GuestExec(ctx context.Context, req GuestExecRequest) (GuestExecResult, error) {
	var result GuestExecResult
	if err := req.Validate(); err != nil {
		return result, err
	}
	timeout := req.TimeoutMS
	if timeout == 0 {
		timeout = 30000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	vm, err := c.GetVM(ctx, req.ID)
	if err != nil {
		return result, err
	}
	result.VMID = vm.Info.ID
	if err := guestExecReady(vm.Mo.Runtime.PowerState, vm.Mo.Guest); err != nil {
		return result, err
	}
	if !strings.HasPrefix(vm.Info.GuestID, "windows") &&
		vm.Mo.Guest.GuestFamily != string(types.VirtualMachineGuestOsFamilyWindowsGuest) {
		return result, badRequest("VMware Tools guest execution currently supports Windows only")
	}
	targetID := normalizeMoRef(os.Getenv("GUEST_EXEC_VM_ID"))
	targetUUID := strings.TrimSpace(os.Getenv("GUEST_EXEC_VM_BIOS_UUID"))
	if targetID == "" || targetID != vm.Info.ID || !eqFold(targetUUID, vm.Info.BiosUUID) {
		return result, errf(http.StatusForbidden, "guest_exec_target_not_configured", "guest execution is not configured for this VM; check GUEST_EXEC_VM_ID and GUEST_EXEC_VM_BIOS_UUID")
	}
	user, password := os.Getenv("GUEST_EXEC_USERNAME"), os.Getenv("GUEST_EXEC_PASSWORD")
	if user == "" || password == "" {
		return result, errf(http.StatusServiceUnavailable, "guest_exec_credentials_missing", "configure GUEST_EXEC_USERNAME and GUEST_EXEC_PASSWORD on the MCP server")
	}
	if c.Gov.ServiceContent.GuestOperationsManager == nil {
		return result, errf(http.StatusServiceUnavailable, "guest_operations_unavailable", "this ESXi connection does not expose Guest Operations")
	}
	auth := &types.NamePasswordAuthentication{Username: user, Password: password}
	manager := guest.NewOperationsManager(c.Gov.Client, vm.Obj.Reference())
	pm, err := manager.ProcessManager(ctx)
	if err != nil {
		return result, guestExecFault(ctx, err, "get process manager")
	}
	fm, err := manager.FileManager(ctx)
	if err != nil {
		return result, guestExecFault(ctx, err, "get file manager")
	}
	env, err := pm.ReadEnvironmentVariable(ctx, auth, []string{"SystemRoot"})
	if err != nil {
		return result, guestExecFault(ctx, err, "read Windows system directory")
	}
	root := ""
	for _, value := range env {
		key, path, _ := strings.Cut(value, "=")
		if strings.EqualFold(key, "SystemRoot") {
			root = strings.TrimRight(path, "\\")
		}
	}
	if root == "" {
		return result, errf(http.StatusBadGateway, "guest_exec_system_root_missing", "Windows did not report SystemRoot")
	}

	dir, err := fm.CreateTemporaryDirectory(ctx, auth, "govmc-exec-", "", "")
	if err != nil {
		return result, guestExecFault(ctx, err, "create guest output directory")
	}
	cleanup := true
	defer func() {
		if cleanup {
			cleanCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer stop()
			_ = fm.DeleteDirectory(cleanCtx, auth, dir, true)
		}
	}()

	encoded := guestExecScript(req, dir)
	if len(encoded) > 30000 {
		return result, badRequest("encoded Windows command exceeds the command-line size limit")
	}
	pid, err := pm.StartProgram(ctx, auth, &types.GuestProgramSpec{
		ProgramPath: root + `\System32\WindowsPowerShell\v1.0\powershell.exe`,
		Arguments:   "-NoProfile -NonInteractive -EncodedCommand " + encoded,
	})
	if err != nil {
		cleanup = false // A lost response can follow a successful start.
		var vimErr *Error
		if errors.As(wrapVim(err), &vimErr) && vimErr.VimFault != "" {
			cleanup = true
			return result, guestExecFault(ctx, err, "start Windows runner")
		}
		return result, guestExecFault(ctx, err, "start Windows runner; execution status unknown, do not retry automatically")
	}
	result.PID = pid
	cleanup = false
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		processes, err := pm.ListProcesses(ctx, auth, []int64{pid})
		if err != nil {
			return result, guestExecFault(ctx, err, "query Windows runner; execution status unknown, do not retry automatically")
		}
		if len(processes) == 0 {
			return result, errf(http.StatusBadGateway, "guest_exec_status_unknown", "Windows runner disappeared; execution status unknown, do not retry automatically")
		}
		if processes[0].EndTime != nil {
			exitCode := processes[0].ExitCode
			result.ExitCode = &exitCode
			cleanup = true
			break
		}
		select {
		case <-ctx.Done():
			return result, guestExecFault(ctx, ctx.Err(), "wait for Windows runner; execution status unknown, do not retry automatically")
		case <-ticker.C:
		}
	}

	transfer := toolbox.Client{ProcessManager: pm, FileManager: fm, Authentication: auth}
	status, _, err := guestExecRead(ctx, transfer, dir+`\status`, 4096)
	if err != nil {
		return result, guestExecFault(ctx, err, "read runner status; command may already have executed")
	}
	if status != "completed" {
		result.ExitCode = nil
		return result, errf(http.StatusBadGateway, "guest_exec_launch_failed", "Windows runner did not complete the requested program: %s", status)
	}
	var stdoutCut, stderrCut bool
	result.Stdout, stdoutCut, err = guestExecRead(ctx, transfer, dir+`\stdout`, 64<<10)
	if err != nil {
		return result, guestExecFault(ctx, err, "read stdout; command already finished")
	}
	result.Stderr, stderrCut, err = guestExecRead(ctx, transfer, dir+`\stderr`, 64<<10)
	result.Truncated = stdoutCut || stderrCut
	if err != nil {
		return result, guestExecFault(ctx, err, "read stderr; command already finished")
	}
	return result, nil
}

func guestExecRead(ctx context.Context, client toolbox.Client, path string, limit int64) (string, bool, error) {
	r, _, err := client.Download(ctx, path)
	if err != nil {
		return "", false, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(data)) > limit {
		return string(data[:limit]), true, err
	}
	return string(data), false, err
}

func guestExecFault(ctx context.Context, err error, stage string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errf(http.StatusGatewayTimeout, "guest_exec_timeout", "%s: timeout", stage)
	}
	var fault *Error
	if errors.As(wrapVim(err), &fault) {
		switch fault.VimFault {
		case "*types.GuestOperationsUnavailable", "*types.GuestComponentsOutOfDate", "*types.OperationDisabledByGuest":
			return errf(http.StatusConflict, "guest_operations_unavailable", "%s: check that VMware Tools is running, up to date and allows Guest Operations", stage)
		case "*types.InvalidGuestLogin":
			return errf(http.StatusForbidden, "guest_exec_invalid_login", "%s: Windows rejected the configured guest credentials", stage)
		}
		if fault.VimFault != "" {
			return errf(http.StatusBadGateway, "guest_exec_failed", "%s: %s", stage, fault.VimFault)
		}
	}
	// File transfer errors can include a temporary URL with an auth ticket.
	return errf(http.StatusBadGateway, "guest_exec_failed", "%s failed", stage)
}

func guestExecScript(req GuestExecRequest, dir string) string {
	args := make([]string, len(req.Args))
	for i, arg := range req.Args {
		args[i] = quoteWindowsArg(arg)
	}
	data, _ := json.Marshal(map[string]string{
		"program": req.Program, "args": strings.Join(args, " "), "dir": dir, "workingDir": req.WorkingDir,
	})
	script := `$ErrorActionPreference = 'Stop'
$d = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + base64.StdEncoding.EncodeToString(data) + `')) | ConvertFrom-Json
$status = Join-Path $d.dir 'status'
try {
    $options = @{
        FilePath = $d.program
        RedirectStandardOutput = (Join-Path $d.dir 'stdout')
        RedirectStandardError = (Join-Path $d.dir 'stderr')
        NoNewWindow = $true
        PassThru = $true
        Wait = $true
    }
    if ($d.args) { $options.ArgumentList = $d.args }
    if ($d.workingDir) { $options.WorkingDirectory = $d.workingDir }
    $process = Start-Process @options
    [IO.File]::WriteAllText($status, 'completed')
    exit $process.ExitCode
} catch {
    [IO.File]::WriteAllText($status, $_.Exception.Message)
    exit 125
}`
	encoded := utf16.Encode([]rune(script))
	bytes := make([]byte, 2*len(encoded))
	for i, unit := range encoded {
		binary.LittleEndian.PutUint16(bytes[2*i:], unit)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

// quoteWindowsArg follows the Windows CRT rules for quotes and backslashes.
func quoteWindowsArg(arg string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, ch := range arg {
		if ch == '\\' {
			slashes++
			continue
		}
		if ch == '"' {
			slashes = 2*slashes + 1
		}
		b.WriteString(strings.Repeat("\\", slashes))
		b.WriteRune(ch)
		slashes = 0
	}
	b.WriteString(strings.Repeat("\\", 2*slashes))
	b.WriteByte('"')
	return b.String()
}
