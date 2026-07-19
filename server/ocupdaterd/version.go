package main

// version.go — the date-form version scheme. This closes the former
// version-format design card (owner decided 2026-07-13: versions are
// date-form and server-generated):
//
//	vYYMMDD-NNNN   e.g. v260713-0001
//
// where YYMMDD is the server-local publish date and NNNN is a zero-padded
// per-day serial (0001..9999) allocated by the server. The publisher does not
// need to send a version at all — POST /api/publish generates one and returns
// it in the 201 body. A client MAY still send one (backward compatible with
// the M1 free-string face) but it must match this shape.
//
// The string stays an opaque exact-match key everywhere else (latest /
// binary?version= / install.sh compare literal strings, unchanged); a nice
// side effect of the shape is that lexical order == publish order for
// server-generated versions.

import (
	"fmt"
	"regexp"
	"time"
)

// versionRe pins the published-version shape: vYYMMDD-NNNN.
var versionRe = regexp.MustCompile(`^v[0-9]{6}-[0-9]{4}$`)

// isValidVersion reports whether a client-supplied version matches the
// date-form shape. (It deliberately does not check the date part is TODAY —
// a publisher re-pushing yesterday's tagged build is legitimate.)
func isValidVersion(v string) bool { return versionRe.MatchString(v) }

// versionPrefix derives one day's version prefix from a wall-clock instant,
// e.g. "v260713-" (server-local time — the daily serial is a human-facing
// convenience, not a distributed-ordering claim).
func versionPrefix(now time.Time) string { return now.Format("v060102-") }

// ── release serial (r-N) — T-e9d1 ────────────────────────────────────────────
//
// Alongside the date-form version string (which stays as the immutable
// exact-match key, see the file header) every release also carries a pure
// monotonic serial minted at publish: r-1, r-2, r-3, … The date-form string
// answers "which build" (a stable download key); the serial answers "how new"
// in one glance — no date arithmetic, gap-free, publish-ordered.
//
// Durability / no-reuse-on-power-loss (owner requirement): the serial is
// allocated as MAX(serial)+1 over the COMMITTED release rows, so a crash
// between allocation and commit consumes nothing — the row (and its serial)
// simply never existed, and the next publish re-derives the same next value.
// A UNIQUE constraint on the column turns a concurrent double-allocation into
// an insert failure the publish handler retries, so the serial is never dup'd
// even under a race.

// releaseTag renders a serial as its human-facing tag, e.g. releaseTag(7) ==
// "r-7". A non-positive serial (an un-backfilled legacy row) renders "" — the
// caller then falls back to the version string rather than showing "r-0".
func releaseTag(serial int64) string {
	if serial <= 0 {
		return ""
	}
	return fmt.Sprintf("r-%d", serial)
}
