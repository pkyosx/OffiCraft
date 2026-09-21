package main

// api_auth_mfa.go — the owner's TOTP second factor: the enrol → activate
// ceremony, the disarm seam, and the one verification entry point login uses.
//
// The algorithm itself is totp.go; this file is the STATE MACHINE around it,
// and the shape of that machine is the security property:
//
//	                   +--- enroll (replaces the unproven one) ---+
//	                   |                                          |
//	                   v                                          |
//	  (none) --enroll--> pending --activate(password + code)--> ACTIVE
//	                                                              |
//	  (none) <---------- disable(password + live code) -----------+
//
// There is deliberately NO edge from ACTIVE back to pending: enroll against an
// armed factor is refused (409). The replace edge is pending→pending only. The
// previous drawing ran that arrow up from ACTIVE, which is the one transition
// this file exists to forbid — and a diagram is read literally.
//
// Two rules hold it together. A secret NEVER becomes active without the owner
// producing a working code from it — otherwise a mis-scanned QR locks the owner
// out of their own server on the next login, with no way back but host shell
// access. And an ACTIVE factor is never replaced in place — rotating means
// disarming first, which requires proving the current one.

import (
	"net/http"
	"strconv"
	"time"
)

// mfaIssuer / mfaAccount label the entry in the owner's authenticator app.
// They are cosmetic to the protocol and load-bearing to the human: an app
// listing three unlabelled "OffiCraft" rows is how someone deletes the wrong
// one. The org and owner names are used when set, so the entry reads like the
// studio it belongs to.
//
// They go through the EXISTING snapshot accessors rather than taking settingsMu
// themselves: every other reader of these two fields does, and a hand-rolled
// RLock here would put a settingsMu reader outside the one section of
// api_stub.go that collects them, where the next person auditing lock
// discipline would not find it.
func (s *apiServer) mfaIssuer() string {
	if name := s.orgNameSnapshot(); name != "" {
		return name
	}
	return "OffiCraft"
}

func (s *apiServer) mfaAccount() string {
	if name := s.ownerNameSnapshot(); name != "" {
		return name
	}
	return wireOwnerID
}

// verifyAndSpendTOTP is THE second-factor check, and the ONLY place the replay
// floor moves. It answers true when the caller may proceed.
//
// 🔴 VERIFY AND SPEND ARE ONE CRITICAL SECTION, deliberately. A TOTP code stays
// cryptographically valid for the whole ~90-second acceptance window, so the
// floor is the only thing that makes it single-use. Reading the secret and floor
// under one lock and writing the new floor under another would leave a window
// where two concurrent logins presenting the SAME code both read the old floor
// and both succeed — the exact replay this defends against. Hence one write
// lock across the whole operation.
//
// MFA off is `true, nil`: there is no factor to check, so there is nothing to
// refuse. A `code` sent to a server with no enrolment is ignored, per the wire
// contract.
//
// A DB write failure is an ERROR, never a silent pass. Advancing the floor is
// part of accepting the code; if the floor cannot be persisted we have not
// really spent it, and passing anyway would leave the code replayable across a
// restart.
func (s *apiServer) verifyAndSpendTOTP(code string, now int64) (bool, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.totpSecret == "" {
		return true, nil
	}
	step, ok := totpVerify(s.totpSecret, code, now, s.totpLastStep)
	if !ok {
		return false, nil
	}
	if err := s.dal.PutSetting(settingTOTPLastStep, strconv.FormatInt(step, 10)); err != nil {
		return false, err
	}
	s.totpLastStep = step
	return true, nil
}

// mfaNotOfferedMsg is the refusal when the feature has not been rolled out.
// 403 (not 404): the route genuinely exists and the caller genuinely is the
// owner — what is missing is the rollout decision, and pretending the endpoint
// is absent would send whoever hits it hunting a deployment problem that is not
// there.
const mfaNotOfferedMsg = "the second factor is not enabled on this server"

// GET /api/auth/mfa — owner-gated. The cockpit's read of the second-factor
// state, and the ONLY read of the feature flag on the wire.
//
// Its own owner-gated route rather than a field on GET /api/settings, because
// that route's floor is admin_agent and its GET is an MCP tool: the owner's
// credential posture is not something to hand every agent in the office.
// secret/otpauth_uri are always null — a secret is disclosed exactly once, by
// enroll.
func (s *apiServer) HandleMfaStateApiAuthMfaGet(w http.ResponseWriter, r *http.Request) {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	writeJSON(w, http.StatusOK, mfaStateDTO{
		Offered:  s.mfaOffered,
		Enrolled: s.totpSecret != "",
	})
}

// POST /api/auth/mfa/offer — owner-gated. Flips the ship-dark feature flag.
//
// 🔴 A ROLLOUT SWITCH, NOT A SECURITY SWITCH. Turning it off is allowed while a
// factor is armed and deliberately changes NOTHING about login: the code is
// still demanded, /api/auth/status still reports mfa_required, and disable still
// works. Letting it switch verification off would make the flag a bypass — a
// stolen owner token could withdraw the feature and walk straight past the
// factor that exists to stop it. Same argument as the both-factors rule on
// disable, applied to the flag.
//
// Not throttled: it compares no secret, so there is nothing here to guess.
func (s *apiServer) HandleMfaOfferApiAuthMfaOfferPost(w http.ResponseWriter, r *http.Request) {
	var body MfaOfferDTO
	if !decodeJSONBodyRequired(w, r, &body, "offered") {
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	// DB first, then the live snapshot — the ordering every settings write in
	// this package uses.
	if err := s.dal.PutSetting(settingMFAOffered, strconv.FormatBool(body.Offered)); err != nil {
		internalError(w, err)
		return
	}
	s.mfaOffered = body.Offered
	writeJSON(w, http.StatusOK, mfaStateDTO{
		Offered:  s.mfaOffered,
		Enrolled: s.totpSecret != "",
	})
}

// POST /api/auth/mfa/enroll — owner-gated. Mints a PENDING secret and returns
// it once. Does not arm anything.
func (s *apiServer) HandleMfaEnrollApiAuthMfaEnrollPost(w http.ResponseWriter, r *http.Request) {
	issuer, account := s.mfaIssuer(), s.mfaAccount()

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if !s.mfaOffered {
		writeError(w, http.StatusForbidden, mfaNotOfferedMsg)
		return
	}
	if s.totpSecret != "" {
		// Rotating an armed factor without proving the old one would make the
		// factor worth exactly as much as the session — which is what it is
		// supposed to outrank. Disable first.
		writeError(w, http.StatusConflict, "a second factor is already active; disable it first")
		return
	}
	secret, err := newTOTPSecret()
	if err != nil {
		internalError(w, err)
		return
	}
	// Overwrites any earlier unproven pending secret: an owner who lost the
	// enrolment screen just starts over, and the abandoned secret was never
	// usable for anything because it was never activated.
	if err := s.dal.PutSetting(settingTOTPPendingSecret, secret); err != nil {
		internalError(w, err)
		return
	}
	uri := totpEnrollmentURI(secret, issuer, account)
	writeJSON(w, http.StatusOK, mfaStateDTO{
		Offered:    true, // gated above, so reaching here means the feature is on
		Enrolled:   false,
		Secret:     &secret,
		OtpauthURI: &uri,
	})
}

// POST /api/auth/mfa/activate — owner-gated. Proves the pending secret and arms
// the factor.
//
// 🔴 IT DEMANDS THE PASSWORD, not just the owner token, and that is the whole
// point of this handler's shape. ARMING a factor is as destructive as removing
// one: a thief holding only a stolen owner token could otherwise enrol a secret
// THEY control and activate it, after which the real owner's password answers
// 401 and they cannot disarm it (disable needs a live code, which only the
// thief can produce). A transient token theft would become a durable lockout
// recoverable only from a host shell — strictly worse than the pre-MFA
// baseline, where a token thief could not lock the owner out at all. The
// symmetric argument is already written out on disable below; it applies here.
//
// Existing owner tokens are deliberately NOT revoked here. The session doing
// this has now proved BOTH factors, making it the most strongly authenticated
// session on the install; logging it out would be theatre, and it would also
// mean the owner's very first act after enabling MFA is an unexplained bounce
// to the login wall.
func (s *apiServer) HandleMfaActivateApiAuthMfaActivatePost(w http.ResponseWriter, r *http.Request) {
	var body MfaActivateDTO
	if !decodeJSONBodyRequired(w, r, &body, "password", "code") {
		return
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if !s.mfaOffered {
		writeError(w, http.StatusForbidden, mfaNotOfferedMsg)
		return
	}
	if s.totpSecret != "" {
		writeError(w, http.StatusConflict, "a second factor is already active")
		return
	}
	pending, err := s.dal.GetSetting(settingTOTPPendingSecret)
	if err != nil {
		internalError(w, err)
		return
	}
	if pending == nil || *pending == "" {
		writeError(w, http.StatusConflict, "no pending enrolment; call /api/auth/mfa/enroll first")
		return
	}
	// 🔴 NOTHING FROM loginThrottle IS CALLED HERE, by the owner's ruling
	// 「只有登入需要 throttling」. This seam is owner-gated, so a caller who
	// reaches it already holds a token — and while the gate was here it shared
	// /api/login's pool, which meant activity on THIS endpoint could answer the
	// owner's own login with a 429. See change-password (api_settings.go) for
	// the full reasoning and the accepted cost. Do not re-add a brake here
	// without a new owner ruling.
	//
	// (The two 409s above therefore no longer have anything to be ordered
	// against, but they stay where they are: a path that consults no credential
	// should decide before one is read, whatever else changes.)

	// BOTH factors, ONE indistinguishable refusal — naming which half failed
	// would confirm a correct password to someone who holds only a session.
	// The floor starts at 0 (this secret has never been used) and the step that
	// proves it is spent immediately, so the activation code itself can never be
	// replayed as the first login.
	passwordOK := s.passwordHash != "" && verifyPassword(body.Password, s.passwordHash)
	step, codeOK := totpVerify(*pending, body.Code, time.Now().Unix(), 0)
	if !passwordOK || !codeOK {
		// The pending secret survives on purpose: the overwhelmingly likely cause
		// is a stale code or a typo, and forcing a fresh QR scan for that would
		// train owners to abandon the ceremony half-done.
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}

	// 🔴 THE FLOOR IS WRITTEN BEFORE THE SECRET, and the order is the fix for a
	// real hazard rather than a style choice. There is no transaction across
	// settings writes, so any of these three can fail alone. Writing the secret
	// first leaves the one state that is genuinely dangerous: armed in the DB
	// with NO floor, so after a restart loadAuthSettings reads floor 0 and the
	// activation code the owner just typed is replayable as a login for the rest
	// of its window. Floor-first fails safe instead: a floor with no secret is
	// MFA still off, which is inert.
	//
	// Memory is updated only after ALL of them land, so a partial write can
	// never leave the live snapshot claiming a factor the DB does not have.
	if err := s.dal.PutSetting(settingTOTPLastStep, strconv.FormatInt(step, 10)); err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.PutSetting(settingTOTPSecret, *pending); err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.DeleteSetting(settingTOTPPendingSecret); err != nil {
		internalError(w, err)
		return
	}
	s.totpSecret = *pending
	s.totpLastStep = step

	writeJSON(w, http.StatusOK, mfaStateDTO{Offered: true, Enrolled: true})
}

// POST /api/auth/mfa/disable — owner-gated, and additionally requires BOTH the
// current password and a live code.
//
// 🔴 THE OWNER TOKEN IS NOT ENOUGH, and that is the entire point. A second
// factor that a stolen session can switch off protects nothing once the session
// is stolen — it would be a lock whose key is kept in the lock.
//
// This is therefore NOT the lost-phone recovery path. An owner who cannot
// produce a code cannot use this endpoint at all; recovery is `ocserverd
// mfa-disable` on the host, which substitutes proof of SHELL ACCESS for proof
// of the factor — the same trust substitution the first-run claim token
// already makes, not a new backdoor.
// 🔴 DELIBERATELY NOT GATED ON mfaOffered. Withdrawing the feature must never
// strand an owner with a factor they can no longer remove through the product:
// the flag decides whether one may be SET UP, and taking the off-switch away
// alongside the on-switch is the opposite of a rollout knob.
func (s *apiServer) HandleMfaDisableApiAuthMfaDisablePost(w http.ResponseWriter, r *http.Request) {
	var body MfaDisableDTO
	if !decodeJSONBodyRequired(w, r, &body, "password", "code") {
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.totpSecret == "" {
		// Nothing to disarm — decided before any credential is read, because
		// this path consults none. (It used to also have to sit before the
		// brake, or a documented 409 became a 429 — the bug measured on
		// set-password once, where that ordering contract is still live.)
		writeError(w, http.StatusConflict, "no second factor is active")
		return
	}
	// No brake of any kind — same owner ruling as activate above
	// (「只有登入需要 throttling」). Do not re-add one without a new ruling.

	// BOTH factors, and a single indistinguishable refusal for either — a
	// message naming which half failed would confirm a correct password to
	// someone who only holds a session.
	passwordOK := s.passwordHash != "" && verifyPassword(body.Password, s.passwordHash)
	step, codeOK := totpVerify(s.totpSecret, body.Code, time.Now().Unix(), s.totpLastStep)
	if !passwordOK || !codeOK {
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}
	_ = step // the secret is about to be destroyed; there is no floor left to advance

	// 🔴 THE ACTIVE SECRET IS DELETED LAST, for the same no-transaction reason
	// activate writes its floor first. Deleting the secret first and then failing
	// leaves the DB disarmed while memory still says armed: the owner is told the
	// disable FAILED, and yet a restart would silently disable it — the state and
	// the receipt disagree. Secret-last fails the other way: everything still
	// armed in both places, matching the error the owner was handed.
	//
	// Memory is updated only after all three land.
	for _, key := range []string{settingTOTPPendingSecret, settingTOTPLastStep, settingTOTPSecret} {
		if err := s.dal.DeleteSetting(key); err != nil {
			internalError(w, err)
			return
		}
	}
	s.totpSecret = ""
	s.totpLastStep = 0

	writeJSON(w, http.StatusOK, mfaStateDTO{Offered: s.mfaOffered, Enrolled: false})
}
