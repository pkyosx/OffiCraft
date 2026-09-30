package main

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

// A whitelist, so a stray 0 can never lock every future login out.
var tokenTTLWhitelist = map[int]bool{
	43200:   true,
	86400:   true,
	604800:  true,
	2592000: true,
}

const (
	minHandoverPct = 40
	// minNoticePct is deliberately 1: what protects the pair is the ordering check
	// (notice strictly below final), not a floor.
	minNoticePct                = 1
	maxHandoverPct              = 90
	minCodexCompactionThreshold = 1
	maxCodexCompactionThreshold = 10
	minMonitoringRefreshSeconds = 1
	maxMonitoringRefreshSeconds = 60
)

// outsource_max_parallel: -1 = unlimited (no global cap); 0 pauses outsource
// assignment entirely.
const (
	minOutsourceParallel = -1
	maxOutsourceParallel = 20
)

func acceleratedGraceInRange(n int) bool {
	return n >= minAcceleratedGraceSecs && n <= maxAcceleratedGraceSecs
}

func wardenCredLifetimeInRange(n int) bool {
	return n >= minWardenCredLifetimeSecs && n <= maxWardenCredLifetimeSecs
}

var wardenCredLifetimeRangeMsg = fmt.Sprintf(
	"must be between %d and %d seconds (one day through 400 days) — a warden renews at "+
		"two thirds of the lifetime, so the remaining third is the window an offline "+
		"machine has to get a replacement, and below a day that window stops surviving "+
		"a working day of downtime",
	minWardenCredLifetimeSecs, maxWardenCredLifetimeSecs)

func reassignHandoverTimeoutInRange(n int) bool {
	return n >= minReassignHandoverTimeoutSecs && n <= maxReassignHandoverTimeoutSecs
}

var reassignHandoverTimeoutRangeMsg = fmt.Sprintf(
	"must be between %d and %d seconds",
	minReassignHandoverTimeoutSecs, maxReassignHandoverTimeoutSecs)

func runtimeLoginCheckIntervalInRange(n int) bool {
	return n >= minRuntimeLoginCheckIntervalSecs && n <= maxRuntimeLoginCheckIntervalSecs
}

var runtimeLoginCheckIntervalRangeMsg = fmt.Sprintf(
	"must be between %d and %d seconds",
	minRuntimeLoginCheckIntervalSecs, maxRuntimeLoginCheckIntervalSecs)

var acceleratedGraceRangeMsg = fmt.Sprintf(
	"must be between %d and %d seconds",
	minAcceleratedGraceSecs, maxAcceleratedGraceSecs)

func outsourceParallelInRange(n int) bool {
	return n >= minOutsourceParallel && n <= maxOutsourceParallel
}

var outsourceParallelRangeMsg = fmt.Sprintf(
	"must be between %d and %d (%d = unlimited)",
	minOutsourceParallel, maxOutsourceParallel, minOutsourceParallel)

const maxOrgNameLen = 80

const maxOwnerNameLen = 80

// maxPushContactEmailLen is the RFC 5321 maximum length of an address.
const maxPushContactEmailLen = 254

// A VAPID subject on one of these makes Apple reject the signed token wholesale
// (BadJwtToken), silently taking push down on every device.
var reservedEmailDomainSuffixes = []string{".local", ".localhost", ".internal", ".test", ".invalid", ".example"}

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

// GET /api/auth/status — PUBLIC. Disclosing `mfa_required` before auth is
// deliberate: the alternative, a distinguishable "password accepted, code
// missing" refusal, leaks MORE because it confirms a correct password.
func (s *apiServer) HandleAuthStatusApiAuthStatusGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, authStatusDTO{
		PasswordSet: s.authPasswordHash() != "",
		MFARequired: s.authMFAEnrolled(),
	})
}

// POST /api/auth/set-password — PUBLIC, gated by the one-shot claim token.
// Order of checks is contract (lifecycle.md §1.3): already set → 409 (the token
// is never consulted); claim mismatch → 401.
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
	// 🔴 NOT a deferred unlock alone: the refusal path waits ~3s and must do so
	// with this mutex RELEASED — sleeping under it would let an unauthenticated
	// caller stall every settings write on the server.
	unlock := s.settingsWriteMu.Acquire()
	defer unlock()
	if s.passwordHash != "" {
		writeError(w, http.StatusConflict, "a password is already set")
		return
	}
	// 🔴 THE THROTTLE SITS *AFTER* THE 409: the already-set path consults no
	// secret, and gating it turns a documented 409 into a 429 (measured).
	// It keeps the brake despite the owner's 「只有登入需要 throttling」: the line
	// is "can an unauthenticated caller reach it" (throttle.go), and this public
	// route runs argon2id — dropping the brake opens an unauthenticated argon2id
	// amplifier. Flagged to the owner as a deliberate exception.
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
		unlock()
		s.holdFailureFloor(started)
		writeError(w, http.StatusUnauthorized, "invalid claim token")
		return
	}
	phc, err := hashPassword(body.Password)
	if err != nil {
		internalError(w, err)
		return
	}
	// The claim is judged again on the row the transaction consumes: the hash is
	// computed outside it (argon2id must not hold the write connection).
	err = s.dal.inTx(func(tx *writeTx) error {
		stored, err := getSettingOn(tx, settingClaimToken)
		if err != nil {
			return err
		}
		if stored == nil ||
			subtle.ConstantTimeCompare([]byte(*stored), []byte(body.ClaimToken)) != 1 {
			return refuseInTx(http.StatusUnauthorized, "invalid claim token")
		}
		if err := putSettingOn(tx, settingPasswordHash, phc); err != nil {
			return err
		}
		return deleteSettingOn(tx, settingClaimToken)
	})
	var refusal *txRefusal
	if errors.As(err, &refusal) {
		unlock()
		s.holdFailureFloor(started)
		writeTxError(w, err)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	s.applySettings(func() { s.passwordHash = phc })
	s.writeOwnerToken(w, s.ownerTokenTTL, time.Now().Unix())
	// Kicked in the BACKGROUND: the run installs a launchd job and then waits
	// for the warden's SSE connect, which must not sit inside settingsWriteMu. Its
	// outcome is persisted and served on GET /api/settings.
	s.kickFirstRunOnboarding()
}

// POST /api/auth/change-password. The signing key never rotates HERE, so
// agent/warden tokens are untouched — a password change must not become a key
// rotation by accident (that is api_signing_keys.go).
//
// It deliberately does NOT demand a TOTP code: a live owner session already
// passed the factor, and this cannot remove it (mfa/disable can, so it
// re-proves it).
//
// 🔴 IT IS NOT THROTTLED AT ALL — owner ruling 「只有登入需要 throttling」; do not
// re-add a cap.
//
// What bounds it is settingsWriteMu, taken BEFORE verifyPassword: verifications
// here are fully serialised (measured). ⚠️ That lock is shared with /api/login's
// verifyAndSpendTOTP, so hammering this endpoint queues every login's
// second-factor step.
func (s *apiServer) HandleChangePasswordApiAuthChangePasswordPost(w http.ResponseWriter, r *http.Request) {
	var body ChangePasswordDTO
	if !decodeJSONBodyRequired(w, r, &body, "current_password", "new_password") {
		return
	}
	if len(body.NewPassword) < minPasswordLen {
		writeError(w, http.StatusUnprocessableEntity, "new_password must be at least 8 characters")
		return
	}
	s.settingsWriteMu.Lock()
	defer s.settingsWriteMu.Unlock()
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
	if err := s.dal.inTx(func(tx *writeTx) error {
		if err := putSettingOn(tx, settingPasswordHash, phc); err != nil {
			return err
		}
		return putSettingOn(tx, settingPasswordChangedAt, strconv.FormatInt(now, 10))
	}); err != nil {
		internalError(w, err)
		return
	}
	s.applySettings(func() {
		s.passwordHash = phc
		s.passwordChangedAt = now
	})
	s.writeOwnerToken(w, s.ownerTokenTTL, now)
}

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

func (s *apiServer) HandleGetSettingsApiSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settingsView())
}

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
	// The notice/final pair is checked on POST-PATCH values and a crossed pair is
	// REFUSED, never reordered. Both sides use the EFFECTIVE current value: a
	// server whose pair was never written holds zeroes, which would refuse an
	// unrelated patch over numbers the caller never sent.
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
	if body.ReassignHandoverTimeoutSecs != nil &&
		!reassignHandoverTimeoutInRange(*body.ReassignHandoverTimeoutSecs) {
		writeError(w, http.StatusUnprocessableEntity,
			"reassign_handover_timeout_secs "+reassignHandoverTimeoutRangeMsg)
		return
	}
	if body.RuntimeLoginCheckIntervalSecs != nil &&
		!runtimeLoginCheckIntervalInRange(*body.RuntimeLoginCheckIntervalSecs) {
		writeError(w, http.StatusUnprocessableEntity,
			"runtime_login_check_interval_secs "+runtimeLoginCheckIntervalRangeMsg)
		return
	}
	if body.RuntimeLoginRecheckIntervalSecs != nil &&
		!runtimeLoginCheckIntervalInRange(*body.RuntimeLoginRecheckIntervalSecs) {
		writeError(w, http.StatusUnprocessableEntity,
			"runtime_login_recheck_interval_secs "+runtimeLoginCheckIntervalRangeMsg)
		return
	}
	if body.OutsourceMaxParallel != nil &&
		!outsourceParallelInRange(*body.OutsourceMaxParallel) {
		writeError(w, http.StatusUnprocessableEntity,
			"outsource_max_parallel "+outsourceParallelRangeMsg)
		return
	}
	if body.WardenCredentialLifetimeSecs != nil &&
		!wardenCredLifetimeInRange(*body.WardenCredentialLifetimeSecs) {
		writeError(w, http.StatusUnprocessableEntity,
			"warden_credential_lifetime_secs "+wardenCredLifetimeRangeMsg)
		return
	}
	// Bounds and reasoning live at minDocCapChars in domain.go. Every doc cap must
	// have a row here: a missing row is an UNCHECKED cap that the load face will
	// later refuse to boot on.
	capRange := []struct {
		field *int
		name  string
		min   int
	}{
		{body.DocCapCharsDuty, "doc_cap_chars_duty", minDocCapChars},
		{body.DocCapCharsInsight, "doc_cap_chars_insight", minDocCapChars},
		{body.DocCapCharsManualSop, "doc_cap_chars_manual_sop", minDocCapChars},
		{body.DocCapCharsSystemInteraction, "doc_cap_chars_system_interaction", minDocCapChars},
		{body.DocCapCharsBootSequence, "doc_cap_chars_boot_sequence", minDocCapChars},
		{body.DocCapCharsOffboard, "doc_cap_chars_offboard", minDocCapChars},
	}
	for _, c := range capRange {
		if c.field != nil && (*c.field < c.min || *c.field > maxDocCapChars) {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("%s must be between %d and %d characters — a lowered cap binds the next write only; stored content over it is never truncated and still reads back",
					c.name, c.min, maxDocCapChars))
			return
		}
	}
	if body.ChatBudgetChars != nil &&
		(*body.ChatBudgetChars < minChatBudgetChars || *body.ChatBudgetChars > maxChatBudgetChars) {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("chat_budget_chars must be between %d and %d characters",
				minChatBudgetChars, maxChatBudgetChars))
		return
	}
	if body.StepNoteCapChars != nil &&
		(*body.StepNoteCapChars < minStepNoteCapChars || *body.StepNoteCapChars > maxStepNoteCapChars) {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("step_note_cap_chars must be between %d and %d characters",
				minStepNoteCapChars, maxStepNoteCapChars))
		return
	}
	loreRange := []struct {
		field    *int
		name     string
		min, max int
	}{
		{body.LoreCapCharsRole, "lore_cap_chars_role", minLoreFoldCapChars, maxLoreFoldCapChars},
		{body.LoreCapCharsManual, "lore_cap_chars_manual", minLoreFoldCapChars, maxLoreFoldCapChars},
		{body.LoreCapCharsTitle, "lore_cap_chars_title", minLoreEntryCapChars, maxLoreEntryCapChars},
		{body.LoreCapCharsBody, "lore_cap_chars_body", minLoreEntryCapChars, maxLoreEntryCapChars},
	}
	for _, c := range loreRange {
		if c.field != nil && (*c.field < c.min || *c.field > c.max) {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("%s must be between %d and %d characters", c.name, c.min, c.max))
			return
		}
	}
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
	// display.theme is checked against the custom_theme TABLE, so a theme must
	// exist (PUT /api/themes/{id}) before it can be selected — a deliberate trade:
	// a new theme can no longer be created and selected in one patch.
	var displayTheme string
	themeProvided := body.DisplayTheme != nil
	if themeProvided {
		displayTheme = strings.TrimSpace(*body.DisplayTheme)
		ok, err := displayThemeExistsOn(s.dal.rdb, displayTheme)
		if err != nil {
			internalError(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnprocessableEntity, displayThemeRefusal)
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
	// 🔴 An explicit empty array is LEGAL and clears the list — the OPPOSITE of the
	// scheduled-message custom_* sets, where [] is a 422.
	var suggestedRepliesReplyCard, suggestedRepliesTaskMessage,
		suggestedRepliesLoreMessage []string
	if body.SuggestedRepliesReplyCard != nil {
		list, err := canonicalSuggestedReplies(*body.SuggestedRepliesReplyCard)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("suggested_replies_reply_card %v", err))
			return
		}
		suggestedRepliesReplyCard = list
	}
	if body.SuggestedRepliesTaskMessage != nil {
		list, err := canonicalSuggestedReplies(*body.SuggestedRepliesTaskMessage)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("suggested_replies_task_message %v", err))
			return
		}
		suggestedRepliesTaskMessage = list
	}
	if body.SuggestedRepliesLoreMessage != nil {
		list, err := canonicalSuggestedReplies(*body.SuggestedRepliesLoreMessage)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("suggested_replies_lore_message %v", err))
			return
		}
		suggestedRepliesLoreMessage = list
	}
	// Every row lands in one transaction and the in-memory snapshot moves only
	// after it commits, so a failed row leaves neither the table nor the
	// snapshot half-patched.
	type settingWrite struct {
		key, value string
		apply      func()
	}
	var writes []settingWrite
	put := func(key, value string, apply func()) {
		writes = append(writes, settingWrite{key: key, value: value, apply: apply})
	}
	unlockMu := s.settingsWriteMu.Acquire()
	defer unlockMu()
	if body.OwnerTokenTtl != nil {
		v := *body.OwnerTokenTtl
		put(settingOwnerTokenTTL, strconv.Itoa(v), func() { s.ownerTokenTTL = int64(v) })
	}
	if body.AgentTokenTtl != nil {
		v := *body.AgentTokenTtl
		put(settingAgentTokenTTL, strconv.Itoa(v), func() { s.agentTokenTTL = int64(v) })
	}
	if body.HandoverPct != nil {
		v := *body.HandoverPct
		put(settingCtxHandoverPct, strconv.Itoa(v), func() { s.ctxHigh.HandoverPct = v })
	}
	if body.NoticePct != nil {
		v := *body.NoticePct
		put(settingCtxNoticePct, strconv.Itoa(v), func() { s.ctxHigh.NoticePct = v })
	}
	if body.CodexCompactionThreshold != nil {
		v := *body.CodexCompactionThreshold
		put(settingCodexCompactionThreshold, strconv.Itoa(v), func() { s.codexCompactionThreshold = v })
	}
	if body.CodexNoticeRound != nil {
		v := *body.CodexNoticeRound
		put(settingCodexNoticeRound, strconv.Itoa(v), func() { s.codexNoticeRound = v })
	}
	if body.MonitoringRefreshSeconds != nil {
		v := *body.MonitoringRefreshSeconds
		put(settingMonitoringRefreshSeconds, strconv.Itoa(v), func() { s.monitoringRefreshSeconds = v })
	}
	if body.AcceleratedGraceSecs != nil {
		v := *body.AcceleratedGraceSecs
		put(settingAcceleratedGraceSecs, strconv.Itoa(v), func() { s.acceleratedGraceSecs = v })
	}
	if body.ReassignHandoverTimeoutSecs != nil {
		v := *body.ReassignHandoverTimeoutSecs
		put(settingReassignHandoverTimeoutSecs, strconv.Itoa(v), func() { s.reassignHandoverTimeoutSecs = v })
	}
	if body.RuntimeLoginCheckIntervalSecs != nil {
		v := *body.RuntimeLoginCheckIntervalSecs
		put(settingRuntimeLoginCheckIntervalSecs, strconv.Itoa(v), func() { s.runtimeLoginCheckIntervalSecs = v })
	}
	if body.RuntimeLoginRecheckIntervalSecs != nil {
		v := *body.RuntimeLoginRecheckIntervalSecs
		put(settingRuntimeLoginRecheckIntervalSecs, strconv.Itoa(v), func() { s.runtimeLoginRecheckIntervalSecs = v })
	}
	if body.WardenCredentialLifetimeSecs != nil {
		v := *body.WardenCredentialLifetimeSecs
		put(settingWardenCredLifetimeSecs, strconv.Itoa(v), func() { s.wardenCredLifetimeSecs = v })
	}
	if body.OutsourceMaxParallel != nil {
		v := *body.OutsourceMaxParallel
		put(settingOutsourceMaxParallel, strconv.Itoa(v), func() { s.outsourceMaxParallel = v })
	}
	capWrite := []struct {
		field *int
		key   string
		dst   *int
	}{
		{body.DocCapCharsDuty, settingDocCapCharsDuty, &s.docCapCharsDuty},
		{body.DocCapCharsInsight, settingDocCapCharsInsight, &s.docCapCharsInsight},
		{body.DocCapCharsManualSop, settingDocCapCharsManualSop, &s.docCapCharsManualSop},
		{body.DocCapCharsSystemInteraction, settingDocCapCharsSystemInteraction, &s.docCapCharsSystemInteraction},
		{body.DocCapCharsBootSequence, settingDocCapCharsBootSequence, &s.docCapCharsBootSequence},
		{body.DocCapCharsOffboard, settingDocCapCharsOffboard, &s.docCapCharsOffboard},
		{body.ChatBudgetChars, settingChatBudgetChars, &s.chatBudgetChars},
		{body.StepNoteCapChars, settingStepNoteCapChars, &s.stepNoteCapChars},
		{body.BackupRetain, settingBackupRetain, &s.backupRetain},
		{body.LoreCapCharsRole, settingLoreCapCharsRole, &s.loreCapCharsRole},
		{body.LoreCapCharsManual, settingLoreCapCharsManual, &s.loreCapCharsManual},
		{body.LoreCapCharsTitle, settingLoreCapCharsTitle, &s.loreCapCharsTitle},
		{body.LoreCapCharsBody, settingLoreCapCharsBody, &s.loreCapCharsBody},
	}
	for _, c := range capWrite {
		if c.field == nil {
			continue
		}
		v, dst := *c.field, c.dst
		put(c.key, strconv.Itoa(v), func() { *dst = v })
	}
	updaterChanged := false
	if body.UpdaterReceiveBeta != nil && *body.UpdaterReceiveBeta != s.updaterReceiveBeta {
		v := *body.UpdaterReceiveBeta
		put(settingUpdaterReceiveBeta, strconv.FormatBool(v), func() { s.updaterReceiveBeta = v })
		updaterChanged = true
	}
	// auto_update needs no kick: auto_update.go reads the live snapshot each tick.
	if body.UpdaterAutoUpdate != nil && *body.UpdaterAutoUpdate != s.updaterAutoUpdate {
		v := *body.UpdaterAutoUpdate
		put(settingUpdaterAutoUpdate, strconv.FormatBool(v), func() { s.updaterAutoUpdate = v })
	}
	if body.OrgName != nil && orgName != s.orgName {
		put(settingOrgName, orgName, func() { s.orgName = orgName })
	}
	if body.OwnerName != nil && ownerName != s.ownerName {
		put(settingOwnerName, ownerName, func() { s.ownerName = ownerName })
	}
	if body.PushContactEmail != nil && pushContactEmail != s.pushContactEmail {
		put(settingPushContactEmail, pushContactEmail, func() { s.pushContactEmail = pushContactEmail })
	}
	// No "is the active theme still there?" check here: DELETE /api/themes/{id}
	// resets the active theme itself (api_themes.go); a second check would drift.
	themeChanged := themeProvided && displayTheme != s.displayTheme
	if themeChanged {
		put(settingDisplayTheme, displayTheme, func() { s.displayTheme = displayTheme })
	}
	if body.DisplayLanguage != nil && displayLanguage != s.displayLanguage {
		put(settingDisplayLanguage, displayLanguage, func() { s.displayLanguage = displayLanguage })
	}
	if body.DisplayWide != nil && *body.DisplayWide != s.displayWide {
		v := *body.DisplayWide
		put(settingDisplayWide, strconv.FormatBool(v), func() { s.displayWide = v })
	}
	if body.SuggestedRepliesReplyCard != nil {
		put(settingSuggestedRepliesReplyCard, encodeSuggestedReplies(suggestedRepliesReplyCard),
			func() { s.suggestedRepliesReplyCard = suggestedRepliesReplyCard })
	}
	if body.SuggestedRepliesTaskMessage != nil {
		put(settingSuggestedRepliesTaskMessage, encodeSuggestedReplies(suggestedRepliesTaskMessage),
			func() { s.suggestedRepliesTaskMessage = suggestedRepliesTaskMessage })
	}
	if body.SuggestedRepliesLoreMessage != nil {
		put(settingSuggestedRepliesLoreMessage, encodeSuggestedReplies(suggestedRepliesLoreMessage),
			func() { s.suggestedRepliesLoreMessage = suggestedRepliesLoreMessage })
	}
	err := s.dal.inTx(func(tx *writeTx) error {
		if themeChanged {
			ok, err := displayThemeExistsOn(tx, displayTheme)
			if err != nil {
				return err
			}
			if !ok {
				return refuseInTx(http.StatusUnprocessableEntity, displayThemeRefusal)
			}
		}
		for _, sw := range writes {
			if err := putSettingOn(tx, sw.key, sw.value); err != nil {
				return err
			}
		}
		// A dismissal with no `failed` banner behind it is a 409 — on a
		// still-running run that is what keeps this read-modify-write from
		// ERASING the verdict (setOnboardingDismissedOn). The refusal rolls the
		// whole patch back.
		if body.OnboardingDismissed != nil {
			if err := setOnboardingDismissedOn(tx, *body.OnboardingDismissed); err != nil {
				if errors.Is(err, errNoOnboardingBanner) {
					return refuseInTx(http.StatusConflict, err.Error())
				}
				return err
			}
		}
		return nil
	})
	if err != nil {
		unlockMu()
		writeTxError(w, err)
		return
	}
	s.applySettings(func() {
		for _, sw := range writes {
			sw.apply()
		}
	})
	unlockMu()
	if updaterChanged {
		s.kickUpdateCheck()
	}
	writeJSON(w, http.StatusOK, s.settingsView())
}

func (s *apiServer) settingsView() settingsDTO {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return settingsDTO{
		OwnerTokenTTL:                   s.ownerTokenTTL,
		AgentTokenTTL:                   s.agentTokenTTL,
		HandoverPct:                     s.ctxHigh.HandoverPct,
		NoticePct:                       s.ctxHigh.NoticePct,
		CodexCompactionThreshold:        s.codexCompactionThreshold,
		CodexNoticeRound:                s.codexNoticeRound,
		MonitoringRefreshSeconds:        s.monitoringRefreshSeconds,
		AcceleratedGraceSecs:            s.acceleratedGraceSecs,
		ReassignHandoverTimeoutSecs:     s.reassignHandoverTimeoutSecs,
		RuntimeLoginCheckIntervalSecs:   s.runtimeLoginCheckIntervalSecs,
		RuntimeLoginRecheckIntervalSecs: s.runtimeLoginRecheckIntervalSecs,
		WardenCredentialLifetimeSecs:    s.wardenCredLifetimeSecs,
		OutsourceMaxParallel:            s.outsourceMaxParallel,
		DocCapCharsDuty:                 s.docCapCharsDuty,
		DocCapCharsInsight:              s.docCapCharsInsight,
		DocCapCharsManualSop:            s.docCapCharsManualSop,
		DocCapCharsSystemInteraction:    s.docCapCharsSystemInteraction,
		DocCapCharsBootSequence:         s.docCapCharsBootSequence,
		DocCapCharsOffboard:             s.docCapCharsOffboard,
		LoreCapCharsRole:                s.loreCapCharsRole,
		LoreCapCharsManual:              s.loreCapCharsManual,
		LoreCapCharsTitle:               s.loreCapCharsTitle,
		LoreCapCharsBody:                s.loreCapCharsBody,
		ChatBudgetChars:                 s.chatBudgetChars,
		StepNoteCapChars:                s.stepNoteCapChars,
		BackupRetain:                    s.backupRetain,
		UpdaterReceiveBeta:              s.updaterReceiveBeta,
		UpdaterAutoUpdate:               s.updaterAutoUpdate,
		OrgName:                         s.orgName,
		OwnerName:                       s.ownerName,
		PushContactEmail:                s.pushContactEmail,
		DisplayTheme:                    s.displayTheme,
		DisplayLanguage:                 s.displayLanguage,
		DisplayWide:                     s.displayWide,
		// Never null on the wire (spec types these as arrays); copied so no response
		// shares a slice with the live snapshot.
		SuggestedRepliesReplyCard:   append([]string{}, s.suggestedRepliesReplyCard...),
		SuggestedRepliesTaskMessage: append([]string{}, s.suggestedRepliesTaskMessage...),
		SuggestedRepliesLoreMessage: append([]string{}, s.suggestedRepliesLoreMessage...),

		Onboarding: s.onboardingReport(),
	}
}
