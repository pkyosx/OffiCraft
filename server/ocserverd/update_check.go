package main

// Flow: GET /api/version answers from the CACHE (updateStatus) and never
// blocks on the network; GET /api/release/check is the owner's 檢查更新 button
// and fetches synchronously. The comparison is semver (version_compare.go): a
// self-build's "0.0.0" still prompts, and an unorderable label never triggers a
// download. The upgrade body lives in upgrade.go (never exposed to agents);
// auto_update.go runs it on the opt-in background cadence.
//
// The source is GitHub's public release WEB pages, never api.github.com: the
// anonymous API budget is per egress IP and other programs on the same network
// can exhaust it for hours. The stable channel follows the release GitHub marks
// Latest (so moving that pointer is what rolls stable stations back); the beta
// channel takes the semver-greatest entry of the releases feed whatever its
// prerelease flag, because the feed does not carry the flag.

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const releaseRepo = "pkyosx/OffiCraft"

const releaseSiteDefaultBase = "https://github.com"

// A var so the test binary can point every test server at an unroutable
// loopback address — a unit test must never reach the real GitHub.
var releaseSiteDefault = releaseSiteDefaultBase

const releaseCheckButtonTTL = 30 * time.Second

const updateCheckTimeout = 8 * time.Second

type updateCheckState struct {
	includePre bool

	checkedAt time.Time

	lastOKAt time.Time
	fetching bool
	ok       bool
	none     bool
	tag      string
}

func (s *apiServer) receiveBetaEnabled() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.updaterReceiveBeta
}

func (s *apiServer) updateCheckInterval() time.Duration {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return time.Duration(s.updaterCheckIntervalSecs) * time.Second
}

func (s *apiServer) releaseSiteBaseURL() string {
	if s.releaseSiteBase != "" {
		return s.releaseSiteBase
	}
	return releaseSiteDefault
}

func releaseRepoPath() string { return "/" + releaseRepo + "/releases" }

func releasePageURL(base, tag string) string {
	return base + releaseRepoPath() + "/tag/" + url.PathEscape(tag)
}

func releaseAssetURL(base, tag, asset string) string {
	return base + releaseRepoPath() + "/download/" + url.PathEscape(tag) + "/" + url.PathEscape(asset)
}

// releaseTagFromURL answers the tag a release page URL names, or "" when the
// URL is not /<repo>/releases/tag/<tag>.
func releaseTagFromURL(u *url.URL) string {
	tag, found := strings.CutPrefix(u.Path, releaseRepoPath()+"/tag/")
	if !found || tag == "" || strings.Contains(tag, "/") {
		return ""
	}
	return tag
}

func (s *apiServer) updateStatus() (available bool, latest *string) {
	includePre := s.receiveBetaEnabled()
	interval := s.updateCheckInterval()
	s.updateMu.Lock()
	if s.updateCheck.includePre != includePre {
		s.updateCheck = updateCheckState{includePre: includePre}
	}
	// ⚠️ Anchored on checkedAt (the last ATTEMPT), NEVER on lastOKAt (the last
	// SUCCESS): anchoring on lastOKAt would make every read while GitHub is down
	// look stale and pound GitHub for exactly as long as it is broken. A failed
	// attempt must leave lastOKAt untouched.
	stale := s.updateCheck.checkedAt.IsZero() ||
		time.Since(s.updateCheck.checkedAt) > interval
	if stale && !s.updateCheck.fetching {
		s.updateCheck.fetching = true
		go s.refreshUpdateCheck(includePre)
	}
	st := s.updateCheck
	s.updateMu.Unlock()

	if !st.ok || st.none || st.tag == "" || !releaseIsNewer(st.tag, appVersion) {
		return false, nil
	}
	v := st.tag
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
	tag, none, err := fetchLatestOffiCraftRelease(s.releaseSiteBaseURL(), includePre)

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
	s.updateCheck.tag = tag
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

// fetchLatestOffiCraftRelease answers the newest admissible tag, or none=true
// when nothing is published on that channel.
func fetchLatestOffiCraftRelease(base string, includePre bool) (tag string, none bool, err error) {
	client := &http.Client{
		Timeout: updateCheckTimeout,
		// /releases/latest answers with a redirect whose Location IS the answer;
		// following it would only fetch a large HTML page.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if includePre {
		return fetchNewestFeedRelease(client, base)
	}
	return fetchLatestMarkedRelease(client, base)
}

func fetchLatestMarkedRelease(client *http.Client, base string) (string, bool, error) {
	resp, err := client.Get(base + releaseRepoPath() + "/latest")
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return "", true, nil
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return "", false, fmt.Errorf("github answered %d", resp.StatusCode)
	}
	loc, err := resp.Location()
	if err != nil {
		return "", false, fmt.Errorf("github redirected without a usable Location: %w", err)
	}
	if tag := releaseTagFromURL(loc); tag != "" {
		return tag, false, nil
	}
	// With nothing marked Latest GitHub sends the visitor to the release list.
	if strings.TrimSuffix(loc.Path, "/") == releaseRepoPath() {
		return "", true, nil
	}
	return "", false, fmt.Errorf("github redirected to %s, not a release page", loc)
}

type releaseFeed struct {
	Entries []struct {
		Links []struct {
			Rel  string `xml:"rel,attr"`
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

// The feed lists only the newest few releases in creation order, not version
// order, so a newer tag created earlier must still win: pick semver-max.
func fetchNewestFeedRelease(client *http.Client, base string) (string, bool, error) {
	feedURL := base + releaseRepoPath() + ".atom"
	resp, err := client.Get(feedURL)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("github answered %d", resp.StatusCode)
	}
	var feed releaseFeed
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&feed); err != nil {
		return "", false, fmt.Errorf("the releases feed is unreadable: %w", err)
	}
	best := ""
	for i, entry := range feed.Entries {
		tag := ""
		for _, link := range entry.Links {
			if link.Rel != "" && link.Rel != "alternate" {
				continue
			}
			if u, err := url.Parse(link.Href); err == nil {
				tag = releaseTagFromURL(u)
			}
			break
		}
		if tag == "" {
			return "", false, fmt.Errorf("releases feed entry %d links to no release page", i+1)
		}
		if best == "" || semverOutranks(tag, best) {
			best = tag
		}
	}
	if best == "" {
		return "", true, nil
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
		tag, pageURL := st.tag, releasePageURL(s.releaseSiteBaseURL(), st.tag)
		dto.LatestTag = &tag
		dto.ReleaseURL = &pageURL
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

	tag, none, err := fetchLatestOffiCraftRelease(s.releaseSiteBaseURL(), includePre)

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
	s.updateCheck.tag = tag
	return s.updateCheck
}
