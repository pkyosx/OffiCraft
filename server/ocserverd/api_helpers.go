package main

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

func currentActor(r *http.Request) string {
	sub, _ := claimsFromContext(r.Context())["sub"].(string)
	return sub
}

func currentScope(r *http.Request) string {
	scope, _ := claimsFromContext(r.Context())["scope"].(string)
	return scope
}

// requestTrigger is the SSE frame `trigger` (spec/sse.md §2.3): the verified
// token sub (the owner token's sub IS the wireOwnerID literal), NEVER a
// client-supplied field (root AGENTS.md §14).
func requestTrigger(r *http.Request) string {
	if sub := currentActor(r); sub != "" {
		return sub
	}
	return triggerServer
}

// Avatar blobs have a single owner and are deleted on replacement/removal, so
// they must never enter the shared attachment graph: chat, reply-card,
// task-message or task-artifact rows would point at missing bytes.
func isMemberAvatarAttachmentID(id string) bool {
	return strings.HasPrefix(id, "ava-")
}

func currentMachineClaim(r *http.Request) string {
	machineID, _ := claimsFromContext(r.Context())["machine_id"].(string)
	return machineID
}

// receiptReporterMachine names the MACHINE speaking on this request, from the
// verified token (CommandResult must never grow a warden id). A warden token's
// sub IS the machine id and carries NO machine_id claim; agent/worker boot
// tokens carry machine_id = their host, so a claim means "not the machine".
// That check is load-bearing: without it a member id comes back as a machine id,
// reads as "a different machine answered", and the receipt watch stamps
// receipt_missing on a receipt the server is holding.
//
// 🔴 It runs ONE way only: claim-less does NOT imply warden (/api/mint tokens
// and unpinned members boot claim-less), so their member id is returned. One id
// space means it can never match a real other machine: the cost is at most a
// spurious receipt_missing — known, unguarded residue. Do not infer wardenhood
// from a blank claim; ask the roster (member.Kind). "" means UNKNOWN: callers
// fall back to their previous behaviour.
func receiptReporterMachine(r *http.Request) string {
	if currentMachineClaim(r) != "" {
		return ""
	}
	return currentActor(r)
}

func (s *apiServer) principalOfRequest(r *http.Request) principalClass {
	return resolvePrincipal(claimsFromContext(r.Context()), s.dal.GetMember)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBodyStrict(w, r, dst)
}

func decodeJSONBodyRequired(w http.ResponseWriter, r *http.Request, dst any, required ...string) bool {
	return decodeJSONBodyStrict(w, r, dst, required...)
}

// decodeJSONBodyPresent also reports which top-level keys were SENT, so a
// handler can tell OMITTED from explicit null (a pointer collapses both) — for
// create_reply_card's linked_task that is "I did not say" vs "not about a task".
func decodeJSONBodyPresent(w http.ResponseWriter, r *http.Request, dst any, required ...string) (map[string]bool, bool) {
	return decodeJSONBodyKeys(w, r, dst, required...)
}

func decodeJSONBodyStrict(w http.ResponseWriter, r *http.Request, dst any, required ...string) bool {
	_, ok := decodeJSONBodyKeys(w, r, dst, required...)
	return ok
}

// decodeJSONBodyKeys refuses unknown keys (nested ones too, e.g.
// edits[i].old_text): the observed data loss was an agent using a neighbouring
// tool's key name — the key was dropped, body.Text stayed nil, strOrEmpty
// folded it to "" and the whole doc was wiped. Absent required key ⇒ 422, never
// "the caller wants it empty". Both answer the wire-frozen 422; semantic
// refusals (anchor miss, wipe guard) stay 400.
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

// relocateNeedsMachineMsg: a relocate must name a machine (owner ruling
// 2026-07-27: 搬遷一定要帶機器) — an empty machine_id used to CLEAR the pin.
// Absent key → 422, present-but-empty → 400; one message for both faces
// (member + worker).
const relocateNeedsMachineMsg = "machine_id must name a machine: a relocate moves " +
	"an agent to a specific machine, and no longer clears its placement"

func internalError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, "internal error: "+err.Error())
}

var errNotFound = errors.New("not found")

// txRefusal is a 4xx decided inside a transaction from the rows it just read:
// returning it rolls the transaction back, and writeTxError answers it.
type txRefusal struct {
	status int
	msg    string
}

func (e *txRefusal) Error() string { return e.msg }

func refuseInTx(status int, msg string) error { return &txRefusal{status: status, msg: msg} }

func writeTxError(w http.ResponseWriter, err error) {
	var refusal *txRefusal
	if errors.As(err, &refusal) {
		writeError(w, refusal.status, refusal.msg)
		return
	}
	internalError(w, err)
}

// errWindDownLadderBackwards carries the wind-down ladder refusal (下線 → 加速 →
// 強制, 「後者一旦發出我們就不該發出前者」) back through workerRestartSelf; its
// handler maps it to the SAME 409 the staff arm writes.
var errWindDownLadderBackwards = errors.New("wind-down ladder may not move backwards")

// memberScope is a REQUIRED parameter rather than a second function (owner
// ruling 2026-08-28): every call site must say which population it serves, and
// the zero value is deliberately not "any" — an unset scope is refused, never
// widened.
type memberScope int

const (
	memberScopeUnset memberScope = iota
	anyMember
	// staffOnly refuses kind='outsource'. Do not relax a caller without
	// answering its reason:
	//   - mint / bootstrap: a contractor's TTL and boot document come from the
	//     worker path; the staff path hands it the WRONG document.
	//   - dismiss: a contractor leaves by being RELEASED with its task;
	//     soft-deleting the row under a live task strands it.
	//   - relocate: its handler needs errNotFound as CONTROL FLOW to fall through
	//     to the worker relocate core; widened, an ow- id takes the member
	//     reconcile path, which is not the same operation.
	staffOnly
)

var errScopeUnset = errors.New("member lookup called without a memberScope")

func (s *apiServer) resolveMember(memberID string, scope memberScope) (*Member, error) {
	return resolveMemberOn(s.dal.rdb, memberID, scope)
}

func resolveMemberOn(q sqlRowQuerier, memberID string, scope memberScope) (*Member, error) {
	if scope == memberScopeUnset {
		return nil, errScopeUnset
	}
	m, err := getMemberOn(q, memberID)
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

// resolveMemberForItemRead keeps a released outsource worker addressable because
// chats, tasks and lore keep its codename; the same roster_status means
// dismissal for staff and teardown for wardens, so those stay not found.
func (s *apiServer) resolveMemberForItemRead(memberID string) (*Member, error) {
	m, err := s.dal.GetMember(memberID)
	if err != nil {
		return nil, err
	}
	if m == nil || (m.RosterStatus == RosterStatusRemoved && m.Kind != KindOutsource) {
		return nil, errNotFound
	}
	return m, nil
}

func (s *apiServer) resolveMachine(machineID string) (*Member, error) {
	return resolveMachineOn(s.dal.rdb, machineID)
}

func resolveMachineOn(q sqlRowQuerier, machineID string) (*Member, error) {
	m, err := getMemberOn(q, machineID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.RosterStatus != RosterStatusActive || m.Kind != machineKind {
		return nil, errNotFound
	}
	return m, nil
}

func writeResolveError(w http.ResponseWriter, err error, what, id string) {
	if errors.Is(err, errNotFound) {
		writeTxError(w, notFoundRefusal(err, what, id))
		return
	}
	internalError(w, err)
}

// notFoundRefusal is a resolver's errNotFound as the 404 writeResolveError
// answers for it; any other error comes back unchanged.
func notFoundRefusal(err error, what, id string) error {
	if errors.Is(err, errNotFound) {
		return refuseInTx(http.StatusNotFound, what+" '"+id+"' not found")
	}
	return err
}

// writeResolveTxError answers a transaction that re-resolved its target: the
// resolver's errNotFound, a txRefusal, or a 500.
func writeResolveTxError(w http.ResponseWriter, err error, what, id string) {
	if errors.Is(err, errNotFound) {
		writeResolveError(w, err, what, id)
		return
	}
	writeTxError(w, err)
}

func validEffort(effort string) bool {
	return effort == "low" || effort == "medium" || effort == "high" ||
		effort == "xhigh" || effort == "max"
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

// isCodexModelFamily names the model words a warden resolves at spawn to the newest
// full id its own Codex lists. Keep in step with cli/ocwarden's codexModelFamilies and
// the frontend's CODEX_MODEL_OPTIONS: nothing compares the three.
func isCodexModelFamily(model string) bool {
	switch model {
	case "astra", "sol", "terra", "luna":
		return true
	}
	return false
}

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

func (s *apiServer) hireRoleKeyAvailable(roleKey string) (bool, error) {
	role, err := s.foldRoleDefDTO(roleKey)
	if err != nil {
		return false, err
	}
	return role != nil, nil
}

// refocusDeadline is derived rather than stored because the grace is reconcile
// configuration: a stored deadline would drift the first time grace is retuned.
func refocusDeadline(refocusSince, grace float64) float64 {
	if refocusSince <= 0.0 {
		return 0.0
	}
	return refocusSince + grace
}

// refocusDeadlineOf: an epoch nobody collects on a clock has NO deadline, and 0
// is how the wire says so (the cockpit maps 0 → null → renders nothing). Reading
// RecycleGrace straight would put a countdown on screen nothing honours.
func refocusDeadlineOf(refocusSince float64, cfg reconcileConfig, refocusOp string) float64 {
	grace, clocked := recycleGraceFor(refocusOp, cfg)
	if !clocked {
		return 0.0
	}
	return refocusDeadline(refocusSince, grace)
}

// winddownDeadlineOf is the ONE "when is this member collected" expression; the
// wire field (MemberDTO.refocus_deadline) and the agent's sentence
// (offboardNoticeFor) both read it. 換手 anchors on refocus_since, 下線
// (desired_state=offline) on stopping_since. Whether an epoch is open is
// gracefulStopEpochOpen and whether it runs a clock is winddownKindFor, each
// asked once here: a second copy of either ruling can drift silently (an
// announced deadline nobody honours cuts a hand-off short).
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

// observedHost does NOT fall back to desired_machine_id: that made a move that
// had not happened indistinguishable from one that had. The durable
// last-observed machine is last_machine_id (MemberDTO.actual_machine).
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

// unreadCountsForRequest is the ONLY door to s.dal.UnreadCountsFor (pinned by a
// test): every surface showing an unread number comes through here. What each
// surface then does with the map (filtering, per-member binding) stays with the
// surface.
func (s *apiServer) unreadCountsForRequest(r *http.Request) (map[string]int, error) {
	return s.dal.UnreadCountsFor(currentActor(r))
}

func (s *apiServer) newMemberDTO(m Member, roleName, observedMachine string, unreadCount int) memberDTO {
	return newMemberDTO(m, roleName, observedMachine, unreadCount,
		PresenceState(m, nowSecs(), s.hub.IsOnline(m.ID)),
		winddownDeadlineOf(m, s.reconcileConfigLive()),
		terminalAttachCommand(s.namespace, m.ID))
}

func newMemberDTO(m Member, roleName, observedMachine string, unreadCount int,
	presence string, refocusDeadline float64, terminalAttach string,
) memberDTO {
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
		Presence:         presence,
		RefocusSince:     m.RefocusSince,
		RefocusOp:        m.RefocusOp,

		RefocusDeadline: refocusDeadline,
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
		// T-139: the WHOLE attach command — a cockpit-side `tmux -L officraft`
		// is wrong on every namespaced station.
		TerminalAttachCommand: terminalAttach,
	}
}

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
		// Served here too, unlike the other derived fields: "" is the wire's
		// "server too old to send one", which a light row would state falsely.
		TerminalAttachCommand: terminalAttachCommand(s.namespace, m.ID),
	}
}

func memberAvatarURL(attachmentID string) string {
	if attachmentID == "" {
		return ""
	}
	return "/api/chat/attachment/" + attachmentID
}

func (s *apiServer) writeSelfReportReceipt(w http.ResponseWriter, m Member) {
	s.writeSelfReportStopReceipt(w, m, "")
}

// 🔴 stop_effect is the ONLY thing distinguishing report_stopped outcomes on
// the wire; both handler arms (staff and the outsource fold) must name theirs,
// or a new outcome becomes a 200 that means nothing.
func (s *apiServer) writeSelfReportStopReceipt(
	w http.ResponseWriter, m Member, stopEffect string,
) {
	writeJSON(w, http.StatusOK, selfReportReceiptDTO{
		ID:           m.ID,
		DesiredState: m.DesiredState,
		RefocusOp:    m.RefocusOp,
		StopEffect:   stopEffect,

		RefocusDeadline: winddownDeadlineOf(m, s.reconcileConfigLive()),
	})
}

func nowSecs() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

func newHexID(n int) string {
	raw := make([]byte, (n+1)/2)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)[:n]
}

func trimString(s string) string {
	return strings.TrimSpace(s)
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func trimmedOrEmpty(p *string) string {
	return strings.TrimSpace(strOrEmpty(p))
}

func intOr(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}

func intSliceOrNil(p *[]int) []int {
	if p == nil {
		return nil
	}
	return *p
}

// requireNonEmptyEdits runs BEFORE the target's resolve/authz chain and
// decodePatchEdits AFTER it; every patch face mirrors that order, otherwise the
// same malformed batch answers 422 on one endpoint and 404 on its neighbour.
func requireNonEmptyEdits(w http.ResponseWriter, dtos []LessonsEditDTO) bool {
	if len(dtos) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"edits requires at least one {old, new} entry")
		return false
	}
	return true
}

// decodePatchEdits refuses an edit with NEITHER old NOR new: it would fold to
// the empty-old append branch, append "", and answer 200 while doing nothing.
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
