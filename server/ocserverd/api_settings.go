package main

// api_settings.go — the B3 owner-cockpit settings surface: the PUBLIC
// first-run probe + claim-token set-password, the owner-gated change-password,
// and the owner-adjustable settings (GET/PATCH /api/settings). Every write
// goes DB-first, then updates the live in-memory snapshot under settingsMu
// (api_stub.go) so a change is durable AND immediate — no restart, no
// per-request DB read on the hot paths.

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// minPasswordLen mirrors the spec's minLength on SetPasswordDTO.password /
// ChangePasswordDTO.new_password.
const minPasswordLen = 8

// tokenTTLWhitelist is the closed PATCH vocabulary for auth.token_ttl
// (12h / 24h / 7d / 30d — a whitelist, so a stray 0 can never lock every
// future login out; SettingsUpdateDTO contract).
var tokenTTLWhitelist = map[int]bool{
	43200:   true,
	86400:   true,
	604800:  true,
	2592000: true,
}

// handover_pct bounds: the warn band sits at 40 (ctx.warn_pct default) — a
// handover threshold below it would fire before the warning.
const (
	minHandoverPct = 40
	maxHandoverPct = 90
)

// outsource_max_parallel bounds: -1 = 無限 (unlimited — no global cap; the
// left-rail popover's 無限 button); 0 pauses outsource assignment entirely;
// 20 is the sanity ceiling for a FINITE cap (a single-owner studio never
// legitimately wants more).
const (
	minOutsourceParallel = -1
	maxOutsourceParallel = 20
)

// maxOrgNameLen caps the studio display name (org.name; T-d693) — a topbar
// label, not a document. Whitespace is trimmed; "" clears it back to the
// localized default. Counted in runes so CJK names get the full budget.
const maxOrgNameLen = 80

// maxOwnerNameLen caps the owner display nickname (owner.name; T-0b41) — a
// topbar pill label, not a document. Whitespace is trimmed; "" clears it back
// to the localized default. Counted in runes so CJK names get the full budget.
const maxOwnerNameLen = 80

// GET /api/auth/status — PUBLIC: the single first-run bit the UI branches on.
func (s *apiServer) HandleAuthStatusApiAuthStatusGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, authStatusDTO{PasswordSet: s.authPasswordHash() != ""})
}

// POST /api/auth/set-password — PUBLIC, gated by the one-shot claim token
// (lifecycle.md §1.3). Order of checks is contract: already set → 409 (the
// token is never consulted); claim mismatch → 401; then store the hash,
// consume the token, and log the caller straight in.
func (s *apiServer) HandleSetPasswordApiAuthSetPasswordPost(w http.ResponseWriter, r *http.Request) {
	var body SetPasswordDTO
	if !decodeJSONBodyRequired(w, r, &body, "password", "claim_token") {
		return
	}
	if len(body.Password) < minPasswordLen {
		writeError(w, http.StatusUnprocessableEntity, "password must be at least 8 characters")
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.passwordHash != "" {
		writeError(w, http.StatusConflict, "a password is already set")
		return
	}
	stored, err := s.dal.GetSetting(settingClaimToken)
	if err != nil {
		internalError(w, err)
		return
	}
	if stored == nil ||
		subtle.ConstantTimeCompare([]byte(*stored), []byte(body.ClaimToken)) != 1 {
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
	s.writeOwnerToken(w, s.tokenTTL, time.Now().Unix())
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
// signing secret never rotates here (B1 zero-invalidation).
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
	s.writeOwnerToken(w, s.tokenTTL, now)
}

// writeOwnerToken mints and writes the owner tokenDTO. Callers hold
// settingsMu (they pass the ttl they read under it) — the mint itself touches
// no guarded state.
func (s *apiServer) writeOwnerToken(w http.ResponseWriter, ttl, now int64) {
	if len(s.secret) == 0 {
		writeError(w, http.StatusUnauthorized, "auth not configured")
		return
	}
	token, err := mintJWT(wireOwnerID, "owner", ttl, s.secret, now, "")
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
// under settingsMu: token_ttl applies from the next login, handover_pct from
// the next context report.
func (s *apiServer) HandleUpdateSettingsApiSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var body SettingsUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.TokenTtl != nil && !tokenTTLWhitelist[*body.TokenTtl] {
		writeError(w, http.StatusUnprocessableEntity,
			"token_ttl must be one of 43200, 86400, 604800, 2592000 seconds")
		return
	}
	if body.HandoverPct != nil &&
		(*body.HandoverPct < minHandoverPct || *body.HandoverPct > maxHandoverPct) {
		writeError(w, http.StatusUnprocessableEntity, "handover_pct must be between 40 and 90")
		return
	}
	if body.OutsourceMaxParallel != nil &&
		(*body.OutsourceMaxParallel < minOutsourceParallel ||
			*body.OutsourceMaxParallel > maxOutsourceParallel) {
		writeError(w, http.StatusUnprocessableEntity,
			"outsource_max_parallel must be between -1 and 20 (-1 = unlimited)")
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
	// custom_themes (T-16a1 P2): replace the saved bundle set. Validated in full
	// (shape + token whitelist + concrete-colour grammar) BEFORE anything is
	// written — a bad bundle 422s and nothing is stored.
	customProvided := body.CustomThemes != nil
	var newCustomThemes []ThemeBundleDTO
	if customProvided {
		newCustomThemes = *body.CustomThemes
		if err := validateThemeBundles(newCustomThemes); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	} else {
		newCustomThemes = s.displayCustomThemesSnapshot()
	}
	// display.theme is now validated against "" | built-in | an id in the
	// POST-patch custom set (so a theme + its bundle can land in one PATCH).
	customIDs := themeBundleIDSet(newCustomThemes)
	var displayTheme string
	themeProvided := body.DisplayTheme != nil
	if themeProvided {
		displayTheme = strings.TrimSpace(*body.DisplayTheme)
		if !isValidDisplayTheme(displayTheme, customIDs) {
			writeError(w, http.StatusUnprocessableEntity,
				`display_theme must be "", office, xian, or an existing custom theme id`)
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
	if body.TokenTtl != nil {
		if err := s.dal.PutSetting(settingTokenTTL, strconv.Itoa(*body.TokenTtl)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.tokenTTL = int64(*body.TokenTtl)
	}
	if body.HandoverPct != nil {
		if err := s.dal.PutSetting(settingCtxHandoverPct, strconv.Itoa(*body.HandoverPct)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.ctxhigh.HandoverPct = *body.HandoverPct
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
	// custom_themes + display.theme are coupled: replacing the bundle set can
	// orphan the active theme, so write the set first, then resolve the theme
	// against the POST-patch set (an explicit theme wins; otherwise a now-dangling
	// active custom theme is reset to "" — §4 simpler branch, server-side reset).
	if customProvided {
		marshaled, err := json.Marshal(newCustomThemes)
		if err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		if err := s.dal.PutSetting(settingDisplayCustomThemes, string(marshaled)); err != nil {
			s.settingsMu.Unlock()
			internalError(w, err)
			return
		}
		s.displayCustomThemes = newCustomThemes
	}
	effectiveIDs := themeBundleIDSet(s.displayCustomThemes)
	finalTheme := s.displayTheme
	if themeProvided {
		finalTheme = displayTheme
	} else if !isValidDisplayTheme(s.displayTheme, effectiveIDs) {
		finalTheme = "" // the active custom theme was deleted by this patch
	}
	if finalTheme != s.displayTheme {
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
	s.settingsMu.Unlock()
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
	// custom_themes always serialises as an array, never null (the wire shape).
	customThemes := s.displayCustomThemes
	if customThemes == nil {
		customThemes = []ThemeBundleDTO{}
	}
	return settingsDTO{
		TokenTTL:             s.tokenTTL,
		HandoverPct:          s.ctxhigh.HandoverPct,
		OutsourceMaxParallel: s.outsourceMaxParallel,
		UpdaterReceiveBeta:   s.updaterReceiveBeta,
		UpdaterAutoUpdate:    s.updaterAutoUpdate,
		OrgName:              s.orgName,
		OwnerName:            s.ownerName,
		DisplayTheme:         s.displayTheme,
		DisplayLanguage:      s.displayLanguage,
		CustomThemes:         customThemes,
		// Read from the DAL, NOT from the settings snapshot: onboarding runs in
		// its own goroutine and finishes after this handler returned, so a
		// boot-time snapshot would serve a permanently stale "running".
		Onboarding: s.onboardingReport(),
	}
}
