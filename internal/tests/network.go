package tests

import (
	"os"
	"testing"
)

// NetworkEnv gates the tests that query live job boards.
const NetworkEnv = "JHT_NETWORK_TESTS"

// RequireNetwork skips a test unless live-network testing is explicitly enabled.
//
// Tests that reach real job boards are unsuitable as a correctness gate: they
// are slow, they need internet access, and they fail whenever a company changes
// or retires its job board, a fact about the world, not a regression in this
// code. They remain useful as a source-health check, so they are opt-in rather
// than deleted.
//
// Use `job-hunter-toolkit health` for routine source-freshness checks.
func RequireNetwork(t *testing.T) {
	t.Helper()

	if os.Getenv(NetworkEnv) == "" {
		t.Skipf("set %s=1 to run tests that query live job boards", NetworkEnv)
	}
}
