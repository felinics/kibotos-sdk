package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/felinics/kibotos-sdk/cli/internal/cli"
)

var (
	version = "0.1.0"
	commit  = ""
	date    = ""
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	code := cli.Execute(ctx, os.Args[1:], cli.Options{
		Build: cli.BuildInfo{
			Version: version,
			Commit:  commit,
			Date:    date,
		},
	})
	os.Exit(code)
}
