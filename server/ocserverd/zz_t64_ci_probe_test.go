package main

// T-64 CI PROBE — TEMPORARY, DO NOT MERGE.
// Measures what a `go test` run can actually see of origin/main, in CI and
// locally. It FAILS ON PURPOSE so its output is printed without -v.

import (
	"os/exec"
	"strings"
	"testing"
)

func t64run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return "rc_err=" + errStr(err) + " out=" + s
}

func errStr(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func TestT64CIProbeWhatCanWeSeeOfOriginMain(t *testing.T) {
	t.Logf("PROBE 1 is-inside-work-tree: %s", t64run(t, "git", "rev-parse", "--is-inside-work-tree"))
	t.Logf("PROBE 2 is-shallow: %s", t64run(t, "git", "rev-parse", "--is-shallow-repository"))
	t.Logf("PROBE 3 rev-parse origin/main: %s", t64run(t, "git", "rev-parse", "origin/main"))
	t.Logf("PROBE 4 remote refs: %s", t64run(t, "git", "for-each-ref", "--format=%(refname)", "refs/remotes"))
	t.Logf("PROBE 5 ls-tree origin/main migrations: %s", t64run(t, "git", "ls-tree", "--full-tree", "--name-only", "origin/main", "server/ocserverd/migrations/"))
	t.Logf("PROBE 6 fetch depth1 origin main: %s", t64run(t, "git", "fetch", "--depth=1", "origin", "main"))
	t.Logf("PROBE 7 after fetch, rev-parse FETCH_HEAD: %s", t64run(t, "git", "rev-parse", "FETCH_HEAD"))
	t.Logf("PROBE 8 after fetch, ls-tree FETCH_HEAD migrations: %s", t64run(t, "git", "ls-tree", "--full-tree", "--name-only", "FETCH_HEAD", "server/ocserverd/migrations/"))
	t.Logf("PROBE 9 git version: %s", t64run(t, "git", "--version"))
	t.Fatal("T-64 probe: failing on purpose so the log above is printed")
}
