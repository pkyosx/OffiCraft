package main

// api_activity.go — the ACTIVITY channel: whether an actor is running a turn
// right now. This is a SECOND dimension beside presence, not a refinement of it:
// `online` is a pure SSE-connection projection (two states, no heartbeat, no
// TTL — state-model.md §3) and deliberately says nothing about whether the model
// is working. The verdict vocabulary and the threshold live in domain.go
// (deriveActivity / activityMaxTurnSecs); this file owns the store, the
// ingestion handler, and the two lifecycle edges.
//
// STORAGE: its OWN in-memory store (s.activity), NOT a corner of s.telemetry.
// Two reasons, both contractual:
//   1. telemetry's published contract is "cleared only when a member is
//      dismissed, NEVER on disconnect" (frontend/CLAUDE.md, verbatim). Activity
//      needs the disconnect edge; sharing the store would turn that sentence
//      into "it depends".
//   2. telemetry holds MEASUREMENT SAMPLES, activity holds a CLAIM. Their
//      de-duplication and expiry rules are different, and the last thing this
//      surface needs is one store with two sets of rules.
// Like both siblings it is volatile by design — a restart honestly forgets every
// claim (state-model.md: observed ⇒ memory, never DB).

import (
	"fmt"
	"net/http"
	"os"
)

// The activity store's entry keys. The entry is a plain map so it can live in
// the same memStore type the gauge and telemetry use; these constants keep the
// key strings from drifting between the writer and the four readers.
const (
	activityKeyState        = "state"         // "active" while a turn claim is held; "" once closed
	activityKeySince        = "since"         // server receive time of the report that opened the claim
	activityKeyLastEnd      = "last_end"      // server receive time of the last OBSERVED turn end
	activityKeyTurnID       = "turn_id"       // the claimed turn, for the idle↔active pairing
	activityKeySessionID    = "session_id"    // the reporter session — the scope of seq
	activityKeySeq          = "seq"           // reporter-monotonic ordering key
	activityKeyTS           = "ts"            // server receive time of the last accepted report
	activityKeyOfflineSince = "offline_since" // server time of the last SSE drop, 0 = connected/never seen
)

// activityStore returns the activity memStore, tolerating a dependency-free
// apiServer (the dozens of hand-built test tables in this package). A nil store
// makes every activity path a clean no-op instead of a panic — the same posture
// the rest of the observation plumbing takes toward missing dependencies.
func (s *apiServer) activityStore() *memStore {
	return s.activity
}

// activityGraceSecs is the reconnect window a turn claim survives an SSE drop
// for — the EXISTING ZombieConfirmGrace (180s), never a second number of our
// own. It reads from the live reconcile config; a zero value means the config
// was never installed (a dependency-free test carrier), so fall back to the
// declared default rather than silently degrading the window to "discard every
// claim on every blip".
func (s *apiServer) activityGraceSecs() float64 {
	if s.reconcileCfg.ZombieConfirmGrace > 0 {
		return s.reconcileCfg.ZombieConfirmGrace
	}
	return defaultReconcileConfig().ZombieConfirmGrace
}

// activityClaimFromEntry reads one actor's stored claim into the pure-function
// input. An absent entry is the zero claim (Reported=false ⇒ ActivityNever),
// which is exactly what "this actor never reported" means.
func activityClaimFromEntry(entry map[string]any) activityClaim {
	if entry == nil {
		return activityClaim{}
	}
	state, _ := entry[activityKeyState].(string)
	since, _ := entry[activityKeySince].(float64)
	lastEnd, _ := entry[activityKeyLastEnd].(float64)
	return activityClaim{
		Reported: true,
		Active:   state == ActivityActive,
		Since:    since,
		LastEnd:  lastEnd,
	}
}

// activityOf is the READ-PATH projection both wires call: a store entry → the
// wire triple (state, working_since, last_turn_completed_at). `online` is the
// caller's already-resolved hub.IsOnline fact — passed in rather than looked up
// so a fold that is already iterating a presence snapshot does not ask the hub
// once per row.
func activityOf(entry map[string]any, online bool, now float64) (string, *float64, *float64) {
	return deriveActivity(activityClaimFromEntry(entry), online, now)
}

// activityEntryOf reads ONE actor's stored entry (nil-safe) — the per-row read
// the outsource projection uses.
func (s *apiServer) activityEntryOf(actorID string) map[string]any {
	store := s.activityStore()
	if store == nil {
		return nil
	}
	return store.Get(actorID)
}

// activitySnapshot returns the whole store (nil-safe) for the monitoring fold,
// mirroring how the telemetry/gauge snapshots are taken once per request.
func (s *apiServer) activitySnapshot() map[string]map[string]any {
	store := s.activityStore()
	if store == nil {
		return map[string]map[string]any{}
	}
	return store.Snapshot()
}

// POST /api/self/activity — one turn-boundary report (report_activity).
//
// Identity comes from the verified token sub and NOTHING else (§14 self-op: a
// caller never says who it is). The handler deliberately does NOT resolve a
// member row: outsource workers (`ow-` ids) have none, and they are exactly the
// sessions the owner most wants an activity reading on. Same shape as
// HandleGetMyTaskApiSelfTaskGet, which reads the sub directly for the same
// reason.
func (s *apiServer) HandleReportActivityApiSelfActivityPost(w http.ResponseWriter, r *http.Request) {
	var body ActivityReportDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.State != ActivityActive && body.State != ActivityIdle {
		writeError(w, http.StatusBadRequest, "state must be 'active' or 'idle'")
		return
	}
	actorID := currentActor(r)
	store := s.activityStore()
	if store == nil {
		// No store wired (dependency-free carrier) — answer honestly rather than
		// claiming a report was recorded.
		writeJSON(w, http.StatusOK, ActivityReportResultDTO{
			Accepted: false, ActivityState: ActivityNever,
		})
		return
	}
	turnID := ""
	if body.TurnId != nil {
		turnID = *body.TurnId
	}
	sessionID := ""
	if body.SessionId != nil {
		sessionID = *body.SessionId
	}
	var seq float64
	if body.Seq != nil {
		seq = *body.Seq
	}

	now := nowSecs()
	online := s.hub.IsOnline(actorID)
	prior := store.Get(actorID)
	beforeState, beforeSince, beforeEnd := activityOf(prior, online, now)

	next, accepted := applyActivityReport(prior, activityReport{
		State:     body.State,
		TurnID:    turnID,
		SessionID: sessionID,
		Seq:       seq,
		Now:       now,
	})
	if accepted {
		store.Set(actorID, next)
	}
	afterState, afterSince, afterEnd := activityOf(next, online, now)

	// Publish only when the DERIVED value actually moved. The reporters are
	// event-driven, but a Codex thread/status/changed echo or a hook that fires
	// twice would otherwise fan an SSE frame per no-op — the same discipline
	// stampWorkerPlacementBlocked follows ("write only when the reason changed").
	if accepted && activityWireChanged(beforeState, beforeSince, beforeEnd, afterState, afterSince, afterEnd) {
		s.publishActivityChange(actorID, requestTrigger(r))
	}
	writeJSON(w, http.StatusOK, ActivityReportResultDTO{
		Accepted: accepted, ActivityState: afterState,
	})
}

// activityReport is one normalized inbound report.
type activityReport struct {
	State     string
	TurnID    string
	SessionID string
	Seq       float64
	Now       float64
}

// applyActivityReport folds one report onto the stored entry. Pure apart from
// map allocation: returns the entry to store and whether anything was accepted.
//
// The four ordering rules (§3.4 of the design):
//
//	R1 within one session_id, a report whose seq is not GREATER than the stored
//	   one is dropped — out-of-order protection;
//	R2 a CHANGED session_id is a new reporter session: accepted, the seq
//	   baseline resets, and any claim held from the old session is discarded (a
//	   new session is never the old session's turn);
//	R3 an "idle" only closes the claim when its turn_id names the claimed turn
//	   (or either side is blank), so a late idle from the previous turn cannot
//	   kill the current one;
//	R4 a re-send of the same (state, turn_id) is an idempotent no-op — stored,
//	   but with no derived change, so nothing is published.
func applyActivityReport(prior map[string]any, rep activityReport) (map[string]any, bool) {
	next := map[string]any{}
	for k, v := range prior {
		next[k] = v
	}
	priorSession, _ := next[activityKeySessionID].(string)
	priorSeq, _ := next[activityKeySeq].(float64)
	sameSession := prior != nil && priorSession == rep.SessionID
	if sameSession && rep.Seq > 0 && priorSeq > 0 && rep.Seq <= priorSeq {
		return prior, false // R1
	}
	if !sameSession && prior != nil {
		// R2 — a different reporter session cannot own the previous claim.
		delete(next, activityKeyState)
		delete(next, activityKeyTurnID)
		delete(next, activityKeySince)
	}
	priorState, _ := next[activityKeyState].(string)
	priorTurn, _ := next[activityKeyTurnID].(string)

	switch rep.State {
	case ActivityActive:
		if priorState == ActivityActive && priorTurn == rep.TurnID && rep.TurnID != "" {
			// R4 — the same turn re-announced. Refresh the bookkeeping but keep
			// `since` anchored to the FIRST announcement: a repeated report must
			// not make a long turn look like it just started.
			break
		}
		next[activityKeyState] = ActivityActive
		next[activityKeyTurnID] = rep.TurnID
		next[activityKeySince] = rep.Now
	case ActivityIdle:
		if priorState == ActivityActive {
			// R3 — only the claimed turn may close the claim.
			if priorTurn != "" && rep.TurnID != "" && priorTurn != rep.TurnID {
				return prior, false
			}
			// A claim genuinely ended, and we watched it end: this is the ONLY
			// place last_end is ever stamped.
			next[activityKeyLastEnd] = rep.Now
		}
		delete(next, activityKeyState)
		delete(next, activityKeyTurnID)
		delete(next, activityKeySince)
	}
	next[activityKeySessionID] = rep.SessionID
	next[activityKeySeq] = rep.Seq
	next[activityKeyTS] = rep.Now
	// A live report means the actor is talking to us; the drop stamp from an
	// earlier disconnect has been superseded.
	delete(next, activityKeyOfflineSince)
	return next, true
}

// activityWireChanged reports whether the three wire values actually moved.
func activityWireChanged(s1 string, since1, end1 *float64, s2 string, since2, end2 *float64) bool {
	if s1 != s2 {
		return true
	}
	return !floatPtrEqual(since1, since2) || !floatPtrEqual(end1, end2)
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// publishActivityChange fans the change on the EXISTING topics — the SSE topic
// set is closed at 12 (spec/sse.md §3.1) and this票 opens none. Members ride the
// owner-only `monitoring` signal the telemetry ingest already uses (the cockpit
// reconciles by refetch, so a bare "something moved" is enough); outsource
// workers ride their own `outsource_worker` delta, which their panel and the
// monitor page's worker rows already subscribe to.
func (s *apiServer) publishActivityChange(actorID, trigger string) {
	if s.hub == nil {
		return
	}
	s.hub.Publish("monitoring", "signal", "monitoring", actorID, nil, audienceOwnerOnly(), trigger)
	if s.dal == nil {
		return
	}
	worker, err := s.dal.GetOutsourceWorker(actorID)
	if err != nil || worker == nil || worker.Status == WorkerStatusReleased {
		return
	}
	s.publishOutsourceWorker(*worker, trigger)
}

// activityOnDisconnect records WHEN the actor's stream dropped. It deliberately
// does NOT clear the claim and does NOT stamp last_end.
//
// Not clearing: `ocagent listen` reconnects through network blips constantly
// (two observed inside a single session while this was written), and Claude only
// speaks again at a turn boundary — so a claim dropped on a blip is gone for the
// REST OF THAT TURN. The display requirement ("an offline agent must never read
// as working") is met by GATING on `online` in deriveActivity instead, which is
// reversible where deletion is not. The reconnect edge then decides whether the
// claim survived, using the existing ZombieConfirmGrace.
//
// Not stamping last_end: we did not observe the turn end, we observed the
// session vanish. Writing a completion time we never saw is a fabrication.
//
// Only touches an entry that ALREADY exists — otherwise "never reported" would
// silently become "reported", and the wire's `never` would stop meaning it.
func (s *apiServer) activityOnDisconnect(actorID string) {
	store := s.activityStore()
	if store == nil {
		return
	}
	entry := store.Get(actorID)
	if entry == nil {
		return
	}
	entry[activityKeyOfflineSince] = nowSecs()
	store.Set(actorID, entry)
}

// activityOnConnect decides at the reconnect edge whether a claim held from
// before the drop is a blip survivor (keep — same turn, still running) or a real
// absence (discard — that was a different life). See
// activityReconnectKeepsClaim for why the window is the EXISTING 180s grace.
func (s *apiServer) activityOnConnect(actorID string) {
	store := s.activityStore()
	if store == nil {
		return
	}
	entry := store.Get(actorID)
	if entry == nil {
		return
	}
	offlineSince, _ := entry[activityKeyOfflineSince].(float64)
	now := nowSecs()
	// No recorded drop means this is not a RECONNECT at all — it is the 0→1
	// edge, and there is nothing to judge. Scoping the judgement here (rather
	// than letting activityReconnectKeepsClaim's "no drop ⇒ not a surviving
	// blip" answer stand in for it) is what keeps the BOOT TURN alive:
	// seeds/boot_sequence.md step 3 mandates that `ocagent listen` is hung LAST
	// ("三步順序不可換，掛 SSE 永遠壓最後"), so an agent has already reported
	// `active` from its UserPromptSubmit hook by the time it first connects.
	// Discarding here would make every spawn / recycle / refocus read 閒置 while
	// the agent is provably mid-turn, and would leave that turn's end
	// unobservable (the Stop would find no claim to close).
	if offlineSince > 0 && !activityReconnectKeepsClaim(offlineSince, now, s.activityGraceSecs()) {
		if state, _ := entry[activityKeyState].(string); state == ActivityActive {
			fmt.Fprintf(os.Stderr,
				"[activity] dropping stale turn claim for %q (offline %.0fs)\n",
				actorID, now-offlineSince)
		}
		delete(entry, activityKeyState)
		delete(entry, activityKeyTurnID)
		delete(entry, activityKeySince)
	}
	delete(entry, activityKeyOfflineSince)
	store.Set(actorID, entry)
}

// activityForget drops an actor's whole activity record. Called from the ONE
// place telemetry is dropped too — the staff hard-delete — so a dismissed
// member leaves nothing behind.
func (s *apiServer) activityForget(actorID string) {
	if store := s.activityStore(); store != nil {
		store.Delete(actorID)
	}
}
