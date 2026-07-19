package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeUpdater spins an httptest ocupdaterd-shaped /api/latest: 200 with the
// given release for the right invite code, 401 otherwise, 404 when version is
// "" (nothing published).
func fakeUpdater(t *testing.T, inviteCode, version, gitSHA string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/latest" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+inviteCode {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"bad invite"}}`)
			return
		}
		if version == "" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"code":"not_found","message":"no version has been published yet"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": version, "git_sha": gitSHA, "sha256": strings.Repeat("a", 64),
			"size": 1, "notes": "", "published_at": 1.0,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
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

func setUpdater(s *apiServer, url, code string) {
	s.settingsMu.Lock()
	s.updaterURL = url
	s.updaterInviteCode = code
	s.settingsMu.Unlock()
}

func TestUpdateStatusUnconfiguredStaysHonestStatic(t *testing.T) {
	s := &apiServer{processSHA: "runsha"}
	available, latest := s.updateStatus()
	if available || latest != nil {
		t.Fatalf("unconfigured must be (false, nil): %v %v", available, latest)
	}
}

func TestUpdateStatusReportsNewerVersion(t *testing.T) {
	up := fakeUpdater(t, "code-1", "1.2.3", "newsha")
	s := &apiServer{processSHA: "runsha"}
	setUpdater(s, up.URL, "code-1")

	// First read serves the (empty) cache and kicks the background fetch.
	if available, _ := s.updateStatus(); available {
		t.Fatal("first read must serve the not-yet-fetched cache (false)")
	}
	waitUpdateSettled(t, s)
	available, latest := s.updateStatus()
	if !available || latest == nil || *latest != "1.2.3" {
		t.Fatalf("want (true, 1.2.3): %v %v", available, latest)
	}
}

func TestUpdateStatusSameGitShaIsUpToDate(t *testing.T) {
	// The updater's latest IS the running build (same git sha) — flagging an
	// update would be a lie even though the version label differs.
	up := fakeUpdater(t, "code-1", "1.2.3", "runsha")
	s := &apiServer{processSHA: "runsha"}
	setUpdater(s, up.URL, "code-1")
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("same-sha publish must read up-to-date: %v %v", available, latest)
	}
}

func TestUpdateStatusNothingPublishedIsUpToDate(t *testing.T) {
	up := fakeUpdater(t, "code-1", "", "")
	s := &apiServer{processSHA: "runsha"}
	setUpdater(s, up.URL, "code-1")
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("404 nothing-published must read (false, nil): %v %v", available, latest)
	}
}

func TestUpdateStatusDegradesGracefullyOnDeadUpdater(t *testing.T) {
	// A closed port: the fetch fails; /api/version's answer stays honest-empty
	// and the read path never blocks on the network.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	s := &apiServer{processSHA: "runsha"}
	setUpdater(s, deadURL, "code-1")
	start := time.Now()
	available, latest := s.updateStatus()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("updateStatus blocked on the network: %v", elapsed)
	}
	if available || latest != nil {
		t.Fatalf("dead updater must degrade to (false, nil): %v %v", available, latest)
	}
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("after the failed fetch still (false, nil): %v %v", available, latest)
	}
	// Failure keeps last-known state AND stamps checkedAt — the dead host is
	// not re-polled before the TTL (exactly one settled attempt).
	s.updateMu.Lock()
	stamped := !s.updateCheck.checkedAt.IsZero()
	s.updateMu.Unlock()
	if !stamped {
		t.Fatal("failed fetch must stamp checkedAt (TTL backoff)")
	}
}

func TestUpdateStatusKeepsLastKnownAcrossFailure(t *testing.T) {
	up := fakeUpdater(t, "code-1", "9.9.9", "newsha")
	s := &apiServer{processSHA: "runsha"}
	setUpdater(s, up.URL, "code-1")
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	if available, _ := s.updateStatus(); !available {
		t.Fatal("precondition: update known")
	}
	// The updater dies; a forced re-check fails — the last-known newer version
	// STANDS (stale-but-honest beats fabricated-empty).
	up.Close()
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	available, latest := s.updateStatus()
	if !available || latest == nil || *latest != "9.9.9" {
		t.Fatalf("failure must keep last-known (true, 9.9.9): %v %v", available, latest)
	}
}

func TestUpdateStatusConfigChangeResetsCache(t *testing.T) {
	up := fakeUpdater(t, "code-1", "1.0.1", "newsha")
	s := &apiServer{processSHA: "runsha"}
	setUpdater(s, up.URL, "code-1")
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	if available, _ := s.updateStatus(); !available {
		t.Fatal("precondition: update known")
	}
	// Clearing the updater must drop straight back to honest-static — a
	// result fetched under the old config never leaks through.
	setUpdater(s, "", "")
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("cleared config must read (false, nil): %v %v", available, latest)
	}
}

func TestValidateUpdaterURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", true},
		{"   ", "", true},
		{"http://127.0.0.1:8790", "http://127.0.0.1:8790", true},
		{"https://updates.example.com/", "https://updates.example.com", true},
		{"ftp://example.com", "", false},
		{"not a url", "", false},
		{"http://", "", false},
		{"/relative/path", "", false},
	}
	for _, c := range cases {
		got, ok := validateUpdaterURL(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("validateUpdaterURL(%q) = (%q, %v), want (%q, %v)",
				c.in, got, ok, c.want, c.ok)
		}
	}
}

// ── the wire faces: settings PATCH secret masking + /api/version + upgrade ──

func ownerLogin(t *testing.T, srvURL, password string) string {
	t.Helper()
	status, data := doJSON(t, "POST", srvURL+"/api/login", "",
		`{"password":"`+password+`"}`)
	if status != 200 {
		t.Fatalf("login: %d %v", status, data)
	}
	return data["token"].(string)
}

func TestSettingsPatchUpdaterFieldsAndSecretMasking(t *testing.T) {
	api, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	token := ownerLogin(t, srv.URL, "settings-pass")

	// Baseline read: unset.
	status, data := doJSON(t, "GET", srv.URL+"/api/settings", token, "")
	if status != 200 || data["updater_url"] != "" || data["updater_invite_code_set"] != false {
		t.Fatalf("baseline: %d %v", status, data)
	}

	// PATCH both; the response must expose the URL but NEVER the code — only
	// the set flag.
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", token,
		`{"updater_url":"http://127.0.0.1:19999/","updater_invite_code":"super-secret-invite"}`)
	if status != 200 {
		t.Fatalf("patch: %d %v", status, data)
	}
	if data["updater_url"] != "http://127.0.0.1:19999" {
		t.Fatalf("url not normalized/echoed: %v", data)
	}
	if data["updater_invite_code_set"] != true {
		t.Fatalf("set flag must flip true: %v", data)
	}
	if _, leaked := data["updater_invite_code"]; leaked {
		t.Fatalf("SECRET invite code leaked on the wire: %v", data)
	}
	raw, _ := json.Marshal(data)
	if strings.Contains(string(raw), "super-secret-invite") {
		t.Fatalf("SECRET invite code value leaked: %s", raw)
	}

	// GET reads back the same masked face; durability: a fresh snapshot load
	// sees both values.
	status, data = doJSON(t, "GET", srv.URL+"/api/settings", token, "")
	if status != 200 || data["updater_url"] != "http://127.0.0.1:19999" ||
		data["updater_invite_code_set"] != true {
		t.Fatalf("read-back: %d %v", status, data)
	}
	auth, err := loadAuthSettings(api.dal, defaultConfig(), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if auth.updaterURL != "http://127.0.0.1:19999" || auth.updaterInviteCode != "super-secret-invite" {
		t.Fatalf("settings not durable: %+v", auth)
	}

	// Bad URL → 422, nothing changes.
	status, _ = doJSON(t, "PATCH", srv.URL+"/api/settings", token,
		`{"updater_url":"not a url"}`)
	if status != 422 {
		t.Fatalf("bad url must 422: %d", status)
	}

	// "" clears both.
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", token,
		`{"updater_url":"","updater_invite_code":""}`)
	if status != 200 || data["updater_url"] != "" || data["updater_invite_code_set"] != false {
		t.Fatalf("clear: %d %v", status, data)
	}
}

func TestVersionEndpointReflectsUpdaterState(t *testing.T) {
	api, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	token := ownerLogin(t, srv.URL, "settings-pass")

	// Unconfigured: the M1 honest-static face.
	status, data := doJSON(t, "GET", srv.URL+"/api/version", "", "")
	if status != 200 || data["update_available"] != false || data["latest_version"] != nil {
		t.Fatalf("unconfigured version face: %d %v", status, data)
	}

	up := fakeUpdater(t, "invite-1", "2.0.0", "somesha")
	status, _ = doJSON(t, "PATCH", srv.URL+"/api/settings", token,
		`{"updater_url":"`+up.URL+`","updater_invite_code":"invite-1"}`)
	if status != 200 {
		t.Fatalf("patch: %d", status)
	}
	// The PATCH kicked a background check; settle, then the public probe
	// reports the real newer version.
	waitUpdateSettled(t, api)
	status, data = doJSON(t, "GET", srv.URL+"/api/version", "", "")
	if status != 200 || data["update_available"] != true || data["latest_version"] != "2.0.0" {
		t.Fatalf("configured version face: %d %v", status, data)
	}
}

// TestVersionEndpointSurfacesReleaseTag: when the updater speaks the T-e9d1
// serial (release_tag on latest + current_release_tag from the self-lookup),
// /api/version shows the running build's r-N in release_tag and the newest r-N
// in latest_version.
func TestVersionEndpointSurfacesReleaseTag(t *testing.T) {
	api, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	token := ownerLogin(t, srv.URL, "settings-pass")

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/latest" || r.Header.Get("Authorization") != "Bearer invite-1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Latest is r-99 on a different sha (→ update available); the caller's
		// own build resolves to r-42 via the self-lookup.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": "v260715-0099", "release_tag": "r-99", "git_sha": "newsha99",
			"current_release_tag": "r-42", "current_serial": 42,
			"sha256": strings.Repeat("a", 64), "size": 1, "published_at": 1.0,
		})
	}))
	t.Cleanup(up.Close)

	status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", token,
		`{"updater_url":"`+up.URL+`","updater_invite_code":"invite-1"}`)
	if status != 200 {
		t.Fatalf("patch: %d", status)
	}
	waitUpdateSettled(t, api)
	status, data := doJSON(t, "GET", srv.URL+"/api/version", "", "")
	if status != 200 {
		t.Fatalf("version: %d", status)
	}
	if data["latest_version"] != "r-99" {
		t.Fatalf("latest_version must be the r-N tag: %v", data["latest_version"])
	}
	if data["release_tag"] != "r-42" {
		t.Fatalf("release_tag must be the running build's r-N: %v", data["release_tag"])
	}
	if data["update_available"] != true {
		t.Fatalf("a newer sha must read update_available: %v", data["update_available"])
	}
}

func TestUpgradeEndpointFaces(t *testing.T) {
	api, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	token := ownerLogin(t, srv.URL, "settings-pass")

	// Owner-gated: anonymous 401.
	status, _ := doJSON(t, "POST", srv.URL+"/api/update/upgrade", "", "")
	if status != 401 {
		t.Fatalf("anonymous upgrade must 401: %d", status)
	}

	// Unconfigured → 409.
	status, data := doJSON(t, "POST", srv.URL+"/api/update/upgrade", token, "")
	if status != 409 {
		t.Fatalf("unconfigured upgrade must 409: %d %v", status, data)
	}

	// Configured but nothing newer (same git sha as the running build) → 409.
	same := fakeUpdater(t, "invite-1", "2.0.0", api.processSHA)
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", token,
		`{"updater_url":"`+same.URL+`","updater_invite_code":"invite-1"}`); status != 200 {
		t.Fatalf("patch: %d", status)
	}
	waitUpdateSettled(t, api)
	status, data = doJSON(t, "POST", srv.URL+"/api/update/upgrade", token, "")
	if status != 409 {
		t.Fatalf("no-newer upgrade must 409: %d %v", status, data)
	}

	// A real newer version now runs the EXECUTION body (upgrade.go) — the
	// success/failure faces live in upgrade_test.go. Here: the fakeUpdater
	// answers /api/latest but 404s /api/binary, so a valid trigger reaches
	// execution and fails HONESTLY at the download, old binary untouched.
	newer := fakeUpdater(t, "invite-2", "2.0.1", "another-sha")
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", token,
		`{"updater_url":"`+newer.URL+`","updater_invite_code":"invite-2"}`); status != 200 {
		t.Fatalf("patch: %d", status)
	}
	waitUpdateSettled(t, api)
	status, data = doJSON(t, "POST", srv.URL+"/api/update/upgrade", token, "")
	if status != 502 {
		t.Fatalf("execution reached but download impossible must 502: %d %v", status, data)
	}
	envelope, _ := data["error"].(map[string]any)
	if envelope == nil || envelope["message"] == "" {
		t.Fatalf("502 must ride the unified envelope: %v", data)
	}
}

// fakeChannelUpdater is an httptest ocupdaterd whose /api/latest answers PER
// CHANNEL: a ga version (may be "" = nothing promoted yet → 404) and a beta
// version. The default (missing channel param) is GA, mirroring the real
// daemon's D1 posture.
func fakeChannelUpdater(t *testing.T, inviteCode, gaVersion, betaVersion string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/latest" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+inviteCode {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		version := gaVersion
		if r.URL.Query().Get("channel") == "beta" {
			version = betaVersion
		}
		if version == "" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"code":"not_found","message":"nothing on this channel yet"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": version, "git_sha": "sha-" + version, "sha256": strings.Repeat("a", 64),
			"size": 1, "notes": "", "published_at": 1.0,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestUpdateCheckFollowsChannelToggle is the client half of the dual-channel
// split: with receive_beta OFF the check follows GA (a beta-only publish is
// INVISIBLE — update_available stays false); flipping the toggle re-keys the
// cache and the same updater now reports the beta version.
func TestUpdateCheckFollowsChannelToggle(t *testing.T) {
	// GA has nothing promoted yet; beta has a fresh publish.
	up := fakeChannelUpdater(t, "code-1", "", "v260714-0002")
	s := &apiServer{processSHA: "runsha"}
	setUpdater(s, up.URL, "code-1")

	// GA follower (default): the beta publish must NOT read as an update.
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("GA follower must not see a beta-only publish: %v %v", available, latest)
	}

	// Flip receive_beta: the cache re-keys (config change) and the SAME
	// updater now answers with the beta version.
	s.settingsMu.Lock()
	s.updaterReceiveBeta = true
	s.settingsMu.Unlock()
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	available, latest := s.updateStatus()
	if !available || latest == nil || *latest != "v260714-0002" {
		t.Fatalf("beta follower must see the beta publish: %v %v", available, latest)
	}

	// Flip back: GA emptiness again (the beta cache result must not leak
	// across the channel change).
	s.settingsMu.Lock()
	s.updaterReceiveBeta = false
	s.settingsMu.Unlock()
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)
	if available, latest := s.updateStatus(); available || latest != nil {
		t.Fatalf("back on GA the beta version must vanish: %v %v", available, latest)
	}
}

// TestSettingsPatchChannelAndAutoUpdateToggles drives the two new knobs over
// the real PATCH/GET wire: default false, PATCH true → durable (DB) + live
// (view), PATCH false → back off; the receive_beta flip re-keys the check
// cache onto the beta channel.
func TestSettingsPatchChannelAndAutoUpdateToggles(t *testing.T) {
	api, srv, d, _ := newSettingsTestServer(t, "hunter2secure")
	token := ownerLogin(t, srv.URL, "hunter2secure")

	status, data := doJSON(t, "GET", srv.URL+"/api/settings", token, "")
	if status != 200 || data["updater_receive_beta"] != false || data["updater_auto_update"] != false {
		t.Fatalf("both toggles must default false: %d %v", status, data)
	}

	up := fakeChannelUpdater(t, "inv-1", "", "v260714-0003")
	status, _ = doJSON(t, "PATCH", srv.URL+"/api/settings", token, fmt.Sprintf(
		`{"updater_url":%q,"updater_invite_code":"inv-1"}`, up.URL))
	if status != 200 {
		t.Fatalf("settings patch (updater config): %d", status)
	}
	waitUpdateSettled(t, api)

	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", token,
		`{"updater_receive_beta":true,"updater_auto_update":true}`)
	if status != 200 || data["updater_receive_beta"] != true || data["updater_auto_update"] != true {
		t.Fatalf("PATCH toggles on: %d %v", status, data)
	}
	// Durable: the DB rows carry the values (a restart's loadAuthSettings
	// reads them back — locked in TestLoadAuthSettingsReadsUpdaterToggles).
	for _, key := range []string{settingUpdaterReceiveBeta, settingUpdaterAutoUpdate} {
		if v, err := d.GetSetting(key); err != nil || v == nil || *v != "true" {
			t.Fatalf("setting %s must be durably true: %v %v", key, v, err)
		}
	}
	// Live: the receive_beta flip kicked the check onto the beta channel.
	waitUpdateSettled(t, api)
	if available, latest := api.updateStatus(); !available || latest == nil || *latest != "v260714-0003" {
		t.Fatalf("after the beta flip the check must follow beta: %v %v", available, latest)
	}

	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", token,
		`{"updater_receive_beta":false,"updater_auto_update":false}`)
	if status != 200 || data["updater_receive_beta"] != false || data["updater_auto_update"] != false {
		t.Fatalf("PATCH toggles off: %d %v", status, data)
	}
}

// TestLoadAuthSettingsReadsUpdaterToggles: the boot snapshot reads both
// toggles back from the DB (restart durability's other half).
func TestLoadAuthSettingsReadsUpdaterToggles(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PutSetting(settingUpdaterReceiveBeta, "true"); err != nil {
		t.Fatal(err)
	}
	if err := d.PutSetting(settingUpdaterAutoUpdate, "true"); err != nil {
		t.Fatal(err)
	}
	auth, err := loadAuthSettings(d, defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings: %v", err)
	}
	if !auth.updaterReceiveBeta || !auth.updaterAutoUpdate {
		t.Fatalf("snapshot must carry both toggles true: %+v", auth)
	}
	// A garbage value is a load-time error, not a silent default.
	if err := d.PutSetting(settingUpdaterAutoUpdate, "banana"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthSettings(d, defaultConfig(), func(string) {}); err == nil {
		t.Fatal("a non-bool updater.auto_update must refuse to load")
	}
}

// TestUpdateCheckSelfReportsRunningBuild: the outbound /api/latest carries
// current_version/current_sha (the updater's fleet monitor records what each
// invite is running — the git sha is the honest build identity; appVersion is
// the constant label). Locks the wire the ocupdaterd portal depends on.
func TestUpdateCheckSelfReportsRunningBuild(t *testing.T) {
	seen := make(chan map[string]string, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/latest" {
			http.NotFound(w, r)
			return
		}
		seen <- map[string]string{
			"channel":         r.URL.Query().Get("channel"),
			"current_version": r.URL.Query().Get("current_version"),
			"current_sha":     r.URL.Query().Get("current_sha"),
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"code":"not_found","message":"no version has been published yet"}}`)
	}))
	t.Cleanup(up.Close)

	s := &apiServer{processSHA: "runsha"}
	setUpdater(s, up.URL, "code-1")
	s.kickUpdateCheck()
	waitUpdateSettled(t, s)

	select {
	case q := <-seen:
		if q["channel"] != "ga" || q["current_version"] != appVersion || q["current_sha"] != "runsha" {
			t.Fatalf("self-report params wrong: %v", q)
		}
	default:
		t.Fatal("the updater never saw the check")
	}
}
