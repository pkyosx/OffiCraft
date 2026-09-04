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
	"fmt"
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

// receiptReporterMachine names the MACHINE that is speaking on this request —
// the one question a warden command_result receipt has never been able to
// answer on its own (CommandResult carries no warden id, and per
// caller-identity-convention it must never grow one: caller identity is taken
// from the verified token, never from a request parameter).
//
// The resolution is entirely inside the identity we already hold:
//
//   - a WARDEN's credential is minted by mintWardenToken with sub == the warden
//     member's own id, and a warden member's id IS the machine id
//     (api_machines.go onboard: "mint a NEW warden member whose own id IS the
//     machine id"). It deliberately carries NO machine_id claim — "a warden
//     carries NO self-binding" (authz.go). So sub is the machine.
//   - an AGENT / WORKER boot token is something running ON a machine rather
//     than the machine itself, and mintAgentToken stamps machine_id = its host.
//     A non-empty claim is therefore the marker for "not a warden", and we
//     return "" rather than mistaking a member id for a machine id.
//
// 🔴 THAT IMPLICATION RUNS ONE WAY ONLY. "non-empty claim ⇒ not a warden" is
// true; its converse — "claim-less ⇒ warden" — is FALSE, and the counter-
// examples are live, not hypothetical:
//
//   - /api/mint hands out long-lived agent tokens with the claim deliberately
//     blank (api_auth.go: mintJWT(m.ID, "agent", ttl, …, "") — lifecycle.md
//     §1.3 mint table: /api/mint — machine_id "none").
//   - an ordinary member with no placement pin boots claim-less, and the owner
//     can put it in that state at will: activate/relocate take machine_id ""
//     to CLEAR the pin (api_members.go — 「"" 仍清掉 pin」).
//
// api_monitoring.go already says this at the telemetry `machine` fallback
// ("claim-less tokens (/api/mint long-lived tokens … a member without
// desired_machine_id boots claim-less too)"). It is repeated here because the
// two directions do NOT have the same standing, and only one of them is
// guarded — read this before assuming the counter-examples above are handled:
//
//   - CLAIM-BEARING is what the check above handles, and it is the reason that
//     one line is load-bearing rather than defensive. Delete it and the token's
//     sub — a MEMBER id — comes back as a MACHINE id, which consumers read as
//     "a DIFFERENT machine answered" (the KNOWN-mismatch arm) instead of
//     UNKNOWN: the receipt watch then refuses to disarm and stamps
//     receipt_missing on a receipt the server is holding in its hand. Pinned by
//     TestReceiptReporter_ClaimBearingTokenIsNotTheMachineItRunsOn, which exists
//     because deleting that line left the whole suite green.
//   - CLAIM-LESS non-warden tokens (the two counter-examples above) are NOT
//     handled, today, in the present tense. The check cannot see them — on the
//     wire they are shaped exactly like a warden — so this function still hands
//     back their own member id. Member ids and machine ids live in ONE primary
//     key space (a machine IS a member row with Kind == machineKind —
//     resolveMachine below is just GetMember plus that kind test), so such an id
//     can never collide with a real other machine: the comparison necessarily
//     mismatches and every consumer takes its fail-closed arm (keep waiting /
//     keep retrying). The cost is at most a spurious receipt_missing, which is
//     UNKNOWN and not failed. KNOWN RESIDUE — nothing above prevents it, and
//     nothing goes red for it. Measured on 9056a4e1: a claim-less agent's
//     receipt left the worker at last_op_reason = "receipt_missing: the stop was
//     handed to machine m-dark but no receipt came back within 90s…".
//
// Do NOT "improve" this by inferring wardenhood from a blank claim. If a caller
// ever needs a hard "is this a warden", ask the roster (member.Kind ==
// KindWarden); the claim can only ever answer the other direction.
//
// "" means UNKNOWN, never "nobody". Every caller must treat it as no evidence
// and fall back to the behaviour it had before it could ask.
func receiptReporterMachine(r *http.Request) string {
	if currentMachineClaim(r) != "" {
		return "" // something running on a machine, not the machine
	}
	return currentActor(r)
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

// decodeJSONBodyPresent decodes like decodeJSONBodyRequired and ALSO reports
// which top-level keys the caller actually sent, so a handler can tell a field
// that was OMITTED from one explicitly sent as null. A Go pointer collapses the
// two (both decode to nil), and for create_reply_card's linked_task that
// collapse would be the whole bug back again: "I did not say" would look
// identical to "I said this ask is not about a task". The names list still
// answers the wire-frozen 422 face; a handler that wants its own status and its
// own sentence (a 400 that spells out both legal shapes) reads the set instead.
func decodeJSONBodyPresent(w http.ResponseWriter, r *http.Request, dst any, required ...string) (map[string]bool, bool) {
	return decodeJSONBodyKeys(w, r, dst, required...)
}

// decodeJSONBodyStrict is the bool-only face every other handler uses; the
// shared body lives in decodeJSONBodyKeys.
func decodeJSONBodyStrict(w http.ResponseWriter, r *http.Request, dst any, required ...string) bool {
	_, ok := decodeJSONBodyKeys(w, r, dst, required...)
	return ok
}

// decodeJSONBodyKeys is the shared mutable-request decoder. It has two
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
// decodeJSONBodyRequired. The first result is the set of top-level keys the
// caller actually SENT (see decodeJSONBodyPresent). Semantic refusals (anchor miss, wipe guard) stay 400.
func decodeJSONBodyKeys(w http.ResponseWriter, r *http.Request, dst any, required ...string) (map[string]bool, bool) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not read request body")
		return nil, false
	}
	var keys map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &keys); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid request body: "+err.Error())
			return nil, false
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(dst); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid request body: "+err.Error())
			return nil, false
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			if err == nil {
				writeError(w, http.StatusUnprocessableEntity, "invalid request body: multiple JSON values")
			} else {
				writeError(w, http.StatusUnprocessableEntity, "invalid request body: "+err.Error())
			}
			return nil, false
		}
	}
	for _, name := range required {
		if _, ok := keys[name]; !ok {
			writeError(w, http.StatusUnprocessableEntity, "field required: "+name)
			return nil, false
		}
	}
	sent := make(map[string]bool, len(keys))
	for name := range keys {
		sent[name] = true
	}
	return sent, true
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

// errWindDownLadderBackwards is the wind-down ladder's refusal (下線 → 加速 →
// 強制, 「後者一旦發出我們就不該發出前者」), raised where the refusal has to travel
// back through a function that returns an error rather than writing a response
// itself — workerRestartSelf. Its handler maps it to the SAME 409 the staff arm
// of that handler writes: one rule, one sentence, two arms.
var errWindDownLadderBackwards = errors.New("wind-down ladder may not move backwards")

// resolveMember returns the LIVE member for memberID (errNotFound when absent
// or soft-removed). kind='outsource' rows resolve as errNotFound too: since
// the P7d table fold an outsource worker lives IN the member table, but the
// member API surface deliberately keeps its pre-fold semantics — worker
// lifecycle rides the outsource routes / the relocate fallback, and an ow- id
// on a member endpoint stays an honest 404, exactly as before the merge.
//
// ⚠️ This 404 coexists with dal.ListMembers, which DOES put ow- rows in the
// GET /api/members response (since T-14 項目 6 removed its
// `WHERE kind != 'outsource'` there is no second, wider query — this is the
// only one). So a caller can see a worker in the roster list and still get a
// 404 from every member verb — deliberately. Anything that reads "it is in
// members, therefore I may call member verbs on it" is wrong at runtime; the
// two halves are only consistent when read together (see the note on
// ListMembers in dal.go).
// memberScope is the second argument every member lookup must carry: whether
// this door serves the WHOLE roster or staff only.
//
// 🔴 IT IS A REQUIRED PARAMETER RATHER THAN A SECOND FUNCTION, AND THAT IS THE
// WHOLE POINT (owner ruling 2026-08-28: 「只有某些行為如果真的需要只拿正職或外包，
// 才下額外參數指定」). Two differently-named functions would let the NEXT member
// verb reach the open one by simply typing the shorter name — no decision, no
// prompt to make one. A parameter cannot be omitted: every new call site is
// made to say which population it serves, permanently, and not merely during
// the refactor that introduced the split.
//
// The zero value is deliberately NOT "any": a scope that arrives unset is a
// caller that never chose, and defaulting that to the wider population is the
// exact failure this ticket exists to remove.
type memberScope int

const (
	// memberScopeUnset is the zero value and is never legal — see the type doc.
	memberScopeUnset memberScope = iota
	// anyMember serves the whole roster, contractors included. This is the
	// DEFAULT POSTURE for reads: GET /api/members already lists ow- rows to the
	// same principal, so an item door refusing them withheld nothing and cost
	// the cockpit one guaranteed 404 plus a whole-roster refetch per contractor
	// chat line.
	anyMember
	// staffOnly additionally refuses kind='outsource'.
	//
	// 🔴 WHY EACH CALLER PASSES IT — do not relax one without answering its reason:
	//   - mint / bootstrap: a contractor's token TTL and its boot document both
	//     come from the worker path; the staff path would hand it the WRONG
	//     document, not merely too much authority.
	//   - activate / deactivate / force-stop / accelerated-stop / refocus: the
	//     contractor equivalents live under /api/outsource-workers/* and drive a
	//     DIFFERENT kill funnel. Two funnels onto one latch is the double-kill
	//     that T-72dd fixed.
	//   - dismiss (DELETE): a contractor leaves by being RELEASED with its task,
	//     not by being fired; soft-deleting the row under a live task strands it.
	//   - relocate: 🔴 SPECIAL — this one needs errNotFound as CONTROL FLOW. Its
	//     handler catches the refusal and falls through to the worker relocate
	//     core (P7c, rc-2786636f30e5). Widen it and an ow- id takes the member
	//     reconcile path instead, which is not the same operation.
	//   - webhook create / update / revoke, and the public POST /in inlet:
	//     nothing reclaims a webhook token when a worker is released, and /in is
	//     the only UNAUTHENTICATED surface here.
	staffOnly
)

// errScopeUnset is what a caller gets for passing the zero memberScope. It is a
// programming error surfaced as a refusal rather than a silent widening.
var errScopeUnset = errors.New("member lookup called without a memberScope")

// resolveMember looks up ONE member row by id, folding the two states that mean
// "there is nobody here" — no row at all, and a soft-removed one — plus
// kind='outsource' when the caller asked for staffOnly.
//
// The list door has answered for contractors since the P7 convergence
// (rc-2786636f30e5, 「外包對齊正職」); this is the item door catching up.
func (s *apiServer) resolveMember(memberID string, scope memberScope) (*Member, error) {
	if scope == memberScopeUnset {
		return nil, errScopeUnset
	}
	m, err := s.dal.GetMember(memberID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus == RosterStatusRemoved {
		return nil, errNotFound
	}
	if scope == staffOnly && m.Kind == KindOutsource {
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

// refocusDeadlineOf takes recycleGraceFor's pair straight through: an epoch
// nobody collects on a clock has NO deadline, and 0 is how the wire says that
// (the cockpit maps 0 → null → renders nothing). Anything else here would put a
// countdown on screen that the reconcile tick has no intention of honouring.
func refocusDeadlineOf(refocusSince float64, cfg reconcileConfig, refocusOp string) float64 {
	grace, clocked := recycleGraceFor(refocusOp, cfg)
	if !clocked {
		return 0.0
	}
	return refocusDeadline(refocusSince, grace)
}

// winddownDeadlineOf is the ONE expression for "when is this member collected",
// across BOTH wind-down axes, and every face that shows a deadline reads it: the
// wire field (MemberDTO.refocus_deadline) and the sentence the agent is handed
// (offboardNoticeFor).
//
// Two axes exist because 停止 and 換手 are genuinely different operations, not
// because anybody wanted two:
//
//   - 換手 (desired_state stays online) anchors on refocus_since;
//   - 下線 (desired_state=offline) anchors on stopping_since and has no
//     refocus_since at all, so refocusDeadlineOf alone answers 0 for it — which
//     was correct while that arm could never carry a clock, and stopped being
//     correct the moment the owner got a 加速停止 button that works there.
//
// 🔴 AND WHETHER THERE IS AN EPOCH TO RUN A CLOCK ON is gracefulStopEpochOpen
// (api_members.go), asked once, here. The 下線 arm's other two zero conditions
// used to be written out — stopping_since <= 0 and forcedEpochLive — which made
// them the NEGATION of the same pair offboardKindOf spells positively to decide
// whether to send a sentence at all. TestOffboardKindOf_AFinalCallAlwaysHasAClock
// exists because those two spellings could come apart; they are now one call.
//
// 🔴 The AUTHORITY on whether there is a clock is winddownKindFor in both arms,
// asked once, here. A second test for the accelerated cause would be a second
// copy of the ruling — the exact split T-ed79 removed — and the harm is
// asymmetric and silent either way: an announced deadline nobody honours makes
// an agent cut its hand-off short; an unannounced one cuts the hand-off off.
func winddownDeadlineOf(m Member, cfg reconcileConfig) float64 {
	if m.DesiredState == DesiredStateOffline {
		grace, clocked := recycleGraceFor(m.RefocusOp, cfg)
		if !clocked || !gracefulStopEpochOpen(m) {
			return 0.0
		}
		return m.StoppingSince + grace
	}
	return refocusDeadlineOf(m.RefocusSince, cfg, m.RefocusOp)
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
		// The grace this member's epoch is ACTUALLY collected on, and 0 when
		// nothing collects it on time at all — which since T-ed79 is EVERY cause
		// except the two 加速停止 arms (context_high and accelerated_stop), not
		// just the owner-pressed 重新聚焦 this comment used to name. The cockpit
		// must show NO deadline rather than a time the owner would watch pass with
		// nothing happening. Reading RecycleGrace straight would report exactly
		// that kind of ceiling, for most of the closed set.
		RefocusDeadline: winddownDeadlineOf(m, s.reconcileConfigLive()),
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
// for outsource rows returned by ListMembers.
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

// requireNonEmptyEdits refuses an empty edits list: it is not "a patch that
// changes nothing", it is a caller that built the request wrong.
//
// Split from decodePatchEdits because the two checks sit on OPPOSITE sides of
// the target's resolve/authz chain in patch_lessons and patch_task_learnings,
// and the newer patch faces mirror that placement rather than invent a second
// order — otherwise the same malformed batch against a nonexistent target
// answers 422 on one endpoint and 404 on its neighbour.
func requireNonEmptyEdits(w http.ResponseWriter, dtos []LessonsEditDTO) bool {
	if len(dtos) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"edits requires at least one {old, new} entry")
		return false
	}
	return true
}

// decodePatchEdits folds a wire []LessonsEditDTO into the engine's
// []LessonsEdit, writing a 422 and returning ok=false for an edit carrying
// NEITHER old NOR new — that would fold to the empty-old APPEND branch where
// appending "" is a perfect no-op, so the batch would answer 200 with an
// unchanged doc, i.e. report success while doing nothing. The check is the one
// patch_lessons and patch_task_learnings already spell inline (T-2d99), lifted
// here so the newer patch faces cannot answer a malformed batch differently.
//
// The WHOLE batch is refused before anything is written, matching the
// anchor-miss posture. Callers run it AFTER resolving the target (see
// requireNonEmptyEdits).
func decodePatchEdits(w http.ResponseWriter, dtos []LessonsEditDTO) ([]LessonsEdit, bool) {
	edits := make([]LessonsEdit, len(dtos))
	for i, e := range dtos {
		if e.Old == nil && e.New == nil {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf(
				"edits[%d]: neither old nor new was given — an edit needs at least one of them "+
					"(empty old appends new); nothing was written", i))
			return nil, false
		}
		edits[i] = LessonsEdit{Old: strOrEmpty(e.Old), New: strOrEmpty(e.New)}
	}
	return edits, true
}
