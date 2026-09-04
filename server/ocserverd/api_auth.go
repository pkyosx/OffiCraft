package main

// api_auth.go — the credential seams (handlers.handle_login / handle_mint /
// handle_bootstrap): the ONE public business entry (login), the owner-gated
// long-lived agent mint, and the agent boot seam (context fold + member JWT).

import (
	"fmt"
	"net/http"
	"time"
)

// maxAgentTTLSecs caps every long-lived agent token
// (service.config.MAX_AGENT_TTL_SECS — 400 days).
const maxAgentTTLSecs int64 = 400 * 86400

// invalidCredentialsMsg is the ONE refusal text /api/login answers with, for
// every cause. It names both factors so the cockpit can point the owner at the
// right field without the server disclosing which half actually failed.
const invalidCredentialsMsg = "invalid password or code"

// mintAgentToken is the ONE agent-scope boot-JWT mint under both spawn paths:
// scope="agent", sub=the member/worker id, machine_id = the boot host claim
// (omitted when empty). Members bind their durable desired machine; workers
// bind the warden actually picked at dispatch time.
func (s *apiServer) mintAgentToken(sub, machineID string, ttl int64) (string, error) {
	return mintJWT(sub, "agent", ttl, s.keys.signingSecret(), time.Now().Unix(), machineID)
}

// mintMemberToken mints a member's boot JWT (service.boot.mint_member_token):
// machine_id = desired_machine_id.
func (s *apiServer) mintMemberToken(m Member, ttl int64) (string, error) {
	return s.mintAgentToken(m.ID, m.DesiredMachineID, ttl)
}

// mintWardenToken mints the permanent machine credential used only by warden
// installation paths. It intentionally cannot accept an arbitrary member: a
// permanent token for an agent or outsource worker would bypass their TTL and
// the 400-day ceiling.
func (s *apiServer) mintWardenToken(m Member) (string, error) {
	if m.Kind != machineKind {
		return "", fmt.Errorf("%w: permanent credentials are warden-only", errInvalidToken)
	}
	return mintJWTWithoutExpiry(m.ID, "agent", s.keys.signingSecret(), time.Now().Unix(), "")
}

// POST /api/login — exchange the owner password (and, once enrolled, a TOTP
// code) for an owner-scoped JWT. Verified ONLY against the DB-stored argon2id
// hash (settings.go); the B1 oc.toml plaintext fallback is gone (B2).
//
// EVERY refusal on this route is the SAME flat 401 with the same message — no
// set password, wrong password, missing code and wrong code are indistinguishable
// (the first-run state is only ever disclosed by the B3 /api/auth/status
// endpoint, and `mfa_required` by the same one). Naming which factor failed
// would confirm a correct password to an attacker who has only guessed one
// half.
//
// 🔴 THE UX COST IS REAL AND ACCEPTED: an owner who fat-fingers the 6-digit
// code is told "invalid password or code", not "invalid code". The cockpit
// covers this by wording its inline error to name both fields, which is honest
// without the server disclosing anything.
//
// 🔴 AND EVERY ONE OF THEM COSTS THE SAME WALL-CLOCK, which is the other half
// of the same property. An identical SENTENCE served at a distinguishable SPEED
// discloses exactly what the sentence refuses to: 「密碼對、碼錯」 does one
// argon2id plus a TOTP verification while 「密碼錯」 stops after the argon2id.
// Every refusal below therefore waits until the instant stamped on the way in
// plus throttleFailureFloor (throttle.go). A SUCCESS does not wait at all.
func (s *apiServer) HandleLoginApiLoginPost(w http.ResponseWriter, r *http.Request) {
	// Stamped BEFORE any work, because the floor is a deadline measured from
	// here — not a sleep added to whatever the handler happened to spend.
	started := time.Now()
	var body LoginDTO
	if !decodeJSONBodyRequired(w, r, &body, "password") {
		return
	}
	// Server CONFIGURATION is settled before any credential work: a missing
	// signing secret is not a credential fact, so it must not take an in-flight
	// slot, must not burn a TOTP step, must not pay the refusal floor, and must
	// not be answered with the credential refusal. It used to sit after the whole
	// verification, which made it a distinguishable refusal reached THROUGH the
	// credential path; settling it here makes it a fact about the SERVER, which
	// GET /api/auth/status already tells anyone who asks.
	if len(s.keys.signingSecret()) == 0 {
		writeError(w, http.StatusUnauthorized, "auth not configured")
		return
	}
	// The brake sits BEFORE argon2id on purpose: at ~19 MiB and ~16-18 ms a
	// verification (measured on one Darwin box — the time is hardware-specific,
	// the memory is a parameter in password.go), the hash is itself the cheapest
	// denial-of-service on this server. begin RESERVES an in-flight slot, which is what stops a concurrent
	// burst running N argon2id verifications at once — and it is also what turns
	// the floor below from a per-request latency into a rate limit, because the
	// slot is held for the whole of that wait.
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
	// Second factor, when one is armed. verifyAndSpendTOTP is a no-op that
	// answers true while MFA is off, so this is the whole branch.
	code := ""
	if body.Code != nil {
		code = *body.Code
	}
	factorOK, err := s.verifyAndSpendTOTP(code, time.Now().Unix())
	if err != nil {
		// The floor could not be persisted, so the code was not really spent.
		// Failing closed here keeps a code from being replayable across the
		// restart that a storage fault tends to be followed by.
		internalError(w, err)
		return
	}
	if !factorOK {
		// 🔴 THE PASSWORD WAS RIGHT. This is the one refusal on this route that
		// is evidence of something rather than of nothing: whoever sent it holds
		// the owner's password and only lacks the phone. No throttle repairs a
		// leaked password, so the answer is to TELL SOMEONE — the assistant, who
		// asks the owner to change it. Dispatched asynchronously and counted, so
		// neither the DB write nor a flood of them can be felt from outside; see
		// noteFactorRefusedAfterCorrectPassword.
		s.noteFactorRefusedAfterCorrectPassword(time.Now())
		s.holdFailureFloor(started)
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}
	ttl := s.ownerTokenTTLValue()
	token, err := mintJWT(wireOwnerID, "owner", ttl, s.keys.signingSecret(), time.Now().Unix(), "")
	if err != nil {
		// A mint failure is a SERVER fault, not a credential one, so it spends no
		// floor and raises no alert: nothing was guessed wrong here. The TOTP
		// step is already spent either way (it had to be, to be single-use), so
		// the owner waits for the next tick — unavoidable, and the 500 says so
		// rather than pretending the credentials were the problem.
		internalError(w, err)
		return
	}
	// Only now is the credential PROVEN all the way to a usable token — and this
	// return spends NO floor. The owner who knows their password never waits;
	// the whole cost of this brake falls on people who get it wrong.
	writeJSON(w, http.StatusOK, tokenDTO{
		Token:     token,
		TokenType: "bearer",
		ExpiresIn: ttl,
		OwnerID:   wireOwnerID,
	})
}

// POST /api/mint — owner-gated (route table requires="owner") mint of a
// long-lived AGENT token for an existing member; ttl capped at 400 days.
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
	// The mint here deliberately carries NO machine_id claim (lifecycle.md
	// §1.3 mint table: /api/mint — machine_id "none").
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

// POST /api/bootstrap — assemble an agent's boot package (admin-gated on the
// route table). With member_id (a warden spawn) the response carries a fresh
// member JWT; a UI preview (no member_id) gets token: null (lifecycle.md §2.3).
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
	boot, err := s.buildBootContext(strOrEmpty(body.Role), member)
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
	// 🔴 ONLY A WARDEN SPAWN IS A SURFACING (T-33). member == nil is the UI
	// preview this handler documents above: the owner looking at what a boot
	// document would say, with no agent behind it and no token minted. Filing a
	// recall row for that would put reads into the journal that nobody ever did,
	// and a journal padded with non-events cannot be used to argue that anything
	// is unused. Recorded after the response is written, because everything
	// before it can still answer 404 or 500 and hand over nothing.
	//
	// 🔴 THE CONDITION IS `token != nil`, NOT `member != nil`, AND THE DIFFERENCE
	// IS A REAL CASE, NOT A STYLE CHOICE. Minting needs BOTH a member and a
	// signing secret; a station running without a secret answers 200 with the
	// document and `token: null`, and NOBODY CAN BOOT ON THAT — there is no
	// credential to connect with. Keying the journal off `member != nil` filed
	// those as deliveries, which is exactly the non-event this comment was
	// written to forbid: the paragraph above already states the criterion as "no
	// token minted", and the code did not implement its own stated rule. Found in
	// review by Kyle, not by a test.
	if token != nil {
		s.recordLoreSurfacing(boot.Lore)
	}
}
