package main

// api_infra.go — GET /api/events (the SSE downlink, spec/sse.md) and POST /api/mcp
// (JSON-RPC, spec/mcp.md). tools/list serves the frozen catalog
// spec/mcp-catalog.json (the wire SSOT); tools/call re-enters the route through
// the app's own mux with the caller's Authorization forwarded (mcp.go).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// The 15 s heartbeat period is contract (spec/sse.md §1).
const (
	sseHeartbeat = 15 * time.Second
	ssePoll      = 250 * time.Millisecond

	// Operator-facing detach log vocabulary: keep these values exact.
	// sseDetachReasonUnset must never equal a reason we can conclude —
	// setDetachReason's first-cause-wins compares against it (when the initial
	// value was peer-closed, later calls silently overwrote real causes).
	sseDetachReasonUnset           = ""
	sseDetachReasonTakeover        = "takeover"
	sseDetachReasonPeerClosed      = "peer-closed"
	sseDetachReasonWriteFailed     = "write-failed"
	sseDetachReasonStationShutdown = "station-shutdown"

	// Must equal stationSHAHeader in cli/ocagent/listen.go (the modules cannot
	// import each other). A mismatch fails silently: the client's Header.Get
	// returns "" and its connection line just omits the sha.
	sseStationSHAHeader = "X-Officraft-Station-Sha"
)

// markStationShutdown must run before the server cancels request contexts or
// the upgrade re-execs; otherwise a server shutdown is indistinguishable from a
// peer FIN/RST inside an SSE handler.
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

// sseWriteTimeout is only the BACKSTOP for a stuck/zero-window consumer whose
// send buffer has filled. The primary half-open reaper is TCP keepalive
// (server.go sseKeepAlive): a write to a silently vanished peer lands in the
// kernel send buffer and succeeds, so a write deadline alone would not detect it.
var sseWriteTimeout = 30 * time.Second

func (s *apiServer) HandleEventsApiEventsGet(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	memberID := ""
	machineID := ""
	if currentScope(r) == "agent" {
		memberID = currentActor(r)
		machineID = currentMachineClaim(r)
	}
	// Zombie SSE gate, checked BEFORE hub.Connect so a member the gate refuses can
	// never take the slot over (zombie-stop outranks takeover).
	if memberID != "" {
		if msg := s.sseStopGateRefusal(memberID); msg != "" {
			fmt.Fprintf(os.Stderr, "[sse] refused reconnect for %q: %s\n", memberID, msg)
			writeError(w, http.StatusConflict, msg)
			return
		}
	}
	listener, err := s.hub.Connect(memberID, machineID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if memberID != "" {
		fmt.Fprintf(os.Stderr, "[sse] attach member=%s gen=%d machine=%s\n",
			memberID, listener.Gen, machineID)
		s.onFirstConnect(memberID)
		// Token-key observation is recorded HERE, not at the auth gate: a renewing
		// warden presents its candidate credential to gated routes before writing it
		// to disk (cli/ocwarden/renewapply.go), so only this stream proves which key
		// the machine is actually running on.
		//
		// Must stay AFTER hub.Connect: the observation may enqueue a renew summons, and
		// enqueueToWarden is fail-closed on a machine the hub does not hold online.
		// Moved above Connect, the renew-summons tests go red (two of them report
		// PREMISE FAILED, pointing at their setup rather than at this ordering).
		s.noteTokenKeyObservation(claimsFromContext(r.Context()), verifyingKeyFromContext(r.Context()))
		s.stampLandedMachine(memberID, machineID)
		// After Connect, so the cross-machine sweep never leaves a zero-live-session
		// window.
		s.identitySweepOnConnect(memberID, machineID)
	}
	detachReason := sseDetachReasonUnset
	setDetachReason := func(reason string) {
		if detachReason == sseDetachReasonUnset {
			detachReason = reason
		}
	}
	defer func() {
		last := s.hub.Disconnect(listener)
		if memberID != "" {
			fmt.Fprintf(os.Stderr, "[sse] detach member=%s gen=%d last=%t reason=%s\n",
				memberID, listener.Gen, last, detachReasonForLog(detachReason))
		}
		if memberID != "" && last {
			s.onLastDisconnect(memberID)
			// Consume a warden's uninstall intent now, before any re-install could
			// reconnect into a standing kill order.
			s.consumeUninstallOnDisconnect(memberID)
		}
	}()

	wardenID := ""
	if memberID != "" {
		if m, err := s.dal.GetMember(memberID); err == nil && m != nil && m.Kind == KindWarden {
			wardenID = memberID
		}
	}

	rc := http.NewResponseController(w)
	armWriteDeadline := func() {
		if sseWriteTimeout > 0 {
			_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
		}
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	// Sent on the stream rather than via a separate probe: a changeover reconnects
	// the whole fleet within seconds. Same value /api/version reports as git_sha.
	w.Header().Set(sseStationSHAHeader, s.processSHA)
	w.WriteHeader(http.StatusOK)
	armWriteDeadline()
	if _, err := w.Write([]byte(": connected\n\n")); err != nil {
		setDetachReason(sseDetachReasonWriteFailed)
	}
	flusher.Flush()

	connRuntime := ""
	if memberID != "" {
		if m, err := s.dal.GetMember(memberID); err == nil && m != nil {
			connRuntime = m.Runtime
		} else if worker, err := s.dal.GetOutsourceWorker(memberID); err == nil && worker != nil {
			connRuntime = worker.Runtime
		}
	}
	lastTokenExpiryReminder := int64(0)
	nextTokenExpiryCheck := int64(0)

	// SOFT, always: the first context threshold is only an advance warning, so it
	// quotes no deadline.
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
		// Exiting here disconnects a live stream up to upgradeRestartDelay
		// (upgrade.go) early. It causes no reconnect churn only because clients back
		// off before re-dialing (ocagent listen, browser EventSource default) — read
		// from the code, not measured; a zero-backoff client would churn and nothing
		// here would go red.
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
			if s.stationShuttingDown.Load() {
				setDetachReason(sseDetachReasonStationShutdown)
			} else {
				setDetachReason(sseDetachReasonTakeover)
			}
			return
		default:
		}
		if frame := listener.pop(); frame != nil {
			if !write(frame) {
				return
			}
			continue
		}
		if memberID != "" {
			if frame, ok := s.handoverNoticeTick(
				memberID, connRuntime, noticeText); ok {
				if !write(frame) {
					return
				}
				continue
			}
		}
		if memberID != "" {
			now := time.Now().Unix()
			if now >= nextTokenExpiryCheck {
				claims := claimsFromContext(r.Context())
				remaining, validExpiry := tokenExpiryRemaining(claims, now)
				nextTokenExpiryCheck = tokenExpiryNextCheck(claims, now)
				switch {
				case !validExpiry:
				case remaining > tokenExpiryWarningWindow:
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
		// Warden commands go only onto THIS connection, never the owner fan-out: the
		// riding member_token is a secret.
		if wardenID != "" {
			if pending := s.hub.DrainWardenCommands(wardenID); len(pending) > 0 {
				for i, cmd := range pending {
					if !write(cmd.Frame) {
						// The drain already emptied the FIFO: hand the unwritten frames back or they
						// are lost silently. The hub decides what to requeue — a blind requeue would
						// put back a stale START that reconcile has already re-decided.
						s.hub.ReturnUndeliveredCommands(wardenID, pending[i:])
						return
					}
					// "Written" is not "delivered" (this band has no ack), but it is the
					// strongest event the server can observe.
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
			return
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

// sseStopGateRefusal returns a non-empty (409) message when memberID must not be
// admitted to /api/events, "" otherwise. It keeps a zombie `ocagent listen`
// that survived its kill from re-projecting a stopped member online. Refusing
// (rather than admitting without projecting) is deliberate: cli/ocagent listen
// treats the refusal as its signal to self-exit.
//
// Deliberately narrower than desired_state=="offline": a freshly hired member is
// desired-offline with no stop anchors and must stay admitted; a warden is
// desired-offline by default (dbseed / onboarding) and is removed through the
// one-shot uninstall intent instead.
func (s *apiServer) sseStopGateRefusal(memberID string) string {
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil {
		return ""
	}
	if m.Kind == KindOutsource && m.RosterStatus == RosterStatusRemoved {
		// A RELEASED worker's session deliberately lives on for its close-out duties
		// (worker_spawn.go reclaim grace) although its row is roster-removed, so the
		// roster gate below would wrongly refuse it.
		return ""
	}
	if m.RosterStatus != RosterStatusActive {
		return "member '" + m.ID + "' is removed from the roster — SSE refused " +
			"(a dismissed member must not re-project online)"
	}
	if m.Kind != KindWarden && parseDesired(m.DesiredState) == DesiredStateOffline &&
		(m.StoppingSince > 0.0 || m.StoppedSince > 0.0) {
		// …unless a graceful close-out is still in flight: the agent's listener
		// treats a run of refusals as "I have been retired" and kills its
		// tmux session (listen_run.go), so refusing here would take a mid-hand-off
		// session down with the hand-off unwritten. A member that reported stopped, or
		// was force-stopped, is still refused (gracefulStopEpochOpen, api_members.go).
		if m.StoppedSince <= 0.0 && gracefulStopEpochOpen(*m) {
			return ""
		}
		return "member '" + m.ID + "' has a stop in effect (desired_state=offline) — " +
			"SSE refused (a stopped member must not re-project online; " +
			"activate it to reconnect)"
	}
	return ""
}

func (s *apiServer) onFirstConnect(memberID string) {
	// Whatever the desired state: the ticks only sample desired-offline subjects, so
	// an anchor surviving a 活化 + reconnect would make the next stop's first offline
	// sample read as already past the confirm window, and collect it on the spot.
	s.offlineConfirmSince.Delete(memberID)
	s.publishOutsourcePresenceEdge(memberID)
	var cleared *Member
	if err := s.dal.inTx(func(tx *writeTx) error {
		m, err := getMemberOn(tx, memberID)
		if err != nil || m == nil || m.WakingSince <= 0 {
			return err
		}
		m.WakingSince = 0.0
		cleared = m
		return writeMemberOn(tx, *m)
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[sse] first-connect waking clear failed for %q: %v\n", memberID, err)
	} else if cleared != nil {
		s.publishMemberPatch(*cleared, memberID)
	}
	s.anchorSessionBoot(memberID)
}

// anchorSessionBoot keeps the gauge's boot_ts in agreement with the durable
// member.session_boot_ts and is the only place that decides whether this
// connect begins a new session. The gauge is emptied on a station re-exec while
// the agents survive it, so an existing durable anchor is RESTORED, never
// re-minted: otherwise every live session reads as seconds old after an upgrade
// and the min-liveness floor, context-high auto-recycle suppressor and worker
// auto-handover loop-break all misfire. A mid-session SSE flap must not reset it
// either. On this edge the anchor may only move backwards in time, never
// forwards.
func (s *apiServer) anchorSessionBoot(memberID string) {
	entry := s.gauge.Get(memberID)
	if entry == nil {
		entry = map[string]any{}
	}
	gaugeTS, gaugeHas := gaugeBootTS(entry)
	ts := nowSecs()
	if gaugeHas {
		ts = gaugeTS
	}

	// Judged and minted in one transaction: an anchor another writer stored
	// after a read outside it (a refused START's restore) is restored here, not
	// overwritten.
	stored := 0.0
	err := s.dal.inTx(func(tx *writeTx) error {
		m, err := getMemberOn(tx, memberID)
		if err != nil || m == nil {
			return err
		}
		if m.SessionBootTS > 0 {
			stored = m.SessionBootTS
			return nil
		}
		stored = ts
		return setMemberSessionBootTSOn(tx, memberID, ts)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[sse] session-boot anchor persist failed for %q: %v\n", memberID, err)
	}
	// The gauge is read again after the transaction, which may have waited for the write
	// connection: a report that landed on the entry meanwhile keeps its keys, and only boot_ts is
	// set here.
	entry = s.gauge.Get(memberID)
	if entry == nil {
		entry = map[string]any{}
	}
	gaugeTS, gaugeHas = gaugeBootTS(entry)
	if err != nil || stored == 0 {
		if gaugeHas {
			return
		}
		entry["boot_ts"] = ts
		s.gauge.Set(memberID, entry)
		return
	}
	if !gaugeHas || gaugeTS != stored {
		entry["boot_ts"] = stored
		s.gauge.Set(memberID, entry)
	}
}

// stampLandedMachine records the machine a session actually connected from.
// Readers: outsource placement (「沒被搬過 + 不是第一次 → 留在上一輪實際跑的那台」),
// the cockpit's pin comparison, and the kill chain's stop target for both
// populations (shutdown.go killTargetChain).
//
// Stamped on connect, not on dispatch: a dispatch may never boot, and sticking to
// it would make a failed boot permanent.
//
// Gated on connectionIsTheGenuineArticle (the same predicate
// identitySweepOnConnect uses): without it a residual ocagent on an old host
// would durably overwrite last_machine_id and the next rebirth would follow the
// ghost.
func (s *apiServer) stampLandedMachine(memberID, machineID string) {
	if machineID == "" {
		return
	}
	m, err := s.dal.GetMember(memberID)
	if err != nil || m == nil || m.LastMachineID == machineID {
		return
	}
	if !s.connectionIsTheGenuineArticle(*m, machineID) {
		return
	}
	// The genuine-article check takes outsourceMu, so it runs on the read above;
	// the stamp lands on the row as it is, and only while the pin it was judged
	// against still stands.
	var stamped *Member
	if err := s.dal.inTx(func(tx *writeTx) error {
		cur, err := getMemberOn(tx, memberID)
		if err != nil || cur == nil || cur.LastMachineID == machineID ||
			cur.DesiredMachineID != m.DesiredMachineID {
			return err
		}
		cur.LastMachineID = machineID
		stamped = cur
		return writeMemberOn(tx, *cur)
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[sse] landed-machine stamp failed for %q: %v\n", memberID, err)
	} else if stamped != nil {
		s.publishMemberPatch(*stamped, memberID)
	}
}

// clearSessionBootTS drops session-scoped state at a STOP boundary, so the next
// connect stamps a fresh anchor (any START snapshot is dropped too; START sites
// use clearSessionBootTSForStart).
//
// 🔴 It clears BOTH stores, the durable half unguarded by the gauge half: if they
// can disagree, a genuinely new session inherits its predecessor's old anchor
// and the respawn-storm guard waves it through. Do not add another writer.
func (s *apiServer) clearSessionBootTS(id string) {
	s.startClearedAnchorsMu.Lock()
	defer s.startClearedAnchorsMu.Unlock()
	delete(s.startClearedAnchors, id)
	s.clearSessionState(id)
}

func (s *apiServer) clearSessionState(id string) {
	if entry := s.gauge.Get(id); entry != nil {
		delete(entry, "boot_ts")
		// A Codex compaction count carried over a refocus would immediately recycle
		// the fresh replacement session.
		delete(entry, "compaction_count")
		// Both halves of the context report too: actionableContextPct (the gate) ignores
		// a pct not newer than boot_ts, but foldActorRuntime (the cockpit, wire.go) shows
		// it raw, so a leftover pair would display a number no threshold acts on. With
		// the ctx stale-guard setting OFF this also deliberately stops a dead session's
		// pct from driving auto-refocus.
		delete(entry, "context_pct")
		delete(entry, "context_pct_ts")
		s.gauge.Set(id, entry)
	}
	s.handoverNoticed.Delete(id)
	s.ctxGateDiagLast.Delete(id)
	// The anchor and the notice claim clear together or not at all. Each is
	// judged on its own: an early return on the anchor alone would leave a stale
	// claim that silences the next session's one notice.
	if err := s.dal.inTx(func(tx *writeTx) error {
		m, err := getMemberOn(tx, id)
		if err != nil || m == nil {
			return err
		}
		if m.SessionBootTS != 0 {
			if err := s.dal.SetMemberSessionBootTS(id, 0); err != nil {
				return err
			}
		}
		if m.HandoverNoticedTS != 0 {
			return s.dal.SetMemberHandoverNoticedTS(id, 0)
		}
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[sse] session-boot anchor and handover-notice claim clear failed for %q, neither cleared: %v\n", id, err)
	}
}

type sessionAnchorSnapshot struct {
	bootTS            float64
	handoverNoticedTS float64
	gauge             map[string]any
}

var sessionAnchorGaugeKeys = []string{"compaction_count", "context_pct", "context_pct_ts"}

// clearSessionBootTSForStart is clearSessionBootTS for a START dispatch. A START
// is only a request: the warden refuses it with session_already_exists when the
// old session is still alive, and that session must keep its anchor
// (restoreRefusedStartAnchor). A START that finds nothing anchored drops any
// older snapshot: an earlier START may have been accepted with its receipt
// lost, and that new session must not inherit the older anchor.
func (s *apiServer) clearSessionBootTSForStart(id string) {
	s.startClearedAnchorsMu.Lock()
	defer s.startClearedAnchorsMu.Unlock()
	snap, ok := s.currentSessionAnchor(id)
	if ok {
		if s.startClearedAnchors == nil {
			s.startClearedAnchors = map[string]sessionAnchorSnapshot{}
		}
		s.startClearedAnchors[id] = snap
	} else {
		delete(s.startClearedAnchors, id)
	}
	s.clearSessionState(id)
}

func (s *apiServer) currentSessionAnchor(id string) (sessionAnchorSnapshot, bool) {
	entry := s.gauge.Get(id)
	snap := sessionAnchorSnapshot{gauge: map[string]any{}}
	snap.bootTS, _ = gaugeBootTS(entry)
	for _, key := range sessionAnchorGaugeKeys {
		if v, has := entry[key]; has {
			snap.gauge[key] = v
		}
	}
	snap.handoverNoticedTS = s.cachedHandoverClaim(id)
	if m, err := s.dal.GetMember(id); err == nil && m != nil {
		if m.SessionBootTS > 0 {
			snap.bootTS = m.SessionBootTS
		}
		if m.HandoverNoticedTS != 0 {
			snap.handoverNoticedTS = m.HandoverNoticedTS
		}
	}
	return snap, snap.bootTS > 0
}

// restoreRefusedStartAnchor settles the snapshot on that START's receipt: only a
// session_already_exists refusal puts it back; any other receipt just drops it.
// If the agent reconnected before the receipt and minted a newer anchor, the
// older one still wins (it is the real session start) and a notice claim taken
// on the newer anchor moves with it, so the session is not told twice. Gauge
// readings re-reported since are newer and are left alone.
func (s *apiServer) restoreRefusedStartAnchor(id, rpc string, ok *bool, reason string) {
	if rpc != reconcileCmdStart && rpc != legacyWardenCmdWorkerStart {
		return
	}
	s.startClearedAnchorsMu.Lock()
	defer s.startClearedAnchorsMu.Unlock()
	snap, had := s.startClearedAnchors[id]
	delete(s.startClearedAnchors, id)
	if !had || ok == nil || *ok || !strings.HasPrefix(reason, spawnClobberReasonPrefix) {
		return
	}
	// The anchor and the claim are judged and written on the row inside one
	// transaction: a reconnect that minted a newer anchor, and a notice claimed
	// on it, since any earlier read are what the comparison must see.
	restored := false
	claim := 0.0
	err := s.dal.inTx(func(tx *writeTx) error {
		m, err := getMemberOn(tx, id)
		if err != nil || m == nil {
			return err
		}
		current := m.SessionBootTS
		if current > 0 && current <= snap.bootTS {
			return nil
		}
		if err := setMemberSessionBootTSOn(tx, id, snap.bootTS); err != nil {
			return err
		}
		claim = snap.handoverNoticedTS
		if current > 0 && m.HandoverNoticedTS == current {
			claim = snap.bootTS
		}
		if claim != m.HandoverNoticedTS {
			if err := setMemberHandoverNoticedTSOn(tx, id, claim); err != nil {
				return err
			}
		}
		restored = true
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[sse] session-boot anchor restore failed for %q: %v\n", id, err)
		return
	}
	if !restored {
		return
	}
	entry := s.gauge.Get(id)
	if entry == nil {
		entry = map[string]any{}
	}
	entry["boot_ts"] = snap.bootTS
	restoreAbsent := func(guard string, keys ...string) {
		if _, has := entry[guard]; has {
			return
		}
		for _, key := range keys {
			if v, has := snap.gauge[key]; has {
				entry[key] = v
			}
		}
	}
	restoreAbsent("compaction_count", "compaction_count")
	restoreAbsent("context_pct_ts", "context_pct", "context_pct_ts")
	s.gauge.Set(id, entry)

	if claim != 0 {
		s.handoverNoticed.Store(id, claim)
	} else {
		s.handoverNoticed.Delete(id)
	}
}

func (s *apiServer) onLastDisconnect(memberID string) {
	s.bankLiveCost(memberID)
	s.publishOutsourcePresenceEdge(memberID)
}

// publishOutsourcePresenceEdge: presence lives only in the Hub, so no durable
// write accompanies a connect/disconnect; the member delta is the owner
// cockpit's invalidation signal.
func (s *apiServer) publishOutsourcePresenceEdge(memberID string) {
	worker, err := s.dal.GetOutsourceWorker(memberID)
	if err != nil || worker == nil || worker.Status == WorkerStatusReleased {
		return
	}
	s.publishOutsourceWorker(*worker, triggerServer)
}

// bankLiveCost folds an actor's live telemetry cost into its durable
// banked_cost. It pops BEFORE the write on purpose: exactly-once banking on an
// edge that is not retried (a failed write only logs). An id that resolves to
// neither kind keeps its live figure.
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
	if m, err := s.dal.GetMember(actorID); err == nil && m != nil && m.Kind != KindOutsource {
		pop()
		if err := s.dal.AddMemberBankedCost(actorID, cost); err != nil {
			fmt.Fprintf(os.Stderr, "[bank] cost bank failed for member %q: %v\n", actorID, err)
			return
		}
		// Not decoration: the wind-down / recycle hooks key on a member delta naming
		// self, and this fold runs on the last-disconnect edge.
		m.BankedCost += cost
		s.publishMemberPatch(*m, actorID)
		return
	}
	if w, err := s.dal.GetOutsourceWorker(actorID); err == nil && w != nil {
		pop()
		if err := s.dal.AddMemberBankedCost(actorID, cost); err != nil {
			fmt.Fprintf(os.Stderr, "[bank] cost bank failed for worker %q: %v\n", actorID, err)
		}
	}
}

// dropLiveCost removes the live telemetry cost and reports what it removed.
// 🔴 Call it AFTER the durable write has succeeded: it is not undoable and the
// figure exists nowhere else. (bankLiveCost pops before its write for the
// opposite reason — do not copy that ordering here.)
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

// HandleResetCostApiMembersMemberIdCostResetPost — the cockpit's 成本歸零 button
// (owner ruling rc-7dea0deefa63).
//
// 🔴 Clears BOTH the durable banked_cost and the live telemetry figure: the
// cockpit adds the two, so clearing one looks like the button did nothing.
// Irreversible — no per-charge ledger exists — so the response is a receipt of
// what was destroyed; never grow it into an undo without a fresh owner ruling.
//
// A RELEASED worker is accepted (owner ruling rc-1344cc76a24a); a dismissed staff
// member is not.
func (s *apiServer) HandleResetCostApiMembersMemberIdCostResetPost(w http.ResponseWriter, r *http.Request, memberId string) {
	// target is the staff row or the outsource worker whose cost the reset
	// clears, or the 404.
	target := func() (*Member, *OutsourceWorker, error) {
		if m, err := s.dal.GetMember(memberId); err == nil && m != nil &&
			m.RosterStatus != RosterStatusRemoved && m.Kind != KindOutsource {
			return m, nil, nil
		}
		wk, err := s.dal.GetOutsourceWorker(memberId)
		if err != nil {
			return nil, nil, err
		}
		if wk == nil {
			return nil, nil, refuseInTx(http.StatusNotFound, "member '"+memberId+"' not found")
		}
		return nil, wk, nil
	}
	if _, _, err := target(); err != nil {
		writeTxError(w, err)
		return
	}
	var staff *Member
	var worker *OutsourceWorker
	var clearedBankedFig float64
	err := s.dal.inTx(func(*writeTx) error {
		var err error
		if staff, worker, err = target(); err != nil {
			return err
		}
		// 🔴 A single-column write: banked_cost is insert-only for putMember, so a
		// whole-row write would land nothing.
		clearedBankedFig, err = s.dal.ZeroMemberBankedCost(memberId)
		return err
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	// 🔴 Durable write first, live drop second.
	clearedBanked := nonZeroCost(clearedBankedFig)
	var cleared *float64
	if staff != nil {
		staff.BankedCost = 0
		s.publishMemberPatch(*staff, requestTrigger(r))
		cleared = s.dropLiveCost(memberId)
	} else {
		worker.BankedCost = 0
		cleared = s.dropLiveCost(memberId)
		s.publishOutsourceWorker(*worker, requestTrigger(r))
	}
	s.publishMonitoringSignal(memberId, requestTrigger(r))
	writeJSON(w, http.StatusOK, costResetDTO{
		MemberID:          memberId,
		ClearedCost:       cleared,
		ClearedBankedCost: clearedBanked,
	})
}

// accountSpendAccountedKey is the account accumulator's own high-water mark on
// the telemetry entry. 🔴 Separate from "cost" on purpose: the ingest overwrites
// "cost" in place before the accrual runs, and bankLiveCost deletes "cost" at
// session end — a baseline there would vanish and the next report would be
// credited a second time. Banking must NOT clear this key.
const accountSpendAccountedKey = "cost_accounted"

// accrueAccountSpend credits the new spend in one telemetry report to its
// account and never touches an actor figure (owner ruling rc-5c5d7c7c6dcd).
//
// 🔴 Reports are CUMULATIVE per session: a report lower than the baseline is a
// new session counting from zero, so its whole value is new spend (skipping it
// under-counts; subtracting makes the account go down; treating it as absolute
// erases earlier sessions). The baseline advances only after the write
// succeeds, so the next report re-carries a failed delta.
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
		entry[accountSpendAccountedKey] = cost
		return
	}
	if err := s.dal.AddAccountSpend(account, delta); err != nil {
		fmt.Fprintf(os.Stderr, "[account] spend accrual failed for %q: %v\n", account, err)
		return
	}
	entry[accountSpendAccountedKey] = cost
}

// startAccountSpendSession forgets the accrual baseline when the waking report
// says a new session began. The decrease fallback in accrueAccountSpend cannot
// see a restart whose first report lands at or above the old figure, nor an
// account change. A reporter that never reports waking keeps only that fallback:
// an accepted, silent under-count.
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

// HandleResetAccountCostApiAccountsCostResetPost — the cockpit's 帳號歸零 button
// (owner ruling rc-5c5d7c7c6dcd): touches no actor. Irreversible; the response
// is a receipt. An unknown account is not a 404: an account is a free telemetry
// string, so "no such account" and "nothing to clear" are the same state.
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
	var had float64
	err := s.dal.inTx(func(*writeTx) error {
		var err error
		had, err = s.dal.ZeroAccountSpend(account)
		return err
	})
	if err != nil {
		internalError(w, err)
		return
	}
	s.publishMonitoringSignal(account, requestTrigger(r))
	writeJSON(w, http.StatusOK, accountCostResetDTO{
		Account:     account,
		ClearedCost: nonZeroCost(had),
	})
}

// nonZeroCost mirrors foldActorRuntime's rule that 0 is not put on the wire, so
// the receipt and the read side share one null semantics.
func nonZeroCost(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

func (s *apiServer) publishMonitoringSignal(actorID, trigger string) {
	s.hub.Publish("monitoring", "signal", "monitoring", actorID, nil, audienceOwnerOnly(), trigger)
}

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

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

// toolsVisibleTo only narrows the listing; route middleware remains the
// enforcement boundary.
func (s *apiServer) toolsVisibleTo(principal principalClass, tools []any) []any {
	visible := make([]any, 0, len(tools))
	for _, raw := range tools {
		descriptor, isObj := raw.(map[string]any)
		if !isObj {
			continue
		}
		name, _ := descriptor["name"].(string)
		spec, known := s.mcpTools[name]
		if !known || !routeReachableBy(principal, spec.Requires) {
			continue
		}
		visible = append(visible, raw)
	}
	return visible
}

// retiredMCPTools refuses removed MCP tool names BY NAME ("that mechanism is
// gone", not "you mistyped"). 🔴 Deliberately neither a route nor an mcpTools
// row: a route would re-list the names in the generated MCP catalog and OpenAPI
// spec (and the drift gates would then enforce that); a placeholder row would
// forward the call.
var retiredMCPTools = map[string]string{
	"replace_lessons": "retired tool: 'replace_lessons' was removed together with the lessons " +
		"document, which no longer exists — record what you learned with 'write_lore_entry'",
	"patch_lessons": "retired tool: 'patch_lessons' was removed together with the lessons " +
		"document, which no longer exists — record what you learned with 'write_lore_entry'",
	"patch_task_learnings": "retired tool: 'patch_task_learnings' was removed together with the " +
		"task manual's learnings document, which no longer exists — record what you learned " +
		"with 'write_lore_entry'",
	"list_outsource_workers": "retired tool: 'list_outsource_workers' was removed together with the " +
		"outsource-only worker surface, which no longer exists — outsource members are listed by the " +
		"same roster read as everyone else, so use 'get_members'",
	"refocus_outsource_worker": "retired tool: 'refocus_outsource_worker' was removed together with " +
		"the outsource-only worker surface, which no longer exists — refocus an outsource member " +
		"through the same door as staff, so use 'refocus_member'",
	"stop_outsource_worker": "retired tool: 'stop_outsource_worker' was removed together with the " +
		"outsource-only worker surface, which no longer exists — take an outsource member down " +
		"through the same door as staff, so use 'deactivate_member'",
	"restart_outsource_worker": "retired tool: 'restart_outsource_worker' was removed together with " +
		"the outsource-only worker surface, which no longer exists — bring an outsource member back " +
		"up through the same door as staff, so use 'activate_member'",
	"set_outsource_worker_model": "retired tool: 'set_outsource_worker_model' was removed together " +
		"with the outsource-only worker surface, which no longer exists — the model of an outsource " +
		"member is edited by the same write as the model of a staff member, so use 'update_member'",
	"accelerated_stop_outsource_worker": "retired tool: 'accelerated_stop_outsource_worker' was " +
		"removed together with the outsource-only worker surface, which no longer exists — the " +
		"accelerated stop is the same act on both sides now, so use 'accelerated_stop_member'",
	"force_stop_outsource_worker": "retired tool: 'force_stop_outsource_worker' was removed together " +
		"with the outsource-only worker surface, which no longer exists — the forced stop is the " +
		"same act on both sides now, so use 'force_stop_member'",
}

func retiredToolMessage(name string) (string, bool) {
	message, retired := retiredMCPTools[name]
	return message, retired
}

func (s *apiServer) HandleMcpApiMcpPost(w http.ResponseWriter, r *http.Request) {
	var payload any
	dec := json.NewDecoder(r.Body)
	// UseNumber keeps numbers as their JSON literals: the id echoes back unmangled
	// and argument splitting renders "3" vs "3.0" exactly as received.
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
		rpcResult(w, id, map[string]any{"tools": s.toolsVisibleTo(s.principalOfRequest(r), tools)})
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
		// Must run before the mcpTools lookup (retired names are on no route row).
		// -32602 is the contract — conformance pins every parameter violation to it;
		// the wording is not.
		if message, retired := retiredToolMessage(name); retired {
			rpcError(w, id, rpcInvalidParams, message)
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
		reqPath, rawQuery, body, splitErr := splitToolArguments(spec, arguments)
		if splitErr != nil {
			// A missing path argument is a tool-level 422 in CallToolResult shape, not a
			// JSON-RPC invalid-params error, so the caller gets the missing field name.
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

// handoverNoticeTick is one quiet tick of the context-high band.
//
// 🔴 handoverNoticeSettled (read-only, ~free) is asked FIRST: decideHandoverNotice
// returns non-nil on EVERY tick past the notice point and composing it folds a
// durable document. Reversed, nothing on the wire changes but a spent session
// pays ~374µs per tick instead of ~246ns.
func (s *apiServer) handoverNoticeTick(
	memberID, connRuntime string, notice func() string,
) ([]byte, bool) {
	record := s.gauge.Get(memberID)
	if s.handoverNoticeSettled(memberID, record) {
		return nil, false
	}
	// Already winding down ⇒ say nothing (owner, 2026-08-24:
	// 「下線 → 加速 → 強制。後者一旦發出我們就不該發出前者」). Do NOT claim
	// when going quiet: that would spend the session's single notice, and an agent
	// whose wind-down is later cleared would never be told.
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
	// Build the frame BEFORE claiming: claiming first would burn the only notice on
	// a marshal failure and go silent forever.
	frame, err := directedFrameText(contextHighTopic, signal)
	if err != nil || !s.claimHandoverNotice(memberID, record) {
		return nil, false
	}
	return frame, true
}
