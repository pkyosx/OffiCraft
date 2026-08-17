package main

// api_helpers.go — the shared handler plumbing (M3 REST sub-batch B): request
// identity accessors (the deps.py twins), JSON body decoding with the
// wire-frozen 422/400 split, target resolution (404 fold), and the
// member-DTO projection builders every members-face handler shares.

import (
	"bytes"
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

// member-avatar blobs have a single mutable owner (member.avatar_attachment_id).
// They must never enter the general multi-reference attachment graph: avatar
// replacement/removal deletes the old blob, which would otherwise leave chat,
// reply-card, task-message, or task-artifact rows pointing at missing bytes.
func isMemberAvatarAttachmentID(id string) bool {
	return strings.HasPrefix(id, "ava-")
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

// decodeJSONBody decodes a mutable request body into dst, answering the
// validation_error envelope on failure. Unknown fields are refused instead of
// being silently discarded: every API write and its MCP loopback must have the
// same fail-closed typo behaviour. Missing/empty bodies still decode the zero
// value (all-optional DTO semantics). Returns false when the response was
// already written.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBodyStrict(w, r, dst)
}

// decodeJSONBodyRequired decodes like decodeJSONBody and then 422s when any of
// the named top-level keys is absent — the Pydantic required-field face (Go
// structs cannot tell a missing key from a zero value).
func decodeJSONBodyRequired(w http.ResponseWriter, r *http.Request, dst any, required ...string) bool {
	return decodeJSONBodyStrict(w, r, dst, required...)
}

// decodeJSONBodyStrict is the shared mutable-request decoder. It has two
// properties that stop a malformed request from masquerading as a valid one:
//
//  1. DisallowUnknownFields — any key the DTO does not declare is a 422, not a
//     silent drop. This is the single highest-leverage guard: the observed data
//     loss was an agent sending write_task_learnings{learnings: "..."} (the key
//     update_task_manual uses for the same document) — the unknown key was
//     dropped, body.Text stayed nil, strOrEmpty folded it to "" and the whole
//     doc was wiped, with the response cheerfully echoing learnings: "". Note
//     encoding/json applies this to NESTED objects too, so it also catches
//     edits[i].old_text in a patch_lessons batch.
//  2. required names — a whole-doc replace must never infer "the caller wants
//     it empty" from "the caller did not say". Absent key ⇒ 422, never a write.
//
// Both faults answer 422 (the wire-frozen validation_error source), matching
// decodeJSONBodyRequired. Semantic refusals (anchor miss, wipe guard) stay 400.
func decodeJSONBodyStrict(w http.ResponseWriter, r *http.Request, dst any, required ...string) bool {
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
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(dst); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid request body: "+err.Error())
			return false
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			if err == nil {
				writeError(w, http.StatusUnprocessableEntity, "invalid request body: multiple JSON values")
			} else {
				writeError(w, http.StatusUnprocessableEntity, "invalid request body: "+err.Error())
			}
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

// relocateNeedsMachineMsg is the 400 a relocate answers when the body carries no
// destination — owner ruling 2026-07-27: 搬遷一定要帶機器.
//
// Until that ruling all three shapes (key absent / explicit null / "") collapsed
// to "" and CLEARED the owner's pin, which contradicted the sticky-placement
// rule that a hand-moved worker is pulled back by no configuration: the same
// verb both set and destroyed the pin, and the destroying form was the one you
// got by forgetting a field. There is no longer an unpin verb on this route; a
// relocate NAMES where to go. Absent key answers 422 (the frozen
// validation_error face for a missing required field, decodeJSONBodyRequired);
// a present-but-empty value is a semantic refusal and stays 400, matching the
// decodeJSONBodyStrict contract. ONE message for both faces (member + worker) so
// they cannot drift into two stories.
const relocateNeedsMachineMsg = "machine_id must name a machine: a relocate moves " +
	"an agent to a specific machine, and no longer clears its placement"

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
//
// ⚠️ This 404 coexists with dal.ListMembersIncludingOutsource, which DOES put
// ow- rows in the GET /api/members response. So a caller can see a worker in
// the roster list and still get a 404 from every member verb — deliberately.
// Anything that reads "it is in members, therefore I may call member verbs on
// it" is wrong at runtime; the two halves are only consistent when read
// together (see the twin note on ListMembersIncludingOutsource in dal.go).
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

// resolveResumeSummaryTarget is resolveMember WITHOUT the kind='outsource'
// fold, for the ONE verb the owner released to workers: reading a worker's
// resume summary from the cockpit (T-4595, ruling rc-64b712bfc703 option ①).
//
// WHY A SECOND RESOLVER RATHER THAN WIDENING resolveMember: that function has
// 16 production call sites across five files, so dropping its outsource arm
// would open every member verb to ow- ids at once — account and token, member
// lifecycle, machines, webhooks. The owner picked the blast radius of one.
// The shape is not invented here: api_members.go already keeps a sibling
// resolver that deliberately does not fold outsource away, and GetMember has
// the same precedent for a worker reading its own row.
//
// It keeps BOTH of resolveMember's other refusals: an absent row and a
// soft-removed one are still errNotFound, so a released worker's summary stops
// being readable at exactly the moment its roster row goes.
func (s *apiServer) resolveResumeSummaryTarget(memberID string) (*Member, error) {
	m, err := s.dal.GetMember(memberID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus == RosterStatusRemoved {
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
	return effort == "low" || effort == "medium" || effort == "high" || effort == "max"
}

const (
	RuntimeClaude = "claude"
	RuntimeCodex  = "codex"
)

func NormalizeRuntime(runtime string) string {
	runtime = strings.TrimSpace(runtime)
	if runtime == "" {
		return RuntimeClaude
	}
	return runtime
}

func ValidRuntime(runtime string) bool {
	return runtime == RuntimeClaude || runtime == RuntimeCodex
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

// refocusDeadline is the epoch by which an in-flight wind-down is force-
// collected — the CEILING the cockpit quotes when it says when a pending launch
// change takes effect at the latest. 0 in, 0 out (no window, no deadline).
//
// It is derived here rather than stored because the grace is reconcile
// configuration, not a property of the stamp: storing it would let the two
// drift the first time the grace is retuned.
func refocusDeadline(refocusSince, grace float64) float64 {
	if refocusSince <= 0.0 {
		return 0.0
	}
	return refocusSince + grace
}

// observedHost resolves a member's OBSERVED machine (handlers.observed_host):
// SSE machine claim → self-reported telemetry.machine; a warden attributes to
// its own id. Honest-empty "" when nothing is observed.
//
// 🔴 It does NOT fall back to desired_machine_id (T-7f28). It used to — against
// its own doc comment — and that made an offline member read as though it were
// already running on the machine the owner had just pinned it to: the observed
// cell and the intent cell showed the same value, so a move that had not
// happened was byte-indistinguishable from one that had. A missing observation
// is information; substituting the intent destroys it. The durable
// last-observed machine lives in last_machine_id (MemberDTO.actual_machine),
// which is what a client compares the pin against.
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
	return ""
}

// newMemberDTO projects one member onto the wire (dto.MemberDTO.from_domain):
// presence derives from the live SSE online fact; observedMachine/unreadCount
// are handler-injected where the surface carries them.
// unreadCountsForRequest is the ONE unread computation every member-facing
// handler shares: the CALLER's chat_read watermark inverted over the whole chat
// stream (UnreadCounts). It exists because GET /api/members/{id} used to hand
// newMemberDTO a literal 0 while GET /api/members computed the real number — the
// same declared field with two different answers, so the cockpit's roster badge
// could only ever go DOWN through a one-member refetch (a chat delta re-read that
// member and zeroed the badge the delta was announcing). The outsource
// single-item handler was already doing it the right way; this makes members
// match. NOT a wire change: MemberDTO has always declared unread_count.
func (s *apiServer) unreadCountsForRequest(r *http.Request) (map[string]int, error) {
	actor := currentActor(r)
	messages, err := s.dal.ListChat()
	if err != nil {
		return nil, err
	}
	receipts, err := s.dal.ListChatReads(actor, "")
	if err != nil {
		return nil, err
	}
	return UnreadCounts(messages, receipts, actor), nil
}

func (s *apiServer) newMemberDTO(m Member, roleName, observedMachine string, unreadCount int) memberDTO {
	return memberDTO{
		ID:               m.ID,
		AvatarURL:        memberAvatarURL(m.AvatarAttachmentID),
		Name:             m.Name,
		Kind:             m.Kind,
		RoleKey:          m.RoleKey,
		RoleName:         roleName,
		Runtime:          NormalizeRuntime(m.Runtime),
		Model:            m.Model,
		ActualModel:      m.ActualModel,
		ActualRuntime:    m.ActualRuntime,
		ActualEffort:     m.ActualEffort,
		ActualMachine:    m.LastMachineID,
		Effort:           m.Effort,
		DesiredState:     m.DesiredState,
		DesiredMachineID: m.DesiredMachineID,
		Machine:          observedMachine,
		Presence:         PresenceState(m, nowSecs(), s.hub.IsOnline(m.ID)),
		RefocusSince:     m.RefocusSince,
		RefocusOp:        m.RefocusOp,
		// The grace this member's epoch is ACTUALLY collected on — an
		// owner-pressed 重新聚焦 opens soft and gets the soft window on top of
		// the final 120s, so reading RecycleGrace here reported a ceiling the
		// server had no intention of honouring, and the cockpit rendered a
		// time the owner then watched pass with nothing happening.
		RefocusDeadline: refocusDeadline(m.RefocusSince, recycleGraceFor(m.RefocusOp, s.reconcileCfg)),
		LastOp:          m.LastOp,
		LastOpOK:        m.LastOpOK,
		LastOpLog:       m.LastOpLog,
		LastOpReason:    m.LastOpReason,
		LastOpAt:        m.LastOpAt,
		ForcedStopAt:    m.ForcedStopAt,
		UnreadCount:     unreadCount,
		RosterStatus:    m.RosterStatus,
		OwnerID:         wireOwnerID,
		SchemaVersion:   wireSchemaVersion,
	}
}

// newMemberLightDTO is the ?fields=light identity-only projection (T-cf91):
// the SAME memberDTO wire shape, carrying only the fields a name+role surface
// reads (id / name / kind / role_key / role_name + the structural
// owner_id / schema_version / roster_status). Everything the full path DERIVES
// — presence (hub), machine (observed host), unread_count (chat watermark) —
// is left HONEST-EMPTY: not computed here, so a light consumer must not read
// it. last_op* is likewise dropped (row text the identity view never shows),
// which is where most of the per-member byte weight goes. Kind remains present
// for outsource rows returned by ListMembersIncludingOutsource.
func (s *apiServer) newMemberLightDTO(m Member, roleName string) memberDTO {
	return memberDTO{
		ID:            m.ID,
		AvatarURL:     memberAvatarURL(m.AvatarAttachmentID),
		Name:          m.Name,
		Kind:          m.Kind,
		RoleKey:       m.RoleKey,
		RoleName:      roleName,
		Runtime:       NormalizeRuntime(m.Runtime),
		RosterStatus:  m.RosterStatus,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
	}
}

func memberAvatarURL(attachmentID string) string {
	if attachmentID == "" {
		return ""
	}
	return "/api/chat/attachment/" + attachmentID
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

// intOr dereferences an optional request-body int, falling back to the field's
// declared default. Distinct from a bare deref-or-zero: the caller names the
// default, so a field whose documented fallback is not 0 (day_of_month is 1)
// cannot silently acquire one.
func intOr(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}

// intSliceOrNil dereferences an optional request-body integer array. An absent
// array and an empty one both become nil — they mean the same thing here, and
// the write seam renders nil as the empty column value.
func intSliceOrNil(p *[]int) []int {
	if p == nil {
		return nil
	}
	return *p
}
