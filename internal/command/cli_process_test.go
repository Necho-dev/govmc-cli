package command

import (
	"fmt"
	"os"
	"testing"
)

// Exercise the real CLI and stdio MCP in separate OS processes with isolated config.
func TestTaskQueryHelperProcess(t *testing.T) {
	if os.Getenv("GOVMC_TASK_QUERY_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	cmd := New(args...)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
