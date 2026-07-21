package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadForTest(t *testing.T, d *DAL, cfg Config) (authSettings, []string) {
	t.Helper()
	var logs []string
	got, err := loadAuthSettings(d, cfg, func(msg string) { logs = append(logs, msg) })
	if err != nil {
		t.Fatalf("loadAuthSettings: %v", err)
	}
	return got, logs
}

func TestLoadAuthSettingsMigratesExistingInstall(t *testing.T) {
	// The existing-install shape: oc.toml carries a plaintext password and NO
	// explicit secret — every already-issued token is signed with the
	// password-DERIVED secret.
	d := newTestDAL(t)
	cfg := defaultConfig()
	cfg.Auth.Password = "old-password"

	// A pre-migration token (e.g. a 400-day agent token) signed with the
	// derived secret.
	now := time.Now().Unix()
	oldToken, err := mintJWT("kyle", "agent", 400*86400, deriveSecretFromPassword("old-password"), now, "")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	got, _ := loadForTest(t, d, cfg)

	// The DERIVED secret was imported, not a fresh mint — the old token still
	// verifies under the loaded secret (zero token invalidation).
	if string(got.secret) != string(deriveSecretFromPassword("old-password")) {
		t.Fatal("migration must import the password-derived secret, not mint a new one")
	}
	if _, err := verifyJWT(oldToken, got.secret, now+1); err != nil {
		t.Fatalf("a pre-migration token must stay valid: %v", err)
	}

	// The password landed as an argon2id hash (no plaintext in the DB) and
	// verifies the old password.
	stored, err := d.GetSetting(settingPasswordHash)
	if err != nil || stored == nil {
		t.Fatalf("password hash must be in the DB: %v %v", stored, err)
	}
	if strings.Contains(*stored, "old-password") {
		t.Fatal("the plaintext must never enter the DB")
	}
	if !verifyPassword("old-password", got.passwordHash) {
		t.Fatal("the old password must verify against the migrated hash")
	}

	// password_changed_at is NOT stamped by the migration (migrating is not a
	// password change; pre-migration tokens must survive the B3 iat check).
	if v, err := d.GetSetting(settingPasswordChangedAt); err != nil || v != nil {
		t.Fatalf("migration must not stamp password_changed_at: %v %v", v, err)
	}

	// Idempotent: a second load (same oc.toml still present) changes nothing.
	hashBefore := *stored
	again, logs := loadForTest(t, d, cfg)
	if string(again.secret) != string(got.secret) || again.passwordHash != hashBefore {
		t.Fatal("a second load must reuse the DB values")
	}
	if len(logs) != 0 {
		t.Fatalf("a second load must not re-migrate: %v", logs)
	}
}

func TestLoadAuthSettingsImportsExplicitSecret(t *testing.T) {
	d := newTestDAL(t)
	cfg := defaultConfig()
	cfg.Auth.Password = "pw"
	cfg.Auth.Secret = "explicit-signing-secret"

	got, _ := loadForTest(t, d, cfg)
	if string(got.secret) != "explicit-signing-secret" {
		t.Fatalf("an explicit oc.toml secret must be imported verbatim: %q", got.secret)
	}
	stored, err := d.GetSetting(settingJWTSecret)
	if err != nil || stored == nil {
		t.Fatalf("secret must be persisted: %v", err)
	}
	if *stored != base64.RawURLEncoding.EncodeToString([]byte("explicit-signing-secret")) {
		t.Fatalf("stored secret must be base64url of the key bytes: %q", *stored)
	}
}

func TestLoadAuthSettingsFreshInstallMintsSecret(t *testing.T) {
	// No DB value, no oc.toml auth at all — only then is a random secret minted.
	d := newTestDAL(t)
	got, logs := loadForTest(t, d, defaultConfig())
	if len(got.secret) != 32 {
		t.Fatalf("a fresh install must mint a 32-byte secret, got %d", len(got.secret))
	}
	if got.passwordHash != "" {
		t.Fatal("a fresh install has no password hash")
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "minted") {
		t.Fatalf("mint must be logged: %v", logs)
	}
	// Stable across restarts: the minted secret is persisted, not re-minted.
	again, logs := loadForTest(t, d, defaultConfig())
	if string(again.secret) != string(got.secret) {
		t.Fatal("the minted secret must be stable across loads")
	}
	if len(logs) != 0 {
		t.Fatalf("no re-mint on the second load: %v", logs)
	}
}

func TestLoadAuthSettingsTokenTTLPrecedence(t *testing.T) {
	// DB value wins over oc.toml.
	d := newTestDAL(t)
	if err := d.PutSetting(settingTokenTTL, "7200"); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.Auth.TokenTTL, cfg.Auth.TokenTTLSet = 3600, true
	if got, _ := loadForTest(t, d, cfg); got.tokenTTL != 7200 {
		t.Fatalf("DB token_ttl must win: %d", got.tokenTTL)
	}

	// An explicitly written oc.toml token_ttl migrates into the DB.
	d2 := newTestDAL(t)
	if got, _ := loadForTest(t, d2, cfg); got.tokenTTL != 3600 {
		t.Fatalf("explicit oc.toml token_ttl must apply: %d", got.tokenTTL)
	}
	if v, err := d2.GetSetting(settingTokenTTL); err != nil || v == nil || *v != "3600" {
		t.Fatalf("explicit oc.toml token_ttl must migrate into the DB: %v %v", v, err)
	}

	// The convention default is NOT written (absent key = code default).
	d3 := newTestDAL(t)
	if got, _ := loadForTest(t, d3, defaultConfig()); got.tokenTTL != defaultTokenTTL {
		t.Fatalf("default token_ttl expected: %d", got.tokenTTL)
	}
	if v, err := d3.GetSetting(settingTokenTTL); err != nil || v != nil {
		t.Fatalf("the default must not be written to the DB: %v %v", v, err)
	}
}

func TestLoadAuthSettingsOrgName(t *testing.T) {
	// Absent → "" (never set; the topbar shows the localized default).
	got, _ := loadForTest(t, newTestDAL(t), defaultConfig())
	if got.orgName != "" {
		t.Fatalf("absent org.name must load as \"\": %q", got.orgName)
	}
	// A stored org.name lands in the boot snapshot verbatim.
	d := newTestDAL(t)
	if err := d.PutSetting(settingOrgName, "伊娃工作室"); err != nil {
		t.Fatal(err)
	}
	got2, _ := loadForTest(t, d, defaultConfig())
	if got2.orgName != "伊娃工作室" {
		t.Fatalf("stored org.name must load into the snapshot: %q", got2.orgName)
	}
}

func TestLoadAuthSettingsOwnerName(t *testing.T) {
	// Absent → "" (never set; the profile pill shows the localized default).
	got, _ := loadForTest(t, newTestDAL(t), defaultConfig())
	if got.ownerName != "" {
		t.Fatalf("absent owner.name must load as \"\": %q", got.ownerName)
	}
	// A stored owner.name lands in the boot snapshot verbatim.
	d := newTestDAL(t)
	if err := d.PutSetting(settingOwnerName, "伊娃"); err != nil {
		t.Fatal(err)
	}
	got2, _ := loadForTest(t, d, defaultConfig())
	if got2.ownerName != "伊娃" {
		t.Fatalf("stored owner.name must load into the snapshot: %q", got2.ownerName)
	}
}

func TestLoadAuthSettingsCtxOverrides(t *testing.T) {
	d := newTestDAL(t)
	for key, value := range map[string]string{
		settingCtxWarnPct:       "55",
		settingCtxHandoverPct:   "70",
		settingCtxRemindStepPct: "8",
		settingCtxMinBootSecs:   "60.5",
		settingCtxStaleGuard:    "false",
	} {
		if err := d.PutSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := loadForTest(t, d, defaultConfig())
	want := SseContextHighConfig{WarnPct: 55, HandoverPct: 70, RemindStepPct: 8, MinBootSecs: 60.5, StaleGuard: false}
	if got.ctxhigh != want {
		t.Fatalf("DB ctx overrides must apply: %+v", got.ctxhigh)
	}

	// Absent keys keep the oc.toml/default value.
	d2 := newTestDAL(t)
	if err := d2.PutSetting(settingCtxHandoverPct, "70"); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.SseContextHigh.WarnPct = 45
	got2, _ := loadForTest(t, d2, cfg)
	if got2.ctxhigh.WarnPct != 45 || got2.ctxhigh.HandoverPct != 70 || got2.ctxhigh.RemindStepPct != 5 {
		t.Fatalf("absent ctx keys must keep the config value: %+v", got2.ctxhigh)
	}

	// One-shot migration: knobs the retired [sse_context_high] table wrote
	// EXPLICITLY land in the DB (ctx.*), so a tuned install survives the key
	// retirement; a DB value present beforehand is never overwritten, and
	// knobs the file never set are never written.
	d3 := newTestDAL(t)
	if err := d3.PutSetting(settingCtxHandoverPct, "70"); err != nil {
		t.Fatal(err)
	}
	cfg3 := defaultConfig()
	cfg3.SseContextHigh.WarnPct = 45
	cfg3.SseContextHighSet.WarnPct = true
	cfg3.SseContextHigh.HandoverPct = 60
	cfg3.SseContextHighSet.HandoverPct = true
	got3, logs := loadForTest(t, d3, cfg3)
	if v, err := d3.GetSetting(settingCtxWarnPct); err != nil || v == nil || *v != "45" {
		t.Fatalf("an explicitly-set knob must migrate into the DB: %v %v", v, err)
	}
	if v, err := d3.GetSetting(settingCtxHandoverPct); err != nil || v == nil || *v != "70" {
		t.Fatalf("a pre-existing DB value must win over the file: %v %v", v, err)
	}
	if got3.ctxhigh.WarnPct != 45 || got3.ctxhigh.HandoverPct != 70 {
		t.Fatalf("runtime snapshot after migration: %+v", got3.ctxhigh)
	}
	if v, err := d3.GetSetting(settingCtxRemindStepPct); err != nil || v != nil {
		t.Fatalf("an unset knob must not be written: %v %v", v, err)
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "sse_context_high") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the ctx migration must be logged: %v", logs)
	}
}

func TestLoginVerifiesAgainstMigratedHash(t *testing.T) {
	// The B1 acceptance cut, end to end at the handler: an existing install
	// (oc.toml password, empty DB) restarts — the old password still logs in,
	// and the login verifies the DB argon2id hash (the plaintext fallback is
	// gone since B2: no hash in the DB = every login denied).
	d := newTestDAL(t)
	cfg := defaultConfig()
	cfg.Auth.Password = "old-password"
	auth, _ := loadForTest(t, d, cfg)

	api := newAPIServer(d, NewHub(), auth.secret, auth.tokenTTL, "../..")
	api.passwordHash = auth.passwordHash
	h, err := buildHandler(specsFor(api), auth.secret, d.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	login := func(payload string) int {
		resp, err := http.Post(srv.URL+"/api/login", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if status := login(`{"password":"old-password"}`); status != 200 {
		t.Fatalf("the pre-migration password must still log in: %d", status)
	}
	if status := login(`{"password":"wrong"}`); status != 401 {
		t.Fatalf("a wrong password stays a flat 401: %d", status)
	}

	// No DB hash → a flat 401 whatever the file says (the B1 plaintext
	// fallback is removed; the first-run state leaks nothing here).
	api.passwordHash = ""
	if status := login(`{"password":"old-password"}`); status != 401 {
		t.Fatalf("no stored hash must deny every login: %d", status)
	}
}

func TestCmdSetPassword(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "set-password.db")
	env := func(password string) func(string) string {
		return envOf(map[string]string{
			"OC_CONFIG":       filepath.Join(t.TempDir(), "absent.toml"),
			"OC_DATABASE_URL": "sqlite:///" + dbPath,
			envNewPassword:    password,
		})
	}
	loadHash := func() string {
		db, err := openSQLite(dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer db.Close()
		v, err := NewDAL(db).GetSetting(settingPasswordHash)
		if err != nil || v == nil {
			t.Fatalf("hash must be stored: %v %v", v, err)
		}
		return *v
	}

	var out strings.Builder
	// Missing $OC_NEW_PASSWORD is a usage error, not a silent empty password.
	if rc := cmdSetPassword(env(""), &out); rc != 2 {
		t.Fatalf("missing env password: want rc 2, got %d", rc)
	}
	// First set on a fresh (unmigrated) DB: migrates + writes the hash.
	if rc := cmdSetPassword(env("first-password"), &out); rc != 0 {
		t.Fatalf("first set: want rc 0, got %d (out: %s)", rc, out.String())
	}
	if !verifyPassword("first-password", loadHash()) {
		t.Fatal("the stored hash must verify the set password")
	}
	if strings.Contains(out.String(), "first-password") {
		t.Fatal("the password must never be echoed")
	}
	// A second call replaces the credential (operator rescue).
	if rc := cmdSetPassword(env("second-password"), &out); rc != 0 {
		t.Fatalf("overwrite: want rc 0, got %d", rc)
	}
	if !verifyPassword("second-password", loadHash()) {
		t.Fatal("the overwrite must store the new password's hash")
	}
}

// TestSetUpdaterRetired pins the teardown of the ocupdaterd chain (t-dc68):
// the set-updater subcommand is GONE (unknown-subcommand usage error), and a
// migrated DB carrying stale updater.url / updater.invite_code rows boots
// cleanly — the retired keys are simply never read — while the two surviving
// toggles (updater.receive_beta / updater.auto_update) keep loading under
// their unchanged DB names (an armed install stays armed).
func TestSetUpdaterRetired(t *testing.T) {
	var out strings.Builder
	if rc := realMain([]string{"set-updater"}, envOf(nil), &out); rc != 2 {
		t.Fatalf("set-updater must be an unknown subcommand now: want rc 2, got %d", rc)
	}
	if !strings.Contains(out.String(), "unknown subcommand") {
		t.Fatalf("want the unknown-subcommand usage error, got: %s", out.String())
	}

	dbPath := filepath.Join(t.TempDir(), "stale-updater.db")
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	d := NewDAL(db)
	for k, v := range map[string]string{
		"updater.url":          "http://127.0.0.1:8790",
		"updater.invite_code":  "ocu-inv-stale",
		"updater.receive_beta": "true",
		"updater.auto_update":  "true",
	} {
		if err := d.PutSetting(k, v); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}
	auth, err := loadAuthSettings(d, Config{}, func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings must ignore the retired updater.* rows: %v", err)
	}
	if !auth.updaterReceiveBeta || !auth.updaterAutoUpdate {
		t.Fatalf("the surviving toggles must load from their unchanged keys: beta=%v auto=%v",
			auth.updaterReceiveBeta, auth.updaterAutoUpdate)
	}
}

func TestCmdClaimToken(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "claim-token.db")
	env := envOf(map[string]string{
		"OC_CONFIG":       filepath.Join(t.TempDir(), "absent.toml"),
		"OC_DATABASE_URL": "sqlite:///" + dbPath,
	})
	lastLine := func(s string) string {
		lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
		return lines[len(lines)-1]
	}

	// No password set: prints a token as the LAST line (the installer banner's
	// parse contract) and stores it so serve reuses the same one.
	var out strings.Builder
	if rc := cmdClaimToken(env, &out); rc != 0 {
		t.Fatalf("no password: want rc 0, got %d (out: %s)", rc, out.String())
	}
	token := lastLine(out.String())
	if token == "" || strings.HasPrefix(token, "[ocserverd]") {
		t.Fatalf("the token must be the bare last output line: %q", out.String())
	}

	// Stable across calls (and thus across serve restarts).
	var again strings.Builder
	if rc := cmdClaimToken(env, &again); rc != 0 || lastLine(again.String()) != token {
		t.Fatalf("a second call must return the same token: rc %d, %q vs %q", rc, lastLine(again.String()), token)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	stored, err := NewDAL(db).GetSetting(settingClaimToken)
	db.Close()
	if err != nil || stored == nil || *stored != token {
		t.Fatalf("the printed token must be the stored one: %v %v", stored, err)
	}

	// Password set: exit 3, no token printed, and the residual token is gone.
	envPW := envOf(map[string]string{
		"OC_CONFIG":       filepath.Join(t.TempDir(), "absent.toml"),
		"OC_DATABASE_URL": "sqlite:///" + dbPath,
		envNewPassword:    "owner-chosen",
	})
	var spOut strings.Builder
	if rc := cmdSetPassword(envPW, &spOut); rc != 0 {
		t.Fatalf("set-password: want rc 0, got %d", rc)
	}
	var after strings.Builder
	if rc := cmdClaimToken(env, &after); rc != 3 {
		t.Fatalf("password set: want rc 3, got %d (out: %s)", rc, after.String())
	}
	if strings.Contains(after.String(), token) {
		t.Fatal("no token may be revealed once a password is set")
	}
	db2, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	residual, err := NewDAL(db2).GetSetting(settingClaimToken)
	db2.Close()
	if err != nil || residual != nil {
		t.Fatalf("the residual token must be deleted: %v %v", residual, err)
	}
}

func TestLoadAuthSettingsFailsLoudOnCorruptValues(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PutSetting(settingJWTSecret, "!!not-base64url!!"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthSettings(d, defaultConfig(), func(string) {}); err == nil {
		t.Fatal("a corrupt stored secret must fail loud, not boot with a broken key")
	}

	d2 := newTestDAL(t)
	if err := d2.PutSetting(settingTokenTTL, "not-a-number"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthSettings(d2, defaultConfig(), func(string) {}); err == nil {
		t.Fatal("a corrupt token_ttl must fail loud")
	}
}

func TestEnsureFirstRunClaimToken(t *testing.T) {
	d := newTestDAL(t)

	// No password: mints once, then returns the SAME token (stable across
	// restarts — the operator's printed token keeps working).
	first, err := ensureFirstRunClaimToken(d, false, func(string) {})
	if err != nil || first == "" {
		t.Fatalf("first-run must mint a claim token: %q %v", first, err)
	}
	again, err := ensureFirstRunClaimToken(d, false, func(string) {})
	if err != nil || again != first {
		t.Fatalf("a restart must reuse the minted token: %q vs %q (%v)", again, first, err)
	}

	// Password set: the residual token is deleted and none is returned — a
	// stale claim token must never outlive the credential it gated.
	got, err := ensureFirstRunClaimToken(d, true, func(string) {})
	if err != nil || got != "" {
		t.Fatalf("with a password set no claim token may exist: %q %v", got, err)
	}
	if v, err := d.GetSetting(settingClaimToken); err != nil || v != nil {
		t.Fatalf("the residual claim token must be deleted: %v %v", v, err)
	}
}
