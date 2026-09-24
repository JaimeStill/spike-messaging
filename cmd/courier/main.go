// Command courier runs the spike's narrated scenarios: the event and reactor
// layer on a broker, one capability per scenario. This file is process entry
// alone: it derives the root context from the interrupt signal, hands the
// process's streams to the composition root, and exits with the code the
// run returns. It imports only internal/app.
package main

import (
	"os"

	"github.com/standards-lab/go-core/process"

	"github.com/JaimeStill/spike-messaging/internal/app"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := process.SignalContext()
	defer stop()
	return app.New(os.Stdout, os.Stderr).Run(ctx)
}
