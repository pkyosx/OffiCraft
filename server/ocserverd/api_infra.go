package main

// api_infra.go — the two gated infra seams:
//
//   * GET /api/events — the full SSE downlink (spec/sse.md): the auth/RBAC
//     gates, the dual-SSE takeover (kick-old-admit-new; only the anti-flap
//     throttle still answers a pre-stream 409), the `: connected` greeting, the
//     online/machine-claim projection, the buffered delta stream (the hub
//     Publish fan-out), the two directed bands — context-high on any agent
//     connection, warden-command on a kind=="warden" connection — and the
//     15 s quiet-stream heartbeat.
//
//   * POST /api/mcp — the JSON-RPC face (spec/mcp.md): parse errors,
//     initialize/ping, notifications → 202, tools/list from the FROZEN
//     catalog (spec/mcp-catalog.json — the wire SSOT), tools/call params
//     validation + the in-process LOOPBACK (mcp.go): split the arguments,
//     re-enter the route through the app's own mux with the caller's
//     Authorization forwarded, wrap the sub-response as a CallToolResult.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// ── GET /api/events ──────────────────────────────────────────────────────────

// sseHeartbeat keeps the connection warm — the 15 s period is contract
// (spec/sse.md §1); the poll cadence is an implementation detail mirroring
// service/realtime.py.
const (
	sseHeartbeat = 15 * time.Second
	ssePoll      = 250 * time.Millisecond
)

// sseWriteTimeout bounds a single SSE write to the client socket (T-7e07,
// BACKSTOP layer). The PRIMARY half-open reaper is TCP keepalive on the
// accepted connection (server.go sseKeepAlive) — keepalive is what detects a
// silently-vanished peer (no FIN/RST), because a small heartbeat write to such
// a peer just lands in the kernel send buffer and returns success, so a write
// deadline alone would not trip until the buffer fills. This deadline still
// earns its place as a backstop for the OTHER stall: a stuck / zero-window
// consumer whose send buffer HAS filled — there the next write genuinely
// blocks, and the deadline turns it into a prompt write error the loop reaps
// into Disconnect instead of blocking indefinitely. A var (not const) so tests
// can shrink it; 0 disables the deadline. Cross-platform and harmless on a
// healthy stream (tiny frames flush instantly, well under the timeout).
var sseWriteTimeout = 30 * time.Second

func (s *apiServer) HandleEventsApiEventsGet(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	// An AGENT connection projects ITS member online for the life of the
	// connection (the single online-projection path; kind-agnostic — a warden
	// flows through here too). The owner (dashboard) connection is memberID ""
	// (never projected online, exempt from the dual-SSE guard).
	memberID := ""
	machineID := ""
	if currentScope(r) == "agent" {
		memberID = currentActor(r)
		machineID = currentMachineClaim(r)
	}
	// Zombie SSE gate (pre-stream, like the takeover-throttle 409 below): a
	// member the server has an ACTIVE stop record for must never RE-project
	// online by reconnecting — see sseStopGateRefusal for the exact predicate
	// and why each legitimate flow stays admitted. Deliberately checked BEFORE
	// hub.Connect: a stop-in-effect member always gets the 409 and can never
	// take the slot over from anyone (zombie-stop semantics outrank takeover).
	if memberID != "" {
		if msg := s.sseStopGateRefusal(memberID); msg != "" {
			fmt.Fprintf(os.Stderr, "[sse] refused reconnect for %q: %s\n", memberID, msg)
			writeError(w, http.StatusConflict, msg)
			return
		}
	}
	listener, err := s.hub.Connect(memberID, machineID)
	if err != nil {
		// A second listener now TAKES OVER (spec/sse.md §5.1); Connect only
		// refuses when the anti-flap throttle trips (errDualSSEThrottled) —
		// raised PRE-stream so the 409 reaches the client as a proper status.
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if memberID != "" {
		fmt.Fprintf(os.Stderr, "[sse] attach member=%s gen=%d machine=%s\n",
			memberID, listener.Gen, machineID)
		// First-connect edge (spec/sse.md §5.2): the wake completes the instant
		// the agent holds this stream — waking_since is spent exactly once
		// (WakingSince>0 guarded, so a takeover re-fire is a no-op). boot_ts is
		// the SESSION anchor (stamped IFF absent, see onFirstConnect): a
		// mid-session SSE flap (drop → reconnect) must NOT reset it. Session-birth
		// freshness comes from the spawn/stop boundary clearing it
		// (clearSessionBootTS), so a genuinely new session re-stamps here.
		s.onFirstConnect(memberID)
		// Cross-machine single-session enforcement (T-bb29 §1): if this is the
		// 正身 confirmed on its desired machine (claim == desired_machine), reap
		// any residual same-id session on OTHER machines. Fires only after the
		// new session is live here → never a zero-live-session window.
		s.identitySweepOnConnect(memberID, machineID)
	}
	defer func() {
		// last gates the §5.2 edge hooks: a kicked listener's Disconnect
		// reports false (the takeover already removed it; the new listener
		// keeps the member online), so the hooks fire only on the REAL
		// online→offline edge — never mid-takeover.
		last := s.hub.Disconnect(listener)
		if memberID != "" {
			fmt.Fprintf(os.Stderr, "[sse] detach member=%s gen=%d last=%t\n",
				memberID, listener.Gen, last)
		}
		if memberID != "" && last {
			// Last-disconnect edge (spec/sse.md §5.2): bank the live telemetry
			// cost into the durable member exactly once (pop-after-fold makes a
			// re-fired edge idempotent).
			s.onLastDisconnect(memberID)
			// A warden dropping its stream while desired_state=="uninstall" has
			// converged — consume the one-shot intent NOW (reconcile.go), before
			// any re-install could reconnect into a standing kill order.
			s.consumeUninstallOnDisconnect(memberID)
		}
	}()

	// Warden-command eligibility (spec/sse.md §7): the connection drains the
	// command FIFO iff its agent-scope token sub resolves to a member of
	// kind == "warden" — the unforgeable addressing key.
	wardenID := ""
	if memberID != "" {
		if m, err := s.dal.GetMember(memberID); err == nil && m != nil && m.Kind == KindWarden {
			wardenID = memberID
		}
	}

	// Backstop write deadline (T-7e07): arm a fresh deadline before every socket
	// write so a stuck / zero-window consumer whose send buffer has filled fails
	// the blocked write promptly instead of blocking indefinitely. The PRIMARY
	// half-open reaper is TCP keepalive (server.go). ResponseController reaches
	// the underlying net.Conn; a writer that does not support deadlines
	// (httptest recorder) returns ErrNotSupported, which we ignore.
	rc := http.NewResponseController(w)
	armWriteDeadline := func() {
		if sseWriteTimeout > 0 {
			_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
		}
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	armWriteDeadline()
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	// Per-connection context-high band state (spec §6): the remind-bucket
	// marker (T-7826 dedup). In-memory, reset on reconnect, never persisted;
	// bucketReset (below WARN / boot) so the first climb into the band emits.
	chLastBucket := bucketReset

	write := func(frame []byte) bool {
		armWriteDeadline()
		if _, err := w.Write(frame); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	ctx := r.Context()
	lastBeat := time.Now()
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-listener.kicked:
			// Taken over (spec/sse.md §5.1): a newer connection for this member
			// holds the slot — return NOW (≤ssePoll after the kick) and let the
			// defer clean up (Disconnect is a map no-op; last=false keeps the
			// §5.2 edge hooks off while the member stays online).
			return
		default:
		}
		// Buffered entity deltas drain first (publish order per connection).
		if frame := listener.pop(); frame != nil {
			if !write(frame) {
				return
			}
			continue
		}
		// Quiet tick: the context-high band (any agent connection — the agent
		// cannot read its own context %, so the server pushes the reminder).
		if memberID != "" {
			signal, newBucket := decideContextHighSignal(
				memberID, s.gauge.Get(memberID), chLastBucket, s.ctxHighConfig())
			chLastBucket = newBucket
			if signal != nil {
				if frame, err := directedFrameText(contextHighTopic, signal); err == nil {
					if !write(frame) {
						return
					}
					continue
				}
			}
		}
		// Warden-command band (warden connections only): drain the pending
		// FIFO onto THIS connection — never the owner fan-out (the riding
		// member_token is a secret).
		if wardenID != "" {
			if pending := s.hub.DrainWardenCommands(wardenID); len(pending) > 0 {
				for _, frame := range pending {
					if !write(frame) {
						return
					}
				}
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-listener.kicked:
			return // taken over mid-quiet-wait — same cleanup path as above
		case <-time.After(ssePoll):
		}
		if time.Since(lastBeat) >= sseHeartbeat {
			lastBeat = time.Now()
			if !write([]byte(": heartbeat\n\n")) {
				return
			}
		}
	}
}

// sseStopGateRefusal is the zombie SSE gate predicate (defence line B of the
// zombie-agent work; line A is the warden's process-tree sweep). It returns a
// non-empty refusal message when memberID must NOT be admitted to /api/events,
// "" when the connection is fine.
//
// WHY: online is a pure connection projection (spec/sse.md §5), so a zombie
// `ocagent listen` that survived its kill keeps RECONNECTING and re-projecting
// a dead agent online — reconcile then sees desired=offline ∧ observed=online
// forever and the roster is wedged on a fake 綠燈. The gate roots that out at
// the projection seam: while a stop is IN EFFECT the server refuses the
// reconnect (pre-stream 409, the same envelope family as the dual-SSE guard),
// so a zombie can never re-project online. Refusal (not
// "admit-but-don't-project") was chosen deliberately: it starves the zombie
// AND hands it an authoritative signal to fail-closed self-exit on
// (cli/ocagent listen), whereas a projected-less stream would keep feeding a
// dead session deltas and leave the wire ambiguous.
//
// The predicate is deliberately NARROWER than desired_state=="offline" alone:
//
//   - roster removed (any kind): a dismissed member / torn-down warden must
//     never resurrect a presence row.
//   - desired offline ∧ a stop anchor set (stopping_since / stopped_since):
//     "a stop is in effect". A freshly HIRED member is desired-offline with
//     NO anchors and stays admitted (dev runs, conformance scratch agents,
//     pre-activate flows). deactivate / force-stop always stamp
//     stopping_since, so every real take-down is covered.
//   - wardens are exempt from the desired-offline arm: a warden's
//     desired_state is offline BY DEFAULT (dbseed / onboarding) and its
//     removal lifecycle is the one-shot uninstall intent, not this gate.
//
// Legit flows that stay untouched: a LIVE connection at deactivate time keeps
// its stream (the wind-down nudge rides it); stop→start clears the anchors
// and flips desired online in the SAME activate write, so the gate lifts
// atomically; recycle/handover keeps desired online throughout. An unknown
// sub (no roster row) is admitted unchanged.
func (s *apiServer) sseStopGateRefusal(memberID string) string {
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil {
		return "" // fail-open on a read fault/unknown sub: never a new refusal class
	}
	if m.Kind == KindOutsource {
		// Outsource members keep the pre-fold worker admission: a RELEASED
		// worker's session deliberately lives on for its close-out duties
		// (worker_spawn.go reclaim grace), so its SSE must stay admitted even
		// though the row is roster-removed — the member stop gate below would
		// wrongly refuse it. Worker stop intent is enforced by the scheduler's
		// desired_state hold-down, not by this gate.
		return ""
	}
	if m.RosterStatus != RosterStatusActive {
		return "member '" + m.ID + "' is removed from the roster — SSE refused " +
			"(a dismissed member must not re-project online)"
	}
	if m.Kind != KindWarden && parseDesired(m.DesiredState) == DesiredStateOffline &&
		(m.StoppingSince > 0.0 || m.StoppedSince > 0.0) {
		return "member '" + m.ID + "' has a stop in effect (desired_state=offline) — " +
			"SSE refused (a stopped member must not re-project online; " +
			"activate it to reconnect)"
	}
	return ""
}

// onFirstConnect handles the SSE first-connect edge for an agent connection:
// clear the caller's waking anchor (the wake completed) and stamp the session
// boot_ts on its gauge entry. Best-effort — a storage fault must not kill the
// stream that just opened.
//
// boot_ts is stamped ONLY when the gauge has none yet (T-8fb2 boot_ts fix): it
// anchors the SESSION, not the individual connection. A mid-session SSE flap
// (drop → reconnect, no spawn/stop in between) must NOT reset it — otherwise
// the min-liveness gate the three lifecycle paths key on (restart_self
// HandleRestartSelf, context-high auto-recycle stampContextHighRecycle, worker
// auto-handover autoHandoverWorker) keeps seeing "just booted" and an
// edge-flapping agent can neither self-rescue nor be auto-handed-over. A
// genuinely new session (respawn / relocate / recycle) re-stamps because the
// spawn/stop boundary cleared boot_ts first (clearSessionBootTS).
func (s *apiServer) onFirstConnect(memberID string) {
	if m, err := s.dal.GetMember(memberID); err == nil && m != nil && m.WakingSince > 0 {
		m.WakingSince = 0.0
		if err := s.putMember(*m, memberID); err != nil {
			fmt.Fprintf(os.Stderr, "[sse] first-connect waking clear failed for %q: %v\n", memberID, err)
		}
	}
	entry := s.gauge.Get(memberID)
	if entry == nil {
		entry = map[string]any{}
	}
	if _, ok := gaugeBootTS(entry); ok {
		return // session already anchored — a reconnect must not reset it
	}
	entry["boot_ts"] = nowSecs()
	s.gauge.Set(memberID, entry)
}

// clearSessionBootTS drops the boot_ts session anchor from a member's / worker's
// gauge entry at a real session BOUNDARY — a START dispatch that begins a new
// session, or a STOP/kill that ends one. onFirstConnect stamps boot_ts only when
// absent, so clearing here is what makes the next connect re-stamp a fresh
// anchor: "reconnect keeps boot_ts, respawn resets it" (T-8fb2). Best-effort; a
// missing entry or missing key is a clean no-op.
func (s *apiServer) clearSessionBootTS(id string) {
	entry := s.gauge.Get(id)
	if entry == nil {
		return
	}
	if _, ok := entry["boot_ts"]; !ok {
		return
	}
	delete(entry, "boot_ts")
	s.gauge.Set(id, entry)
}

// onLastDisconnect handles the SSE last-disconnect edge for an agent
// connection: fold the live telemetry cost into the actor's durable
// banked_cost, then POP the live field (exactly-once-per-edge banking).
func (s *apiServer) onLastDisconnect(memberID string) {
	s.bankLiveCost(memberID)
}

// bankLiveCost is the ONE cost-banking fold for BOTH actor kinds (T-ba6b —
// owner constitution: 外包＝系統代管的正職員工, so the worker reuses the member
// mechanism instead of a parallel copy): pop the actor's live telemetry cost
// and add it to the durable banked_cost — member.banked_cost or
// outsource_worker.banked_cost, whichever the id resolves to. Callers: the
// SSE last-disconnect edge (both kinds ride the same /api/events surface) and
// every worker kill funnel (respawnWorkerNow / stopWorkerNow — refocus, 換
// model, relocate, stop, auto-handover), so a kill+respawn no longer zeroes
// the owner-visible spend. POP-AFTER-RESOLVE + pop-before-write keeps it
// exactly-once AND loss-free: an id that resolves to neither kind leaves the
// live figure in place (the old member-only fold silently destroyed a
// worker's cost here). Best-effort — a failed write only logs.
func (s *apiServer) bankLiveCost(actorID string) {
	entry := s.telemetry.Get(actorID)
	cost, ok := entry["cost"].(float64)
	if !ok {
		return
	}
	pop := func() {
		delete(entry, "cost")
		s.telemetry.Set(actorID, entry)
	}
	// An outsource member banks through the WORKER branch below (its delta fans
	// on the outsource_worker topic, never as a member patch — pre-fold parity).
	if m, err := s.dal.GetMember(actorID); err == nil && m != nil && m.Kind != KindOutsource {
		pop()
		m.BankedCost += cost
		if err := s.putMember(*m, actorID); err != nil {
			fmt.Fprintf(os.Stderr, "[bank] cost bank failed for member %q: %v\n", actorID, err)
		}
		return
	}
	if w, err := s.dal.GetOutsourceWorker(actorID); err == nil && w != nil {
		pop()
		w.BankedCost += cost
		if err := s.dal.PutOutsourceWorker(*w); err != nil {
			fmt.Fprintf(os.Stderr, "[bank] cost bank failed for worker %q: %v\n", actorID, err)
		}
	}
}

// ── POST /api/mcp ────────────────────────────────────────────────────────────

// JSON-RPC error codes (spec/mcp.md closed set).
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

// mcpProtocolVersion mirrors service.mcp.transport._PROTOCOL_VERSION.
const mcpProtocolVersion = "2025-06-18"

func rpcError(w http.ResponseWriter, id any, code int, message string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
}

func rpcResult(w http.ResponseWriter, id any, result any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

// mcpCatalogTools loads the FROZEN tool catalog (spec/mcp-catalog.json — the
// committed wire SSOT the Python tools/list serves byte-equal descriptors of).
// Kept as the tools/list DESCRIPTOR source on purpose: spec/mcp.md §4 makes
// byte-equality against the snapshot the contract (derivation mechanism free),
// and deriving the inputSchema bodies statically in Go would duplicate every
// DTO schema — a second drifting list. The tool NAME surface (tools/call
// routing + catalog_hash) IS table-derived (mcp.go mcpToolIndex), and the
// conformance suite pins snapshot ≡ live list ≡ table order, so the two views
// cannot drift silently. Disk-first with the bindist embed as the
// single-binary fallback (assets.go).
func (s *apiServer) mcpCatalogTools() ([]any, error) {
	raw, err := s.root.readMCPCatalogFrom(bindistFS())
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Tools []any `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	return catalog.Tools, nil
}

func (s *apiServer) HandleMcpApiMcpPost(w http.ResponseWriter, r *http.Request) {
	var payload any
	dec := json.NewDecoder(r.Body)
	// UseNumber keeps request numbers as their JSON literals — the id echoes
	// back unmangled and tools/call argument splitting renders "3" vs "3.0"
	// exactly as received (Python-side str() parity).
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		rpcError(w, nil, rpcParseError, "parse error: body is not valid JSON")
		return
	}
	obj, isObj := payload.(map[string]any)
	if !isObj {
		rpcError(w, nil, rpcInvalidRequest, "invalid request: expected a JSON object")
		return
	}
	method := obj["method"]
	id, hasID := obj["id"]
	methodName, methodIsStr := method.(string)
	if !methodIsStr {
		rpcError(w, id, rpcInvalidRequest, "invalid request: method must be a string")
		return
	}
	// A notification (no id, or the notifications/* namespace) gets no
	// response body — acknowledge with a bodyless 202.
	if !hasID || strings.HasPrefix(methodName, "notifications/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("null"))
		return
	}

	switch methodName {
	case "initialize":
		requestedVersion := mcpProtocolVersion
		if params, isMap := obj["params"].(map[string]any); isMap {
			if v, isStr := params["protocolVersion"].(string); isStr && v != "" {
				requestedVersion = v
			}
		}
		rpcResult(w, id, map[string]any{
			"protocolVersion": requestedVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "officraft", "version": appVersion},
		})
		return

	case "ping":
		rpcResult(w, id, map[string]any{})
		return

	case "tools/list":
		tools, err := s.mcpCatalogTools()
		if err != nil {
			rpcError(w, id, rpcInternalError, "catalog unavailable: "+err.Error())
			return
		}
		rpcResult(w, id, map[string]any{"tools": tools})
		return

	case "tools/call":
		params, isMap := obj["params"].(map[string]any)
		if !isMap {
			rpcError(w, id, rpcInvalidParams, "invalid params: expected an object")
			return
		}
		name, nameIsStr := params["name"].(string)
		if !nameIsStr {
			rpcError(w, id, rpcInvalidParams, "invalid params: name must be a string")
			return
		}
		arguments := map[string]any{}
		if args, present := params["arguments"]; present && args != nil {
			argsObj, isObjArgs := args.(map[string]any)
			if !isObjArgs {
				rpcError(w, id, rpcInvalidParams, "invalid params: 'arguments' must be an object")
				return
			}
			arguments = argsObj
		}
		spec, known := s.mcpTools[name]
		if !known {
			rpcError(w, id, rpcInvalidParams, "unknown tool: '"+name+"'")
			return
		}
		s.fillLessonsIdentityArgs(r, name, arguments)
		reqPath, rawQuery, body := splitToolArguments(spec, arguments)
		status, raw, err := s.loopbackCall(r, spec.Method, reqPath, rawQuery, body)
		if err != nil {
			rpcError(w, id, rpcInternalError, "tool call failed: "+err.Error())
			return
		}
		rpcResult(w, id, callToolResult(status, raw))
		return
	}

	rpcError(w, id, rpcMethodNotFound, "method not found: '"+methodName+"'")
}
