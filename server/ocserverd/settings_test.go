package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func settingsTestLoadAuth(t *testing.T, d *DAL, cfg Config) (authSettings, []string, error) {
	t.Helper()
	var logs []string
	got, err := loadAuthSettings(d, cfg, func(msg string) { logs = append(logs, msg) })
	return got, logs, err
}

func TestCanonicalSuggestedReplies(t *testing.T) {
	t.Run("trims entries drops blank entries and preserves order", func(t *testing.T) {
		got, err := canonicalSuggestedReplies([]string{"  first  ", "", "\t第二\n", "   "})
		if err != nil {
			t.Fatalf("canonicalSuggestedReplies: %v", err)
		}
		want := []string{"first", "第二"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("canonicalSuggestedReplies: got=%#v want=%#v", got, want)
		}
	})

	t.Run("an entry at the rune limit is accepted", func(t *testing.T) {
		input := []string{strings.Repeat("水", maxSuggestedReplyLen)}
		got, err := canonicalSuggestedReplies(input)
		if err != nil {
			t.Fatalf("canonicalSuggestedReplies: %v", err)
		}
		want := []string{strings.Repeat("水", maxSuggestedReplyLen)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("boundary entry: got=%#v want=%#v", got, want)
		}
	})

	t.Run("an entry beyond the rune limit is refused", func(t *testing.T) {
		_, err := canonicalSuggestedReplies([]string{strings.Repeat("水", maxSuggestedReplyLen+1)})
		if err == nil {
			t.Fatal("an overlong entry must be refused")
		}
		if err.Error() != "must be at most 120 characters per entry" {
			t.Fatalf("overlong error: got=%q", err)
		}
	})

	t.Run("more than the list limit is refused", func(t *testing.T) {
		input := make([]string, maxSuggestedReplies+1)
		for i := range input {
			input[i] = fmt.Sprintf("reply-%d", i)
		}
		_, err := canonicalSuggestedReplies(input)
		if err == nil {
			t.Fatal("an over-cap list must be refused")
		}
		if err.Error() != "must be at most 20 entries" {
			t.Fatalf("over-cap error: got=%q", err)
		}
	})
}

func TestEncodeSuggestedReplies(t *testing.T) {
	t.Run("a nil list is stored as an empty JSON array", func(t *testing.T) {
		got := encodeSuggestedReplies(nil)
		if got != "[]" {
			t.Fatalf("encoded nil list: got=%q want=%q", got, "[]")
		}
	})

	t.Run("a canonical list is stored in order as one JSON value", func(t *testing.T) {
		got := encodeSuggestedReplies([]string{"收到", "next"})
		want := `["收到","next"]`
		if got != want {
			t.Fatalf("encoded list: got=%q want=%q", got, want)
		}
	})
}

func TestDecodeSuggestedReplies(t *testing.T) {
	t.Run("a stored JSON array is canonicalized on read", func(t *testing.T) {
		got, err := decodeSuggestedReplies(`["  first  ","", "第二"]`)
		if err != nil {
			t.Fatalf("decodeSuggestedReplies: %v", err)
		}
		want := []string{"first", "第二"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("decoded list: got=%#v want=%#v", got, want)
		}
	})

	t.Run("malformed stored JSON is refused", func(t *testing.T) {
		_, err := decodeSuggestedReplies("not-json")
		if err == nil {
			t.Fatal("malformed stored JSON must be refused")
		}
		if err.Error() != `must be a JSON array of strings: "not-json"` {
			t.Fatalf("malformed JSON error: got=%q", err)
		}
	})

	t.Run("an over-cap stored array is refused", func(t *testing.T) {
		row := "[" + strings.TrimSuffix(strings.Repeat(`"x",`, maxSuggestedReplies+1), ",") + "]"
		_, err := decodeSuggestedReplies(row)
		if err == nil {
			t.Fatal("an over-cap stored array must be refused")
		}
		if err.Error() != "must be at most 20 entries" {
			t.Fatalf("over-cap stored array error: got=%q", err)
		}
	})
}

func TestLoadAuthSettings(t *testing.T) {
	t.Run("a fresh install mints and persists a stable secret with shipped defaults", func(t *testing.T) {
		d := newAPITestDAL(t)
		got, logs, err := settingsTestLoadAuth(t, d, defaultConfig())
		if err != nil {
			t.Fatalf("loadAuthSettings: %v", err)
		}
		wantLogs := []string{"minted a fresh JWT signing secret into DB settings (new install)"}
		if !reflect.DeepEqual(logs, wantLogs) {
			t.Fatalf("fresh install logs: got=%#v want=%#v", logs, wantLogs)
		}
		if len(got.secret) != 32 || got.passwordHash != "" {
			t.Fatalf("fresh snapshot has wrong credential state: secret=%d password=%q", len(got.secret), got.passwordHash)
		}
		stored, err := d.GetSetting(settingJWTSecret)
		if err != nil || stored == nil {
			t.Fatalf("fresh secret was not persisted: %v %v", stored, err)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(*stored)
		if err != nil || !reflect.DeepEqual(decoded, got.secret) {
			t.Fatalf("persisted secret does not match the loaded secret: %q %v", *stored, err)
		}
		if got.ownerTokenTTL != defaultOwnerTokenTTL || got.agentTokenTTL != defaultAgentTokenTTL {
			t.Fatalf("fresh token TTLs are wrong: owner=%d agent=%d", got.ownerTokenTTL, got.agentTokenTTL)
		}
		again, secondLogs, err := settingsTestLoadAuth(t, d, defaultConfig())
		if err != nil {
			t.Fatalf("second loadAuthSettings: %v", err)
		}
		if !reflect.DeepEqual(again, got) || len(secondLogs) != 0 {
			t.Fatalf("second load must reuse the persisted snapshot: got=%+v logs=%#v", again, secondLogs)
		}
	})

	t.Run("an existing password install imports the derived signing secret and stores only a hash", func(t *testing.T) {
		d := newAPITestDAL(t)
		cfg := defaultConfig()
		cfg.Auth.Password = "old-password"
		oldSecret := deriveSecretFromPassword(cfg.Auth.Password)
		oldToken, err := mintJWT("kip", "agent", 400*86400, oldSecret, 1700000000, "")
		if err != nil {
			t.Fatalf("mint pre-migration token: %v", err)
		}
		got, logs, err := settingsTestLoadAuth(t, d, cfg)
		if err != nil {
			t.Fatalf("loadAuthSettings: %v", err)
		}
		if !reflect.DeepEqual(got.secret, oldSecret) {
			t.Fatalf("migration did not preserve the password-derived signing secret")
		}
		wantLogs := []string{
			"migrated the password-derived JWT secret into DB settings (existing tokens stay valid)",
			"migrated oc.toml [auth].password into DB settings as an argon2id hash",
		}
		if !reflect.DeepEqual(logs, wantLogs) {
			t.Fatalf("migration logs: got=%#v want=%#v", logs, wantLogs)
		}
		if _, err := verifyJWT(oldToken, got.secret, 1700000001); err != nil {
			t.Fatalf("pre-migration token no longer verifies: %v", err)
		}
		stored, err := d.GetSetting(settingPasswordHash)
		if err != nil || stored == nil || strings.Contains(*stored, cfg.Auth.Password) {
			t.Fatalf("password was not stored as a non-plaintext hash: %v %v", stored, err)
		}
		if !verifyPassword(cfg.Auth.Password, got.passwordHash) {
			t.Fatal("the migrated password hash does not verify the old password")
		}
		changed, err := d.GetSetting(settingPasswordChangedAt)
		if err != nil || changed != nil {
			t.Fatalf("migration must not stamp password_changed_at: %v %v", changed, err)
		}
	})

	t.Run("database credentials win over values still present in the config", func(t *testing.T) {
		d := newAPITestDAL(t)
		if err := d.PutSetting(settingJWTSecret, base64.RawURLEncoding.EncodeToString([]byte("db-secret"))); err != nil {
			t.Fatalf("PutSetting secret: %v", err)
		}
		dbHash, err := hashPassword("db-password")
		if err != nil {
			t.Fatalf("hash database password: %v", err)
		}
		if err := d.PutSetting(settingPasswordHash, dbHash); err != nil {
			t.Fatalf("PutSetting password hash: %v", err)
		}
		cfg := defaultConfig()
		cfg.Auth.Secret = "file-secret"
		cfg.Auth.Password = "file-password"
		got, logs, err := settingsTestLoadAuth(t, d, cfg)
		if err != nil {
			t.Fatalf("loadAuthSettings: %v", err)
		}
		if len(logs) != 0 {
			t.Fatalf("database credentials must not be migrated: %#v", logs)
		}
		if string(got.secret) != "db-secret" || !verifyPassword("db-password", got.passwordHash) || verifyPassword("file-password", got.passwordHash) {
			t.Fatalf("database credentials did not win: %+v", got)
		}
	})

	t.Run("stored successor TTLs context values and suggestion lists load independently", func(t *testing.T) {
		d := newAPITestDAL(t)
		for key, value := range map[string]string{
			settingOwnerTokenTTL:               "7200",
			settingAgentTokenTTL:               "1800",
			settingCtxNoticePct:                "41",
			settingCtxHandoverPct:              "66",
			settingCtxMinBootSecs:              "12.5",
			settingCtxStaleGuard:               "false",
			settingSuggestedRepliesReplyCard:   ` ["first"] `,
			settingSuggestedRepliesTaskMessage: `["second","third"]`,
		} {
			if err := d.PutSetting(key, value); err != nil {
				t.Fatalf("PutSetting(%q): %v", key, err)
			}
		}
		got, logs, err := settingsTestLoadAuth(t, d, defaultConfig())
		if err != nil {
			t.Fatalf("loadAuthSettings: %v", err)
		}
		wantLogs := []string{"minted a fresh JWT signing secret into DB settings (new install)"}
		if !reflect.DeepEqual(logs, wantLogs) {
			t.Fatalf("stored-values logs: got=%#v want=%#v", logs, wantLogs)
		}
		if got.ownerTokenTTL != 7200 || got.agentTokenTTL != 1800 {
			t.Fatalf("successor TTLs did not load independently: %+v", got)
		}
		wantCtx := SseContextHighConfig{NoticePct: 41, HandoverPct: 66, MinBootSecs: 12.5, StaleGuard: false}
		if got.ctxhigh != wantCtx {
			t.Fatalf("context settings did not load as stored: %+v", got.ctxhigh)
		}
		if !reflect.DeepEqual(got.suggestedRepliesReplyCard, []string{"first"}) || !reflect.DeepEqual(got.suggestedRepliesTaskMessage, []string{"second", "third"}) {
			t.Fatalf("suggested reply lists did not load independently: reply=%#v task=%#v", got.suggestedRepliesReplyCard, got.suggestedRepliesTaskMessage)
		}
	})

	t.Run("a value rejected by the write face stops the boot loader", func(t *testing.T) {
		cases := []struct {
			name  string
			key   string
			value string
			want  string
		}{
			{name: "invalid signing secret", key: settingJWTSecret, value: "!", want: "settings auth.jwt_secret: not valid base64url: illegal base64 data at input byte 0"},
			{name: "invalid owner token TTL", key: settingOwnerTokenTTL, value: "not-a-number", want: `settings auth.owner_token_ttl: not a positive integer: "not-a-number"`},
			{name: "invalid suggested reply JSON", key: settingSuggestedRepliesTaskMessage, value: "not-json", want: `settings suggested_replies.task_message: must be a JSON array of strings: "not-json"`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				d := newAPITestDAL(t)
				if err := d.PutSetting(tc.key, tc.value); err != nil {
					t.Fatalf("PutSetting(%q): %v", tc.key, err)
				}
				_, _, err := settingsTestLoadAuth(t, d, defaultConfig())
				if err == nil {
					t.Fatal("corrupt persisted setting must stop boot")
				}
				if err.Error() != tc.want {
					t.Fatalf("corrupt setting error: got=%q want=%q", err, tc.want)
				}
			})
		}
	})
}

func TestMigrateCtxOverrides(t *testing.T) {
	t.Run("explicit file knobs fill missing rows while existing rows win", func(t *testing.T) {
		d := newAPITestDAL(t)
		if err := d.PutSetting(settingCtxHandoverPct, "70"); err != nil {
			t.Fatalf("PutSetting existing handover: %v", err)
		}
		cfg := defaultConfig()
		cfg.SseContextHigh = SseContextHighConfig{NoticePct: 45, HandoverPct: 60, MinBootSecs: 12.5, StaleGuard: false}
		cfg.SseContextHighSet = SseContextHighSet{NoticePct: true, HandoverPct: true, MinBootSecs: true, StaleGuard: true}
		var logs []string
		if err := migrateCtxOverrides(d, cfg, func(msg string) { logs = append(logs, msg) }); err != nil {
			t.Fatalf("migrateCtxOverrides: %v", err)
		}
		wantLogs := []string{"migrated oc.toml [sse_context_high] overrides into DB settings (ctx.*)"}
		if !reflect.DeepEqual(logs, wantLogs) {
			t.Fatalf("migration logs: got=%#v want=%#v", logs, wantLogs)
		}
		for key, want := range map[string]string{
			settingCtxNoticePct:   "45",
			settingCtxHandoverPct: "70",
			settingCtxMinBootSecs: "12.5",
			settingCtxStaleGuard:  "false",
		} {
			got, err := d.GetSetting(key)
			if err != nil || got == nil || *got != want {
				t.Fatalf("GetSetting(%q): want %q, got %v (err=%v)", key, want, got, err)
			}
		}
		var secondLogs []string
		if err := migrateCtxOverrides(d, cfg, func(msg string) { secondLogs = append(secondLogs, msg) }); err != nil {
			t.Fatalf("second migrateCtxOverrides: %v", err)
		}
		if len(secondLogs) != 0 {
			t.Fatalf("a second migration must be silent: %#v", secondLogs)
		}
	})

	t.Run("knobs absent from the file do not create database rows", func(t *testing.T) {
		d := newAPITestDAL(t)
		var logs []string
		if err := migrateCtxOverrides(d, defaultConfig(), func(msg string) { logs = append(logs, msg) }); err != nil {
			t.Fatalf("migrateCtxOverrides: %v", err)
		}
		if len(logs) != 0 {
			t.Fatalf("an empty file migration must not log: %#v", logs)
		}
		for _, key := range []string{settingCtxNoticePct, settingCtxHandoverPct, settingCtxMinBootSecs, settingCtxStaleGuard} {
			got, err := d.GetSetting(key)
			if err != nil || got != nil {
				t.Fatalf("GetSetting(%q): absent file knob must stay absent, got %v (err=%v)", key, got, err)
			}
		}
	})
}

func TestEnsureFirstRunClaimToken(t *testing.T) {
	t.Run("an unset password mints one stored token and reuses it", func(t *testing.T) {
		d := newAPITestDAL(t)
		var logs []string
		first, err := ensureFirstRunClaimToken(d, false, func(msg string) { logs = append(logs, msg) })
		if err != nil {
			t.Fatalf("first ensureFirstRunClaimToken: %v", err)
		}
		wantLogs := []string{"minted a first-run claim token (no password set yet)"}
		if !reflect.DeepEqual(logs, wantLogs) {
			t.Fatalf("first claim token logs: got=%#v want=%#v", logs, wantLogs)
		}
		raw, err := base64.RawURLEncoding.DecodeString(first)
		if err != nil || len(raw) != 32 {
			t.Fatalf("first-run token is not a 32-byte raw-base64url value: %q %v", first, err)
		}
		stored, err := d.GetSetting(settingClaimToken)
		if err != nil || stored == nil || *stored != first {
			t.Fatalf("minted token was not stored: %v %v", stored, err)
		}
		var secondLogs []string
		again, err := ensureFirstRunClaimToken(d, false, func(msg string) { secondLogs = append(secondLogs, msg) })
		if err != nil || again != first || len(secondLogs) != 0 {
			t.Fatalf("existing token was not reused silently: %q %q %#v", first, again, secondLogs)
		}
	})

	t.Run("a password-set server deletes a residual token and returns no token", func(t *testing.T) {
		d := newAPITestDAL(t)
		if err := d.PutSetting(settingClaimToken, "residual-claim-token"); err != nil {
			t.Fatalf("PutSetting claim token: %v", err)
		}
		var logs []string
		got, err := ensureFirstRunClaimToken(d, true, func(msg string) { logs = append(logs, msg) })
		if err != nil {
			t.Fatalf("ensureFirstRunClaimToken: %v", err)
		}
		wantLogs := []string{"deleted a residual first-run claim token (password already set)"}
		if !reflect.DeepEqual(logs, wantLogs) {
			t.Fatalf("password-set claim token logs: got=%#v want=%#v", logs, wantLogs)
		}
		if got != "" {
			t.Fatalf("password-set state returned a claim token: %q", got)
		}
		stored, err := d.GetSetting(settingClaimToken)
		if err != nil || stored != nil {
			t.Fatalf("residual token was not deleted: %v %v", stored, err)
		}
	})
}

func TestOpenAuthDAL(t *testing.T) {
	t.Run("a sqlite DSN opens and migrates a store and its close function is safe twice", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "auth.db")
		env := envOf(map[string]string{
			envConfigPath:  filepath.Join(dir, "absent.toml"),
			envDatabaseURL: "sqlite:///" + dbPath,
		})
		var out strings.Builder
		d, auth, done, rc := openAuthDAL("settings-test", env, &out)
		if rc != 0 || d == nil {
			t.Fatalf("openAuthDAL: rc=%d d=%v output=%s", rc, d, out.String())
		}
		dsn := "sqlite:///" + dbPath
		wantOutput := fmt.Sprintf(
			"[ocserverd] settings-test: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] settings-test: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] settings-test: database    = %s (DSN %s), from $OC_DATABASE_URL\n"+
				"[ocserverd] settings: minted a fresh JWT signing secret into DB settings (new install)\n",
			filepath.Join(dir, "absent.toml"), dbPath, dsn,
		)
		if out.String() != wantOutput {
			t.Fatalf("openAuthDAL output: got=%q want=%q", out.String(), wantOutput)
		}
		if len(auth.secret) != 32 {
			t.Fatalf("opened auth snapshot has no fresh signing secret: %d", len(auth.secret))
		}
		if got, err := d.GetSetting(settingJWTSecret); err != nil || got == nil {
			t.Fatalf("migrated DB has no signing secret: %v %v", got, err)
		}
		done()
		done()
		if _, err := os.Stat(dbPath); err != nil {
			t.Fatalf("database file was not created: %v", err)
		}
	})

	t.Run("a non-sqlite DSN is refused before a store is opened", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "absent.toml")
		env := envOf(map[string]string{
			envConfigPath:  configPath,
			envDatabaseURL: "postgres://example/officraft",
		})
		var out strings.Builder
		d, _, done, rc := openAuthDAL("settings-test", env, &out)
		done()
		dsn := "postgres://example/officraft"
		wantOutput := fmt.Sprintf(
			"[ocserverd] settings-test: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] settings-test: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] settings-test: database    = %s (not a sqlite DSN — no local file), from $OC_DATABASE_URL\n"+
				"[ocserverd] FATAL: settings-test supports sqlite DSNs only for now (got %q)\n",
			configPath, dsn, dsn,
		)
		if rc != 1 || d != nil || out.String() != wantOutput {
			t.Fatalf("non-sqlite resolution was not refused: rc=%d d=%v output=%s", rc, d, out.String())
		}
	})

	t.Run("a malformed config is reported without opening a database", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "oc.toml")
		if err := os.WriteFile(cfgPath, []byte("[unknown]\nvalue = 1\n"), 0o600); err != nil {
			t.Fatalf("write malformed config: %v", err)
		}
		env := envOf(map[string]string{envConfigPath: cfgPath, envDatabaseURL: "sqlite:///" + filepath.Join(dir, "never.db")})
		var out strings.Builder
		d, _, done, rc := openAuthDAL("settings-test", env, &out)
		done()
		wantOutput := fmt.Sprintf(
			"[ocserverd] FATAL: parse %s: unknown setting(s): unknown, unknown.value; only [extensions] may contain extension-owned settings\n",
			cfgPath,
		)
		if rc != 1 || d != nil || out.String() != wantOutput {
			t.Fatalf("malformed config was not refused: rc=%d d=%v output=%s", rc, d, out.String())
		}
	})
}

func TestCmdSetPassword(t *testing.T) {
	t.Run("missing password is a usage error and a valid password is stored without echoing it", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "set-password.db")
		configPath := filepath.Join(dir, "absent.toml")
		dsn := "sqlite:///" + dbPath
		env := func(password string) func(string) string {
			return envOf(map[string]string{
				envConfigPath:  configPath,
				envDatabaseURL: dsn,
				envNewPassword: password,
			})
		}
		var missing strings.Builder
		if rc := cmdSetPassword(env(""), &missing); rc != 2 {
			t.Fatalf("missing password: want rc 2, got %d", rc)
		}
		wantMissing := "[ocserverd] set-password: OC_NEW_PASSWORD must carry the new password (env, not argv — argv leaks via ps)\n"
		if missing.String() != wantMissing {
			t.Fatalf("missing-password output: got=%q want=%q", missing.String(), wantMissing)
		}
		if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
			t.Fatalf("missing password opened a database: stat err=%v", err)
		}

		var first strings.Builder
		if rc := cmdSetPassword(env("first-password"), &first); rc != 0 {
			t.Fatalf("first password: want rc 0, got %d output=%s", rc, first.String())
		}
		wantFirst := fmt.Sprintf(
			"[ocserverd] set-password: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] set-password: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] set-password: database    = %s (DSN %s), from $OC_DATABASE_URL\n"+
				"[ocserverd] settings: minted a fresh JWT signing secret into DB settings (new install)\n"+"[ocserverd] set-password: owner password hash stored in DB settings (takes effect at the next serve start)\n",
			configPath, dbPath, dsn,
		)
		if first.String() != wantFirst {
			t.Fatalf("first set-password output: got=%q want=%q", first.String(), wantFirst)
		}
		if strings.Contains(first.String(), "first-password") {
			t.Fatal("the password was echoed")
		}
		readHash := func() string {
			t.Helper()
			db, err := openSQLite(dbPath)
			if err != nil {
				t.Fatalf("open password DB: %v", err)
			}
			defer db.Close()
			got, err := NewDAL(db).GetSetting(settingPasswordHash)
			if err != nil || got == nil {
				t.Fatalf("password hash is missing: %v %v", got, err)
			}
			return *got
		}
		if !verifyPassword("first-password", readHash()) {
			t.Fatal("the first password hash does not verify")
		}

		var second strings.Builder
		if rc := cmdSetPassword(env("second-password"), &second); rc != 0 {
			t.Fatalf("replacement password: want rc 0, got %d output=%s", rc, second.String())
		}
		wantSecond := fmt.Sprintf(
			"[ocserverd] set-password: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] set-password: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] set-password: database    = %s (DSN %s), from $OC_DATABASE_URL\n"+
				"[ocserverd] set-password: owner password hash stored in DB settings (takes effect at the next serve start)\n",
			configPath, dbPath, dsn,
		)
		if second.String() != wantSecond {
			t.Fatalf("replacement set-password output: got=%q want=%q", second.String(), wantSecond)
		}
		if strings.Contains(second.String(), "second-password") || verifyPassword("first-password", readHash()) || !verifyPassword("second-password", readHash()) {
			t.Fatal("password replacement did not replace the stored credential without echoing it")
		}
	})
}

func TestCmdMFADisable(t *testing.T) {
	t.Run("clears active pending and replay-floor MFA settings while retaining other settings", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "mfa-disable.db")
		db, err := openSQLite(dbPath)
		if err != nil {
			t.Fatalf("open DB: %v", err)
		}
		if err := runMigrations(db); err != nil {
			db.Close()
			t.Fatalf("migrate DB: %v", err)
		}
		d := NewDAL(db)
		secret, err := newTOTPSecret()
		if err != nil {
			db.Close()
			t.Fatalf("new TOTP secret: %v", err)
		}
		for key, value := range map[string]string{
			settingTOTPSecret:        secret,
			settingTOTPPendingSecret: "pending-secret",
			settingTOTPLastStep:      "123",
			settingOrgName:           "kept",
		} {
			if err := d.PutSetting(key, value); err != nil {
				db.Close()
				t.Fatalf("PutSetting(%q): %v", key, err)
			}
		}
		db.Close()
		configPath := filepath.Join(dir, "absent.toml")
		dsn := "sqlite:///" + dbPath
		env := envOf(map[string]string{
			envConfigPath:  configPath,
			envDatabaseURL: dsn,
		})
		var out strings.Builder
		if rc := cmdMFADisable(env, &out); rc != 0 {
			t.Fatalf("cmdMFADisable: want rc 0, got %d output=%s", rc, out.String())
		}
		wantOutput := fmt.Sprintf(
			"[ocserverd] mfa-disable: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] mfa-disable: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] mfa-disable: database    = %s (DSN %s), from $OC_DATABASE_URL\n"+
				"[ocserverd] settings: minted a fresh JWT signing secret into DB settings (new install)\n"+"[ocserverd] mfa-disable: the owner's second factor is cleared (takes effect at the next serve start)\n",
			configPath, dbPath, dsn,
		)
		if out.String() != wantOutput {
			t.Fatalf("mfa-disable output: got=%q want=%q", out.String(), wantOutput)
		}
		check := func() {
			t.Helper()
			readDB, err := openSQLite(dbPath)
			if err != nil {
				t.Fatalf("reopen DB: %v", err)
			}
			defer readDB.Close()
			read := NewDAL(readDB)
			for _, key := range []string{settingTOTPSecret, settingTOTPPendingSecret, settingTOTPLastStep} {
				got, err := read.GetSetting(key)
				if err != nil || got != nil {
					t.Fatalf("GetSetting(%q): want nil, got %v (err=%v)", key, got, err)
				}
			}
			kept, err := read.GetSetting(settingOrgName)
			if err != nil || kept == nil || *kept != "kept" {
				t.Fatalf("unrelated setting was changed: %v %v", kept, err)
			}
		}
		check()
		var again strings.Builder
		if rc := cmdMFADisable(env, &again); rc != 0 {
			t.Fatalf("second cmdMFADisable: want rc 0, got %d output=%s", rc, again.String())
		}
		wantAgain := fmt.Sprintf(
			"[ocserverd] mfa-disable: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] mfa-disable: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] mfa-disable: database    = %s (DSN %s), from $OC_DATABASE_URL\n"+
				"[ocserverd] mfa-disable: the owner's second factor is cleared (takes effect at the next serve start)\n",
			configPath, dbPath, dsn,
		)
		if again.String() != wantAgain {
			t.Fatalf("second mfa-disable output: got=%q want=%q", again.String(), wantAgain)
		}
		check()
	})
}

func TestCmdClaimToken(t *testing.T) {
	t.Run("prints and persists the first-run token then returns no token after a password is set", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "claim-token.db")
		configPath := filepath.Join(dir, "absent.toml")
		dsn := "sqlite:///" + dbPath
		env := envOf(map[string]string{
			envConfigPath:  configPath,
			envDatabaseURL: dsn,
		})
		lastLine := func(s string) string {
			lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
			return lines[len(lines)-1]
		}
		var first strings.Builder
		if rc := cmdClaimToken(env, &first); rc != 0 {
			t.Fatalf("first claim-token: want rc 0, got %d output=%s", rc, first.String())
		}
		token := lastLine(first.String())
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(raw) != 32 || strings.HasPrefix(token, "[ocserverd]") {
			t.Fatalf("last output line is not a bare 32-byte claim token: %q %v", token, err)
		}
		wantFirst := fmt.Sprintf(
			"[ocserverd] claim-token: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] claim-token: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] claim-token: database    = %s (DSN %s), from $OC_DATABASE_URL\n"+
				"[ocserverd] settings: minted a fresh JWT signing secret into DB settings (new install)\n"+"[ocserverd] settings: minted a first-run claim token (no password set yet)\n"+
				"%s\n",
			configPath, dbPath, dsn, token,
		)
		if first.String() != wantFirst {
			t.Fatalf("first claim-token output: got=%q want=%q", first.String(), wantFirst)
		}
		readDB, err := openSQLite(dbPath)
		if err != nil {
			t.Fatalf("open claim DB: %v", err)
		}
		stored, err := NewDAL(readDB).GetSetting(settingClaimToken)
		readDB.Close()
		if err != nil || stored == nil || *stored != token {
			t.Fatalf("printed claim token was not persisted: %v %v", stored, err)
		}

		var second strings.Builder
		if rc := cmdClaimToken(env, &second); rc != 0 {
			t.Fatalf("second claim-token: want rc 0, got %d output=%s", rc, second.String())
		}
		wantSecond := fmt.Sprintf(
			"[ocserverd] claim-token: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] claim-token: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] claim-token: database    = %s (DSN %s), from $OC_DATABASE_URL\n"+
				"%s\n",
			configPath, dbPath, dsn, token,
		)
		if second.String() != wantSecond {
			t.Fatalf("second claim-token output: got=%q want=%q", second.String(), wantSecond)
		}

		envWithPassword := envOf(map[string]string{
			envConfigPath:  filepath.Join(dir, "absent.toml"),
			envDatabaseURL: "sqlite:///" + dbPath,
			envNewPassword: "owner-password",
		})
		var setOut strings.Builder
		if rc := cmdSetPassword(envWithPassword, &setOut); rc != 0 {
			t.Fatalf("set password before claim: want rc 0, got %d output=%s", rc, setOut.String())
		}
		wantSet := fmt.Sprintf(
			"[ocserverd] set-password: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] set-password: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] set-password: database    = %s (DSN %s), from $OC_DATABASE_URL\n"+
				"[ocserverd] set-password: owner password hash stored in DB settings (takes effect at the next serve start)\n",
			configPath, dbPath, dsn,
		)
		if setOut.String() != wantSet {
			t.Fatalf("set-password output before claim: got=%q want=%q", setOut.String(), wantSet)
		}
		var after strings.Builder
		if rc := cmdClaimToken(env, &after); rc != 3 {
			t.Fatalf("claim-token after password: want rc 3, got %d output=%s", rc, after.String())
		}
		wantAfter := fmt.Sprintf(
			"[ocserverd] claim-token: config file = none (looked at %s, from $OC_CONFIG)\n"+
				"[ocserverd] claim-token: to point this run at a config file, set OC_CONFIG=/path/to/oc.toml; it is set now, but names a file that is not there, and while it is set ./oc.toml is not consulted. Without one, nothing is read from oc.toml — $OC_DATABASE_URL and the built-in defaults decide the rest.\n"+
				"[ocserverd] claim-token: database    = %s (DSN %s), from $OC_DATABASE_URL\n"+
				"[ocserverd] settings: deleted a residual first-run claim token (password already set)\n"+
				"[ocserverd] claim-token: a password is already set — no claim token exists\n",
			configPath, dbPath, dsn,
		)
		if after.String() != wantAfter {
			t.Fatalf("password-set claim-token output: got=%q want=%q", after.String(), wantAfter)
		}
		if strings.Contains(after.String(), token) || lastLine(after.String()) == token {
			t.Fatal("claim-token was revealed after a password was set")
		}
		readDB, err = openSQLite(dbPath)
		if err != nil {
			t.Fatalf("reopen claim DB: %v", err)
		}
		residual, err := NewDAL(readDB).GetSetting(settingClaimToken)
		readDB.Close()
		if err != nil || residual != nil {
			t.Fatalf("residual claim token was not deleted: %v %v", residual, err)
		}
	})
}

func TestApplyCtxOverrides(t *testing.T) {
	t.Run("absent rows keep ordinary values except for the derived notice point", func(t *testing.T) {
		d := newAPITestDAL(t)
		cfg := SseContextHighConfig{NoticePct: 99, HandoverPct: 70, MinBootSecs: 1.5, StaleGuard: true}
		if err := applyCtxOverrides(d, &cfg); err != nil {
			t.Fatalf("applyCtxOverrides: %v", err)
		}
		want := SseContextHighConfig{NoticePct: 60, HandoverPct: 70, MinBootSecs: 1.5, StaleGuard: true}
		if cfg != want {
			t.Fatalf("absent rows: got=%+v want=%+v", cfg, want)
		}
	})

	t.Run("stored rows override all corresponding context values", func(t *testing.T) {
		d := newAPITestDAL(t)
		for key, value := range map[string]string{
			settingCtxHandoverPct: "75",
			settingCtxNoticePct:   "65",
			settingCtxMinBootSecs: "12.5",
			settingCtxStaleGuard:  "false",
		} {
			if err := d.PutSetting(key, value); err != nil {
				t.Fatalf("PutSetting(%q): %v", key, err)
			}
		}
		cfg := SseContextHighConfig{NoticePct: 10, HandoverPct: 20, MinBootSecs: 1, StaleGuard: true}
		if err := applyCtxOverrides(d, &cfg); err != nil {
			t.Fatalf("applyCtxOverrides: %v", err)
		}
		want := SseContextHighConfig{NoticePct: 65, HandoverPct: 75, MinBootSecs: 12.5, StaleGuard: false}
		if cfg != want {
			t.Fatalf("stored rows did not fully override context: got=%+v want=%+v", cfg, want)
		}
	})

	t.Run("malformed stored values return named errors", func(t *testing.T) {
		cases := []struct {
			name  string
			key   string
			value string
			want  string
		}{
			{name: "handover integer", key: settingCtxHandoverPct, value: "bad", want: `settings ctx.handover_pct: not an integer: "bad"`},
			{name: "notice integer", key: settingCtxNoticePct, value: "bad", want: `settings ctx.notice_pct: not a non-negative integer: "bad"`},
			{name: "minimum boot number", key: settingCtxMinBootSecs, value: "bad", want: `settings ctx.min_boot_secs: not a number: "bad"`},
			{name: "stale guard boolean", key: settingCtxStaleGuard, value: "bad", want: `settings ctx.stale_guard: not a bool: "bad"`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				d := newAPITestDAL(t)
				if err := d.PutSetting(tc.key, tc.value); err != nil {
					t.Fatalf("PutSetting(%q): %v", tc.key, err)
				}
				cfg := defaultSseContextHigh()
				err := applyCtxOverrides(d, &cfg)
				if err == nil {
					t.Fatal("malformed context setting must return an error")
				}
				if err.Error() != tc.want {
					t.Fatalf("malformed context error: got=%q want=%q", err, tc.want)
				}
			})
		}
	})
}
