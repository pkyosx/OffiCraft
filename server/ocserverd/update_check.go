package main

// update_check.go — the software update check against GitHub Releases
// (updater teardown t-dc68: the retired server/ocupdaterd chain's replacement).
//
// Design:
//   - Software ships as GitHub Releases on pkyosx/OffiCraft (bin/release
//     builds + packages; publishing is an explicit `gh release create`). The
//     check asks the PUBLIC GitHub API — anonymously, no token, zero
//     configuration: there is no updater URL and no invite code any more.
//   - `updater.receive_beta` (default false) picks WHICH releases the check
//     follows: false = official releases only, true = prereleases too (the
//     GitHub `--prerelease` flag replaces the old updater's beta channel).
//   - GET /api/version consults a CACHED check result (updateStatus): a stale
//     cache kicks ONE background refresh goroutine and answers immediately
//     from what is known — an unreachable GitHub can never slow the probe
//     down (graceful degradation: on failure the last-known answer stands and
//     the next attempt waits out the same TTL, so a broken network is not
//     hammered).
//   - GET /api/release/check is the owner's EXPLICIT 檢查更新 button: it
//     answers synchronously (bounded by updateCheckTimeout) with the fresh
//     verdict — up_to_date / update_available (tag + release link) / unknown
//     (GitHub unreachable — the honest degraded verdict, still a 200).
//   - Comparison rule (T-9374): the newest release tag vs the RUNNING
//     appVersion (/api/version's `version`) under SEMVER ORDERING
//     (version_compare.go) — update_available only when the tag is STRICTLY
//     newer; running >= latest is up to date (a lagging release list can
//     never read as "an update", let alone downgrade). A self-build keeps
//     the honest "0.0.0", which sorts below any real release and therefore
//     still prompts. An unorderable label on either side reads as no update
//     (plus a log warning) — never a download trigger.
//   - Upgrading is the owner's call, expressed one of two ways: the EXPLICIT
//     trigger POST /api/update/upgrade (always available), or the OPT-IN
//     `updater.auto_update` toggle (default OFF) whose background cadence
//     runs the same verified execution body unattended (auto_update.go).
//     The execution body (download + digest verify + binary swap + restart)
//     lives in upgrade.go and is never exposed to agents (MCPExclude).

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// releaseRepo is the official distribution repo whose Releases are checked.
const releaseRepo = "pkyosx/OffiCraft"

// releaseAPIDefaultBase is the real GitHub API host; apiServer.releaseAPIBase
// overrides it per-server in tests.
const releaseAPIDefaultBase = "https://api.github.com"

// releaseAPIDefault is the process-wide base a server without an override
// uses. A var so the test binary's TestMain can point EVERY test server at an
// unroutable loopback address — a unit test must never reach the real GitHub
// (hermeticity + the anonymous 60/hour rate limit).
var releaseAPIDefault = releaseAPIDefaultBase

// updateCheckTTL is how long one background check result (success OR failure)
// is trusted before /api/version kicks a fresh refresh.
//
// ⚠️ The TTL is anchored on updateCheckState.checkedAt — the last ATTEMPT —
// and must NEVER be re-anchored on lastOKAt (the last SUCCESS). The two
// timestamps answer different questions on purpose: checkedAt exists to
// rate-limit the network (an unreachable GitHub is retried once per TTL, not
// hammered), lastOKAt exists to report how fresh the ANSWER is. Anchoring
// staleness on lastOKAt would make every read while GitHub is down look
// stale, and the server would pound GitHub for exactly as long as it is
// broken — the opposite of the graceful degradation this cache is for.
const updateCheckTTL = 5 * time.Minute

// releaseCheckButtonTTL is the explicit button's much shorter reuse window —
// mashing 檢查更新 must not hammer the anonymous GitHub rate limit
// (60/hour/IP), but a deliberate re-click after half a minute is honored.
const releaseCheckButtonTTL = 30 * time.Second

// updateCheckTimeout bounds one outbound GitHub call — the background refresh
// runs off the request path; the explicit button waits at most this long.
const updateCheckTimeout = 8 * time.Second

// githubReleaseAsset is the slice of a GitHub release asset this server reads.
type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// githubRelease is the slice of GitHub's releases list body this server reads.
type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	// TargetCommitish is the commit the tag was cut from. It arrives in the
	// SAME response this server was already fetching, so reading it costs
	// nothing; upgrade_notice.go needs it to name where an upgrade landed.
	// ⚠️ GitHub types this field as "commitish", and for a release created
	// against a branch it can be a branch NAME rather than a sha — every
	// release this project cuts carries a full sha (verified against the live
	// API, 2026-09-05), and the one consumer treats a non-sha as material it
	// simply passes through to a human reader rather than as a fact it acts on.
	TargetCommitish string               `json:"target_commitish"`
	Draft           bool                 `json:"draft"`
	Prerelease      bool                 `json:"prerelease"`
	Assets          []githubReleaseAsset `json:"assets"`
}

// updateCheckState is the cached result of the last GitHub probe, guarded by
// apiServer.updateMu. includePre remembers WHICH channel produced it: a
// channel flip reads as an empty (unknown) state until its own fetch lands.
type updateCheckState struct {
	includePre bool
	// checkedAt is the last ATTEMPT (success OR failure) — the TTL anchor
	// that keeps an unreachable GitHub from being re-polled every request.
	// Never report it as "when we last knew": a failed attempt stamps it.
	checkedAt time.Time // zero = never checked under this channel
	// lastOKAt is the last SUCCESSFUL check — the freshness of the answer
	// /api/version is serving (update_checked_ok_at). Zero = never succeeded
	// under this channel; a FAILED attempt must leave it untouched, which is
	// the whole point of keeping it apart from checkedAt. It is deliberately
	// NOT the TTL anchor (see updateCheckTTL).
	lastOKAt time.Time
	fetching bool
	ok       bool          // a fetch has SUCCEEDED under this channel
	none     bool          // GitHub reachable, but no matching release published
	rel      githubRelease // the newest matching release (valid when ok && !none)
}

// receiveBetaEnabled reads the live prerelease toggle under the settings lock.
func (s *apiServer) receiveBetaEnabled() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.updaterReceiveBeta
}

// releaseAPIBaseURL resolves the GitHub API base (test seam aware).
func (s *apiServer) releaseAPIBaseURL() string {
	if s.releaseAPIBase != "" {
		return s.releaseAPIBase
	}
	return releaseAPIDefault
}

// updateStatus answers /api/version's two fields from the cache, kicking a
// background refresh when the cache is missing/stale. NEVER blocks on the
// network. Nothing known (yet) → the honest-static (false, nil).
func (s *apiServer) updateStatus() (available bool, latest *string) {
	includePre := s.receiveBetaEnabled()
	s.updateMu.Lock()
	if s.updateCheck.includePre != includePre {
		// Channel changed since the cache was built — reset to unknown; the
		// kicked fetch below rebuilds it under the new channel.
		s.updateCheck = updateCheckState{includePre: includePre}
	}
	stale := s.updateCheck.checkedAt.IsZero() ||
		time.Since(s.updateCheck.checkedAt) > updateCheckTTL
	if stale && !s.updateCheck.fetching {
		s.updateCheck.fetching = true
		go s.refreshUpdateCheck(includePre)
	}
	st := s.updateCheck
	s.updateMu.Unlock()

	if !st.ok || st.none || st.rel.TagName == "" || !releaseIsNewer(st.rel.TagName, appVersion) {
		return false, nil
	}
	v := st.rel.TagName
	return true, &v
}

// kickUpdateCheck force-expires the cache and starts a refresh NOW (unless one
// is already in flight) — the settings PATCH calls it so the software-update
// card reflects a channel flip without waiting out the TTL.
func (s *apiServer) kickUpdateCheck() {
	includePre := s.receiveBetaEnabled()
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateCheck.includePre != includePre {
		s.updateCheck = updateCheckState{includePre: includePre}
	}
	s.updateCheck.checkedAt = time.Time{}
	if !s.updateCheck.fetching {
		s.updateCheck.fetching = true
		go s.refreshUpdateCheck(includePre)
	}
}

// refreshUpdateCheck runs in its own goroutine: one bounded GitHub fetch,
// then a guarded cache write. Failure is GRACEFUL: the last-known release
// stands (stale-but-honest beats fabricated-empty) and checkedAt is stamped
// so unreachable GitHub is not re-polled before the TTL — but it is no longer
// SILENT (the failure is logged) and it does not move lastOKAt, so nothing
// downstream can mistake a failed attempt for a fresh successful check.
func (s *apiServer) refreshUpdateCheck(includePre bool) {
	rel, none, err := fetchLatestOffiCraftRelease(s.releaseAPIBaseURL(), includePre)

	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateCheck.includePre != includePre {
		// The channel moved under this fetch — its result describes the WRONG
		// channel; drop it (the new channel's own kick is/was scheduled).
		return
	}
	s.updateCheck.fetching = false
	// The ATTEMPT stamp: written whether or not the fetch worked (TTL anchor).
	s.updateCheck.checkedAt = time.Now()
	if err != nil {
		// Loud, once per TTL at most: before this line a check that could
		// never reach GitHub was entirely SILENT — /api/version simply kept
		// answering update_available=false and nothing in the log said why.
		log.Printf("[update-check] GitHub release check failed (channel include_prerelease=%v): %v",
			includePre, err)
		return
	}
	// SUCCESS ONLY: the freshness stamp of the answer being served.
	s.updateCheck.lastOKAt = s.updateCheck.checkedAt
	s.updateCheck.ok = true
	s.updateCheck.none = none
	s.updateCheck.rel = rel
}

// updateCheckedOKAt reports WHEN the last SUCCESSFUL check landed under the
// current channel, as a strict RFC3339 stamp — nil when no check has ever
// succeeded (never checked, or every attempt failed). It is the honest
// freshness of /api/version's update_available: a false there means something
// different when this is minutes old than when it is nil or hours old.
//
// Read-only: unlike updateStatus it neither kicks a refresh nor resets the
// cache on a channel flip (the caller does that first), so the stamp it
// returns always describes the state that produced the answer alongside it.
func (s *apiServer) updateCheckedOKAt() *string {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateCheck.lastOKAt.IsZero() {
		return nil
	}
	stamp := s.updateCheck.lastOKAt.UTC().Format(time.RFC3339)
	return &stamp
}

// fetchLatestOffiCraftRelease asks GitHub for the repo's releases and picks
// the SEMVER-GREATEST non-draft one the channel admits (prereleases only when
// includePre). GitHub orders that list by creation time, NOT by version, so
// position is not a version ranking. A 404 or an empty/filtered-out list is the honest
// "nothing published yet" → (zero, true, nil); any other non-200 / transport
// / decode failure is an error (the caller keeps its last-known state).
func fetchLatestOffiCraftRelease(base string, includePre bool) (githubRelease, bool, error) {
	req, err := http.NewRequest(http.MethodGet,
		base+"/repos/"+releaseRepo+"/releases?per_page=20", nil)
	if err != nil {
		return githubRelease{}, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: updateCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return githubRelease{}, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, false, fmt.Errorf("github answered %d", resp.StatusCode)
	}
	var list []githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&list); err != nil {
		return githubRelease{}, false, err
	}
	// Pick the SEMVER-MAXIMUM admissible release, not the first row. GitHub's
	// /releases list is ordered by CREATION TIME, not by version — a v0.9.9
	// created before a v0.4.1 (backfilled tag, re-cut release, out-of-order
	// publishing) lands SECOND in the body, and taking list[0] would report
	// v0.4.1 as "the newest" and silently hide the real v0.9.9 from anyone
	// running something in between. The draft/prerelease filters below are
	// unchanged and still gate admission; only the tie-break among the
	// admitted rows moved from position to semver ordering.
	best := githubRelease{}
	found := false
	for _, rel := range list {
		if rel.Draft || rel.TagName == "" {
			continue
		}
		if rel.Prerelease && !includePre {
			continue
		}
		if !found || semverOutranks(rel.TagName, best.TagName) {
			best, found = rel, true
		}
	}
	if !found {
		return githubRelease{}, true, nil
	}
	return best, false, nil
}

// The closed release-check verdict set (releaseCheckDTO.Status).
const (
	releaseStatusUpToDate = "up_to_date"
	releaseStatusUpdate   = "update_available"
	releaseStatusUnknown  = "unknown"
)

// releaseCheckDTO is the GET /api/release/check body (spec ReleaseCheckDTO).
type releaseCheckDTO struct {
	// Status: "up_to_date" | "update_available" | "unknown" (GitHub not
	// reachable / not answering — the honest degraded verdict).
	Status string `json:"status"`
	// CurrentVersion mirrors /api/version's version (official package = the
	// release tag; self-build = "0.0.0").
	CurrentVersion string `json:"current_version"`
	// LatestTag / ReleaseURL describe the newest admissible GitHub Release;
	// null when unknown or when no release has been published yet.
	LatestTag  *string `json:"latest_tag"`
	ReleaseURL *string `json:"release_url"`
}

// HandleCheckReleaseApiReleaseCheckGet answers the owner's explicit 檢查更新
// click: a SYNCHRONOUS fresh check (short reuse window absorbs mashing),
// folded into the same cache the background loop and auto-update read.
func (s *apiServer) HandleCheckReleaseApiReleaseCheckGet(w http.ResponseWriter, r *http.Request) {
	st := s.syncUpdateCheck()
	dto := releaseCheckDTO{Status: releaseStatusUnknown, CurrentVersion: appVersion}
	switch {
	case st.ok && st.none:
		// GitHub answered: nothing admissible has ever been published —
		// nothing newer than the running build exists.
		dto.Status = releaseStatusUpToDate
	case st.ok:
		tag, htmlURL := st.rel.TagName, st.rel.HTMLURL
		dto.LatestTag = &tag
		if htmlURL != "" {
			dto.ReleaseURL = &htmlURL
		}
		if releaseIsNewer(tag, appVersion) {
			dto.Status = releaseStatusUpdate
		} else {
			// running >= latest (or an unorderable label): up to date —
			// silence over misleading (releaseIsNewer logs the warning).
			dto.Status = releaseStatusUpToDate
		}
	}
	writeJSON(w, http.StatusOK, dto)
}

// syncUpdateCheck serves the cache while it is button-fresh (checked within
// releaseCheckButtonTTL under the current channel) and otherwise fetches
// synchronously, folding the result into the shared cache. A failed fetch
// keeps the last-known release data but reports the failure to THIS caller
// (ok=false → the button shows 查不到 instead of a stale certainty).
func (s *apiServer) syncUpdateCheck() updateCheckState {
	includePre := s.receiveBetaEnabled()
	s.updateMu.Lock()
	if s.updateCheck.includePre == includePre && s.updateCheck.ok &&
		!s.updateCheck.checkedAt.IsZero() &&
		time.Since(s.updateCheck.checkedAt) <= releaseCheckButtonTTL {
		st := s.updateCheck
		s.updateMu.Unlock()
		return st
	}
	s.updateMu.Unlock()

	rel, none, err := fetchLatestOffiCraftRelease(s.releaseAPIBaseURL(), includePre)

	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateCheck.includePre != includePre {
		s.updateCheck = updateCheckState{includePre: includePre}
	}
	s.updateCheck.fetching = false
	s.updateCheck.checkedAt = time.Now()
	if err != nil {
		// Keep the background cache's last-known state, but answer THIS
		// click honestly: the fresh look failed. checkedAt (the attempt) is
		// stamped above; lastOKAt is NOT — a failure must never pass itself
		// off as a successful check.
		return updateCheckState{includePre: includePre}
	}
	s.updateCheck.lastOKAt = s.updateCheck.checkedAt
	s.updateCheck.ok = true
	s.updateCheck.none = none
	s.updateCheck.rel = rel
	return s.updateCheck
}
