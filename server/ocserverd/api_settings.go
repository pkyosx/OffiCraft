package main

// api_settings.go — the B3 owner-cockpit settings surface: the PUBLIC
// first-run probe + claim-token set-password, the owner-gated change-password,
// and the owner-adjustable settings (GET/PATCH /api/settings). Every write
// goes DB-first, then updates the live in-memory snapshot under settingsMu
// (api_stub.go) so a change is durable AND immediate — no restart, no
// per-request DB read on the hot paths.

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// minPasswordLen mirrors the spec's minLength on SetPasswordDTO.password /
// ChangePasswordDTO.new_password.
const minPasswordLen = 8

// tokenTTLWhitelist is the closed PATCH vocabulary for each token lifetime.
// (12h / 24h / 7d / 30d — a whitelist, so a stray 0 can never lock every
// future login out; SettingsUpdateDTO contract).
var tokenTTLWhitelist = map[int]bool{
	43200:   true,
	86400:   true,
	604800:  true,
	2592000: true,
}

// handover_pct bounds. The floor is no longer about out-ordering a separate
// warn threshold (that knob is gone — T-c382): the advance notice is now
// DERIVED as handover_pct - handoverNoticeLeadPct, so the two can never invert.
// 40 remains the floor because the notice must still land somewhere useful —
// below it the lead would put the notice on a barely-used gauge, and at
// handoverNoticeLeadPct or less there would be no notice at all.
const (
	minHandoverPct = 40
	// minNoticePct is deliberately 1, not 40: the SOFT notice is an invitation
	// to start closing out, and an owner who wants it early should get it early.
	// What actually protects the pair is the ordering check (notice strictly
	// below final), not a floor.
	minNoticePct                = 1
	maxHandoverPct              = 90
	minCodexCompactionThreshold = 1
	maxCodexCompactionThreshold = 10
	minMonitoringRefreshSeconds = 1
	maxMonitoringRefreshSeconds = 60
)

// outsource_max_parallel bounds: -1 = 無限 (unlimited — no global cap; the
// left-rail popover's 無限 button); 0 pauses outsource assignment entirely;
// 20 is the sanity ceiling for a FINITE cap (a single-owner studio never
// legitimately wants more).
const (
	minOutsourceParallel = -1
	maxOutsourceParallel = 20
)

// outsourceParallelInRange is the SINGLE source of truth for which
// task.outsource_max_parallel values this build accepts. BOTH faces call it —
// the PATCH validator right below, and the boot-time loader in settings.go
// (loadAuthSettings) — so a value that survives a save can never be the value
// that refuses to boot on the next start.
//
// It exists because the two faces used to disagree: the PATCH face allowed
// -1 (the 無限 button) while the loader applied the generic "non-negative
// integer" check that belongs to timestamp-shaped keys, so saving -1 succeeded
// and the NEXT start died with `FATAL: load settings` (exit 1) — no warning at
// save time. Whoever narrows or widens these bounds must edit this one place,
// and both faces move together.
// acceleratedGraceInRange is the SINGLE source of truth for which
// stop.accelerated_grace_secs values this build accepts, for exactly the reason
// outsourceParallelInRange below is: BOTH faces call it — the PATCH validator
// and the boot-time loader in settings.go — so a value that survives a save can
// never be the value that refuses to boot on the next start.
func acceleratedGraceInRange(n int) bool {
	return n >= minAcceleratedGraceSecs && n <= maxAcceleratedGraceSecs
}

// acceleratedGraceRangeMsg is the ONE wording of that refusal, derived from the
// constants so it can never quote a range the code does not enforce.
var acceleratedGraceRangeMsg = fmt.Sprintf(
	"must be between %d and %d seconds",
	minAcceleratedGraceSecs, maxAcceleratedGraceSecs)

func outsourceParallelInRange(n int) bool {
	return n >= minOutsourceParallel && n <= maxOutsourceParallel
}

// outsourceParallelRangeMsg is the ONE wording of that refusal, derived from
// the constants above so it can never quote a range the code does not enforce.
// The PATCH face prefixes the field name; the loader prefixes `settings <key>`.
// It states the range and nothing else — there is no bypass to teach.
var outsourceParallelRangeMsg = fmt.Sprintf(
	"must be between %d and %d (%d = unlimited)",
	minOutsourceParallel, maxOutsourceParallel, minOutsourceParallel)

// maxOrgNameLen caps the studio display name (org.name; T-d693) — a topbar
// label, not a document. Whitespace is trimmed; "" clears it back to the
// localized default. Counted in runes so CJK names get the full budget.
const maxOrgNameLen = 80

// maxOwnerNameLen caps the owner display nickname (owner.name; T-0b41) — a
// topbar pill label, not a document. Whitespace is trimmed; "" clears it back
// to the localized default. Counted in runes so CJK names get the full budget.
const maxOwnerNameLen = 80

// maxPushContactEmailLen caps the push contact address (push.contact_email;
// T-8a82) at the RFC 5321 maximum length of an address.
const maxPushContactEmailLen = 254

// reservedEmailDomainSuffixes are the domain suffixes that cannot resolve on
// the public internet. A VAPID subject on one of them makes Apple reject the
// signed token wholesale (BadJwtToken) before it looks at any subscription, so
// accepting one here would silently take push down on every device — the exact
// failure T-8a82 was opened for. Rejected at the edge instead.
var reservedEmailDomainSuffixes = []string{".local", ".localhost", ".internal", ".test", ".invalid", ".example"}

// validatePushContactEmail accepts a single trimmed local@domain address on a
// public domain. It is deliberately stricter than RFC 5322: the address is not
// a mailbox we deliver to, it is an identity a push gateway validates.
func validatePushContactEmail(address string) error {
	if utf8.RuneCountInString(address) > maxPushContactEmailLen {
		return fmt.Errorf("push_contact_email must be at most %d characters", maxPushContactEmailLen)
	}
	local, domain, ok := strings.Cut(address, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return errors.New("push_contact_email must be an email address like name@example.com")
	}
	if strings.ContainsAny(address, " \t\r\n:,;<>") {
		return errors.New("push_contact_email must be a single plain address, without a mailto: prefix or display name")
	}
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return errors.New("push_contact_email must use a real public domain")
	}
	lowered := strings.ToLower(domain)
	for _, reserved := range reservedEmailDomainSuffixes {
		if strings.HasSuffix(lowered, reserved) {
			return fmt.Errorf("push_contact_email cannot use the reserved domain %q — push gateways reject it", domain)
		}
	}
	return nil
}

// GET /api/auth/status — PUBLIC: the two pre-auth bits the login wall branches
// on. `password_set` picks first-run setup vs login; `mfa_required` decides
// whether the wall renders a code field.
//
// Disclosing `mfa_required` to an unauthenticated caller is deliberate. The wall
// must render the right fields before anyone holds a token, and the alternative
// — a distinguishable "password accepted, code missing" refusal — leaks strictly
// MORE, because it confirms a correct password. This leaks one bit that a single
// login attempt would reveal anyway.
func (s *apiServer) HandleAuthStatusApiAuthStatusGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, authStatusDTO{
		PasswordSet: s.authPasswordHash() != "",
		MFARequired: s.authMFAEnrolled(),
	})
}

// POST /api/auth/set-password — PUBLIC, gated by the one-shot claim token
// (lifecycle.md §1.3). Order of checks is contract: already set → 409 (the
// token is never consulted); claim mismatch → 401; then store the hash,
// consume the token, and log the caller straight in.
func (s *apiServer) HandleSetPasswordApiAuthSetPasswordPost(w http.ResponseWriter, r *http.Request) {
	// Stamped before any work: the refusal floor is a deadline measured from
	// here, not a sleep appended to whatever this handler spent (throttle.go).
	started := time.Now()
	var body SetPasswordDTO
	if !decodeJSONBodyRequired(w, r, &body, "password", "claim_token") {
		return
	}
	if len(body.Password) < minPasswordLen {
		writeError(w, http.StatusUnprocessableEntity, "password must be at least 8 characters")
		return
	}
	s.settingsMu.Lock()
	// 🔴 NOT `defer s.settingsMu.Unlock()`, and the reason is the floor. The
	// refusal path below waits ~3s before answering, and it must do that with
	// this mutex RELEASED: settingsMu is the whole auth/settings snapshot, so
	// sleeping under it would let an unauthenticated caller stall every settings
	// read and write on the server for three seconds a request — turning a brake
	// on guessing into a denial of service against everything else. The in-flight
	// slot, which is what the wait is supposed to occupy, is held throughout
	// either way.
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			s.settingsMu.Unlock()
		}
	}
	defer unlock()
	if s.passwordHash != "" {
		writeError(w, http.StatusConflict, "a password is already set")
		return
	}
	// 🔴 THE THROTTLE SITS *AFTER* THE 409, NOT BEFORE IT, and the order is the
	// contract. The already-set path never consults the claim token, so nothing
	// is being guessed on it and there is nothing to throttle; gating it would
	// turn a documented 409 into a 429 (measured: it broke
	// test_set_password_after_set_conflicts). The brake belongs on the ONE
	// comparison that is a guessing oracle — the token check below.
	//
	// 🔴 THIS ONE KEEPS THE BRAKE, and it is the seam that shows what the rule
	// actually is. The owner's ruling reads 「只有登入需要 throttling」 and this
	// is not a login — but the line is "can an unauthenticated caller reach it",
	// not "is the caller logged in" (throttle.go). Set-password is PUBLIC
	// (authPublic), it compares a caller-supplied 32-byte secret, and it runs the
	// same argon2id as login (m=19MiB, t=2, p=1 — password.go); the measured
	// shape that made the cap non-negotiable was on exactly this class of route
	// (500 concurrent posts ⇒ ~500 real verifications). A stranger who can reach
	// the port can reach this. So it carries the full brake: the in-flight cap
	// here, and the refusal floor below.
	//
	// ⚠️ Flagged to the owner as a deliberate exception to the literal wording of
	// his ruling; he can overrule it. Until he does, dropping the brake here is
	// not "following the ruling", it is opening an unauthenticated argon2id
	// amplifier.
	release, wait, blocked := s.loginThrottle.begin()
	if blocked {
		writeThrottled(w, wait)
		return
	}
	defer release()
	stored, err := s.dal.GetSetting(settingClaimToken)
	if err != nil {
		internalError(w, err)
		return
	}
	if stored == nil ||
		subtle.ConstantTimeCompare([]byte(*stored), []byte(body.ClaimToken)) != 1 {
		unlock() // before the wait — see the note on the Lock above
		s.holdFailureFloor(started)
		writeError(w, http.StatusUnauthorized, "invalid claim token")
		return
	}
	phc, err := hashPassword(body.Password)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.PutSetting(settingPasswordHash, phc); err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.DeleteSetting(settingClaimToken); err != nil {
		internalError(w, err)
		return
	}
	s.passwordHash = phc
	s.writeOwnerToken(w, s.ownerTokenTTL, time.Now().Unix())
	// T-ba62: the owner has just claimed this server — do the two things that
	// used to be manual (install THIS host's warden, bring the seeded assistant
	// online) so a fresh install lands on a working studio instead of an empty
	// cockpit. Kicked in the BACKGROUND: the run installs a launchd job and then
	// waits for the warden's SSE connect, which must not sit inside this
	// handler's settingsMu. It is idempotent and self-reporting — the outcome
	// (including WHY it failed) is persisted and served on GET /api/settings.
	s.kickFirstRunOnboarding()
}

// POST /api/auth/change-password — owner-gated. Re-verifies the current
// password (a stolen live session cannot silently rotate the credential),
// stores the new hash, and stamps auth.password_changed_at: every owner token
// minted BEFORE the change is refused at the auth gate from now on. The
// response carries a fresh owner token (iat = the stamp) so the current
// session survives its own change. Agent/warden tokens are untouched — the
// signing key never rotates HERE (B1 zero-invalidation). Rotating one is its
// own owner action (api_signing_keys.go); a password change is not one, and
// must not become one by accident.
//
// It deliberately does NOT also demand a TOTP code, unlike mfa/disable. While a
// factor is armed, holding a live owner session already implies having passed
// it, and a password change does not weaken the factor: the new password still
// cannot be used to log in without a code, and the factor itself cannot be
// removed here. mfa/disable is different precisely because it DOES remove the
// factor, which is why that one re-proves it.
//
// 🔴 IT IS NOT THROTTLED AT ALL — not the floor, not the in-flight cap, not one
// call into loginThrottle. That is the owner's ruling, verbatim: 「只有登入需要
// throttling」. It is deliberate, it is a narrowing of what this handler used to
// do, and it must not be quietly re-added by someone who reads the paragraph
// below and thinks a cap looks prudent.
//
// WHY IT HOLDS. Reaching this handler at all means holding a live owner token,
// and a stolen owner token is already the whole disaster this system defends
// against: 「被進來本身嚴重程度跟密碼外流是一樣的」. Every cost a brake imposes
// here is paid by the honest owner in latency and in 429s, against an attacker
// who is already inside and does not need this endpoint to hurt them.
//
// 🔑 AND THE CAP HAD A COST OF ITS OWN, which is what settled it: the pool was
// SHARED with /api/login. A token holder hammering this endpoint could fill all
// four slots and make the OWNER's login answer 429 — an already-authenticated
// caller degrading the front door. Removing the gate here removes that coupling
// outright rather than papering it over with a second pool.
//
// ⚠️ THE COST, STATED PLAINLY AND ACCEPTED KNOWINGLY: whoever holds an owner
// token can guess the CURRENT password here at full speed, and a successful
// guess is a takeover rather than a read — rotating the password stamps
// password_changed_at, which revokes the legitimate owner's own tokens and
// leaves them locked out to a host shell.
//
// 🔴 WHAT REMAINS IS NOT "NOTHING" — IT IS settingsMu, THREE LINES BELOW. An
// earlier version of this comment said concurrent argon2id here was "unbounded",
// which is a scarier claim than the code supports and the exact species of
// defect this ticket exists to catch. The write lock is taken BEFORE
// verifyPassword, so these verifications are FULLY SERIALISED: measured 8
// concurrent calls at 7.1-7.9x the cost of one (ratio ≈ N), against
// /api/login's 4 at 1.15-1.31x (ratio ≈ 1) as the positive control. A token
// holder gets ~1 guess per verification — about 60 a second where one call is
// 16-18 ms — and the process-wide concurrent-argon2id ceiling is login's 4 plus
// this 1, ~95 MiB (arithmetic on the measured concurrency, not measured).
// Dropping the gate changed neither: the old order was begin() → settingsMu →
// verifyPassword, so settingsMu was always the binding constraint. What grew is
// the queue behind the lock, at kilobytes per waiter.
//
// ⚠️ AND THAT LOCK IS SHARED WITH LOGIN. /api/login's verifyAndSpendTOTP takes
// the same write lock, so a caller hammering this endpoint puts every login's
// second-factor step behind its queue. That was equally true before this
// change and is written down here because none of the three documents describing
// this subsystem said it.
func (s *apiServer) HandleChangePasswordApiAuthChangePasswordPost(w http.ResponseWriter, r *http.Request) {
	var body ChangePasswordDTO
	if !decodeJSONBodyRequired(w, r, &body, "current_password", "new_password") {
		return
	}
	if len(body.NewPassword) < minPasswordLen {
		writeError(w, http.StatusUnprocessableEntity, "new_password must be at least 8 characters")
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.passwordHash == "" || !verifyPassword(body.CurrentPassword, s.passwordHash) {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	phc, err := hashPassword(body.NewPassword)
	if err != nil {
		internalError(w, err)
		return
	}
	now := time.Now().Unix()
	if err := s.dal.PutSetting(settingPasswordHash, phc); err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.PutSetting(settingPasswordChangedAt, strconv.FormatInt(now, 10)); err != nil {
		internalError(w, err)
		return
	}
	s.passwordHash = phc
	s.passwordChangedAt = now
	s.writeOwnerToken(w, s.ownerTokenTTL, now)
}

// writeOwnerToken mints and writes the owner tokenDTO. Callers hold
// settingsMu (they pass the ttl they read under it) — the mint itself touches
// no guarded state.
func (s *apiServer) writeOwnerToken(w http.ResponseWriter, ttl, now int64) {
	if len(s.keys.signingSecret()) == 0 {
		writeError(w, http.StatusUnauthorized, "auth not configured")
		return
	}
	token, err := mintJWT(wireOwnerID, "owner", ttl, s.keys.signingSecret(), now, "")
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenDTO{
		Token:     token,
		TokenType: "bearer",
		ExpiresIn: ttl,
		OwnerID:   wireOwnerID,
	})
}

// GET /api/settings — owner-gated read of the adjustable settings.
func (s *apiServer) HandleGetSettingsApiSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settingsView())
}

// PATCH /api/settings — partial update; both knobs validated BEFORE anything
// is written (a 422 writes nothing), then DB write + in-place snapshot update
// under settingsMu: owner_token_ttl applies from the next login,
// agent_token_ttl from the next agent spawn, handover_pct from
// the next context report.
func (s *apiServer) HandleUpdateSettingsApiSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var body SettingsUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.OwnerTokenTtl != nil && !tokenTTLWhitelist[*body.OwnerTokenTtl] {
		writeError(w, http.StatusUnprocessableEntity,
			"owner_token_ttl must be one of 43200, 86400, 604800, 2592000 seconds")
		return
	}
	if body.AgentTokenTtl != nil && !tokenTTLWhitelist[*body.AgentTokenTtl] {
		writeError(w, http.StatusUnprocessableEntity,
			"agent_token_ttl must be one of 43200, 86400, 604800, 2592000 seconds")
		return
	}
	if body.HandoverPct != nil &&
		(*body.HandoverPct < minHandoverPct || *body.HandoverPct > maxHandoverPct) {
		writeError(w, http.StatusUnprocessableEntity, "handover_pct must be between 40 and 90")
		return
	}
	if body.NoticePct != nil &&
		(*body.NoticePct < minNoticePct || *body.NoticePct > maxHandoverPct-1) {
		writeError(w, http.StatusUnprocessableEntity, "notice_pct must be between 1 and 89")
		return
	}
	if body.CodexCompactionThreshold != nil && (*body.CodexCompactionThreshold < minCodexCompactionThreshold || *body.CodexCompactionThreshold > maxCodexCompactionThreshold) {
		writeError(w, http.StatusUnprocessableEntity, "codex_compaction_threshold must be between 1 and 10")
		return
	}
	if body.CodexNoticeRound != nil && (*body.CodexNoticeRound < minCodexCompactionThreshold || *body.CodexNoticeRound > maxCodexCompactionThreshold) {
		writeError(w, http.StatusUnprocessableEntity, "codex_notice_round must be between 1 and 10")
		return
	}
	// The two offboard points are a PAIR and are checked against the POST-PATCH
	// values, not against whatever arrived in this body: either one may be sent
	// alone, and what must hold is that the SOFT notice still lands strictly
	// before the FINAL one. A pair that crosses is REFUSED rather than quietly
	// reordered — silently swapping them would leave the owner looking at a
	// cockpit that disagrees with when his agents actually get notified. Same
	// rule on both axes; codex measures in rounds because that is what its
	// handover reads.
	//
	// Both sides read the EFFECTIVE current value, not the raw stored one: a
	// server whose pair has never been written holds zeroes, and comparing
	// those would refuse an unrelated patch (org_name, a doc cap) with a
	// complaint about numbers the caller never sent.
	ctxNow := s.ctxHighConfig()
	shipped := defaultSseContextHigh()
	noticePct, handoverPct := ctxNow.NoticePct, ctxNow.HandoverPct
	if handoverPct <= 0 {
		handoverPct = shipped.HandoverPct
	}
	if noticePct <= 0 {
		noticePct = shipped.NoticePct
	}
	if body.NoticePct != nil {
		noticePct = *body.NoticePct
	}
	if body.HandoverPct != nil {
		handoverPct = *body.HandoverPct
	}
	if noticePct >= handoverPct {
		writeError(w, http.StatusUnprocessableEntity,
			"notice_pct must be strictly below handover_pct")
		return
	}
	noticeRound, finalRound := s.codexNoticeRoundSetting(), s.codexCompactionThresholdSetting()
	if finalRound < 1 {
		finalRound = defaultCodexCompactionThreshold
	}
	if noticeRound < 1 {
		noticeRound = finalRound - 1
	}
	if body.CodexNoticeRound != nil {
		noticeRound = *body.CodexNoticeRound
	}
	if body.CodexCompactionThreshold != nil {
		finalRound = *body.CodexCompactionThreshold
	}
	if noticeRound >= finalRound {
		writeError(w, http.StatusUnprocessableEntity,
			"codex_notice_round must be strictly below codex_compaction_threshold")
		return
	}
	if body.MonitoringRefreshSeconds != nil && (*body.MonitoringRefreshSeconds < minMonitoringRefreshSeconds || *body.MonitoringRefreshSeconds > maxMonitoringRefreshSeconds) {
		writeError(w, http.StatusUnprocessableEntity, "monitoring_refresh_seconds must be between 1 and 60")
		return
	}
	if body.AcceleratedGraceSecs != nil &&
		!acceleratedGraceInRange(*body.AcceleratedGraceSecs) {
		writeError(w, http.StatusUnprocessableEntity,
			"accelerated_grace_secs "+acceleratedGraceRangeMsg)
		return
	}
	if body.OutsourceMaxParallel != nil &&
		!outsourceParallelInRange(*body.OutsourceMaxParallel) {
		writeError(w, http.StatusUnprocessableEntity,
			"outsource_max_parallel "+outsourceParallelRangeMsg)
		return
	}
	// Each floor is THAT segment's shipped default, so a knob only ever RAISES
	// its cap (owner 2026-07-31). Lowering one would strand every document that
	// is legal today in shrink-only mode — the refusal says so rather than
	// making the caller infer it from a bare range. Five independent knobs:
	// three role-journal segments since T-ae38, and the manual's SOP and
	// learnings since T-30f1. Duty's floor is minDutyCapChars, NOT the other
	// four's minDocCapChars, or its own shipped default would be unreachable
	// from this surface. The numbers live in domain.go — do not restate them
	// here. Every knob must appear in this table: a missing row is not a
	// missing check, it is an UNCHECKED cap that the load face will later
	// refuse to boot on.
	capRange := []struct {
		field *int
		name  string
		min   int
	}{
		{body.DocCapCharsDuty, "doc_cap_chars_duty", minDutyCapChars},
		{body.DocCapCharsInsight, "doc_cap_chars_insight", minDocCapChars},
		{body.DocCapCharsLearning, "doc_cap_chars_learning", minDocCapChars},
		{body.DocCapCharsManualSop, "doc_cap_chars_manual_sop", minDocCapChars},
		{body.DocCapCharsManualLearnings, "doc_cap_chars_manual_learnings", minDocCapChars},
		{body.DocCapCharsSystemInteraction, "doc_cap_chars_system_interaction", minSystemInteractionCapChars},
		{body.DocCapCharsBootSequence, "doc_cap_chars_boot_sequence", minBootSequenceCapChars},
		{body.DocCapCharsOffboard, "doc_cap_chars_offboard", minOffboardCapChars},
	}
	for _, c := range capRange {
		if c.field != nil && (*c.field < c.min || *c.field > maxDocCapChars) {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("%s must be between %d and %d characters — the floor is the shipped default, so the document cap can only be raised, never lowered",
					c.name, c.min, maxDocCapChars))
			return
		}
	}
	// chat_budget_chars (T-c9b4) is checked on its own and NOT as a row in the
	// table above: it has its own ceiling, and the message above ("the floor is
	// the shipped default … can only be raised") would be a lie about it. The
	// chat block is repacked from scratch on every read, so lowering the budget
	// costs nothing that the doc caps' floor rule exists to protect — the owner
	// asked for a knob he can turn DOWN.
	//
	// 🔴 maxChatBudgetChars is pinned to resumeChatFetch (see domain.go). Raising
	// it here without raising that constant first breaks the packer's guarantee
	// that it never runs out of candidates before it runs out of budget.
	if body.ChatBudgetChars != nil &&
		(*body.ChatBudgetChars < minChatBudgetChars || *body.ChatBudgetChars > maxChatBudgetChars) {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("chat_budget_chars must be between %d and %d characters",
				minChatBudgetChars, maxChatBudgetChars))
		return
	}
	// backup_retain (T-8) — checked on its own too. It is not a character count,
	// its unit is FILES, and it is the only knob on this endpoint whose value
	// causes DELETION, so it does not belong in a table whose shared message
	// talks about characters and shipped-default floors.
	if body.BackupRetain != nil &&
		(*body.BackupRetain < minBackupRetain || *body.BackupRetain > maxBackupRetain) {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("backup_retain must be between %d and %d backups per pool",
				minBackupRetain, maxBackupRetain))
		return
	}
	var orgName string
	if body.OrgName != nil {
		orgName = strings.TrimSpace(*body.OrgName)
		if utf8.RuneCountInString(orgName) > maxOrgNameLen {
			writeError(w, http.StatusUnprocessableEntity,
				"org_name must be at most 80 characters")
			return
		}
	}
	var ownerName string
	if body.OwnerName != nil {
		ownerName = strings.TrimSpace(*body.OwnerName)
		if utf8.RuneCountInString(ownerName) > maxOwnerNameLen {
			writeError(w, http.StatusUnprocessableEntity,
				"owner_name must be at most 80 characters")
			return
		}
	}
	var pushContactEmail string
	if body.PushContactEmail != nil {
		pushContactEmail = strings.TrimSpace(*body.PushContactEmail)
		if pushContactEmail != "" {
			if err := validatePushContactEmail(pushContactEmail); err != nil {
				writeError(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
		}
	}
	// display.theme names WHICH theme is active; the themes themselves left this
	// endpoint in T-83ef and live behind /api/themes now. So the vocabulary this
	// value is checked against is read from the TABLE rather than from a bundle
	// array that used to arrive in the same request.
	//
	// ⚠️ THAT COSTS SOMETHING REAL AND IT IS A DELIBERATE TRADE. Before the split
	// a caller could land a new theme AND select it in ONE patch, because the
	// bundle was in the body being validated. It cannot any more: the theme has
	// to exist (PUT /api/themes/{id}) before it can be selected. Two requests
	// instead of one is the price of "changing one theme does not re-send them
	// all", and the failure mode is loud (a 422 naming the field), not silent.
	var displayTheme string
	themeProvided := body.DisplayTheme != nil
	if themeProvided {
		displayTheme = strings.TrimSpace(*body.DisplayTheme)
		ok, err := s.displayThemeExists(displayTheme)
		if err != nil {
			internalError(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnprocessableEntity,
				`display_theme must be "", office, or an existing custom theme id`)
			return
		}
	}
	var displayLanguage string
	if body.DisplayLanguage != nil {
		displayLanguage = strings.TrimSpace(*body.DisplayLanguage)
		if displayLanguage != "" && !displayLanguageAllowed[displayLanguage] {
			writeError(w, http.StatusUnprocessableEntity,
				"display_language must be one of zh, en")
			return
		}
	}
	s.settingsMu.Lock()
	if body.OwnerTokenTtl != nil {
		if err := s.dal.PutSetting(settingOwnerTokenTTL, strconv.Itoa(*body.OwnerTokenTtl)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.ownerTokenTTL = int64(*body.OwnerTokenTtl)
	}
	if body.AgentTokenTtl != nil {
		if err := s.dal.PutSetting(settingAgentTokenTTL, strconv.Itoa(*body.AgentTokenTtl)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.agentTokenTTL = int64(*body.AgentTokenTtl)
	}
	if body.HandoverPct != nil {
		if err := s.dal.PutSetting(settingCtxHandoverPct, strconv.Itoa(*body.HandoverPct)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.ctxhigh.HandoverPct = *body.HandoverPct
	}
	if body.NoticePct != nil {
		if err := s.dal.PutSetting(settingCtxNoticePct, strconv.Itoa(*body.NoticePct)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.ctxhigh.NoticePct = *body.NoticePct
	}
	if body.CodexCompactionThreshold != nil {
		if err := s.dal.PutSetting(settingCodexCompactionThreshold, strconv.Itoa(*body.CodexCompactionThreshold)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.codexCompactionThreshold = *body.CodexCompactionThreshold
	}
	if body.CodexNoticeRound != nil {
		if err := s.dal.PutSetting(settingCodexNoticeRound, strconv.Itoa(*body.CodexNoticeRound)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.codexNoticeRound = *body.CodexNoticeRound
	}
	if body.MonitoringRefreshSeconds != nil {
		if err := s.dal.PutSetting(settingMonitoringRefreshSeconds, strconv.Itoa(*body.MonitoringRefreshSeconds)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.monitoringRefreshSeconds = *body.MonitoringRefreshSeconds
	}
	if body.AcceleratedGraceSecs != nil {
		if err := s.dal.PutSetting(settingAcceleratedGraceSecs,
			strconv.Itoa(*body.AcceleratedGraceSecs)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.acceleratedGraceSecs = *body.AcceleratedGraceSecs
	}
	if body.OutsourceMaxParallel != nil {
		if err := s.dal.PutSetting(settingOutsourceMaxParallel,
			strconv.Itoa(*body.OutsourceMaxParallel)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.outsourceMaxParallel = *body.OutsourceMaxParallel
	}
	capWrite := []struct {
		field *int
		key   string
		dst   *int
	}{
		{body.DocCapCharsDuty, settingDocCapCharsDuty, &s.docCapCharsDuty},
		{body.DocCapCharsInsight, settingDocCapCharsInsight, &s.docCapCharsInsight},
		{body.DocCapCharsLearning, settingDocCapCharsLearning, &s.docCapCharsLearning},
		{body.DocCapCharsManualSop, settingDocCapCharsManualSop, &s.docCapCharsManualSop},
		{body.DocCapCharsManualLearnings, settingDocCapCharsManualLearnings, &s.docCapCharsManualLearnings},
		{body.DocCapCharsSystemInteraction, settingDocCapCharsSystemInteraction, &s.docCapCharsSystemInteraction},
		{body.DocCapCharsBootSequence, settingDocCapCharsBootSequence, &s.docCapCharsBootSequence},
		{body.DocCapCharsOffboard, settingDocCapCharsOffboard, &s.docCapCharsOffboard},
		{body.ChatBudgetChars, settingChatBudgetChars, &s.chatBudgetChars},
		{body.BackupRetain, settingBackupRetain, &s.backupRetain},
	}
	for _, c := range capWrite {
		if c.field == nil {
			continue
		}
		if err := s.dal.PutSetting(c.key, strconv.Itoa(*c.field)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		*c.dst = *c.field
	}
	// A channel flip changes WHO "latest" is (official-only vs prereleases
	// too) — it re-kicks the GitHub check so the software-update card follows
	// immediately.
	updaterChanged := false
	if body.UpdaterReceiveBeta != nil && *body.UpdaterReceiveBeta != s.updaterReceiveBeta {
		if err := s.dal.PutSetting(settingUpdaterReceiveBeta,
			strconv.FormatBool(*body.UpdaterReceiveBeta)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.updaterReceiveBeta = *body.UpdaterReceiveBeta
		updaterChanged = true
	}
	// The auto-update toggle needs no kick: the cadence (auto_update.go)
	// reads the live snapshot on its next tick.
	if body.UpdaterAutoUpdate != nil && *body.UpdaterAutoUpdate != s.updaterAutoUpdate {
		if err := s.dal.PutSetting(settingUpdaterAutoUpdate,
			strconv.FormatBool(*body.UpdaterAutoUpdate)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.updaterAutoUpdate = *body.UpdaterAutoUpdate
	}
	if body.OrgName != nil && orgName != s.orgName {
		if err := s.dal.PutSetting(settingOrgName, orgName); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.orgName = orgName
	}
	if body.OwnerName != nil && ownerName != s.ownerName {
		if err := s.dal.PutSetting(settingOwnerName, ownerName); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.ownerName = ownerName
	}
	if body.PushContactEmail != nil && pushContactEmail != s.pushContactEmail {
		if err := s.dal.PutSetting(settingPushContactEmail, pushContactEmail); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.pushContactEmail = pushContactEmail
	}
	// The custom_themes ↔ display.theme coupling that used to live here is GONE
	// from this endpoint, and deliberately so: this endpoint can no longer delete
	// a theme, so it can no longer orphan the active one. The one request that
	// still can — DELETE /api/themes/{id} — performs the reset itself and reports
	// it in its receipt (api_themes.go). A "is the active theme still there?"
	// sweep here would be a second opinion about a fact that endpoint already
	// settled, and the two would drift.
	if themeProvided && displayTheme != s.displayTheme {
		finalTheme := displayTheme
		if err := s.dal.PutSetting(settingDisplayTheme, finalTheme); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.displayTheme = finalTheme
	}
	if body.DisplayLanguage != nil && displayLanguage != s.displayLanguage {
		if err := s.dal.PutSetting(settingDisplayLanguage, displayLanguage); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.displayLanguage = displayLanguage
	}
	// display.wide (T-756f) is a plain bool with no enum to validate — same
	// store-as-text shape as the two updater toggles.
	if body.DisplayWide != nil && *body.DisplayWide != s.displayWide {
		if err := s.dal.PutSetting(settingDisplayWide,
			strconv.FormatBool(*body.DisplayWide)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.displayWide = *body.DisplayWide
	}
	// lore.enabled (T-33) — the station-wide lore feature switch. Same
	// store-as-text plain-bool shape as display.wide right above it, and it goes
	// through the SAME owner/admin-gated PATCH as every other knob on this
	// surface on purpose: 誰能切 was answered by the station's existing settings
	// mechanism, not by a new permission invented for this one flag.
	//
	// 🔴 THE IN-MEMORY WRITE THREE LINES DOWN IS WHAT MAKES THE PROMISE TRUE.
	// Every lore path reads loreEnabledSnapshot() per call, so the moment this
	// assignment lands under settingsMu the next request sees the new value —
	// no restart, no cache to expire. The one surface that cannot follow is a
	// boot context, which is assembled once at wake.
	if body.LoreEnabled != nil && *body.LoreEnabled != s.loreEnabled {
		if err := s.dal.PutSetting(settingLoreEnabled,
			strconv.FormatBool(*body.LoreEnabled)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.loreEnabled = *body.LoreEnabled
	}
	s.settingsMu.Unlock()
	// onboarding_dismissed (T-0648) is written OUTSIDE settingsMu, and last:
	// it does not live in the settings snapshot at all — it is a field on the
	// onboarding report row, which settingsView already reads straight from the
	// DAL (the run finishes in its own goroutine, so a snapshot copy would go
	// stale).
	//
	// A dismissal with no banner behind it (no report, or a report that is not
	// `failed`) is refused with 409 rather than absorbed as a quiet 200: on a
	// run that is still `running` the refusal is what keeps this request's
	// unlocked read-modify-write from interleaving with the run's own write and
	// ERASING the verdict — see setOnboardingDismissed. The banner is the only
	// sender and it sends this field on its own, so the refusal does not strand
	// a half-applied settings PATCH in practice.
	if body.OnboardingDismissed != nil {
		if err := s.setOnboardingDismissed(*body.OnboardingDismissed); err != nil {
			if errors.Is(err, errNoOnboardingBanner) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			internalError(w, err)
			return
		}
	}
	if updaterChanged {
		// Force-expire the update-check cache + refresh in the background so
		// the software-update card reflects the new channel without waiting
		// out the TTL (never blocks this response).
		s.kickUpdateCheck()
	}
	writeJSON(w, http.StatusOK, s.settingsView())
}

// settingsView assembles the SettingsDTO body from the live in-memory snapshot.
// Every field is served from memory, so this cannot fail.
func (s *apiServer) settingsView() settingsDTO {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return settingsDTO{
		OwnerTokenTTL:                s.ownerTokenTTL,
		AgentTokenTTL:                s.agentTokenTTL,
		HandoverPct:                  s.ctxhigh.HandoverPct,
		NoticePct:                    s.ctxhigh.NoticePct,
		CodexCompactionThreshold:     s.codexCompactionThreshold,
		CodexNoticeRound:             s.codexNoticeRound,
		MonitoringRefreshSeconds:     s.monitoringRefreshSeconds,
		AcceleratedGraceSecs:         s.acceleratedGraceSecs,
		OutsourceMaxParallel:         s.outsourceMaxParallel,
		DocCapCharsDuty:              s.docCapCharsDuty,
		DocCapCharsInsight:           s.docCapCharsInsight,
		DocCapCharsLearning:          s.docCapCharsLearning,
		DocCapCharsManualSop:         s.docCapCharsManualSop,
		DocCapCharsManualLearnings:   s.docCapCharsManualLearnings,
		DocCapCharsSystemInteraction: s.docCapCharsSystemInteraction,
		DocCapCharsBootSequence:      s.docCapCharsBootSequence,
		DocCapCharsOffboard:          s.docCapCharsOffboard,
		ChatBudgetChars:              s.chatBudgetChars,
		BackupRetain:                 s.backupRetain,
		UpdaterReceiveBeta:           s.updaterReceiveBeta,
		UpdaterAutoUpdate:            s.updaterAutoUpdate,
		OrgName:                      s.orgName,
		OwnerName:                    s.ownerName,
		PushContactEmail:             s.pushContactEmail,
		DisplayTheme:                 s.displayTheme,
		DisplayLanguage:              s.displayLanguage,
		DisplayWide:                  s.displayWide,
		LoreEnabled:                  s.loreEnabled,
		// Read from the DAL, NOT from the settings snapshot: onboarding runs in
		// its own goroutine and finishes after this handler returned, so a
		// boot-time snapshot would serve a permanently stale "running".
		Onboarding: s.onboardingReport(),
	}
}
