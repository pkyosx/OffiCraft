package main

// settings.go — the DB settings store's read layer (owner-password-in-db
// design; B1 storage swap + B2 config shrink): the closed settings key set,
// the boot-time snapshot the server runs on, the one-shot oc.toml → DB
// auto-migration for existing installs, and the local CLI seams — set-password
// (harness / operator-rescue credential write) and claim-token (the installer
// banner's read of the one-shot first-run claim code).
//
// Read precedence: DB settings → code defaults. oc.toml's retired [auth] /
// [sse_context_high] keys are consumed ONLY by the one-shot migration here
// (loader warns + runtime ignores them — config.go). The snapshot is loaded
// ONCE at serve start — no per-request DB reads; the B3 settings PATCH
// endpoint will update the in-memory copy alongside the DB write.

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
)

// The closed settings key set. The setting table is schemaless key-value; the
// reader here holds the schema (type, default, who writes). Keys not listed
// are never read.
const (
	// settingPasswordHash is the argon2id PHC hash of the owner password
	// (password.go). Absent = password not yet set (first-run flow, B3).
	settingPasswordHash = "auth.password_hash"
	// settingJWTSecret is the HS256 signing secret, base64url of the raw key
	// bytes. Always present after first boot (migrated or minted).
	settingJWTSecret = "auth.jwt_secret"
	// settingPasswordChangedAt (epoch seconds, default 0) is written by
	// change-password (B3); owner-scope tokens with iat before it are refused
	// once the B3 verification lands. The oc.toml migration deliberately does
	// NOT stamp it — migrating is not a password change, and pre-migration
	// tokens must survive.
	settingPasswordChangedAt = "auth.password_changed_at"
	// settingTokenTTL is the owner-login JWT lifetime in seconds (default
	// defaultTokenTTL).
	settingTokenTTL = "auth.token_ttl"
	// settingClaimToken is the ONE-SHOT first-run claim token (B3): minted at
	// serve start while no password is set, printed only to the local serve
	// log / installer banner, required by POST /api/auth/set-password and
	// deleted on success (possession proves host shell access — the gate
	// against a public-tunnel visitor claiming a fresh server).
	settingClaimToken = "auth.claim_token"
	// ctx.* mirror the SseContextHighConfig knobs (defaults in
	// defaultSseContextHigh; only handover_pct gets UI in B3).
	settingCtxWarnPct       = "ctx.warn_pct"
	settingCtxHandoverPct   = "ctx.handover_pct"
	settingCtxRemindStepPct = "ctx.remind_step_pct"
	settingCtxMinBootSecs   = "ctx.min_boot_secs"
	settingCtxStaleGuard    = "ctx.stale_guard"
	// settingOutsourceMaxParallel (M3, owner ruling ③) is the GLOBAL cap on
	// concurrently live (assigned + active) outsource workers — the Phase 2
	// assignment scheduler's admission knob; member tasks never count (H7).
	settingOutsourceMaxParallel = "task.outsource_max_parallel"
	// The retired updater.url / updater.invite_code keys belonged to the
	// removed ocupdaterd updater-server chain (updates now ship as GitHub
	// Releases on pkyosx/OffiCraft — update_check.go). They are no longer
	// read or written; stale rows in an old DB are simply ignored (the key
	// set is closed — "keys not listed are never read"). The two toggles
	// below SURVIVE the teardown with their DB names unchanged (an armed
	// install stays armed across the migration).
	//
	// settingUpdaterReceiveBeta (bool, default false) picks WHICH GitHub
	// releases the update check follows: false = official releases only,
	// true = prereleases too (the GitHub `--prerelease` flag replaces the
	// old updater's beta channel).
	settingUpdaterReceiveBeta = "updater.receive_beta"
	// settingUpdaterAutoUpdate (bool, default false) arms the background
	// self-upgrade loop (auto_update.go): when ON and GitHub has a newer
	// release, the server runs the same verified upgrade body as the manual
	// endpoint and re-execs itself — unattended. Default OFF: upgrading
	// stays an explicit owner action unless the owner opts in.
	settingUpdaterAutoUpdate = "updater.auto_update"
	// settingOrgName (T-d693) is the studio display name shown in the cockpit
	// topbar ("AI 工作室"). NOT secret — the owner sets it (PATCH /api/settings),
	// and every agent reads it back through get_global_context so a member knows
	// which studio it serves. "" (default) = never set: the topbar falls back to
	// the localized default string (frontend), and agents see an empty name.
	settingOrgName = "org.name"
)

// defaultOutsourceMaxParallel is the code-side default when the key was never
// written.
const defaultOutsourceMaxParallel = 3

// authSettings is the boot-time snapshot cmdServe stamps onto the apiServer.
type authSettings struct {
	secret               []byte
	passwordHash         string // "" = not set in DB (first-run: set-password flow)
	passwordChangedAt    int64  // epoch secs; owner tokens with iat before it are refused
	tokenTTL             int64
	ctxhigh              SseContextHighConfig
	outsourceMaxParallel int    // task.outsource_max_parallel (default 3)
	updaterReceiveBeta   bool   // updater.receive_beta (default false = official releases only)
	updaterAutoUpdate    bool   // updater.auto_update (default false = manual upgrades only)
	orgName              string // org.name ("" = never set → localized default in the topbar)
}

// loadAuthSettings loads the snapshot from the migrated DB, running the
// one-shot oc.toml → DB migration for whatever is not in the DB yet:
//
//   - JWT secret: DB value wins. Absent + oc.toml has an explicit
//     [auth].secret → import it verbatim. Absent + oc.toml has a password
//     (the existing-install shape) → import the password-DERIVED secret
//     (deriveSecretFromPassword), NOT a fresh mint — every already-issued
//     token (400-day agent tokens, warden tokens) is signed with that derived
//     key, so importing it means zero token invalidation; the secret is
//     thereafter pinned in the DB, decoupled from the password. Only a truly
//     fresh install (no DB value, no oc.toml auth) mints a random secret.
//   - Password: DB hash wins; absent + oc.toml plaintext → store its argon2id
//     hash (the plaintext itself never enters the DB).
//   - token_ttl: DB → explicitly-written oc.toml value (migrated in) → default.
//   - ctx.*: DB overrides on top of the oc.toml/[defaults] config.
func loadAuthSettings(d *DAL, cfg Config, logf func(string)) (authSettings, error) {
	out := authSettings{
		tokenTTL: int64(cfg.Auth.TokenTTL),
		ctxhigh:  cfg.SseContextHigh,
	}

	stored, err := d.GetSetting(settingJWTSecret)
	if err != nil {
		return out, err
	}
	if stored != nil {
		raw, err := base64.RawURLEncoding.DecodeString(*stored)
		if err != nil || len(raw) == 0 {
			return out, fmt.Errorf("settings %s: not valid base64url: %v", settingJWTSecret, err)
		}
		out.secret = raw
	} else {
		var key []byte
		switch {
		case cfg.Auth.Secret != "":
			key = []byte(cfg.Auth.Secret)
			logf("migrated oc.toml [auth].secret into DB settings")
		case cfg.Auth.Password != "":
			key = deriveSecretFromPassword(cfg.Auth.Password)
			logf("migrated the password-derived JWT secret into DB settings (existing tokens stay valid)")
		default:
			key = make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return out, err
			}
			logf("minted a fresh JWT signing secret into DB settings (new install)")
		}
		if err := d.PutSetting(settingJWTSecret, base64.RawURLEncoding.EncodeToString(key)); err != nil {
			return out, err
		}
		out.secret = key
	}

	hash, err := d.GetSetting(settingPasswordHash)
	if err != nil {
		return out, err
	}
	if hash != nil {
		out.passwordHash = *hash
	} else if cfg.Auth.Password != "" {
		phc, err := hashPassword(cfg.Auth.Password)
		if err != nil {
			return out, err
		}
		if err := d.PutSetting(settingPasswordHash, phc); err != nil {
			return out, err
		}
		out.passwordHash = phc
		logf("migrated oc.toml [auth].password into DB settings as an argon2id hash")
	}

	ttl, err := d.GetSetting(settingTokenTTL)
	if err != nil {
		return out, err
	}
	if ttl != nil {
		n, err := strconv.ParseInt(*ttl, 10, 64)
		if err != nil || n <= 0 {
			return out, fmt.Errorf("settings %s: not a positive integer: %q", settingTokenTTL, *ttl)
		}
		out.tokenTTL = n
	} else if cfg.Auth.TokenTTLSet {
		if err := d.PutSetting(settingTokenTTL, strconv.Itoa(cfg.Auth.TokenTTL)); err != nil {
			return out, err
		}
		logf("migrated oc.toml [auth].token_ttl into DB settings")
	}

	changed, err := d.GetSetting(settingPasswordChangedAt)
	if err != nil {
		return out, err
	}
	if changed != nil {
		n, err := strconv.ParseInt(*changed, 10, 64)
		if err != nil || n < 0 {
			return out, fmt.Errorf("settings %s: not a non-negative integer: %q", settingPasswordChangedAt, *changed)
		}
		out.passwordChangedAt = n
	}

	if err := migrateCtxOverrides(d, cfg, logf); err != nil {
		return out, err
	}
	if err := applyCtxOverrides(d, &out.ctxhigh); err != nil {
		return out, err
	}

	out.outsourceMaxParallel = defaultOutsourceMaxParallel
	if v, err := d.GetSetting(settingOutsourceMaxParallel); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || n < 0 {
			return out, fmt.Errorf(
				"settings %s: not a non-negative integer: %q",
				settingOutsourceMaxParallel, *v)
		}
		out.outsourceMaxParallel = n
	}

	getBool := func(key string, dst *bool) error {
		v, err := d.GetSetting(key)
		if err != nil || v == nil {
			return err
		}
		b, err := strconv.ParseBool(*v)
		if err != nil {
			return fmt.Errorf("settings %s: not a bool: %q", key, *v)
		}
		*dst = b
		return nil
	}
	if err := getBool(settingUpdaterReceiveBeta, &out.updaterReceiveBeta); err != nil {
		return out, err
	}
	if err := getBool(settingUpdaterAutoUpdate, &out.updaterAutoUpdate); err != nil {
		return out, err
	}
	if v, err := d.GetSetting(settingOrgName); err != nil {
		return out, err
	} else if v != nil {
		out.orgName = *v
	}
	return out, nil
}

// migrateCtxOverrides is the [sse_context_high] leg of the one-shot oc.toml →
// DB migration: each knob the file wrote EXPLICITLY is imported into its
// ctx.* settings key unless the DB already has one (DB wins forever after).
// Without this, retiring the table would silently reset a tuned install to
// the defaults. Absent-from-file knobs are never written (code default).
func migrateCtxOverrides(d *DAL, cfg Config, logf func(string)) error {
	imported := false
	put := func(set bool, key, value string) error {
		if !set {
			return nil
		}
		stored, err := d.GetSetting(key)
		if err != nil || stored != nil {
			return err
		}
		if err := d.PutSetting(key, value); err != nil {
			return err
		}
		imported = true
		return nil
	}
	c, s := cfg.SseContextHigh, cfg.SseContextHighSet
	if err := put(s.WarnPct, settingCtxWarnPct, strconv.Itoa(c.WarnPct)); err != nil {
		return err
	}
	if err := put(s.HandoverPct, settingCtxHandoverPct, strconv.Itoa(c.HandoverPct)); err != nil {
		return err
	}
	if err := put(s.RemindStepPct, settingCtxRemindStepPct, strconv.Itoa(c.RemindStepPct)); err != nil {
		return err
	}
	if err := put(s.MinBootSecs, settingCtxMinBootSecs, strconv.FormatFloat(c.MinBootSecs, 'f', -1, 64)); err != nil {
		return err
	}
	if err := put(s.StaleGuard, settingCtxStaleGuard, strconv.FormatBool(c.StaleGuard)); err != nil {
		return err
	}
	if imported {
		logf("migrated oc.toml [sse_context_high] overrides into DB settings (ctx.*)")
	}
	return nil
}

// ensureFirstRunClaimToken keeps the one-shot claim token in step with the
// password state at serve start. Password NOT set: return the existing token
// or mint one (32 random bytes, base64url) — cmdServe prints it to the serve
// log so the first-run UI flow can consume it. Password set: any residual
// token (e.g. the CLI set-password seam raced first) is deleted and "" is
// returned — a stale claim token must never outlive the credential it gated.
func ensureFirstRunClaimToken(d *DAL, passwordSet bool, logf func(string)) (string, error) {
	stored, err := d.GetSetting(settingClaimToken)
	if err != nil {
		return "", err
	}
	if passwordSet {
		if stored != nil {
			if err := d.DeleteSetting(settingClaimToken); err != nil {
				return "", err
			}
			logf("deleted a residual first-run claim token (password already set)")
		}
		return "", nil
	}
	if stored != nil {
		return *stored, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if err := d.PutSetting(settingClaimToken, token); err != nil {
		return "", err
	}
	logf("minted a first-run claim token (no password set yet)")
	return token, nil
}

// envNewPassword feeds cmdSetPassword: the password rides the environment,
// never argv (argv is world-readable via ps while the process runs).
const envNewPassword = "OC_NEW_PASSWORD"

// openAuthDAL is the shared plumbing of the local settings subcommands
// (set-password / claim-token): resolve config + DSN (sqlite only), open +
// migrate the store, load the auth snapshot (running the one-shot oc.toml →
// DB migration first, so an old-style install's file credential is imported
// before either seam looks at the password state). A non-zero rc means the
// error is already printed; done is always safe to call.
func openAuthDAL(name string, env func(string) string, out io.Writer) (d *DAL, auth authSettings, done func(), rc int) {
	done = func() {}
	cfg, warnings, err := loadConfig(configPath(env))
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return nil, auth, done, 1
	}
	for _, w := range warnings {
		fmt.Fprintf(out, "[ocserverd] WARN: %s\n", w)
	}
	dsn := resolveDSN(env, cfg)
	dbPath, ok := sqliteFilePath(dsn)
	if !ok {
		fmt.Fprintf(out, "[ocserverd] FATAL: %s supports sqlite DSNs only for now (got %q)\n", name, dsn)
		return nil, auth, done, 1
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: open %s: %v\n", dbPath, err)
		return nil, auth, done, 1
	}
	done = func() { db.Close() }
	if err := runMigrations(db); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: goose up: %v\n", err)
		return nil, auth, done, 1
	}
	d = NewDAL(db)
	auth, err = loadAuthSettings(d, cfg, func(msg string) {
		fmt.Fprintf(out, "[ocserverd] settings: %s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: load settings: %v\n", err)
		return nil, auth, done, 1
	}
	return d, auth, done, 0
}

// cmdSetPassword (ocserverd set-password) writes the owner password's
// argon2id hash straight into the DB settings — the local seam the test
// harnesses (conformance/e2e) use to seed a KNOWN credential, and the
// operator's shell-access rescue when the password is lost. Fresh installs
// no longer seed a password here: the first-run claim flow (claim token →
// POST /api/auth/set-password) is how the owner sets one.
//
// The password comes from $OC_NEW_PASSWORD (env, never argv — argv leaks via
// ps). Exit codes: 0 = written, 1 = fatal, 2 = usage.
func cmdSetPassword(env func(string) string, out io.Writer) int {
	password := env(envNewPassword)
	if password == "" {
		fmt.Fprintf(out, "[ocserverd] set-password: %s must carry the new password (env, not argv — argv leaks via ps)\n", envNewPassword)
		return 2
	}
	d, _, done, rc := openAuthDAL("set-password", env, out)
	defer done()
	if rc != 0 {
		return rc
	}
	phc, err := hashPassword(password)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: hash password: %v\n", err)
		return 1
	}
	if err := d.PutSetting(settingPasswordHash, phc); err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: store password hash: %v\n", err)
		return 1
	}
	fmt.Fprintln(out, "[ocserverd] set-password: owner password hash stored in DB settings (takes effect at the next serve start)")
	return 0
}

// cmdClaimToken (ocserverd claim-token) prints the one-shot first-run claim
// code so the installer banner can show it after serve is healthy — a local
// DB read behind shell access, mirroring the serve-log print; the code never
// rides an unauthenticated HTTP endpoint. Password not set: the existing
// token is printed (minted if absent — ensureFirstRunClaimToken is the single
// authority, so serve reuses it). Password set: nothing to claim, exit 3.
// The token is the LAST line of output (settings/migration notes are
// "[ocserverd]"-prefixed lines above it). Exit codes: 0 = printed, 3 = no
// token (password already set), 1 = fatal.
func cmdClaimToken(env func(string) string, out io.Writer) int {
	d, auth, done, rc := openAuthDAL("claim-token", env, out)
	defer done()
	if rc != 0 {
		return rc
	}
	token, err := ensureFirstRunClaimToken(d, auth.passwordHash != "", func(msg string) {
		fmt.Fprintf(out, "[ocserverd] settings: %s\n", msg)
	})
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: claim token: %v\n", err)
		return 1
	}
	if token == "" {
		fmt.Fprintln(out, "[ocserverd] claim-token: a password is already set — no claim token exists")
		return 3
	}
	fmt.Fprintln(out, token)
	return 0
}

// applyCtxOverrides layers any DB-written ctx.* values onto the config-derived
// SseContextHighConfig (absent keys keep the incoming value).
func applyCtxOverrides(d *DAL, c *SseContextHighConfig) error {
	getInt := func(key string, dst *int) error {
		v, err := d.GetSetting(key)
		if err != nil || v == nil {
			return err
		}
		n, err := strconv.Atoi(*v)
		if err != nil {
			return fmt.Errorf("settings %s: not an integer: %q", key, *v)
		}
		*dst = n
		return nil
	}
	if err := getInt(settingCtxWarnPct, &c.WarnPct); err != nil {
		return err
	}
	if err := getInt(settingCtxHandoverPct, &c.HandoverPct); err != nil {
		return err
	}
	if err := getInt(settingCtxRemindStepPct, &c.RemindStepPct); err != nil {
		return err
	}
	if v, err := d.GetSetting(settingCtxMinBootSecs); err != nil {
		return err
	} else if v != nil {
		f, err := strconv.ParseFloat(*v, 64)
		if err != nil {
			return fmt.Errorf("settings %s: not a number: %q", settingCtxMinBootSecs, *v)
		}
		c.MinBootSecs = f
	}
	if v, err := d.GetSetting(settingCtxStaleGuard); err != nil {
		return err
	} else if v != nil {
		b, err := strconv.ParseBool(*v)
		if err != nil {
			return fmt.Errorf("settings %s: not a bool: %q", settingCtxStaleGuard, *v)
		}
		c.StaleGuard = b
	}
	return nil
}
