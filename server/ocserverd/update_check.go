package main

// Flow: GET /api/version answers from the CACHE (updateStatus) and never
// blocks on the network; GET /api/release/check is the owner's 檢查更新 button
// and fetches synchronously. The comparison is semver (version_compare.go): a
// self-build's "0.0.0" still prompts, and an unorderable label never triggers a
// download. The upgrade body lives in upgrade.go (never exposed to agents);
// auto_update.go runs it on the opt-in background cadence.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const releaseRepo = "pkyosx/OffiCraft"

const releaseAPIDefaultBase = "https://api.github.com"

// A var so the test binary can point every test server at an unroutable
// loopback address — a unit test must never reach the real GitHub.
var releaseAPIDefault = releaseAPIDefaultBase

// ⚠️ Anchored on checkedAt (the last ATTEMPT), NEVER on lastOKAt (the last
// SUCCESS): anchoring on lastOKAt would make every read while GitHub is down
// look stale and pound GitHub for exactly as long as it is broken. A failed
// attempt must leave lastOKAt untouched.
const updateCheckTTL = 5 * time.Minute

// Mashing 檢查更新 must not hammer GitHub's anonymous rate limit (60/hour/IP).
const releaseCheckButtonTTL = 30 * time.Second

const updateCheckTimeout = 8 * time.Second

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName    string               `json:"tag_name"`
	HTMLURL    string               `json:"html_url"`
	Draft      bool                 `json:"draft"`
	Prerelease bool                 `json:"prerelease"`
	Assets     []githubReleaseAsset `json:"assets"`
}

type updateCheckState struct {
	includePre bool

	checkedAt time.Time

	lastOKAt time.Time
	fetching bool
	ok       bool
	none     bool
	rel      githubRelease
}

func (s *apiServer) receiveBetaEnabled() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.updaterReceiveBeta
}

func (s *apiServer) releaseAPIBaseURL() string {
	if s.releaseAPIBase != "" {
		return s.releaseAPIBase
	}
	return releaseAPIDefault
}

func (s *apiServer) updateStatus() (available bool, latest *string) {
	includePre := s.receiveBetaEnabled()
	s.updateMu.Lock()
	if s.updateCheck.includePre != includePre {
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

func (s *apiServer) refreshUpdateCheck(includePre bool) {
	rel, none, err := fetchLatestOffiCraftRelease(s.releaseAPIBaseURL(), includePre)

	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateCheck.includePre != includePre {
		return
	}
	s.updateCheck.fetching = false
	s.updateCheck.checkedAt = time.Now()
	if err != nil {
		log.Printf("[update-check] GitHub release check failed (channel include_prerelease=%v): %v",
			includePre, err)
		return
	}
	s.updateCheck.lastOKAt = s.updateCheck.checkedAt
	s.updateCheck.ok = true
	s.updateCheck.none = none
	s.updateCheck.rel = rel
}

func (s *apiServer) updateCheckedOKAt() *string {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateCheck.lastOKAt.IsZero() {
		return nil
	}
	stamp := s.updateCheck.lastOKAt.UTC().Format(time.RFC3339)
	return &stamp
}

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
	// 🔴 GitHub's /releases list is ordered by CREATION TIME, not by version, so
	// taking list[0] would hide a newer tag created earlier; pick semver-max.
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

const (
	releaseStatusUpToDate = "up_to_date"
	releaseStatusUpdate   = "update_available"
	releaseStatusUnknown  = "unknown"
)

type releaseCheckDTO struct {
	Status string `json:"status"`

	CurrentVersion string `json:"current_version"`

	LatestTag  *string `json:"latest_tag"`
	ReleaseURL *string `json:"release_url"`
}

func (s *apiServer) HandleCheckReleaseApiReleaseCheckGet(w http.ResponseWriter, r *http.Request) {
	st := s.syncUpdateCheck()
	dto := releaseCheckDTO{Status: releaseStatusUnknown, CurrentVersion: appVersion}
	switch {
	case st.ok && st.none:
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
			dto.Status = releaseStatusUpToDate
		}
	}
	writeJSON(w, http.StatusOK, dto)
}

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
		return updateCheckState{includePre: includePre}
	}
	s.updateCheck.lastOKAt = s.updateCheck.checkedAt
	s.updateCheck.ok = true
	s.updateCheck.none = none
	s.updateCheck.rel = rel
	return s.updateCheck
}
