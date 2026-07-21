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
	s.gauge.Set(agentID, entry)
	// No agent consumes the context signal on the wire (it drives the
	// server-side context-high band, not fan-out); owner cockpit only.
	s.hub.Publish("context", "signal", "context", agentID, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, agentContextDTO{
		AgentID:    agentID,
		ContextPct: pct,
		RateLimits: rateLimits,
		TS:         now,
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
		body.Effort == nil && body.SelfUpdate == nil && body.CommandResult == nil {
		writeError(w, http.StatusBadRequest,
			"rate_limits, tokens, hardware, binaries, claude, cost, effort, "+
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
	rateLimits, ok := asObject(body.RateLimits, "rate_limits")
	if !ok {
		return
	}
	tokens, ok := asObject(body.Tokens, "tokens")
	if !ok {
		return
	}
	hardware, ok := asObject(body.Hardware, "hardware")
	if !ok {
		return
	}
	binaries, ok := asObject(body.Binaries, "binaries")
	if !ok {
		return
	}
	claude, ok := asObject(body.Claude, "claude")
	if !ok {
		return
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
	}
	if body.Tokens != nil {
		entry["tokens"] = tokens
	}
	if body.Hardware != nil {
		entry["hardware"] = hardware
	}
	if body.Binaries != nil {
		entry["binaries"] = binaries
	}
	if body.Claude != nil {
		entry["claude"] = claude
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
	if account, isStr := body.Account.(string); isStr && account != "" {
		entry["account"] = account
	}
	// account_label (T-260e): the reporter's human-readable label for the
	// account key (oauthAccount email/org — PII). Folded into the entry for the
	// OWNER-FACING monitoring fold only; it is deliberately NOT echoed on the
	// agent-readable ingest response below and never joins the stable key.
	if label, isStr := body.AccountLabel.(string); isStr && label != "" {
		entry["account_label"] = label
	}
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

	// Liveness (T-5896): the owner-visible stuck-suspect field, from the SAME
	// pure decideLiveness the reconcile tick logs/pushes with (one source, two
	// consumers). Computed once for the whole roster; the expensive unread scan
	// inside is paid only when some session is already online-silent.
	memberIDs := make([]string, len(members))
	for i := range members {
		memberIDs[i] = members[i].ID
	}
	liveness := s.livenessForMembers(memberIDs, now)

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
		rt := foldActorRuntime(entry, gauge[m.ID], m.BankedCost)
		sig := liveness[m.ID]
		sessions = append(sessions, monitoringSessionDTO{
			ID:         m.ID,
			Name:       m.Name,
			Role:       roleName,
			Model:      m.Model,
			Effort:     effort,
			Machine:    resolveDisplay(machineNames, s.observedHost(m)),
			Account:    resolveSessionAccount(rt.account),
			Presence:   PresenceState(m, now, s.hub.IsOnline(m.ID)),
			ContextPct: rt.contextPct,
			Cost:       rt.cost,
			BankedCost: rt.bankedCost,
			Tokens:     entryObj(entry, "tokens"),
			Stuck:      sig.Stuck,
			IdleSecs:   sig.IdleSecs,
		})
	}

	// Machines: freshest hardware per OBSERVED host; CPU/RAM point-in-time,
	// never summed.
	hostCounts := map[string]int{}
	hwByHost := map[string]map[string]any{}
	hwTS := map[string]float64{}
	acctByHost := map[string]map[string]bool{}
	for _, m := range members {
		entry := tele(m.ID)
		host := s.observedHost(m)
		hostCounts[host]++
		if hw, ok := entry["hardware"].(map[string]any); ok {
			ts, _ := entry["ts"].(float64)
			if prior, seen := hwTS[host]; !seen || ts > prior {
				hwTS[host] = ts
				hwByHost[host] = hw
			}
		}
		if account, ok := entry["account"].(string); ok && account != "" {
			if acctByHost[host] == nil {
				acctByHost[host] = map[string]bool{}
			}
			acctByHost[host][account] = true
		}
	}
	hosts := make([]string, 0, len(hostCounts))
	for host := range hostCounts {
		hosts = append(hosts, host)
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
			Machine:           host,
			DisplayName:       resolveDisplay(machineNames, host),
			Agents:            hostCounts[host],
			Accounts:          accounts,
			BinStatus:         s.machineBinStatus(host),
			ClaudeVersion:     claudeVersion,
			ClaudeCredSource:  claudeCredSource,
			ClaudeSubReadable: claudeSubReadable,
		}
		if hw != nil {
			row.CpuPct = teleNum(hw["cpu_pct"])
			row.RamPct = teleNum(hw["ram_pct"])
			row.BatteryPct = teleNum(hw["battery_pct"])
			row.ACPower = teleBool(hw["ac_power"])
		}
		machines = append(machines, row)
	}

	// Accounts: freshest rate_limits per account + Σ(live cost + banked_cost);
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
	rlTS := map[string]float64{}
	acctCost := map[string]float64{}
	acctHasCost := map[string]bool{}
	for _, m := range members {
		entry := tele(m.ID)
		account, ok := entry["account"].(string)
		if !ok || account == "" {
			continue
		}
		ts, _ := entry["ts"].(float64)
		if rl, isObj := entry["rate_limits"].(map[string]any); isObj {
			if prior, seen := rlTS[account]; !seen || ts > prior {
				rlTS[account] = ts
				freshRL[account] = rl
			}
		}
		if cost, isNum := entry["cost"].(float64); isNum {
			acctCost[account] += cost
			acctHasCost[account] = true
		}
		if m.BankedCost != 0 {
			acctCost[account] += m.BankedCost
			acctHasCost[account] = true
		}
	}
	accountKeys := map[string]bool{}
	for account := range freshRL {
		accountKeys[account] = true
	}
	for account := range acctCost {
		accountKeys[account] = true
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
