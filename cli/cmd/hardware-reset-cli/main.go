package main

import (
	"context"
	"github.com/guajun/hardware-reset-cli/cli/internal/app"
	"os"
	"os/signal"
)

var version = "0.1.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, version, app.DefaultBackend()))
}
