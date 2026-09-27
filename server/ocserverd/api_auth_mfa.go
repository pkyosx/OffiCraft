package main

// The owner's TOTP second factor (algorithm in totp.go). Two rules are the
// security property: a secret never arms without the owner producing a working
// code from it (a mis-scanned QR would lock the owner out, recoverable only from
// a host shell), and an ACTIVE factor is never replaced in place — rotating
// means disarming first, which requires proving the current one.

import (
	"net/http"
	"strconv"
	"time"
)

// Read through the existing snapshot accessors, not a hand-rolled settingsMu
// RLock: api_stub.go collects every settingsMu reader in one section for lock
// audits.
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

// 🔴 VERIFY AND SPEND ARE ONE CRITICAL SECTION. A code stays valid for the whole
// ~90-second window, so the floor is the only thing that makes it single-use;
// reading the floor under one lock and writing it under another lets two
// concurrent logins with the SAME code both succeed.
//
// A failed floor write is an ERROR, never a pass: an unpersisted floor leaves the
// code replayable across a restart.
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

// 403, not 404: the route exists and the caller is the owner; what is missing is
// the rollout decision.
const mfaNotOfferedMsg = "the second factor is not enabled on this server"

// Its own owner-gated route rather than a field on GET /api/settings: that
// route's floor is admin_agent and its GET is an MCP tool.
func (s *apiServer) HandleMfaStateApiAuthMfaGet(w http.ResponseWriter, r *http.Request) {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	writeJSON(w, http.StatusOK, mfaStateDTO{
		Offered:  s.mfaOffered,
		Enrolled: s.totpSecret != "",
	})
}

// 🔴 A ROLLOUT SWITCH, NOT A SECURITY SWITCH. Turning it off while a factor is
// armed changes nothing about login or disable; otherwise a stolen owner token
// could withdraw the feature and walk past the factor.
func (s *apiServer) HandleMfaOfferApiAuthMfaOfferPost(w http.ResponseWriter, r *http.Request) {
	var body MfaOfferDTO
	if !decodeJSONBodyRequired(w, r, &body, "offered") {
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
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

func (s *apiServer) HandleMfaEnrollApiAuthMfaEnrollPost(w http.ResponseWriter, r *http.Request) {
	issuer, account := s.mfaIssuer(), s.mfaAccount()

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if !s.mfaOffered {
		writeError(w, http.StatusForbidden, mfaNotOfferedMsg)
		return
	}
	if s.totpSecret != "" {
		writeError(w, http.StatusConflict, "a second factor is already active; disable it first")
		return
	}
	secret, err := newTOTPSecret()
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.dal.PutSetting(settingTOTPPendingSecret, secret); err != nil {
		internalError(w, err)
		return
	}
	uri := totpEnrollmentURI(secret, issuer, account)
	writeJSON(w, http.StatusOK, mfaStateDTO{
		Offered:    true,
		Enrolled:   false,
		Secret:     &secret,
		OtpauthURI: &uri,
	})
}

// 🔴 IT DEMANDS THE PASSWORD, not just the owner token: otherwise a token thief
// could arm a secret THEY control, and the owner could never disarm it (disable
// needs a live code) — a transient theft becomes a durable lockout.
//
// Existing owner tokens are deliberately NOT revoked: this session just proved
// both factors.
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
	// 🔴 No loginThrottle here — owner ruling「只有登入需要 throttling」. Do not
	// re-add a brake without a new owner ruling.

	// BOTH factors, ONE indistinguishable refusal — naming which half failed
	// would confirm a correct password to someone who holds only a session.
	// Floor 0 is right for a never-used secret; the proving step is spent below,
	// so the activation code cannot be replayed as the first login.
	passwordOK := s.passwordHash != "" && verifyPassword(body.Password, s.passwordHash)
	step, codeOK := totpVerify(*pending, body.Code, time.Now().Unix(), 0)
	if !passwordOK || !codeOK {
		// The pending secret survives on purpose: a stale code or typo must not
		// force a fresh QR scan.
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}

	// 🔴 The floor and the secret land together: a secret armed with no floor
	// makes the activation code replayable as a login after a restart. The
	// pending secret is read again here because it is the one being armed.
	err = s.dal.inTx(func(tx *writeTx) error {
		stillPending, err := getSettingOn(tx, settingTOTPPendingSecret)
		if err != nil {
			return err
		}
		if stillPending == nil || *stillPending != *pending {
			return refuseInTx(http.StatusConflict, "no pending enrolment; call /api/auth/mfa/enroll first")
		}
		if err := putSettingOn(tx, settingTOTPLastStep, strconv.FormatInt(step, 10)); err != nil {
			return err
		}
		if err := putSettingOn(tx, settingTOTPSecret, *pending); err != nil {
			return err
		}
		return deleteSettingOn(tx, settingTOTPPendingSecret)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	s.totpSecret = *pending
	s.totpLastStep = step

	writeJSON(w, http.StatusOK, mfaStateDTO{Offered: true, Enrolled: true})
}

// Not the lost-phone recovery path: an owner who cannot produce a code uses
// `ocserverd mfa-disable` on the host.
//
// 🔴 DELIBERATELY NOT GATED ON mfaOffered: withdrawing the feature must never
// strand an owner with a factor they cannot remove through the product.
func (s *apiServer) HandleMfaDisableApiAuthMfaDisablePost(w http.ResponseWriter, r *http.Request) {
	var body MfaDisableDTO
	if !decodeJSONBodyRequired(w, r, &body, "password", "code") {
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.totpSecret == "" {
		writeError(w, http.StatusConflict, "no second factor is active")
		return
	}
	// No brake — same owner ruling as activate.

	passwordOK := s.passwordHash != "" && verifyPassword(body.Password, s.passwordHash)
	step, codeOK := totpVerify(s.totpSecret, body.Code, time.Now().Unix(), s.totpLastStep)
	if !passwordOK || !codeOK {
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}
	_ = step

	// 🔴 One transaction: a partial delete would leave the DB disarmed while the
	// owner is told disable FAILED, and a restart would silently disable it.
	if err := s.dal.inTx(func(tx *writeTx) error {
		for _, key := range []string{settingTOTPPendingSecret, settingTOTPLastStep, settingTOTPSecret} {
			if err := deleteSettingOn(tx, key); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		internalError(w, err)
		return
	}
	s.totpSecret = ""
	s.totpLastStep = 0

	writeJSON(w, http.StatusOK, mfaStateDTO{Offered: s.mfaOffered, Enrolled: false})
}
