package server

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// RunCLI parses the supported command-line flags, writes the requested output,
// and starts the HTTP server when no immediate-exit flag is present.
func RunCLI(args []string, stdout io.Writer, stderr io.Writer) error {
	showHelp, showVersion, err := parseCLIFlags(args)
	if err != nil {
		writeCLIUsage(stderr)
		return fmt.Errorf("parse CLI flags: %w", err)
	}

	switch {
	case showHelp:
		writeCLIUsage(stdout)
		return nil
	case showVersion:
		return writeCLIVersion(stdout)
	default:
		return Run()
	}
}

// parseCLIFlags parses the supported command-line flags and rejects unexpected
// positional arguments so startup behavior stays explicit.
func parseCLIFlags(args []string) (bool, bool, error) {
	parser := flag.NewFlagSet("aggr", flag.ContinueOnError)
	parser.SetOutput(io.Discard)

	showHelp := parser.Bool("help", false, "Show usage and exit.")
	parser.BoolVar(showHelp, "h", false, "Show usage and exit.")
	showVersion := parser.Bool("version", false, "Show version and exit.")

	if err := parser.Parse(args); err != nil {
		return false, false, err
	}

	if parser.NArg() > 0 {
		return false, false, fmt.Errorf("unexpected positional arguments: %s", strings.Join(parser.Args(), " "))
	}

	return *showHelp, *showVersion, nil
}

// writeCLIUsage writes the gateway command usage summary to the provided
// writer so help output stays consistent across the binary entrypoints.
func writeCLIUsage(writer io.Writer) {
	if writer == nil {
		return
	}

	_, _ = fmt.Fprintf(
		writer,
		"Usage: aggr [--help] [--version]\n\nOptions:\n  --help     Show usage and exit.\n  --version  Show version and exit.\n",
	)
}

// writeCLIVersion writes the normalized build-time version string to the
// provided writer when version output is requested.
func writeCLIVersion(writer io.Writer) error {
	if writer == nil {
		return nil
	}

	_, err := fmt.Fprintln(writer, Version())
	return err
}
