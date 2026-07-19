package main

// api_helpers.go — the shared handler plumbing (M3 REST sub-batch B): request
// identity accessors (the deps.py twins), JSON body decoding with the
// wire-frozen 422/400 split, target resolution (404 fold), and the
// member-DTO projection builders every members-face handler shares.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── identity accessors (service/deps.py twins; claims via requireAuth) ───────

// currentActor is the verified token sub — the ONE caller identity
// (deps.current_actor). "" never happens past the auth gate (verify requires a
// non-empty sub).
func currentActor(r *http.Request) string {
	sub, _ := claimsFromContext(r.Context())["sub"].(string)
	return sub
}

// currentScope is the verified token scope (deps.current_scope).
func currentScope(r *http.Request) string {
	scope, _ := claimsFromContext(r.Context())["scope"].(string)
	return scope
}

// requestTrigger resolves the SSE frame `trigger` attribution for a
// request-driven durable write (spec/sse.md §2.3): the verified token sub —
// "owner" for owner scope (the owner token's sub IS the wireOwnerID literal),
// otherwise the agent/worker/warden member id. A blank sub (no auth context —
// should not happen past the gate) folds to the server attribution rather
// than an empty trigger. NEVER a client-supplied field (root CLAUDE.md §14).
func requestTrigger(r *http.Request) string {
	if sub := currentActor(r); sub != "" {
		return sub
	}
	return triggerServer
}

// currentMachineClaim is the token's optional placement claim
// (deps.current_machine_claim) — "" when absent.
func currentMachineClaim(r *http.Request) string {
	machineID, _ := claimsFromContext(r.Context())["machine_id"].(string)
	return machineID
}

// principalOfRequest resolves the caller's principal class (the in-handler
// twin of the route choke — handlers.principal_at_least call sites).
func (s *apiServer) principalOfRequest(r *http.Request) string {
	return resolvePrincipal(claimsFromContext(r.Context()), s.dal.GetMember)
}

// ── body decoding (the wire-frozen validation_error face) ────────────────────

// decodeJSONBody decodes the request body into dst, answering the
// validation_error envelope on failure. Missing/empty bodies decode the zero
// value (all-optional DTO semantics, frozen from the original wire); a
// malformed JSON body or a type mismatch is a 422 (validation source). Returns false
// when the response was already written.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not read request body")
		return false
	}
	if len(raw) == 0 {
		return true
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid request body: "+err.Error())
		return false
	}
	return true
}

// decodeJSONBodyRequired decodes like decodeJSONBody and then 422s when any of
// the named top-level keys is absent — the Pydantic required-field face (Go
// structs cannot tell a missing key from a zero value).
func decodeJSONBodyRequired(w http.ResponseWriter, r *http.Request, dst any, required ...string) bool {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not read request body")
		return false
	}
	var keys map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &keys); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid request body: "+err.Error())
			return false
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid request body: "+err.Error())
			return false
		}
	}
	for _, name := range required {
		if _, ok := keys[name]; !ok {
			writeError(w, http.StatusUnprocessableEntity, "field required: "+name)
			return false
		}
	}
	return true
}

// ── storage error fold ────────────────────────────────────────────────────────

// internalError answers the honest 500 envelope for a storage/asset fault.
func internalError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, "internal error: "+err.Error())
}

// ── target resolution (handlers._resolve_member/_resolve_machine/_resolve_self)

var errNotFound = errors.New("not found")

// resolveMember returns the LIVE member for memberID (errNotFound when absent
// or soft-removed). kind='outsource' rows resolve as errNotFound too: since
// the P7d table fold an outsource worker lives IN the member table, but the
// member API surface deliberately keeps its pre-fold semantics — worker
// lifecycle rides the outsource routes / the relocate fallback, and an ow- id
// on a member endpoint stays an honest 404, exactly as before the merge.
func (s *apiServer) resolveMember(memberID string) (*Member, error) {
	m, err := s.dal.GetMember(memberID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus == RosterStatusRemoved || m.Kind == KindOutsource {
		return nil, errNotFound
	}
	return m, nil
}

// resolveMachine returns the live ACTIVE kind=="warden" member whose id IS
// machineID (errNotFound otherwise).
func (s *apiServer) resolveMachine(machineID string) (*Member, error) {
	m, err := s.dal.GetMember(machineID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus != RosterStatusActive || m.Kind != machineKind {
		return nil, errNotFound
	}
	return m, nil
}

// writeResolveError folds a resolve failure onto the wire: errNotFound → 404
// with the Python detail string, anything else → 500.
func writeResolveError(w http.ResponseWriter, err error, what, id string) {
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, what+" '"+id+"' not found")
		return
	}
	internalError(w, err)
}

// ── member projections ────────────────────────────────────────────────────────

// Valid effort levels (handlers._MEMBER_EFFORTS): a closed vocabulary — an
// unknown effort is a 422, never silently coerced.
func validEffort(effort string) bool {
	return effort == "low" || effort == "medium" || effort == "high"
}

// memberRoleName resolves a member's role display title
// (handlers._member_role_name): a seed role shows its stable seed title, a
// custom role its overlay name, an unknown/unbound role an honest "".
func (s *apiServer) memberRoleName(m Member) (string, error) {
	if name := seedRoleName(m.RoleKey); name != "" {
		return name, nil
	}
	if m.RoleKey != "" {
		overlay, err := s.dal.GetRoleDef(m.RoleKey)
		if err != nil {
			return "", err
		}
		if overlay != nil && !overlay.Tombstoned {
			return overlay.Name, nil
		}
	}
	return "", nil
}

// observedHost resolves a member's OBSERVED machine (handlers.observed_host):
// SSE machine claim → self-reported telemetry.machine → desired_machine_id; a
// warden attributes to its own id. Honest-empty "" when nothing is observed.
func (s *apiServer) observedHost(m Member) string {
	if m.Kind == machineKind {
		return m.ID
	}
	if host := s.hub.MachineOf(m.ID); host != "" {
		return host
	}
	if entry := s.telemetry.Get(m.ID); entry != nil {
		if tele, _ := entry["machine"].(string); tele != "" {
			return tele
		}
	}
	return m.DesiredMachineID
}

// newMemberDTO projects one member onto the wire (dto.MemberDTO.from_domain):
// presence derives from the live SSE online fact; observedMachine/unreadCount
// are handler-injected where the surface carries them.
func (s *apiServer) newMemberDTO(m Member, roleName, observedMachine string, unreadCount int) memberDTO {
	return memberDTO{
		ID:               m.ID,
		MemberNo:         MemberNo(m.ID),
		Name:             m.Name,
		Kind:             m.Kind,
		RoleKey:          m.RoleKey,
		RoleName:         roleName,
		Model:            m.Model,
		Effort:           m.Effort,
		DesiredState:     m.DesiredState,
		DesiredMachineID: m.DesiredMachineID,
		Machine:          observedMachine,
		Presence:         PresenceState(m, nowSecs(), s.hub.IsOnline(m.ID)),
		RefocusSince:     m.RefocusSince,
		LastOp:           m.LastOp,
		LastOpOK:         m.LastOpOK,
		LastOpLog:        m.LastOpLog,
		LastOpReason:     m.LastOpReason,
		LastOpAt:         m.LastOpAt,
		UnreadCount:      unreadCount,
		RosterStatus:     m.RosterStatus,
		OwnerID:          wireOwnerID,
		SchemaVersion:    wireSchemaVersion,
	}
}

// newMemberLightDTO is the ?fields=light identity-only projection (T-cf91):
// the SAME memberDTO wire shape, carrying only the fields a name+role surface
// reads (id / member_no / name / kind / role_key / role_name + the structural
// owner_id / schema_version / roster_status). Everything the full path DERIVES
// — presence (hub), machine (observed host), unread_count (chat watermark) —
// is left HONEST-EMPTY: not computed here, so a light consumer must not read
// it. last_op* is likewise dropped (row text the identity view never shows),
// which is where most of the per-member byte weight goes.
func (s *apiServer) newMemberLightDTO(m Member, roleName string) memberDTO {
	return memberDTO{
		ID:            m.ID,
		MemberNo:      MemberNo(m.ID),
		Name:          m.Name,
		Kind:          m.Kind,
		RoleKey:       m.RoleKey,
		RoleName:      roleName,
		RosterStatus:  m.RosterStatus,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
	}
}

// writeMemberDTO is the common single-member response tail (role name folded,
// no observed machine / unread injection).
func (s *apiServer) writeMemberDTO(w http.ResponseWriter, m Member) {
	roleName, err := s.memberRoleName(m)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.newMemberDTO(m, roleName, "", 0))
}

// nowSecs is the float epoch clock (time.time()).
func nowSecs() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// newHexID mints a server-side id: n random lowercase hex chars (the Python
// uuid4().hex[:n] convention behind m-/c-/att-/r- ids).
func newHexID(n int) string {
	raw := make([]byte, (n+1)/2)
	if _, err := rand.Read(raw); err != nil {
		panic(err) // the OS entropy source failing is not a servable state
	}
	return hex.EncodeToString(raw)[:n]
}

// trimString is strings.TrimSpace under the handlers' local name.
func trimString(s string) string {
	return strings.TrimSpace(s)
}

// strOrEmpty dereferences an optional request-body string.
func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// trimmedOrEmpty dereferences + trims an optional request-body string.
func trimmedOrEmpty(p *string) string {
	return strings.TrimSpace(strOrEmpty(p))
}
