package main

// version_compare_test.go — the T-9374 semver-ordering rule: update_available
// only when latest is STRICTLY newer than the running version; an older tag
// (the auto-downgrade bug) reads false; unorderable labels read false plus a
// log warning and can never reach the download path.

import (
	"bytes"
	"log"
	"net/http"
	"strings"
	"testing"
)

// captureLog redirects the standard logger into a buffer for one test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

// TestReleaseIsNewerOrdering enumerates the comparison rule directly.
func TestReleaseIsNewerOrdering(t *testing.T) {
	cases := []struct {
		name    string
		latest  string
		running string
		want    bool
	}{
		// The bug's own shape: latest LAGS the running build → no update.
		{"older tag regression (the T-9374 downgrade shape)", "v0.4.1", "v0.4.2", false},
		{"equal tags", "v0.4.2", "v0.4.2", false},
		{"strictly newer", "v0.4.3", "v0.4.2", true},
		// v prefix: release tags carry it, and it must survive parsing on
		// BOTH sides — a stripped-or-choked prefix would turn every check
		// into "unparseable → false" and silently kill updates forever.
		{"v-prefixed both sides, newer", "v0.4.2", "v0.4.1", true},
		{"v-prefixed both sides, older", "v0.4.1", "v0.4.2", false},
		{"bare vs v-prefixed", "v0.4.2", "0.4.1", true},
		// Self-build "0.0.0" sorts below any real release → still prompts.
		{"self-build 0.0.0 sees any release as newer", "v0.2.0", "0.0.0", true},
		// Numeric (not lexicographic) field ordering.
		{"numeric ordering v0.10.0 > v0.9.9", "v0.10.0", "v0.9.9", true},
		// Prerelease precedence: v0.4.2-rc1 < v0.4.2.
		{"prerelease is older than its release", "v0.4.2-rc1", "v0.4.2", false},
		{"release is newer than its own prerelease", "v0.4.2", "v0.4.2-rc1", true},
		// Unorderable labels (either side, or both) → false, never true.
		{"unparseable latest", "banana", "v0.4.2", false},
		{"unparseable running (sha-stamped self-build)", "v0.4.2", "3fa826f-dev", false},
		{"unparseable both sides", "banana", "split", false},
		{"empty latest", "", "v0.4.2", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := releaseIsNewer(tc.latest, tc.running); got != tc.want {
				t.Fatalf("releaseIsNewer(%q, %q) = %v, want %v", tc.latest, tc.running, got, tc.want)
			}
		})
	}
}

// TestReleaseIsNewerUnparseableLogsWarning pins the failure path's OWN
// behaviour: false is not enough — the silent-no-update verdict must leave a
// warning in the log (the only trace an operator gets), and the orderable
// path must NOT spam it.
func TestReleaseIsNewerUnparseableLogsWarning(t *testing.T) {
	buf := captureLog(t)
	if releaseIsNewer("not-a-version", "v0.4.2") {
		t.Fatal("unparseable latest must read false")
	}
	if out := buf.String(); !strings.Contains(out, "warning: cannot order versions") ||
		!strings.Contains(out, `"not-a-version"`) || !strings.Contains(out, `"v0.4.2"`) {
		t.Fatalf("unparseable comparison must log a warning naming both labels, got: %q", out)
	}

	buf.Reset()
	if releaseIsNewer("v0.4.1", "banana2") {
		t.Fatal("unparseable running must read false")
	}
	if !strings.Contains(buf.String(), "warning: cannot order versions") {
		t.Fatalf("unparseable running side must log the warning, got: %q", buf.String())
	}

	buf.Reset()
	_ = releaseIsNewer("v0.4.3", "v0.4.2")
	if strings.Contains(buf.String(), "cannot order versions") {
		t.Fatalf("an orderable comparison must not warn, got: %q", buf.String())
	}
}

// ── updateStatus (/api/version's face) under the new rule ────────────────────

// TestUpdateStatusOlderTagReadsUpToDate is THE acceptance regression for
// T-9374: the cached "latest" lags the running build (v0.4.1 vs v0.4.2 — the
// exact prod observation) and update_available must be false.
func TestUpdateStatusOlderTagReadsUpToDate(t *testing.T) {
	setAppVersion(t, "v0.4.2")
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.4.1"})
	s := &apiServer{releaseAPIBase: gh.srv.URL}
	s.updateStatus()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("latest v0.4.1 vs running v0.4.2 must read (false, nil): %v %v", available, latest)
	}
}

// TestUpdateStatusVPrefixedNewerTagPrompts pins the v-prefix happy path end
// to end: real release tags are "vX.Y.Z" on BOTH sides, and the check must
// still order them (a prefix-choked parser would read false here forever).
func TestUpdateStatusVPrefixedNewerTagPrompts(t *testing.T) {
	setAppVersion(t, "v0.4.1")
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.4.2"})
	s := &apiServer{releaseAPIBase: gh.srv.URL}
	s.updateStatus()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); !available || latest == nil || *latest != "v0.4.2" {
		t.Fatalf("v0.4.2 over v0.4.1 must prompt: %v %v", available, latest)
	}
}

// TestUpdateStatusSelfBuildPrompts: the self-build "0.0.0" sorts below any
// release and keeps prompting (behaviour preserved under the new ordering).
func TestUpdateStatusSelfBuildPrompts(t *testing.T) {
	setAppVersion(t, "0.0.0")
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.2.0"})
	s := &apiServer{releaseAPIBase: gh.srv.URL}
	s.updateStatus()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); !available || latest == nil || *latest != "v0.2.0" {
		t.Fatalf("self-build must still be prompted to v0.2.0: %v %v", available, latest)
	}
}

// TestUpdateStatusUnparseableTagReadsFalseAndWarns: an unorderable release
// tag must degrade to the quiet (false, nil) — plus the log warning — and
// never present as an update.
func TestUpdateStatusUnparseableTagReadsFalseAndWarns(t *testing.T) {
	setAppVersion(t, "v0.4.2")
	gh := newFakeGitHub(t, fakeRelease{tag: "nightly-build"})
	s := &apiServer{releaseAPIBase: gh.srv.URL}
	s.updateStatus()
	waitUpdateSettled(t, s)
	buf := captureLog(t)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("unparseable tag must read (false, nil): %v %v", available, latest)
	}
	if out := buf.String(); !strings.Contains(out, "warning: cannot order versions") ||
		!strings.Contains(out, `"nightly-build"`) {
		t.Fatalf("unparseable tag must warn in the log, got: %q", out)
	}
}

// TestUpdateStatusPrereleaseOfRunningReadsUpToDate: with the beta channel on,
// the running release's OWN prerelease (v0.4.2-rc1 < v0.4.2) is not an
// update.
func TestUpdateStatusPrereleaseOfRunningReadsUpToDate(t *testing.T) {
	setAppVersion(t, "v0.4.2")
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.4.2-rc1", prerelease: true})
	s := &apiServer{releaseAPIBase: gh.srv.URL}
	s.settingsMu.Lock()
	s.updaterReceiveBeta = true
	s.settingsMu.Unlock()
	s.updateStatus()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("v0.4.2-rc1 vs running v0.4.2 must read (false, nil): %v %v", available, latest)
	}
}

// ── the 檢查更新 button under the new rule ───────────────────────────────────

// TestReleaseCheckOlderTagReadsUpToDate: the explicit button on the same
// lagging-latest shape answers up_to_date (while still SHOWING the tag).
func TestReleaseCheckOlderTagReadsUpToDate(t *testing.T) {
	setAppVersion(t, "v0.4.2")
	api, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	token := ownerLogin(t, srv.URL, "settings-pass")
	gh := newFakeGitHub(t, fakeRelease{tag: "v0.4.1"})
	api.releaseAPIBase = gh.srv.URL

	status, data := doJSON(t, "GET", srv.URL+"/api/release/check", token, "")
	if status != 200 || data["status"] != "up_to_date" || data["latest_tag"] != "v0.4.1" {
		t.Fatalf("older-latest button verdict must be up_to_date: %d %v", status, data)
	}
}

// ── the upgrade honesty gate (upgrade.go): downgrades structurally blocked ───

// TestUpgradePinnedOlderReleaseAnswers409 is the downgrade-block teeth: the
// CACHE believes a newer build exists (poisoned, the stale-cache race), but
// the trigger-time pin fetches an OLDER release than the running build — the
// gate must refuse (409) and the old binary must be untouched. This is the
// exact path that used to auto-downgrade armed machines.
func TestUpgradePinnedOlderReleaseAnswers409(t *testing.T) {
	api, srvURL, token, exePath, restarted := newUpgradeTestServer(t)
	setAppVersion(t, "v0.4.2")
	gh := githubWithRelease(t, "v0.4.1", smokePassingBinary, "", true)
	pointAtGitHub(t, api, gh)
	// Poison the cache so the precondition gate (updateStatus) says "newer
	// known" — only the pin-time gate stands between us and a downgrade.
	api.updateMu.Lock()
	api.updateCheck.rel.TagName = "v9.9.9-phantom"
	api.updateMu.Unlock()

	status, _ := doJSON(t, "POST", srvURL+"/api/update/upgrade", token, "")
	if status != http.StatusConflict {
		t.Fatalf("pinned-older-than-running must 409, never downgrade: %d", status)
	}
	assertUpgradeUntouched(t, exePath, restarted)
}

// TestAutoUpdateTickNeverDowngrades runs the SAME lagging-latest shape
// through the armed auto-update cadence: the tick must not act at all
// (updateStatus already reads false), and the binary must be untouched.
func TestAutoUpdateTickNeverDowngrades(t *testing.T) {
	api, _, _, exePath, restarted := newUpgradeTestServer(t)
	setAppVersion(t, "v0.4.2")
	api.settingsMu.Lock()
	api.updaterAutoUpdate = true
	api.settingsMu.Unlock()
	gh := githubWithRelease(t, "v0.4.1", smokePassingBinary, "", true)
	pointAtGitHub(t, api, gh)

	if acted := api.autoUpdateTick(); acted {
		t.Fatal("an armed tick must not act on an OLDER latest")
	}
	assertUpgradeUntouched(t, exePath, restarted)
}
