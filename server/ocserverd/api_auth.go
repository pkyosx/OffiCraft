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
}

// ── T-80: which key is each machine's credential actually signed by ─────────

// renewAskInterval is how often the station will re-ask ONE machine to renew
// while it is still observed on an outgoing key.
//
// WHY THERE IS AN INTERVAL AT ALL. The observation below runs on every
// authenticated request, and a machine stuck on the old key keeps making them —
// without a floor, one stale machine would mint one frame per request.
//
// 🔴 WHY IT REPEATS RATHER THAN FIRING ONCE. The warden's "I have been asked to
// renew" flag is PROCESS-LOCAL: it dies with a warden restart, and nothing on
// that side remembers to ask again. A one-shot ask would therefore be silently
// lost by exactly the machines most likely to be misbehaving, and the station's
// count would sit at "still on the old key" forever with no further action.
//
// WHY FIVE MINUTES. It is bounded from BELOW by the settle loop: a renew is
// answered by nothing (no receipt — owner ruling A), so the only evidence it
// worked is this machine's NEXT authenticated request carrying the new key. A
// live warden heartbeats every 30s, so five minutes gives a renew about ten
// heartbeats to complete and be observed before the station concludes nothing
// happened and asks again — re-asking a machine that is already mid-renew is
// noise, not pressure. It is bounded from ABOVE by the restart case: a warden
// that reboots and forgets its flag waits at most this long to be reminded. The
// cost at this value is one frame per stale machine per five minutes, against a
// heartbeat that is already ten times more frequent.
const renewAskInterval = 5 * time.Minute

// tokenKeyObservation is one MACHINE's memo row.
//
// 🔴 ONLY MACHINES ARE MEMOISED, AND THAT IS A BOUND, NOT AN OPTIMISATION. The
// map is keyed by the token's `sub`. Machines are a roster: finite, small, and
// they keep their ids across restarts, so a memo keyed on them has a ceiling
// equal to the number of machines ever seen by this process. Agents and
// outsource workers do NOT: every outsource ticket mints a brand-new `ow-…`
// identity, and they pass through this same gate, so memoising them would make
// the map grow monotonically with the number of workers this station has ever
// run, with nothing ever reclaiming it. That is a slow leak with no upper bound
// and no owner — the kind that is invisible until a long-running station is
// already fat.
//
// THE PRICE IS REAL AND WORTH NAMING: a non-machine caller now pays one
// GetMember per authenticated request instead of a map read. That is a fourth
// read of a row requireAuth ALREADY reads up to three times on the same request
// (agentIatFloorRefusal, permanentCredentialRefusal, revocationRefusal), and it
// lands on the READ pool (8 connections), never the write pool. Paying a
// bounded, already-paid-for read to avoid an unbounded map is the right side of
// that trade. The alternative — an eviction policy — would be new machinery for
// a problem nobody has measured.
type tokenKeyObservation struct {
	// keyID is the signing-key id last RECORDED for this machine.
	keyID string
	// renewAskedAt is when a renew was last ASKED FOR while this machine was
	// observed on keyID; zero = never. It stamps the ATTEMPT, not the success:
	// an unreachable warden must not be retried (or log-spammed) once per
	// request either.
	renewAskedAt time.Time
}

// noteTokenKeyObservation records, against the identity that just
// authenticated, WHICH signing key verified its credential — and, when that key
// is no longer the one signing, asks that machine to go get a new credential.
//
// 🔴 THE VALUE IS THIS STATION'S OWN OBSERVATION, AND THE DESIGN LEANS ON THAT.
// keyID comes from verifyJWTAnyKey: the key whose HMAC actually matched. There
// is no claim, no header field and no heartbeat block through which a machine
// could tell us which key it holds, and there must not be one. The question this
// answers — "is every machine off the outgoing key, i.e. is it safe to press
// remove" — gates an IMMEDIATE, un-grace-periodded revocation, so an answer a
// machine could assert would be an answer a stale or hostile machine could
// assert. A machine that has not connected since the rotation simply keeps its
// old value, which reads honestly as "nothing has proved this one moved".
//
// 🔴 THERE IS EXACTLY ONE NON-TEST CALL SITE AND ITS LOCATION IS THE GUARANTEE.
// This is called from api_infra.go's SSE handler, after hub.Connect, and NOWHERE
// ELSE — deliberately not from requireAuth, where an earlier shape had it. A
// machine cannot choose the value, but it CAN choose which credential it
// presents: renew-credential is zero-argument self-service, so on a gated route
// a warden could mint a fresh credential, present it once, and be recorded as
// converged with nothing written to disk. Observing only the stream narrows
// "presented it" to "is running on it". api_signing_key_observation_reach_t80_
// _test.go asserts that call-site count; read its header before adding a second.
//
// 🔴 ONLY A CHANGE REACHES THE DATABASE, AND THE MEMO IS WHAT MAKES THAT TRUE —
// BUT NOT FOR THE REASON IT ORIGINALLY DID. When this ran on every gated request
// the memo stood between a bookkeeping column and a write pool ONE connection
// wide (server/CLAUDE.md §7). On the SSE path the traffic is far lower, so that
// is no longer what it is buying: what it now suppresses is the RECONNECT storm
// — a machine that drops and redials, a fleet coming back after a station
// restart — where the observed key is unchanged every time. Do not read the
// lower call volume as "the memo is now pointless" and delete it:
// TestRepeatedRequestsOnAnUnchangedKeyCostNoFurtherWrites dies without it, and
// that is the guard, not this comment. It stays process-local and lossy on
// purpose: a restart costs one redundant write per machine and nothing else.
//
// 🔑 THERE IS EXACTLY ONE SUPPRESSION, DELIBERATELY. An earlier shape had two —
// this memo AND a `if m.TokenKeyID != keyID` re-check before the write — and the
// second one made the first unguarded: removing the memo left every test green,
// because the DB comparison still absorbed the repeat. Two representations of
// "have we already recorded this" is the same-fact-twice shape this repo keeps
// getting bitten by, and here it hid which of them was load-bearing. The memo is
// now it, and TestRepeatedRequestsOnAnUnchangedKeyCostNoFurtherWrites dies
// without it.
//
// Only warden rows are stamped and only warden rows are summoned (Kind ==
// machineKind). Agents and outsource workers reach the same gate, but their
// credentials are short-lived and re-minted by the server itself, so they are
// never what stands between the owner and a removal. They take NO memo slot —
// see tokenKeyObservation for why that bound matters more than their hot path.
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
		// Cold memo, or the observed key CHANGED. This is the one and only path
		// that touches the database.
		m, err := s.dal.GetMember(sub)
		if err != nil {
			// A read failure is not evidence of anything. Leave the memo alone
			// so the next request retries, exactly as the roster-revocation seam
			// declines to treat a lookup error as a verdict (server/CLAUDE.md §2).
			return
		}
		if m == nil || m.Kind != machineKind {
			// Not a machine: nothing to record and nothing to summon — and
			// deliberately NOTHING MEMOISED either, so this map cannot grow with
			// the number of ephemeral worker identities. See the type's header.
			return
		}
		if err := s.dal.SetMemberTokenKeyID(sub, keyID); err != nil {
			// Do not memo a write that did not land, or the observation is lost
			// until the next rotation.
			return
		}
		// A changed key resets renewAskedAt to zero: the previous stamp was
		// about a DIFFERENT key, and a machine that has just moved deserves an
		// immediate ask if it landed somewhere that is still not current.
		obs = tokenKeyObservation{keyID: keyID}
		s.rememberTokenKey(sub, obs)
	}

	s.askMachineToRenewIfStale(sub, keyID)
}

// askMachineToRenewIfStale is the SUMMONS half (T-80, owner ruling A): when the
// credential this machine keeps presenting is signed by a key that is no longer
// the signing one, push the `renew` verb down its warden-command downlink.
//
// 🔴 THE STATION SAYS "GO", IT DOES NOT SEND ANYTHING. The frame is
// buildTargetFrame's member_id-only shape — no token, no key, no material of any
// kind. member_id is informational addressing; the frame already rides that
// machine's own connection.
//
// 🔴 THE QUEUE IS NOT THE SUPPRESSION STATE. PutWardenCommand is
// conflict-do-nothing and a pending row is DELETED when the frame is written to
// the stream, so "the queue is empty" says nothing about whether this machine
// has been asked. The memo is what remembers, and only the memo.
//
// A one-key ring can never reach the ask: the key that verified is by
// construction on the ring, so on a ring of one it IS the active key and the
// comparison below returns early. An install that has never rotated therefore
// summons nobody.
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
	// A warden's id IS its machine id, so the frame's target and its subject are
	// the same value. enqueueToWarden carries the usual fail-closed reachability
	// gate: an offline warden gets nothing, and will be asked again after the
	// interval — the ask is already stamped, so an unreachable machine costs one
	// attempt per interval rather than one per request.
	s.enqueueToWarden(machineID, machineID, frame)
}

// claimRenewAsk is the compare-and-stamp that makes the interval hold under
// concurrency: two requests arriving together must not both decide the interval
// has elapsed. Reports whether THIS caller won the right to ask.
func (s *apiServer) claimRenewAsk(machineID, keyID string) bool {
	now := s.keyRenewNow()
	s.tokenKeyObsMu.Lock()
	defer s.tokenKeyObsMu.Unlock()
	obs, ok := s.tokenKeyObs[machineID]
	if !ok || obs.keyID != keyID {
		// The memo moved underneath us; the request that moved it owns the
		// decision.
		return false
	}
	if !obs.renewAskedAt.IsZero() && now.Sub(obs.renewAskedAt) < renewAskInterval {
		return false
	}
	obs.renewAskedAt = now
	s.tokenKeyObs[machineID] = obs
	return true
}

// keyRenewNow reads the injectable clock; nil means the real one.
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
