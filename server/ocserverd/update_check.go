package main

// update_check.go — the software update check against the configured updater
// server (server/ocupdaterd).
//
// Design (exit-beta step 4; dual-channel + auto-update toggles 2026-07-14):
//   - The owner points this server at an updater instance via two DB settings
//     (settings.go): `updater.url` + `updater.invite_code` (the invite code is
//     a SECRET — write-only through PATCH /api/settings, never echoed back;
//     reads expose only updater_invite_code_set).
//   - `updater.receive_beta` (default false) picks WHICH channel the check
//     follows: the checker sends ?channel=beta|ga to /api/latest (GA is also
//     the updater-side default — a pre-toggle server keeps its stable
//     semantics with zero config).
//   - GET /api/version consults a CACHED check result (updateStatus): a stale
//     cache kicks ONE background refresh goroutine and answers immediately
//     from what is known — a dead/slow updater can never slow the probe down
//     (graceful degradation: on failure the last-known answer stands and the
//     next attempt waits out the same TTL, so a broken network is not
//     hammered).
//   - Upgrading is the owner's call, expressed one of two ways: the EXPLICIT
//     trigger POST /api/update/upgrade (always available), or the OPT-IN
//     `updater.auto_update` toggle (default OFF) whose background cadence
//     runs the same verified execution body unattended (auto_update.go).
//     The former "nothing here ever upgrades automatically" posture was
//     retired by the owner's D2 decision (2026-07-14) — automatic now means
//     "the owner armed it", never "the server decided". The execution body
//     (download + digest verify + binary swap + restart) lives in upgrade.go
//     and is never exposed to agents (MCPExclude on the route).
//
// The wire the checker speaks is ocupdaterd's GET /api/latest (Bearer invite
// code → {"version", "git_sha", ...}); that daemon is deliberately OUTSIDE the
// frozen spec/openapi.json, so the client here is a plain JSON GET.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// updateCheckTTL is how long one check result (success OR failure) is trusted
// before /api/version kicks a fresh background refresh.
const updateCheckTTL = 5 * time.Minute

// updateCheckTimeout bounds the outbound /api/latest call — the refresh runs
// off the request path, so this only caps how long a goroutine lingers.
const updateCheckTimeout = 5 * time.Second

// updateCheckState is the cached result of the last updater probe, guarded by
// apiServer.updateMu. cfgURL/cfgCode/cfgChannel remember WHICH updater config
// produced it: a completed fetch whose config has since changed is discarded,
// and a config change (including a channel flip) reads as an empty (unknown)
// state until its own fetch lands.
type updateCheckState struct {
	cfgURL        string
	cfgCode       string
	cfgChannel    string
	checkedAt     time.Time // zero = never checked under this config
	fetching      bool
	latestVersion string // display label of the newest release ("" = none known)
	latestGitSHA  string // updater-reported git_sha of that version ("" ok)
	// runningTag is the r-N the updater assigned to THIS server's own build
	// (self-lookup by git sha, T-e9d1); "" = the updater doesn't know this
	// build (never published there) or predates the serial. /api/version shows
	// it as the running release_tag.
	runningTag string
}

// The two update channels the checker can follow (ocupdaterd's closed set).
const (
	updateChannelGA   = "ga"
	updateChannelBeta = "beta"
)

// updaterConfig reads the live updater settings snapshot ("" = unset;
// channel derives from the receive-beta toggle).
func (s *apiServer) updaterConfig() (updaterURL, inviteCode, channel string) {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	channel = updateChannelGA
	if s.updaterReceiveBeta {
		channel = updateChannelBeta
	}
	return s.updaterURL, s.updaterInviteCode, channel
}

// updateStatus answers /api/version's two fields from the cache, kicking a
// background refresh when the cache is missing/stale. NEVER blocks on the
// network. No updater configured → the M1 honest-static (false, nil).
func (s *apiServer) updateStatus() (available bool, latest *string) {
	cfgURL, cfgCode, cfgChannel := s.updaterConfig()
	if cfgURL == "" || cfgCode == "" {
		return false, nil
	}
	s.updateMu.Lock()
	if s.updateCheck.cfgURL != cfgURL || s.updateCheck.cfgCode != cfgCode || s.updateCheck.cfgChannel != cfgChannel {
		// Config changed since the cache was built — reset to unknown; the
		// kicked fetch below rebuilds it under the new config.
		s.updateCheck = updateCheckState{cfgURL: cfgURL, cfgCode: cfgCode, cfgChannel: cfgChannel}
	}
	stale := s.updateCheck.checkedAt.IsZero() ||
		time.Since(s.updateCheck.checkedAt) > updateCheckTTL
	if stale && !s.updateCheck.fetching {
		s.updateCheck.fetching = true
		go s.refreshUpdateCheck(cfgURL, cfgCode, cfgChannel)
	}
	v, sha := s.updateCheck.latestVersion, s.updateCheck.latestGitSHA
	s.updateMu.Unlock()

	if v == "" || v == appVersion {
		return false, nil
	}
	// Same build published under a version label: the updater's git_sha
	// matching the RUNNING sha means there is nothing newer to install —
	// flagging an "update" would be a lie.
	if sha != "" && sha == s.processSHA {
		return false, nil
	}
	return true, &v
}

// runningReleaseTag answers /api/version's release_tag field: the r-N the
// updater assigned to THIS build (self-lookup), or nil when unknown (no updater
// configured, this build never published there, or a pre-serial updater). Never
// blocks — reads the same cache updateStatus refreshes.
func (s *apiServer) runningReleaseTag() *string {
	cfgURL, cfgCode, _ := s.updaterConfig()
	if cfgURL == "" || cfgCode == "" {
		return nil
	}
	s.updateMu.Lock()
	tag := s.updateCheck.runningTag
	s.updateMu.Unlock()
	if tag == "" {
		return nil
	}
	return &tag
}

// kickUpdateCheck force-expires the cache and starts a refresh NOW (unless one
// is already in flight) — the settings PATCH calls it so the software-update
// card reflects a newly configured updater without waiting out the TTL.
func (s *apiServer) kickUpdateCheck() {
	cfgURL, cfgCode, cfgChannel := s.updaterConfig()
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if cfgURL == "" || cfgCode == "" {
		s.updateCheck = updateCheckState{}
		return
	}
	if s.updateCheck.cfgURL != cfgURL || s.updateCheck.cfgCode != cfgCode || s.updateCheck.cfgChannel != cfgChannel {
		s.updateCheck = updateCheckState{cfgURL: cfgURL, cfgCode: cfgCode, cfgChannel: cfgChannel}
	}
	s.updateCheck.checkedAt = time.Time{}
	if !s.updateCheck.fetching {
		s.updateCheck.fetching = true
		go s.refreshUpdateCheck(cfgURL, cfgCode, cfgChannel)
	}
}

// refreshUpdateCheck runs in its own goroutine: one bounded GET
// {updater}/api/latest?channel=, then a guarded cache write. Failure is
// GRACEFUL: the last-known latest stands (stale-but-honest beats
// fabricated-empty) and checkedAt is stamped so the dead updater is not
// re-polled before the TTL.
func (s *apiServer) refreshUpdateCheck(cfgURL, cfgCode, cfgChannel string) {
	res, err := fetchUpdaterLatest(cfgURL, cfgCode, cfgChannel, appVersion, s.processSHA)

	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateCheck.cfgURL != cfgURL || s.updateCheck.cfgCode != cfgCode || s.updateCheck.cfgChannel != cfgChannel {
		// The config moved under this fetch — its result describes the WRONG
		// updater; drop it (the new config's own kick is/was scheduled).
		return
	}
	s.updateCheck.fetching = false
	s.updateCheck.checkedAt = time.Now()
	if err == nil {
		s.updateCheck.latestVersion = res.displayVersion()
		s.updateCheck.latestGitSHA = res.GitSHA
		s.updateCheck.runningTag = res.CurrentReleaseTag
	}
}

// fetchUpdaterLatest speaks ocupdaterd's GET /api/latest?channel=. A 404 is
// the honest "nothing published yet on that channel" → ("", "", nil); any
// other non-200 / transport / decode failure is an error (the caller keeps
// its last-known state).
//
// The check also SELF-REPORTS what this server is running via the optional
// current_version/current_sha params (fleet monitoring: the updater's portal
// shows each invite's last check + running build; the git sha is the honest
// build identity here — appVersion is a constant label). Purely additive
// outbound params; an older updater ignores unknown query keys.
// updaterLatest is the slice of ocupdaterd's /api/latest body this checker
// reads. ReleaseTag / CurrentReleaseTag are the T-e9d1 additions (a pre-serial
// updater omits both; the fields stay ""); displayVersion prefers the human
// r-N tag and falls back to the date-form version string for such an updater.
type updaterLatest struct {
	Version           string `json:"version"`
	ReleaseTag        string `json:"release_tag"`
	GitSHA            string `json:"git_sha"`
	CurrentReleaseTag string `json:"current_release_tag"`
}

func (u updaterLatest) displayVersion() string {
	if u.ReleaseTag != "" {
		return u.ReleaseTag
	}
	return u.Version
}

func fetchUpdaterLatest(cfgURL, cfgCode, channel, currentVersion, currentSHA string) (updaterLatest, error) {
	req, err := http.NewRequest(http.MethodGet,
		strings.TrimRight(cfgURL, "/")+"/api/latest?channel="+url.QueryEscape(channel)+
			"&current_version="+url.QueryEscape(currentVersion)+
			"&current_sha="+url.QueryEscape(currentSHA), nil)
	if err != nil {
		return updaterLatest{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfgCode)
	client := &http.Client{Timeout: updateCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return updaterLatest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return updaterLatest{}, nil // updater reachable, nothing published yet
	}
	if resp.StatusCode != http.StatusOK {
		return updaterLatest{}, fmt.Errorf("updater answered %d", resp.StatusCode)
	}
	var body updaterLatest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return updaterLatest{}, err
	}
	if body.Version == "" {
		return updaterLatest{}, fmt.Errorf("updater /api/latest carried no version")
	}
	return body, nil
}

// validateUpdaterURL admits "" (clears the setting — update checks off) or an
// absolute http(s) URL with a host. Anything else is the PATCH 422.
func validateUpdaterURL(raw string) (normalized string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", true
	}
	u, err := url.Parse(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return strings.TrimRight(trimmed, "/"), true
}
