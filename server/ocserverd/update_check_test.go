// Skeleton generated from server/ocserverd/update_check.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestReceiveBetaEnabled(t *testing.T) {
	t.Skip("TODO: receiveBetaEnabled reads the live prerelease toggle under the settings lock.")
}

func TestReleaseAPIBaseURL(t *testing.T) {
	t.Skip("TODO: releaseAPIBaseURL resolves the GitHub API base (test seam aware).")
}

func TestUpdateStatus(t *testing.T) {
	t.Skip("TODO: updateStatus answers /api/version's two fields from the cache, kicking a background refresh when the cache is missing/stale.")
}

func TestKickUpdateCheck(t *testing.T) {
	t.Skip("TODO: kickUpdateCheck force-expires the cache and starts a refresh NOW (unless one is already in flight) — the settings PATCH calls it so the software-update card reflects a channel flip without waiting out the TTL.")
}

func TestRefreshUpdateCheck(t *testing.T) {
	t.Skip("TODO: refreshUpdateCheck runs in its own goroutine: one bounded GitHub fetch, then a guarded cache write.")
}

func TestUpdateCheckedOKAt(t *testing.T) {
	t.Skip("TODO: updateCheckedOKAt reports WHEN the last SUCCESSFUL check landed under the current channel, as a strict RFC3339 stamp — nil when no check has ever succeeded (never checked, or every attempt failed).")
}

func TestFetchLatestOffiCraftRelease(t *testing.T) {
	t.Skip("TODO: fetchLatestOffiCraftRelease asks GitHub for the repo's releases and picks the SEMVER-GREATEST non-draft one the channel admits (prereleases only when includePre).")
}

func TestHandleCheckReleaseApiReleaseCheckGet(t *testing.T) {
	t.Run("a well-formed GET /api/release/check answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/release/check request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/release/check reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/release/check request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestSyncUpdateCheck(t *testing.T) {
	t.Skip("TODO: syncUpdateCheck serves the cache while it is button-fresh (checked within releaseCheckButtonTTL under the current channel) and otherwise fetches synchronously, folding the result into the shared cache.")
}
