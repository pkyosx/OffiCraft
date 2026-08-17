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
	"encoding/json"
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
	// settingLegacyTokenTTL is the former shared TTL. It is read only as a
	// migration source; upgrades copy it into BOTH independent successor keys.
	settingLegacyTokenTTL = "auth.token_ttl"
	settingOwnerTokenTTL  = "auth.owner_token_ttl"
	settingAgentTokenTTL  = "auth.agent_token_ttl"
	// settingClaimToken is the ONE-SHOT first-run claim token (B3): minted at
	// serve start while no password is set, printed only to the local serve
	// log / installer banner, required by POST /api/auth/set-password and
	// deleted on success (possession proves host shell access — the gate
	// against a public-tunnel visitor claiming a fresh server).
	settingClaimToken = "auth.claim_token"
	// ctx.* mirror the SseContextHighConfig knobs (defaults in
	// defaultSseContextHigh; only handover_pct gets UI in B3).
	//
	// `ctx.warn_pct` and `ctx.remind_step_pct` are RETIRED (T-c382) and
	// deliberately not listed: the key set is closed, so an unlisted key is
	// never read — which is exactly the intent. They used to hold a SECOND
	// threshold beside handover_pct, and the advance notice is now derived from
	// handover_pct instead. Old rows stay in the table, unread. Do not re-add
	// them; two thresholds for one decision is the bug this ticket fixed.
	settingCtxNoticePct             = "ctx.notice_pct"
	settingCtxHandoverPct           = "ctx.handover_pct"
	settingCtxMinBootSecs           = "ctx.min_boot_secs"
	settingCtxStaleGuard            = "ctx.stale_guard"
	settingCodexCompactionThreshold = "codex.compaction_threshold"
	settingCodexNoticeRound         = "codex.notice_round"
	settingMonitoringRefreshSeconds = "monitoring.refresh_seconds"
	// settingOutsourceMaxParallel (M3, owner ruling ③) is the GLOBAL cap on
	// concurrently live (assigned + active) outsource workers — the Phase 2
	// assignment scheduler's admission knob; member tasks never count (H7).
	settingOutsourceMaxParallel = "task.outsource_max_parallel"
	// settingDocCapChars* (T-3aeb, owner 2026-07-31; split four ways in T-ae38,
	// owner 2026-08-03; the manual's one split again in T-30f1) are the size
	// caps on the accumulating context documents — see contextDocMaxCharsDefault
	// in domain.go for the rule they feed. Adjustable so the owner can raise one
	// without a release; each floor equals that segment's default, so a cap only
	// ever goes UP.
	//
	// 🔴 EVERY key carries a suffix, including the ones that inherited an older
	// key's value. There is no `doc.cap_chars` any more, and since T-30f1 no
	// `doc.cap_chars.manual` either, and that is the point: an agent reading
	// `get_settings` sees key NAMES with no descriptions attached, so a key that
	// names a WHOLE artefact sitting beside the segments it was split into reads
	// as "the default for all of them". Someone wanting to raise the manual's
	// learnings cap would edit `.manual` and believe they had moved both halves,
	// and nothing would say otherwise. Each rename costs one migration, which
	// had to be written regardless — the value has to move either way.
	//
	// The task manual's `sop_md` / `learnings` answer to `.manual_sop` /
	// `.manual_learnings`, NOT to any of the three role-journal segments: they
	// are keyed by `type_key`, so they are assets of a task TYPE, not entries in
	// a role's journal. They are two keys and not one because the SOP is a
	// blueprint that is refined in place while the learnings accumulate — one
	// number could only ever be right for one of them.
	settingDocCapCharsDuty            = "doc.cap_chars.duty"
	settingDocCapCharsInsight         = "doc.cap_chars.insight"
	settingDocCapCharsLearning        = "doc.cap_chars.learning"
	settingDocCapCharsManualSop       = "doc.cap_chars.manual_sop"
	settingDocCapCharsManualLearnings = "doc.cap_chars.manual_learnings"
	// The two boot-context document kinds (T-791e). Same shape as the five
	// above, and deliberately suffixed the same way: a bare `doc.cap_chars`
	// would read as a global default beside them, and an agent looking at
	// get_settings sees key
	// names only — never a description.
	settingDocCapCharsSystemInteraction = "doc.cap_chars.system_interaction"
	settingDocCapCharsBootSequence      = "doc.cap_chars.boot_sequence"
	settingDocCapCharsOffboard          = "doc.cap_chars.offboard"
	// settingChatBudgetChars (T-c9b4) is the wake snapshot's chat block budget —
	// what resumeChatPackBudget spends (api_chat.go). It is deliberately NOT a
	// `doc.cap_chars.*` key: those cap a STORED document and their floors equal
	// their own defaults so a cap can only be raised, while this one bounds a
	// block that is repacked from scratch on every read and is therefore free to
	// move in both directions. See domain.go for the range and why its ceiling is
	// tied to resumeChatFetch.
	settingChatBudgetChars = "chat.budget_chars"
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
	// settingOwnerName (T-0b41) is the owner's display nickname shown in the
	// cockpit topbar profile pill. Server-backed (PATCH /api/settings) so the
	// nickname syncs across the owner's devices. "" (default) = never set: the
	// pill falls back to the localized default label (frontend). Unlike
	// org.name it is NOT an agent read path — it never enters get_global_context.
	settingOwnerName = "owner.name"
	// settingPushContactEmail (T-8a82) is the contact address handed to the push
	// gateways as the VAPID subject. It is owner-supplied because the server
	// cannot know a reachable identity for itself — it sits behind a tunnel and
	// its public hostname is a deployment fact. "" (default) = never set, and
	// delivery is then refused outright rather than attempted with a made-up
	// address: Apple answers BadJwtToken for anything on an unreachable domain,
	// which takes push down on every device with no visible error.
	settingPushContactEmail = "push.contact_email"
	// settingDisplayTheme (T-0b41-p2) is the owner's cockpit visual theme
	// ("office", the only built-in, or a custom theme id). Server-backed (PATCH
	// /api/settings) so the choice
	// syncs across the owner's devices — but it must also apply BEFORE login, so
	// the frontend keeps a localStorage cache and treats this server value as the
	// cross-device source of truth reconciled at login. "" (default) = never set:
	// the frontend keeps its cached/default theme. NOT an agent read path.
	settingDisplayTheme = "display.theme"
	// settingDisplayLanguage (T-0b41-p2) is the owner's cockpit language
	// ("zh" / "en"). Same dual-layer contract as display.theme: server is the
	// cross-device truth, localStorage the pre-auth cache. "" (default) = never
	// set. NOT an agent read path.
	settingDisplayLanguage = "display.language"
	// settingDisplayWide (T-756f, bool, default false) picks the cockpit LAYOUT
	// width: false (never set) keeps the centred ~1040px content column the
	// cockpit has always shipped; true lifts that cap so the topbar / nav tabs /
	// main area span the window (the 22px side gutters stay either way). Same
	// dual-layer contract as display.theme: server is the cross-device truth,
	// localStorage the pre-auth cache. Stored like the updater toggles —
	// strconv.FormatBool text, absent row = false. NOT an agent read path.
	settingDisplayWide = "display.wide"
	// settingDisplayCustomThemes (T-16a1 P2) is the owner's saved custom theme
	// bundles — a JSON array of {id,name,colors} colour bundles (theme_bundle.go).
	// Server-backed so the set syncs across devices; display.theme may point at
	// any id in it. Absent/"" = none saved. NOT an agent read path.
	settingDisplayCustomThemes = "display.custom_themes"
)

// displayThemeAllowed / displayLanguageAllowed are the enum whitelists for the
// two display prefs (T-0b41-p2). A PATCH value outside the set (and non-empty,
// which clears back to unset) is a 422 — the frontend only ever renders these
// concrete values, so an out-of-set string would only be corruption.
var displayThemeAllowed = map[string]bool{"office": true}
var displayLanguageAllowed = map[string]bool{"zh": true, "en": true}

// defaultOutsourceMaxParallel is the code-side default when the key was never
// written.
const defaultOutsourceMaxParallel = 3
const defaultCodexCompactionThreshold = 3
const defaultMonitoringRefreshSeconds = 5

// authSettings is the boot-time snapshot cmdServe stamps onto the apiServer.
type authSettings struct {
	secret                       []byte
	passwordHash                 string // "" = not set in DB (first-run: set-password flow)
	passwordChangedAt            int64  // epoch secs; owner tokens with iat before it are refused
	ownerTokenTTL                int64
	agentTokenTTL                int64
	ctxhigh                      SseContextHighConfig
	codexCompactionThreshold     int // codex.compaction_threshold — the FINAL round (handover)
	codexNoticeRound             int // codex.notice_round — the FIRST, soft notice round (T-a9d6)
	monitoringRefreshSeconds     int
	outsourceMaxParallel         int              // task.outsource_max_parallel (default 3)
	docCapCharsDuty              int              // doc.cap_chars.duty (default dutyCapCharsDefault)
	docCapCharsInsight           int              // doc.cap_chars.insight (default contextDocMaxCharsDefault)
	docCapCharsLearning          int              // doc.cap_chars.learning (default contextDocMaxCharsDefault)
	docCapCharsManualSop         int              // doc.cap_chars.manual_sop (default contextDocMaxCharsDefault)
	docCapCharsManualLearnings   int              // doc.cap_chars.manual_learnings (default contextDocMaxCharsDefault)
	docCapCharsSystemInteraction int              // doc.cap_chars.system_interaction (default systemInteractionCapCharsDefault)
	docCapCharsBootSequence      int              // doc.cap_chars.boot_sequence (default bootSequenceCapCharsDefault; ONE cap, both runtimes)
	docCapCharsOffboard          int              // doc.cap_chars.offboard (default offboardCapCharsDefault)
	chatBudgetChars              int              // chat.budget_chars (default chatBudgetCharsDefault)
	updaterReceiveBeta           bool             // updater.receive_beta (default false = official releases only)
	updaterAutoUpdate            bool             // updater.auto_update (default false = manual upgrades only)
	orgName                      string           // org.name ("" = never set → localized default in the topbar)
	ownerName                    string           // owner.name ("" = never set → localized default in the profile pill)
	pushContactEmail             string           // push.contact_email ("" = never set → Web Push delivery is refused)
	displayTheme                 string           // display.theme ("" = never set → frontend cache/default)
	displayLanguage              string           // display.language ("" = never set → frontend cache/default)
	displayWide                  bool             // display.wide (default false = the narrow centred column)
	displayCustomThemes          []ThemeBundleDTO // display.custom_themes (nil = none saved)
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
//   - token TTLs: successor DB keys win independently. On upgrade, the legacy
//     shared value is copied to each missing successor, preserving both
//     deployed behaviours; fresh installs use their separate code defaults.
//   - ctx.*: DB overrides on top of the oc.toml/[defaults] config.
func loadAuthSettings(d *DAL, cfg Config, logf func(string)) (authSettings, error) {
	out := authSettings{
		ownerTokenTTL:            defaultOwnerTokenTTL,
		agentTokenTTL:            defaultAgentTokenTTL,
		ctxhigh:                  cfg.SseContextHigh,
		codexCompactionThreshold: defaultCodexCompactionThreshold,
		monitoringRefreshSeconds: defaultMonitoringRefreshSeconds,
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

	legacyTTL, err := d.GetSetting(settingLegacyTokenTTL)
	if err != nil {
		return out, err
	}
	if legacyTTL == nil && cfg.Auth.TokenTTLSet {
		v := strconv.Itoa(cfg.Auth.TokenTTL)
		legacyTTL = &v
	}
	for _, target := range []struct {
		key      string
		dst      *int64
		fallback int64
	}{
		{settingOwnerTokenTTL, &out.ownerTokenTTL, defaultOwnerTokenTTL},
		{settingAgentTokenTTL, &out.agentTokenTTL, defaultAgentTokenTTL},
	} {
		stored, err := d.GetSetting(target.key)
		if err != nil {
			return out, err
		}
		if stored == nil && legacyTTL != nil {
			if err := d.PutSetting(target.key, *legacyTTL); err != nil {
				return out, err
			}
			stored = legacyTTL
			logf("migrated legacy auth.token_ttl into " + target.key)
		}
		if stored == nil {
			*target.dst = target.fallback
			continue
		}
		n, err := strconv.ParseInt(*stored, 10, 64)
		if err != nil || n <= 0 {
			return out, fmt.Errorf("settings %s: not a positive integer: %q", target.key, *stored)
		}
		*target.dst = n
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
	if v, err := d.GetSetting(settingCodexCompactionThreshold); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || n < 1 || n > 10 {
			return out, fmt.Errorf("settings %s: must be 1..10: %q", settingCodexCompactionThreshold, *v)
		}
		out.codexCompactionThreshold = n
	}
	// codex.notice_round is the FIRST, soft notice round (T-a9d6). Absent (every
	// install that predates the pair) → threshold - 1, which is exactly where
	// T-c382 derived it, so an upgrade changes no behaviour.
	out.codexNoticeRound = out.codexCompactionThreshold - 1
	if v, err := d.GetSetting(settingCodexNoticeRound); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || n < 1 || n > 10 {
			return out, fmt.Errorf("settings %s: must be 1..10: %q", settingCodexNoticeRound, *v)
		}
		out.codexNoticeRound = n
	}
	if v, err := d.GetSetting(settingMonitoringRefreshSeconds); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || n < 1 || n > 60 {
			return out, fmt.Errorf("settings %s: must be 1..60: %q", settingMonitoringRefreshSeconds, *v)
		}
		out.monitoringRefreshSeconds = n
	}

	out.outsourceMaxParallel = defaultOutsourceMaxParallel
	if v, err := d.GetSetting(settingOutsourceMaxParallel); err != nil {
		return out, err
	} else if v != nil {
		// SAME bounds as the PATCH face — one predicate, one wording
		// (outsourceParallelInRange / outsourceParallelRangeMsg in
		// api_settings.go). This used to apply the generic non-negative-integer
		// check, which belongs to timestamp-shaped keys: it rejected -1, a value
		// the write face, the UI and the docs all call legal, so saving it
		// succeeded and the next start died here.
		n, err := strconv.Atoi(*v)
		if err != nil || !outsourceParallelInRange(n) {
			return out, fmt.Errorf(
				"settings %s: %s: %q",
				settingOutsourceMaxParallel, outsourceParallelRangeMsg, *v)
		}
		out.outsourceMaxParallel = n
	}

	// doc.cap_chars.* — range-checked at load like the other bounded integers,
	// so a hand-edited DB row can never install a cap that the PATCH face would
	// have refused. Each floor is that segment's own default (owner 2026-07-31:
	// a cap only ever goes up), so a stored value below it is corruption, not a
	// downgrade. The legacy single `doc.cap_chars` row was RENAMED to
	// `doc.cap_chars.manual` by migration 00048, and that row was in turn
	// COPIED to `.manual_sop` and `.manual_learnings` and deleted by 00049 —
	// the DB never holds a retired key beside its successors.
	//
	// The max is a parameter rather than maxDocCapChars because T-c9b4 added a
	// bounded integer with its OWN ceiling (chat.budget_chars); baking one
	// ceiling in would have forced a near-copy of this loader for it.
	loadCap := func(key string, min, max int, dst *int, def int) error {
		*dst = def
		v, err := d.GetSetting(key)
		if err != nil || v == nil {
			return err
		}
		n, err := strconv.Atoi(*v)
		if err != nil || n < min || n > max {
			return fmt.Errorf("settings %s: must be %d..%d: %q",
				key, min, max, *v)
		}
		*dst = n
		return nil
	}
	if err := loadCap(settingDocCapCharsDuty, minDutyCapChars, maxDocCapChars,
		&out.docCapCharsDuty, dutyCapCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsInsight, minDocCapChars, maxDocCapChars,
		&out.docCapCharsInsight, contextDocMaxCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsLearning, minDocCapChars, maxDocCapChars,
		&out.docCapCharsLearning, contextDocMaxCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsManualSop, minDocCapChars, maxDocCapChars,
		&out.docCapCharsManualSop, contextDocMaxCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsManualLearnings, minDocCapChars, maxDocCapChars,
		&out.docCapCharsManualLearnings, contextDocMaxCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsSystemInteraction, minSystemInteractionCapChars, maxDocCapChars,
		&out.docCapCharsSystemInteraction, systemInteractionCapCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsBootSequence, minBootSequenceCapChars, maxDocCapChars,
		&out.docCapCharsBootSequence, bootSequenceCapCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsOffboard, minOffboardCapChars, maxDocCapChars,
		&out.docCapCharsOffboard, offboardCapCharsDefault); err != nil {
		return out, err
	}

	// chat.budget_chars (T-c9b4) — range-checked at load for the same reason the
	// caps above are: a hand-edited DB row must not install a value the PATCH
	// face would have refused.
	if err := loadCap(settingChatBudgetChars, minChatBudgetChars, maxChatBudgetChars,
		&out.chatBudgetChars, chatBudgetCharsDefault); err != nil {
		return out, err
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
	if err := getBool(settingDisplayWide, &out.displayWide); err != nil {
		return out, err
	}
	if v, err := d.GetSetting(settingOrgName); err != nil {
		return out, err
	} else if v != nil {
		out.orgName = *v
	}
	if v, err := d.GetSetting(settingOwnerName); err != nil {
		return out, err
	} else if v != nil {
		out.ownerName = *v
	}
	if v, err := d.GetSetting(settingPushContactEmail); err != nil {
		return out, err
	} else if v != nil {
		out.pushContactEmail = *v
	}
	if v, err := d.GetSetting(settingDisplayTheme); err != nil {
		return out, err
	} else if v != nil {
		out.displayTheme = *v
	}
	if v, err := d.GetSetting(settingDisplayLanguage); err != nil {
		return out, err
	} else if v != nil {
		out.displayLanguage = *v
	}
	if v, err := d.GetSetting(settingDisplayCustomThemes); err != nil {
		return out, err
	} else if v != nil && *v != "" {
		if err := json.Unmarshal([]byte(*v), &out.displayCustomThemes); err != nil {
			return out, fmt.Errorf("settings %s: not a valid theme-bundle array: %v",
				settingDisplayCustomThemes, err)
		}
		// Prune unrecognised wording codes on READ too (T-081b). The write path
		// prunes, but a row written BEFORE the whitelist shrank still carries the
		// retired codes, and this load is what GET /api/settings echoes — so
		// without this the dead codes are served back until the owner happens to
		// PATCH themes again. Prune only: a stored row is never re-REJECTED here
		// (a whitelist that shrinks must not brick settings load, and the drop is
		// exactly the write path's own semantics).
		for i := range out.displayCustomThemes {
			if w := out.displayCustomThemes[i].Wording; w != nil {
				dropUnknownWordingCodes(*w, fmt.Sprintf("stored custom_themes[%d]", i))
			}
		}
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
	if err := put(s.HandoverPct, settingCtxHandoverPct, strconv.Itoa(c.HandoverPct)); err != nil {
		return err
	}
	if err := put(s.NoticePct, settingCtxNoticePct, strconv.Itoa(c.NoticePct)); err != nil {
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
	// ctx.warn_pct / ctx.remind_step_pct have NO reader since T-c382 — the
	// advance notice is derived from handover_pct, so a second stored threshold
	// could only ever disagree with the one the owner sets. Rows an old install
	// migrated are left in the table (they are the record of what it used to be
	// tuned to) and are simply never read.
	if err := getInt(settingCtxHandoverPct, &c.HandoverPct); err != nil {
		return err
	}
	// ctx.notice_pct is the FIRST (soft) notice — T-a9d6. ABSENCE is what
	// matters here, not the zero value: an install that predates the pair has
	// no row, and its notice must land where T-c382 derived it (handover minus
	// the lead) rather than at the shipped default, which would silently move
	// the notice of every deployment whose handover the owner had tuned.
	// Reading the row itself rather than checking c.NoticePct is the point —
	// the config default is already non-zero, so the value alone cannot tell
	// "never set" from "set to that number".
	stored, err := d.GetSetting(settingCtxNoticePct)
	if err != nil {
		return err
	}
	if stored == nil {
		if at, ok := claudeNoticePct(c.HandoverPct); ok {
			c.NoticePct = at
		}
	} else {
		n, err := strconv.Atoi(*stored)
		if err != nil || n < 0 {
			return fmt.Errorf("settings %s: not a non-negative integer: %q", settingCtxNoticePct, *stored)
		}
		c.NoticePct = n
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
