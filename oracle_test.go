package espree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// espreeOracleDir is the vendored npm espree the parity tests compare against.
var espreeOracleDir = filepath.Join("original", "node_modules", "espree")

// requireOracle skips — never fails — a parity test when node or the vendored
// npm oracle is unavailable. The oracle is a test-time dependency and is not
// committed; CI installs it (`npm ci` in original/) before running the suite, so
// a skip here reads as "not verified in this environment", never as a pass.
func requireOracle(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available: parity checks need the npm oracle")
	}
	if _, err := os.Stat(espreeOracleDir); err != nil {
		t.Skipf("%s missing: run `npm ci` in original/ to compare against real espree", espreeOracleDir)
	}
}
