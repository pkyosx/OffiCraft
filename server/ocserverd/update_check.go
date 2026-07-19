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
//   - Comparison rule: the newest release tag vs the RUNNING appVersion
//     (/api/version's `version`). Official packages are stamped with the tag
//     (bin/release → OC_APP_VERSION), so tag == version means up to date. A
//     self-build keeps the honest "0.0.0" and simply reads as "not the
//     official latest" — which is true.
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
	TagName    string               `json:"tag_name"`
	HTMLURL    string               `json:"html_url"`
	Draft      bool                 `json:"draft"`
	Prerelease bool                 `json:"prerelease"`
	Assets     []githubReleaseAsset `json:"assets"`
}

// updateCheckState is the cached result of the last GitHub probe, guarded by
// apiServer.updateMu. includePre remembers WHICH channel produced it: a
// channel flip reads as an empty (unknown) state until its own fetch lands.
type updateCheckState struct {
	includePre bool
	checkedAt  time.Time // zero = never checked under this channel
	fetching   bool
	ok         bool          // a fetch has SUCCEEDED under this channel
	none       bool          // GitHub reachable, but no matching release published
	rel        githubRelease // the newest matching release (valid when ok && !none)
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

	if !st.ok || st.none || st.rel.TagName == "" || st.rel.TagName == appVersion {
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
// so unreachable GitHub is not re-polled before the TTL.
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
	s.updateCheck.checkedAt = time.Now()
	if err == nil {
		s.updateCheck.ok = true
		s.updateCheck.none = none
		s.updateCheck.rel = rel
	}
}

// fetchLatestOffiCraftRelease asks GitHub for the repo's releases (newest
// first) and picks the first non-draft one the channel admits (prereleases
// only when includePre). A 404 or an empty/filtered-out list is the honest
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
	for _, rel := range list {
		if rel.Draft || rel.TagName == "" {
			continue
		}
		if rel.Prerelease && !includePre {
			continue
		}
		return rel, false, nil
	}
	return githubRelease{}, true, nil
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
		if tag == appVersion {
			dto.Status = releaseStatusUpToDate
		} else {
			dto.Status = releaseStatusUpdate
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
		// click honestly: the fresh look failed.
		return updateCheckState{includePre: includePre}
	}
	s.updateCheck.ok = true
	s.updateCheck.none = none
	s.updateCheck.rel = rel
	return s.updateCheck
}
