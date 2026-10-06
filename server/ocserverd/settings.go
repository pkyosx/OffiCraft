package main

// Read precedence: DB settings → code defaults. oc.toml's retired [auth] /
// [sse_context_high] keys are consumed ONLY by the one-shot migration here
// (loader warns + runtime ignores them — config.go). The snapshot is loaded
// ONCE at serve start — no per-request DB reads, except the login-link rows.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The closed settings key set: keys not listed (including retired ones whose
// rows stay in old DBs) are never read.
const (
	settingPasswordHash = "auth.password_hash"
	// settingJWTSecret is the PRE-RING HS256 key. The live key set is the ring in
	// keyring.go; rotation never updates this row — loadKeyring adopts it as the
	// ring's first key, and every pre-ring token is signed with it.
	settingJWTSecret = "auth.jwt_secret"
	// Owner-scope tokens with iat before settingPasswordChangedAt are refused.
	// The oc.toml migration deliberately does NOT stamp it — migrating is not a
	// password change, and pre-migration tokens must survive.
	settingPasswordChangedAt = "auth.password_changed_at"

	settingLegacyTokenTTL = "auth.token_ttl"
	settingOwnerTokenTTL  = "auth.owner_token_ttl"
	settingAgentTokenTTL  = "auth.agent_token_ttl"
	// settingClaimToken is the one-shot first-run claim token, printed only to the
	// local serve log / installer banner: possession proves host shell access —
	// the gate against a public-tunnel visitor claiming a fresh server.
	settingClaimToken = "auth.claim_token"
	// Written only by the `ocserverd login-link` host subcommands and read by
	// POST /api/auth/login-link on every request, so a running serve follows the
	// CLI without a restart. Deliberately absent from GET/PATCH /api/settings: an
	// owner token must not be able to switch on a password-less way in.
	settingLoginLinkEnabled = "auth.login_link_enabled"
	settingLoginLink        = "auth.login_link"
	// settingMFAOffered is the ship-dark flag "may the second factor be SET UP".
	// 🔴 It gates set-up, NEVER verification: if it switched verification off, a
	// stolen owner token could withdraw the feature and walk past an armed factor.
	settingMFAOffered = "auth.mfa_offered"
	// settingTOTPSecret present ⇒ MFA is armed; every verification path reads
	// THIS key, never the flag above. The pending secret (enroll → activate)
	// exists so a secret is never made active before the owner produced a working
	// code from it — a mistyped QR scan would otherwise lock the owner out.
	// settingTOTPLastStep is the replay floor: without it the same code replays
	// for ~90 seconds.
	settingTOTPSecret        = "auth.totp_secret"
	settingTOTPPendingSecret = "auth.totp_pending_secret"
	settingTOTPLastStep      = "auth.totp_last_step"
	// `ctx.warn_pct` / `ctx.remind_step_pct` are retired and deliberately not
	// listed: the notice is derived from handover_pct. Do not re-add them — two
	// thresholds for one decision is the bug that removed them.
	settingCtxNoticePct             = "ctx.notice_pct"
	settingCtxHandoverPct           = "ctx.handover_pct"
	settingCtxMinBootSecs           = "ctx.min_boot_secs"
	settingCtxStaleGuard            = "ctx.stale_guard"
	settingCodexCompactionThreshold = "codex.compaction_threshold"
	settingCodexNoticeRound         = "codex.notice_round"
	settingMonitoringRefreshSeconds = "monitoring.refresh_seconds"
	settingDiskUsageIntervalSecs    = "monitoring.disk_usage_interval_secs"
	// settingAcceleratedGraceSecs is the grace a CLOCKED wind-down gets.
	// 🔴 Deliberately ONE key for BOTH clocked causes: the clock
	// (recycleGraceFor) and the sentence (offboardNoticeFor) read a single
	// judgement (winddownKindFor); a per-cause knob would let the agent quote one
	// number while the tick collects on another.
	settingAcceleratedGraceSecs = "stop.accelerated_grace_secs"
	// settingWardenCredLifetimeSecs is how long a MACHINE credential lives. It is
	// both the renewal trigger (each warden renews at two thirds of it, read via
	// GET /api/machines/credential-policy) and the expiry (mintWardenToken stamps
	// exp = iat + this). ⚠️ The two must stay the SAME number — a fleet that
	// renews after it has already been refused is silent — so both read
	// wardenCredLifetimeValue(). An exp is fixed at mint, so a change reaches only
	// credentials minted afterwards. It is an owner-typed number, not the token-TTL
	// pick list (owner 2026-09-06).
	settingWardenCredLifetimeSecs = "auth.warden_credential_lifetime_secs"
	// settingOutsourceMaxParallel is the GLOBAL cap on live (assigned + active)
	// outsource workers; member tasks never count (owner ruling ③).
	settingOutsourceMaxParallel = "task.outsource_max_parallel"
	// settingReassignHandoverTimeoutSecs is how long an OUTSOURCE predecessor under
	// the reassign hold may go without a change to the task's updated time before
	// the handover-timeout reaper reclaims it.
	settingReassignHandoverTimeoutSecs = "task.reassign_handover_timeout_secs"
	// settingRuntimeLoginCheckIntervalSecs is how often each warden re-checks a
	// Claude/Codex login that last read as logged in, and
	// settingRuntimeLoginRecheckIntervalSecs one that last read as logged out or
	// unknown; wardens read both from their heartbeat reply.
	settingRuntimeLoginCheckIntervalSecs   = "runtime.login_check_interval_secs"
	settingRuntimeLoginRecheckIntervalSecs = "runtime.login_recheck_interval_secs"
	// 🔴 EVERY doc.cap_chars key carries a suffix: an agent reading get_settings
	// sees key names with no descriptions, so a bare `doc.cap_chars` beside its
	// segments reads as "the default for all of them".
	settingDocCapCharsDuty      = "doc.cap_chars.duty"
	settingDocCapCharsInsight   = "doc.cap_chars.insight"
	settingDocCapCharsManualSop = "doc.cap_chars.manual_sop"

	settingDocCapCharsSystemInteraction = "doc.cap_chars.system_interaction"
	settingDocCapCharsBootSequence      = "doc.cap_chars.boot_sequence"
	settingDocCapCharsOffboard          = "doc.cap_chars.offboard"
	// lore role/manual are FOLD budgets (how much lore a staff boot document / a
	// task manual read carries); title/body bound one entry write.
	settingLoreCapCharsRole   = "lore.cap_chars.role"
	settingLoreCapCharsManual = "lore.cap_chars.manual"
	settingLoreCapCharsTitle  = "lore.cap_chars.title"
	settingLoreCapCharsBody   = "lore.cap_chars.body"
	settingChatBudgetChars    = "chat.budget_chars"

	settingStepNoteCapChars = "task.step_note_cap_chars"
	// settingBackupRetain has TWO readers: this snapshot (GET/PATCH
	// /api/settings) and the backup engine, which reads the row directly
	// (liveBackupRetain in backup.go) because it holds no apiServer. N counts
	// versions, not days, and is PER POOL — see backup.go.
	settingBackupRetain = "backup.retain"
	// receive_beta=true follows GitHub prereleases too (update_check.go).
	settingUpdaterReceiveBeta = "updater.receive_beta"

	settingUpdaterAutoUpdate = "updater.auto_update"
	// org.name reaches every agent via get_global_context; owner.name and
	// display.* never enter an agent read path. "" = never set: the frontend
	// falls back to its localized default / cached value.
	settingOrgName = "org.name"

	settingOwnerName = "owner.name"
	// settingPushContactEmail is the VAPID subject handed to push gateways. ""
	// means delivery is refused outright rather than attempted with a made-up
	// address: Apple answers BadJwtToken for an unreachable domain, which takes
	// push down on every device with no visible error.
	settingPushContactEmail = "push.contact_email"
	// display.*: the server is the cross-device truth; the frontend keeps a
	// localStorage cache because the choice must apply BEFORE login.
	settingDisplayTheme = "display.theme"

	settingDisplayLanguage = "display.language"

	settingDisplayWide = "display.wide"
	// suggested_replies.*: ONE KEY PER BOX (owner ruling) — the three boxes are
	// different conversations, and separate rows keep changing one list a single
	// write instead of an unlocked read-modify-write over a shared blob.
	settingSuggestedRepliesReplyCard   = "suggested_replies.reply_card"
	settingSuggestedRepliesTaskMessage = "suggested_replies.task_message"
	settingSuggestedRepliesLoreMessage = "suggested_replies.lore_message"
	// `display.custom_themes` deliberately has NO constant: the themes live in the
	// custom_theme table (/api/themes), and the old row is kept unread only as the
	// rollback path (deleting it is gated — see the top of
	// migration_00059_custom_theme_table.go). That migration spells the key out
	// itself on purpose: it must describe the schema at its own version.
)

// The built-in theme ids. Twin of RESERVED_THEME_IDS in frontend/src/lib/themeBundleCore.ts.
var displayThemeAllowed = map[string]bool{"office": true, "office-light": true}
var displayLanguageAllowed = map[string]bool{"zh": true, "en": true}

const defaultOutsourceMaxParallel = 3
const defaultCodexCompactionThreshold = 3
const defaultMonitoringRefreshSeconds = 5

// The floor is 10 s: a single-digit grace is indistinguishable from force-stop
// while still printing a countdown the agent is invited to use. Past one hour
// the clock stops being an escalation.
const (
	acceleratedGraceSecsDefault = int(StoppingTimeoutSecs)
	minAcceleratedGraceSecs     = 10
	maxAcceleratedGraceSecs     = 3600
)

const (
	reassignHandoverTimeoutSecsDefault = 1800
	minReassignHandoverTimeoutSecs     = 60
	maxReassignHandoverTimeoutSecs     = 86400
)

// The floor is the warden's heartbeat cadence: the check runs at most once per
// heartbeat, so a shorter interval cannot be honoured. Both login intervals share
// the range.
const (
	runtimeLoginCheckIntervalSecsDefault   = 300
	runtimeLoginRecheckIntervalSecsDefault = 30
	minRuntimeLoginCheckIntervalSecs       = 30
	maxRuntimeLoginCheckIntervalSecs       = 3600
)

// One measurement walks the whole OffiCraft directory for tens of seconds,
// hence the ten-minute floor.
const (
	diskUsageIntervalSecsDefault = 3600
	minDiskUsageIntervalSecs     = 600
	maxDiskUsageIntervalSecs     = 86400
)

// THE DEFAULT IS 30 DAYS (owner rc-f2b96594c621): the production station has
// no row for this setting, so moving the default moves production.
//
// 🔴 Two conditions fail SILENTLY and have NO mechanical guard (spec/lifecycle.md
// §1.6): ① removing a signing key while machines still hold credentials it
// signed refuses them all at once — safe only when `token_key_current` on GET
// /api/machines says yes for every row; ② a machine off the network longer than
// the last third of the lifetime loses its credential for good.
//
// The one-day floor is derived from that retry window at the warden's 15-minute
// poll (selfUpdateInterval, cli/ocwarden). Lowering the setting under the
// fleet's age makes every machine due on its next poll — the per-machine
// stagger does NOT soften that (measured: 40 of 40).
const (
	wardenCredLifetimeSecsDefault = 30 * 86400
	minWardenCredLifetimeSecs     = 86400
	maxWardenCredLifetimeSecs     = int(maxAgentTTLSecs)
)

type authSettings struct {
	secret                          []byte
	passwordHash                    string
	passwordChangedAt               int64
	mfaOffered                      bool
	totpSecret                      string
	totpLastStep                    int64
	ownerTokenTTL                   int64
	agentTokenTTL                   int64
	ctxHigh                         SseContextHighConfig
	codexCompactionThreshold        int // the FINAL round (handover)
	codexNoticeRound                int // the FIRST, soft notice round
	monitoringRefreshSeconds        int
	acceleratedGraceSecs            int
	wardenCredLifetimeSecs          int
	outsourceMaxParallel            int
	reassignHandoverTimeoutSecs     int
	runtimeLoginCheckIntervalSecs   int
	runtimeLoginRecheckIntervalSecs int
	diskUsageIntervalSecs           int
	docCapCharsDuty                 int
	docCapCharsInsight              int
	docCapCharsManualSop            int
	docCapCharsSystemInteraction    int
	docCapCharsBootSequence         int // ONE cap, both runtimes
	docCapCharsOffboard             int
	loreCapCharsRole                int
	loreCapCharsManual              int
	loreCapCharsTitle               int
	loreCapCharsBody                int
	chatBudgetChars                 int
	stepNoteCapChars                int
	backupRetain                    int
	updaterReceiveBeta              bool
	updaterAutoUpdate               bool
	orgName                         string
	ownerName                       string
	pushContactEmail                string
	displayTheme                    string
	displayLanguage                 string
	displayWide                     bool

	suggestedRepliesReplyCard   []string
	suggestedRepliesTaskMessage []string
	suggestedRepliesLoreMessage []string
}

// A suggestion list is a menu read at a glance under a reply box: 20 entries
// already overflow a phone, and 120 runes is one sentence.
const (
	maxSuggestedReplies  = 20
	maxSuggestedReplyLen = 120
)

// canonicalSuggestedReplies REFUSES an over-bound entry instead of truncating
// it: a silently shortened sentence is one the owner never wrote, offered one
// tap away from being sent.
func canonicalSuggestedReplies(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if utf8.RuneCountInString(v) > maxSuggestedReplyLen {
			return nil, fmt.Errorf("must be at most %d characters per entry",
				maxSuggestedReplyLen)
		}
		out = append(out, v)
	}
	if len(out) > maxSuggestedReplies {
		return nil, fmt.Errorf("must be at most %d entries", maxSuggestedReplies)
	}
	return out, nil
}

func encodeSuggestedReplies(list []string) string {
	if list == nil {
		list = []string{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeSuggestedReplies(raw string) ([]string, error) {
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("must be a JSON array of strings: %q", raw)
	}
	return canonicalSuggestedReplies(list)
}

// loadAuthSettings loads the boot snapshot, running the one-shot oc.toml → DB
// migration for whatever the DB lacks. An existing install's JWT secret is
// imported as the password-DERIVED key, NOT a fresh mint: every already-issued
// token (400-day agent tokens, warden tokens) is signed with it.
//
// Every bounded value is range-checked here with the PATCH face's own
// predicate/bounds: a hand-edited row the PATCH face would refuse must stop the
// boot, and a value that survives a save must never refuse the next start.
func loadAuthSettings(d *DAL, cfg Config, logf func(string)) (authSettings, error) {
	out := authSettings{
		ownerTokenTTL:                   defaultOwnerTokenTTL,
		agentTokenTTL:                   defaultAgentTokenTTL,
		ctxHigh:                         cfg.SseContextHigh,
		codexCompactionThreshold:        defaultCodexCompactionThreshold,
		monitoringRefreshSeconds:        defaultMonitoringRefreshSeconds,
		acceleratedGraceSecs:            acceleratedGraceSecsDefault,
		wardenCredLifetimeSecs:          wardenCredLifetimeSecsDefault,
		reassignHandoverTimeoutSecs:     reassignHandoverTimeoutSecsDefault,
		runtimeLoginCheckIntervalSecs:   runtimeLoginCheckIntervalSecsDefault,
		runtimeLoginRecheckIntervalSecs: runtimeLoginRecheckIntervalSecsDefault,
		diskUsageIntervalSecs:           diskUsageIntervalSecsDefault,
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

	offered, err := d.GetSetting(settingMFAOffered)
	if err != nil {
		return out, err
	}
	out.mfaOffered = offered != nil && *offered == "true"

	// An undecodable TOTP secret is a hard boot error: ignoring it would boot with
	// MFA quietly off. The pending secret is not in the snapshot — activate reads
	// it straight from the DB.
	totpSecret, err := d.GetSetting(settingTOTPSecret)
	if err != nil {
		return out, err
	}
	if totpSecret != nil && *totpSecret != "" {
		if _, err := decodeTOTPSecret(*totpSecret); err != nil {
			return out, fmt.Errorf("settings %s: %w", settingTOTPSecret, err)
		}
		out.totpSecret = *totpSecret
	}

	lastStep, err := d.GetSetting(settingTOTPLastStep)
	if err != nil {
		return out, err
	}
	if lastStep != nil {
		n, err := strconv.ParseInt(*lastStep, 10, 64)
		if err != nil || n < 0 {
			return out, fmt.Errorf("settings %s: not a non-negative integer: %q", settingTOTPLastStep, *lastStep)
		}
		out.totpLastStep = n
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
	if err := applyCtxOverrides(d, &out.ctxHigh); err != nil {
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
	if v, err := d.GetSetting(settingAcceleratedGraceSecs); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || !acceleratedGraceInRange(n) {
			return out, fmt.Errorf("settings %s: %s: %q",
				settingAcceleratedGraceSecs, acceleratedGraceRangeMsg, *v)
		}
		out.acceleratedGraceSecs = n
	}
	if v, err := d.GetSetting(settingReassignHandoverTimeoutSecs); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || !reassignHandoverTimeoutInRange(n) {
			return out, fmt.Errorf("settings %s: %s: %q",
				settingReassignHandoverTimeoutSecs, reassignHandoverTimeoutRangeMsg, *v)
		}
		out.reassignHandoverTimeoutSecs = n
	}
	if v, err := d.GetSetting(settingRuntimeLoginCheckIntervalSecs); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || !runtimeLoginCheckIntervalInRange(n) {
			return out, fmt.Errorf("settings %s: %s: %q",
				settingRuntimeLoginCheckIntervalSecs, runtimeLoginCheckIntervalRangeMsg, *v)
		}
		out.runtimeLoginCheckIntervalSecs = n
	}
	if v, err := d.GetSetting(settingRuntimeLoginRecheckIntervalSecs); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || !runtimeLoginCheckIntervalInRange(n) {
			return out, fmt.Errorf("settings %s: %s: %q",
				settingRuntimeLoginRecheckIntervalSecs, runtimeLoginCheckIntervalRangeMsg, *v)
		}
		out.runtimeLoginRecheckIntervalSecs = n
	}
	if v, err := d.GetSetting(settingDiskUsageIntervalSecs); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || !diskUsageIntervalInRange(n) {
			return out, fmt.Errorf("settings %s: %s: %q",
				settingDiskUsageIntervalSecs, diskUsageIntervalRangeMsg, *v)
		}
		out.diskUsageIntervalSecs = n
	}

	if v, err := d.GetSetting(settingWardenCredLifetimeSecs); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || !wardenCredLifetimeInRange(n) {
			return out, fmt.Errorf("settings %s: %s: %q",
				settingWardenCredLifetimeSecs, wardenCredLifetimeRangeMsg, *v)
		}
		out.wardenCredLifetimeSecs = n
	}

	out.outsourceMaxParallel = defaultOutsourceMaxParallel
	if v, err := d.GetSetting(settingOutsourceMaxParallel); err != nil {
		return out, err
	} else if v != nil {
		n, err := strconv.Atoi(*v)
		if err != nil || !outsourceParallelInRange(n) {
			return out, fmt.Errorf(
				"settings %s: %s: %q",
				settingOutsourceMaxParallel, outsourceParallelRangeMsg, *v)
		}
		out.outsourceMaxParallel = n
	}

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
	if err := loadCap(settingDocCapCharsDuty, minDocCapChars, maxDocCapChars,
		&out.docCapCharsDuty, dutyCapCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsInsight, minDocCapChars, maxDocCapChars,
		&out.docCapCharsInsight, contextDocMaxCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsManualSop, minDocCapChars, maxDocCapChars,
		&out.docCapCharsManualSop, contextDocMaxCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsSystemInteraction, minDocCapChars, maxDocCapChars,
		&out.docCapCharsSystemInteraction, systemInteractionCapCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsBootSequence, minDocCapChars, maxDocCapChars,
		&out.docCapCharsBootSequence, bootSequenceCapCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingDocCapCharsOffboard, minDocCapChars, maxDocCapChars,
		&out.docCapCharsOffboard, offboardCapCharsDefault); err != nil {
		return out, err
	}

	if err := loadCap(settingLoreCapCharsRole, minLoreFoldCapChars, maxLoreFoldCapChars,
		&out.loreCapCharsRole, loreRoleCapCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingLoreCapCharsManual, minLoreFoldCapChars, maxLoreFoldCapChars,
		&out.loreCapCharsManual, loreManualCapCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingLoreCapCharsTitle, minLoreEntryCapChars, maxLoreEntryCapChars,
		&out.loreCapCharsTitle, loreTitleCapCharsDefault); err != nil {
		return out, err
	}
	if err := loadCap(settingLoreCapCharsBody, minLoreEntryCapChars, maxLoreEntryCapChars,
		&out.loreCapCharsBody, loreBodyCapCharsDefault); err != nil {
		return out, err
	}

	if err := loadCap(settingChatBudgetChars, minChatBudgetChars, maxChatBudgetChars,
		&out.chatBudgetChars, chatBudgetCharsDefault); err != nil {
		return out, err
	}

	if err := loadCap(settingStepNoteCapChars, minStepNoteCapChars, maxStepNoteCapChars,
		&out.stepNoteCapChars, stepNoteCapCharsDefault); err != nil {
		return out, err
	}

	if err := loadCap(settingBackupRetain, minBackupRetain, maxBackupRetain,
		&out.backupRetain, backupRetainDefault); err != nil {
		return out, err
	}

	loadSuggestedReplies := func(key string, dst *[]string) error {
		*dst = []string{}
		v, err := d.GetSetting(key)
		if err != nil || v == nil {
			return err
		}
		list, derr := decodeSuggestedReplies(*v)
		if derr != nil {
			return fmt.Errorf("settings %s: %v", key, derr)
		}
		*dst = list
		return nil
	}
	if err := loadSuggestedReplies(settingSuggestedRepliesReplyCard,
		&out.suggestedRepliesReplyCard); err != nil {
		return out, err
	}
	if err := loadSuggestedReplies(settingSuggestedRepliesTaskMessage,
		&out.suggestedRepliesTaskMessage); err != nil {
		return out, err
	}
	if err := loadSuggestedReplies(settingSuggestedRepliesLoreMessage,
		&out.suggestedRepliesLoreMessage); err != nil {
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
	return out, nil
}

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

const envNewPassword = "OC_NEW_PASSWORD"

func openAuthDAL(name string, env func(string) string, out io.Writer) (d *DAL, auth authSettings, done func(), rc int) {
	done = func() {}
	cfg, dsn, rc := announceResolution(name, env, out)
	if rc != 0 {
		return nil, auth, done, rc
	}
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

// cmdSetPassword is the local seam the test harnesses (conformance/e2e) use to
// seed a KNOWN credential, and the operator's shell-access rescue when the
// password is lost.
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

// cmdMFADisable is THE lost-authenticator recovery path: POST
// /api/auth/mfa/disable demands a code from the very device that was lost. Not a
// backdoor — it substitutes host shell access for the factor, as the claim token
// does, and whoever can run it can already run set-password or replace the binary.
func cmdMFADisable(env func(string) string, out io.Writer) int {
	d, _, done, rc := openAuthDAL("mfa-disable", env, out)
	defer done()
	if rc != 0 {
		return rc
	}
	for _, key := range []string{settingTOTPSecret, settingTOTPPendingSecret, settingTOTPLastStep} {
		if err := d.DeleteSetting(key); err != nil {
			fmt.Fprintf(out, "[ocserverd] FATAL: clear %s: %v\n", key, err)
			return 1
		}
	}
	fmt.Fprintln(out, "[ocserverd] mfa-disable: the owner's second factor is cleared (takes effect at the next serve start)")
	return 0
}

// cmdClaimToken prints the first-run claim code for the installer banner (a
// local DB read; the code never rides an unauthenticated HTTP endpoint). The
// token is the LAST output line. Exit codes: 0 = printed, 3 = password already
// set (no token), 1 = fatal.
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

const loginLinkTTLSecs int64 = 600

type loginLinkRecord struct {
	Hash      string `json:"hash"`
	ExpiresAt int64  `json:"expires_at"`
}

func loginLinkHash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// mintLoginLink replaces any earlier code: only the latest link is valid. Only
// the hash is stored, so a copy of the database cannot be replayed as a link.
func mintLoginLink(d *DAL, now int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	rec, err := json.Marshal(loginLinkRecord{Hash: loginLinkHash(code), ExpiresAt: now + loginLinkTTLSecs})
	if err != nil {
		return "", err
	}
	if err := d.PutSetting(settingLoginLink, string(rec)); err != nil {
		return "", err
	}
	return code, nil
}

// consumeLoginLinkOn deletes the stored code in the caller's transaction when
// code redeems it, so two concurrent redemptions cannot both succeed.
func consumeLoginLinkOn(tx *writeTx, code string, now int64) (bool, error) {
	enabled, err := getSettingOn(tx, settingLoginLinkEnabled)
	if err != nil || enabled == nil || *enabled != "true" {
		return false, err
	}
	stored, err := getSettingOn(tx, settingLoginLink)
	if err != nil || stored == nil {
		return false, err
	}
	var rec loginLinkRecord
	if err := json.Unmarshal([]byte(*stored), &rec); err != nil {
		return false, fmt.Errorf("settings %s: %w", settingLoginLink, err)
	}
	if now >= rec.ExpiresAt ||
		subtle.ConstantTimeCompare([]byte(loginLinkHash(code)), []byte(rec.Hash)) != 1 {
		return false, nil
	}
	return true, deleteSettingOn(tx, settingLoginLink)
}

// cmdLoginLink: `login-link enable|disable` switches the feature, a bare
// `login-link [--base-url URL]` mints a link and prints it as the LAST output
// line. Exit codes: 0 = done, 2 = bad arguments, 3 = feature off (nothing
// minted), 1 = fatal.
func cmdLoginLink(args []string, env func(string) string, out io.Writer) int {
	action, baseURL := "mint", ""
	switch {
	case len(args) == 1 && (args[0] == "enable" || args[0] == "disable"):
		action = args[0]
	case len(args) == 0:
	case len(args) == 2 && args[0] == "--base-url" && args[1] != "":
		baseURL = args[1]
	case len(args) == 1 && strings.HasPrefix(args[0], "--base-url=") && len(args[0]) > len("--base-url="):
		baseURL = strings.TrimPrefix(args[0], "--base-url=")
	default:
		fmt.Fprintln(out, "[ocserverd] usage: ocserverd login-link [enable | disable | --base-url <url>]")
		return 2
	}
	d, _, done, rc := openAuthDAL("login-link", env, out)
	defer done()
	if rc != 0 {
		return rc
	}
	switch action {
	case "enable":
		if err := d.PutSetting(settingLoginLinkEnabled, "true"); err != nil {
			fmt.Fprintf(out, "[ocserverd] FATAL: enable login links: %v\n", err)
			return 1
		}
		fmt.Fprintln(out, "[ocserverd] login-link: enabled — `ocserverd login-link` now mints one-time owner login links")
		return 0
	case "disable":
		for _, key := range []string{settingLoginLinkEnabled, settingLoginLink} {
			if err := d.DeleteSetting(key); err != nil {
				fmt.Fprintf(out, "[ocserverd] FATAL: clear %s: %v\n", key, err)
				return 1
			}
		}
		fmt.Fprintln(out, "[ocserverd] login-link: disabled — every login link is refused")
		return 0
	}
	enabled, err := d.GetSetting(settingLoginLinkEnabled)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: read %s: %v\n", settingLoginLinkEnabled, err)
		return 1
	}
	if enabled == nil || *enabled != "true" {
		fmt.Fprintln(out, "[ocserverd] login-link: login links are disabled on this station — run `ocserverd login-link enable` first; no link was minted")
		return 3
	}
	code, err := mintLoginLink(d, time.Now().Unix())
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: mint login link: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "[ocserverd] login-link: valid for %d minutes and for one login; minting another invalidates it\n", loginLinkTTLSecs/60)
	fmt.Fprintf(out, "%s/?login=%s\n", strings.TrimRight(baseURL, "/"), code)
	return 0
}

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
	if err := getInt(settingCtxHandoverPct, &c.HandoverPct); err != nil {
		return err
	}
	// ABSENCE of the ctx.notice_pct row is what matters, not its value: an install
	// that predates the pair must get the handover-derived notice, and the config
	// default is non-zero, so c.NoticePct cannot tell "never set" from "set".
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
