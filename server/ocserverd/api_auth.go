package main

import (
	"fmt"
	"net/http"
	"time"
)

const maxAgentTTLSecs int64 = 400 * 86400

// The ONE refusal text for every login failure cause: naming which factor
// failed would confirm a correct password to someone who guessed only one half.
const invalidCredentialsMsg = "invalid password or code"

// The one agent boot-JWT mint for both spawn paths: members bind their durable
// desired machine, workers bind the warden picked at dispatch time.
func (s *apiServer) mintAgentToken(sub, machineID string, ttl int64) (string, error) {
	return mintJWT(sub, "agent", ttl, s.keys.signingSecret(), time.Now().Unix(), machineID)
}

func (s *apiServer) mintMemberToken(m Member, ttl int64) (string, error) {
	return s.mintAgentToken(m.ID, m.DesiredMachineID, ttl)
}

// Machine-only: a machine credential is exempt from the §1.2 cut-3 agent iat
// floor, so handing one to an agent or worker would give it a credential no
// boot report can end.
//
// 🔴 The TTL lives here, not at the five callers (onboard, boot-command, claim,
// bootstrap-here, renew-credential), so they all mint the same shape.
//
// 🔴 The credential carries an `exp` (owner 2026-09-06). That is safe only while
// every warden renews at two thirds of this lifetime — see the failure
// conditions at wardenCredLifetimeSecsDefault (settings.go).
func (s *apiServer) mintWardenToken(m Member) (string, error) {
	if m.Kind != machineKind {
		return "", fmt.Errorf("%w: machine credentials are warden-only", errInvalidToken)
	}
	return mintJWT(m.ID, "agent", int64(s.wardenCredLifetimeValue()),
		s.keys.signingSecret(), time.Now().Unix(), "")
}

// 🔴 Every refusal also costs the same wall-clock: each waits until the instant
// stamped on the way in plus throttleFailureFloor (throttle.go), because
// 「密碼對、碼錯」 does argon2id plus a TOTP check while 「密碼錯」 stops after
// argon2id. A success does not wait.
func (s *apiServer) HandleLoginApiLoginPost(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	var body LoginDTO
	if !decodeJSONBodyRequired(w, r, &body, "password") {
		return
	}
	// A missing signing secret is a SERVER fact (GET /api/auth/status already
	// tells anyone), so it is settled before any credential work: no throttle
	// slot, no TOTP step burned, no floor, not the credential refusal.
	if len(s.keys.signingSecret()) == 0 {
		writeError(w, http.StatusUnauthorized, "auth not configured")
		return
	}
	// The brake sits BEFORE argon2id on purpose: at ~19 MiB and ~16-18 ms (one
	// Darwin box) per verification the hash is itself the cheapest DoS here. The
	// slot is held through the refusal floor, which makes the floor a rate limit.
	release, wait, blocked := s.loginThrottle.begin()
	if blocked {
		writeThrottled(w, wait)
		return
	}
	defer release()

	hash := s.authPasswordHash()
	if hash == "" || !verifyPassword(body.Password, hash) {
		s.holdFailureFloor(started)
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}
	// verifyAndSpendTOTP answers true while MFA is off.
	code := ""
	if body.Code != nil {
		code = *body.Code
	}
	factorOK, err := s.verifyAndSpendTOTP(code, time.Now().Unix())
	if err != nil {
		// The spent-step floor was not persisted: fail closed so the code cannot
		// be replayed.
		internalError(w, err)
		return
	}
	if !factorOK {
		s.noteFactorRefusedAfterCorrectPassword(time.Now())
		s.holdFailureFloor(started)
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}
	ttl := s.ownerTokenTTLValue()
	token, err := mintJWT(wireOwnerID, "owner", ttl, s.keys.signingSecret(), time.Now().Unix(), "")
	if err != nil {
		// Server fault: no floor, no alert. The TOTP step is already spent, so
		// the owner waits for the next one.
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

// Owner-gated in the route table.
func (s *apiServer) HandleMintApiMintPost(w http.ResponseWriter, r *http.Request) {
	var body MintRequestDTO
	if !decodeJSONBodyRequired(w, r, &body, "member_id", "ttl_days") {
		return
	}
	m, err := s.resolveMember(body.MemberId, staffOnly)
	if err != nil {
		writeResolveError(w, err, "member", body.MemberId)
		return
	}
	ttl := int64(body.TtlDays) * 86400
	if ttl > maxAgentTTLSecs {
		ttl = maxAgentTTLSecs
	}
	// Deliberately NO machine_id claim (lifecycle.md §1.3 mint table).
	token, err := mintJWT(m.ID, "agent", ttl, s.keys.signingSecret(), time.Now().Unix(), "")
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenDTO{
		Token:     token,
		TokenType: "bearer",
		ExpiresIn: ttl,
		OwnerID:   m.ID,
	})
}

// Admin-gated in the route table. A UI preview (no member_id) gets token: null
// (lifecycle.md §2.3).
func (s *apiServer) HandleBootstrapApiBootstrapPost(w http.ResponseWriter, r *http.Request) {
	var body BootstrapRequestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	var member *Member
	if body.MemberId != nil {
		m, err := s.resolveMember(*body.MemberId, staffOnly)
		if err != nil {
			writeResolveError(w, err, "member", *body.MemberId)
			return
		}
		member = m
	}
	previewMember := member
	if previewMember == nil {
		selected, err := s.selectUniqueActiveStaffForRolePreview(strOrEmpty(body.Role))
		if err != nil {
			internalError(w, err)
			return
		}
		previewMember = selected
	}
	boot, err := s.buildBootContext(strOrEmpty(body.Role), previewMember)
	if err != nil {
		internalError(w, err)
		return
	}
	if boot == nil {
		roleKey := resolveBootRoleKey(strOrEmpty(body.Role), member)
		writeError(w, http.StatusNotFound, "role '"+roleKey+"' not found")
		return
	}
	var token *string
	if member != nil && len(s.keys.signingSecret()) > 0 {
		minted, err := s.mintMemberToken(*member, s.agentTokenTTLValue())
		if err != nil {
			internalError(w, err)
			return
		}
		token = &minted
	}
	writeJSON(w, http.StatusOK, bootstrapDTO{
		Role:    boot.RoleKey,
		Name:    boot.Name,
		Context: boot.Context,
		Token:   token,
	})
}

// 🔴 It repeats rather than firing once because the warden's "asked to renew"
// flag is PROCESS-LOCAL and dies with a warden restart. Five minutes: a renew
// has no receipt (owner ruling A), so the only evidence is this machine's next
// request on the new key, and a live warden heartbeats every 30s.
const renewAskInterval = 5 * time.Minute

// 🔴 ONLY MACHINES ARE MEMOISED, AND THAT IS A BOUND: every outsource ticket
// mints a new `ow-…` identity, so memoising workers would grow the map forever.
type tokenKeyObservation struct {
	keyID string

	renewAskedAt time.Time
}

// 🔴 keyID is this station's own observation (the key whose HMAC matched in
// verifyJWTAnyKey); there must never be a channel through which a machine can
// assert which key it holds, because the answer gates an immediate revocation.
//
// 🔴 Exactly one non-test call site: api_infra.go's SSE handler after
// hub.Connect, deliberately not requireAuth — on a gated route a warden could
// mint a fresh credential via renew-credential, present it once, and be
// recorded as converged.
//
// 🔑 The memo is the ONE suppression of repeat writes (it absorbs reconnect
// storms); do not add a DB-side `m.TokenKeyID != keyID` re-check — that hid
// the memo from TestRepeatedRequestsOnAnUnchangedKeyCostNoFurtherWrites.
func (s *apiServer) noteTokenKeyObservation(claims map[string]any, keyID string) {
	if s == nil || s.dal == nil || keyID == "" {
		return
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return
	}
	s.tokenKeyObsMu.Lock()
	obs, ok := s.tokenKeyObs[sub]
	s.tokenKeyObsMu.Unlock()

	if !ok || obs.keyID != keyID {
		m, err := s.dal.GetMember(sub)
		if err != nil {
			return
		}
		if m == nil || m.Kind != machineKind {
			return
		}
		if err := s.dal.SetMemberTokenKeyID(sub, keyID); err != nil {
			return
		}
		obs = tokenKeyObservation{keyID: keyID}
		s.rememberTokenKey(sub, obs)
	}

	s.askMachineToRenewIfStale(sub, keyID)
}

// 🔴 The frame carries no token, key or material of any kind — only "go".
//
// 🔴 The queue is not the suppression state: PutWardenCommand is
// conflict-do-nothing and a pending row is deleted once written to the stream,
// so only the memo remembers whether this machine was asked.
func (s *apiServer) askMachineToRenewIfStale(machineID, keyID string) {
	if s.keys == nil || s.hub == nil {
		return
	}
	active := s.keys.activeKeyID()
	if active == "" || keyID == active {
		return
	}
	if !s.claimRenewAsk(machineID, keyID) {
		return
	}
	frame, ok := buildTargetFrame(reconcileCmdRenew, machineID)
	if !ok {
		return
	}
	// A warden's id IS its machine id, so target and subject are the same value.
	s.enqueueToWarden(machineID, machineID, frame)
}

func (s *apiServer) claimRenewAsk(machineID, keyID string) bool {
	now := s.keyRenewNow()
	s.tokenKeyObsMu.Lock()
	defer s.tokenKeyObsMu.Unlock()
	obs, ok := s.tokenKeyObs[machineID]
	if !ok || obs.keyID != keyID {
		return false
	}
	if !obs.renewAskedAt.IsZero() && now.Sub(obs.renewAskedAt) < renewAskInterval {
		return false
	}
	obs.renewAskedAt = now
	s.tokenKeyObs[machineID] = obs
	return true
}

func (s *apiServer) keyRenewNow() time.Time {
	if s.keyRenewClock != nil {
		return s.keyRenewClock()
	}
	return time.Now()
}

func (s *apiServer) rememberTokenKey(sub string, obs tokenKeyObservation) {
	s.tokenKeyObsMu.Lock()
	defer s.tokenKeyObsMu.Unlock()
	if s.tokenKeyObs == nil {
		s.tokenKeyObs = map[string]tokenKeyObservation{}
	}
	s.tokenKeyObs[sub] = obs
}
