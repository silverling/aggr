package server

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// cliOptions stores the parsed command-line mode selected by the caller.
type cliOptions struct {
	// ShowHelp reports whether the caller requested usage text and an immediate exit.
	ShowHelp bool
	// ShowVersion reports whether the caller requested the build version and an immediate exit.
	ShowVersion bool
	// Upgrade reports whether the caller requested an in-place binary self-upgrade.
	Upgrade bool
}

// RunCLI parses the supported command-line flags, writes the requested output,
// and starts the HTTP server when no immediate-exit flag is present.
func RunCLI(args []string, stdout io.Writer, stderr io.Writer) error {
	options, err := parseCLIFlags(args)
	if err != nil {
		writeCLIUsage(stderr)
		return fmt.Errorf("parse CLI flags: %w", err)
	}

	switch {
	case options.ShowHelp:
		writeCLIUsage(stdout)
		return nil
	case options.ShowVersion:
		return writeCLIVersion(stdout)
	case options.Upgrade:
		return runSelfUpgrade(stdout)
	default:
		return Run()
	}
}

// parseCLIFlags parses the supported command-line flags and rejects unexpected
// positional arguments so startup behavior stays explicit.
func parseCLIFlags(args []string) (cliOptions, error) {
	parser := flag.NewFlagSet("aggr", flag.ContinueOnError)
	parser.SetOutput(io.Discard)

	showHelp := parser.Bool("help", false, "Show usage and exit.")
	parser.BoolVar(showHelp, "h", false, "Show usage and exit.")
	showVersion := parser.Bool("version", false, "Show version and exit.")

	if err := parser.Parse(args); err != nil {
		return cliOptions{}, err
	}

	options := cliOptions{
		ShowHelp:    *showHelp,
		ShowVersion: *showVersion,
	}

	switch parser.NArg() {
	case 0:
		return options, nil
	case 1:
		if parser.Arg(0) != "upgrade" {
			return cliOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(parser.Args(), " "))
		}

		options.Upgrade = true
		return options, nil
	default:
		return cliOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(parser.Args(), " "))
	}
}

// writeCLIUsage writes the gateway command usage summary to the provided
// writer so help output stays consistent across the binary entrypoints.
func writeCLIUsage(writer io.Writer) {
	if writer == nil {
		return
	}

	_, _ = fmt.Fprintf(
		writer,
		"Usage: aggr [--help] [--version] [upgrade]\n\nOptions:\n  --help     Show usage and exit.\n  --version  Show version and exit.\n  upgrade    Download the latest GitHub release and replace this binary.\n\nEnvironment:\n  AGGR_ACCESS_KEY   Shared access key required for Web UI and /api access.\n  AGGR_ADDR         HTTP listen address. Default: :8080\n  AGGR_DB_PATH      SQLite database path. Default: aggr.db\n  AGGR_ENV          Runtime environment label. Default: prod\n  AGGR_GITHUB_REPO  Optional release repository override for upgrade. Default: silverling/aggr\n\nConfiguration:\n  Environment variables can be set in the shell or in a local .env file.\n  Example:\n    AGGR_ACCESS_KEY=change-me\n    AGGR_ADDR=:8080\n    AGGR_DB_PATH=aggr.db\n",
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
