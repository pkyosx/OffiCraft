package main

// api_auth.go — the credential seams (handlers.handle_login / handle_mint /
// handle_bootstrap): the ONE public business entry (login), the owner-gated
// long-lived agent mint, and the agent boot seam (context fold + member JWT).

import (
	"net/http"
	"time"
)

// maxAgentTTLSecs caps every long-lived agent token
// (service.config.MAX_AGENT_TTL_SECS — 400 days).
const maxAgentTTLSecs int64 = 400 * 86400

// mintAgentToken is the ONE agent-scope boot-JWT mint under both spawn paths:
// scope="agent", sub=the member/worker id, machine_id = the boot host claim
// (omitted when empty). Members bind their durable desired machine; workers
// bind the warden actually picked at dispatch time.
func (s *apiServer) mintAgentToken(sub, machineID string, ttl int64) (string, error) {
	return mintJWT(sub, "agent", ttl, s.secret, time.Now().Unix(), machineID)
}

// mintMemberToken mints a member's boot JWT (service.boot.mint_member_token):
// machine_id = desired_machine_id.
func (s *apiServer) mintMemberToken(m Member, ttl int64) (string, error) {
	return s.mintAgentToken(m.ID, m.DesiredMachineID, ttl)
}

// POST /api/login — exchange the owner password for an owner-scoped JWT.
// Verified ONLY against the DB-stored argon2id hash (settings.go); the B1
// oc.toml plaintext fallback is gone (B2). A wrong password OR no set
// password is a flat 401 with no distinguishing hint (the first-run state is
// only ever disclosed by the B3 /api/auth/status endpoint).
func (s *apiServer) HandleLoginApiLoginPost(w http.ResponseWriter, r *http.Request) {
	var body LoginDTO
	if !decodeJSONBodyRequired(w, r, &body, "password") {
		return
	}
	hash := s.authPasswordHash()
	if hash == "" || !verifyPassword(body.Password, hash) {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	if len(s.secret) == 0 {
		writeError(w, http.StatusUnauthorized, "auth not configured")
		return
	}
	ttl := s.authTokenTTL()
	token, err := mintJWT(wireOwnerID, "owner", ttl, s.secret, time.Now().Unix(), "")
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

// POST /api/mint — owner-gated (route table requires="owner") mint of a
// long-lived AGENT token for an existing member; ttl capped at 400 days.
func (s *apiServer) HandleMintApiMintPost(w http.ResponseWriter, r *http.Request) {
	var body MintRequestDTO
	if !decodeJSONBodyRequired(w, r, &body, "member_id", "ttl_days") {
		return
	}
	m, err := s.resolveMember(body.MemberId)
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
	token, err := mintJWT(m.ID, "agent", ttl, s.secret, time.Now().Unix(), "")
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
		m, err := s.resolveMember(*body.MemberId)
		if err != nil {
			writeResolveError(w, err, "member", *body.MemberId)
			return
		}
		member = m
	}
	boot, err := s.buildBootContext(strOrEmpty(body.Role), member, strOrEmpty(body.TaskType))
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
	if member != nil && len(s.secret) > 0 {
		minted, err := s.mintMemberToken(*member, s.authTokenTTL())
		if err != nil {
			internalError(w, err)
			return
		}
		token = &minted
	}
	writeJSON(w, http.StatusOK, bootstrapDTO{
		Role:     boot.RoleKey,
		Name:     boot.Name,
		TaskType: boot.TaskType,
		Context:  boot.Context,
		Token:    token,
	})
}
