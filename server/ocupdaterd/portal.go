package main

// portal.go — the management portal: a browser UI over what the local CLI
// subcommands already do (versions / promote / invites) plus the downstream
// fleet monitor (one invite == one downstream OC; /api/latest stamps its
// heartbeat, see server.go handleLatest).
//
// Face (all NEW routes — the published client API in server.go is untouched):
//
//	GET  /portal/                        the embedded single-file UI (portal.html)
//	GET  /portal/api/status              PUBLIC — {"password_set": bool} (the first-run bit)
//	POST /portal/api/set-password        PUBLIC, one-shot-claim-gated — first-run claim
//	POST /portal/api/login               PUBLIC — password → session token
//	GET  /portal/api/overview            session — releases + invites(fleet) + latest ga/beta
//	POST /portal/api/promote             session — {"version"}: stamp GA (forward-fix only)
//	POST /portal/api/invites             session — {"name"}: mint (code in response ONCE)
//	POST /portal/api/invites/{id}/revoke session — revoke one live invite
//
// Auth is the ocserverd first-run pattern, owner-designated as the template
// (server/ocserverd/api_settings.go + password.go): while no password is set,
// serve prints a one-shot claim URL to the LOCAL log; POST set-password is
// gated by that claim token (already-set → 409 before the token is ever
// consulted; mismatch → 401) and logs the caller straight in. After that,
// login exchanges the password (argon2id-verified, hash-only-in-DB) for a
// session token — random 32 bytes stored sha256-HASHED in portal_session
// (same never-store-plaintext posture as the credential table), expiring
// after portalSessionTTL. ocserverd's JWT plumbing is deliberately NOT
// copied: this daemon's credential model is hashed random tokens (auth.go),
// so sessions reuse that machinery instead of importing a second one.

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// minPasswordLen mirrors ocserverd's SetPasswordDTO minimum.
const minPasswordLen = 8

// portalSessionTTL bounds one portal login. 7 days: an ops portal visited
// every few days should not demand a password on every glance; the token is
// revocable by restarting with a wiped portal_session table if ever needed.
const portalSessionTTL = 7 * 24 * time.Hour

// Settings keys (setting table, store.go).
const (
	settingPortalPasswordHash = "portal.password_hash"
	settingPortalClaimToken   = "portal.claim_token"
)

//go:embed portal.html
var portalHTML []byte

func (s *updaterServer) registerPortal(mux *http.ServeMux) {
	mux.HandleFunc("GET /portal/{$}", s.handlePortalIndex)
	mux.HandleFunc("GET /portal/api/status", s.handlePortalStatus)
	mux.HandleFunc("POST /portal/api/set-password", s.handlePortalSetPassword)
	mux.HandleFunc("POST /portal/api/login", s.handlePortalLogin)
	mux.HandleFunc("GET /portal/api/overview", s.handlePortalOverview)
	mux.HandleFunc("POST /portal/api/promote", s.handlePortalPromote)
	mux.HandleFunc("POST /portal/api/invites", s.handlePortalMintInvite)
	mux.HandleFunc("POST /portal/api/invites/{id}/revoke", s.handlePortalRevokeInvite)
}

// portalSetupURL answers the serve-start banner: "" when the portal is already
// claimed (password set); otherwise the one-shot setup URL, minting and
// persisting the claim token on first need (an unclaimed portal across
// restarts keeps ONE stable claim token — the printed URL stays valid).
func portalSetupURL(store *Store, addr string) (string, error) {
	hash, err := store.GetSetting(settingPortalPasswordHash)
	if err != nil {
		return "", err
	}
	if hash != nil && *hash != "" {
		return "", nil
	}
	claim, err := store.GetSetting(settingPortalClaimToken)
	if err != nil {
		return "", err
	}
	if claim == nil || *claim == "" {
		minted, _, err := mintSecret("claim")
		if err != nil {
			return "", err
		}
		if err := store.PutSetting(settingPortalClaimToken, minted); err != nil {
			return "", err
		}
		claim = &minted
	}
	return fmt.Sprintf("http://%s/portal/?claim=%s", addr, *claim), nil
}

// ── plumbing ─────────────────────────────────────────────────────────────────

// decodePortalBody decodes a small JSON body (1MB cap). false = 400 written.
func decodePortalBody(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "the request body must be JSON: "+err.Error())
		return false
	}
	return true
}

// portalPasswordHash reads the stored portal password PHC ("" = first-run).
func (s *updaterServer) portalPasswordHash() (string, error) {
	v, err := s.store.GetSetting(settingPortalPasswordHash)
	if err != nil || v == nil {
		return "", err
	}
	return *v, nil
}

// requirePortalSession gates the session-only routes: Bearer session token,
// verified against the hashed portal_session rows. false = 401 written
// (unknown / expired / missing are ONE indistinguishable "no").
func (s *updaterServer) requirePortalSession(w http.ResponseWriter, r *http.Request) bool {
	presented := bearerToken(r)
	if presented != "" {
		live, err := s.store.LivePortalSession(hashSecret(presented))
		if err != nil {
			internalError(w, err)
			return false
		}
		if live {
			return true
		}
	}
	writeError(w, http.StatusUnauthorized, "portal session missing or expired — log in again")
	return false
}

// mintPortalSession creates a session and answers the token body (the shape
// both set-password and login share).
func (s *updaterServer) mintPortalSession(w http.ResponseWriter) {
	plaintext, secretHash, err := mintSecret("session")
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.store.InsertPortalSession(secretHash, portalSessionTTL); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      plaintext,
		"token_type": "bearer",
		"expires_in": int64(portalSessionTTL.Seconds()),
	})
}

// ── handlers ─────────────────────────────────────────────────────────────────

func (s *updaterServer) handlePortalIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(portalHTML)
}

// GET /portal/api/status — PUBLIC: the single first-run bit the UI branches on
// (ocserverd's GET /api/auth/status twin).
func (s *updaterServer) handlePortalStatus(w http.ResponseWriter, r *http.Request) {
	hash, err := s.portalPasswordHash()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"password_set": hash != ""})
}

// POST /portal/api/set-password — PUBLIC, gated by the one-shot claim token.
// Order of checks mirrors ocserverd's contract: length → already set (409, the
// token is never consulted) → claim mismatch (401) → store hash, consume the
// token, log the caller straight in.
func (s *updaterServer) handlePortalSetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password   string `json:"password"`
		ClaimToken string `json:"claim_token"`
	}
	if !decodePortalBody(w, r, &body) {
		return
	}
	if len(body.Password) < minPasswordLen {
		writeError(w, http.StatusUnprocessableEntity, "password must be at least 8 characters")
		return
	}
	hash, err := s.portalPasswordHash()
	if err != nil {
		internalError(w, err)
		return
	}
	if hash != "" {
		writeError(w, http.StatusConflict, "a password is already set")
		return
	}
	stored, err := s.store.GetSetting(settingPortalClaimToken)
	if err != nil {
		internalError(w, err)
		return
	}
	if stored == nil ||
		subtle.ConstantTimeCompare([]byte(*stored), []byte(body.ClaimToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid claim token — use the setup URL printed in the ocupdaterd serve log")
		return
	}
	phc, err := hashPassword(body.Password)
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.store.PutSetting(settingPortalPasswordHash, phc); err != nil {
		internalError(w, err)
		return
	}
	if err := s.store.DeleteSetting(settingPortalClaimToken); err != nil {
		internalError(w, err)
		return
	}
	s.mintPortalSession(w)
}

// POST /portal/api/login — exchange the portal password for a session token.
// A wrong password OR no set password is a flat 401 with no distinguishing
// hint (the first-run state is only disclosed by /portal/api/status).
func (s *updaterServer) handlePortalLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodePortalBody(w, r, &body) {
		return
	}
	hash, err := s.portalPasswordHash()
	if err != nil {
		internalError(w, err)
		return
	}
	if hash == "" || !verifyPassword(body.Password, hash) {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	s.mintPortalSession(w)
}

// GET /portal/api/overview — session — everything the dashboard renders in one
// read: the release list (newest first, with channel), the invite list with
// each downstream's fleet record, and the two per-channel latests so the UI
// can flag who is behind. latest_ga/latest_beta are null while that channel is
// empty (never a fabricated row).
func (s *updaterServer) handlePortalOverview(w http.ResponseWriter, r *http.Request) {
	if !s.requirePortalSession(w, r) {
		return
	}
	releases, err := s.store.ListReleases()
	if err != nil {
		internalError(w, err)
		return
	}
	invites, err := s.store.ListInvites()
	if err != nil {
		internalError(w, err)
		return
	}
	latestGA, err := s.store.LatestRelease(channelGA)
	if err != nil {
		internalError(w, err)
		return
	}
	latestBeta, err := s.store.LatestRelease(channelBeta)
	if err != nil {
		internalError(w, err)
		return
	}
	releaseDTOs := make([]map[string]any, 0, len(releases))
	for _, rel := range releases {
		releaseDTOs = append(releaseDTOs, releaseDTO(rel))
	}
	inviteDTOs := make([]map[string]any, 0, len(invites))
	for _, c := range invites {
		inviteDTOs = append(inviteDTOs, inviteDTO(c))
	}
	var gaDTO, betaDTO any
	if latestGA != nil {
		gaDTO = releaseDTO(*latestGA)
	}
	if latestBeta != nil {
		betaDTO = releaseDTO(*latestBeta)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"releases":    releaseDTOs,
		"invites":     inviteDTOs,
		"latest_ga":   gaDTO,
		"latest_beta": betaDTO,
	})
}

// inviteDTO is one invite row for the portal (fleet record included; the code
// itself is unrecoverable by design — only its hash exists).
func inviteDTO(c Credential) map[string]any {
	nullable := func(v *float64) any {
		if v == nil {
			return nil
		}
		return *v
	}
	return map[string]any{
		"id":            c.ID,
		"name":          c.Name,
		"created_at":    c.CreatedAt,
		"revoked_at":    nullable(c.RevokedAt),
		"last_check_at": nullable(c.LastCheckAt),
		"last_version":  c.LastVersion,
		"last_sha":      c.LastSHA,
		"last_channel":  c.LastChannel,
	}
}

// POST /portal/api/promote — session — the portal face of GA promotion; the
// exact semantics of POST /api/promote (idempotent, no demote, forward-fix
// only), just authenticated by the operator's session instead of a publish
// token.
func (s *updaterServer) handlePortalPromote(w http.ResponseWriter, r *http.Request) {
	if !s.requirePortalSession(w, r) {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if !decodePortalBody(w, r, &body) {
		return
	}
	version := strings.TrimSpace(body.Version)
	if version == "" {
		writeError(w, http.StatusBadRequest, `the promote body needs a version, e.g. {"version":"v260713-0002"}`)
		return
	}
	rel, err := s.store.PromoteRelease(version)
	if err != nil {
		internalError(w, err)
		return
	}
	if rel == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("version %q is not published here", version))
		return
	}
	writeJSON(w, http.StatusOK, releaseDTO(*rel))
}

// POST /portal/api/invites — session — mint one invite. The plaintext code is
// in THIS response only (the store keeps its hash; there is no re-read).
func (s *updaterServer) handlePortalMintInvite(w http.ResponseWriter, r *http.Request) {
	if !s.requirePortalSession(w, r) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodePortalBody(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusUnprocessableEntity, "name is required — one invite per person/machine, name it after who runs it")
		return
	}
	plaintext, secretHash, err := mintSecret(kindInvite)
	if err != nil {
		internalError(w, err)
		return
	}
	id, err := s.store.InsertCredential(kindInvite, name, secretHash)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":   id,
		"name": name,
		"code": plaintext, // shown once — never retrievable again
	})
}

// POST /portal/api/invites/{id}/revoke — session — revoke one live invite.
func (s *updaterServer) handlePortalRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if !s.requirePortalSession(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the invite id must be a number")
		return
	}
	ok, err := s.store.RevokeInvite(id)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no live invite has id %d (already revoked, or never existed)", id))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "revoked": true})
}
