package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Necho-dev/govmc-cli/internal/command"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := command.New(os.Args[1:]...).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "govmc:", err)
		os.Exit(1)
	}
}
