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
	api := newAPIServer(d, NewHub(), auth.secret, auth.tokenTTL, "../..")
	api.passwordHash = auth.passwordHash
	api.passwordChangedAt = auth.passwordChangedAt
	api.ctxhigh = auth.ctxhigh
	h, err := buildHandler(specsFor(api), auth.secret, d.GetMember, api.authPasswordChangedAt)
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
	oldOwner, err := mintJWT(wireOwnerID, "owner", 86400, api.secret, past, "")
	if err != nil {
		t.Fatal(err)
	}
	agentToken, err := mintJWT("kyle", "agent", 86400, api.secret, past, "")
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
	if status != 200 || data["token_ttl"] != float64(86400) ||
		data["handover_pct"] != float64(50) {
		t.Fatalf("settings defaults: %d %v", status, data)
	}

	// Invalid values → 422, nothing written.
	for _, body := range []string{
		`{"token_ttl":0}`, `{"token_ttl":3600}`,
		`{"handover_pct":39}`, `{"handover_pct":91}`,
		`{"token_ttl":604800,"handover_pct":10}`, // one bad field poisons the whole patch
	} {
		if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, body); status != 422 {
			t.Fatalf("PATCH %s: want 422, got %d", body, status)
		}
	}
	if v, err := d.GetSetting(settingTokenTTL); err != nil || v != nil {
		t.Fatalf("a rejected patch must write nothing: %v %v", v, err)
	}

	// Valid patch: durable + live immediately.
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"token_ttl":604800,"handover_pct":60}`)
	if status != 200 || data["token_ttl"] != float64(604800) ||
		data["handover_pct"] != float64(60) {
		t.Fatalf("PATCH response must echo the new settings: %d %v", status, data)
	}
	if v, err := d.GetSetting(settingTokenTTL); err != nil || v == nil || *v != "604800" {
		t.Fatalf("token_ttl must be durable: %v %v", v, err)
	}
	if got := api.ctxHighConfig().HandoverPct; got != 60 {
		t.Fatalf("handover_pct must be live: %d", got)
	}
	// The next login mints with the new TTL — no restart.
	status, data = doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"settings-pass"}`)
	if status != 200 || data["expires_in"] != float64(604800) {
		t.Fatalf("login must pick up the patched TTL immediately: %d %v", status, data)
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
		`{"display_theme":"xian","display_language":"en"}`); status != 200 ||
		data["display_theme"] != "xian" || data["display_language"] != "en" {
		t.Fatalf("display prefs patch must echo: %d %v", status, data)
	}
	if v, err := d.GetSetting(settingDisplayTheme); err != nil || v == nil || *v != "xian" {
		t.Fatalf("display_theme must be durable: %v %v", v, err)
	}
	if got := api.displayThemeSnapshot(); got != "xian" {
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

	// custom_themes (T-16a1 P2) defaults to an empty array on the settings surface.
	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 {
		t.Fatalf("settings GET: %d", status)
	}
	if arr, ok := data["custom_themes"].([]any); !ok || len(arr) != 0 {
		t.Fatalf("custom_themes default must be an empty array: %v", data["custom_themes"])
	}

	// A legal bundle round-trips: 200, echoed, durable, and display_theme may
	// point at the new custom id in the SAME patch.
	legal := `{"custom_themes":[{"id":"midnight","name":"Midnight","colors":` +
		`{"--color-bg":"#101018","--color-accent":"rgba(120, 200, 255, 0.9)",` +
		`"--color-text":"transparent"}}],"display_theme":"midnight"}`
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, legal); status != 200 {
		t.Fatalf("legal custom_themes bundle must 200: %d %v", status, data)
	}
	if data["display_theme"] != "midnight" {
		t.Fatalf("display_theme must accept an existing custom id: %v", data["display_theme"])
	}
	if arr, ok := data["custom_themes"].([]any); !ok || len(arr) != 1 {
		t.Fatalf("custom_themes must echo the saved bundle: %v", data["custom_themes"])
	}
	if v, err := d.GetSetting(settingDisplayCustomThemes); err != nil || v == nil ||
		!strings.Contains(*v, "midnight") {
		t.Fatalf("custom_themes must be durable: %v %v", v, err)
	}

	// A non-whitelisted token name → 422, nothing overwritten.
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"custom_themes":[{"id":"x1","name":"X","colors":{"--color-bogus":"#fff"}}]}`); status != 422 {
		t.Fatalf("non-whitelisted token must 422: got %d", status)
	}

	// Each illegal colour value → 422, and none of them is ever stored.
	for _, bad := range []string{
		`url(https://evil)`, `red;}`, `<script>`, `expression(1)`, `var(--x)`,
		`#fff;background:url(x)`, strings.Repeat("f", 70),
	} {
		body := `{"custom_themes":[{"id":"bad1","name":"Bad","colors":{"--color-bg":` +
			mustJSONString(bad) + `}}]}`
		if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, body); status != 422 {
			t.Fatalf("illegal colour %q must 422: got %d", bad, status)
		}
	}
	// The rejected patches wrote nothing — the midnight bundle still stands alone.
	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 {
		t.Fatalf("settings GET: %d", status)
	}
	if arr, _ := data["custom_themes"].([]any); len(arr) != 1 {
		t.Fatalf("a rejected custom_themes patch must write nothing: %v", data["custom_themes"])
	}

	// display_theme pointing at a non-existent custom id → 422.
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"display_theme":"ghost"}`); status != 422 {
		t.Fatalf("display_theme=non-existent custom id must 422: got %d", status)
	}

	// wording overlay (T-16a1 P3): a legal per-language overlay round-trips —
	// 200, echoed, durable — keyed on whitelisted message codes with zh/en langs.
	worded := `{"custom_themes":[{"id":"worded","name":"Worded","colors":` +
		`{"--color-bg":"#101018"},"wording":{"zh":{"nav.tasks":"待辦"},` +
		`"en":{"profile.themeOffice":"Office Mode"}}}]}`
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, worded); status != 200 {
		t.Fatalf("legal wording overlay must 200: %d %v", status, data)
	}
	if got := wordingValue(t, data, 0, "zh", "nav.tasks"); got != "待辦" {
		t.Fatalf("wording overlay must echo the zh override: %q", got)
	}
	if got := wordingValue(t, data, 0, "en", "profile.themeOffice"); got != "Office Mode" {
		t.Fatalf("wording overlay must echo the en override: %q", got)
	}
	if v, err := d.GetSetting(settingDisplayCustomThemes); err != nil || v == nil ||
		!strings.Contains(*v, "待辦") {
		t.Fatalf("wording overlay must be durable: %v %v", v, err)
	}

	// Every illegal wording overlay → 422, and none is ever stored: a code
	// outside the whitelist, a language other than zh/en, an over-cap value, and
	// a value carrying a control character (newline).
	for _, bad := range []string{
		`"wording":{"zh":{"not.a.real.key":"x"}}`,                           // non-whitelisted code
		`"wording":{"xian":{"nav.tasks":"仙"}}`,                              // language not in {zh,en}
		`"wording":{"zh":{"nav.tasks":"` + strings.Repeat("字", 201) + `"}}`, // over the 200-rune cap
		`"wording":{"zh":{"nav.tasks":"a\nb"}}`,                             // control character (newline)
		`"wording":{"zh":{"nav.tasks":"   "}}`,                              // empty after trimming
	} {
		body := `{"custom_themes":[{"id":"w2","name":"W2","colors":{"--color-bg":"#111"},` + bad + `}]}`
		if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, body); status != 422 {
			t.Fatalf("illegal wording %q must 422: got %d", bad, status)
		}
	}
	// The rejected wording patches wrote nothing — the "worded" bundle still stands.
	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 {
		t.Fatalf("settings GET: %d", status)
	}
	if arr, _ := data["custom_themes"].([]any); len(arr) != 1 {
		t.Fatalf("a rejected wording patch must write nothing: %v", data["custom_themes"])
	}
	if got := wordingValue(t, data, 0, "zh", "nav.tasks"); got != "待辦" {
		t.Fatalf("a rejected wording patch must leave the stored overlay intact: %q", got)
	}

	// Deleting the active custom theme (replacing the set without it) resets
	// display_theme back to "" server-side — no dangling active theme.
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"custom_themes":[]}`); status != 200 {
		t.Fatalf("clearing custom_themes: %d %v", status, data)
	}
	if data["display_theme"] != "" {
		t.Fatalf("deleting the active custom theme must reset display_theme to \"\": %v",
			data["display_theme"])
	}
	if got := api.displayThemeSnapshot(); got != "" {
		t.Fatalf("the dangling-theme reset must be live in the snapshot: %q", got)
	}
	if v, err := d.GetSetting(settingDisplayTheme); err != nil || v == nil || *v != "" {
		t.Fatalf("the dangling-theme reset must be durable: %v %v", v, err)
	}
}

// wordingValue digs custom_themes[idx].wording[lang][code] out of a settings
// response body, failing the test if any hop is missing or mistyped.
func wordingValue(t *testing.T, data map[string]any, idx int, lang, code string) string {
	t.Helper()
	arr, ok := data["custom_themes"].([]any)
	if !ok || idx >= len(arr) {
		t.Fatalf("custom_themes[%d] missing: %v", idx, data["custom_themes"])
	}
	bundle, _ := arr[idx].(map[string]any)
	wording, _ := bundle["wording"].(map[string]any)
	langMap, _ := wording[lang].(map[string]any)
	v, _ := langMap[code].(string)
	return v
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
