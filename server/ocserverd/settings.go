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
	"strings"
	"unicode/utf8"
)

// The closed settings key set. The setting table is schemaless key-value; the
// reader here holds the schema (type, default, who writes). Keys not listed
// are never read.
const (
	// settingPasswordHash is the argon2id PHC hash of the owner password
	// (password.go). Absent = password not yet set (first-run flow, B3).
	settingPasswordHash = "auth.password_hash"
	// settingJWTSecret is the PRE-RING HS256 signing key, base64url of the raw
	// key bytes. Always present after first boot (migrated or minted).
	//
	// Since T-62 the live key set is the ring in keyring.go
	// (auth.jwt_keys / auth.jwt_active_key_id) and THAT is what signs and
	// verifies. This row is not deleted and not updated by a rotation: it is
	// what loadKeyring adopts as the ring's first key on an install that has
	// never rotated, and it is the key every token issued before the ring
	// existed is signed with.
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
	// settingMFAOffered is the ship-dark FEATURE FLAG: does this server offer the
	// second factor at all? Absent/false is the default, so an install that
	// upgrades into this build is completely unaffected until its owner opts in.
	//
	// 🔴 IT GATES SET-UP, NEVER VERIFICATION, and that asymmetry is the whole
	// safety property. While it is false, enroll/activate are refused and the
	// cockpit hides the entry — but a factor that is ALREADY armed keeps being
	// demanded at login, /api/auth/status keeps reporting mfa_required, and
	// disable keeps working. If the flag switched verification off it would BE
	// the bypass: a stolen owner token could withdraw the feature and walk past
	// the factor that exists to stop exactly that. Same reasoning as the
	// both-factors rule on disable.
	settingMFAOffered = "auth.mfa_offered"
	// The owner's TOTP second factor (totp.go). Three keys, because enrolment
	// is a two-step ceremony and replay defence needs a floor:
	//
	//   settingTOTPSecret — the ACTIVE base32 secret. Present ⇒ MFA is on and
	//     /api/login demands a code. This is the single bit that ARMS the second
	//     factor.
	//
	//     🔴 settingMFAOffered above is NOT a second opinion about that, and the
	//     distinction is load-bearing: "may the factor be set up" (a rollout
	//     decision) and "is a factor armed" (a security fact) are different
	//     questions, and only the secret answers the second one. There is still
	//     deliberately no flag that could disagree with the presence of a usable
	//     secret about whether login demands a code — every verification path
	//     reads THIS key and never the flag.
	//   settingTOTPPendingSecret — a minted-but-unproven secret, written by
	//     enroll and consumed by activate. It exists so a secret can NEVER be
	//     promoted to active until the owner has produced a working code from
	//     it: enrolling into the active slot directly would let a mistyped QR
	//     scan lock the owner out of their own server on the next login.
	//   settingTOTPLastStep — the highest RFC 6238 time step already spent.
	//     A code stays valid across the acceptance window, so without this
	//     floor the same six digits replay for ~90 seconds.
	settingTOTPSecret        = "auth.totp_secret"
	settingTOTPPendingSecret = "auth.totp_pending_secret"
	settingTOTPLastStep      = "auth.totp_last_step"
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
	// settingAcceleratedGraceSecs (T-ed79, owner 2026-08-21 「120 秒這個設定可以
	// 調整，統一在第二門檻跟加速停止使用」) is the grace a CLOCKED wind-down gets,
	// in seconds. Its shipped default is StoppingTimeoutSecs, which is where the
	// 120 came from.
	//
	// 🔴 It is deliberately ONE key for BOTH clocked causes rather than one per
	// cause. The clock (recycleGraceFor) and the sentence (offboardKindOf →
	// offboardNoticeFor) already read a single judgement (winddownKindFor); a
	// second knob would re-open the same split from the other end — the agent
	// quoted one number while the tick collected on another. This key says HOW
	// LONG; it never says WHO is on a clock.
	settingAcceleratedGraceSecs = "stop.accelerated_grace_secs"
	// settingWardenCredLifetimeSecs (T-fc53, owner 2026-09-06 「憑證改回有到期日
	// (30 天)，同時換發改看『發下來多久』」) is how long a MACHINE credential is
	// meant to live, in seconds.
	//
	// 🔴 IT IS NOT AN EXPIRY, AND THE DISTINCTION IS THE WHOLE FIRST PACKAGE.
	// mintWardenToken still mints without an exp claim, nothing at the auth gate
	// reads this, and no credential stops working because of it. What it governs
	// is RENEWAL: each warden reads it (GET /api/machines/credential-policy) and
	// replaces its own credential once that credential is two thirds of this old,
	// measured from the `iat` claim it already carries.
	//
	// WHY THAT ORDER — a setting that only drives renewal, before the expiry it is
	// named after. The renewal path had never once been observed to run: the old
	// trigger asked "how much of the lifetime is left", which needs an exp, so it
	// answered "not due" on every machine in the fleet forever. Putting expiries
	// back first would have started a clock on every host against a path nobody
	// had watched work. With no expiry in play the worst this setting can do is
	// make machines renew too often or not at all, and neither takes a host off
	// the network.
	//
	// It is deliberately an owner-typed NUMBER rather than a pick from the
	// 12h/1d/7d/30d list the two token TTLs use (owner 2026-09-06): the reason for
	// changing it is to watch a renewal happen without waiting a month, and 3 days
	// is not on that list.
	settingWardenCredLifetimeSecs = "auth.warden_credential_lifetime_secs"
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
	// settingStepNoteCapChars (T-119) is the ceiling on ONE task step's working
	// note — what both note write faces refuse a longer note against, and what
	// get_task / get_task_step report as note_cap_chars. It was the hard-coded
	// chatBodyMaxChars until the owner made it adjustable (2026-09-06).
	//
	// It is a `task.` key and NOT a `doc.cap_chars.*` one, for the same reason
	// the chat budget above is not: those floors equal their own defaults so a
	// document cap can only ever be raised, while this one may be LOWERED. It
	// can, because it is enforced only on WRITE — a note already stored above a
	// newly lowered cap stays readable in full and simply becomes uneditable
	// until it is shortened, which is a state the owner asked to be able to
	// create deliberately.
	settingStepNoteCapChars = "task.step_note_cap_chars"
	// settingBackupRetain (T-8, owner 2026-08-27: 「我覺得應該只保留最新的 N 版
	// 備份，N 可以設定，剩餘的應該直接移除」) is N — how many database backup
	// files survive rotation. Its default, floor and ceiling all live in
	// backup.go (backupRetainDefault / minBackupRetain / maxBackupRetain), which
	// is also where the two things a reader will otherwise get wrong are written
	// down:
	//
	//   - N counts VERSIONS, not days. The calendar depth it buys depends on how
	//     many backups that stretch of days happened to produce.
	//   - N is PER POOL, not per directory. Two pools ⇒ up to 2 × N files.
	//
	// 🔴 This row has TWO readers and that is deliberate: the cockpit face reads
	// it through the boot snapshot below (so GET /api/settings can show it and
	// PATCH can move it), and the backup engine reads the row DIRECTLY at
	// snapshot time (liveBackupRetain in backup.go) because it holds no
	// apiServer and three of its four triggers run without one. Both are bounded
	// by the same three constants, so they cannot disagree about what is legal.
	settingBackupRetain = "backup.retain"
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
	// settingLoreEnabled (T-33, bool, default FALSE) is the station-wide LORE
	// feature switch — 一個站一個開關 (owner: 「我們可以在自己的 site 上打開
	// 這個功能優先體驗一陣子」), and the default is OFF because the owner said so
	// in as many words (「預設是關閉起來的」).
	//
	// 🔴 OFF MEANS THE FEATURE DOES NOT EXIST FOR AN AGENT, NOT THAT ITS DATA
	// MOVES. Every /api/lore/* route refuses, the boot context folds in no
	// 對象目錄, and the cockpit shows no 傳承 tab. Nothing is copied into
	// 長期筆記 / 教訓 and nothing is deleted: an agent that cannot write lore
	// simply goes on calling the learning / lesson tools it already had. That is
	// the WHOLE of 「fallback 到原本的 learning / lesson」 — it is the agent
	// walking its old path, never the station carrying a memory across stores.
	//
	// ⚠️ WHY A TRANSFER WOULD BE WORSE THAN NOTHING, stated so nobody adds one
	// back: the target document has its own character cap, so the move can FAIL,
	// and a failure mid-move leaves a memory that is in neither place while the
	// write that triggered it has already returned. The problem set only exists
	// if the transfer does.
	//
	// It is a plain bool with no "never set" state (absent row = false = OFF),
	// stored as strconv.FormatBool text like the updater / display.wide toggles.
	settingLoreEnabled = "lore.enabled"

	// settingSuggestedRepliesReplyCard / settingSuggestedRepliesTaskMessage
	// (T-122) hold the owner's one-click 建議回覆 — the sentences the cockpit
	// offers under a reply box so an answer is one tap instead of one typing
	// session. Stored as a JSON ARRAY OF STRINGS in the single `value` column
	// (the setting table is key/value — migrations/00002_settings.sql), which is
	// why there is no migration: a new key needs no DDL.
	//
	// TWO KEYS, NOT ONE, and no nested object: answering a 請示卡 and writing to
	// a task in progress are different conversations, so one list's sentences
	// are wrong in the other's box (owner ruling). Two rows also keep "change
	// only one of them" a single PATCH-time write instead of an unlocked
	// read-modify-write over one shared blob.
	//
	// ABSENT ROW = the empty list, and so is a stored `[]`: "the owner
	// configured none" is a legal, ordinary state that draws no chips. The reply
	// box must keep working with none — the suggestions are a convenience laid
	// over it, never a part of it.
	settingSuggestedRepliesReplyCard   = "suggested_replies.reply_card"
	settingSuggestedRepliesTaskMessage = "suggested_replies.task_message"
	// [T-16a1 P2 / T-83ef] `display.custom_themes` — the row that used to hold
	// every saved theme as one JSON array — HAS NO CONSTANT HERE ANY MORE, and
	// that is deliberate rather than an oversight:
	//
	//   - The WIRE is retired. Settings neither serves nor accepts
	//     custom_themes; the themes live in the custom_theme table behind
	//     /api/themes, and nothing in the server writes this row.
	//   - The ROW is NOT deleted. It is kept as the rollback path the migration
	//     was allowed to run against — an install that goes back to a pre-split
	//     binary finds its themes exactly as it left them, byte for byte.
	//     Deleting it is a separate, gated change; the precondition is at the
	//     top of migration_00059_custom_theme_table.go.
	//   - The only code that still names the key is that migration, and it
	//     spells the string out ITSELF (`legacyCustomThemesKey`) on purpose: a
	//     migration has to keep describing the schema as it was AT ITS OWN
	//     VERSION, so it must not follow a constant that later versions are
	//     free to rename or retire.
	//
	// 🔴 This block previously kept a `settingDisplayCustomThemes` constant and
	// justified it by saying migration 00059 and its tests still addressed the
	// row. That was FALSE — the migration deliberately does not reference it,
	// and deleting the constant left `go build ./...` green, which is how it was
	// caught. It is recorded here because the danger was not the unused constant
	// but the REASON attached to it: a comment that hands the next reader a
	// wrong argument for keeping something is worse than no comment, and nothing
	// mechanical checks a rationale.
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

// The stop.accelerated_grace_secs bounds. The default is StoppingTimeoutSecs —
// the constant the 120 has always come from — so an install that never writes
// the key behaves exactly as it did before the knob existed.
//
// The floor is 10 s rather than 1: the value is a hand-off window an agent is
// told about and then works inside, and a single-digit one is indistinguishable
// from force-stop while still printing a countdown clause that invites the agent
// to use it. The ceiling is one hour — past that the clock stops being an
// escalation and becomes the "no clock at all" the soft causes already have.
const (
	acceleratedGraceSecsDefault = int(StoppingTimeoutSecs)
	minAcceleratedGraceSecs     = 10
	maxAcceleratedGraceSecs     = 3600
)

// The auth.warden_credential_lifetime_secs bounds (T-fc53).
//
// THE DEFAULT IS 30 DAYS because that is what the owner ruled the credential
// lifetime should be, so an install that never writes the key already behaves the
// way the second package will make it behave literally.
//
// 🔴 THE FLOOR IS ONE DAY, AND IT IS NOT AN ARBITRARY ROUND NUMBER — it is derived
// from the retry window. A warden renews at two thirds of the lifetime, so the
// LAST THIRD is the window in which a machine that was switched off, asleep or
// off the network can still get a replacement; its poll is 15 minutes
// (selfUpdateInterval, cli/ocwarden). One day therefore buys an eight-hour window
// ≈ 32 attempts, which survives a working day of downtime. Halve the lifetime
// again and the window is four hours; take it to an hour and the window is twenty
// minutes, i.e. one or two polls — at which point a single missed poll is the
// difference between a machine that renews and one that does not. The floor is
// where that stops being true, not where the number stops looking tidy.
//
// It is also comfortably below the 3 days the owner said he would set to watch a
// renewal happen (owner 2026-09-06), which is the constraint that decided a day
// rather than a week.
//
// ⚠️ WHAT THE FLOOR DOES NOT PROTECT AGAINST, said out loud: nothing here stops
// the owner lowering the setting far enough that credentials already in the field
// are instantly past two thirds of it — that is the fleet-wide simultaneous
// renewal he was told about and accepted.
//
// 🔴 AND IT IS NOT SOFTENED BY THE WARDEN'S PER-MACHINE STAGGER, which an earlier
// version of this comment claimed. That stagger is at most an hour and is ADDED TO
// the threshold, so once the threshold sits under the whole fleet's age every
// machine is due on its very next poll regardless (measured: 40 of 40). What makes
// the event survivable is that every failure on the renewal path keeps the old
// credential and a failed exec does not exit — not this range, and not the stagger.
//
// THE CEILING IS maxAgentTTLSecs (400 days), the same ceiling every other
// long-lived credential on this station already lives under. Naming the same
// number twice was rejected: this one is derived from it.
const (
	wardenCredLifetimeSecsDefault = 30 * 86400
	minWardenCredLifetimeSecs     = 86400
	maxWardenCredLifetimeSecs     = int(maxAgentTTLSecs)
)

// authSettings is the boot-time snapshot cmdServe stamps onto the apiServer.
type authSettings struct {
	secret                       []byte
	passwordHash                 string // "" = not set in DB (first-run: set-password flow)
	passwordChangedAt            int64  // epoch secs; owner tokens with iat before it are refused
	mfaOffered                   bool   // auth.mfa_offered — may the factor be SET UP? never gates verification
	totpSecret                   string // "" = MFA off; non-empty ⇒ /api/login demands a TOTP code
	totpLastStep                 int64  // highest TOTP step already spent (replay floor)
	ownerTokenTTL                int64
	agentTokenTTL                int64
	ctxhigh                      SseContextHighConfig
	codexCompactionThreshold     int // codex.compaction_threshold — the FINAL round (handover)
	codexNoticeRound             int // codex.notice_round — the FIRST, soft notice round (T-a9d6)
	monitoringRefreshSeconds     int
	acceleratedGraceSecs         int    // stop.accelerated_grace_secs (default acceleratedGraceSecsDefault)
	wardenCredLifetimeSecs       int    // auth.warden_credential_lifetime_secs (default wardenCredLifetimeSecsDefault)
	outsourceMaxParallel         int    // task.outsource_max_parallel (default 3)
	docCapCharsDuty              int    // doc.cap_chars.duty (default dutyCapCharsDefault)
	docCapCharsInsight           int    // doc.cap_chars.insight (default contextDocMaxCharsDefault)
	docCapCharsLearning          int    // doc.cap_chars.learning (default contextDocMaxCharsDefault)
	docCapCharsManualSop         int    // doc.cap_chars.manual_sop (default contextDocMaxCharsDefault)
	docCapCharsManualLearnings   int    // doc.cap_chars.manual_learnings (default contextDocMaxCharsDefault)
	docCapCharsSystemInteraction int    // doc.cap_chars.system_interaction (default systemInteractionCapCharsDefault)
	docCapCharsBootSequence      int    // doc.cap_chars.boot_sequence (default bootSequenceCapCharsDefault; ONE cap, both runtimes)
	docCapCharsOffboard          int    // doc.cap_chars.offboard (default offboardCapCharsDefault)
	chatBudgetChars              int    // chat.budget_chars (default chatBudgetCharsDefault)
	stepNoteCapChars             int    // task.step_note_cap_chars (default stepNoteCapCharsDefault)
	backupRetain                 int    // backup.retain (default backupRetainDefault; N is PER POOL, and counts versions not days)
	updaterReceiveBeta           bool   // updater.receive_beta (default false = official releases only)
	updaterAutoUpdate            bool   // updater.auto_update (default false = manual upgrades only)
	orgName                      string // org.name ("" = never set → localized default in the topbar)
	ownerName                    string // owner.name ("" = never set → localized default in the profile pill)
	pushContactEmail             string // push.contact_email ("" = never set → Web Push delivery is refused)
	displayTheme                 string // display.theme ("" = never set → frontend cache/default)
	displayLanguage              string // display.language ("" = never set → frontend cache/default)
	displayWide                  bool   // display.wide (default false = the narrow centred column)
	loreEnabled                  bool   // lore.enabled (T-33; default false = the whole lore feature is OFF)

	// suggested_replies.* (T-122) — the two one-click 建議回覆 lists, each stored
	// as a JSON array of strings. nil/empty = the owner configured none, which
	// draws no chips and is an ordinary state, not a failure.
	suggestedRepliesReplyCard   []string // suggested_replies.reply_card
	suggestedRepliesTaskMessage []string // suggested_replies.task_message
}

// maxSuggestedReplies / maxSuggestedReplyLen bound each 建議回覆 list (T-122).
// A list is a menu the owner reads at a glance under a reply box, not a
// document: twenty sentences is already more than fits on a phone, and 120
// runes is one sentence rather than a paragraph. Counted in RUNES so a CJK
// sentence gets the full budget, and measured AFTER trimming so trailing
// whitespace can never be what pushes an entry over.
const (
	maxSuggestedReplies  = 20
	maxSuggestedReplyLen = 120
)

// canonicalSuggestedReplies trims every entry, drops the blank ones, and
// REFUSES anything over either bound instead of truncating it.
//
// 🔴 Refusing rather than truncating is the whole point: a silently shortened
// sentence is a sentence the owner never wrote, and it would be offered to him
// as one tap away from being sent. The caller turns the error into a 422 (PATCH
// face, prefixed with the wire field name) or a boot failure (loader, prefixed
// with `settings <key>`), which is the same invariant every bounded setting on
// this endpoint holds: a value the PATCH face rejects must not be a value the
// next boot accepts.
//
// The EMPTY list is a legal result, including from an explicitly empty input —
// "offer no suggestions there" is an ordinary configuration, not a failure.
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

// encodeSuggestedReplies renders a canonical list into the single TEXT `value`
// column the setting table gives every key. A nil list stores "[]", never
// "null": the two mean the same thing here and storing one shape keeps the
// loader's parse total.
func encodeSuggestedReplies(list []string) string {
	if list == nil {
		list = []string{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		// []string cannot fail to marshal; "[]" keeps the row parseable if it
		// somehow ever did.
		return "[]"
	}
	return string(b)
}

// decodeSuggestedReplies parses a stored row back, applying the SAME bounds the
// PATCH face applies. A row that fails either is corruption (nothing but a hand
// edit can produce one) and is reported, never repaired.
func decodeSuggestedReplies(raw string) ([]string, error) {
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("must be a JSON array of strings: %q", raw)
	}
	return canonicalSuggestedReplies(list)
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
		acceleratedGraceSecs:     acceleratedGraceSecsDefault,
		wardenCredLifetimeSecs:   wardenCredLifetimeSecsDefault,
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

	// The ship-dark feature flag. Anything other than a literal "true" is false,
	// including a missing row — an install that has never heard of this key must
	// come up with the feature dark.
	offered, err := d.GetSetting(settingMFAOffered)
	if err != nil {
		return out, err
	}
	out.mfaOffered = offered != nil && *offered == "true"

	// The active TOTP secret. A stored value that does not DECODE is a hard boot
	// error, not a silently-ignored one: the alternative is booting with MFA
	// quietly off because a row got mangled, which is the failure mode an owner
	// would never notice until it mattered. The pending secret is deliberately
	// NOT loaded into the snapshot — it is read straight from the DB by activate
	// (a once-per-enrolment path with no hot reader).
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
	// stop.accelerated_grace_secs — range-checked at load for the same reason
	// every other bounded integer here is: a hand-edited DB row must not install
	// a grace the PATCH face would have refused. SAME bounds as that face
	// (acceleratedGraceInRange), so a value that survives a save can never be the
	// value that refuses to boot on the next start.
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

	// auth.warden_credential_lifetime_secs — range-checked at load against the
	// SAME predicate the PATCH face uses (wardenCredLifetimeInRange), so a value
	// that survives a save can never be the value that refuses to boot on the next
	// start. A hand-edited row must not install a lifetime the write face would
	// have refused: this one is read by every machine in the fleet, and a two-hour
	// lifetime accepted here would have the whole fleet renewing on every poll
	// with nothing on the wire to say why.
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

	// task.step_note_cap_chars (T-119) — range-checked at load for the same
	// reason: a hand-edited DB row must not install a value the PATCH face would
	// have refused.
	if err := loadCap(settingStepNoteCapChars, minStepNoteCapChars, maxStepNoteCapChars,
		&out.stepNoteCapChars, stepNoteCapCharsDefault); err != nil {
		return out, err
	}

	// backup.retain (T-8) — range-checked at load for the same reason as the
	// caps above, and here it matters more than anywhere else on this list: this
	// is the only setting whose value decides how many files get DELETED. A
	// hand-edited row the PATCH face would have refused must stop the server,
	// not quietly become rotation's instruction.
	if err := loadCap(settingBackupRetain, minBackupRetain, maxBackupRetain,
		&out.backupRetain, backupRetainDefault); err != nil {
		return out, err
	}

	// suggested_replies.* (T-122) — the string-array counterpart of loadCap, and
	// it exists for exactly the reason loadCap does: without it a hand-edited row
	// holding 200 sentences, or one 5000 runes long, would be a value the PATCH
	// face refuses that the next boot nevertheless installs. An absent row is the
	// empty list (the shipped default); an unparseable or over-cap row stops the
	// server rather than quietly becoming what the cockpit offers.
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
	if err := getBool(settingLoreEnabled, &out.loreEnabled); err != nil {
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
	// display.custom_themes is NOT loaded here any more (T-83ef). The themes moved
	// to their own table and their own endpoints, so holding a boot-time copy of
	// them in the settings snapshot would be a second, never-refreshed opinion
	// about a set that /api/themes writes — while the row this key names is kept
	// only as a rollback path and has no writer left. The READ-PATH WORDING PRUNE
	// that used to sit here moved with them, to decodeStoredThemeBundle
	// (api_themes.go); it had to, or a theme carrying a retired message-key code
	// would start being served back verbatim, silently.
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

// cmdMFADisable (ocserverd mfa-disable) clears the owner's TOTP second factor
// from DB settings. It is THE lost-authenticator recovery path, and the only
// one — POST /api/auth/mfa/disable deliberately cannot serve that purpose,
// because it demands a live code from the very device that was lost.
//
// 🔴 WHY THIS IS NOT A BACKDOOR. It substitutes proof of HOST SHELL ACCESS for
// proof of the factor, which is the same trust substitution the first-run claim
// token already makes, and it grants nothing new: whoever can run this can also
// run `set-password`, read the SQLite file, or replace the binary. A second
// factor was never a defence against someone standing on the host — it defends
// the network face, where the password alone used to be enough.
//
// It is deliberately IDEMPOTENT and silent about whether a factor was armed: an
// operator running this has already lost access and does not need a puzzle, and
// "was MFA on?" is not a secret worth a distinct exit code here.
//
// Exit codes: 0 = cleared (or nothing was armed), 1 = fatal.
func cmdMFADisable(env func(string) string, out io.Writer) int {
	d, _, done, rc := openAuthDAL("mfa-disable", env, out)
	defer done()
	if rc != 0 {
		return rc
	}
	// Every key the ceremony can leave behind, so a re-enrolment starts clean
	// rather than inheriting a floor or a stale pending secret.
	for _, key := range []string{settingTOTPSecret, settingTOTPPendingSecret, settingTOTPLastStep} {
		if err := d.DeleteSetting(key); err != nil {
			fmt.Fprintf(out, "[ocserverd] FATAL: clear %s: %v\n", key, err)
			return 1
		}
	}
	fmt.Fprintln(out, "[ocserverd] mfa-disable: the owner's second factor is cleared (takes effect at the next serve start)")
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
