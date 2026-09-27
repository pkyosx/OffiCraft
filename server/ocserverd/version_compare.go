package main

// version_compare.go — semver ordering for the update check (owner-ruled, T-9374).
// update_available is true iff the newest admissible tag is STRICTLY newer than the
// running version; mere string inequality once auto-downgraded an armed
// `updater.auto_update` onto an OLDER tag whenever the cached "latest" lagged. The
// self-build's "0.0.0" (server.go) sorts below any release, so it still prompts.
// An unparseable side answers false plus a warning: an unorderable tag must NEVER
// trigger a download/swap.

import (
	"log"

	"golang.org/x/mod/semver"
)

func canonicalSemver(label string) (string, bool) {
	if label == "" {
		return "", false
	}
	v := label
	if v[0] != 'v' {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", false
	}
	return v, true
}

func releaseIsNewer(candidateTag, running string) bool {
	c, okC := canonicalSemver(candidateTag)
	r, okR := canonicalSemver(running)
	if !okC || !okR {
		log.Printf("[update-check] warning: cannot order versions (latest %q vs running %q) — reporting no update", candidateTag, running)
		return false
	}
	return semver.Compare(c, r) > 0
}

// semverOutranks (the release-list walk) differs from releaseIsNewer on purpose:
// it is SILENT (one non-semver tag would otherwise warn on every fetch), and an
// unorderable INCUMBENT loses to any orderable candidate so a stray tag cannot pin
// the selection, while an unorderable CANDIDATE never wins.
func semverOutranks(candidate, incumbent string) bool {
	c, okC := canonicalSemver(candidate)
	if !okC {
		return false
	}
	i, okI := canonicalSemver(incumbent)
	if !okI {
		return true
	}
	return semver.Compare(c, i) > 0
}
