package main

// The two ingest stores are IN-MEMORY — restart amnesia is contract
// (lifecycle.md §3). command_result folds durably onto member.last_op*. The
// monitoring fold never fabricates a number.

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Re-clamp: the warden already truncates to 4 KB, but the body is untrusted.
const commandResultLogMax = 4096

// The reason is a one-line "<code>: <detail>" (SpawnOutcome.Reason), not the
// log dump.
const commandResultReasonMax = 512

// MERGES onto the prior entry so the session boot_ts anchor survives.
func (s *apiServer) HandleIngestAgentContextApiAgentContextPost(w http.ResponseWriter, r *http.Request) {
	var body AgentContextIngestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	pct, ok := body.ContextPct.(float64)
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
	// No agent consumes the context signal on the wire (it drives the server-side
	// context-high band); owner cockpit only.
	s.hub.Publish("context", "signal", "context", agentID, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, agentContextReceiptDTO{AgentID: agentID, TS: now})
}

// -1 is the warden's 未量到 sentinel → nil, never a fabricated 0.
func teleNum(value any) *float64 {
	n, ok := value.(float64)
	if !ok || n < 0 {
		return nil
	}
	return &n
}

func teleBool(value any) *bool {
	b, ok := value.(bool)
	if !ok {
		return nil
	}
	return &b
}

// Mirrors the frozen spec's AgentTelemetryIngestDTO.hardware types. Only
// DECLARED keys are judged: additionalProperties stays true (owner ruling
// rc-55861dd893c6), so an undeclared probe from a newer warden is never a
// defect.
var declaredHardwareTypes = map[string]string{
	"cpu_pct":     "number",
	"ram_pct":     "number",
	"battery_pct": "number",
	"ac_power":    "boolean",
}

// Absent, null and negative (the -1 sentinel) are real answers, not invalid.
// Only a wrong JSON type is — it is accepted with a 200, stored, and would
// otherwise read back as a silent null forever.
func hardwareInvalidKeys(hw map[string]any) []string {
	invalid := []string{}
	for key, want := range declaredHardwareTypes {
		value, present := hw[key]
		if !present || value == nil {
			continue
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

// "at" is RFC3339 from the warden; garbage → 0.0 so a bad timestamp can never
// shortcut presence.
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

// Cross-module contract with cli/ocwarden/command.go (rpcStop/rpcWorkerStop):
// the reason prefix of an idempotent NO-OP stop — nothing was killed.
const stopNoopReasonPrefix = "no_such_session"

// A no-op stop proves only that ONE warden held no session (identity sweeps
// broadcast stop to every warden), so folding it over last_op would forge a
// "successfully stopped" story. Callers SKIP the last_op fold for these.
func isStopNoopReceipt(rpc string, ok *bool, reason string) bool {
	if !isStopRPC(rpc) {
		return false
	}
	if ok == nil || !*ok {
		return false
	}
	return strings.HasPrefix(reason, stopNoopReasonPrefix)
}

func isStopRPC(rpc string) bool {
	return rpc == "stop" || rpc == "worker_stop"
}

// Written by stampWakeObservability (reconcile.go) when a START lapsed its start
// window — the only dispatch-level writer of last_op_reason.
const wakeTimeoutReasonCode = "wake_timeout"

// last_op* is ONE slot with two writers: this fold (execution outcome) and
// stampWakeObservability (dispatch diagnosis). The receipt wins the slot, but a
// displaced diagnosis is carried into last_op_log — in place, because the wire
// is frozen (AGENTS.md §13).
func supersededDispatchClue(m Member) string {
	if !strings.HasPrefix(m.LastOpReason, wakeTimeoutReasonCode+":") {
		return ""
	}
	return fmt.Sprintf("[superseded dispatch diagnosis @%.0f] %s",
		m.LastOpAt, m.LastOpReason)
}

// warden refusal code → the runtime it could not find logged in
var wardenLoginRefusalRuntime = map[string]string{
	"claude_not_logged_in": RuntimeClaude,
	"codex_not_logged_in":  RuntimeCodex,
}

func isWardenLoginRefusal(reason string) bool {
	code, _, found := strings.Cut(reason, ":")
	_, isLogin := wardenLoginRefusalRuntime[code]
	return found && isLogin
}

// loginRefusalNamingMachine rewrites a warden's not-logged-in refusal into the
// sentence placement writes, naming the reporting machine the warden's own text
// cannot; the cockpit localizes that one sentence. The warden's text is kept as
// the log.
//
// Its `at` becomes the server's receipt time: wakeTimeoutYieldsToReceipt compares
// it with the server-clock start anchor, and the warden's own stamp is whole
// seconds on the machine's clock — a refusal of this start would read as older
// than the start and lose to wake_timeout.
func loginRefusalNamingMachine(commandResult map[string]any, reporter string) map[string]any {
	reason := stringOf(commandResult["reason"])
	code, _, found := strings.Cut(reason, ":")
	runtime, isLogin := wardenLoginRefusalRuntime[code]
	if !found || !isLogin {
		return commandResult
	}
	out := make(map[string]any, len(commandResult)+1)
	for k, v := range commandResult {
		out[k] = v
	}
	out["at"] = nowSecs()
	if reporter == "" {
		return out
	}
	if _, hasLog := out["log"].(string); !hasLog {
		out["log"] = reason
	}
	out["reason"] = code + ": machine '" + reporter + "' is not logged in to " + runtime
	return out
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func boolPtrOf(v any) *bool {
	b, isBool := v.(bool)
	if !isBool {
		return nil
	}
	return &b
}

// reporter is the MACHINE that sent this receipt, from the verified token (never
// the payload); trigger is only the SSE attribution string.
func (s *apiServer) foldCommandResult(commandResult map[string]any, trigger, reporter string) {
	// The warden sends exactly one id per receipt (command.go): worker_id
	// present ⇒ worker receipt.
	workerIDRaw, _ := commandResult["worker_id"].(string)
	// Disarm the receipt deadline (receipt_watch.go) BEFORE any early return: an
	// arrived receipt answers the deadline even when the fold declines to write it.
	s.noteReceiptArrived(strings.TrimSpace(workerIDRaw), reporter)
	s.noteReceiptArrived(strings.TrimSpace(memberIDRawOf(commandResult)), reporter)
	// Also before any early return: a no_such_session stop is dropped by both
	// folds, but for the worker-stop retry it is the strongest evidence there is.
	if isStopNoopReceipt(
		stringOf(commandResult["rpc"]), boolPtrOf(commandResult["ok"]),
		stringOf(commandResult["reason"]),
	) {
		s.noteWorkerStopNoSuchSession(strings.TrimSpace(workerIDRaw), reporter)
		s.noteWorkerStopNoSuchSession(
			strings.TrimSpace(memberIDRawOf(commandResult)), reporter)
	}
	if isStopRPC(stringOf(commandResult["rpc"])) {
		if ok := boolPtrOf(commandResult["ok"]); ok != nil && *ok {
			s.noteWorkerStopSucceeded(strings.TrimSpace(workerIDRaw), reporter)
			s.noteWorkerStopSucceeded(
				strings.TrimSpace(memberIDRawOf(commandResult)), reporter)
		}
	}
	commandResult = loginRefusalNamingMachine(commandResult, reporter)
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
	// A worker start/stop rides the member verbs, so its receipt arrives keyed
	// member_id == the ow- id; route it to the worker fold.
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
	reason, _ := commandResult["reason"].(string)
	if len(reason) > commandResultReasonMax {
		reason = reason[:commandResultReasonMax]
	}
	var okPtr *bool
	if ok, isBool := commandResult["ok"].(bool); isBool {
		okPtr = &ok
	}
	if isStopNoopReceipt(rpc, okPtr, reason) {
		fmt.Fprintf(os.Stderr,
			"[monitoring] no-op stop receipt for member %q (%s) — last_op NOT folded\n",
			memberID, reason)
		return
	}
	s.restoreRefusedStartAnchor(memberID, rpc, okPtr, reason)
	// The receipt and, for an ok uninstall, the intent it folds back land together
	// on the row as it stands in the transaction: a command_result is pushed once
	// and never re-sent.
	var folded Member
	converged := false
	err = s.dal.inTx(func(tx *writeTx) error {
		cur, err := getMemberOn(tx, memberID)
		if err != nil || cur == nil {
			return err
		}
		before := *cur
		opLog := logText
		if clue := supersededDispatchClue(*cur); clue != "" {
			opLog = clue + "\n" + opLog
			if len(opLog) > commandResultLogMax {
				opLog = opLog[:commandResultLogMax]
			}
			fmt.Fprintf(os.Stderr,
				"[monitoring] member %q: %s receipt supersedes a dispatch diagnosis — "+
					"carried into last_op_log (%s)\n", memberID, rpc, cur.LastOpReason)
		}
		cur.LastOp = rpc
		cur.LastOpOK = okPtr
		cur.LastOpLog = opLog
		cur.LastOpReason = reason
		cur.LastOpAt = commandResultAtEpoch(commandResult["at"])
		// An ok uninstall folds the lifecycle intent back to offline (record kept).
		converged = cur.LastOp == "uninstall" && cur.LastOpOK != nil && *cur.LastOpOK
		if converged {
			cur.DesiredState = DesiredStateOffline
			if err := writeMemberOn(tx, before, *cur); err != nil {
				return err
			}
		}
		folded = *cur
		return persistMemberOpReceiptOn(tx, *cur)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] command_result fold failed for member %q: %v\n", memberID, err)
		return
	}
	if folded.ID == "" {
		return
	}
	if converged {
		s.publishMemberPatch(folded, trigger)
	}
	s.publishMemberPatch(folded, trigger)
}

// Holds s.outsourceMu for the whole read-modify-write-publish: notifyWorkerSpawn
// read-modify-writes the same row under the same lock, and the later write would
// otherwise silently clobber the earlier. The telemetry handler holds no
// scheduler lock, so this is deadlock-free.
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
	if isStopNoopReceipt(rpc, okVal, reason) {
		fmt.Fprintf(os.Stderr,
			"[monitoring] no-op stop receipt for worker %q (%s) — last_op NOT folded\n",
			workerID, reason)
		return
	}
	s.restoreRefusedStartAnchor(w.ID, rpc, okVal, reason)
	w.LastOp = rpc
	w.LastOpOK = okVal
	w.LastOpLog = logText
	w.LastOpReason = reason
	w.LastOpAt = commandResultAtEpoch(commandResult["at"])
	// Single-column write: a reconcile tick may be writing the same row through
	// its own re-read.
	if err := s.dal.SetMemberLastOp(w.ID, w.LastOp, w.LastOpOK, w.LastOpLog,
		w.LastOpReason, w.LastOpAt); err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] worker command_result fold failed for %q: %v\n", workerID, err)
		return
	}
	// A REFUSED start benches the last spawn target (map stamped by
	// notifyWorkerSpawn under this lock) so the retry pauses instead of hammering
	// it. A session_already_exists refusal is not benched: the old session is still
	// alive.
	if (rpc == reconcileCmdStart || rpc == legacyWardenCmdWorkerStart) &&
		okVal != nil && !*okVal && !strings.HasPrefix(reason, spawnClobberReasonPrefix) {
		s.benchWorkerMachine(w.ID, s.workerSpawnTarget[w.ID], nowSecs())
	}
	s.publishOutsourceWorker(*w, trigger)
}

func (s *apiServer) HandleIngestTelemetryApiMonitoringTelemetryPost(w http.ResponseWriter, r *http.Request) {
	var body AgentTelemetryIngestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.RateLimits == nil && body.Tokens == nil && body.Hardware == nil &&
		body.Binaries == nil && body.Claude == nil && body.Cost == nil &&
		body.Effort == nil && body.Runtime == nil && body.Runtimes == nil &&
		body.SelfUpdate == nil && body.CommandResult == nil && body.WardenShape == nil &&
		body.CutoverEffect == nil && body.ModelCall == nil {
		writeError(w, http.StatusBadRequest,
			"rate_limits, tokens, hardware, binaries, claude, cost, effort, runtime, runtimes, "+
				"self_update, command_result, warden_shape, cutover_effect or model_call is required")
		return
	}
	if body.ModelCall != nil && !validModelCallReport(body.ModelCall) {
		writeError(w, http.StatusBadRequest,
			"model_call.last_failure.kind must be 'auth', 'rate_limit', 'server' or 'other'")
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
	// hardware / claude / runtimes are declared in the frozen spec (so codegen
	// types them *map) but deliberately NOT closed: a warden that grows a probe
	// must never have its whole report refused.
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
	// Refusing an out-of-vocabulary warden_shape is safe (unlike a nested
	// hardware key): a top-level scalar only our own warden produces.
	var wardenShape *string
	if body.WardenShape != nil {
		text, isStr := body.WardenShape.(string)
		if !isStr || !ValidWardenShape(text) {
			writeError(w, http.StatusBadRequest,
				"warden_shape must be 'anchor', 'legacy' or 'unknown'")
			return
		}
		wardenShape = &text
	}
	var cutoverEffect *string
	if body.CutoverEffect != nil {
		text, isStr := body.CutoverEffect.(string)
		if !isStr || !ValidCutoverEffect(text) {
			writeError(w, http.StatusBadRequest,
				"cutover_effect must be 'effective', 'not_effective' or 'unproven'")
			return
		}
		cutoverEffect = &text
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
	var model *string
	if body.Model != nil {
		text, isStr := body.Model.(string)
		if !isStr {
			writeError(w, http.StatusBadRequest, "model must be a string")
			return
		}
		model = &text
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
		// Its own stamp: the entry `ts` advances on EVERY report, including ones
		// with no hardware, and would make a dead reading look fresh.
		entry["hardware_ts"] = nowSecs()
	}
	if body.Binaries != nil {
		entry["binaries"] = binaries
	}
	// Partial-merge: a report with no warden_shape must not clear the stored
	// verdict into the absent case, which means something else.
	if wardenShape != nil {
		entry["warden_shape"] = *wardenShape
	}
	if cutoverEffect != nil {
		entry["cutover_effect"] = *cutoverEffect
	}
	if body.Claude != nil {
		entry["claude"] = claude
	}
	loginFlipped := false
	if body.Runtimes != nil {
		next := loginStatesOf(map[string]any{"runtimes": runtimes})
		// A stale machine's logged-out runtime warns nobody, so its return to
		// fresh telemetry is a change the rows must hear about too.
		wasStale := *runtimeCapabilitiesStale(entry, true, nowSecs())
		loginFlipped = loginStatesDiffer(loginStatesOf(entry), next) || (wasStale && anyLoggedOut(next))
		entry["runtimes"] = runtimes
		// Same per-sample stamp as hardware_ts. Only the logged-out mark
		// (runtimeReportedLoggedOut) reads it, to discount a stale verdict; nothing
		// expires the map on it — placement would then reclassify a quiet machine as
		// a legacy warden and hand it Claude work.
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
	// 🔴 model is deliberately NOT stashed on the entry: its home is the durable
	// actual_model column (stampReportedLaunchFacts). A copy here has no reader
	// and invites the next change to read it, blanking the column fleet-wide on
	// every re-exec.
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
	// Machine attribution comes from the token's machine_id claim first
	// (caller-identity-convention.md). The payload machine is only a fallback
	// for claim-less tokens: /api/mint tokens, outsource-worker tokens, and a
	// member booted without desired_machine_id.
	if claim := currentMachineClaim(r); claim != "" {
		entry["machine"] = claim
	} else if machine, isStr := body.Machine.(string); isStr && machine != "" {
		entry["machine"] = machine
	}
	pairingBefore := modelCallPairingOf(entry)
	applyAccountReport(entry, body.Account, body.AccountLabel, runtime)
	// The account accumulator is fed HERE and nowhere else (T-53, owner ruling
	// rc-5c5d7c7c6dcd), and AFTER applyAccountReport so the increase is credited
	// to the account this report proved.
	s.accrueAccountSpend(entry)
	modelCallChanged := modelCallPairingOf(entry) != pairingBefore
	var storedFailure *modelCallFailureDTO
	if body.ModelCall != nil {
		mayChange := s.modelCallMayChangeWarnings(entry, body.ModelCall)
		var merged bool
		merged, storedFailure = mergeModelCall(entry, body.ModelCall)
		modelCallChanged = modelCallChanged || (merged && mayChange)
	}
	var modelCallBefore map[string]map[string]any
	if modelCallChanged {
		modelCallBefore = s.telemetry.Snapshot()
	}
	entry["ts"] = nowSecs()
	s.telemetry.Set(agentID, entry)
	s.stampReportedLaunchFacts(agentID,
		derefOr(model, ""), derefOr(runtime, ""), derefOr(effort, ""), requestTrigger(r))
	// No agent consumes the monitoring signal on the wire; owner cockpit only.
	s.hub.Publish("monitoring", "signal", "monitoring", agentID, nil, audienceOwnerOnly(), requestTrigger(r))

	if loginFlipped {
		s.publishLoginPairsOn(agentID, requestTrigger(r))
	}
	if modelCallChanged {
		s.publishModelCallChanges(modelCallBefore, s.modelCallClock(), requestTrigger(r))
		if storedFailure != nil {
			s.scheduleModelCallReset(*storedFailure)
		}
	}

	if commandResult != nil {
		s.foldCommandResult(commandResult, requestTrigger(r), receiptReporterMachine(r))
	}

	receipt := agentTelemetryReceiptDTO{
		AgentID: agentID,
		Machine: entryStr(entry, "machine"),
		TS:      entry["ts"].(float64),
	}
	if s.principalOfRequest(r) == principalMachine {
		interval := s.runtimeLoginCheckInterval()
		receipt.LoginCheckIntervalSecs = &interval
		recheck := s.runtimeLoginRecheckInterval()
		receipt.LoginRecheckIntervalSecs = &recheck
	}
	writeJSON(w, http.StatusOK, receipt)
}

// These columns must be durable: s.telemetry is in-memory, so a re-exec would
// blank them fleet-wide, and an offline agent must still show what it LAST ran
// so a pending launch change can be told from an applied one.
//
// Write-on-change (reports arrive ~30s per session; each write is an SSE delta).
// A blank field is a no-op, never an erasure: producers omit what they cannot
// read. An unknown or removed sub is skipped — upserting would create or
// resurrect a roster row. Outsource rows take outsourceMu: the outsource tick
// read-modify-writes the same row.
func (s *apiServer) stampReportedLaunchFacts(agentID, model, runtime, effort, trigger string) {
	model = strings.TrimSpace(model)
	runtime = strings.TrimSpace(runtime)
	effort = strings.TrimSpace(effort)
	if model == "" && runtime == "" && effort == "" {
		return
	}
	m, err := s.dal.GetMember(agentID)
	if err != nil || m == nil || m.RosterStatus == RosterStatusRemoved {
		return
	}
	if m.Kind == KindOutsource {
		s.outsourceMu.Lock()
		defer s.outsourceMu.Unlock()
	}
	// Decided on the row as it stands in the transaction, not on the read above.
	var stamped Member
	err = s.dal.inTx(func(tx *writeTx) error {
		cur, err := getMemberOn(tx, agentID)
		if err != nil || cur == nil || cur.RosterStatus == RosterStatusRemoved {
			return err
		}
		before := *cur
		changed := false
		for _, f := range []struct {
			name     string
			reported string
			column   *string
		}{
			{"actual_model", model, &cur.ActualModel},
			{"actual_runtime", runtime, &cur.ActualRuntime},
			{"actual_effort", effort, &cur.ActualEffort},
		} {
			if f.reported == "" || *f.column == f.reported {
				continue
			}
			*f.column = f.reported
			changed = true
		}
		if !changed {
			return nil
		}
		stamped = *cur
		return writeMemberOn(tx, before, *cur)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] reported launch-fact stamp failed: agent=%s model=%s runtime=%s effort=%s: %v\n",
			agentID, model, runtime, effort, err)
		return
	}
	if stamped.ID != "" {
		s.publishMemberPatch(stamped, trigger)
	}
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

// Three 30s heartbeat cadences: two may be lost without a healthy machine
// flickering to "no data". Deliberately NOT tied to presence.
//
// ⚠️ It also gates the rate-limit pace verdict, and rate_limits for claude comes
// from cli/ocagent's contextreport (reportThrottleSecs 30.0), not the warden
// (reportThrottle 30s): two 30s constants in different Go modules with nothing
// linking them. Raising either would silently withhold verdicts with the suite
// green — if you touch either throttle, come back here.
const telemetryFreshSecs = 90.0

func runtimeCapabilitiesStampOf(entry map[string]any) float64 {
	ts, _ := entry["runtimes_ts"].(float64)
	return ts
}

// runtimeCapabilitiesStale is the verdict served as runtime_capabilities_stale;
// nil = never reported. A map with no stamp predates the stamp and is stale.
func runtimeCapabilitiesStale(entry map[string]any, reported bool, now float64) *bool {
	if ts := runtimeCapabilitiesStampOf(entry); ts > 0 {
		stale := now-ts > telemetryFreshSecs
		return &stale
	}
	if !reported {
		return nil
	}
	stale := true
	return &stale
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

func hardwareStampOf(entry map[string]any) float64 {
	ts, _ := entry["hardware_ts"].(float64)
	return ts
}

type monitoringActor struct {
	id      string
	runtime string
	host    string
	// Deliberately not `live`: a departed actor still attributes its account to
	// its box but is not tallied as present there.
	countsAsPresentAgent bool
}

// `model` is deliberately not a per-kind override: both kinds read
// member.ActualModel off the roster row, one column with one meaning.
type monitoringSessionSource struct {
	member   Member
	host     string
	presence string
}

func (s *apiServer) HandleGetMonitoringApiMonitoringGet(w http.ResponseWriter, r *http.Request) {
	all, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	var members, departed []Member
	for _, m := range all {
		// Asked with the SAME named predicate the lifecycle halves use
		// (lifecycle_roster.go), which puts this handler under the parity test.
		// Not redundant with the worker loop: `all` is the whole member table, and
		// without this `continue` every live contractor enters actors and sources
		// twice (machine card agents N+1, duplicate session row).
		if lifecycleTickDriverFor(m) != driverReconcile {
			continue
		}
		if m.RosterStatus == RosterStatusRemoved {
			departed = append(departed, m)
			continue
		}
		members = append(members, m)
	}
	telemetry := s.telemetry.Snapshot()
	gauge := s.gauge.Snapshot()
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
		return telemetry[memberID]
	}

	// account_label overlay: owner-only (PII gate); the owner-edited alias
	// (accountNames) always wins over the reported label.
	acctLabels := accountLabelOverlay(telemetry, s.principalOfRequest(r) == principalOwner)
	// Session rows serve a readable name or "" — never the raw credential key,
	// which would reach the member detail panel.
	resolveSessionAccount := func(raw string) string {
		return resolveAccountDisplay(accountNames, acctLabels, raw)
	}

	// actors = members ∪ outsource workers. The value folds run over actors, not
	// members: members is driver-filtered and would miss every outsource session.
	//
	// ⚠️ Known, deliberately not addressed here: actors grows monotonically —
	// exits only mark the row, and telemetry is never deleted (only a restart
	// clears it).
	workers, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		internalError(w, err)
		return
	}
	actors := make([]monitoringActor, 0, len(members)+len(workers))
	for _, m := range members {
		actors = append(actors, monitoringActor{
			id: m.ID, runtime: m.Runtime, host: s.observedHost(m),

			countsAsPresentAgent: true,
		})
	}
	// ⚠️ Departed members and released workers are DELIBERATELY included (owner
	// ruling rc-968d6f360cbe): their telemetry outlives the exit, and the raw-key
	// loop at the end still lists its account, so an actor skipped here leaves
	// that account card with no machine and no usage windows.
	for _, m := range departed {
		actors = append(actors, monitoringActor{
			id: m.ID, runtime: m.Runtime, host: s.observedHost(m),
		})
	}
	for _, wk := range workers {
		actors = append(actors, monitoringActor{
			id:                   wk.ID,
			runtime:              wk.Runtime,
			host:                 s.observedWorkerHost(wk.ID, telemetry[wk.ID]),
			countsAsPresentAgent: wk.Status != WorkerStatusReleased,
		})
	}

	// sessions = every live AI session, staff and outsource alike (owner ruling
	// rc-1f8156f25b7a ①), built by one loop. A worker's id is its token sub, so
	// its telemetry is keyed like a member's.
	sources := make([]monitoringSessionSource, 0, len(members)+len(workers))
	for _, m := range members {
		sources = append(sources, monitoringSessionSource{
			member:   m,
			host:     s.observedHost(m),
			presence: PresenceState(m, now, s.hub.IsOnline(m.ID)),
		})
	}
	for _, wk := range workers {
		if wk.Status == WorkerStatusReleased {
			continue
		}
		sources = append(sources, monitoringSessionSource{
			member: memberFromWorker(wk),

			host:     s.observedWorkerHost(wk.ID, telemetry[wk.ID]),
			presence: workerPresence(wk, now, s.hub.IsOnline(wk.ID)),
		})
	}

	sessions := []monitoringSessionDTO{}
	for _, src := range sources {
		m := src.member
		entry := tele(m.ID)
		roleName, err := s.memberRoleName(m)
		if err != nil {
			internalError(w, err)
			return
		}
		effort := m.ActualEffort
		rt := foldActorRuntime(entry, gauge[m.ID], m.BankedCost, m.Runtime)
		sessions = append(sessions, monitoringSessionDTO{
			ID:   m.ID,
			Name: m.Name,
			Role: roleName,
			// Reported, not the owner-configured m.Runtime: a pending switch must
			// not look applied.
			Runtime: m.ActualRuntime,
			// Likewise reported, with no fallback to m.Model for either kind.
			Model:           m.ActualModel,
			Effort:          effort,
			Machine:         resolveDisplay(machineNames, src.host),
			Account:         resolveSessionAccount(rt.account),
			Presence:        src.presence,
			ContextPct:      rt.contextPct,
			CompactionCount: rt.compactionCount,
			Cost:            rt.cost,
			BankedCost:      rt.bankedCost,
			Tokens:          entryObj(entry, "tokens"),
		})
	}

	hostCounts := map[string]int{}
	hwByHost := map[string]map[string]any{}
	hwTS := map[string]float64{}
	acctByHost := map[string]map[string]bool{}
	// Over actors: an account observed only on an outsource session must still
	// attribute to its box. But agents counts LIVE actors only — accounts is
	// history, agents is present tense, and they may disagree.
	for _, a := range actors {
		entry := tele(a.id)
		host := a.host
		if a.countsAsPresentAgent {
			hostCounts[host]++
		}
		if hw, ok := entry["hardware"].(map[string]any); ok {
			// Keep the freshest sample REGARDLESS of age; the stamp of an expired
			// sample lets the cockpit say "not measured for an hour".
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
	// Which boxes exist is the ROSTER's answer (kind=warden ∧ roster active, the
	// same predicate GET /api/machines lists), not telemetry's: telemetry is
	// append-only, so a row set of observed hosts would show a removed machine
	// forever. Do NOT restore hosts to the observed set.
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
			WardenShape:         s.machineWardenShape(host),
			CutoverEffect:       s.machineCutoverEffect(host),
			// Honest-empty, never null: the spec types this as a plain array.
			HardwareInvalid: []string{},
		}
		// A hardware sample is served only while fresh: telemetry is never cleared
		// on disconnect, so an old reading would sit beside an "offline" badge.
		// Past the TTL the numbers go null; the stamp stays so the cases differ.
		if ts := hwTS[host]; ts > 0 {
			stamp := ts
			stale := now-ts > telemetryFreshSecs
			row.HardwareTS = &stamp
			// The verdict rides the wire so the cockpit does not re-derive the
			// threshold (hardware {} is a legal fresh report).
			row.HardwareStale = &stale
			if hw != nil && !stale {
				row.CpuPct = teleNum(hw["cpu_pct"])
				row.RamPct = teleNum(hw["ram_pct"])
				row.BatteryPct = teleNum(hw["battery_pct"])
				row.ACPower = teleBool(hw["ac_power"])
				// Scoped to the SERVED sample: a stale row's blanks already have their
				// reason (hardware_stale).
				row.HardwareInvalid = hardwareInvalidKeys(hw)
			}
		}
		// Capability probes are KEPT past the window and marked stale, not
		// blanked: "codex was not logged in as of 3h ago" is the only surface that
		// explains a worker parked on machine_unavailable.
		if entry := s.telemetry.Get(host); entry != nil {
			if ts := runtimeCapabilitiesStampOf(entry); ts > 0 {
				stamp := ts
				row.RuntimeCapabilitiesTS = &stamp
			}
			row.RuntimeCapabilitiesStale = runtimeCapabilitiesStale(entry, len(row.RuntimeCapabilities) > 0, now)
		}
		machines = append(machines, row)
	}

	// Over the roster's hosts, not acctByHost's: a departed actor's last box may
	// have been removed since, and an account card must not name it.
	acctHosts := map[string]map[string]bool{}
	for _, host := range hosts {
		for account := range acctByHost[host] {
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
	}
	// The account figure is the account's OWN accumulator, not a fold over
	// actors (T-53, owner ruling rc-5c5d7c7c6dcd「分開：帳號卡自己一份數字，清它不動
	// 成員」): it need not equal the members' sum, and an actor leaving does not
	// pull it down.
	accountSpend, err := s.dal.ListAccountSpend()
	if err != nil {
		internalError(w, err)
		return
	}
	for account, spent := range accountSpend {
		if spent > 0 {
			acctCost[account] = spent
			acctHasCost[account] = true
		}
	}
	accountKeys := map[string]bool{}
	// An identified account is listed even before any rate-limit window or cost.
	for account := range acctHosts {
		accountKeys[account] = true
	}
	for account := range freshRL {
		accountKeys[account] = true
	}
	for account := range acctCost {
		accountKeys[account] = true
	}
	// A zeroed account keeps its card: dropping it would read as "gone".
	for account := range accountSpend {
		accountKeys[account] = true
	}
	// Every reported key is listed here, even one withheld from a mismatched
	// session/machine fold.
	for _, entry := range telemetry {
		if account, _ := entry["account"].(string); account != "" {
			accountKeys[account] = true
		}
	}
	calls := newModelCallBoard(all, telemetry, s.modelCallClock())
	sortedAccounts := make([]string, 0, len(accountKeys))
	for account := range accountKeys {
		sortedAccounts = append(sortedAccounts, account)
	}
	sort.Strings(sortedAccounts)
	accounts := []monitoringAccountDTO{}
	for _, account := range sortedAccounts {
		windows := ShapeWindows(anyOrNil(freshRL[account]), now, rlTS[account], telemetryFreshSecs)
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
		var accountLabel *string
		if label := acctLabels[account]; label != "" {
			accountLabel = &label
		}
		// Raw-key fallback only here: this row is where the owner aliases a key.
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
			LimitReached: calls.limit(account),
		})
	}

	writeJSON(w, http.StatusOK, monitoringDTO{
		Sessions: sessions,
		Machines: machines,
		Accounts: accounts,
	})
}

// A typed nil inside `any` is not nil to ShapeWindows' type switch.
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
