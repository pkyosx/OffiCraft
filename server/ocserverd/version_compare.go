package main

// version_compare.go — semver ordering for the update check (T-9374).
//
// The update check used to call ANY release tag that merely DIFFERS from the
// running version "an update" (plain string inequality). That misreports —
// and, worse, auto-downgrades: whenever the cached "latest" lagged behind the
// running build (fresh release not yet indexed by GitHub, stale cache, a
// late-created release for an old tag), an armed `updater.auto_update` would
// happily download the OLDER tag and swap it over the newer running binary.
//
// The rule now (owner-ruled behaviour spec, T-9374):
//   - update_available is true iff the newest admissible release tag is
//     STRICTLY NEWER than the running version under semver ordering;
//     running >= latest reads as up to date.
//   - Every parseable version is ORDERED — including the self-build's honest
//     "0.0.0" (server.go), which sorts below any real release and therefore
//     still reads as update-available. (Previously self-builds only prompted
//     because "0.0.0" ≠ tag; now they prompt because 0.0.0 < any release.)
//   - If EITHER side does not parse as semver, the answer is false plus a
//     log warning: silence over misleading — and an unorderable tag must
//     NEVER trigger a download/swap.
//
// Ordering delegates to golang.org/x/mod/semver (the Go toolchain's own
// dependency-free semver package) rather than a hand-rolled parser: correct
// prerelease precedence (v0.4.2-rc1 < v0.4.2) and numeric field comparison
// for free. x/mod/semver requires the "v" prefix, while our labels come both
// ways (release tags "v0.4.2", self-build "0.0.0"), so canonicalSemver
// normalizes the prefix before validating.

import (
	"log"

	"golang.org/x/mod/semver"
)

// canonicalSemver normalizes one version label for ordering: an optional "v"
// prefix is tolerated (GitHub tags carry it, the self-build "0.0.0" does
// not), then the label must be valid semver (MAJOR.MINOR.PATCH with optional
// prerelease/build suffix). Returns the canonical "v"-prefixed form.
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

// releaseIsNewer reports whether candidateTag is STRICTLY newer than running
// under semver ordering — the single comparison behind update_available
// (update_check.go), the 檢查更新 verdict, and the upgrade honesty gate
// (upgrade.go). Either side unparseable → false + a log warning: never
// mislead, and never let an unorderable tag reach the download path.
func releaseIsNewer(candidateTag, running string) bool {
	c, okC := canonicalSemver(candidateTag)
	r, okR := canonicalSemver(running)
	if !okC || !okR {
		log.Printf("[update-check] warning: cannot order versions (latest %q vs running %q) — reporting no update", candidateTag, running)
		return false
	}
	return semver.Compare(c, r) > 0
}

// semverOutranks reports whether candidate should displace incumbent as "the
// newest release" while walking a release LIST (fetchLatestOffiCraftRelease).
// It differs from releaseIsNewer in two deliberate ways:
//   - SILENT. The list walk visits every admissible tag; a repo that carries
//     one non-semver label would otherwise emit a warning line per fetch.
//     releaseIsNewer still warns at the single decision point that matters
//     (the chosen tag vs the running version).
//   - An unorderable INCUMBENT is outranked by any orderable candidate, so a
//     stray non-semver tag cannot pin the selection to itself. An unorderable
//     CANDIDATE never wins — an unorderable tag must not reach the download
//     path (it also cannot pass releaseIsNewer downstream).
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
