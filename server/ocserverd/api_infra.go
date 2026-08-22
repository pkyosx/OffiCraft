package main

// api_infra.go — the two gated infra seams:
//
//   * GET /api/events — the full SSE downlink (spec/sse.md): the auth/RBAC
//     gates, the dual-SSE takeover (kick-old-admit-new; only the anti-flap
//     throttle still answers a pre-stream 409), the `: connected` greeting, the
//     online/machine-claim projection, the buffered delta stream (the hub
//     Publish fan-out), the directed bands — context-high and token-expiry on
//     restartable agent connections, warden-command on a kind=="warden"
//     connection — and the
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

	// These values are part of the operator-facing detach log contract. Keep
	// them exact and stable: the log is how an operator separates a normal
	// peer drop from a takeover, a failed write, or the station itself closing.
	sseDetachReasonTakeover        = "takeover"
	sseDetachReasonPeerClosed      = "peer-closed"
	sseDetachReasonWriteFailed     = "write-failed"
	sseDetachReasonStationShutdown = "station-shutdown"

	// sseStationSHAHeader carries this station's build sha to the client when
	// the stream opens (T-5b83), so ocagent's connection line can name the
	// build it just attached to.
	//
	// 🔴 THIS STRING IS HALF OF A CROSS-MODULE CONTRACT and the modules cannot
	// import each other. The other half is stationSHAHeader in
	// cli/ocagent/listen.go. A typo does NOT fail loudly — the client's
	// Header.Get returns "" and its connection line silently omits the sha,
	// which is byte-identical to the honest "this station sent none". If the
	// two halves drift apart, nothing turns red on its own — see the task note
	// for the guard this still owes.
	sseStationSHAHeader = "X-Officraft-Station-Sha"
)

// markStationShutdown records the process-level cause before the server
// cancels request contexts or the upgrade re-execs. Without this ordering a
// server shutdown is indistinguishable from a peer FIN/RST inside an SSE
// handler.
func (s *apiServer) markStationShutdown() {
	s.stationShuttingDown.Store(true)
}

func (s *apiServer) clearStationShutdown() {
	s.stationShuttingDown.Store(false)
}

func (s *apiServer) cancelStationContext() {
	if s.stationCancel != nil {
		s.stationCancel()
	}
}

func (s *apiServer) sseContextDetachReason() string {
	if s.stationShuttingDown.Load() {
		return sseDetachReasonStationShutdown
	}
	return sseDetachReasonPeerClosed
}

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
	// hub.Connect, so a member the gate REFUSES can never take the slot over
	// from anyone (zombie-stop semantics outrank takeover).
	//
	// ⚠️ Read "refuses", not "has a stop anchor": since T-a9d6 a close-out in
	// flight is admitted on purpose and therefore DOES reach hub.Connect with
	// ordinary takeover semantics. The sentence that used to sit here said a
	// stop-in-effect member "always" gets the 409, which this ticket's own
	// change made false — the exact species of stale self-description it exists
	// to remove (independent review caught it here).
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
		// T-98f4 sticky placement: this connection is the PROOF that the session
		// actually came up, and its token's machine claim names where. Record it
		// so the next rebirth stays put instead of re-deriving placement from a
		// 手冊 that may have been edited since the worker was born.
		s.stampLandedMachine(memberID, machineID)
		// Cross-machine single-session enforcement (T-bb29 §1): if this is the
		// 正身 confirmed on its desired machine (claim == desired_machine), reap
		// any residual same-id session on OTHER machines. Fires only after the
		// new session is live here → never a zero-live-session window.
		s.identitySweepOnConnect(memberID, machineID)
	}
	detachReason := sseDetachReasonPeerClosed
	setDetachReason := func(reason string) {
		// The first concrete cause wins. In particular, a write failure that
		// happens while the station is closing is still useful socket evidence,
		// not a retroactive peer/context guess.
		if detachReason == sseDetachReasonPeerClosed {
			detachReason = reason
		}
	}
	defer func() {
		// last gates the §5.2 edge hooks: a kicked listener's Disconnect
		// reports false (the takeover already removed it; the new listener
		// keeps the member online), so the hooks fire only on the REAL
		// online→offline edge — never mid-takeover.
		last := s.hub.Disconnect(listener)
		if memberID != "" {
			fmt.Fprintf(os.Stderr, "[sse] detach member=%s gen=%d last=%t reason=%s\n",
				memberID, listener.Gen, last, detachReason)
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
	// T-5b83: hand the client the build it is attaching to, so ocagent's
	// connection line can name it. A version change restarts the station and
	// therefore drops every stream — the connection line already marks every
	// changeover, it just never said which commit. Stamping it here rather
	// than answering a separate probe is deliberate: a changeover reconnects
	// the whole fleet within seconds, and that is the worst possible moment to
	// take N extra requests. This is the same value /api/version reports as
	// git_sha (both read s.processSHA), so the two can be reconciled.
	w.Header().Set(sseStationSHAHeader, s.processSHA)
	w.WriteHeader(http.StatusOK)
	armWriteDeadline()
	if _, err := w.Write([]byte(": connected\n\n")); err != nil {
		// Keep the pre-existing greeting behaviour (the handler continues into
		// its normal loop) while retaining the concrete socket evidence.
		setDetachReason(sseDetachReasonWriteFailed)
	}
	flusher.Flush()

	// This connection's runtime, resolved ONCE (the notice rule differs per
	// runtime — see decideHandoverNotice). Members and outsource workers live in
	// different tables and both connect here, so both are tried; "" falls
	// through to the claude rule, which is the fail-safe direction (a percentage
	// notice on an unknown runtime is a wasted line, a missing one is a lost
	// close-out).
	connRuntime := ""
	if memberID != "" {
		if m, err := s.dal.GetMember(memberID); err == nil && m != nil {
			connRuntime = m.Runtime
		} else if w, err := s.dal.GetOutsourceWorker(memberID); err == nil && w != nil {
			connRuntime = w.Runtime
		}
	}
	// Per-connection token-expiry band state (spec §6.1): the last time the
	// still-unacknowledged warning was sent. A restart replaces this connection
	// and its JWT, which naturally clears the reminder state.
	lastTokenExpiryReminder := int64(0)
	nextTokenExpiryCheck := int64(0)

	write := func(frame []byte) bool {
		armWriteDeadline()
		if _, err := w.Write(frame); err != nil {
			setDetachReason(sseDetachReasonWriteFailed)
			return false
		}
		flusher.Flush()
		return true
	}

	ctx := r.Context()
	lastBeat := time.Now()
	for {
		if s.stationShuttingDown.Load() {
			setDetachReason(sseDetachReasonStationShutdown)
			return
		}
		select {
		case <-listener.kicked:
			if s.stationShuttingDown.Load() {
				setDetachReason(sseDetachReasonStationShutdown)
			} else {
				setDetachReason(sseDetachReasonTakeover)
			}
			return
		default:
		}
		if ctx.Err() != nil {
			setDetachReason(s.sseContextDetachReason())
			return
		}
		select {
		case <-listener.kicked:
			// Taken over (spec/sse.md §5.1): a newer connection for this member
			// holds the slot — return NOW (≤ssePoll after the kick) and let the
			// defer clean up (Disconnect is a map no-op; last=false keeps the
			// §5.2 edge hooks off while the member stays online).
			if s.stationShuttingDown.Load() {
				setDetachReason(sseDetachReasonStationShutdown)
			} else {
				setDetachReason(sseDetachReasonTakeover)
			}
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
		// Quiet tick: the ONE advance handover notice (any agent connection — the
		// agent cannot read its own context %, so the server pushes it).
		if memberID != "" {
			record := s.gauge.Get(memberID)
			signal := decideHandoverNotice(
				memberID, connRuntime, record,
				s.ctxHighConfig(), s.codexNoticeRoundSetting(), s.codexCompactionThreshold,
				s.offboardText)
			// ONCE PER SESSION, not once per connection (T-c382). The dedup key is
			// the gauge's boot_ts — the SESSION anchor, restored from the durable
			// member row on reconnect — so an SSE flap mid-session cannot re-fire
			// the notice. Per-connection state (what this used to hold) would:
			// every reconnect would nudge again, which is the bombardment the
			// owner asked to be rid of, wearing a different hat.
			if signal != nil {
				// Build the frame BEFORE claiming: claiming first would burn the
				// one-and-only notice on a marshal failure and go silent forever,
				// and "sent it" vs "silently dropped it" would look identical.
				if frame, err := directedFrameText(contextHighTopic, signal); err == nil &&
					s.claimHandoverNotice(memberID, record) {
					if !write(frame) {
						return
					}
					continue
				}
			}
		}
		// Token-expiry band (spec §6.1): the server alone can read the verified
		// JWT expiry, so it repeatedly directs a still-valid agent to checkpoint
		// and restart before auth fails. While the token is still far away, its
		// exp schedules the NEXT check at the 30-minute boundary; once pending,
		// a member read occurs only at the 30-second reminder cadence. This keeps
		// the hot SSE poll free of DB reads without delaying the first warning.
		if memberID != "" {
			now := time.Now().Unix()
			if now >= nextTokenExpiryCheck {
				claims := claimsFromContext(r.Context())
				remaining, validExpiry := tokenExpiryRemaining(claims, now)
				nextTokenExpiryCheck = tokenExpiryNextCheck(claims, now)
				switch {
				case !validExpiry:
					// Fail-safe on an absent/malformed/expired claim. The schedule above
					// avoids re-checking every 250ms while preserving stream health.
				case remaining > tokenExpiryWarningWindow:
					// The schedule above is the exact 30-minute warning boundary.
				default:
					member, err := s.dal.GetMember(memberID)
					if err == nil {
						signal, last := decideTokenExpirySignal(
							memberID, claims, member, s.gauge.Get(memberID),
							now, lastTokenExpiryReminder)
						lastTokenExpiryReminder = last
						if signal != nil {
							if frame, err := directedFrameText(tokenExpiryTopic, signal); err == nil {
								if !write(frame) {
									return
								}
								continue
							}
						}
					}
				}
			}
		}
		// Warden-command band (warden connections only): drain the pending
		// FIFO onto THIS connection — never the owner fan-out (the riding
		// member_token is a secret).
		if wardenID != "" {
			if pending := s.hub.DrainWardenCommands(wardenID); len(pending) > 0 {
				for i, cmd := range pending {
					if !write(cmd.Frame) {
						// T-66a2 (supersedes T-e0e3 O1's blunt requeue): the drain
						// already emptied the FIFO, so THIS frame and every frame
						// behind it are in nobody's hands but ours. Returning here
						// used to discard them with no log, no receipt and no field
						// — a lost order looked exactly like an order never sent.
						// Hand them back so the hub can requeue what has no
						// re-decision path (update) and NAME what it drops; a blind
						// requeue-everything would put a stale START back in the
						// queue that reconcile has already re-decided.
						s.hub.ReturnUndeliveredCommands(wardenID, pending[i:])
						return
					}
					// T-66a2 L3: the write succeeded, so this frame no longer
					// needs restart insurance. "Written" is NOT "delivered" —
					// this band has no ack — but it is the strongest event the
					// server can observe, so it is the clearing event.
					s.hub.MarkWardenCommandWritten(wardenID, cmd.Frame)
				}
				continue
			}
		}
		select {
		case <-ctx.Done():
			setDetachReason(s.sseContextDetachReason())
			return
		case <-listener.kicked:
			if s.stationShuttingDown.Load() {
				setDetachReason(sseDetachReasonStationShutdown)
			} else {
				setDetachReason(sseDetachReasonTakeover)
			}
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
		// 🔴 …unless the session is still WORKING its offboard sequence
		// (T-a9d6). 下線 no longer collects on a clock — the agent is shown the
		// sequence and asked to close out and report stopped itself — so a
		// session legitimately sits in exactly this state for as long as the
		// close-out takes. Refusing its reconnect there does not stop anything:
		// the agent's own listener treats a run of authoritative refusals as
		// "I have been retired" and kills its tmux session (listen_run.go), so
		// a network blip or a station upgrade mid-hand-off would take the
		// session down with the hand-off unwritten. That is the exact harm this
		// ticket exists to remove, arriving through a different door.
		//
		// The gate still closes the moment the close-out is DONE: stopped_since
		// is what the agent stamps when it has finished, and from then on a
		// reconnect is a stopped member re-projecting online, which is what the
		// refusal was written for.
		//
		// What separates the two is what the member itself has done. A session
		// that has reported stopped is finished; a member the owner FORCE-
		// stopped was cut off deliberately and must not come back on its own.
		// Anything else with a stop anchor is a close-out in flight.
		if m.StoppedSince <= 0.0 && !forcedEpochLive(*m) {
			return ""
		}
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
//
// 🔴 T-4235: "stamped IFF absent" is now decided against the DURABLE anchor
// (member.session_boot_ts), not against the gauge — see anchorSessionBoot. The
// gauge is emptied by contract on a station re-exec while the AGENTS survive it,
// so asking the gauge "is this session already anchored?" answered "no" for
// every live session the instant the station upgraded, and the reconnect minted
// a fresh anchor. The whole fleet then read as seconds old for ten minutes.
func (s *apiServer) onFirstConnect(memberID string) {
	// Worker presence is projected from this connection edge. The owner's live
	// worker list subscribes to outsource_worker, so fan its canonical delta
	// even when no durable member field changed (the common case).
	s.publishOutsourcePresenceEdge(memberID)
	if m, err := s.dal.GetMember(memberID); err == nil && m != nil && m.WakingSince > 0 {
		m.WakingSince = 0.0
		if err := s.putMember(*m, memberID); err != nil {
			fmt.Fprintf(os.Stderr, "[sse] first-connect waking clear failed for %q: %v\n", memberID, err)
		}
	}
	s.anchorSessionBoot(memberID)
}

// anchorSessionBoot is the T-4235 session-anchor resolution, run on the SSE
// first-connect edge. It keeps the gauge's boot_ts — which every consumer still
// reads — in agreement with the durable member.session_boot_ts, and it is the
// ONLY place that decides whether this connect begins a new session:
//
//	durable > 0   this session is ALREADY anchored. Two shapes reach here and
//	              both must leave the anchor where it is: a mid-session SSE flap
//	              (the gauge still holds the same value → nothing to do), and a
//	              server re-exec (the gauge is EMPTY → RESTORE it from the
//	              durable value, never mint a new "now"). The restore is the
//	              whole fix: it is what makes the min-liveness floor, the
//	              context-high auto-recycle suppressor, and the worker
//	              auto-handover loop-break all see the real session age again,
//	              immediately, for sessions that were already running when the
//	              station upgraded.
//	durable == 0  no session is anchored — the last one ended at a real
//	              spawn/stop boundary (clearSessionBootTS zeroes BOTH stores) or
//	              this entity has never connected. THIS is a session birth, so
//	              stamp a fresh anchor in both stores. The respawn-storm guard is
//	              therefore not weakened: a genuinely new session still reads
//	              seconds old and restart_self still answers 429.
//
// A pre-existing gauge boot_ts with no durable twin (a session that was already
// anchored when this column shipped, or a durable write that failed) is ADOPTED
// rather than overwritten: the anchor may only ever move backwards in time on
// this edge, never forwards, because forwards is exactly the defect.
//
// Best-effort on the durable half — a storage fault must not kill the stream
// that just opened; the gauge half still carries the session within this
// process, which is the pre-T-4235 behaviour.
func (s *apiServer) anchorSessionBoot(memberID string) {
	entry := s.gauge.Get(memberID)
	if entry == nil {
		entry = map[string]any{}
	}
	gaugeTS, gaugeHas := gaugeBootTS(entry)

	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil {
		// No durable row to anchor against (an id the roster does not know).
		// Degrade to the gauge-only rule rather than refusing to anchor at all.
		if gaugeHas {
			return
		}
		entry["boot_ts"] = nowSecs()
		s.gauge.Set(memberID, entry)
		return
	}

	if m.SessionBootTS > 0 {
		if !gaugeHas || gaugeTS != m.SessionBootTS {
			entry["boot_ts"] = m.SessionBootTS
			s.gauge.Set(memberID, entry)
		}
		return
	}

	ts := nowSecs()
	if gaugeHas {
		ts = gaugeTS
	}
	entry["boot_ts"] = ts
	s.gauge.Set(memberID, entry)
	if err := s.dal.SetMemberSessionBootTS(memberID, ts); err != nil {
		fmt.Fprintf(os.Stderr, "[sse] session-boot anchor persist failed for %q: %v\n", memberID, err)
	}
}

// stampLandedMachine records the machine a session actually connected from
// (T-98f4) — the durable anchor rule 3 of the outsource placement decision
// reads (「沒被搬過 + 不是第一次 → 留在上一輪實際跑的那台」), and, for every
// kind, the last-observed machine the cockpit compares the owner's pin against.
//
// WHY THE CONNECT EDGE and not the dispatch: a dispatch is an intent that may
// never boot (the whole X-46 family of stalls), and sticking to a machine the
// worker never ran on would make a failed boot permanent. The SSE machine claim
// comes off the worker's own minted token (notifyWorkerSpawn passes the resolved
// warden into mintAgentToken), so it names the host the session is genuinely on
// and it survives a server re-exec — unlike workerSpawnTarget, which is
// in-memory by contract, and unlike hub.MachineOf, which only exists while the
// session is live.
//
// NO LONGER SCOPED TO kind == outsource (T-7f28). It was, on the reasoning that
// a staff member's desired_machine_id already pins it — but a pin is the
// INTENT, and the moment the owner re-pins a member the intent stops describing
// where it is. Without a durable observation an offline member has nothing to
// compare the new pin against, so a move that has not happened yet cannot be
// told from one that has. The anchor stays a PLACEMENT input for outsource only
// (notifyWorkerSpawn is the sole reader); for staff it is purely observational.
// A blank claim writes nothing (an owner dashboard connection, or an agent token
// minted before machine claims existed) — "" means "unknown", never "nowhere",
// and erasing a known landing on an unknowable connect is how a worker would
// silently fall back to the 手冊 again.
//
// 🔴 GATED ON THE 正身 CHECK, not on the mere fact of a connection
// (connectionIsTheGenuineArticle — the SAME predicate identitySweepOnConnect
// runs on the next line, and for the same reason: a wanderer's claim carries no
// authority). "連上了" is not the criterion; "連上了 而且 確實是派到這裡的" is.
// Without the gate a residual ocagent left over on an old host — the exact
// doppelganger the sweep exists to reap — would DURABLY overwrite last_machine_id
// on connect, and the next rebirth would follow the ghost. Sticky workers
// commonly carry DesiredMachineID == "", and after a server re-exec
// workerSpawnTarget is empty too, so such a connection is not even swept: the
// stamp would be the ghost's only lasting effect on the fleet. An unverifiable
// connection therefore leaves the known landing alone (fail-safe, the same
// direction as the blank-claim rule below). A LEGITIMATE first landing still
// stamps: the pin may be blank, but the dispatch the server just made names the
// machine, and that is what the token's claim echoes back.
//
// Best-effort, and deliberately WRITE-ONLY-ON-CHANGE: a reconnect on the same
// machine must not cost a row write plus an SSE delta.
func (s *apiServer) stampLandedMachine(memberID, machineID string) {
	if machineID == "" {
		return
	}
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil || m.LastMachineID == machineID {
		return
	}
	if !s.connectionIsTheGenuineArticle(*m, machineID) {
		return // a wanderer's claim never rewrites where this worker lives
	}
	m.LastMachineID = machineID
	if err := s.putMember(*m, memberID); err != nil {
		fmt.Fprintf(os.Stderr, "[sse] landed-machine stamp failed for %q: %v\n", memberID, err)
	}
}

// clearSessionBootTS drops session-scoped gauge state from a member's / worker's
// gauge entry at a real session BOUNDARY — a START dispatch that begins a new
// session, or a STOP/kill that ends one. onFirstConnect stamps boot_ts only when
// absent, so clearing here is what makes the next connect re-stamp a fresh
// anchor: "reconnect keeps boot_ts, respawn resets it" (T-8fb2). Best-effort; a
// missing entry or missing key is a clean no-op.
//
// 🔴 T-4235: it clears BOTH stores, and the durable half is NOT guarded by the
// gauge half. Zeroing member.session_boot_ts here is the ONLY thing that makes
// the next connect stamp a fresh anchor, so the moment these two stores can
// disagree about "is a session anchored?" the respawn-storm guard is weakened in
// the dangerous direction — a genuinely new session would inherit its
// predecessor's hours-old anchor and be waved through. Keeping the write and the
// clear inside this one pair of functions is what makes drift impossible; do not
// add a third writer.
func (s *apiServer) clearSessionBootTS(id string) {
	if entry := s.gauge.Get(id); entry != nil {
		delete(entry, "boot_ts")
		// Codex compaction count belongs to the old App Server thread. Carrying
		// it over a refocus would immediately recycle the fresh replacement
		// session.
		delete(entry, "compaction_count")
		s.gauge.Set(id, entry)
	}
	// The advance-notice claim (T-c382) is keyed on the anchor being dropped
	// here, so it is session-scoped state too — drop it on the same boundary
	// rather than leaving one record per agent id alive for the process's
	// lifetime.
	s.handoverNoticedMu.Lock()
	delete(s.handoverNoticed, id)
	s.handoverNoticedMu.Unlock()
	// Write-on-change: the clear runs on every session boundary, and an
	// unconditional UPDATE would cost a row write per boundary for nothing.
	m, err := s.dal.GetMember(id)
	if err != nil || m == nil {
		return
	}
	if m.SessionBootTS != 0 {
		if err := s.dal.SetMemberSessionBootTS(id, 0); err != nil {
			fmt.Fprintf(os.Stderr, "[sse] session-boot anchor clear failed for %q: %v\n", id, err)
		}
	}
	// 🔴 The durable half of the notice claim (T-6ebc) is tested SEPARATELY, not
	// under the anchor's condition. The two columns describe the same session but
	// they are not written in one transaction, so a boundary that finds the
	// anchor already at 0 can still find a claim standing — and returning early
	// on the anchor alone would leave it there for the NEXT session to inherit,
	// which silences the one notice that session is entitled to. Silence is the
	// failure mode no one reports.
	if m.HandoverNoticedTS != 0 {
		if err := s.dal.SetMemberHandoverNoticedTS(id, 0); err != nil {
			fmt.Fprintf(os.Stderr, "[sse] handover-notice claim clear failed for %q: %v\n", id, err)
		}
	}
}

// onLastDisconnect handles the SSE last-disconnect edge for an agent
// connection: fold the live telemetry cost into the actor's durable
// banked_cost, then POP the live field (exactly-once-per-edge banking).
func (s *apiServer) onLastDisconnect(memberID string) {
	s.bankLiveCost(memberID)
	s.publishOutsourcePresenceEdge(memberID)
}

// publishOutsourcePresenceEdge makes the worker-list projection converge after
// a real SSE online edge. Presence lives only in Hub, so no durable write is
// guaranteed to accompany a clean connect/disconnect; the existing
// outsource_worker delta is the owner cockpit's canonical invalidation signal.
func (s *apiServer) publishOutsourcePresenceEdge(memberID string) {
	worker, err := s.dal.GetOutsourceWorker(memberID)
	if err != nil || worker == nil || worker.Status == WorkerStatusReleased {
		return
	}
	s.publishOutsourceWorker(*worker, triggerServer)
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
// cannot drift silently. EMBED-ONLY — the bindist copy is the sole source and
// disk is never consulted (assets.go readMCPCatalogFrom). This sentence used to
// say "disk-first with the embed as fallback"; it was wrong, and a reviewer
// reading it "corrected" a correct implementation on its authority.
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
