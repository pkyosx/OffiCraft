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
	// sseDetachReasonUnset is the PRE-DECISION state, and it must never equal
	// any reason we can conclude. setDetachReason's "first concrete cause wins"
	// rule is a comparison against this value: while the initial value doubled
	// as a conclusion (peer-closed did, until T-3b4e review), a later call
	// silently overwrote a real cause and nothing went red. The printed
	// default lives in detachReasonForLog, so the operator-facing vocabulary
	// is unchanged.
	sseDetachReasonUnset           = ""
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

// detachReasonForLog keeps the operator vocabulary exactly as it was: an exit
// that concluded nothing is still reported as peer-closed, which is what a
// return with no recorded cause means. The sentinel never reaches the log.
func detachReasonForLog(reason string) string {
	if reason == sseDetachReasonUnset {
		return sseDetachReasonPeerClosed
	}
	return reason
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
	detachReason := sseDetachReasonUnset
	setDetachReason := func(reason string) {
		// The first concrete cause wins. In particular, a write failure that
		// happens while the station is closing is still useful socket evidence,
		// not a retroactive peer/context guess.
		if detachReason == sseDetachReasonUnset {
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
				memberID, listener.Gen, last, detachReasonForLog(detachReason))
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

	// The notice source, bound once per connection rather than once per tick.
	// Binding it is free; RUNNING it is not — it is a fold over a durable
	// document — which is why handoverNoticeTick decides whether this tick can
	// emit BEFORE it calls it. SOFT, always: the first context threshold is an
	// advance warning and nothing collects it at a named instant, so it reads
	// 〈停止〉 and quotes no deadline (see decideHandoverNotice).
	noticeText := func() string {
		return s.winddownNoticeText(offboardKindSoft, 0)
	}

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
		// Leaving here DISCONNECTS a live stream early — by up to
		// upgradeRestartDelay (upgrade.go), the gap between the mark and the
		// re-exec. Two things about that, and the second is not ours to keep
		// true (T-3b4e review):
		//   · No client reconnects inside that window today, so the early exit
		//     causes no attach/detach churn: ocagent sleeps its backoff before
		//     re-dialing and that backoff RESETS to listenBackoffStart (1s,
		//     cli/ocagent/listen.go) on a healthy stream, and the cockpit uses
		//     a bare EventSource with no `retry:` from us, so it takes the
		//     browser default (seconds). BOTH FIGURES ARE READ FROM THE CODE,
		//     NOT MEASURED against a real upgrade.
		//   · That quiet therefore RESTS ON CLIENT BACKOFF, not on anything
		//     this file guarantees. Drop a client's backoff to zero, or attach
		//     one with none, and the churn appears — and NOTHING here goes red.
		// Only the UPGRADE path reaches this shape at all: a signal shutdown
		// runs httpServer.Shutdown (server.go), which stops accepting, so a new
		// connection is refused at accept rather than admitted and bounced.
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
			if frame, ok := s.handoverNoticeTick(
				memberID, connRuntime, noticeText); ok {
				if !write(frame) {
					return
				}
				continue
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
// its stream (the wind-down nudge rides it); stop→start clears the anchors and
// flips desired online, and since T-55 that is TWO writes, not one — the anchors
// through their sole writer first, desired_state with the row write after. The
// gate keys on desired_state, so it lifts on the SECOND of the two and a failure
// between them leaves the gate standing (refusing) rather than half-lifted, which
// is the safe side; recycle/handover keeps desired online throughout. An unknown
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
		//
		// The second half of that is gracefulStopEpochOpen (api_members.go), and
		// this site used to write its two terms out by hand. Under
		// stopped_since <= 0 the enclosing guard has ALREADY established
		// stopping_since > 0 (its disjunction has no other arm left), so the
		// hand-written forced term and the call below decide exactly the same
		// rows — the call merely re-asks a term that is already true.
		// This is the ENTITLEMENT of a graceful stop epoch, the same compound
		// autoHandoverWorker's stop arm asks; it is staff-only because the
		// outsource kind returns above.
		if m.StoppedSince <= 0.0 && gracefulStopEpochOpen(*m) {
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
		// …and the session's CONTEXT REPORT, both halves (T-72dd).
		//
		// 🔴 WHY, AND ONLY WHY. This is NOT known to be the cause of the
		// silent no-op this ticket chased — that question is neither confirmed
		// nor excluded. The reason it changes is narrower and stands on its
		// own: TWO READERS OF THIS KEY DISAGREE. actionableContextPct (the
		// gate/threshold reader) refuses a pct whose context_pct_ts is not
		// strictly newer than boot_ts, and boot_ts is what the line above just
		// deleted; foldActorRuntime (the cockpit / get_monitoring reader, in
		// wire.go) takes context_pct RAW with no such test. Leaving the pair
		// standing across a boundary therefore leaves the panel showing a
		// number that no threshold in the server will ever act on — the
		// displayed percentage and the judged percentage are two different
		// numbers, which is wrong whatever else is or is not broken.
		//
		// Dropping BOTH halves is what makes them agree: the gate reader
		// already answers "no number", and now the cockpit's honest dash says
		// the same thing until the fresh session files its first report. It is
		// the same rule compaction_count is deleted under one line up — this is
		// the OLD session's reading, and it does not describe the new one.
		//
		// 🔴 AND THESE TWO DELETES ARE NOT "PURELY OBSERVATIONAL". That ⚠️
		// beside noteContextGateSkip describes the DIAGNOSTIC LINE and nothing
		// else; it does not cover this pair, and reading it as a claim about
		// the whole ticket is wrong. Whether these deletes move a threshold is
		// decided by the ctx stale-guard setting, which actionableContextPct
		// takes as a parameter:
		//
		//   - guard ON (the code default): boot_ts was dropped one line up, so
		//     the guard already refuses the pct for want of an anchor. Removing
		//     the pair changes nothing any threshold sees. Observational.
		//   - guard OFF: that function returns the raw pct WITHOUT ever reading
		//     boot_ts. So before this change a dead session's leftover pct
		//     stayed actionable across the boundary and could still drive the
		//     auto-refocus and advance-notice predicates in reconcile.go and
		//     the SSE context band. After it, that reading is simply absent.
		//     That is a BEHAVIOUR CHANGE — it suppresses auto-refocus fired on
		//     a dead session's residue — and a deliberate one: a fresh session's
		//     first window must not be judged on its predecessor's number, and
		//     the new session restores a real one the moment it reports.
		//
		// The guard-OFF branch is REACHABLE, not theoretical: the value is
		// settings-driven and read from the DB at startup, so the default is a
		// default and not a guarantee. (A developer spot-check of one live
		// deployment's settings at the time of writing found no row for the
		// key, i.e. that site was running the default — one site at one moment,
		// which is not evidence that nobody ever turns it off, and says nothing
		// about any other deployment.)
		delete(entry, "context_pct")
		delete(entry, "context_pct_ts")
		s.gauge.Set(id, entry)
	}
	// The advance-notice claim (T-c382) is keyed on the anchor being dropped
	// here, so it is session-scoped state too — drop it on the same boundary
	// rather than leaving one record per agent id alive for the process's
	// lifetime.
	s.handoverNoticedMu.Lock()
	delete(s.handoverNoticed, id)
	s.handoverNoticedMu.Unlock()
	// The context-gate diagnostic's throttle window (T-72dd) is session-scoped
	// for BOTH of the reasons the claim above is, and it is dropped here rather
	// than left to accumulate for exactly the reason written one comment up:
	// "rather than leaving one record per agent id alive for the process's
	// lifetime". A worker id is minted per task, so an un-pruned cell per actor
	// is a slow leak with no upper bound but the process.
	//
	// It is also the behaviour we want. The window exists to stop one actor
	// repeating itself WITHIN a session; a NEW session is a new set of numbers,
	// and making it serve out its predecessor's window would suppress the first
	// — most interesting — description of it.
	s.ctxGateDiagMu.Lock()
	delete(s.ctxGateDiagAt, id)
	s.ctxGateDiagMu.Unlock()
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
// and add it to the durable member.banked_cost of whichever kind the id
// resolves to (the outsource_worker table was folded into member in 00025, so
// both kinds are the same column and the same sole writer). Callers: the
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
		if err := s.dal.AddMemberBankedCost(actorID, cost); err != nil {
			fmt.Fprintf(os.Stderr, "[bank] cost bank failed for member %q: %v\n", actorID, err)
			return
		}
		// The member delta this fold used to get for free from putMember. It
		// is not decoration: the wind-down / recycle hooks key on a member
		// delta naming self, and this fold runs ON the last-disconnect edge.
		m.BankedCost += cost
		s.publishMemberPatch(*m, actorID)
		return
	}
	if w, err := s.dal.GetOutsourceWorker(actorID); err == nil && w != nil {
		pop()
		// No delta on purpose (pre-fold parity): a worker's changes ride the
		// outsource_worker projection, never a member patch naming an ow- id.
		if err := s.dal.AddMemberBankedCost(actorID, cost); err != nil {
			fmt.Fprintf(os.Stderr, "[bank] cost bank failed for worker %q: %v\n", actorID, err)
		}
	}
}

// dropLiveCost removes the live telemetry cost from an actor's entry and
// reports what it removed (nil when there was nothing there). It is the half of
// a cost reset that bankLiveCost's pop() is the half of a bank: same key, same
// read-modify-Set shape, so the two operations cannot drift apart on where the
// live figure lives.
//
// 🔴 CALL IT AFTER THE DURABLE WRITE HAS SUCCEEDED, never before. It is not
// undoable and its subject exists nowhere else, so calling it first turns any
// durable-write failure into unrecoverable data loss on a request that then
// answers 500. bankLiveCost pops BEFORE its write for the opposite reason
// (exactly-once banking on an edge that will not be retried) — do not copy that
// ordering here.
func (s *apiServer) dropLiveCost(actorID string) *float64 {
	entry := s.telemetry.Get(actorID)
	if entry == nil {
		return nil
	}
	cost, ok := entry["cost"].(float64)
	if !ok {
		return nil
	}
	delete(entry, "cost")
	s.telemetry.Set(actorID, entry)
	return &cost
}

// HandleResetCostApiMembersMemberIdCostResetPost — POST
// /api/members/{member_id}/cost/reset, the cockpit's 成本歸零 button (owner
// ruling rc-7dea0deefa63, option 0「最小、不可逆」).
//
// 🔴 BOTH HALVES OR NEITHER. The owner-visible 估計$ is two numbers added on the
// client: the durable banked_cost column and the live in-memory telemetry
// figure. Clearing only the durable half is not a smaller version of this
// button — the live figure reappears on the very next cockpit read, which the
// owner cannot tell apart from the button doing nothing at all. That is why the
// live drop is not an optimisation here and why a test pins it.
//
// 🔴 IRREVERSIBLE, deliberately. No snapshot is kept and there is no undo route:
// spend is stored as two accumulators with no per-charge ledger behind them, so
// nothing else in this system holds the discarded figure. The response is
// therefore a RECEIPT of what was destroyed — the two values as they stood
// immediately before the write, which is the last moment they exist anywhere.
// It is not an undo and must never grow into one without a fresh owner ruling.
//
// The actor is resolved the way bankLiveCost resolves it, so ONE route serves
// both kinds: a staff member, or an outsource worker.
//
// 🔴 A RELEASED WORKER IS ACCEPTED, and it is the one outsource write door that
// takes a removed roster row (owner ruling rc-1344cc76a24a, 2026-09-02:「連已經
// 退場的也要能清（帳號卡才會真的歸零）」, overriding this route's earlier 404).
// The reason it must differ from its neighbours: released is the STEADY STATE
// for a worker — ReleaseWorkersForTask fires on every task close — and a
// released worker's own 估計$ is still rendered, so refusing it here would
// leave a figure on screen that the button next to it cannot clear. The other
// outsource doors refuse released rows because they drive a LIVE session; this
// one only edits a number that is still being displayed.
//
// ⚠️ THE REASON THE OWNER GAVE FOR THAT RULING NO LONGER FOLLOWS, while the
// ruling itself stands. He asked for it so that「帳號卡才會真的歸零」 — true
// under the model of that day, where the account card was a fold over its
// actors. A day later rc-5c5d7c7c6dcd made the card an accumulator of its own,
// so clearing actors (released or not) no longer moves it at all; the account
// has its own button now. Kept as written rather than quietly re-motivated:
// a stale rationale attached to a live ruling is how the next reader concludes
// the ruling itself is stale.
//
// Staff are different and stay filtered: removing a member HARD-DELETES the row
// AND its telemetry entry (api_roles.go, the repo's only telemetry.Delete), so
// a removed member has no figure anywhere and there is nothing here to clear.
func (s *apiServer) HandleResetCostApiMembersMemberIdCostResetPost(w http.ResponseWriter, r *http.Request, memberId string) {
	// Staff first, mirroring bankLiveCost: an outsource member banks (and so
	// resets) through the WORKER branch, never as a member patch.
	if m, err := s.dal.GetMember(memberId); err == nil && m != nil &&
		m.RosterStatus != RosterStatusRemoved && m.Kind != KindOutsource {
		// 🔴 DURABLE FIRST, LIVE SECOND, and the order is the whole safety
		// property (found by independent review, T-54). The live figure lives
		// only in memory and this call is its executioner: drop it before the
		// durable write and a failed write answers 500 having ALREADY destroyed
		// half the number, with the receipt — the one record of what was
		// destroyed — never reaching the caller. Nothing anywhere could
		// reconstruct it. This way round, a failed write leaves BOTH halves
		// exactly as they were, and the owner simply presses again.
		//
		// 🔴 AND IT IS A SINGLE-COLUMN WRITE, mirroring bankLiveCost: handing
		// the whole row to putMember would write NOTHING, because banked_cost is
		// deliberately insert-only — a whole-row write never lands it on an
		// existing row (T-14 項目 6).
		// The receipt comes back from the same transaction that destroys the
		// figure, so it names what was actually destroyed.
		clearedBankedFig, err := s.dal.ZeroMemberBankedCost(memberId)
		if err != nil {
			internalError(w, err)
			return
		}
		clearedBanked := nonZeroCost(clearedBankedFig)
		// The member delta putMember used to fan for free — the same half
		// bankLiveCost publishes by hand for the same reason.
		m.BankedCost = 0
		s.publishMemberPatch(*m, requestTrigger(r))
		cleared := s.dropLiveCost(memberId)
		s.publishMonitoringSignal(memberId, requestTrigger(r))
		writeJSON(w, http.StatusOK, costResetDTO{
			MemberID:          memberId,
			ClearedCost:       cleared,
			ClearedBankedCost: clearedBanked,
		})
		return
	}
	wk, err := s.dal.GetOutsourceWorker(memberId)
	if err != nil {
		internalError(w, err)
		return
	}
	// NO status filter: a released worker is reset like any other (see the
	// ruling in this handler's doc). Only a genuinely unknown id is a 404.
	if wk == nil {
		writeError(w, http.StatusNotFound, "member '"+memberId+"' not found")
		return
	}
	// Durable first, live second — the same ordering the member arm above
	// explains, for the same reason. Both arms must fail the same way. And the
	// same single-column seam: a worker's banked_cost IS member.banked_cost
	// (P7d), so the whole-row writers cannot move it either.
	clearedBankedFig, err := s.dal.ZeroMemberBankedCost(memberId)
	if err != nil {
		internalError(w, err)
		return
	}
	clearedBanked := nonZeroCost(clearedBankedFig)
	wk.BankedCost = 0
	cleared := s.dropLiveCost(memberId)
	s.publishOutsourceWorker(*wk, requestTrigger(r))
	s.publishMonitoringSignal(memberId, requestTrigger(r))
	writeJSON(w, http.StatusOK, costResetDTO{
		MemberID:          memberId,
		ClearedCost:       cleared,
		ClearedBankedCost: clearedBanked,
	})
}

// accountSpendAccountedKey is the accumulator's own high-water mark on the
// telemetry entry: the reported cost figure that has ALREADY been credited to
// the account.
//
// 🔴 IT IS A SEPARATE KEY FROM "cost" ON PURPOSE, for two reasons and neither
// is tidiness. First, "cost" is overwritten IN PLACE by the ingest before the
// accrual runs, so by then the previous figure is simply gone — the baseline has
// to be recorded somewhere of its own or there is no baseline at all. Second,
// bankLiveCost DELETES "cost" at the end of a session (it moves the figure into
// the actor's durable column); a baseline living there would vanish with it, and
// the first report after a reconnect would read as a brand-new session and
// credit its whole cumulative figure a SECOND time — a double-count this code
// would have MANUFACTURED, on top of the reconnect bias the ticket already
// documents and leaves alone. So banking must NOT clear this key: doing so turns
// TestAccountSpend_BankingASessionDoesNotMakeTheNextReportCountTwice red (6
// becomes 12), which is exactly what it is there for.
const accountSpendAccountedKey = "cost_accounted"

// accrueAccountSpend credits the NEW spend in one telemetry report to the
// account it was reported under (T-53, owner ruling rc-5c5d7c7c6dcd
// 「分開：帳號卡自己一份數字，清它不動成員」).
//
// It is called from the telemetry ingest and from nowhere else, because a
// report arriving is the only moment new spend becomes visible. It never reads
// or writes any ACTOR figure: that separation is the ruling.
//
// 🔴 HOW "THE NEW PART" IS COMPUTED, which is the whole correctness of this
// function. An agent reports its session's CUMULATIVE cost, so the increase is
// this report minus the last one credited. A report LOWER than the last is not
// a refund and not a mistake — it is a NEW SESSION counting from zero — so its
// whole value is new spend, and the baseline restarts there. The three
// plausible-looking alternatives are all wrong in ways nothing would flag:
// skipping a decrease loses everything the new session spends until it passes
// the old figure; adding the difference makes the account figure go DOWN, which
// is the silent-lie shape this design exists to avoid; and treating the report
// as an absolute would erase the earlier sessions' spend. Pinned end-to-end by
// TestAccountSpend_ASessionRestartCountsFromZeroRatherThanGoingBackwards.
//
// 🔴 THE BASELINE ADVANCES ONLY AFTER THE WRITE SUCCEEDS, and that ordering is
// the difference between "one report was lost" and "that money is gone for
// good" (found by independent review, T-56). A failed write is best-effort by
// design — failing the ingest would turn a bookkeeping problem into a monitoring
// outage — but best-effort only holds if the NEXT report can still see the
// delta. Advance the baseline first and the failed increment is subtracted from
// a report that was never credited: permanently missing, with a 200 on the way
// out and nothing but a stderr line to say so.
//
// A NEW SESSION also resets the baseline explicitly, from the waking report —
// see startAccountSpendSession. The decrease rule below is the fallback for a
// session that never announced itself, and it is a KNOWN, ACCEPTED under-count,
// not a complete substitute: see the boundary note on startAccountSpendSession.
func (s *apiServer) accrueAccountSpend(entry map[string]any) {
	account, _ := entry["account"].(string)
	if account == "" {
		return
	}
	cost, ok := entry["cost"].(float64)
	if !ok {
		return
	}
	accounted, seen := entry[accountSpendAccountedKey].(float64)
	delta := cost
	if seen && cost >= accounted {
		delta = cost - accounted
	}
	if delta <= 0 {
		// Nothing to credit, so nothing can be lost by moving the mark.
		entry[accountSpendAccountedKey] = cost
		return
	}
	if err := s.dal.AddAccountSpend(account, delta); err != nil {
		// Leave the baseline where it was: the next report will carry this
		// delta again, because its own increase is measured from the last
		// figure that was actually banked.
		fmt.Fprintf(os.Stderr, "[account] spend accrual failed for %q: %v\n", account, err)
		return
	}
	entry[accountSpendAccountedKey] = cost
}

// startAccountSpendSession forgets the accrual baseline because a NEW SESSION is
// starting: the next cost this actor reports is counted from zero, so its whole
// figure is new spend rather than an increase over the previous session's.
//
// 🔴 WHY AN EXPLICIT BOUNDARY, when accrueAccountSpend already treats a DECREASE
// as a restart (T-56): that fallback cannot see a restart whose first report
// happens to land at or above the old figure — a short session followed by a
// busier one — and it therefore under-credits the difference, silently. It also
// cannot tell a session that CHANGED ACCOUNT apart from one that carried on: on
// the wire those two look identical, and crediting the whole figure to the new
// account would invent money that was already banked against the old one. The
// waking report is the one place the server is TOLD a generation began, so it is
// where the question stops being a guess.
//
// 🔴 THE RESIDUAL BOUNDARY, ACCEPTED AND NOT CLOSED (named at the request of
// independent review, T-56): a reporter that never announces waking still has
// only the decrease fallback, so a generation of its whose first report lands AT
// OR ABOVE the previous one is credited the difference rather than its whole
// figure. The account card then reads LOW, permanently, and nothing flags it.
// This is accepted rather than fixed because the wire carries no other signal
// that a generation began; every OffiCraft member calls report_waking as step 1
// of its boot sequence, so the gap covers only a reporter outside that contract,
// and closing it would mean guessing from the numbers again. Pinned — the low
// number asserted deliberately — by
// TestAccountSpend_AReporterThatNeverWakesUnderCountsAndThatIsAccepted.
//
// Best-effort and silent when there is nothing to forget: an actor with no
// telemetry entry yet has no baseline to clear, which is the same state this
// produces.
func (s *apiServer) startAccountSpendSession(actorID string) {
	entry := s.telemetry.Get(actorID)
	if entry == nil {
		return
	}
	if _, present := entry[accountSpendAccountedKey]; !present {
		return
	}
	delete(entry, accountSpendAccountedKey)
	s.telemetry.Set(actorID, entry)
}

// HandleResetAccountCostApiAccountsCostResetPost — POST /api/accounts/cost/reset,
// the cockpit's 帳號歸零 button (owner ruling rc-5c5d7c7c6dcd, 2026-09-02).
//
// 🔴 IT TOUCHES NO ACTOR, and that is the entire point of the ruling: the owner
// asked for the account figure and the per-member figure to be clearable
// independently, because what he watches is spend per account. Pressing this
// leaves every member's and worker's 估計$ exactly as it was.
//
// IRREVERSIBLE: no snapshot, no undo route, and no per-charge ledger behind the
// accumulator, so the response is a receipt of the figure as it stood
// immediately before the write — the last moment it exists anywhere.
//
// An unknown account tag is NOT a 404. An account is a free telemetry string
// with no roster row, so 「沒有這個帳號」 and 「這個帳號沒東西可清」 are the same
// state: 200, cleared_cost null. That also makes the second press honest rather
// than an error, and the second press is the likely one.
func (s *apiServer) HandleResetAccountCostApiAccountsCostResetPost(w http.ResponseWriter, r *http.Request) {
	var body AccountCostResetRequestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	account := trimString(body.Account)
	if account == "" {
		writeError(w, http.StatusUnprocessableEntity, "account cannot be blank")
		return
	}
	had, err := s.dal.ZeroAccountSpend(account)
	if err != nil {
		internalError(w, err)
		return
	}
	// The cockpit's account card is folded from the monitoring read, so the
	// signal is what makes the zero appear without a manual refresh.
	s.publishMonitoringSignal(account, requestTrigger(r))
	writeJSON(w, http.StatusOK, accountCostResetDTO{
		Account: account,
		// nonZeroCost, so "there was nothing to clear" reads as absent rather
		// than as "zero was cleared" — the same null semantics as the per-actor
		// receipt and as the read side.
		ClearedCost: nonZeroCost(had),
	})
}

// nonZeroCost mirrors foldActorRuntime's rule for the banked figure: 0 is not
// put on the wire. On this receipt that reads as "there was nothing banked to
// clear" rather than "zero was cleared", and it keeps the reset's two fields
// field-for-field identical to the read side so a client reuses one summing
// rule instead of growing a second one.
func nonZeroCost(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

// publishMonitoringSignal fans the same owner-only cockpit invalidation the
// telemetry ingest fans, so a reset converges the 估計$ cell without waiting for
// the next sample. No agent consumes it.
func (s *apiServer) publishMonitoringSignal(actorID, trigger string) {
	s.hub.Publish("monitoring", "signal", "monitoring", actorID, nil, audienceOwnerOnly(), trigger)
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
		if retired := s.fillLessonsIdentityArgs(r, name, arguments); retired != nil {
			// A RETIRED ARGUMENT IS A TOOL-LEVEL REFUSAL, not a JSON-RPC
			// invalid-params error — same CallToolResult shape a REST 400
			// takes, so the caller reads the field name and the replacement
			// instead of a transport-level complaint it cannot act on.
			status := http.StatusBadRequest
			raw, marshalErr := json.Marshal(map[string]map[string]string{
				"error": {"code": errorCodeForStatus(status), "message": retired.Error()},
			})
			if marshalErr != nil {
				rpcError(w, id, rpcInternalError, "tool validation failed: "+marshalErr.Error())
				return
			}
			rpcResult(w, id, callToolResult(status, raw))
			return
		}
		reqPath, rawQuery, body, splitErr := splitToolArguments(spec, arguments)
		if splitErr != nil {
			// A known tool with a missing path argument is a tool-level input
			// validation refusal, not a JSON-RPC invalid-params error. Keep it in
			// the same CallToolResult shape as a REST 422 so callers receive the
			// missing field name instead of a route reached after path.Clean.
			status := http.StatusUnprocessableEntity
			raw, marshalErr := json.Marshal(map[string]map[string]string{
				"error": {"code": errorCodeForStatus(status), "message": splitErr.Error()},
			})
			if marshalErr != nil {
				rpcError(w, id, rpcInternalError, "tool validation failed: "+marshalErr.Error())
				return
			}
			rpcResult(w, id, callToolResult(status, raw))
			return
		}
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

// handoverNoticeTick is ONE quiet tick of the context-high band: it reports the
// frame to write, or ok=false to stay quiet. Split out of the SSE loop so the
// property below can be MEASURED by a test instead of asserted by a comment.
//
// 🔴 THE ORDER OF THE TWO STEPS IS THE POINT.
//
// The once-per-session fact is enforced by claimHandoverNotice, which runs
// AFTER decideHandoverNotice has composed the signal — and composing it runs
// `offboard`, a fold over a durable document. But decideHandoverNotice returns
// non-nil on EVERY tick once the agent is past its notice point, not just the
// first: the "fires once" gate is downstream of it. So for the whole remainder
// of a high-band session — a tick every ssePoll — this used to compose a frame
// that was then thrown away, at the full cost of the fold. Measured on an empty
// station, a SILENT tick costs 246ns with this guard and 374µs without it.
//
// handoverNoticeSettled is asked FIRST for that reason. It is read-only (gauge
// record + the process-local claim cache, no query), so it cannot change what
// is sent — only whether the work of composing an already-spent notice is done
// at all. TestHandoverNoticeTick_ClosureIsNotRunAfterTheClaim counts the
// closure calls and fails if this order is reversed.
func (s *apiServer) handoverNoticeTick(
	memberID, connRuntime string, notice func() string,
) ([]byte, bool) {
	record := s.gauge.Get(memberID)
	if s.handoverNoticeSettled(memberID, record) {
		return nil, false
	}
	// 🔴 ALREADY WINDING DOWN ⇒ SAY NOTHING (owner, 2026-08-24, verbatim:
	// 「下線 → 加速 → 強制。後者一旦發出我們就不該發出前者」).
	//
	// This band is the ONE wind-down path that never read the member row at
	// all: it decided purely from the gauge (how full the context is) and its
	// own once-per-session claim. So an agent the owner had ALREADY put into
	// 加速停止 — counting down to a deadline — would, the moment its usage
	// crossed the FIRST threshold, be handed a 停止 notice telling it there is
	// no hurry. Measured, not reasoned: with the member parked in
	// accelerated_stop and the gauge over the notice point, this tick emitted
	// the frame.
	//
	// 🔴 THE SISTER GUARD IS NOT THIS ONE. armRefocusEpoch's ladder governs who
	// may overwrite refocus_op — a DB field's write order. This governs a push
	// that never touches that field. Two paths, two writes, two dedup
	// mechanisms: neither covers the other, which is why both exist.
	//
	// Placed AFTER the settled check on purpose, and the ordering comment above
	// is the reason: settled is read-only and ~free, this costs a member read.
	// Placed BEFORE decideHandoverNotice for the same reason — a row read is far
	// cheaper than the document fold that composing the notice runs.
	//
	// NOT claimed when it goes quiet. Claiming would spend the session's single
	// notice on a tick nobody was sent, so an agent whose wind-down is later
	// cleared would never be told it is near the line at all.
	if m, err := s.dal.GetMember(memberID); err == nil && m != nil &&
		winddownStageOf(*m) != winddownStageNone {
		return nil, false
	}
	signal := decideHandoverNotice(
		memberID, connRuntime, record,
		s.ctxHighConfig(), s.codexNoticeRoundSetting(), s.codexCompactionThreshold,
		notice)
	if signal == nil {
		return nil, false
	}
	// ONCE PER SESSION, not once per connection (T-c382). The dedup key is the
	// gauge's boot_ts — the SESSION anchor, restored from the durable member row
	// on reconnect — so an SSE flap mid-session cannot re-fire the notice.
	// Per-connection state (what this used to hold) would: every reconnect would
	// nudge again, which is the bombardment the owner asked to be rid of,
	// wearing a different hat.
	//
	// Build the frame BEFORE claiming: claiming first would burn the
	// one-and-only notice on a marshal failure and go silent forever, and
	// "sent it" vs "silently dropped it" would look identical.
	frame, err := directedFrameText(contextHighTopic, signal)
	if err != nil || !s.claimHandoverNotice(memberID, record) {
		return nil, false
	}
	return frame, true
}
