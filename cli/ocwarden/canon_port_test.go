package main

import (
	"fmt"
	"os"
	"regexp"
	"testing"
)

// TestDefaultBase_TracksCanonicalServerPort is a drift canary: it derives the
// CURRENT canonical port from the single source of truth
// (server/ocserverd/config.go's `defaultPort` const) instead of trusting that
// this package's own defaultBase literal was kept in sync by hand. Mirrors
// the same pattern already landed for e2e_test/lib/oc_lifecycle.sh's
// OC_CANONICAL_SERVE_PORT (T-b76b) and e2e_test/tests_guard/run.sh's
// CANON_PORT (commit 9463d06) — a hand-maintained literal here would just be
// this package's turn to go stale next time the port moves.
//
// Go tests run with cwd = the package directory, so the source file is two
// levels up (cli/ocwarden -> repo root -> server/ocserverd).
func TestDefaultBase_TracksCanonicalServerPort(t *testing.T) {
	const configPath = "../../server/ocserverd/config.go"
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("FATAL: could not read %s — refusing to run without a working canonical-port guard: %v", configPath, err)
	}

	re := regexp.MustCompile(`(?m)^\s*defaultPort\s*=\s*([0-9]+)`)
	m := re.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("FATAL: could not parse defaultPort out of %s — refusing to run without a working canonical-port guard", configPath)
	}
	canonPort := string(m[1])

	want := fmt.Sprintf("http://127.0.0.1:%s", canonPort)
	if defaultBase != want {
		t.Errorf("defaultBase = %q, want %q (derived from %s's defaultPort=%s) — ocwarden's default has drifted from the canonical server port again", defaultBase, want, configPath, canonPort)
	}
}
