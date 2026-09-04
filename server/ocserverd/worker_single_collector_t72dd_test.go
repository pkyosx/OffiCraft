package main

import "testing"

// T-72dd 裁定二 — the symptom, and the single-collector guarantee.

// activeWorkerAtPct seeds an ACTIVE, desired-online, ONLINE worker whose gauge
// sits at pct, old enough that the boot-storm loop-guard cannot suppress it.
func activeWorkerAtPct(t *testing.T, s *apiServer, id string, pct float64, now float64) OutsourceWorker {
	t.Helper()
	w := fsmWorkerFixture(t, s, id, WorkerStatusActive, now-50_000)
	w.DesiredState = DesiredStateOnline
	putWorkerFixture(t, s, w)
	if _, err := s.hub.Connect(id, ""); err != nil {
		t.Fatalf("connect worker SSE: %v", err)
	}
	s.gauge.Set(id, map[string]any{
		"context_pct": pct, "context_pct_ts": now - 5, "boot_ts": now - 50_000,
	})
	s.workerSpawnTarget[id] = ServerSelfHost
	return w
}

// 🔴 THE SYMPTOM. An ACTIVE, desired-online worker past ctx.handover_pct must
// actually reach the 收口. Before this ticket it could not: autoHandoverWorker
// stamped the epoch, and then the tick's own gate (RefocusSince == 0) masked the
// only thing that collects it, so the handover sat open forever.
func TestWorkerContextHandover_ActuallyReachesTheCollect_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()
	cfg := s.ctxHighConfig()

	s.outsourceMu.Lock()
	w := activeWorkerAtPct(t, s, "ow-hi", float64(cfg.HandoverPct)+1, now)
	s.outsourceMu.Unlock()
	_ = w

	// Tick 1: the shared threshold pass opens the epoch (加速停止 — the SECOND
	// threshold is a clocked cause), and the shared FSM sees it in the SAME tick.
	s.runOutsourceTick(now)
	got, err := s.dal.GetOutsourceWorker("ow-hi")
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	t.Logf("AFTER TICK 1: refocus_since=%v refocus_op=%q stopped_since=%v",
		got.RefocusSince, got.RefocusOp, got.StoppedSince)
	if got.RefocusSince <= 0 || got.RefocusOp != refocusOpContextHigh {
		t.Fatalf("the SECOND threshold must open a context_high epoch, got op=%q since=%v",
			got.RefocusOp, got.RefocusSince)
	}

	// The agent works its close-out and reports stopped — the latch, no kill.
	if _, err := s.workerReportStopped("ow-hi", triggerServer); err != nil {
		t.Fatalf("report_stopped: %v", err)
	}
	if n := len(s.hub.DrainWardenCommands(ServerSelfHost)); n != 0 {
		t.Fatalf("the stopped-report must LATCH only — the kill belongs to the "+
			"FSM, got %d frames", n)
	}

	// Tick 2: the shared FSM collects. THIS is the frame the symptom was missing.
	s.runOutsourceTick(now + 1)
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	t.Logf("AFTER TICK 2: %d frame(s)", len(frames))
	for i, f := range frames {
		rpc, args := decodeWardenFrame(t, f.Frame)
		t.Logf("  frame[%d]: %s %v", i, rpc, args)
	}
	if got := countStops(t, frames); got != 1 {
		t.Fatalf("a context-high worker that filed its dump-done must be KILLED "+
			"exactly once, got %d stop(s) in %d frame(s)", got, len(frames))
	}
}

// 🔴 THE FIRST THRESHOLD OPENS A CLOCKLESS 停止, not the accelerated one. This is
// the half of staff parity the worker copy never had — it only ever knew
// handover_pct — and it is the half the owner ruled must carry NO clock.
func TestWorkerContextNotice_IsTheClocklessKind_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()
	cfg := s.ctxHighConfig()

	s.outsourceMu.Lock()
	activeWorkerAtPct(t, s, "ow-nt", float64(cfg.NoticePct), now)
	s.outsourceMu.Unlock()

	s.runOutsourceTick(now)
	got, err := s.dal.GetOutsourceWorker("ow-nt")
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	t.Logf("FIRST THRESHOLD (pct=%d): refocus_op=%q refocus_since=%v",
		cfg.NoticePct, got.RefocusOp, got.RefocusSince)
	if got.RefocusOp != refocusOpContextNotice {
		t.Fatalf("the FIRST threshold must open context_notice, got %q", got.RefocusOp)
	}
	kind, clocked := winddownKindFor(got.RefocusOp)
	t.Logf("winddownKindFor(%q) = kind=%q clocked=%v", got.RefocusOp, kind, clocked)
	if clocked || kind != offboardKindSoft {
		t.Fatalf("the first threshold must be the SOFT, CLOCKLESS kind — "+
			"kind=%q clocked=%v", kind, clocked)
	}
	// …and nothing was killed for merely crossing the soft line.
	if n := len(s.hub.DrainWardenCommands(ServerSelfHost)); n != 0 {
		t.Fatalf("the soft threshold must dispatch NO kill, got %d frames", n)
	}
}

// 🔴 ONE COLLECTOR. The same state run through two ticks must produce exactly
// ONE kill. This is the assertion that would catch a second collector being
// reintroduced — the failure mode is not a tidy duplicate but a kill that lands
// on the REPLACEMENT session.
func TestWorkerHandover_TwoTicksProduceExactlyOneKill_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()

	s.outsourceMu.Lock()
	w := fsmWorkerFixture(t, s, "ow-1c", WorkerStatusActive, now-50_000)
	w.DesiredState = DesiredStateOnline
	w.RefocusSince = now - 5_000
	w.RefocusOp = refocusOpRefocus // soft, clockless — collected by the dump only
	w.StoppedSince = now - 10      // the agent said it is done
	putWorkerFixture(t, s, w)
	if _, err := s.hub.Connect("ow-1c", ""); err != nil {
		t.Fatalf("connect: %v", err)
	}
	// A boot_ts BEFORE the epoch: the respawn has NOT landed, so the loop-break
	// must not fire and this really is the collectable state on both ticks.
	s.gauge.Set("ow-1c", map[string]any{"boot_ts": now - 50_000})
	s.workerSpawnTarget["ow-1c"] = ServerSelfHost
	s.outsourceMu.Unlock()

	s.runOutsourceTick(now)
	first := s.hub.DrainWardenCommands(ServerSelfHost)
	s.runOutsourceTick(now + 1) // same state, immediately again
	second := s.hub.DrainWardenCommands(ServerSelfHost)

	t.Logf("tick1: %d frame(s) / %d stop(s)   tick2: %d frame(s) / %d stop(s)",
		len(first), countStops(t, first), len(second), countStops(t, second))
	if got := countStops(t, first); got != 1 {
		t.Fatalf("tick 1 must collect exactly once, got %d stop(s)", got)
	}
	if countStops(t, second) != 0 {
		for i, f := range second {
			rpc, args := decodeWardenFrame(t, f.Frame)
			t.Logf("  EXTRA frame[%d]: %s %v", i, rpc, args)
		}
		t.Fatalf("tick 2 re-collected the SAME state — that is a second collector "+
			"(or a lost stop_retry de-dupe), and its kill lands on whatever session "+
			"is up by then; got %d extra stop(s)", countStops(t, second))
	}
}

// The promotion, which the worker copy never had: a context_notice epoch whose
// session keeps filling crosses handover_pct and BECOMES 加速停止.
func TestWorkerContextPromotion_NoticeBecomesAccelerated_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()
	cfg := s.ctxHighConfig()

	s.outsourceMu.Lock()
	activeWorkerAtPct(t, s, "ow-pr", float64(cfg.NoticePct), now)
	s.outsourceMu.Unlock()
	s.runOutsourceTick(now)
	first, _ := s.dal.GetOutsourceWorker("ow-pr")
	if first == nil || first.RefocusOp != refocusOpContextNotice {
		t.Fatalf("fixture: expected a context_notice epoch, got %+v", first)
	}

	// It keeps filling, past the SECOND threshold.
	s.gauge.Set("ow-pr", map[string]any{
		"context_pct": float64(cfg.HandoverPct) + 1, "context_pct_ts": now + 9,
		"boot_ts": now - 50_000,
	})
	s.runOutsourceTick(now + 10)
	got, _ := s.dal.GetOutsourceWorker("ow-pr")
	t.Logf("PROMOTION: %q -> %q (refocus_since %v -> %v)",
		first.RefocusOp, got.RefocusOp, first.RefocusSince, got.RefocusSince)
	if got.RefocusOp != refocusOpContextHigh {
		t.Fatalf("the second threshold must PROMOTE a notice epoch to context_high, got %q",
			got.RefocusOp)
	}
	if got.RefocusSince <= first.RefocusSince {
		t.Fatal("the promotion must re-stamp refocus_since, or the 加速停止 deadline " +
			"lands in the past and the worker is collected on a zero-second clock")
	}
}

// 🔴 THE OWNER'S CARVE-OUT, unchanged: an epoch the OWNER opened (重新聚焦) is
// never promoted, at ANY pct. Feeding workers through the staff pass must not
// quietly put a clock on a stop the owner asked to have none.
func TestWorkerOwnerOpenedEpoch_NeverPromotes_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()
	cfg := s.ctxHighConfig()

	s.outsourceMu.Lock()
	w := activeWorkerAtPct(t, s, "ow-ow", float64(cfg.HandoverPct)+5, now)
	w.RefocusSince = now - 100
	w.RefocusOp = refocusOpRefocus // the OWNER's 重新聚焦
	putWorkerFixture(t, s, w)
	s.outsourceMu.Unlock()

	s.runOutsourceTick(now)
	got, _ := s.dal.GetOutsourceWorker("ow-ow")
	t.Logf("OWNER EPOCH at pct=%d: refocus_op=%q (must stay %q)",
		cfg.HandoverPct+5, got.RefocusOp, refocusOpRefocus)
	if got.RefocusOp != refocusOpRefocus {
		t.Fatalf("an owner-opened epoch must NEVER be promoted, at any pct — "+
			"got %q. canPromoteToAcceleratedStop only promotes context_notice",
			got.RefocusOp)
	}
	if _, clocked := winddownKindFor(got.RefocusOp); clocked {
		t.Fatal("重新聚焦 must still run NO clock")
	}
}

// countStops reports how many of these warden frames are member `stop`s — the
// KILLS. The collect funnel dispatches stop+start together, so counting raw
// frames would conflate "collected twice" with "collected once and respawned".
// The invariant every test here is really asserting is about kills.
func countStops(t *testing.T, frames []wardenCmd) int {
	t.Helper()
	n := 0
	for _, f := range frames {
		if rpc, _ := decodeWardenFrame(t, f.Frame); rpc == reconcileCmdStop {
			n++
		}
	}
	return n
}

// 🔴 THE RE-READ BETWEEN THE TWO TICK PASSES (T-72dd, outsource_sched.go).
//
// The ACTIVE branch runs autoHandoverWorker and then the shared FSM. The first
// pass can CLOSE the epoch — that is the loop-break, which fires the moment a
// respawn boots (boot_ts > refocus_since). The row handed to the FSM must
// therefore be re-read, because the snapshot the tick loop is iterating still
// says refocus_since > 0 ∧ stopped_since > 0 — and to decideUp's recycle arm
// that reads as "a collected wind-down, still online, collect it". The session
// that is online at that moment is the REPLACEMENT.
//
// So a stale read here is not a tidiness问题: it is a kill aimed at a healthy
// agent that booted seconds ago. This test is the only thing standing on that
// line — feeding the FSM the stale snapshot instead makes it go red.
func TestTickReReadsRowBeforeFSM_SoTheLoopBreakIsNotOverruled_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()

	s.outsourceMu.Lock()
	w := fsmWorkerFixture(t, s, "ow-rr", WorkerStatusActive, now-50_000)
	w.DesiredState = DesiredStateOnline
	// A wind-down that was already COLLECTED: the epoch is open and the agent's
	// dump-done is latched. On its own that is precisely the recycle arm's
	// collect condition.
	w.RefocusSince = now - 500
	w.RefocusOp = refocusOpRefocus
	w.StoppedSince = now - 400
	putWorkerFixture(t, s, w)
	// …and the respawn HAS landed: a session booted AFTER the epoch was stamped.
	// This is what makes the loop-break fire on the first pass.
	s.gauge.Set("ow-rr", map[string]any{"boot_ts": now - 100})
	s.workerSpawnTarget["ow-rr"] = ServerSelfHost
	s.outsourceMu.Unlock()

	// The replacement session is ONLINE — the thing a stale read would kill.
	if _, err := s.hub.Connect("ow-rr", ""); err != nil {
		t.Fatalf("connect the replacement session: %v", err)
	}
	s.hub.DrainWardenCommands(ServerSelfHost)

	s.runOutsourceTick(now)

	// THE HARM FIRST, so a regression names the danger rather than a symptom.
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	stops := countStops(t, frames)
	got, err := s.dal.GetOutsourceWorker("ow-rr")
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	t.Logf("after the tick: %d frame(s), %d stop(s); online=%v refocus_since=%v",
		len(frames), stops, s.hub.IsOnline("ow-rr"), got.RefocusSince)
	if stops != 0 {
		t.Fatalf("the FSM was handed a STALE row and collected an epoch the "+
			"loop-break had just closed — that kill lands on the REPLACEMENT "+
			"session, which is alive and seconds old; got %d stop(s)", stops)
	}
	if !s.hub.IsOnline("ow-rr") {
		t.Fatal("the replacement session must still be up")
	}
	// …and the second half of the same damage: collecting off the stale row
	// writes that row back, so the epoch the loop-break just closed REAPPEARS and
	// the worker is stuck winding down forever.
	if got.RefocusSince != 0 {
		t.Fatalf("the closed epoch must stay closed — a collect off the stale "+
			"snapshot writes it back onto the row, got since=%v", got.RefocusSince)
	}
}

// 🔴 THE STOP ANCHOR IS SCOPED TO ONE EPOCH, NOT TO THE WORKER (T-72dd).
//
// decideUp's recycle arm de-dupes on `st.LastCommand == stop` + StopRetry. That
// is right WITHIN one wind-down (a STOP that did not land is re-sent, never
// doubled) and wrong ACROSS two: a worker handed over twice inside the retry
// window would have its SECOND collect swallowed by the FIRST one's anchor —
// a genuinely different wind-down, with its own 預告 already fanned at the
// agent, that nothing ever collects.
//
// This is asserted here by NAME because the only other test that covers it
// (TestRefocusWorker_BanksCostAcrossRespawn) says nothing about epochs in its
// title, so a future edit would read its failure as a cost-accounting problem.
func TestSecondHandoverIsNotSwallowedByTheFirstsStopAnchor_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()

	collectOnce := func(label string, stampAt float64) int {
		s.outsourceMu.Lock()
		w, err := s.dal.GetOutsourceWorker("ow-2e")
		if err != nil || w == nil {
			t.Fatalf("%s: read worker: %v", label, err)
		}
		w.RefocusSince = stampAt
		w.RefocusOp = refocusOpRefocus // clockless — collected by the dump alone
		w.StoppedSince = stampAt + 1   // the agent reported done
		if err := s.dal.PutOutsourceWorker(*w); err != nil {
			t.Fatalf("%s: stamp: %v", label, err)
		}
		seedWorkerAnchors(t, s, *w)
		// boot_ts stays BEFORE the epoch so the loop-break cannot close it first.
		s.gauge.Set("ow-2e", map[string]any{"boot_ts": now - 50_000})
		s.workerSpawnTarget["ow-2e"] = ServerSelfHost
		s.hub.DrainWardenCommands(ServerSelfHost)
		fresh, _ := s.dal.GetOutsourceWorker("ow-2e")
		s.reconcileWorkerLiveness(*fresh, stampAt+2)
		s.outsourceMu.Unlock()
		return countStops(t, s.hub.DrainWardenCommands(ServerSelfHost))
	}

	s.outsourceMu.Lock()
	w := fsmWorkerFixture(t, s, "ow-2e", WorkerStatusActive, now-50_000)
	w.DesiredState = DesiredStateOnline
	putWorkerFixture(t, s, w)
	s.outsourceMu.Unlock()
	if _, err := s.hub.Connect("ow-2e", ""); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if got := collectOnce("1st handover", now); got != 1 {
		t.Fatalf("the first handover must be collected once, got %d stop(s)", got)
	}
	// A SECOND handover, stamped strictly AFTER the first collect's stop (as any
	// real one is) but well inside StopRetry of it — the window in which the
	// stale anchor would swallow it.
	second := collectOnce("2nd handover", now+5)
	t.Logf("second handover inside stop_retry: %d stop(s)", second)
	if second != 1 {
		t.Fatalf("the second handover is its OWN epoch and must be collected on "+
			"its own — the first epoch's stop anchor must not swallow it; "+
			"got %d stop(s)", second)
	}
}
