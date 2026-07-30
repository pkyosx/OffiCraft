package main

// api_monitoring.go — the observation channel (handlers.
// handle_ingest_agent_context / handle_ingest_telemetry /
// handle_get_monitoring): the two IN-MEMORY ingest stores (restart amnesia is
// contract, lifecycle.md §3) keyed on the VERIFIED token sub, the durable
// command_result fold onto member.last_op*, and the three-section monitoring
// fold that never fabricates a number.

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// commandResultLogMax re-clamps the folded command_result log (the warden
// already truncates to 4 KB; the body is untrusted).
const commandResultLogMax = 4096

// commandResultReasonMax caps the folded command_result reason — a one-line
// structured "<code>: <detail>" summary (SpawnOutcome.Reason), NOT the log
// dump, so it gets a much tighter bound (the body is untrusted).
const commandResultReasonMax = 512

// POST /api/agent/context — ingest the caller's context gauge. Non-numeric
// context_pct → flat 400 (never 422). MERGES onto the prior entry so the
// session boot_ts anchor survives.
func (s *apiServer) HandleIngestAgentContextApiAgentContextPost(w http.ResponseWriter, r *http.Request) {
	var body AgentContextIngestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	pct, ok := body.ContextPct.(float64) // JSON numbers land as float64; bool/str/nil fail
	if !ok {
		writeError(w, http.StatusBadRequest, "context_pct must be a number")
		return
	}
	var compactions *int
	if body.CompactionCount != nil {
		value, ok := body.CompactionCount.(float64)
		if !ok || value < 0 || value != math.Trunc(value) {
			writeError(w, http.StatusBadRequest, "compaction_count must be a non-negative integer")
			return
		}
		count := int(value)
		compactions = &count
	}
	agentID := currentActor(r)
	rateLimits := map[string]any{}
	if body.RateLimits != nil {
		for k, v := range *body.RateLimits {
			rateLimits[k] = v
		}
	}
	now := nowSecs()
	entry := s.gauge.Get(agentID)
	if entry == nil {
		entry = map[string]any{}
	}
	entry["context_pct"] = pct
	entry["rate_limits"] = rateLimits
	entry["ts"] = now
	entry["context_pct_ts"] = now
	if compactions != nil {
		entry["compaction_count"] = *compactions
	}
	s.gauge.Set(agentID, entry)
	// No agent consumes the context signal on the wire (it drives the
	// server-side context-high band, not fan-out); owner cockpit only.
	s.hub.Publish("context", "signal", "context", agentID, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, agentContextDTO{
		AgentID:         agentID,
		ContextPct:      pct,
		CompactionCount: compactions,
		RateLimits:      rateLimits,
		TS:              now,
	})
}

// teleNum shapes a telemetry numeric: bool / non-number / negative sentinel
// (-1 = 未量到) → nil, NEVER a fabricated 0 (handlers._tele_num).
func teleNum(value any) *float64 {
	n, ok := value.(float64)
	if !ok || n < 0 {
		return nil
	}
	return &n
}

// teleBool shapes a telemetry boolean: absent / non-bool stays honest-nil.
func teleBool(value any) *bool {
	b, ok := value.(bool)
	if !ok {
		return nil
	}
	return &b
}

// declaredHardwareTypes is the hardware sub-shape the SERVER READS BY NAME, and
// the JSON type each of those reads needs. It mirrors the frozen spec's
// AgentTelemetryIngestDTO.hardware declaration (number for the three percents,
// boolean for ac_power) — the same four keys teleNum/teleBool are applied to in
// the machines fold, listed once so the classifier below and the readers below
// cannot drift apart.
//
// Only DECLARED keys are listed, and that is the point: `additionalProperties`
// on this block stays true (owner ruling rc-55861dd893c6) so a warden that grows
// an undeclared probe still lands its whole report, and an undeclared key has no
// declared type to be wrong about. Judging one would turn "a newer warden sent
// something we have not heard of" into an on-screen defect — the same
// intolerance the ruling rejected, moved from the ingest path to the read path.
var declaredHardwareTypes = map[string]string{
	"cpu_pct":     "number",
	"ram_pct":     "number",
	"battery_pct": "number",
	"ac_power":    "boolean",
}

// hardwareInvalidKeys names the declared hardware keys that are PRESENT in this
// sample but carry a value the reader cannot use — sorted, empty when the sample
// is clean. It is what puts "measured, but unreadable" on the wire as something
// other than silence; see monitoringMachineDTO.HardwareInvalid for why that
// distinction is the whole point of the field.
//
// Three things are deliberately NOT invalid, because each is a real answer:
//   - an ABSENT key. collectHardware omits every probe that failed, so a machine
//     with no battery legitimately sends no battery_pct. That is "never
//     measured", the case this field exists to stop being confused WITH.
//   - an explicit null. The frozen spec declares every one of these as
//     `anyOf: [<type>, null]`, so null is a declared way of saying the same
//     thing an omission says.
//   - a negative number. teleNum withholds it (-1 is the 未量到 sentinel), but it
//     is a number: the type contract is met and the producer is not broken, so
//     calling it invalid would blame a healthy reporter.
//
// What IS invalid is a value of the wrong JSON type — a string cpu_pct, a
// stringly "yes" for ac_power — i.e. exactly the shapes that are accepted with a
// 200, stored verbatim, and then read back as null forever.
func hardwareInvalidKeys(hw map[string]any) []string {
	invalid := []string{}
	for key, want := range declaredHardwareTypes {
		value, present := hw[key]
		if !present || value == nil {
			continue // absent / explicitly null — a declared way of saying "no reading"
		}
		ok := false
		switch want {
		case "number":
			_, ok = value.(float64)
		case "boolean":
			_, ok = value.(bool)
		}
		if !ok {
			invalid = append(invalid, key)
		}
	}
	sort.Strings(invalid)
	return invalid
}

// commandResultAtEpoch parses a command_result "at" (RFC3339 from the warden;
// a bare epoch number accepted for robustness; garbage → 0.0 so a bad
// timestamp can never shortcut presence).
func commandResultAtEpoch(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0.0
		}
		if t, err := time.Parse(time.RFC3339, text); err == nil {
			return float64(t.UnixNano()) / 1e9
		}
		if t, err := time.ParseInLocation("2006-01-02T15:04:05", text, time.Local); err == nil {
			return float64(t.UnixNano()) / 1e9
		}
	}
	return 0.0
}

// stopNoopReasonPrefix is the prefix of the warden stop receipt Reason
// (cli/ocwarden/command.go rpcStop/rpcWorkerStop) when the robust stop was an
// idempotent NO-OP: the addressed session did not exist on that warden and no
// member process was found — nothing was actually killed. Twin of the
// spawnClobberReasonPrefix cross-module contract (reconcile.go).
const stopNoopReasonPrefix = "no_such_session"

// isStopNoopReceipt reports whether a command_result receipt is a no-op stop:
// an OK stop whose reason carries the no_such_session code. Such a receipt
// proves only that ONE warden's tmux view held no session — it is NOT evidence
// the member was killed (identity sweeps broadcast stop to every other warden;
// a mis-routed / already-dead stop no-ops the same way), so folding it over
// last_op would forge a "successfully stopped" story onto a member whose live
// session was never touched (T-9adc, the 2026-07-20 incident's misleading
// last_op=stop/ok=true). Callers SKIP the last_op fold for these receipts.
func isStopNoopReceipt(rpc string, ok *bool, reason string) bool {
	if rpc != "stop" && rpc != "worker_stop" {
		return false
	}
	if ok == nil || !*ok {
		return false // a FAILED stop is always folded — failure must stay visible
	}
	return strings.HasPrefix(reason, stopNoopReasonPrefix)
}

// wakeTimeoutReasonCode is the reason CODE stampWakeObservability writes
// (reconcile.go) when a START lapsed its start window. It is the only
// dispatch-level — as opposed to execution-level — writer of last_op_reason
// today, and naming it here is the cross-module contract that lets the receipt
// fold recognise what it is about to overwrite.
const wakeTimeoutReasonCode = "wake_timeout"

// supersededDispatchClue returns a one-line carry-forward of the member's
// CURRENT last_op_reason when that reason is a dispatch-level diagnosis (the
// "nothing ever came back" story) about to be replaced by an execution receipt
// (the "the machine acted and here is what happened" story). Empty when there is
// nothing worth preserving — a receipt superseding a receipt is ordinary.
//
// Deliberately in-place inside the existing five last_op* fields: a separate
// durable slot would grow MemberDTO, and the wire is frozen (CLAUDE.md §13).
// This follows the isStopNoopReceipt precedent — the fold already knows that not
// every receipt deserves the slot on its own terms.
func supersededDispatchClue(m Member) string {
	if !strings.HasPrefix(m.LastOpReason, wakeTimeoutReasonCode+":") {
		return ""
	}
	return fmt.Sprintf("[superseded dispatch diagnosis @%.0f] %s",
		m.LastOpAt, m.LastOpReason)
}

// foldCommandResult folds ONE warden command_result receipt onto the
// addressed member's last_op* fields (handlers._fold_command_result).
// Fail-safe: a missing/blank member_id or an unknown member is ignored; any
// storage fault is logged and swallowed (an observation fold must never 500).
func (s *apiServer) foldCommandResult(commandResult map[string]any, trigger string) {
	// T-9ccf: a worker receipt keys on worker_id (a worker has no roster member) —
	// route it to the worker fold FIRST. The warden sends exactly one id per
	// receipt (command.go), so worker_id present ⇒ this is a worker receipt.
	workerIDRaw, _ := commandResult["worker_id"].(string)
	if workerID := strings.TrimSpace(workerIDRaw); workerID != "" {
		s.foldWorkerCommandResult(workerID, commandResult, trigger)
		return
	}
	memberIDRaw, _ := commandResult["member_id"].(string)
	memberID := strings.TrimSpace(memberIDRaw)
	if memberID == "" {
		return
	}
	m, err := s.dal.GetMember(memberID)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] command_result fold failed for member %q: %v\n", memberID, err)
		return
	}
	if m == nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] command_result for unknown member %q — ignored\n", memberID)
		return
	}
	// P5b convergence: a worker start/stop now rides the member verbs, so its
	// receipt arrives keyed member_id == the ow- id. Route it to the worker fold
	// (PutOutsourceWorker + the owner-only outsource_worker delta) — never the
	// member putMember fold, whose member-topic fan-out would leak an outsource
	// row onto the staff roster wire.
	if m.Kind == KindOutsource {
		s.foldWorkerCommandResult(memberID, commandResult, trigger)
		return
	}
	rpc, _ := commandResult["rpc"].(string)
	logText, isLog := commandResult["log"].(string)
	if !isLog {
		logText, _ = commandResult["reason"].(string)
	}
	if len(logText) > commandResultLogMax {
		logText = logText[:commandResultLogMax]
	}
	// The structured cause ("<code>: <detail>" — SpawnOutcome.Reason), persisted
	// SEPARATELY from the log so the owner-facing 最近操作 block can show a
	// one-line WHY without parsing the log dump. A receipt without one (old
	// warden / successful op) folds "" — the FE then shows status-only as before.
	reason, _ := commandResult["reason"].(string)
	if len(reason) > commandResultReasonMax {
		reason = reason[:commandResultReasonMax]
	}
	var okPtr *bool
	if ok, isBool := commandResult["ok"].(bool); isBool {
		okPtr = &ok
	}
	// T-9adc: a NO-OP stop receipt (idempotent ok over a session that was never
	// there) must not overwrite last_op — get_member's 最近操作 must reflect
	// what actually HAPPENED, not what one session-less warden politely 200'd.
	if isStopNoopReceipt(rpc, okPtr, reason) {
		fmt.Fprintf(os.Stderr,
			"[monitoring] no-op stop receipt for member %q (%s) — last_op NOT folded\n",
			memberID, reason)
		return
	}
	// T-66a2: the five last_op* fields are ONE slot with TWO blind writers —
	// this fold (an EXECUTION outcome: the machine received the order and acted)
	// and stampWakeObservability (a DISPATCH-level diagnosis: nothing ever came
	// back). The second is the clue that decides whether to go look at that
	// machine at all, and until now the next spawn receipt erased it outright:
	// not archived, not superseded, just gone in one putMember. The receipt is
	// genuinely newer and must still win the slot — but the clue it displaces is
	// carried into last_op_log rather than destroyed. Prefixed, not appended, so
	// it survives the commandResultLogMax clamp of a long log dump; bounded to
	// ONE hop because the new last_op_reason is the receipt's own, so the next
	// fold has no dispatch diagnosis left to carry.
	if clue := supersededDispatchClue(*m); clue != "" {
		logText = clue + "\n" + logText
		if len(logText) > commandResultLogMax {
			logText = logText[:commandResultLogMax]
		}
		fmt.Fprintf(os.Stderr,
			"[monitoring] member %q: %s receipt supersedes a dispatch diagnosis — "+
				"carried into last_op_log (%s)\n", memberID, rpc, m.LastOpReason)
	}
	m.LastOp = rpc
	m.LastOpOK = okPtr
	m.LastOpLog = logText
	m.LastOpReason = reason
	m.LastOpAt = commandResultAtEpoch(commandResult["at"])
	// UNINSTALL CONVERGENCE: an ok uninstall receipt folds the machine
	// lifecycle intent back to offline (record kept — re-installable).
	if m.LastOp == "uninstall" && m.LastOpOK != nil && *m.LastOpOK {
		m.DesiredState = DesiredStateOffline
	}
	if err := s.putMember(*m, trigger); err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] command_result fold failed for member %q: %v\n", memberID, err)
	}
}

// foldWorkerCommandResult folds ONE warden worker command_result receipt
// (worker_start / worker_stop, T-9ccf) onto the addressed outsource_worker
// row's last_op* fields — the worker twin of foldCommandResult's member fold,
// reusing the SAME clamps and three-valued ok. Fail-safe: an unknown worker or
// any storage fault is logged and swallowed (an observation fold must never
// 500), and it fans an owner-only outsource_worker delta so the cockpit sees
// the fresh reason immediately. It deliberately does NOT touch lifecycle
// (status / released_ts) — a receipt is an observation, never a state change.
//
// Holds s.outsourceMu for the whole read-modify-write-publish: the worker row is
// also read-modify-written by notifyWorkerSpawn (the spawn stamp) under the same
// lock, so without it the two full-row upserts race and the later write silently
// clobbers the earlier (a spawn stamp could erase a just-folded failure reason,
// or vice versa — the "失敗可見" DoD's exact hazard). The telemetry HTTP handler
// that reaches here holds no scheduler lock, so acquiring it is deadlock-free.
func (s *apiServer) foldWorkerCommandResult(workerID string, commandResult map[string]any, trigger string) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()

	w, err := s.dal.GetOutsourceWorker(workerID)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] worker command_result fold failed for %q: %v\n", workerID, err)
		return
	}
	if w == nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] worker command_result for unknown worker %q — ignored\n", workerID)
		return
	}
	rpc, _ := commandResult["rpc"].(string)
	logText, isLog := commandResult["log"].(string)
	if !isLog {
		logText, _ = commandResult["reason"].(string)
	}
	if len(logText) > commandResultLogMax {
		logText = logText[:commandResultLogMax]
	}
	reason, _ := commandResult["reason"].(string)
	if len(reason) > commandResultReasonMax {
		reason = reason[:commandResultReasonMax]
	}
	var okVal *bool
	if ok, isBool := commandResult["ok"].(bool); isBool {
		v := ok
		okVal = &v
	}
	// T-9adc: a NO-OP stop receipt never overwrites the worker's last_op —
	// same honesty rule as the member fold (identity sweeps broadcast stop to
	// every other warden; their polite idempotent OKs are not kill evidence).
	if isStopNoopReceipt(rpc, okVal, reason) {
		fmt.Fprintf(os.Stderr,
			"[monitoring] no-op stop receipt for worker %q (%s) — last_op NOT folded\n",
			workerID, reason)
		return
	}
	w.LastOp = rpc
	w.LastOpOK = okVal
	w.LastOpLog = logText
	w.LastOpReason = reason
	w.LastOpAt = commandResultAtEpoch(commandResult["at"])
	if err := s.dal.PutOutsourceWorker(*w); err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] worker command_result fold failed for %q: %v\n", workerID, err)
		return
	}
	// DoD② 換機: a REFUSED start means the last spawn target could not boot
	// this worker (RAM/creds/ghost) — bench that machine for it so the next
	// re-spawn rotates to a different warden instead of re-picking the same bad
	// one. The target comes from the in-memory spawn map (notifyWorkerSpawn
	// stamped it under this same lock; durable spawn columns retired in P7d).
	// Both the converged member verb (`start`, P5b) and the legacy worker verb
	// (an old warden in the transition window) count.
	if (rpc == reconcileCmdStart || rpc == legacyWardenCmdWorkerStart) &&
		okVal != nil && !*okVal {
		s.benchWorkerMachine(w.ID, s.workerSpawnTarget[w.ID], nowSecs())
	}
	s.publishOutsourceWorker(*w, trigger)
}

// POST /api/monitoring/telemetry — ingest one warden telemetry report:
// partial-report MERGE onto the in-memory entry; an all-absent body or a
// wrong-typed field is a flat 400 (never 422); command_result additionally
// folds durably onto the addressed member.
func (s *apiServer) HandleIngestTelemetryApiMonitoringTelemetryPost(w http.ResponseWriter, r *http.Request) {
	var body AgentTelemetryIngestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.RateLimits == nil && body.Tokens == nil && body.Hardware == nil &&
		body.Binaries == nil && body.Claude == nil && body.Cost == nil &&
		body.Effort == nil && body.Runtime == nil && body.Runtimes == nil &&
		body.SelfUpdate == nil && body.CommandResult == nil {
		writeError(w, http.StatusBadRequest,
			"rate_limits, tokens, hardware, binaries, claude, cost, effort, runtime, runtimes, "+
				"self_update or command_result is required")
		return
	}
	asObject := func(v any, name string) (map[string]any, bool) {
		if v == nil {
			return nil, true
		}
		obj, ok := v.(map[string]any)
		if !ok {
			writeError(w, http.StatusBadRequest, name+" must be an object")
			return nil, false
		}
		return obj, true
	}
	// hardware / claude / runtimes DECLARE their nested shape in the frozen spec
	// (T-90be), so codegen types them as *map[string]interface{} instead of the
	// bare interface{} the undeclared blocks still get. The declaration is what
	// lets the CI guard see a nested rename; it deliberately does NOT close the
	// object (additionalProperties stays true), so an undeclared nested key is
	// still accepted and stored exactly as before — a warden that grows a probe
	// must never have its WHOLE report refused (that is the a7fa594 outage).
	// Dereferencing here keeps every downstream line reading the same
	// map[string]any it always did.
	declaredObject := func(p *map[string]any) map[string]any {
		if p == nil {
			return nil
		}
		return *p
	}
	rateLimits, ok := asObject(body.RateLimits, "rate_limits")
	if !ok {
		return
	}
	tokens, ok := asObject(body.Tokens, "tokens")
	if !ok {
		return
	}
	hardware := declaredObject(body.Hardware)
	binaries, ok := asObject(body.Binaries, "binaries")
	if !ok {
		return
	}
	claude := declaredObject(body.Claude)
	runtimes := declaredObject(body.Runtimes)
	for name, raw := range runtimes {
		if !ValidRuntime(name) {
			writeError(w, http.StatusBadRequest, "runtimes keys must be 'claude' or 'codex'")
			return
		}
		capability, isObj := raw.(map[string]any)
		if !isObj {
			writeError(w, http.StatusBadRequest, "runtimes."+name+" must be an object")
			return
		}
		if v, exists := capability["installed"]; exists {
			if _, valid := v.(bool); !valid {
				writeError(w, http.StatusBadRequest, "runtimes."+name+".installed must be a boolean")
				return
			}
		}
		if v, exists := capability["logged_in"]; exists && v != nil {
			if _, valid := v.(bool); !valid {
				writeError(w, http.StatusBadRequest, "runtimes."+name+".logged_in must be a boolean or null")
				return
			}
		}
		if v, exists := capability["version"]; exists && v != nil {
			if _, valid := v.(string); !valid {
				writeError(w, http.StatusBadRequest, "runtimes."+name+".version must be a string or null")
				return
			}
		}
	}
	var runtime *string
	if body.Runtime != nil {
		text, isStr := body.Runtime.(string)
		if !isStr || !ValidRuntime(text) {
			writeError(w, http.StatusBadRequest, "runtime must be 'claude' or 'codex'")
			return
		}
		runtime = &text
	}
	var cost *float64
	if body.Cost != nil {
		n, isNum := body.Cost.(float64)
		if !isNum {
			writeError(w, http.StatusBadRequest, "cost must be a number")
			return
		}
		cost = &n
	}
	var effort *string
	if body.Effort != nil {
		text, isStr := body.Effort.(string)
		if !isStr {
			writeError(w, http.StatusBadRequest, "effort must be a string")
			return
		}
		effort = &text
	}
	selfUpdate, ok := asObject(body.SelfUpdate, "self_update")
	if !ok {
		return
	}
	commandResult, ok := asObject(body.CommandResult, "command_result")
	if !ok {
		return
	}

	agentID := currentActor(r)
	entry := s.telemetry.Get(agentID)
	if entry == nil {
		entry = map[string]any{}
	}
	if body.RateLimits != nil {
		entry["rate_limits"] = rateLimits
		entry["rate_limits_ts"] = nowSecs()
	}
	if body.Tokens != nil {
		entry["tokens"] = tokens
	}
	if body.Hardware != nil {
		entry["hardware"] = hardware
		// Stamp WHEN this hardware sample was taken, separately from the entry's
		// `ts`. The entry ts advances on EVERY report — a command_result receipt or
		// an identity-only heartbeat carries no hardware, yet would refresh `ts` and
		// make a long-dead CPU reading look like it arrived a second ago. The read
		// path's freshness verdict must be about the hardware sample itself, so it
		// gets its own stamp (same shape as the gauge's context_pct_ts).
		entry["hardware_ts"] = nowSecs()
	}
	if body.Binaries != nil {
		entry["binaries"] = binaries
	}
	if body.Claude != nil {
		entry["claude"] = claude
	}
	if body.Runtimes != nil {
		entry["runtimes"] = runtimes
		// Same per-sample stamp as hardware_ts, same reason (T-b36a): the entry
		// ts advances on every report, so a receipt carrying no capability probe
		// would make an arbitrarily old "codex not logged in" look freshly
		// measured. Placement (machineSupportsRuntime) deliberately does NOT
		// consult this — expiring the map there would silently reclassify a quiet
		// machine as a legacy warden and hand it Claude work; freshness here is a
		// question about what the COCKPIT may present as current.
		entry["runtimes_ts"] = nowSecs()
	}
	if runtime != nil {
		entry["runtime"] = *runtime
	}
	if cost != nil {
		entry["cost"] = *cost
	}
	if effort != nil {
		entry["effort"] = *effort
	}
	if selfUpdate != nil {
		entry["self_update"] = selfUpdate
		fmt.Fprintf(os.Stderr,
			"[monitoring] warden self-update: agent=%s binary=%v %v->%v at=%v\n",
			agentID, orUnknown(selfUpdate["binary"]), orUnknown(selfUpdate["old_hash"]),
			orUnknown(selfUpdate["new_hash"]), orUnknown(selfUpdate["at"]))
	}
	if commandResult != nil {
		entry["command_result"] = commandResult
	}
	// Machine attribution comes from AUTH first (the token's machine_id
	// placement claim — caller-identity-convention.md: facts derive from the
	// verified token, not a self-report). The payload machine is only a
	// fallback for claim-less tokens (/api/mint long-lived tokens and
	// outsource-worker tokens mint machine_id "none" by design; a member
	// without desired_machine_id boots claim-less too).
	if claim := currentMachineClaim(r); claim != "" {
		entry["machine"] = claim
	} else if machine, isStr := body.Machine.(string); isStr && machine != "" {
		entry["machine"] = machine
	}
	// Account keys belong to a runtime-specific identity space, so the key, its
	// provenance stamp and its reporter label move as ONE unit through the
	// partial merge — see applyAccountReport (account_display.go) for the three
	// fail-closed rules that keep the pairing true across the whole sequence of
	// reports, not just the happy path.
	applyAccountReport(entry, body.Account, body.AccountLabel, runtime)
	entry["ts"] = nowSecs()
	s.telemetry.Set(agentID, entry)
	// No agent consumes the monitoring signal on the wire; owner cockpit only.
	s.hub.Publish("monitoring", "signal", "monitoring", agentID, nil, audienceOwnerOnly(), requestTrigger(r))

	if commandResult != nil {
		s.foldCommandResult(commandResult, requestTrigger(r))
	}

	writeJSON(w, http.StatusOK, agentTelemetryDTO{
		AgentID:       agentID,
		Machine:       entryStr(entry, "machine"),
		Account:       entryStr(entry, "account"),
		RateLimits:    entryObj(entry, "rate_limits"),
		Tokens:        entryObj(entry, "tokens"),
		Hardware:      entryObj(entry, "hardware"),
		Binaries:      entryObj(entry, "binaries"),
		Claude:        entryObj(entry, "claude"),
		Runtime:       entryStr(entry, "runtime"),
		Runtimes:      entryObj(entry, "runtimes"),
		Cost:          entryNum(entry, "cost"),
		Effort:        entryStr(entry, "effort"),
		SelfUpdate:    entryObj(entry, "self_update"),
		CommandResult: entryObj(entry, "command_result"),
		TS:            entry["ts"].(float64),
	})
}

func orUnknown(v any) any {
	if v == nil {
		return "?"
	}
	return v
}

func entryStr(entry map[string]any, key string) *string {
	if s, ok := entry[key].(string); ok {
		return &s
	}
	return nil
}

func entryObj(entry map[string]any, key string) map[string]any {
	obj, _ := entry[key].(map[string]any)
	return obj
}

func entryNum(entry map[string]any, key string) *float64 {
	if n, ok := entry[key].(float64); ok {
		return &n
	}
	return nil
}

// telemetryFreshSecs is how long a reported telemetry SAMPLE stays serveable —
// the hardware snapshot and the runtime capability probes both ride the same
// warden heartbeat, so they get the same window rather than two knobs that can
// disagree about what "recent" means.
//
// The warden heartbeat cadence is 30s (cli/ocwarden: reportThrottle), and a
// heartbeat the server ACCEPTS resets the loop straight back to that cadence, so
// a healthy machine restamps every ~30s plus request latency. 90s = three
// cadences: two heartbeats may be lost (a sleeping laptop, a network blip, a
// server restart mid-cycle) without a healthy machine ever flickering to "no
// data", while the window in which the cockpit can show a number that is no
// longer true is bounded at a minute and a half instead of being unbounded.
//
// Deliberately NOT tied to presence: "the warden's SSE dropped" and "nobody has
// measured this box lately" are different facts, and the second is the one these
// numbers depend on. A machine can be online with a wedged collector, and it can
// be briefly offline with a 5-second-old sample that is still perfectly true.
const telemetryFreshSecs = 90.0

// runtimeCapabilitiesStampOf reads WHEN the entry's capability probe was taken.
// Same fail-closed reading as hardwareStampOf: a map with no stamp has an
// unknown age, and unknown age is not freshness.
func runtimeCapabilitiesStampOf(entry map[string]any) float64 {
	ts, _ := entry["runtimes_ts"].(float64)
	return ts
}

func rateLimitStampOf(entry map[string]any) float64 {
	ts, _ := entry["rate_limits_ts"].(float64)
	return ts
}

func usableRateLimitWindow(raw any, windowSecs, now float64) (map[string]any, float64, bool) {
	window, ok := raw.(map[string]any)
	if !ok {
		return nil, 0, false
	}
	resetAt := parseResetsAt(window["resets_at"])
	if resetAt == nil {
		return nil, 0, false
	}
	elapsedPct := (now - (*resetAt - windowSecs)) / windowSecs * 100
	if elapsedPct < 0 || elapsedPct >= 100 {
		return nil, 0, false
	}
	return window, *resetAt, true
}

// hardwareStampOf reads WHEN the entry's hardware sample was taken. Fail-closed:
// an entry carrying hardware with no hardware_ts has an UNKNOWN age, and unknown
// age is not freshness — it reads as the epoch, i.e. stale. Every writer of
// entry["hardware"] stamps this alongside it (see the ingest handler), so the
// zero case means "written by something that does not know when it measured",
// which must never be presented as a live reading.
func hardwareStampOf(entry map[string]any) float64 {
	ts, _ := entry["hardware_ts"].(float64)
	return ts
}

// monitoringActor is the ONE thing the account/machine value folds need from an
// actor: who it is (telemetry key), what runtime it is currently on (the
// provenance gate's other half — NOT read off the entry, see telemetryAccount),
// where it was observed, what it has already banked, and whether it should be
// COUNTED as an agent present on its box. Members and outsource workers project
// onto it identically; nothing downstream of the projection can tell them
// apart, which is precisely the point (T-fc2f).
type monitoringActor struct {
	id      string
	runtime string
	host    string
	banked  float64
	// countsAsPresentAgent answers ONE narrow question — "should this actor be
	// tallied into machines[].agents?" — and nothing else.
	//
	// ⚠️ NAMED THIS WAY ON PURPOSE. The obvious name, `live`, is a trap: the
	// actors loop below argues at length that a released worker IS still alive
	// and still burning money (SPEC §6.3 keeps its session up to run close-out
	// duties), and that argument is why released workers are included in the
	// COST fold. A field called `live` set to false for exactly those workers
	// would flatly contradict the comment a dozen lines away, and the next
	// reader would reasonably conclude the flag was inverted by mistake.
	//
	// The criterion here is NOT the "is it still spending?" test from the actors
	// loop. Both readings are true at once and they are not in tension:
	//
	//	still spending?          → yes for a released worker  → counts for COST
	//	present as a live agent? → no for a released worker   → not in the TALLY
	//
	// Members are only ever built into `actors` after the handler has filtered
	// RosterStatusRemoved, so they are true by construction. For workers this is
	// `Status != WorkerStatusReleased`. Note that released is the STEADY state
	// for a worker (see the actors loop), so this flag is false for most of a
	// worker's recorded lifetime — it is the common case, not the edge.
	countsAsPresentAgent bool
}

// GET /api/monitoring — the three-section fold (sessions / machines /
// accounts) over the roster + gauge + warden telemetry. NEVER fabricates a
// number: unmeasured stays null / honest-empty.
func (s *apiServer) HandleGetMonitoringApiMonitoringGet(w http.ResponseWriter, r *http.Request) {
	all, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	var members []Member
	for _, m := range all {
		if m.RosterStatus != RosterStatusRemoved {
			members = append(members, m)
		}
	}
	telemetry := s.telemetry.Snapshot()
	gauge := s.gauge.Snapshot()
	// The activity store (T-a1d7) is the THIRD observation snapshot, taken here
	// with the other two so every row in this response reads one coherent
	// moment. Absent entry ⇒ ActivityNever, the honest "nothing was reported".
	activity := s.activitySnapshot()
	now := nowSecs()
	machineNames, err := s.dal.MachineDisplayNames()
	if err != nil {
		internalError(w, err)
		return
	}
	accountNames, err := s.dal.AccountDisplayNames()
	if err != nil {
		internalError(w, err)
		return
	}
	resolveDisplay := func(overlay map[string]string, raw string) string {
		if name := overlay[raw]; name != "" {
			return name
		}
		return raw
	}
	tele := func(memberID string) map[string]any {
		return telemetry[memberID] // nil map reads are safe
	}

	// account_label overlay (T-260e): the freshest reporter-supplied
	// human-readable label per account key (oauthAccount email/org), owner-only
	// (PII gate inside the shared fold — empty for any non-owner caller). Scans
	// the WHOLE telemetry snapshot, so a label reported by an outsource-worker
	// session resolves here too (T-ba6b). The owner-edited alias (accountNames)
	// ALWAYS wins over the reported label (never overwritten).
	acctLabels := accountLabelOverlay(telemetry, s.principalOfRequest(r) == principalOwner)
	// Session rows serve a READABLE name or "" — never the raw credential key
	// (T-ba6b: the raw hash/uuid must not reach the member detail panel, which
	// joins its Claude Account cell from this field). The accounts fold below
	// keeps its raw-key fallback: that row is the aliasing surface.
	resolveSessionAccount := func(raw string) string {
		return resolveAccountDisplay(accountNames, acctLabels, raw)
	}

	// actors = members ∪ LIVE outsource workers. The three VALUE folds below
	// (machine attribution / rate-limit windows / cost) run over THIS list, not
	// over `members` alone — `dal.ListMembers()` is `WHERE kind != 'outsource'`,
	// so a member-only fold cannot see a single outsource session.
	//
	// That was the owner-reported bug (T-fc2f): the accounts overview HAPPILY
	// grew a row for an outsource-held key — the raw-key loop further down scans
	// the WHOLE telemetry snapshot — while machine / cost / five_hour / seven_day
	// all came from folds that had never looked at a worker. A key held by BOTH a
	// member and a worker (seth-m5-claude) hid it for months; a key held ONLY by a
	// worker (eva-m5-claude) rendered as a green card with three dashes.
	//
	// NOT a widening of attribution: telemetryAccount's provenance gate still
	// decides whether each entry's key may be read under that actor's runtime
	// (T-69bc / 2eb6590 — an account must never be borrowed from an older
	// runtime). This only fixes WHICH actors get asked.
	//
	// Members and workers are disjoint by construction (kind != 'outsource' vs
	// kind = 'outsource'), so each actor — and each actor's banked_cost —
	// contributes exactly once.
	// ⚠️ KNOWN, DELIBERATELY NOT ADDRESSED HERE (registered as separate scope).
	// `actors` grows MONOTONICALLY with every task this station has ever run.
	// Two facts combine: ListOutsourceWorkers returns every kind='outsource'
	// member row ever created (released included — row retention IS the audit
	// trail, see its doc comment), and worker telemetry entries are never
	// deleted (the repo's only s.telemetry.Delete is the staff hard-delete in
	// api_roles.go). Nothing prunes either side, so this slice and the telemetry
	// snapshot both grow without bound over the station's lifetime.
	//
	// The `machines` list inherits the same growth: its rows are the host keys
	// minted from this slice, so it grows with the set of boxes that have EVER
	// hosted a worker, not just currently-live ones.
	//
	// 🔴 ATTRIBUTION — GET THIS RIGHT BEFORE YOU "FIX" ANYTHING. That machines
	// growth was introduced when the RELEASED FILTER WAS REMOVED from the worker
	// branch above, which is what first admitted released workers into `actors`.
	// It was NOT introduced by the zero-value mint in the machines fold below.
	// An earlier revision of this comment blamed the mint; that was measured and
	// found WRONG. Go's `hostCounts[host]++` already mints a missing key, so the
	// revision before the mint existed minted exactly the same key set from
	// exactly the same actors — the host-key sets of the two revisions are
	// identical, only the counts differ. The zero-value mint is therefore purely
	// NARROWING (it removes released workers from the tally) and can never widen
	// the machines list.
	//
	// Why the distinction is load-bearing rather than pedantic: someone trying
	// to bound this growth will read the attribution and go delete the mint.
	// Doing that does not shrink anything — it deletes the machines row for a
	// box whose workers have all been released, and takes that account's machine
	// attribution down with it (measured; see the fold below). The lever that
	// actually controls this growth is which workers enter `actors`, and that
	// lever is deliberately set to "all of them" for the reasons above.
	//
	// What I checked: the arithmetic stays CORRECT — acctCost is a sum, and each
	// row contributes its own live+banked exactly once, so no total drifts as
	// the set grows. What I did NOT check: whether this handler's per-request
	// cost (it is O(actors) on every GET /api/monitoring, with a DB read of the
	// full worker table) stays acceptable after months of traffic, nor whether
	// anything downstream assumes the actor set or the machines list is bounded.
	// Do not read the correctness result as a performance result.
	workers, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		internalError(w, err)
		return
	}
	actors := make([]monitoringActor, 0, len(members)+len(workers))
	for _, m := range members {
		actors = append(actors, monitoringActor{
			id: m.ID, runtime: m.Runtime, host: s.observedHost(m), banked: m.BankedCost,
			// `members` is already RosterStatusRemoved-filtered above.
			countsAsPresentAgent: true,
		})
	}
	for _, wk := range workers {
		// ⚠️ NO status filter here, and released workers are DELIBERATELY included.
		//
		// The criterion is: FOLLOW THE TELEMETRY LIFECYCLE, NOT THE ROSTER STATUS.
		// The two loops in this handler must agree about who exists, because one
		// of them (the raw-key loop near the end) MINTS the account row while this
		// one supplies its values. Any actor the raw-key loop can see but this
		// loop skips renders as a green card with dashes — that IS the T-fc2f bug,
		// not a special case of it.
		//
		// So the member side's RosterStatusRemoved filter is NOT a precedent to
		// copy, even though workerStatusFromMember makes released the exact same
		// predicate. It is correct there for a reason that does not hold here:
		// removing a member HARD-DELETES it and calls s.telemetry.Delete
		// (api_roles.go — the only telemetry.Delete in the repo), so its entry is
		// gone and the raw-key loop cannot mint a row either. Both loops agree by
		// construction. A released worker's telemetry is never deleted, so
		// filtering it here makes the two loops disagree.
		//
		// And released is the STEADY STATE for outsource workers, not an edge
		// case: ReleaseWorkersForTask fires on every task close (api_tasks.go
		// closeTask) and on every close-out report (dismissOutsourceWorkersForTask).
		// A filter here would therefore hide almost all outsource spend — the
		// owner-reported eva-m5-claude symptom, restored verbatim.
		//
		// Nor is a released worker even finished: SPEC §6.3 (see closeTask) keeps
		// its SESSION alive on purpose to run the close-out duties, so it is still
		// live and still burning money after the flip.
		//
		// Finally, money already spent is a historical fact. An account's
		// cumulative cost must never JUMP BACKWARDS the instant a task closes; a
		// total that silently shrinks is read as wrong data far more readily than
		// a dash is. Pinned by
		// TestGetMonitoring_ReleasedWorkerSpendStaysInTheAccount.
		actors = append(actors, monitoringActor{
			id:                   wk.ID,
			runtime:              wk.Runtime,
			host:                 s.observedWorkerHost(wk.ID, telemetry[wk.ID]),
			banked:               wk.BankedCost,
			countsAsPresentAgent: wk.Status != WorkerStatusReleased,
		})
	}

	sessions := []monitoringSessionDTO{}
	for _, m := range members {
		entry := tele(m.ID)
		roleName, err := s.memberRoleName(m)
		if err != nil {
			internalError(w, err)
			return
		}
		effort := ""
		if e, ok := entry["effort"].(string); ok {
			effort = e
		}
		// Runtime facts fold through the SAME foldActorRuntime the outsource
		// worker DTO reads (P7b read-path convergence — one fold, two wires).
		rt := foldActorRuntime(entry, gauge[m.ID], m.BankedCost, m.Runtime)
		// Activity (T-a1d7) folds through the SAME deriveActivity the outsource
		// worker DTO reads — one derivation, two wires, exactly like the runtime
		// facts above. `online` is the same single SSE authority PresenceState
		// gets, so the two dimensions can never disagree about liveness.
		online := s.hub.IsOnline(m.ID)
		actState, actSince, actEnd := activityOf(activity[m.ID], online, now)
		sessions = append(sessions, monitoringSessionDTO{
			ID:                  m.ID,
			Name:                m.Name,
			Role:                roleName,
			Runtime:             NormalizeRuntime(m.Runtime),
			Model:               m.Model,
			Effort:              effort,
			Machine:             resolveDisplay(machineNames, s.observedHost(m)),
			Account:             resolveSessionAccount(rt.account),
			Presence:            PresenceState(m, now, online),
			ContextPct:          rt.contextPct,
			CompactionCount:     rt.compactionCount,
			Cost:                rt.cost,
			BankedCost:          rt.bankedCost,
			Tokens:              entryObj(entry, "tokens"),
			ActivityState:       actState,
			WorkingSince:        actSince,
			LastTurnCompletedAt: actEnd,
		})
	}

	// Machines: freshest hardware per OBSERVED host; CPU/RAM point-in-time,
	// never summed.
	hostCounts := map[string]int{}
	hwByHost := map[string]map[string]any{}
	hwTS := map[string]float64{}
	acctByHost := map[string]map[string]bool{}
	// Over `actors`, not `members`: an account observed only on an outsource
	// session must still attribute to the box it is burning on, and a host that
	// carries nothing but workers must still get a row for that account to hang
	// off.
	//
	// ⚠️ The agent count does NOT follow for the same reason. An earlier revision
	// of this comment argued that "a row claiming 0 agents while naming an
	// account observed there would contradict itself" and used that to count
	// released workers. THAT ARGUMENT IS REJECTED — do not reinstate it. The two
	// columns answer different questions and are allowed to disagree:
	//
	//	accounts — money already burned on this box. HISTORY. Includes the dead.
	//	agents   — who is alive on this box right now. PRESENT TENSE. Live only.
	//
	// There is no contradiction in "0 agents, one account": the spend happened,
	// the spender left. Counting released workers would make a machine that has
	// run forty closed-out tasks report forty agents while two are running —
	// a number that misleads the owner, that nobody asked for, and that the
	// member side has never produced (the handler filters RosterStatusRemoved
	// before `actors` is built, so staff have always been counted live-only).
	// Including released workers would be the behaviour CHANGE here; excluding
	// them preserves what `agents` has always meant.
	//
	// THE ONE CASE THIS UNDERCOUNTS, and its exact bound. A released worker does
	// keep its session briefly (SPEC §6.3, close-out duties), so for that window
	// it is a real running process that `agents` does not count. The window is
	// bounded at BOTH ends: dismissOutsourceWorkersForTask reclaims it the
	// moment the close-out report lands, and the outsource tick force-reclaims
	// it at workerReclaimGraceSecs = 120.0 (worker_spawn.go) after release
	// regardless. So the undercount is at most ~120s per worker and then the
	// session is genuinely gone — whereas the overcount from counting released
	// workers is UNBOUNDED and grows with every task the box has ever run. A
	// bounded 2-minute undercount is the better error, which is what makes 0 an
	// acceptable answer rather than merely a defensible one.
	//
	// So the count below increments for LIVE actors only.
	//
	// ⚠️ T-fc2f ALSO WROTE A SEPARATE ZERO-VALUE MINT HERE. It is GONE (T-b89d),
	// and its removal is a proven no-op on the wire — do not reinstate it from
	// the git history without reading this. It existed because `hosts` was
	// derived from the hostCounts key SET, so a box whose workers had all been
	// released needed SOMETHING to put its key in the map. `hosts` now comes
	// from the machine REGISTRY (see the block below), and every registered
	// machine's own warden is unconditionally an actor on its own host key
	// (`observedHost` returns m.ID for kind=warden, no branches) and is
	// unconditionally live (`countsAsPresentAgent: true` for every non-removed
	// member) — so `hostCounts[host]++` already writes a key for every host the
	// registry admits, and the mint could only ever add keys nothing reads.
	// Structural, not empirical: keys(hostCounts) ⊇ registry either way, and
	// the row set is now the registry, so the mint changed no output at all.
	// (`hostCounts` is read with a plain lookup, and a Go map's zero value for
	// an absent host is already the 0 the mint used to write.)
	//
	// Precisely what does and does not change for workers, since "no-op" is too
	// coarse a word for this loop:
	//   - hwByHost/hwTS: genuinely a no-op. The agent-side reporter (cli/ocagent
	//     contextreport `telemetryBody`) has no `hardware` field at all, and the
	//     ingest DTO is additionalProperties:false, so an `ow-` entry can never
	//     carry one. Only the per-machine warden samples hardware.
	//   - hostCounts: a count, and nothing more. Since T-b89d it does NOT decide
	//     whether a row appears — the registry does — so a host it never hears
	//     about still gets a row (reading 0), and a host it counts is dropped
	//     unless the roster knows it.
	//
	// ⚠️ THE HOST KEY SET IS NOT THE ROW SET (T-b89d). What this loop observes —
	// including hosts that are not machines at all ("") and hosts of boxes the
	// owner has since removed — is not what gets listed. Which hosts become
	// machines ROWS is a separate, roster-driven decision made below.
	for _, a := range actors {
		entry := tele(a.id)
		host := a.host
		// Count only the LIVE ones. 0 is an honest answer for a box whose
		// workers have all been released; an inflated count is not.
		if a.countsAsPresentAgent {
			hostCounts[host]++
		}
		if hw, ok := entry["hardware"].(map[string]any); ok {
			// Track the freshest sample per host REGARDLESS of age; whether its
			// numbers may be served is decided per row below. Keeping the stamp
			// for an expired sample is what lets the cockpit say "nobody has
			// measured this box for an hour" instead of showing the same blank
			// row as a box that has never reported hardware at all.
			if ts := hardwareStampOf(entry); ts > 0 {
				if prior, seen := hwTS[host]; !seen || ts > prior {
					hwTS[host] = ts
					hwByHost[host] = hw
				}
			}
		}
		if account := telemetryAccount(entry, a.runtime); account != "" {
			if acctByHost[host] == nil {
				acctByHost[host] = map[string]bool{}
			}
			acctByHost[host][account] = true
		}
	}
	// T-b89d — WHICH BOXES EXIST IS THE ROSTER'S ANSWER, NOT TELEMETRY'S.
	//
	// The loop above supplies VALUES per host; it must not decide MEMBERSHIP.
	// Telemetry is append-only in this repo (the only s.telemetry.Delete is the
	// staff hard-delete in api_roles.go) and a worker's `machine` string is
	// never rewritten when its box goes away, so a row set minted from
	// "whoever reported" is a set that can only ever GROW. Deleting a machine
	// flips its warden member to roster removed (api_machines.go
	// HandleDeleteMachine…) and touches no telemetry at all — so before this,
	// a deleted box kept a machines row FOREVER, resurrected on every request
	// by the released workers that once ran on it. The owner was being shown,
	// permanently, a machine they had removed.
	//
	// `registeredMachines` is the SAME predicate GET /api/machines lists and
	// resolveMachine accepts (kind=warden ∧ roster=active) — deliberately the
	// same one, so the cockpit's machine list and the monitoring machine list
	// can never disagree about what exists. Note the predicate is roster-based,
	// NOT presence-based and NOT uninstall-based.
	//
	// The four boundaries, each decided and each pinned by a test:
	//
	//  1. REGISTERED BUT OFFLINE (nobody running on it, warden not connected) —
	//     ROW STAYS, with honest-null hardware and no accounts. Existence is a
	//     roster fact, not a liveness fact; a laptop that is closed has not
	//     stopped being one of your machines. Falls out of iterating the
	//     registry: nothing has to have been observed for the row to exist.
	//     Pinned: TestGetMonitoring_RegisteredButSilentMachineStillListed.
	//
	//  2. REMOVED (deleted / decommissioned) — ROW GONE, even though its
	//     telemetry lives on. This is the ticket.
	//     Pinned: TestGetMonitoring_RemovedMachineLeavesNoOrphanRow.
	//
	//  3. UNINSTALLED BUT NOT DELETED — ROW STAYS. Uninstall is a one-shot
	//     lifecycle INTENT that keeps the record on purpose ("re-installable",
	//     see the uninstall-convergence fold above); it never touches
	//     roster_status, and GET /api/machines still lists it. Hiding it here
	//     while it is listed there would be the two surfaces disagreeing, which
	//     is the exact failure this predicate was chosen to prevent.
	//     Pinned: TestGetMonitoring_UninstalledButUndeletedMachineStillListed.
	//
	//  4. HOST UNRESOLVED ("") — NO ROW. "" is not a machine id, it is the
	//     absence of one (observedWorkerHost's honest empty), so it can never
	//     be in the registry. This deletes the blank machines row unplaced
	//     actors used to produce — and with it the surprise that an ACCOUNT could
	//     be attributed to a machine that does not exist. The account itself is
	//     NOT lost: acctByHost is untouched, so the accounts section still
	//     carries the key with an honest-empty `machine` cell. "I don't know
	//     where this ran" is a true statement; "it ran on «blank»" is not.
	//     Pinned: TestGetMonitoring_UnplacedActorMintsNoBlankMachineRow.
	//
	// ⚠️ IT TOUCHES THE MACHINES SECTION AND NOTHING ELSE, and that is the whole
	// safety argument. `hostCounts` becomes a pure counter (absent host reads 0)
	// and acctByHost / acctHosts are not consulted here at all — so the accounts
	// section (the surface T-fc2f fixed: an outsource-held key's cost, windows,
	// and machine attribution) is bit-for-bit unaffected, including for a key
	// whose box was later removed. Pinned by
	// TestGetMonitoring_RemovedMachineLeavesNoOrphanRow, which asserts the row
	// is gone and the account is intact in the same body.
	//
	// ⚠️ Do NOT "restore" `hosts` to the observed host set as a way of showing
	// more boxes. It shows exactly one class of extra box — the ones that no
	// longer exist — and that is the defect this fold was written to remove.
	hosts := make([]string, 0, len(all))
	for _, m := range all {
		if m.Kind == machineKind && m.RosterStatus == RosterStatusActive {
			hosts = append(hosts, m.ID)
		}
	}
	sort.Strings(hosts)
	machines := []monitoringMachineDTO{}
	for _, host := range hosts {
		hw := hwByHost[host]
		accounts := []string{}
		for account := range acctByHost[host] {
			accounts = append(accounts, account)
		}
		sort.Strings(accounts)
		// The host string IS the warden's member id (machines are warden
		// members), so the registry verdicts apply verbatim here.
		claudeVersion, claudeCredSource, claudeSubReadable := s.machineClaudeInfo(host)
		row := monitoringMachineDTO{
			Machine:             host,
			DisplayName:         resolveDisplay(machineNames, host),
			Agents:              hostCounts[host],
			Accounts:            accounts,
			BinStatus:           s.machineBinStatus(host),
			ClaudeVersion:       claudeVersion,
			ClaudeCredSource:    claudeCredSource,
			ClaudeSubReadable:   claudeSubReadable,
			RuntimeCapabilities: s.machineRuntimeCapabilities(host),
			// Honest-empty, never null: the spec types this as a plain array,
			// and "no key is broken" is a real answer that every row can give
			// — including one with no sample at all, which has no key that
			// COULD be broken. Same shape discipline as Accounts.
			HardwareInvalid: []string{},
		}
		// A hardware sample is only SERVEABLE while it is fresh. Telemetry is
		// never cleared on disconnect (only on dismissal), so without this gate a
		// machine that reported once and then went away kept serving its last
		// CPU/RAM/battery numbers forever — beside an "offline" badge, with
		// nothing on the wire saying how old they were. That reads as a confident
		// live measurement and has already been misread. Past the TTL the numbers
		// go back to the SAME honest nulls a machine that never reported hardware
		// serves; the stamp stays on the wire so the two cases remain telling
		// apart. See telemetryFreshSecs for the threshold.
		if ts := hwTS[host]; ts > 0 {
			stamp := ts
			stale := now-ts > telemetryFreshSecs
			row.HardwareTS = &stamp
			// The verdict rides the wire next to the stamp for the same reason
			// runtime_capabilities_stale does: the window lives HERE, and a
			// cockpit that re-derived it from `now - hardware_ts` would be a
			// second home for the threshold, judged against a clock this server
			// has never seen. Without it the only way to render "expired" is to
			// guess from all-null values — which is wrong for a fresh sample
			// whose probes all failed (hardware {} is a legal report).
			row.HardwareStale = &stale
			if hw != nil && !stale {
				row.CpuPct = teleNum(hw["cpu_pct"])
				row.RamPct = teleNum(hw["ram_pct"])
				row.BatteryPct = teleNum(hw["battery_pct"])
				row.ACPower = teleBool(hw["ac_power"])
				// …and SAY when one of those four came back nil because the
				// value was unusable rather than absent. The four readers above
				// are total functions into nil: a string cpu_pct produces the
				// exact same row as a machine that has never had a CPU probe,
				// so without this the cockpit cannot tell a broken reporter
				// from a box with no battery — the failure this whole field
				// exists to end. See monitoringMachineDTO.HardwareInvalid.
				//
				// Scoped to the SERVED sample on purpose. A stale row's blanks
				// already have a published reason (hardware_stale), and a row
				// that is withholding its numbers anyway has no business also
				// passing judgement on values it is not reading; naming keys
				// there would put two competing explanations on one blank cell.
				row.HardwareInvalid = hardwareInvalidKeys(hw)
			}
		}
		// Capability probes carry the same age question with a different answer:
		// the values are KEPT past the window and marked instead of blanked,
		// because "codex was not logged in as of 3h ago" is the only surface that
		// explains a worker parked on machine_unavailable — deleting it would
		// trade one silent screen for another. Only the confidence is withdrawn.
		if entry := s.telemetry.Get(host); entry != nil {
			if ts := runtimeCapabilitiesStampOf(entry); ts > 0 {
				stamp := ts
				stale := now-ts > telemetryFreshSecs
				row.RuntimeCapabilitiesTS = &stamp
				row.RuntimeCapabilitiesStale = &stale
			} else if len(row.RuntimeCapabilities) > 0 {
				// A map of unknown age must not read as current either.
				stale := true
				row.RuntimeCapabilitiesStale = &stale
			}
		}
		machines = append(machines, row)
	}

	// Accounts: current valid rate-limit window per account + Σ(live cost + banked_cost);
	// machine = the observed host set, display-resolved and comma-joined.
	acctHosts := map[string]map[string]bool{}
	for host, accts := range acctByHost {
		for account := range accts {
			if acctHosts[account] == nil {
				acctHosts[account] = map[string]bool{}
			}
			acctHosts[account][host] = true
		}
	}
	freshRL := map[string]map[string]any{}
	rlTS := map[string]map[string]float64{}
	rlResetAt := map[string]map[string]float64{}
	acctCost := map[string]float64{}
	acctHasCost := map[string]bool{}
	// Same `actors` list, same reason: the latest valid rate-limit window and the
	// account's total spend are ACCOUNT-wide facts, and an outsource session
	// burns the same quota and the same money as a member one. The agent-side
	// reporter is identity-agnostic — cli/ocagent's contextreport POSTs
	// rate_limits/cost keyed on nothing but its JWT sub — so a worker's entry
	// carries exactly the same fields a member's does.
	for _, a := range actors {
		entry := tele(a.id)
		account := telemetryAccount(entry, a.runtime)
		if account == "" {
			continue
		}
		if rl, isObj := entry["rate_limits"].(map[string]any); isObj {
			if freshRL[account] == nil {
				freshRL[account] = map[string]any{}
				rlTS[account] = map[string]float64{}
				rlResetAt[account] = map[string]float64{}
			}
			for windowKey, windowSecs := range WindowSeconds {
				window, resetAt, usable := usableRateLimitWindow(rl[windowKey], windowSecs, now)
				if !usable {
					continue
				}
				priorTS, seen := rlTS[account][windowKey]
				if !seen || resetAt > rlResetAt[account][windowKey] ||
					(resetAt == rlResetAt[account][windowKey] && rateLimitStampOf(entry) > priorTS) {
					rlTS[account][windowKey] = rateLimitStampOf(entry)
					rlResetAt[account][windowKey] = resetAt
					freshRL[account][windowKey] = window
				}
			}
		}
		if cost, isNum := entry["cost"].(float64); isNum {
			acctCost[account] += cost
			acctHasCost[account] = true
		}
		// One banked balance per ACTOR. Members and workers are disjoint at the
		// SQL level — ListMembers is `WHERE kind != 'outsource'`, ListOutsourceWorkers
		// is `WHERE kind = 'outsource'`, over the SAME member table — so no row
		// can project into `actors` twice and no balance can be added twice.
		// A key held by both a member and a worker therefore sums two DISTINCT
		// balances. Do not take this paragraph's word for it: the arithmetic is
		// pinned end-to-end by
		// TestGetMonitoring_SharedAccountSumsMemberAndWorkerExactlyOnce, which
		// goes red on both a missing worker and a double-counted one.
		if a.banked != 0 {
			acctCost[account] += a.banked
			acctHasCost[account] = true
		}
	}
	accountKeys := map[string]bool{}
	// An identified account is still useful observability even before Codex has
	// supplied a rate-limit window or a billable-cost estimate.  Keeping it in
	// the fold lets the cockpit show the bound ChatGPT account honestly instead
	// of presenting the misleading "no account usage data" empty state.
	for account := range acctHosts {
		accountKeys[account] = true
	}
	for account := range freshRL {
		accountKeys[account] = true
	}
	for account := range acctCost {
		accountKeys[account] = true
	}
	// The account overview is global owner observability, not a member-account
	// attribution cell. Keep every reported key visible here even when it is
	// deliberately withheld from a mismatched session/machine fold.
	for _, entry := range telemetry {
		if account, _ := entry["account"].(string); account != "" {
			accountKeys[account] = true
		}
	}
	sortedAccounts := make([]string, 0, len(accountKeys))
	for account := range accountKeys {
		sortedAccounts = append(sortedAccounts, account)
	}
	sort.Strings(sortedAccounts)
	accounts := []monitoringAccountDTO{}
	for _, account := range sortedAccounts {
		windows := ShapeWindows(anyOrNil(freshRL[account]), now)
		hostLabels := []string{}
		for host := range acctHosts[account] {
			hostLabels = append(hostLabels, resolveDisplay(machineNames, host))
		}
		sort.Strings(hostLabels)
		var cost *float64
		if acctHasCost[account] {
			rounded := round4(acctCost[account])
			cost = &rounded
		}
		// account_label passthrough: same owner-only overlay as the
		// display_name fold (acctLabels is empty for non-owner callers), so
		// the PII gate is reused verbatim. Absent label → field omitted.
		var accountLabel *string
		if label := acctLabels[account]; label != "" {
			accountLabel = &label
		}
		// Raw-key fallback stays HERE only: the accounts row is where the
		// owner aliases a key, so the key itself is the information.
		displayName := resolveAccountDisplay(accountNames, acctLabels, account)
		if displayName == "" {
			displayName = account
		}
		accounts = append(accounts, monitoringAccountDTO{
			Account:      account,
			AccountLabel: accountLabel,
			DisplayName:  displayName,
			Machine:      strings.Join(hostLabels, ", "),
			Cost:         cost,
			FiveHour:     windows["five_hour"],
			SevenDay:     windows["seven_day"],
		})
	}

	writeJSON(w, http.StatusOK, monitoringDTO{
		Sessions: sessions,
		Machines: machines,
		Accounts: accounts,
	})
}

// anyOrNil widens a possibly-nil typed map to `any` so ShapeWindows sees a
// true nil (a typed nil inside any is not nil to a type switch on map).
func anyOrNil(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}

// round4 mirrors Python round(x, 4) (banker's rounding).
func round4(x float64) float64 {
	return math.RoundToEven(x*10000) / 10000
}
