package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newSettingsTestServer assembles the full handler stack (auth gate + RBAC
// choke + owner-iat floor) over a fresh DB. password == "" is the first-run
// shape (no hash, claim token minted); otherwise the hash is migrated in and
// no claim token exists.
func newSettingsTestServer(t *testing.T, password string) (*apiServer, *httptest.Server, *DAL, string) {
	t.Helper()
	d := newTestDAL(t)
	cfg := defaultConfig()
	cfg.Auth.Password = password
	auth, _ := loadForTest(t, d, cfg)
	claim, err := ensureFirstRunClaimToken(d, auth.passwordHash != "", func(string) {})
	if err != nil {
		t.Fatalf("ensureFirstRunClaimToken: %v", err)
	}
	api := newAPIServer(d, NewHub(), singleKeyring(auth.secret), auth.ownerTokenTTL, "../..")
	api.agentTokenTTL = auth.agentTokenTTL
	api.passwordHash = auth.passwordHash
	api.passwordChangedAt = auth.passwordChangedAt
	api.ctxhigh = auth.ctxhigh
	h, err := buildHandler(specsFor(api), api.keys, d.GetMember, api.authPasswordChangedAt)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return api, srv, d, claim
}

func doJSON(t *testing.T, method, url, token, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("non-JSON body (%d): %s", resp.StatusCode, raw)
		}
	}
	data, _ := parsed.(map[string]any)
	return resp.StatusCode, data
}

func TestAuthStatusReflectsPasswordState(t *testing.T) {
	_, srv, _, claim := newSettingsTestServer(t, "")

	status, data := doJSON(t, "GET", srv.URL+"/api/auth/status", "", "")
	if status != 200 || data["password_set"] != false {
		t.Fatalf("first run must report password_set=false: %d %v", status, data)
	}

	status, _ = doJSON(t, "POST", srv.URL+"/api/auth/set-password", "",
		`{"password":"first-run-pass","claim_token":"`+claim+`"}`)
	if status != 200 {
		t.Fatalf("set-password: %d", status)
	}
	status, data = doJSON(t, "GET", srv.URL+"/api/auth/status", "", "")
	if status != 200 || data["password_set"] != true {
		t.Fatalf("status must flip live after set-password: %d %v", status, data)
	}
}

func TestSetPasswordConsumesClaimTokenAndLogsIn(t *testing.T) {
	_, srv, d, claim := newSettingsTestServer(t, "")
	if claim == "" {
		t.Fatal("a first-run server must mint a claim token")
	}

	// Wrong claim token → 401; nothing set.
	if status, _ := doJSON(t, "POST", srv.URL+"/api/auth/set-password", "",
		`{"password":"first-run-pass","claim_token":"wrong"}`); status != 401 {
		t.Fatalf("wrong claim token: want 401, got %d", status)
	}
	// Short password → 422 before the claim token is consulted.
	if status, _ := doJSON(t, "POST", srv.URL+"/api/auth/set-password", "",
		`{"password":"short","claim_token":"`+claim+`"}`); status != 422 {
		t.Fatalf("short password: want 422, got %d", status)
	}

	status, data := doJSON(t, "POST", srv.URL+"/api/auth/set-password", "",
		`{"password":"first-run-pass","claim_token":"`+claim+`"}`)
	if status != 200 || data["token"] == nil || data["token_type"] != "bearer" {
		t.Fatalf("set-password must mint an owner token: %d %v", status, data)
	}
	owner := data["token"].(string)

	// The minted token is a live owner session (an owner-gated route works).
	if status, _ := doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 {
		t.Fatalf("the set-password token must pass owner routes: %d", status)
	}
	// The one-shot token is consumed.
	if v, err := d.GetSetting(settingClaimToken); err != nil || v != nil {
		t.Fatalf("the claim token must be deleted on success: %v %v", v, err)
	}
	// Login with the new password works.
	if status, _ := doJSON(t, "POST", srv.URL+"/api/login", "",
		`{"password":"first-run-pass"}`); status != 200 {
		t.Fatalf("login with the set password: %d", status)
	}
	// A second claim (any token) is a flat 409 — already set.
	if status, _ := doJSON(t, "POST", srv.URL+"/api/auth/set-password", "",
		`{"password":"stomp-pass-123","claim_token":"`+claim+`"}`); status != 409 {
		t.Fatalf("second set-password: want 409, got %d", status)
	}
}

func TestChangePasswordRevokesPreChangeOwnerTokens(t *testing.T) {
	api, srv, _, _ := newSettingsTestServer(t, "old-password")

	// An owner token and an agent token minted BEFORE the change (iat in the
	// past — a same-second change must not mask the revocation).
	past := time.Now().Unix() - 10
	oldOwner, err := mintJWT(wireOwnerID, "owner", 86400, api.keys.signingSecret(), past, "")
	if err != nil {
		t.Fatal(err)
	}
	agentToken, err := mintJWT("kyle", "agent", 86400, api.keys.signingSecret(), past, "")
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := doJSON(t, "GET", srv.URL+"/api/settings", oldOwner, ""); status != 200 {
		t.Fatalf("pre-change owner token must work before the change: %d", status)
	}

	// Wrong current password → 401, credential unchanged.
	if status, _ := doJSON(t, "POST", srv.URL+"/api/auth/change-password", oldOwner,
		`{"current_password":"wrong","new_password":"new-password-1"}`); status != 401 {
		t.Fatalf("wrong current password: want 401, got %d", status)
	}
	if status, _ := doJSON(t, "POST", srv.URL+"/api/login", "",
		`{"password":"old-password"}`); status != 200 {
		t.Fatalf("a failed change must leave the old password valid: %d", status)
	}

	status, data := doJSON(t, "POST", srv.URL+"/api/auth/change-password", oldOwner,
		`{"current_password":"old-password","new_password":"new-password-1"}`)
	if status != 200 || data["token"] == nil {
		t.Fatalf("change-password: %d %v", status, data)
	}
	fresh := data["token"].(string)

	// Old owner token is revoked (iat < changed_at); the fresh one works.
	if status, _ := doJSON(t, "GET", srv.URL+"/api/settings", oldOwner, ""); status != 401 {
		t.Fatalf("a pre-change owner token must be refused: %d", status)
	}
	if status, _ := doJSON(t, "GET", srv.URL+"/api/settings", fresh, ""); status != 200 {
		t.Fatalf("the fresh owner token must work: %d", status)
	}
	// Agent tokens are untouched (secret never rotates, no iat floor for them).
	if status, _ := doJSON(t, "GET", srv.URL+"/api/members", agentToken, ""); status != 200 {
		t.Fatalf("a pre-change agent token must survive: %d", status)
	}
	// Old password no longer logs in; the new one does.
	if status, _ := doJSON(t, "POST", srv.URL+"/api/login", "",
		`{"password":"old-password"}`); status != 401 {
		t.Fatalf("old password after change: want 401, got %d", status)
	}
	if status, _ := doJSON(t, "POST", srv.URL+"/api/login", "",
		`{"password":"new-password-1"}`); status != 200 {
		t.Fatalf("new password must log in: %d", status)
	}
	// Short new password → 422.
	if status, _ := doJSON(t, "POST", srv.URL+"/api/auth/change-password", fresh,
		`{"current_password":"new-password-1","new_password":"short"}`); status != 422 {
		t.Fatalf("short new password: want 422, got %d", status)
	}
}

func TestUpdateSettingsValidatesAndAppliesImmediately(t *testing.T) {
	api, srv, d, _ := newSettingsTestServer(t, "settings-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"settings-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	// GET: defaults.
	status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, "")
	if status != 200 || data["owner_token_ttl"] != float64(86400) || data["agent_token_ttl"] != float64(604800) ||
		data["handover_pct"] != float64(50) {
		t.Fatalf("settings defaults: %d %v", status, data)
	}

	// Invalid values → 422, nothing written.
	for _, body := range []string{
		`{"owner_token_ttl":0}`, `{"agent_token_ttl":3600}`,
		`{"handover_pct":39}`, `{"handover_pct":91}`,
		`{"agent_token_ttl":604800,"handover_pct":10}`, // one bad field poisons the whole patch
	} {
		if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, body); status != 422 {
			t.Fatalf("PATCH %s: want 422, got %d", body, status)
		}
	}
	if v, err := d.GetSetting(settingOwnerTokenTTL); err != nil || v != nil {
		t.Fatalf("a rejected patch must write nothing: %v %v", v, err)
	}

	// Changing the owner-login setting leaves the agent setting untouched.
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"owner_token_ttl":604800,"handover_pct":60}`)
	if status != 200 || data["owner_token_ttl"] != float64(604800) || data["agent_token_ttl"] != float64(604800) ||
		data["handover_pct"] != float64(60) {
		t.Fatalf("PATCH response must echo the new settings: %d %v", status, data)
	}
	if v, err := d.GetSetting(settingOwnerTokenTTL); err != nil || v == nil || *v != "604800" {
		t.Fatalf("owner_token_ttl must be durable: %v %v", v, err)
	}
	if got := api.agentTokenTTLValue(); got != 604800 {
		t.Fatalf("owner patch must not alter agent mint TTL: %d", got)
	}
	if got := api.ctxHighConfig().HandoverPct; got != 60 {
		t.Fatalf("handover_pct must be live: %d", got)
	}
	// The next login mints with the new TTL — no restart.
	status, data = doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"settings-pass"}`)
	if status != 200 || data["expires_in"] != float64(604800) {
		t.Fatalf("login must pick up the patched TTL immediately: %d %v", status, data)
	}

	// Conversely an agent patch applies to future agent mints only, not logins.
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"agent_token_ttl":2592000}`)
	if status != 200 || data["owner_token_ttl"] != float64(604800) || data["agent_token_ttl"] != float64(2592000) {
		t.Fatalf("agent patch must leave owner setting untouched: %d %v", status, data)
	}
	if v, err := d.GetSetting(settingAgentTokenTTL); err != nil || v == nil || *v != "2592000" {
		t.Fatalf("agent_token_ttl must be durable: %v %v", v, err)
	}
	if minted, err := api.mintAgentToken("agent-test", "", api.agentTokenTTLValue()); err != nil {
		t.Fatalf("agent mint: %v", err)
	} else if claims, err := verifyJWT(minted, api.keys.signingSecret(), time.Now().Unix()); err != nil || claims["exp"].(float64)-claims["iat"].(float64) != 2592000 {
		t.Fatalf("agent mint must use patched agent TTL: %+v %v", claims, err)
	}

	// Empty patch = no-op read.
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{}`); status != 200 {
		t.Fatalf("empty patch: %d", status)
	}
}

// TestOrgNameSettingRoundTrips covers the T-d693 studio name: owner writes it
// through PATCH /api/settings (validated, trimmed, durable + live), and every
// agent reads it back through get_global_context (the MCP read path).
func TestOrgNameSettingRoundTrips(t *testing.T) {
	api, srv, d, _ := newSettingsTestServer(t, "org-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"org-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	// Default: unset → "" on both the owner surface and the agent read path.
	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 || data["org_name"] != "" {
		t.Fatalf("org_name default must be \"\": %d %v", status, data)
	}
	if status, data = doJSON(t, "GET", srv.URL+"/api/global-context", owner, ""); status != 200 || data["org_name"] != "" {
		t.Fatalf("global-context org_name default must be \"\": %d %v", status, data)
	}

	// Over the 80-rune cap → 422, nothing written.
	long := `{"org_name":"` + strings.Repeat("水", 81) + `"}`
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, long); status != 422 {
		t.Fatalf("org_name over the cap must 422: got %d", status)
	}
	if v, err := d.GetSetting(settingOrgName); err != nil || v != nil {
		t.Fatalf("a rejected org_name patch must write nothing: %v %v", v, err)
	}

	// Valid patch: trimmed, echoed, durable, live in the snapshot.
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"org_name":"  伊娃工作室  "}`); status != 200 || data["org_name"] != "伊娃工作室" {
		t.Fatalf("org_name patch must trim + echo: %d %v", status, data)
	}
	if v, err := d.GetSetting(settingOrgName); err != nil || v == nil || *v != "伊娃工作室" {
		t.Fatalf("org_name must be durable: %v %v", v, err)
	}
	if got := api.orgNameSnapshot(); got != "伊娃工作室" {
		t.Fatalf("org_name must be live in the snapshot: %q", got)
	}

	// The agent read path (get_global_context) reflects the new name.
	if status, data = doJSON(t, "GET", srv.URL+"/api/global-context", owner, ""); status != 200 || data["org_name"] != "伊娃工作室" {
		t.Fatalf("global-context must surface the studio name: %d %v", status, data)
	}

	// "" clears it back to the default (the settings-API capability).
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"org_name":""}`); status != 200 || data["org_name"] != "" {
		t.Fatalf("empty org_name must clear: %d %v", status, data)
	}
	if got := api.orgNameSnapshot(); got != "" {
		t.Fatalf("cleared org_name must be live: %q", got)
	}
}

// TestOwnerNameSettingRoundTrips covers the T-0b41 owner nickname: the owner
// writes it through PATCH /api/settings (validated, trimmed, durable + live in
// the snapshot) and reads it back on the settings surface. Unlike org.name it
// is NOT an agent read path, so global-context never carries it.
func TestOwnerNameSettingRoundTrips(t *testing.T) {
	api, srv, d, _ := newSettingsTestServer(t, "owner-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"owner-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	// Default: unset → "" on the settings surface.
	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 || data["owner_name"] != "" {
		t.Fatalf("owner_name default must be \"\": %d %v", status, data)
	}

	// Over the 80-rune cap → 422, nothing written.
	long := `{"owner_name":"` + strings.Repeat("水", 81) + `"}`
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, long); status != 422 {
		t.Fatalf("owner_name over the cap must 422: got %d", status)
	}
	if v, err := d.GetSetting(settingOwnerName); err != nil || v != nil {
		t.Fatalf("a rejected owner_name patch must write nothing: %v %v", v, err)
	}

	// Valid patch: trimmed, echoed, durable, live in the snapshot.
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"owner_name":"  伊娃  "}`); status != 200 || data["owner_name"] != "伊娃" {
		t.Fatalf("owner_name patch must trim + echo: %d %v", status, data)
	}
	if v, err := d.GetSetting(settingOwnerName); err != nil || v == nil || *v != "伊娃" {
		t.Fatalf("owner_name must be durable: %v %v", v, err)
	}
	if got := api.ownerNameSnapshot(); got != "伊娃" {
		t.Fatalf("owner_name must be live in the snapshot: %q", got)
	}

	// The nickname never leaks onto the agent read path.
	if status, data = doJSON(t, "GET", srv.URL+"/api/global-context", owner, ""); status != 200 {
		t.Fatalf("global-context: %d", status)
	}
	if _, ok := data["owner_name"]; ok {
		t.Fatalf("owner_name must NOT appear on the agent read path: %v", data)
	}

	// "" clears it back to the default.
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"owner_name":""}`); status != 200 || data["owner_name"] != "" {
		t.Fatalf("empty owner_name must clear: %d %v", status, data)
	}
	if got := api.ownerNameSnapshot(); got != "" {
		t.Fatalf("cleared owner_name must be live: %q", got)
	}
}

func TestPushContactEmailSettingRoundTripsAndRejectsUnsafeDomains(t *testing.T) {
	api, srv, d, _ := newSettingsTestServer(t, "owner-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"owner-pass"}`)
	if status != http.StatusOK {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != http.StatusOK || data["push_contact_email"] != "" {
		t.Fatalf("push_contact_email default must be empty: %d %v", status, data)
	}
	if status, _ = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"push_contact_email":"notify@officraft.local"}`); status != http.StatusUnprocessableEntity {
		t.Fatalf("reserved VAPID contact domain must 422: %d", status)
	}
	if v, err := d.GetSetting(settingPushContactEmail); err != nil || v != nil {
		t.Fatalf("rejected contact email must not persist: %v %v", v, err)
	}
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"push_contact_email":"  notify@example.org  "}`); status != http.StatusOK || data["push_contact_email"] != "notify@example.org" {
		t.Fatalf("contact email patch must trim + echo: %d %v", status, data)
	}
	if got := api.pushVAPIDSubscriber(); got != "notify@example.org" {
		t.Fatalf("contact email must be live for delivery: %q", got)
	}
}

// TestDisplayPrefsSettingRoundTrips covers the T-0b41-p2 dual-layer display
// prefs: the owner writes theme/language through PATCH /api/settings (enum
// validated, durable + live in the snapshot) and reads them back on the settings
// surface. Like owner.name they are NOT an agent read path.
func TestDisplayPrefsSettingRoundTrips(t *testing.T) {
	api, srv, d, _ := newSettingsTestServer(t, "owner-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"owner-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	// Default: unset → "" on the settings surface.
	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 ||
		data["display_theme"] != "" || data["display_language"] != "" {
		t.Fatalf("display prefs default must be \"\": %d %v", status, data)
	}

	// An out-of-enum value → 422, nothing written.
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"display_theme":"neon"}`); status != 422 {
		t.Fatalf("out-of-enum display_theme must 422: got %d", status)
	}
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"display_language":"fr"}`); status != 422 {
		t.Fatalf("out-of-enum display_language must 422: got %d", status)
	}
	if v, err := d.GetSetting(settingDisplayTheme); err != nil || v != nil {
		t.Fatalf("a rejected display_theme patch must write nothing: %v %v", v, err)
	}

	// Valid patch: echoed, durable, live in the snapshot.
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"display_theme":"office","display_language":"en"}`); status != 200 ||
		data["display_theme"] != "office" || data["display_language"] != "en" {
		t.Fatalf("display prefs patch must echo: %d %v", status, data)
	}
	if v, err := d.GetSetting(settingDisplayTheme); err != nil || v == nil || *v != "office" {
		t.Fatalf("display_theme must be durable: %v %v", v, err)
	}
	if got := api.displayThemeSnapshot(); got != "office" {
		t.Fatalf("display_theme must be live in the snapshot: %q", got)
	}
	if got := api.displayLanguageSnapshot(); got != "en" {
		t.Fatalf("display_language must be live in the snapshot: %q", got)
	}

	// Neither pref leaks onto the agent read path.
	if status, data = doJSON(t, "GET", srv.URL+"/api/global-context", owner, ""); status != 200 {
		t.Fatalf("global-context: %d", status)
	}
	if _, ok := data["display_theme"]; ok {
		t.Fatalf("display_theme must NOT appear on the agent read path: %v", data)
	}
	if _, ok := data["display_language"]; ok {
		t.Fatalf("display_language must NOT appear on the agent read path: %v", data)
	}

	// "" clears back to unset.
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"display_theme":""}`); status != 200 ||
		data["display_theme"] != "" {
		t.Fatalf("empty display_theme must clear: %d %v", status, data)
	}
	if got := api.displayThemeSnapshot(); got != "" {
		t.Fatalf("cleared display_theme must be live: %q", got)
	}

	// custom_themes / wording / dangling-active-theme MOVED OUT (T-83ef), not
	// dropped. Themes left the settings surface entirely in that ticket — the
	// field is gone from both faces — so every assertion here that reached them
	// through /api/settings now reaches them through /api/themes and lives in
	// api_themes_test.go. What moved, by claim: the legal-bundle round trip, the
	// illegal-bundle refusals, the wording overlay round trip, the unknown-code
	// prune on write, the illegal-wording matrix, display_theme validated against
	// existing themes, and the reset of a display_theme whose theme was deleted.
	// That the field is ABSENT from both faces is itself asserted, by
	// TestSettingsNoLongerCarriesCustomThemes.
}

// TestDisplayWideSettingRoundTrips covers the T-756f layout-width pref: OFF out
// of the box (the shipped narrow centred column), a PATCH flips it durably +
// live in the snapshot, an omitted field never changes it (PATCH semantics), and
// it stays off the agent read path like the other display prefs.
func TestDisplayWideSettingRoundTrips(t *testing.T) {
	api, srv, d, _ := newSettingsTestServer(t, "owner-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"owner-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	// Default: never set → false (narrow). The DB row does not exist at all —
	// the shipped look must need no migration to stay the shipped look.
	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 ||
		data["display_wide"] != false {
		t.Fatalf("display_wide must default to false: %d %v", status, data["display_wide"])
	}
	if v, err := d.GetSetting(settingDisplayWide); err != nil || v != nil {
		t.Fatalf("an untouched display_wide must write nothing: %v %v", v, err)
	}
	if api.displayWideSnapshot() {
		t.Fatalf("display_wide snapshot must default to false")
	}

	// Turning it on: echoed, durable, live.
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"display_wide":true}`); status != 200 || data["display_wide"] != true {
		t.Fatalf("display_wide patch must echo true: %d %v", status, data["display_wide"])
	}
	if v, err := d.GetSetting(settingDisplayWide); err != nil || v == nil || *v != "true" {
		t.Fatalf("display_wide must be durable: %v %v", v, err)
	}
	if !api.displayWideSnapshot() {
		t.Fatalf("display_wide must be live in the snapshot")
	}

	// PATCH semantics: an unrelated patch leaves it alone.
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"display_language":"en"}`); status != 200 || data["display_wide"] != true {
		t.Fatalf("an omitted display_wide must not change it: %d %v", status, data["display_wide"])
	}

	// It never leaks onto the agent read path (same boundary as theme/language).
	if status, data = doJSON(t, "GET", srv.URL+"/api/global-context", owner, ""); status != 200 {
		t.Fatalf("global-context: %d", status)
	}
	if _, ok := data["display_wide"]; ok {
		t.Fatalf("display_wide must NOT appear on the agent read path: %v", data)
	}

	// Turning it back off is durable too (false is a written value, not a delete).
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"display_wide":false}`); status != 200 || data["display_wide"] != false {
		t.Fatalf("display_wide must flip back to false: %d %v", status, data["display_wide"])
	}
	if v, err := d.GetSetting(settingDisplayWide); err != nil || v == nil || *v != "false" {
		t.Fatalf("the flip back to narrow must be durable: %v %v", v, err)
	}
	if api.displayWideSnapshot() {
		t.Fatalf("the flip back to narrow must be live in the snapshot")
	}
}

// mustJSONString renders s as a JSON string literal (quoting embedded specials)
// so a raw colour value can be embedded in a test request body verbatim.
func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
