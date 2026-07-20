package main

// update_check_test.go — the GitHub-Releases update check (update_check.go):
// the cached /api/version face, the prerelease channel toggle, graceful
// degradation when GitHub is unreachable, and the explicit
// GET /api/release/check button (fresh verdict incl. the honest "unknown").

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// TestMain points EVERY test server's default GitHub base at an unroutable
// loopback address: a unit test must never reach the real api.github.com
// (hermeticity + the anonymous rate limit). Tests that want a GitHub set the
// per-server releaseAPIBase seam to an httptest fake.
func TestMain(m *testing.M) {
	releaseAPIDefault = "http://127.0.0.1:1"
	os.Exit(m.Run())
}

// fakeRelease is one release row the fake GitHub serves (newest first).
type fakeRelease struct {
	tag        string
	prerelease bool
	draft      bool
	assets     map[string][]byte // name → bytes (download face)
}

// fakeGitHub is an httptest GitHub API speaking the two faces this server
// uses: the releases list and the asset downloads. status != 0 forces every
// list response to that code (unreachable/ratelimited GitHub); mu-guarded so
// tests can flip releases/status mid-flight.
type fakeGitHub struct {
	srv      *httptest.Server
	mu       sync.Mutex
	releases []fakeRelease
	status   int
	hits     int
}

func newFakeGitHub(t *testing.T, releases ...fakeRelease) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{releases: releases}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		switch {
		case r.URL.Path == "/repos/pkyosx/OffiCraft/releases":
			g.hits++
			if g.status != 0 {
				w.WriteHeader(g.status)
				return
			}
			var list []map[string]any
			for _, rel := range g.releases {
				var assets []map[string]any
				for name := range rel.assets {
					assets = append(assets, map[string]any{
						"name":                 name,
						"browser_download_url": g.srv.URL + "/dl/" + rel.tag + "/" + name,
						"size":                 len(rel.assets[name]),
					})
				}
				list = append(list, map[string]any{
					"tag_name":   rel.tag,
					"html_url":   g.srv.URL + "/rel/" + rel.tag,
					"draft":      rel.draft,
					"prerelease": rel.prerelease,
					"assets":     assets,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)
		case len(r.URL.Path) > 4 && r.URL.Path[:4] == "/dl/":
			for _, rel := range g.releases {
				for name, body := range rel.assets {
					if r.URL.Path == "/dl/"+rel.tag+"/"+name {
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write(body)
						return
					}
				}
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGitHub) setStatus(code int) {
	g.mu.Lock()
	g.status = code
	g.mu.Unlock()
}

func (g *fakeGitHub) setReleases(releases ...fakeRelease) {
	g.mu.Lock()
	g.releases = releases
	g.mu.Unlock()
}

// waitUpdateSettled polls until no background fetch is in flight and the cache
// carries a stamp (one refresh completed).
func waitUpdateSettled(t *testing.T, s *apiServer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.updateMu.Lock()
		done := !s.updateCheck.fetching && !s.updateCheck.checkedAt.IsZero()
		s.updateMu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("update check never settled")
}

// expireUpdateCache force-expires the cache stamp WITHOUT kicking a fetch, so
// the next updateStatus/syncUpdateCheck call is the one that reaches out.
func expireUpdateCache(s *apiServer) {
	s.updateMu.Lock()
	s.updateCheck.checkedAt = time.Time{}
	s.updateMu.Unlock()
}

// setAppVersion overrides the package version label for one test.
func setAppVersion(t *testing.T, v string) {
	t.Helper()
	orig := appVersion
	appVersion = v
	t.Cleanup(func() { appVersion = orig })
}

func ownerLogin(t *testing.T, srvURL, password string) string {
	t.Helper()
	status, data := doJSON(t, "POST", srvURL+"/api/login", "",
		`{"password":"`+password+`"}`)
	if status != 200 {
		t.Fatalf("login: %d %v", status, data)
	}
	return data["token"].(string)
}

func TestUpdateStatusReportsNewerRelease(t *testing.T) {
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.2.0"})
	s := &apiServer{releaseAPIBase: gh.srv.URL}

	// First read serves the (empty) cache and kicks the background fetch —
	// it must answer immediately and honestly (nothing known yet).
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("pre-settle read must be (false, nil): %v %v", available, latest)
	}
	waitUpdateSettled(t, s)
	available, latest := s.updateStatus()
	if !available || latest == nil || *latest != "v0.2.0" {
		t.Fatalf("settled read must report v0.2.0: %v %v", available, latest)
	}
}

func TestUpdateStatusUnreachableGitHubDegradesGracefully(t *testing.T) {
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.2.0"})
	gh.setStatus(http.StatusInternalServerError)
	s := &apiServer{releaseAPIBase: gh.srv.URL}

	// Nothing ever fetched + GitHub broken: the honest static face.
	s.updateStatus()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("a failed first fetch must stay (false, nil): %v %v", available, latest)
	}

	// A SUCCEEDED fetch whose GitHub later dies: the last-known answer stands
	// (stale-but-honest beats fabricated-empty).
	gh.setStatus(0)
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); !available || latest == nil || *latest != "v0.2.0" {
		t.Fatalf("recovered fetch must report v0.2.0: %v %v", available, latest)
	}
	gh.setStatus(http.StatusBadGateway)
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); !available || latest == nil || *latest != "v0.2.0" {
		t.Fatalf("last-known must survive a later failure: %v %v", available, latest)
	}
}

func TestUpdateStatusFailureIsNotRepolledBeforeTTL(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.setStatus(http.StatusForbidden) // the rate-limit shape
	s := &apiServer{releaseAPIBase: gh.srv.URL}
	s.updateStatus()
	waitUpdateSettled(t, s)
	gh.mu.Lock()
	hits := gh.hits
	gh.mu.Unlock()
	// Repeated probe reads inside the TTL must not reach out again.
	for i := 0; i < 5; i++ {
		s.updateStatus()
	}
	time.Sleep(50 * time.Millisecond)
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if gh.hits != hits {
		t.Fatalf("a failed check must wait out the TTL, not hammer GitHub: %d → %d", hits, gh.hits)
	}
}

func TestUpdateStatusSameTagIsUpToDate(t *testing.T) {
	setAppVersion(t, "v0.2.0")
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.2.0"})
	s := &apiServer{releaseAPIBase: gh.srv.URL}
	s.updateStatus()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("running the latest tag must read up-to-date: %v %v", available, latest)
	}
}

func TestUpdateStatusChannelGovernsPrereleases(t *testing.T) {
	gh := newFakeGitHub(t,
		fakeRelease{tag: "v0.3.0-rc1", prerelease: true},
		fakeRelease{tag: "v0.3.0-draft", draft: true},
		fakeRelease{tag: "v0.2.0"},
	)
	s := &apiServer{releaseAPIBase: gh.srv.URL}

	// Default channel: official releases only — the prerelease (and the
	// draft) are skipped.
	s.updateStatus()
	waitUpdateSettled(t, s)
	if _, latest := s.updateStatus(); latest == nil || *latest != "v0.2.0" {
		t.Fatalf("official channel must pick v0.2.0: %v", latest)
	}

	// Flip receive-beta: the channel change resets the cache (no stale
	// cross-channel answer) and the next fetch admits the prerelease.
	s.settingsMu.Lock()
	s.updaterReceiveBeta = true
	s.settingsMu.Unlock()
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("a channel flip must read unknown until its own fetch lands: %v %v", available, latest)
	}
	waitUpdateSettled(t, s)
	if _, latest := s.updateStatus(); latest == nil || *latest != "v0.3.0-rc1" {
		t.Fatalf("beta channel must pick the prerelease: %v", latest)
	}
}

// ── the wire faces: /api/version + GET /api/release/check ───────────────────

func TestVersionEndpointReflectsReleaseState(t *testing.T) {
	api, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	gh := newFakeGitHub(t, fakeRelease{tag: "v9.9.9"})
	api.releaseAPIBase = gh.srv.URL

	// First probe kicks the check; settle, then the public probe reports the
	// real newer release.
	doJSON(t, "GET", srv.URL+"/api/version", "", "")
	waitUpdateSettled(t, api)
	status, data := doJSON(t, "GET", srv.URL+"/api/version", "", "")
	if status != 200 || data["update_available"] != true || data["latest_version"] != "v9.9.9" {
		t.Fatalf("version face: %d %v", status, data)
	}
}

func TestReleaseCheckButtonVerdicts(t *testing.T) {
	api, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	token := ownerLogin(t, srv.URL, "settings-pass")
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.5.0"})
	api.releaseAPIBase = gh.srv.URL

	// Owner-gated: anonymous is refused.
	if status, _ := doJSON(t, "GET", srv.URL+"/api/release/check", "", ""); status != 401 {
		t.Fatalf("anonymous check must 401: %d", status)
	}

	// Newer release: update_available + tag + the human release link.
	status, data := doJSON(t, "GET", srv.URL+"/api/release/check", token, "")
	if status != 200 || data["status"] != "update_available" ||
		data["latest_tag"] != "v0.5.0" || data["release_url"] != gh.srv.URL+"/rel/v0.5.0" {
		t.Fatalf("update_available verdict: %d %v", status, data)
	}
	if data["current_version"] != appVersion {
		t.Fatalf("current_version must mirror /api/version: %v", data)
	}

	// Running build IS the latest tag → up_to_date.
	setAppVersion(t, "v0.5.0")
	status, data = doJSON(t, "GET", srv.URL+"/api/release/check", token, "")
	if status != 200 || data["status"] != "up_to_date" || data["latest_tag"] != "v0.5.0" {
		t.Fatalf("up_to_date verdict: %d %v", status, data)
	}

	// GitHub down: the button reaches out fresh (cache expired) and answers
	// the honest degraded "unknown" — still a 200, never a 5xx.
	gh.setStatus(http.StatusBadGateway)
	expireUpdateCache(api)
	status, data = doJSON(t, "GET", srv.URL+"/api/release/check", token, "")
	if status != 200 || data["status"] != "unknown" || data["latest_tag"] != nil {
		t.Fatalf("unknown verdict: %d %v", status, data)
	}
}

func TestReleaseCheckNothingPublishedReadsUpToDate(t *testing.T) {
	api, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	token := ownerLogin(t, srv.URL, "settings-pass")
	gh := newFakeGitHub(t) // empty release list
	api.releaseAPIBase = gh.srv.URL

	status, data := doJSON(t, "GET", srv.URL+"/api/release/check", token, "")
	if status != 200 || data["status"] != "up_to_date" ||
		data["latest_tag"] != nil || data["release_url"] != nil {
		t.Fatalf("no-release verdict: %d %v", status, data)
	}
}

func TestReleaseCheckButtonReusesFreshCache(t *testing.T) {
	api, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	token := ownerLogin(t, srv.URL, "settings-pass")
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.6.0"})
	api.releaseAPIBase = gh.srv.URL

	for i := 0; i < 3; i++ {
		if status, _ := doJSON(t, "GET", srv.URL+"/api/release/check", token, ""); status != 200 {
			t.Fatalf("check %d: %d", i, status)
		}
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if gh.hits != 1 {
		t.Fatalf("button mashing must reuse the fresh cache: %d GitHub hits", gh.hits)
	}
}

// ── list ordering: the newest release is the SEMVER max, not row 0 (T-05ab) ──
//
// GitHub orders /releases by CREATION time, not by version. These three tests
// are the guardrail behind that: each isolates ONE property and asserts it as
// its first (and effectively only) claim, so a failure names the property.

// TestFetchPicksSemverMaxNotFirstRow is Joey's reported symptom, reduced: the
// list is out of version order and the real newest release sits SECOND. Taking
// list[0] answers v0.4.1 — which, to a v0.4.2 server, reads "you are already
// up to date" while a v0.9.9 sits published. Underreporting, not a downgrade,
// but the user never learns the release exists.
func TestFetchPicksSemverMaxNotFirstRow(t *testing.T) {
	gh := newFakeGitHub(t,
		fakeRelease{tag: "v0.4.1"},
		fakeRelease{tag: "v0.9.9"},
	)
	rel, none, err := fetchLatestOffiCraftRelease(gh.srv.URL, false)
	if err != nil || none {
		t.Fatalf("fetch: err=%v none=%v", err, none)
	}
	if rel.TagName != "v0.9.9" {
		t.Fatalf("out-of-order list must yield the semver max v0.9.9, got %q", rel.TagName)
	}
}

// TestFetchOrdersV0_10_0AboveV0_9_9 is the case string comparison gets WRONG:
// lexicographically "v0.9.9" > "v0.10.0" (the character '9' outranks '1'), but
// under semver 0.10.0 is the newer minor. A sort-by-string implementation
// passes the test above and fails this one — which is exactly why both exist.
func TestFetchOrdersV0_10_0AboveV0_9_9(t *testing.T) {
	gh := newFakeGitHub(t,
		fakeRelease{tag: "v0.9.9"},
		fakeRelease{tag: "v0.10.0"},
	)
	rel, none, err := fetchLatestOffiCraftRelease(gh.srv.URL, false)
	if err != nil || none {
		t.Fatalf("fetch: err=%v none=%v", err, none)
	}
	if rel.TagName != "v0.10.0" {
		t.Fatalf("v0.10.0 must outrank v0.9.9 under SEMVER (string compare says otherwise), got %q", rel.TagName)
	}
}

// TestFetchMaxRespectsDraftAndPrereleaseFilters pins the non-regression the
// max-picker could plausibly have broken: the filters gate ADMISSION, and the
// semver max is taken only among admitted rows. Both inadmissible rows here
// are deliberately the highest tags in the list, so a filter that stopped
// applying would be the winner and this test would name it.
func TestFetchMaxRespectsDraftAndPrereleaseFilters(t *testing.T) {
	gh := newFakeGitHub(t,
		fakeRelease{tag: "v0.2.0"},
		fakeRelease{tag: "v9.9.9", draft: true},
		fakeRelease{tag: "v5.0.0", prerelease: true},
		fakeRelease{tag: "v0.3.0"},
	)

	// Official channel: draft AND prerelease excluded → the max of {0.2.0, 0.3.0}.
	rel, none, err := fetchLatestOffiCraftRelease(gh.srv.URL, false)
	if err != nil || none {
		t.Fatalf("fetch(official): err=%v none=%v", err, none)
	}
	if rel.TagName != "v0.3.0" {
		t.Fatalf("official channel must exclude the draft v9.9.9 and prerelease v5.0.0, got %q", rel.TagName)
	}

	// Beta channel: the prerelease is admitted and IS the max — but the draft
	// stays excluded in every channel (draft is not a channel, it is unpublished).
	rel, none, err = fetchLatestOffiCraftRelease(gh.srv.URL, true)
	if err != nil || none {
		t.Fatalf("fetch(beta): err=%v none=%v", err, none)
	}
	if rel.TagName != "v5.0.0" {
		t.Fatalf("beta channel must admit v5.0.0 yet still exclude the draft v9.9.9, got %q", rel.TagName)
	}
}
