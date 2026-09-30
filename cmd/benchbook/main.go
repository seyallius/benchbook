// Command benchbook records benchmark runs and reproducible milestone history, and
// publishes them as a static documentation site.
//
// It is deliberately thin: parse the process arguments, hand them to the CLI
// package, and turn the returned code into an exit status.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/seyallius/benchbook/internal/cli"
)

// main wires the process signals into a cancellable context, runs one invocation
// and exits with the documented status code (0 success, 1 runtime error, 2 usage or
// configuration error, 3 refused by a safety rule).
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := cli.New(cli.Options{})
	os.Exit(app.Run(ctx, os.Args[1:]))
}
