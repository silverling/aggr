package server_test

import (
	"bytes"
	"strings"
	"testing"

	gatewayserver "github.com/silverling/aggr/server"
)

// TestCLIHelpFlag verifies that the gateway prints usage text and exits
// cleanly without attempting to start the HTTP server.
func TestCLIHelpFlag(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := gatewayserver.RunCLI([]string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("RunCLI(--help) returned error: %v", err)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "Usage: aggr") {
		t.Fatalf("stdout = %q, want usage header", output)
	}
	if !strings.Contains(output, "--version") {
		t.Fatalf("stdout = %q, want version flag description", output)
	}
	if !strings.Contains(output, "upgrade") {
		t.Fatalf("stdout = %q, want upgrade command guidance", output)
	}
	if !strings.Contains(output, "AGGR_ACCESS_KEY") {
		t.Fatalf("stdout = %q, want environment variable guidance", output)
	}
	if !strings.Contains(output, "AGGR_GITHUB_REPO") {
		t.Fatalf("stdout = %q, want GitHub repository override guidance", output)
	}
	if !strings.Contains(output, ".env file") {
		t.Fatalf("stdout = %q, want .env guidance", output)
	}
}

// TestCLIVersionFlag verifies that the gateway prints the current build-time
// version string and exits cleanly.
func TestCLIVersionFlag(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := gatewayserver.RunCLI([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("RunCLI(--version) returned error: %v", err)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	output := strings.TrimSpace(stdout.String())
	if output != gatewayserver.Version() {
		t.Fatalf("stdout = %q, want %q", output, gatewayserver.Version())
	}
}

// TestCLIRejectsUnexpectedArguments verifies that unsupported positional
// arguments are rejected with a usage-guided error.
func TestCLIRejectsUnexpectedArguments(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := gatewayserver.RunCLI([]string{"unexpected"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("RunCLI(unexpected) returned nil, want error")
	}

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}

	output := stderr.String()
	if !strings.Contains(output, "Usage: aggr") {
		t.Fatalf("stderr = %q, want usage header", output)
	}
}
