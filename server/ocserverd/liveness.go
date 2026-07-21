package main

// liveness.go — the agent liveness (STUCK-suspect) signal (T-5896, layer 1:
// SIGNAL ONLY). It reads a signal that already flows but nobody consumed: the
// per-member context-report timestamp (gauge "ts", stamped on every statusLine
// tick — api_monitoring.go HandleIngestAgentContextApiAgentContextPost). Nobody
// subtracted it from now to ask "has this session made ANY progress lately?".
// This file adds exactly that subtraction and NOTHING else — it NEVER dispatches
// a remedy (no stop / refocus / restart). Auto-remediation is the layer the
// owner explicitly deferred: a false "stuck" light costs one glance; a false
// auto-kill vaporises a working agent's context.

// stuckSilenceSecs is the liveness "no activity" floor: a member is a STUCK
// suspect only after its last context report is older than this. Because the
// report is re-stamped on every statusLine tick, a session actively burning
// tokens keeps it fresh; a silence this long means ZERO observable progress for
// the window.
//
// Sized deliberately LARGE (15 min) to bias the failure toward UNDER-flagging:
// a legitimate long single operation (a big build, a long test run, a deep
// research turn) must not trip it, and an owner-visible signal is only useful
// if it is rarely wrong — a noisy light becomes an ignored light. Comfortably
// above minSelfRestartSecs (600s). A plain const like the reconcile timers and
// minSelfRestartSecs; promote to a runtime setting only if practice shows it
// needs field tuning.
const stuckSilenceSecs = 900.0

// gaugeLastReportSecs is seconds since a member's LAST context report (gauge
// "ts", stamped every statusLine tick — api_monitoring.go). nil when there is
// no usable ts (missing gauge / server-restart amnesia / never reported) so
// every caller FAILS OPEN — no report ts means "not stuck", never a fabricated
// alarm. The last-report twin of gaugeSecsSinceBoot (which anchors on boot_ts,
// a different question); reading THIS against now is the one step T-5896 adds.
func gaugeLastReportSecs(record map[string]any, now float64) *float64 {
	if record == nil {
		return nil
	}
	ts, ok := asNumber(record["ts"])
	if !ok || ts <= 0 {
		return nil
	}
	secs := now - ts
	return &secs
}

// totalUnread sums an UnreadCounts per-sender map into the member's whole
// inbound-unread count (the "something is waiting for it" magnitude).
func totalUnread(perSender map[string]int) int {
	total := 0
	for _, n := range perSender {
		total += n
	}
	return total
}

// livenessSignal is the stuck-suspect verdict for one member. Pure value.
// IdleSecs is nil exactly when there is no usable report ts (fail-open input);
// Reason is a human diagnosis for the reconcile log.
type livenessSignal struct {
	Stuck    bool
	IdleSecs *float64
	Reason   string
}

// decideLiveness is the PURE stuck-suspect decision for one member. A member is
// a stuck suspect iff ALL THREE hold — this triple is the whole design, do not
// collapse it:
//
//   - online: it still holds its SSE (the cockpit "online" light is on).
//     Offline is not stuck, it is offline (presence / zombie-takeover own that).
//     The failure this catches is precisely "body wedged but the SSE-holding
//     child still alive → the light stays green" (persona §8) — so online is a
//     PRECONDITION, never an exclusion.
//   - silent: seconds-since-last-report > silenceSecs. A missing report ts
//     fails OPEN (not stuck) — never invent an alarm from absent data.
//   - something waiting: unreadInbound > 0 — a message was delivered to it that
//     it has not even read. An IDLE agent (nothing waiting) is legitimately
//     quiet; silence ALONE must never flag it. This is the sentinel that keeps
//     a normal quiet agent off the board, and it matters more than catching a
//     real stuck one (a false light on a working agent is the expensive error).
//
// KNOWN LIMIT (inherited, not solved): a member honestly inside one long tool
// call is externally identical to a wedged one — statusLine only re-stamps at
// turn boundaries, so neither re-stamps "ts". The large silenceSecs biases that
// unavoidable miss toward under-flagging (a real stall surfaces late; a long
// legit op is not flagged), matching the owner's low-false-positive ruling.
//
// unreadInbound is consulted ONLY on the final (online && silent) path; callers
// may pass 0 when they short-circuit before that, but the value passed on the
// stuck path MUST be the member's true inbound-unread count.
func decideLiveness(
	record map[string]any, unreadInbound int, online bool, silenceSecs, now float64,
) livenessSignal {
	idle := gaugeLastReportSecs(record, now)
	if !online {
		return livenessSignal{IdleSecs: idle, Reason: "offline — not a liveness candidate"}
	}
	if idle == nil {
		return livenessSignal{Reason: "no report ts — fail open (not stuck)"}
	}
	if *idle <= silenceSecs {
		return livenessSignal{IdleSecs: idle, Reason: "reporting — fresh within the silence window"}
	}
	if unreadInbound <= 0 {
		return livenessSignal{
			IdleSecs: idle,
			Reason:   "silent but idle (nothing waiting) — legitimately quiet, not stuck",
		}
	}
	return livenessSignal{
		Stuck:    true,
		IdleSecs: idle,
		Reason:   "STUCK suspect: online, no report past the silence window, unread inbound waiting",
	}
}
