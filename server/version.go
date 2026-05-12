package server

import "strings"

// buildVersion stores the user-visible gateway version string injected by the
// build pipeline through Go linker flags.
var buildVersion = "dev"

// Version returns the normalized build-time version string shown by the CLI,
// startup logs, and Web UI.
func Version() string {
	version := strings.TrimSpace(buildVersion)
	if version == "" {
		return "dev"
	}

	return version
}
