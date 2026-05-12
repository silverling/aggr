package main

import (
	"log/slog"
	"os"

	"github.com/silverling/aggr/server"
)

// main delegates to the server package and reports any startup failure to stderr.
func main() {
	if err := server.RunCLI(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		logger.Error("aggr exited with error", "error", err)
		os.Exit(1)
	}
}
