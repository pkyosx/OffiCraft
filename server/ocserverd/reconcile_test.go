package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// ── fixtures ─────────────────────────────────────────────────────────────────

// newReconcileTestServer wires a full apiServer (temp sqlite + migrations +
// seed + hub + checkout-root assets) — the producer integration face.
func newReconcileTestServer(t *testing.T) *apiServer {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "reconcile-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return newAPIServer(dal, NewHub(), singleKeyring([]byte("reconcile-test-secret")), 3600, "../..")
}

func testAgent(id string) Member {
	return Member{
		ID: id, Name: id, Kind: KindStaff, Effort: "medium",
		DesiredState:     DesiredStateOnline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
	}
}

// putTestMember seeds a member row so that the ROW ENDS UP LOOKING LIKE `m` —
// which is what every caller has always meant by it.
//
// 🔴 THE SECOND WRITE IS NOT REDUNDANT (T-55). The four wind-down anchors left
// PutMember's DO UPDATE SET, so on a row that ALREADY EXISTS the upsert above
// silently drops stopping_since / stopped_since / refocus_since / refocus_op.
// (An earlier version of this sentence said the upsert "carries 31 columns".
// That was the INSERT list minus four; the DO UPDATE SET list is shorter still,
// because every other migrated column is missing from it too. The count is not
// restated here — singleColumnOwnedFields is the enforced answer.) Fixtures that re-seed a row to open or close a
// wind-down — and there are dozens — would then be asserting against anchors
// they never actually planted, and the tests would go GREEN while testing
// nothing. Planting them through their sole writer is what keeps the helper's
// contract true. Production callers must NOT do this: there the dropped columns
// are the whole point, because their snapshot is stale.
func putTestMember(t *testing.T, s *apiServer, m Member) {
	t.Helper()
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("put member %s: %v", m.ID, err)
	}
	if err := s.dal.SetMemberWindDownAnchors(m.ID, m.StoppingSince, m.StoppedSince,
		m.RefocusSince, m.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors for %s: %v", m.ID, err)
	}
}

// seedMemberAnchors / seedWorkerAnchors plant the four wind-down anchors on a row
// that ALREADY EXISTS, through their sole writer (T-55).
//
// A fixture that reads a row, sets stopping_since / stopped_since /
// refocus_since / refocus_op on the snapshot and writes it back whole no longer
// moves those four columns — they left PutMember's DO UPDATE SET. The write
// still succeeds, so the fixture reads exactly as it always did while planting
// nothing, and the test that depends on it goes green having exercised the
// wrong state. Call one of these next to the whole-row fixture write.
func seedMemberAnchors(t *testing.T, s *apiServer, m Member) {
	t.Helper()
	if err := s.dal.SetMemberWindDownAnchors(m.ID, m.StoppingSince, m.StoppedSince,
		m.RefocusSince, m.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors for %s: %v", m.ID, err)
	}
}

func seedWorkerAnchors(t *testing.T, s *apiServer, w OutsourceWorker) {
	t.Helper()
	if err := s.dal.SetMemberWindDownAnchors(w.ID, w.StoppingSince, w.StoppedSince,
		w.RefocusSince, w.RefocusOp); err != nil {
		t.Fatalf("seed wind-down anchors for %s: %v", w.ID, err)
	}
}

// connectOnline projects memberID online for the test's lifetime.
func connectOnline(t *testing.T, s *apiServer, memberID string) *hubListener {
	t.Helper()
	l, err := s.hub.Connect(memberID, "")
	if err != nil {
		t.Fatalf("connect %s: %v", memberID, err)
	}
	t.Cleanup(func() { s.hub.Disconnect(l) })
	return l
}

type drainedFrame struct {
	Topic string
	RPC   string
	Args  map[string]any
}

func drainFrames(t *testing.T, s *apiServer, wardenID string) []drainedFrame {
	t.Helper()
	var out []drainedFrame
	for _, cmd := range s.hub.DrainWardenCommands(wardenID) {
		raw := cmd.Frame
		text := strings.TrimSpace(strings.TrimPrefix(string(raw), "data: "))
		var envelope struct {
			Topic string `json:"topic"`
			Data  struct {
				RPC  string         `json:"rpc"`
				Args map[string]any `json:"args"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(text), &envelope); err != nil {
			t.Fatalf("frame decode: %v (%q)", err, text)
		}
		out = append(out, drainedFrame{
			Topic: envelope.Topic, RPC: envelope.Data.RPC, Args: envelope.Data.Args,
		})
	}
	return out
}

func obsOf(id, desired string, online bool) memberObservation {
	return memberObservation{MemberID: id, Desired: desired, Online: online}
}

// obsRelocate is an online desired-online observation carrying the two machine
// facts that drive the relocation recycle.
func obsRelocate(id, running, target string) memberObservation {
	o := obsOf(id, DesiredStateOnline, true)
	o.RunningMachine = running
	o.TargetMachine = target
	return o
}

// connectOnlineMachine projects memberID online carrying a machine claim (the
// SSE machine_id) for the test's lifetime — the relocation running-machine fact.
func connectOnlineMachine(t *testing.T, s *apiServer, memberID, machineID string) *hubListener {
	t.Helper()
	l, err := s.hub.Connect(memberID, machineID)
	if err != nil {
		t.Fatalf("connect %s@%s: %v", memberID, machineID, err)
	}
	t.Cleanup(func() { s.hub.Disconnect(l) })
	return l
}

// putWarden seeds an ACTIVE, desired-online warden member (its member id IS its
// machine id) so wardenTargetOf/reachability resolve it.
func putWarden(t *testing.T, s *apiServer, id string) {
	t.Helper()
	putTestMember(t, s, Member{
		ID: id, Name: id, Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	})
}

// ── parseDesired ─────────────────────────────────────────────────────────────

func TestParseDesired(t *testing.T) {
	cases := map[string]string{
		"online":    DesiredStateOnline,
		"uninstall": DesiredStateUninstall,
		"offline":   DesiredStateOffline,
		"":          DesiredStateOffline,
		"junk":      DesiredStateOffline, // fail-safe: an unknown intent never spawns
	}
	for raw, want := range cases {
		if got := parseDesired(raw); got != want {
			t.Errorf("parseDesired(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestDefaultReconcileConfigUsesWakingTTLSecs pins the owner-approved value and
// the derived config fields independently.
func TestDefaultReconcileConfigUsesWakingTTLSecs(t *testing.T) {
	if WakingTTLSecs != 120.0 {
		t.Fatalf("WakingTTLSecs = %v, want 120.0 (owner ruling 2026-08-27)", WakingTTLSecs)
	}
	cfg := defaultReconcileConfig()
	if cfg.StartTimeout != WakingTTLSecs {
		t.Fatalf("StartTimeout = %v, want WakingTTLSecs (%v)", cfg.StartTimeout, WakingTTLSecs)
	}
	if cfg.ZombieConfirmGrace != 2*WakingTTLSecs {
		t.Fatalf("ZombieConfirmGrace = %v, want 2*WakingTTLSecs (%v)", cfg.ZombieConfirmGrace, 2*WakingTTLSecs)
	}
}

// ── reconcileDecide ──────────────────────────────────────────────────────────

func TestReconcileDecide(t *testing.T) {
	cfg := defaultReconcileConfig()

	t.Run("desired online and offline dispatches START", func(t *testing.T) {
		d := reconcileDecide(obsOf("m", DesiredStateOnline, false), newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdStart || d.State.Phase != reconcilePhaseStarting ||
			d.State.LastCommand != reconcileCmdStart || d.State.LastCommandAt != 1000 {
			t.Fatalf("decision: %+v", d)
		}
	})

	t.Run("converged online resets failure bookkeeping", func(t *testing.T) {
		st := reconcileState{
			Phase: reconcilePhaseBackoff, Attempts: 3, BackoffUntil: 2000,
			CircuitOpen: true, CircuitCooldownUntil: 3000,
			LastCommand: reconcileCmdStart, LastCommandAt: 900, StopDeadline: 42,
		}
		d := reconcileDecide(obsOf("m", DesiredStateOnline, true), st, cfg, 1000)
		want := reconcileState{Phase: reconcilePhaseOnline, LastCommand: reconcileCmdNone}
		if d.Command != reconcileCmdNone || d.State != want {
			t.Fatalf("decision: %+v", d)
		}
	})

	t.Run("START in flight within start_timeout waits", func(t *testing.T) {
		st := newReconcileState()
		st.LastCommand = reconcileCmdStart
		st.LastCommandAt = 1000
		d := reconcileDecide(obsOf("m", DesiredStateOnline, false), st, cfg, 1000+cfg.StartTimeout)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseStarting {
			t.Fatalf("decision: %+v", d)
		}
	})

	t.Run("START silent-timeout arms backoff but never trips the breaker", func(t *testing.T) {
		st := newReconcileState()
		now := 1000.0
		for i := 0; i < cfg.CircuitThreshold+2; i++ {
			st.LastCommand = reconcileCmdStart
			st.LastCommandAt = now
			now += cfg.StartTimeout + 1
			d := reconcileDecide(obsOf("m", DesiredStateOnline, false), st, cfg, now)
			if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseBackoff {
				t.Fatalf("attempt %d: %+v", i, d)
			}
			if d.State.CircuitOpen {
				t.Fatalf("a delivery-miss timeout must not trip the sticky breaker: %+v", d.State)
			}
			st = d.State
			now = st.BackoffUntil + 1
		}
		if st.Attempts != cfg.CircuitThreshold+2 {
			t.Fatalf("attempts: %d", st.Attempts)
		}
	})

	t.Run("backoff window suppresses START until it lapses", func(t *testing.T) {
		st := newReconcileState()
		st.BackoffUntil = 2000
		d := reconcileDecide(obsOf("m", DesiredStateOnline, false), st, cfg, 1500)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseBackoff {
			t.Fatalf("within backoff: %+v", d)
		}
		d = reconcileDecide(obsOf("m", DesiredStateOnline, false), st, cfg, 2000)
		if d.Command != reconcileCmdStart {
			t.Fatalf("after backoff: %+v", d)
		}
	})

	t.Run("circuit open suppresses START and half-opens after cooldown", func(t *testing.T) {
		st := newReconcileState()
		st.CircuitOpen = true
		st.CircuitCooldownUntil = 5000
		st.Attempts = cfg.CircuitThreshold
		d := reconcileDecide(obsOf("m", DesiredStateOnline, false), st, cfg, 4000)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseCircuitOpen {
			t.Fatalf("open breaker: %+v", d)
		}
		d = reconcileDecide(obsOf("m", DesiredStateOnline, false), st, cfg, 5000)
		if d.Command != reconcileCmdStart || d.State.CircuitOpen || d.State.Attempts != 0 {
			t.Fatalf("half-open must grant a fresh retry: %+v", d)
		}
	})

	// 下線 runs NO clock (owner 2026-08-16, card rc-27d1710174dd 「不要兜底：只有
	// 你按強制下線才收它」). The button shows the agent the offboard sequence and
	// asks it to stop itself; the escalation is the owner's force-stop, not a
	// timer's. Collection still happens the instant the agent reports stopped.
	t.Run("desired offline and online waits indefinitely, arming no clock", func(t *testing.T) {
		d := reconcileDecide(obsOf("m", DesiredStateOffline, true), newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseStopping ||
			d.State.StopDeadline != 0 {
			t.Fatalf("decision: %+v", d)
		}
		// A day later — far past every window this server has ever had — still
		// nothing. The owner is the one who decides time is up.
		d2 := reconcileDecide(obsOf("m", DesiredStateOffline, true), d.State, cfg, 1000+86400)
		if d2.Command != reconcileCmdNone {
			t.Fatalf("a day of silence must still dispatch nothing: %+v", d2)
		}
	})

	// …and the timed wind-down is still REACHABLE, as the one value that
	// restores the old behaviour wholesale (a compile-time constant today, not
	// something the owner can set).
	t.Run("soft window of zero restores the timed wind-down", func(t *testing.T) {
		timed := cfg
		timed.SoftOffboardGrace = 0
		d := reconcileDecide(obsOf("m", DesiredStateOffline, true), newReconcileState(), timed, 1000)
		if d.Command != reconcileCmdNone || d.State.StopDeadline != 1000+timed.StopGrace {
			t.Fatalf("decision: %+v", d)
		}
		d2 := reconcileDecide(obsOf("m", DesiredStateOffline, true), d.State, timed, 1000+timed.StopGrace)
		if d2.Command != reconcileCmdStop {
			t.Fatalf("grace elapsed must dispatch the robust stop: %+v", d2)
		}
	})

	t.Run("grace elapsed dispatches the single robust STOP with stop_retry dedupe", func(t *testing.T) {
		cfg := cfg
		cfg.SoftOffboardGrace = 0 // the timed wind-down — see the sub-test above
		st := newReconcileState()
		st.StopDeadline = 1000
		d := reconcileDecide(obsOf("m", DesiredStateOffline, true), st, cfg, 1000)
		if d.Command != reconcileCmdStop || d.State.LastCommand != reconcileCmdStop {
			t.Fatalf("first stop: %+v", d)
		}
		d2 := reconcileDecide(obsOf("m", DesiredStateOffline, true), d.State, cfg, 1000+cfg.StopRetry-1)
		if d2.Command != reconcileCmdNone {
			t.Fatalf("within stop_retry must dedupe: %+v", d2)
		}
		d3 := reconcileDecide(obsOf("m", DesiredStateOffline, true), d.State, cfg, 1000+cfg.StopRetry)
		if d3.Command != reconcileCmdStop || d3.State.LastCommandAt != 1000+cfg.StopRetry {
			t.Fatalf("past stop_retry must re-dispatch: %+v", d3)
		}
	})

	t.Run("desired offline converged resets stop bookkeeping but keeps the breaker", func(t *testing.T) {
		st := reconcileState{
			Phase: reconcilePhaseStopping, Attempts: 2, BackoffUntil: 99,
			CircuitOpen: true, CircuitCooldownUntil: 9e9,
			LastCommand: reconcileCmdStop, LastCommandAt: 900, StopDeadline: 950,
		}
		d := reconcileDecide(obsOf("m", DesiredStateOffline, false), st, cfg, 1000)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseOffline ||
			d.State.StopDeadline != 0 || d.State.LastCommand != reconcileCmdNone ||
			d.State.Attempts != 0 {
			t.Fatalf("decision: %+v", d)
		}
		if !d.State.CircuitOpen {
			t.Fatal("offline-converged must not clear the sticky breaker (machine.py parity)")
		}
	})

	t.Run("recycle waits for the dump then robust-stops", func(t *testing.T) {
		obs := obsOf("m", DesiredStateOnline, true)
		obs.RefocusSince = 1000
		d := reconcileDecide(obs, newReconcileState(), cfg, 1010)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseStopping {
			t.Fatalf("awaiting dump: %+v", d)
		}
		obs.AgentStopped = true
		d = reconcileDecide(obs, d.State, cfg, 1020)
		if d.Command != reconcileCmdStop {
			t.Fatalf("dump done must robust-stop: %+v", d)
		}
		// De-dupe inside stop_retry, re-dispatch past it.
		d2 := reconcileDecide(obs, d.State, cfg, 1020+cfg.StopRetry-1)
		if d2.Command != reconcileCmdNone {
			t.Fatalf("within stop_retry: %+v", d2)
		}
		d3 := reconcileDecide(obs, d.State, cfg, 1020+cfg.StopRetry)
		if d3.Command != reconcileCmdStop {
			t.Fatalf("past stop_retry: %+v", d3)
		}
	})

	// The stuck-dump force-stop belongs to the ONE cause that runs a clock —
	// 加速停止, the second context threshold (T-ed79). The op is named here
	// because it is what the grace is read from; an unnamed op is a 停止 and is
	// collected by the agent's own stopped report, asserted above.
	t.Run("recycle grace elapsed force-stops a stuck dump", func(t *testing.T) {
		obs := obsOf("m", DesiredStateOnline, true)
		obs.RefocusSince = 1000
		obs.RefocusOp = refocusOpContextHigh
		d := reconcileDecide(obs, newReconcileState(), cfg, 1000+cfg.RecycleGrace)
		if d.Command != reconcileCmdStop {
			t.Fatalf("grace elapsed must force-stop: %+v", d)
		}
	})

	// ── relocation: owner re-pinned a LIVE member's desired_machine (kyle-62b2) ──

	// T-14 #4: the mismatch is now answered in TWO ways, and which one depends on
	// obs.HandoverArmable — "would a wind-down epoch actually be stamped on this
	// row?". The armable case is the normal one and it WAITS; the non-armable case
	// is the fallback and it is what the arm used to do unconditionally.
	t.Run("online with a machine mismatch opens a wind-down instead of killing", func(t *testing.T) {
		obs := obsRelocate("m", "mach-old", "mach-new")
		obs.HandoverArmable = true
		d := reconcileDecide(obs, newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdNone {
			t.Fatalf("a member that CAN hand over must not be killed on sight: %+v", d)
		}
		if d.ArmHandoverOp != memberOpRelocate {
			t.Fatalf("the decision must ask for a relocate wind-down, got ArmHandoverOp=%q", d.ArmHandoverOp)
		}
		// st.LastCommand is deliberately NOT advanced: the collection belongs to
		// the refocus arm, whose first-dispatch bookkeeping must start clean.
		if d.State.LastCommand == reconcileCmdStop {
			t.Fatalf("the wind-down arm must not claim a STOP it did not dispatch: %+v", d.State)
		}
	})

	// The name is load-bearing: this is the BACKSTOP-OF-THE-BACKSTOP, not the
	// ordinary relocation. It used to be the ONLY behaviour of this arm and the
	// subtest was named "…robust-stops toward the RUNNING machine" with no
	// qualifier, which stopped being true for the ordinary case in T-14 #4. The
	// assertions below are byte-for-byte the ones that stood then — they are still
	// right, about a NARROWER input: a member for which no wind-down can be opened
	// (a warden row, or one already on the 強制停止 rung) has no hand-off to wait
	// for, and waiting for one that cannot happen is not gentler than killing, it
	// is just never converging.
	t.Run("online with a machine mismatch and NOTHING to hand over robust-stops toward the RUNNING machine", func(t *testing.T) {
		d := reconcileDecide(obsRelocate("m", "mach-old", "mach-new"), newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdStop || d.State.Phase != reconcilePhaseStopping ||
			d.State.LastCommand != reconcileCmdStop || d.State.LastCommandAt != 1000 {
			t.Fatalf("mismatch must robust-stop: %+v", d)
		}
		// The STOP must route to the OLD (running) machine's warden — that is where
		// the session to kill lives; routing to the new machine would no-op forever.
		if d.DispatchWarden != "mach-old" {
			t.Fatalf("relocation STOP must target the running machine, got %q", d.DispatchWarden)
		}
		// stop_retry dedupe, exactly like refocus recycle / decideDown.
		d2 := reconcileDecide(obsRelocate("m", "mach-old", "mach-new"), d.State, cfg, 1000+cfg.StopRetry-1)
		if d2.Command != reconcileCmdNone {
			t.Fatalf("within stop_retry must dedupe: %+v", d2)
		}
		d3 := reconcileDecide(obsRelocate("m", "mach-old", "mach-new"), d.State, cfg, 1000+cfg.StopRetry)
		if d3.Command != reconcileCmdStop || d3.DispatchWarden != "mach-old" {
			t.Fatalf("past stop_retry must re-dispatch to running machine: %+v", d3)
		}
	})

	t.Run("online with the running machine already the target just converges", func(t *testing.T) {
		d := reconcileDecide(obsRelocate("m", "mach-x", "mach-x"), newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseOnline ||
			d.DispatchWarden != "" {
			t.Fatalf("running==target must converge, never relocate: %+v", d)
		}
	})

	t.Run("online with an UNKNOWN running machine never relocates (boot not yet stamped)", func(t *testing.T) {
		// RunningMachine "" is a claim-less / still-booting member — relocating it
		// would flap a booting member into a STOP→START loop. The critical guard.
		d := reconcileDecide(obsRelocate("m", "", "mach-new"), newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseOnline {
			t.Fatalf("empty running machine must NEVER relocate: %+v", d)
		}
	})

	t.Run("online with an empty target machine never relocates", func(t *testing.T) {
		d := reconcileDecide(obsRelocate("m", "mach-old", ""), newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseOnline {
			t.Fatalf("empty target machine must never relocate: %+v", d)
		}
	})

	t.Run("refocus recycle takes precedence over a machine mismatch", func(t *testing.T) {
		// A refocus already owns the member — the relocation recycle must not stack
		// on top of it. What pins the precedence is the REASON: only the recycle arm
		// says "recycle:".
		//
		// ⚠️ It used to also assert DispatchWarden == "" ("routes normally"). T-b6d9
		// made the recycle arm address the RUNNING machine, because a 改機器 is now
		// collected BY this arm while the session is still on the origin and the pin
		// already names the destination — routing by the pin there would ask the
		// destination's warden to kill a session it does not hold, and the old one
		// would live forever. For a member sitting on its own pin the two are the
		// same machine, so nothing observable changed for 重新聚焦.
		obs := obsRelocate("m", "mach-old", "mach-new")
		obs.RefocusSince = 1000
		obs.AgentStopped = true
		d := reconcileDecide(obs, newReconcileState(), cfg, 1010)
		if d.Command != reconcileCmdStop || !strings.HasPrefix(d.Reason, "recycle:") {
			t.Fatalf("refocus must own the recycle, not the relocation path: %+v", d)
		}
		if d.DispatchWarden != "mach-old" {
			t.Fatalf("a recycle STOP must be addressed to the RUNNING machine: %+v", d)
		}
	})

	t.Run("offline with a machine mismatch just STARTs (relocation is an online-only recycle)", func(t *testing.T) {
		obs := obsRelocate("m", "mach-old", "mach-new")
		obs.Online = false
		d := reconcileDecide(obs, newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdStart || d.DispatchWarden != "" {
			t.Fatalf("an offline member just STARTs onto its target: %+v", d)
		}
	})

	t.Run("uninstall dispatches immediately when the warden is online", func(t *testing.T) {
		d := reconcileDecide(obsOf("w", DesiredStateUninstall, true), newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdUninstall || d.State.LastCommand != reconcileCmdUninstall {
			t.Fatalf("decision: %+v", d)
		}
		d2 := reconcileDecide(obsOf("w", DesiredStateUninstall, true), d.State, cfg, 1000+cfg.StopRetry-1)
		if d2.Command != reconcileCmdNone {
			t.Fatalf("within stop_retry must dedupe: %+v", d2)
		}
		d3 := reconcileDecide(obsOf("w", DesiredStateUninstall, true), d.State, cfg, 1000+cfg.StopRetry)
		if d3.Command != reconcileCmdUninstall {
			t.Fatalf("past stop_retry must re-dispatch: %+v", d3)
		}
	})

	t.Run("uninstall converged when the warden is offline", func(t *testing.T) {
		st := newReconcileState()
		st.LastCommand = reconcileCmdUninstall
		st.LastCommandAt = 900
		d := reconcileDecide(obsOf("w", DesiredStateUninstall, false), st, cfg, 1000)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseOffline ||
			d.State.LastCommand != reconcileCmdNone {
			t.Fatalf("decision: %+v", d)
		}
	})

	t.Run("START that clobbered a live deaf session robust-stops the zombie", func(t *testing.T) {
		st := newReconcileState()
		st.LastCommand = reconcileCmdStart
		st.LastCommandAt = 1000
		// T-9adc: the takeover additionally requires a SUSTAINED offline record
		// (second confirmation) — this member has been offline past the grace.
		st.OfflineSince = 1000 - cfg.ZombieConfirmGrace
		obs := obsOf("m", DesiredStateOnline, false)
		obs.LastOpKind = reconcileCmdStart
		obs.LastOpReason = "session_already_exists: tmux session \"member-m\" is already live (clobber-guard refused to stomp it)"
		// A clobber receipt is positive proof the slot is squatted, so the zombie
		// is reaped even INSIDE the start window where a plain in-flight START
		// would still be waiting for presence.
		d := reconcileDecide(obs, st, cfg, 1000+cfg.StartTimeout-1)
		if d.Command != reconcileCmdStop || d.State.Phase != reconcilePhaseStopping ||
			d.State.LastCommand != reconcileCmdStop {
			t.Fatalf("clobbered START must robust-stop the zombie: %+v", d)
		}
	})

	t.Run("zombie takeover WITHHELD inside the reconnect-confirm grace (T-9adc)", func(t *testing.T) {
		// 斷線 → 寬限內:the clobber receipt alone must NOT fire the STOP — a
		// session mid-reconnect (the 2026-07-20 SSE-blip incident) is
		// indistinguishable from a zombie at this instant.
		st := newReconcileState()
		st.LastCommand = reconcileCmdStart
		st.LastCommandAt = 1000
		obs := obsOf("m", DesiredStateOnline, false)
		obs.LastOpKind = reconcileCmdStart
		obs.LastOpReason = "session_already_exists: tmux session \"member-m\" is already live (clobber-guard refused to stomp it)"
		// First offline observation: OfflineSince arms NOW → 0 elapsed < grace.
		d := reconcileDecide(obs, st, cfg, 1010)
		if d.Command != reconcileCmdNone {
			t.Fatalf("takeover STOP must be withheld inside the grace: %+v", d)
		}
		if d.State.OfflineSince != 1010 {
			t.Fatalf("first offline tick must arm OfflineSince: %+v", d.State)
		}
		if d.State.LastCommand != reconcileCmdStart {
			t.Fatalf("holding must keep the START context so the arm re-evaluates: %+v", d.State)
		}
		// One second before the grace lapses: still withheld (boundary).
		d2 := reconcileDecide(obs, d.State, cfg, 1010+cfg.ZombieConfirmGrace-1)
		if d2.Command != reconcileCmdNone {
			t.Fatalf("still inside the grace — must keep withholding: %+v", d2)
		}
		// Grace lapsed with no reconnect: the STOP fires (bounded — a true
		// zombie is still reaped; the window can never degrade into never-kill).
		d3 := reconcileDecide(obs, d2.State, cfg, 1010+cfg.ZombieConfirmGrace)
		if d3.Command != reconcileCmdStop || d3.State.LastCommand != reconcileCmdStop {
			t.Fatalf("grace lapsed — takeover STOP must fire: %+v", d3)
		}
	})

	t.Run("reconnect inside the grace cancels the takeover (T-9adc)", func(t *testing.T) {
		// 斷線 → 寬限內重連 → 不殺:an online observation is the liveness proof —
		// it clears OfflineSince and converges; no STOP is ever dispatched.
		st := newReconcileState()
		st.LastCommand = reconcileCmdStart
		st.LastCommandAt = 1000
		obs := obsOf("m", DesiredStateOnline, false)
		obs.LastOpKind = reconcileCmdStart
		obs.LastOpReason = "session_already_exists: tmux session \"member-m\" is already live (clobber-guard refused to stomp it)"
		hold := reconcileDecide(obs, st, cfg, 1010)
		if hold.Command != reconcileCmdNone {
			t.Fatalf("expected the withheld takeover: %+v", hold)
		}
		// The session reconnects on its own (the incident's actual outcome).
		back := obsOf("m", DesiredStateOnline, true)
		d := reconcileDecide(back, hold.State, cfg, 1040)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseOnline {
			t.Fatalf("reconnected member must converge, never be stopped: %+v", d)
		}
		if d.State.OfflineSince != 0 {
			t.Fatalf("online observation must clear the offline anchor: %+v", d.State)
		}
		// Even if it drops offline again later, the window re-arms from ZERO —
		// no stale clock can fast-track a kill after a proven reconnect.
		st2 := d.State
		st2.LastCommand = reconcileCmdStart
		st2.LastCommandAt = 2000
		again := reconcileDecide(obs, st2, cfg, 2010)
		if again.Command != reconcileCmdNone || again.State.OfflineSince != 2010 {
			t.Fatalf("post-reconnect offline must re-arm the grace from zero: %+v", again)
		}
	})

	t.Run("reaped zombie respawns clean on the next tick", func(t *testing.T) {
		st := newReconcileState()
		st.LastCommand = reconcileCmdStart
		st.LastCommandAt = 1000
		st.OfflineSince = 1000 - cfg.ZombieConfirmGrace // T-9adc: sustained offline
		obs := obsOf("m", DesiredStateOnline, false)
		obs.LastOpKind = reconcileCmdStart
		obs.LastOpReason = "session_already_exists: tmux session \"member-m\" is already live (clobber-guard refused to stomp it)"
		stop := reconcileDecide(obs, st, cfg, 1010)
		if stop.Command != reconcileCmdStop {
			t.Fatalf("expected robust stop: %+v", stop)
		}
		// Warden reaped the session (kill ladder → stopped). Next tick: still not
		// online, but st.LastCommand is now stop, so the zombie arm no longer
		// fires and the plain spawn arm lands a clean START.
		next := reconcileDecide(obsOf("m", DesiredStateOnline, false), stop.State, cfg, 1020)
		if next.Command != reconcileCmdStart || next.State.LastCommand != reconcileCmdStart {
			t.Fatalf("reaped slot must respawn clean: %+v", next)
		}
	})

	t.Run("in-flight START with a non-clobber receipt keeps waiting", func(t *testing.T) {
		st := newReconcileState()
		st.LastCommand = reconcileCmdStart
		st.LastCommandAt = 1000
		obs := obsOf("m", DesiredStateOnline, false)
		obs.LastOpKind = reconcileCmdStart
		obs.LastOpReason = "claude_bin_unresolved: set OC_CLAUDE_BIN"
		d := reconcileDecide(obs, st, cfg, 1000+cfg.StartTimeout-1)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseStarting {
			t.Fatalf("a non-clobber start failure must not takeover: %+v", d)
		}
	})
}

// ── registerStartFailure ─────────────────────────────────────────────────────

func TestRegisterStartFailure(t *testing.T) {
	cfg := defaultReconcileConfig()

	t.Run("arms exponential backoff up to the cap", func(t *testing.T) {
		st := newReconcileState()
		st = registerStartFailure(st, cfg, 1000, false)
		if st.Attempts != 1 || st.BackoffUntil != 1000+cfg.BackoffBase {
			t.Fatalf("first failure: %+v", st)
		}
		st = registerStartFailure(st, cfg, 1000, false)
		if st.BackoffUntil != 1000+cfg.BackoffBase*2 {
			t.Fatalf("second failure: %+v", st)
		}
		st.Attempts = 200 // huge attempt count must saturate at the cap, not overflow
		st = registerStartFailure(st, cfg, 1000, false)
		if st.BackoffUntil != 1000+cfg.BackoffCap {
			t.Fatalf("cap saturation: %+v", st)
		}
	})

	t.Run("trips the sticky breaker only when circuit-eligible", func(t *testing.T) {
		st := newReconcileState()
		st.Attempts = cfg.CircuitThreshold - 1
		ineligible := registerStartFailure(st, cfg, 1000, false)
		if ineligible.CircuitOpen {
			t.Fatal("ineligible failure must not trip the breaker")
		}
		eligible := registerStartFailure(st, cfg, 1000, true)
		if !eligible.CircuitOpen || eligible.CircuitCooldownUntil != 1000+cfg.CircuitCooldown {
			t.Fatalf("eligible failure at threshold must trip: %+v", eligible)
		}
	})
}

// ── bootStormTripped ─────────────────────────────────────────────────────────

func TestBootStormTripped(t *testing.T) {
	secs := func(v float64) *float64 { return &v }
	if bootStormTripped(secs(30), 120) != true {
		t.Fatal("a fresh over-line boot must trip the guard")
	}
	if bootStormTripped(secs(300), 120) || bootStormTripped(nil, 120) ||
		bootStormTripped(secs(-1), 120) || bootStormTripped(secs(30), 0) {
		t.Fatal("mature boot / missing / negative data / disabled guard must never trip")
	}
}

// ── wardenTargetOf ───────────────────────────────────────────────────────────

func TestWardenTargetOf(t *testing.T) {
	s := newReconcileTestServer(t)
	putTestMember(t, s, testAgent("m-a"))

	if got := s.wardenTargetOf(ServerSelfHost); got != ServerSelfHost {
		t.Fatalf("a warden addresses itself: %q", got)
	}
	if got := s.wardenTargetOf("m-a"); got != ServerSelfHost {
		t.Fatalf("an agent routes to its desired machine's warden: %q", got)
	}
	// A pin naming no active warden resolves to NOTHING, not to the raw string.
	// Handing the unresolved pin on as if it were a host is how the literal
	// "auto" became a destination: every dispatch addressed a machine that could
	// not exist, forever, and the stall was indistinguishable from an offline one.
	orphan := testAgent("m-orphan")
	orphan.DesiredMachineID = "m-no-such-warden"
	putTestMember(t, s, orphan)
	if got := s.wardenTargetOf("m-orphan"); got != "" {
		t.Fatalf("a pin naming no active warden must resolve to no target: %q", got)
	}
	unplaced := testAgent("m-unplaced")
	unplaced.DesiredMachineID = ""
	putTestMember(t, s, unplaced)
	if got := s.wardenTargetOf("m-unplaced"); got != "" {
		t.Fatalf("an unplaced member must resolve to no target: %q", got)
	}
	if got := s.wardenTargetOf("m-missing"); got != "" {
		t.Fatalf("a missing member resolves to no target: %q", got)
	}
}

// ── runReconcileTick ─────────────────────────────────────────────────────────

func TestRunReconcileTick(t *testing.T) {
	t.Run("dispatches one START and stays idempotent across ticks", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putTestMember(t, s, testAgent("m-a"))
		connectOnline(t, s, ServerSelfHost)

		s.runReconcileTick(1000)
		frames := drainFrames(t, s, ServerSelfHost)
		if len(frames) != 1 || frames[0].RPC != "start" || frames[0].Topic != "warden-command" {
			t.Fatalf("frames: %+v", frames)
		}
		args := frames[0].Args
		if args["member_id"] != "m-a" || args["member_token"] == "" ||
			args["persona_context"] == "" || args["role"] != "assistant" {
			t.Fatalf("start args: %+v", args)
		}
		// Idempotence: the START is in flight — repeated scans re-dispatch nothing.
		s.runReconcileTick(1001)
		s.runReconcileTick(1002)
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("in-flight START must not re-dispatch: %+v", frames)
		}
	})

	t.Run("fails closed when the warden is unreachable and retries when it connects", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putTestMember(t, s, testAgent("m-a"))

		s.runReconcileTick(1000)
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("no live warden downstream must dispatch nothing: %+v", frames)
		}
		connectOnline(t, s, ServerSelfHost)
		s.runReconcileTick(1030)
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 1 || frames[0].RPC != "start" {
			t.Fatalf("warden online must dispatch the retried START: %+v", frames)
		}
	})

	t.Run("fails closed on an unknown role (no persona to boot with)", func(t *testing.T) {
		s := newReconcileTestServer(t)
		ghost := testAgent("m-ghost")
		ghost.RoleKey = "no-such-role"
		putTestMember(t, s, ghost)
		connectOnline(t, s, ServerSelfHost)

		s.runReconcileTick(1000)
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("unknown role must never START: %+v", frames)
		}
	})

	t.Run("excludes wardens except a desired-uninstall one", func(t *testing.T) {
		s := newReconcileTestServer(t)
		connectOnline(t, s, ServerSelfHost)
		warden, err := s.dal.GetMember(ServerSelfHost)
		if err != nil || warden == nil {
			t.Fatalf("seed warden: %v", err)
		}
		warden.DesiredState = DesiredStateOnline
		putTestMember(t, s, *warden)

		s.runReconcileTick(1000)
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("a desired-online warden is never a spawn candidate: %+v", frames)
		}
		warden.DesiredState = DesiredStateUninstall
		putTestMember(t, s, *warden)
		s.runReconcileTick(1030)
		frames := drainFrames(t, s, ServerSelfHost)
		if len(frames) != 1 || frames[0].RPC != "uninstall" ||
			frames[0].Args["member_id"] != ServerSelfHost {
			t.Fatalf("desired-uninstall warden must get the uninstall RPC: %+v", frames)
		}
		// While the warden is still ONLINE the intent is live, never consumed.
		m, _ := s.dal.GetMember(ServerSelfHost)
		if m.DesiredState != DesiredStateUninstall {
			t.Fatalf("online warden must keep the uninstall intent: %+v", m)
		}
	})

	t.Run("consumes a residual uninstall intent once the warden is offline", func(t *testing.T) {
		s := newReconcileTestServer(t)
		box := Member{
			ID: "m-box", Name: "box", Kind: KindWarden, Effort: "medium",
			DesiredState: DesiredStateUninstall, RosterStatus: RosterStatusActive,
		}
		putTestMember(t, s, box)

		s.runReconcileTick(1000)
		m, err := s.dal.GetMember("m-box")
		if err != nil || m == nil || m.DesiredState != DesiredStateOffline {
			t.Fatalf("offline warden's uninstall intent must be consumed: %+v (%v)", m, err)
		}
		if m.RosterStatus != RosterStatusActive {
			t.Fatalf("record must be kept (re-installable): %+v", m)
		}
		// The consumed intent is one-shot: a later reconnect (re-install) must
		// NOT be answered with another UNINSTALL.
		connectOnline(t, s, "m-box")
		s.runReconcileTick(1030)
		if frames := drainFrames(t, s, "m-box"); len(frames) != 0 {
			t.Fatalf("re-connected warden must not receive a stale uninstall: %+v", frames)
		}
	})

	t.Run("dispatches the robust STOP only after the grace elapses", func(t *testing.T) {
		s := newReconcileTestServer(t)
		stopper := testAgent("m-stop")
		stopper.DesiredState = DesiredStateOffline
		putTestMember(t, s, stopper)
		connectOnline(t, s, ServerSelfHost)
		connectOnline(t, s, "m-stop") // still online while desired offline

		// The default (owner's ruling) runs no clock at all: a day of ticks
		// dispatches nothing, because the escalation is his force-stop.
		s.runReconcileTick(1000)
		s.runReconcileTick(1000 + 86400)
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("下線 must dispatch nothing on a timer: %+v", frames)
		}
		// With the soft window off — the value that restores the timed
		// wind-down — the same tick collects it.
		s.reconcileCfg.SoftOffboardGrace = 0
		s.runReconcileTick(2000)
		s.runReconcileTick(2000 + s.reconcileCfg.StopGrace)
		frames := drainFrames(t, s, ServerSelfHost)
		if len(frames) != 1 || frames[0].RPC != "stop" || frames[0].Args["member_id"] != "m-stop" {
			t.Fatalf("grace elapsed must dispatch the robust stop: %+v", frames)
		}
	})

	t.Run("auto-stamps refocus_since from a handover-band gauge", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putTestMember(t, s, testAgent("m-hot"))
		connectOnline(t, s, ServerSelfHost)
		connectOnline(t, s, "m-hot")
		now := 10000.0
		s.gauge.Set("m-hot", map[string]any{
			"context_pct":    float64(s.ctxhigh.HandoverPct),
			"context_pct_ts": now - 10,
			"boot_ts":        now - 500, // mature boot → no boot-storm suppression
		})

		s.runReconcileTick(now)
		m, err := s.dal.GetMember("m-hot")
		if err != nil || m == nil || m.RefocusSince != now {
			t.Fatalf("handover band must auto-stamp refocus_since: %+v (%v)", m, err)
		}
		// Second tick: already recycling — the marker must not re-stamp.
		s.runReconcileTick(now + 30)
		m, _ = s.dal.GetMember("m-hot")
		if m.RefocusSince != now {
			t.Fatalf("an already-marked member must not re-stamp: %+v", m)
		}
	})

	t.Run("Codex auto-stamps only after three actual compactions", func(t *testing.T) {
		s := newReconcileTestServer(t)
		m := testAgent("m-codex-compact")
		m.Runtime = RuntimeCodex
		putTestMember(t, s, m)
		connectOnline(t, s, ServerSelfHost)
		connectOnline(t, s, m.ID)
		now := 10000.0
		// 99% with two compactions is the notice ROUND, not the ceiling: a codex
		// session hands over on compaction count, so the percentage must not put
		// it on the accelerated stop. T-ed79: the notice round DOES open the
		// plain 停止 (it is the first threshold on the codex axis).
		s.gauge.Set(m.ID, map[string]any{"context_pct": 99.0, "context_pct_ts": now - 10, "boot_ts": now - 500, "compaction_count": 2})
		s.runReconcileTick(now)
		got, _ := s.dal.GetMember(m.ID)
		if got.RefocusOp != refocusOpContextNotice {
			t.Fatalf("Codex must ignore percent-only handover (want the notice "+
				"round's 停止, %q), got %+v", refocusOpContextNotice, got)
		}
		s.gauge.Set(m.ID, map[string]any{"context_pct": 20.0, "context_pct_ts": now - 5, "boot_ts": now - 500, "compaction_count": 3})
		s.runReconcileTick(now + 1)
		got, _ = s.dal.GetMember(m.ID)
		if got.RefocusSince != now+1 || got.RefocusOp != refocusOpContextHigh {
			t.Fatalf("the third Codex compaction must promote to %q and RE-STAMP "+
				"refocus_since (a deadline measured from the notice round is "+
				"already in the past), got %+v", refocusOpContextHigh, got)
		}
	})

	t.Run("relocation stops the OLD machine's warden, then STARTs onto the NEW one", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putWarden(t, s, "mach-old")
		putWarden(t, s, "mach-new")
		// A live member running on mach-old, freshly re-pinned to mach-new.
		mover := testAgent("m-move")
		mover.DesiredMachineID = "mach-new"
		putTestMember(t, s, mover)
		connectOnline(t, s, "mach-old")                               // old warden reachable (holds the session)
		connectOnline(t, s, "mach-new")                               // new warden reachable (START target)
		moverConn := connectOnlineMachine(t, s, "m-move", "mach-old") // running on the OLD machine

		// T-14 #4: the first tick opens a wind-down and dispatches NOTHING. It is
		// asserted rather than skipped past, because "no frame yet" is the whole
		// behaviour change and a tick that quietly dispatched here would be the
		// regression. What this subtest exists for — the ROUTING of the STOP and
		// of the respawn START — is unchanged and asserted below, one hand-off
		// later.
		s.runReconcileTick(1000)
		if f := drainFrames(t, s, "mach-old"); len(f) != 0 {
			t.Fatalf("the first tick must open a wind-down, not kill: %+v", f)
		}
		if f := drainFrames(t, s, "mach-new"); len(f) != 0 {
			t.Fatalf("the first tick must dispatch nothing at all: %+v", f)
		}
		armed, _ := s.dal.GetMember("m-move")
		if armed.RefocusSince <= 0 || armed.RefocusOp != memberOpRelocate {
			t.Fatalf("the first tick must stamp a relocate wind-down: %+v", armed)
		}
		// The agent files its stopped report (POST /api/self/stopped) — the 收口.
		armed.StoppedSince = 1001
		putTestMember(t, s, *armed)

		s.runReconcileTick(1002)
		// The STOP must land on the OLD machine's warden FIFO — never the new one.
		oldFrames := drainFrames(t, s, "mach-old")
		if len(oldFrames) != 1 || oldFrames[0].RPC != "stop" || oldFrames[0].Args["member_id"] != "m-move" {
			t.Fatalf("relocation STOP must land on the running (old) machine's warden: %+v", oldFrames)
		}
		if newFrames := drainFrames(t, s, "mach-new"); len(newFrames) != 0 {
			t.Fatalf("the target (new) machine's warden must NOT get the STOP: %+v", newFrames)
		}
		// The kill lands: the member drops offline. The next tick STARTs it onto
		// the NEW machine (a fresh boot token minted with desired_machine=mach-new,
		// routed to the new machine's warden).
		s.hub.Disconnect(moverConn)
		s.runReconcileTick(1002 + s.reconcileCfg.StopRetry)
		newFrames := drainFrames(t, s, "mach-new")
		if len(newFrames) != 1 || newFrames[0].RPC != "start" || newFrames[0].Args["member_id"] != "m-move" {
			t.Fatalf("after the kill the START must route to the new machine: %+v", newFrames)
		}
		if oldFrames := drainFrames(t, s, "mach-old"); len(oldFrames) != 0 {
			t.Fatalf("the old machine's warden must NOT get the respawn START: %+v", oldFrames)
		}
	})

	t.Run("a claim-less (still-booting) online member is never relocated", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putWarden(t, s, "mach-new")
		booting := testAgent("m-boot")
		booting.DesiredMachineID = "mach-new"
		putTestMember(t, s, booting)
		connectOnline(t, s, "mach-new")
		connectOnline(t, s, "m-boot") // online but carries NO machine claim (claim-less boot)

		s.runReconcileTick(1000)
		if frames := drainFrames(t, s, "mach-new"); len(frames) != 0 {
			t.Fatalf("a claim-less online member must never be recycled: %+v", frames)
		}
	})
}

// ── stampMemberPlacementBlocked ──────────────────────────────────────────────

// A member decided START with no resolvable machine dispatches NOTHING, and the
// stall is named on the row the cockpit reads instead of retrying in silence
// every 30s. Written only when the cause CHANGES (the cadence re-decides the
// same START forever).
func TestStampMemberPlacementBlocked(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-real")
	connectOnline(t, s, "mach-real")
	connectOnline(t, s, ServerSelfHost)

	unplaced := testAgent("m-unplaced")
	unplaced.DesiredMachineID = ""
	putTestMember(t, s, unplaced)

	now := 5000.0
	s.runReconcileTick(now)
	got, err := s.dal.GetMember("m-unplaced")
	if err != nil || got == nil {
		t.Fatalf("re-read member: %v", err)
	}
	if !strings.HasPrefix(got.LastOpReason, placementReasonNoMachine+":") {
		t.Fatalf("an unplaced member must be stamped %s: %+v", placementReasonNoMachine, got)
	}
	if got.LastOp != reconcileCmdStart || got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("the stamp must be a FAILED start op: %+v", got)
	}
	if got.LastOpAt != now {
		t.Fatalf("last_op_at = %v, want %v", got.LastOpAt, now)
	}
	for _, host := range []string{ServerSelfHost, "mach-real"} {
		if frames := drainFrames(t, s, host); len(frames) != 0 {
			t.Fatalf("%s must receive nothing for an unplaced member: %+v", host, frames)
		}
	}

	// Anti-churn: the same cause on the next tick writes nothing.
	s.runReconcileTick(now + 30)
	if again, _ := s.dal.GetMember("m-unplaced"); again == nil || again.LastOpAt != now {
		t.Fatalf("an unchanged cause must NOT re-stamp: %+v", again)
	}

	// A pin naming no active machine is the OTHER variant, and it names the pin.
	ghosted := testAgent("m-ghosted")
	ghosted.DesiredMachineID = "mach-ghost"
	putTestMember(t, s, ghosted)
	s.runReconcileTick(now + 60)
	gh, _ := s.dal.GetMember("m-ghosted")
	if gh == nil || !strings.HasPrefix(gh.LastOpReason, placementReasonUnavailable+":") {
		t.Fatalf("a pin naming no active machine must be stamped %s: %+v",
			placementReasonUnavailable, gh)
	}
	if !strings.Contains(gh.LastOpReason, "mach-ghost") {
		t.Fatalf("the reason must name the machine the owner chose: %q", gh.LastOpReason)
	}

	// SENTINEL: a member pinned to a real ONLINE warden still STARTs normally, so
	// the refusals above are the missing placement, not a broken fixture.
	placed := testAgent("m-placed")
	placed.DesiredMachineID = "mach-real"
	putTestMember(t, s, placed)
	s.runReconcileTick(now + 90)
	frames := drainFrames(t, s, "mach-real")
	if len(frames) != 1 || frames[0].RPC != reconcileCmdStart ||
		frames[0].Args["member_id"] != "m-placed" {
		t.Fatalf("a member pinned to a real online warden must START: %+v", frames)
	}
	if p, _ := s.dal.GetMember("m-placed"); p == nil || p.LastOpReason != "" {
		t.Fatalf("a dispatched member must not be stamped blocked: %+v", p)
	}
}

// The stamp is a whole-row write on the snapshot the tick loaded, and the HTTP
// faces (relocate / deactivate) write member rows without holding reconcileMu —
// so it re-reads first: a relocate that landed mid-tick keeps its NEW pin, and a
// member removed mid-tick is not written back at all.
func TestStampMemberPlacementBlockedReReadsTheRow(t *testing.T) {
	s := newReconcileTestServer(t)
	stale := testAgent("m-moved")
	stale.DesiredMachineID = "mach-ghost-a"
	putTestMember(t, s, stale)

	// A relocate lands after the tick took its snapshot. It moves the pin the
	// way the relocate face does since T-55 — through the column's sole writer,
	// not a whole-row write, which since that change would persist nothing.
	if err := s.dal.SetMemberDesiredMachineID("m-moved", "mach-ghost-b"); err != nil {
		t.Fatalf("relocate: %v", err)
	}

	now := 7000.0
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(stale, now)
	s.reconcileMu.Unlock()

	got, _ := s.dal.GetMember("m-moved")
	// SENTINEL: the block really is stamped — the fixture reaches the write.
	if got == nil || !strings.HasPrefix(got.LastOpReason, placementReasonUnavailable+":") ||
		got.LastOpAt != now {
		t.Fatalf("an unresolvable pin must stamp a block at %v: %+v", now, got)
	}
	if got.DesiredMachineID != "mach-ghost-b" {
		t.Fatalf("the stamp must not write the stale pin back over a relocate, got %q",
			got.DesiredMachineID)
	}

	// A member REMOVED after the snapshot is left alone: writing the snapshot
	// back would resurrect it into the active roster.
	removed := *got
	removed.RosterStatus = RosterStatusRemoved
	putTestMember(t, s, removed)
	// The receipt has to be cleared through its SOLE writer (T-55): zeroing the
	// three fields on the snapshot above no longer does anything, because a
	// whole-row write cannot move these columns any more. Left uncleared, the
	// first stamp's receipt would still be on the row and the assertion below
	// would be reading it instead of the second stamp's absence.
	if err := s.dal.SetMemberOpReceipt("m-moved", "", nil, "", "", 0); err != nil {
		t.Fatalf("clear receipt: %v", err)
	}

	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(stale, now+30)
	s.reconcileMu.Unlock()

	after, _ := s.dal.GetMember("m-moved")
	if after == nil || after.RosterStatus != RosterStatusRemoved ||
		after.LastOpReason != "" || after.LastOpAt != 0 {
		t.Fatalf("a removed member must not be written back by the stamp: %+v", after)
	}
}

// ── stampContextHighRecycle ──────────────────────────────────────────────────

func TestStampContextHighRecycle(t *testing.T) {
	newHot := func(t *testing.T) (*apiServer, []Member) {
		s := newReconcileTestServer(t)
		putTestMember(t, s, testAgent("m-hot"))
		m, _ := s.dal.GetMember("m-hot")
		return s, []Member{*m}
	}
	freshGauge := func(now float64, pct float64) map[string]any {
		return map[string]any{
			"context_pct": pct, "context_pct_ts": now - 10, "boot_ts": now - 500,
		}
	}
	now := 10000.0

	// 🔴 POSITIVE CONTROL, and it has to come first (T-c382). Every other
	// subtest here asserts that something must NOT recycle, which means a
	// mutant that disables auto-refocus outright — shouldAutoRefocus returning
	// a flat false — left this whole test GREEN. "The handover works" and "the
	// handover is dead" were indistinguishable, and nothing would have said so.
	// Measured, not assumed: that mutant was planted and this file passed.
	t.Run("an online member over the handover line IS recycled", func(t *testing.T) {
		s, members := newHot(t)
		connectOnline(t, s, "m-hot")
		s.gauge.Set("m-hot", freshGauge(now, float64(s.ctxhigh.HandoverPct)))
		s.stampContextHighRecycle(members, now)
		if members[0].RefocusSince != now {
			t.Fatalf("crossing the handover line must stamp refocus_since, got %v",
				members[0].RefocusSince)
		}
	})

	t.Run("codex is recycled on compaction count, not percent", func(t *testing.T) {
		// The other half of the positive control: codex has its own axis, and a
		// change that quietly folded it onto the percentage rule would still
		// look green against the claude case above.
		s := newReconcileTestServer(t)
		m := testAgent("m-codex")
		m.Runtime = RuntimeCodex
		putTestMember(t, s, m)
		fresh, _ := s.dal.GetMember("m-codex")
		members := []Member{*fresh}
		connectOnline(t, s, "m-codex")
		s.gauge.Set("m-codex", map[string]any{
			"context_pct": 1.0, "context_pct_ts": now - 10, "boot_ts": now - 500,
			"compaction_count": defaultCodexCompactionThreshold,
		})
		s.stampContextHighRecycle(members, now)
		if members[0].RefocusSince != now {
			t.Fatalf("a codex member at its compaction threshold must recycle even "+
				"at 1%% context, got %v", members[0].RefocusSince)
		}
	})

	t.Run("skips a stale pct", func(t *testing.T) {
		s, members := newHot(t)
		connectOnline(t, s, "m-hot")
		s.gauge.Set("m-hot", map[string]any{
			"context_pct":    99.0,
			"context_pct_ts": now - 600, // reported before this boot
			"boot_ts":        now - 500,
		})
		s.stampContextHighRecycle(members, now)
		if members[0].RefocusSince != 0 {
			t.Fatal("a stale pct must never auto-recycle")
		}
	})

	t.Run("skips a boot-storm fresh boot", func(t *testing.T) {
		s, members := newHot(t)
		connectOnline(t, s, "m-hot")
		s.gauge.Set("m-hot", map[string]any{
			"context_pct": 99.0, "context_pct_ts": now - 1, "boot_ts": now - 10,
		})
		s.stampContextHighRecycle(members, now)
		if members[0].RefocusSince != 0 {
			t.Fatal("a fresh over-line boot must be suppressed (loop-guard)")
		}
	})

	t.Run("below the handover line opens a 停止, not the accelerated stop", func(t *testing.T) {
		s, members := newHot(t)
		connectOnline(t, s, "m-hot")
		// One point below the handover line, and above the notice line. T-ed79:
		// this region DOES open a wind-down now — the plain one, which nothing
		// collects on a clock. What it must never do is open the accelerated
		// stop, which is what crossing the handover threshold is for.
		s.gauge.Set("m-hot", freshGauge(now, float64(s.ctxhigh.HandoverPct-1)))
		s.stampContextHighRecycle(members, now)
		if members[0].RefocusOp != refocusOpContextNotice {
			t.Fatalf("one point below the handover line: refocus_op = %q, want %q",
				members[0].RefocusOp, refocusOpContextNotice)
		}
	})

	t.Run("skips a below-notice gauge and an offline member", func(t *testing.T) {
		s, members := newHot(t)
		connectOnline(t, s, "m-hot")
		s.gauge.Set("m-hot", freshGauge(now, float64(s.ctxhigh.NoticePct-1)))
		s.stampContextHighRecycle(members, now)
		if members[0].RefocusSince != 0 {
			t.Fatalf("below the FIRST threshold nothing may be opened, got op=%q",
				members[0].RefocusOp)
		}

		s2, members2 := newHot(t) // no SSE connection → offline
		s2.gauge.Set("m-hot", freshGauge(now, float64(s2.ctxhigh.HandoverPct)))
		s2.stampContextHighRecycle(members2, now)
		if members2[0].RefocusSince != 0 {
			t.Fatal("an offline member is never stamped")
		}
	})
}

// ── clearRecycleMarkersOnRespawn ─────────────────────────────────────────────

func TestClearRecycleMarkersOnRespawn(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-r")
	m.RefocusSince = 900
	m.StoppedSince = 910
	m.StoppingSince = 905
	putTestMember(t, s, m)
	members := []Member{m}

	t.Run("clears all three markers on the respawn-pending state", func(t *testing.T) {
		s.clearRecycleMarkersOnRespawn(members) // desired online ∧ ¬online ∧ marked
		got := members[0]
		if got.RefocusSince != 0 || got.StoppedSince != 0 || got.StoppingSince != 0 {
			t.Fatalf("markers must clear: %+v", got)
		}
		persisted, _ := s.dal.GetMember("m-r")
		if persisted.RefocusSince != 0 || persisted.StoppingSince != 0 {
			t.Fatalf("clear must persist: %+v", persisted)
		}
	})

	t.Run("skips a still-online recycle-pending member", func(t *testing.T) {
		m2 := testAgent("m-r2")
		m2.RefocusSince = 900
		putTestMember(t, s, m2)
		connectOnline(t, s, "m-r2")
		members2 := []Member{m2}
		s.clearRecycleMarkersOnRespawn(members2)
		if members2[0].RefocusSince != 900 {
			t.Fatal("a recycle-pending (still online) member must keep its marker")
		}
	})

	t.Run("skips a desired-offline member", func(t *testing.T) {
		m3 := testAgent("m-r3")
		m3.DesiredState = DesiredStateOffline
		m3.RefocusSince = 900
		putTestMember(t, s, m3)
		members3 := []Member{m3}
		s.clearRecycleMarkersOnRespawn(members3)
		if members3[0].RefocusSince != 900 {
			t.Fatal("desired-offline teardown is unrelated — no clear")
		}
	})
}

// ── clearStaleStoppingOnOnline ───────────────────────────────────────────────

func TestClearStaleStoppingOnOnline(t *testing.T) {
	s := newReconcileTestServer(t)

	t.Run("clears the anchor on a desired-online observed-online member", func(t *testing.T) {
		m := testAgent("m-s")
		m.StoppingSince = 900
		putTestMember(t, s, m)
		connectOnline(t, s, "m-s")
		members := []Member{m}
		// An anchor older than the whole soft window cannot be a live
		// close-out; a fresh one can, and the sub-test below pins that.
		s.clearStaleStoppingOnOnline(members, 900+SoftOffboardGraceSecs)
		if members[0].StoppingSince != 0 {
			t.Fatal("a survived-stop anchor must clear")
		}
		persisted, _ := s.dal.GetMember("m-s")
		if persisted.StoppingSince != 0 {
			t.Fatalf("clear must persist: %+v", persisted)
		}
	})

	t.Run("leaves an offline or desired-offline member untouched", func(t *testing.T) {
		offline := testAgent("m-s2")
		offline.StoppingSince = 900
		putTestMember(t, s, offline)
		down := testAgent("m-s3")
		down.DesiredState = DesiredStateOffline
		down.StoppingSince = 900
		putTestMember(t, s, down)
		connectOnline(t, s, "m-s3")
		members := []Member{offline, down}
		s.clearStaleStoppingOnOnline(members, 900+SoftOffboardGraceSecs)
		if members[0].StoppingSince != 900 || members[1].StoppingSince != 900 {
			t.Fatalf("no false clears: %+v", members)
		}
	})

	// T-2123: the owner watched a member report 「開始收尾」 and go straight back
	// to green. This sweep was the eraser — a session WORKING its offboard
	// sequence is online, wanted online, and carries the anchor, which is the
	// same shape as a session that survived a stop. The fresh anchor stays.
	t.Run("leaves a close-out that has only just started alone", func(t *testing.T) {
		m := testAgent("m-s4")
		m.StoppingSince = 900
		putTestMember(t, s, m)
		connectOnline(t, s, "m-s4")
		members := []Member{m}
		s.clearStaleStoppingOnOnline(members, 900+SoftOffboardGraceSecs-1)
		if members[0].StoppingSince != 900 {
			t.Fatalf("an in-flight close-out must keep its anchor: %+v", members[0])
		}
		persisted, _ := s.dal.GetMember("m-s4")
		if persisted.StoppingSince != 900 {
			t.Fatalf("nothing may be persisted either: %+v", persisted)
		}
	})

	// T-7723: the owner reported a member 「開始收尾」 at 07:50 and asked TWICE at
	// 08:12 why it had never reported stopping. It had — 22 minutes earlier, and
	// this sweep had erased the anchor at minute 10 while the member was still
	// landing packages and collecting sub-agents. The anchor's AGE cannot tell a
	// close-out that is taking a while apart from one that was abandoned; what
	// separates them is whether the member is still SAYING anything, and the
	// gauge's report ts is the one signal the server has that originates in the
	// member's own live session.
	t.Run("keeps the anchor while the member is still reporting, past the whole window", func(t *testing.T) {
		m := testAgent("m-s5")
		m.StoppingSince = 900
		putTestMember(t, s, m)
		connectOnline(t, s, "m-s5")
		now := 900 + SoftOffboardGraceSecs*3 // deep past the window
		// …and it filed a context report one second ago.
		s.gauge.Set("m-s5", map[string]any{"ts": now - 1})
		members := []Member{m}
		s.clearStaleStoppingOnOnline(members, now)
		if members[0].StoppingSince != 900 {
			t.Fatalf("a close-out that is still reporting must stay visible: %+v", members[0])
		}
		persisted, _ := s.dal.GetMember("m-s5")
		if persisted.StoppingSince != 900 {
			t.Fatalf("nothing may be persisted either: %+v", persisted)
		}
	})

	// The reverse control, and the reason the sweep is not simply deleted: a
	// member that reported stopping and then went QUIET is exactly the residue
	// this sweep was written for. Its behaviour must not change at all.
	t.Run("still clears when the member has gone quiet for the whole window", func(t *testing.T) {
		m := testAgent("m-s6")
		m.StoppingSince = 900
		putTestMember(t, s, m)
		connectOnline(t, s, "m-s6")
		now := 900 + SoftOffboardGraceSecs*3
		// Its last word came in long before the window closed.
		s.gauge.Set("m-s6", map[string]any{"ts": now - SoftOffboardGraceSecs - 1})
		members := []Member{m}
		s.clearStaleStoppingOnOnline(members, now)
		if members[0].StoppingSince != 0 {
			t.Fatalf("a silent survived-stop anchor must still clear: %+v", members[0])
		}
		persisted, _ := s.dal.GetMember("m-s6")
		if persisted.StoppingSince != 0 {
			t.Fatalf("clear must persist: %+v", persisted)
		}
	})

	// 🔴 The OTHER half of the max(), and it is the half that fires most often:
	// the gauge's ts OUTLIVES the session that wrote it. anchorSessionBoot only
	// refreshes boot_ts and clearSessionBootTS only deletes boot_ts — nothing
	// ever clears ts — so a member that reports stopping right after a quiet
	// stretch has an anchor NEWER than its last context report. Taking the
	// gauge's ts on its own would then sweep a close-out that started seconds
	// ago, which is worse than the behaviour this ticket exists to fix.
	t.Run("a fresh anchor wins over a stale gauge report", func(t *testing.T) {
		m := testAgent("m-s8")
		now := 10_000.0
		m.StoppingSince = now // reported stopping just now
		putTestMember(t, s, m)
		connectOnline(t, s, "m-s8")
		// …but its last context report is older than the whole window.
		s.gauge.Set("m-s8", map[string]any{"ts": now - SoftOffboardGraceSecs*2})
		members := []Member{m}
		s.clearStaleStoppingOnOnline(members, now)
		if members[0].StoppingSince != now {
			t.Fatalf("a just-reported close-out must not be swept by an old gauge ts: %+v", members[0])
		}
		persisted, _ := s.dal.GetMember("m-s8")
		if persisted.StoppingSince != now {
			t.Fatalf("nothing may be persisted either: %+v", persisted)
		}
	})

	// 🔴 The gauge is in-memory and volatile by contract (hub.go): a station
	// re-exec blanks it for everyone. A missing record must therefore mean "no
	// opinion" and fall back to the anchor's age — the pre-T-7723 rule — so the
	// volatile store can never make the outcome WORSE than it was before. Same
	// fail-open shape as gaugeSecsSinceBoot's loop guard.
	t.Run("falls back to the anchor age when there is no gauge record at all", func(t *testing.T) {
		m := testAgent("m-s7")
		m.StoppingSince = 900
		putTestMember(t, s, m)
		connectOnline(t, s, "m-s7")
		members := []Member{m}
		s.clearStaleStoppingOnOnline(members, 900+SoftOffboardGraceSecs)
		if members[0].StoppingSince != 0 {
			t.Fatalf("no gauge ⇒ the old rule decides, and it says clear: %+v", members[0])
		}
	})
}

// ── reconcileMemberNow ───────────────────────────────────────────────────────

func TestReconcileMemberNow(t *testing.T) {
	t.Run("dispatches the START immediately and the cadence stays a no-op", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putTestMember(t, s, testAgent("m-a"))
		connectOnline(t, s, ServerSelfHost)

		s.reconcileMemberNow("m-a")
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 1 || frames[0].RPC != "start" {
			t.Fatalf("instant tick must dispatch: %+v", frames)
		}
		// The shared store makes the following cadence tick idempotent.
		s.runReconcileTick(nowSecs())
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("cadence after instant tick must not double-dispatch: %+v", frames)
		}
	})

	t.Run("ignores a non-uninstall warden and a removed member", func(t *testing.T) {
		s := newReconcileTestServer(t)
		connectOnline(t, s, ServerSelfHost)
		warden, _ := s.dal.GetMember(ServerSelfHost)
		warden.DesiredState = DesiredStateOnline
		putTestMember(t, s, *warden)
		s.reconcileMemberNow(ServerSelfHost)

		gone := testAgent("m-gone")
		gone.RosterStatus = RosterStatusRemoved
		putTestMember(t, s, gone)
		s.reconcileMemberNow("m-gone")

		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("no dispatch expected: %+v", frames)
		}
	})

	t.Run("no-reconcile disables the event-driven dispatch", func(t *testing.T) {
		s := newReconcileTestServer(t)
		s.noReconcile = true
		putTestMember(t, s, testAgent("m-a"))
		connectOnline(t, s, ServerSelfHost)
		s.reconcileMemberNow("m-a")
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("--no-reconcile must dispatch nothing: %+v", frames)
		}
	})
}

// ── dispatchRobustStopNow ────────────────────────────────────────────────────

func TestDispatchRobustStopNow(t *testing.T) {
	t.Run("enqueues one stop frame to the reachable warden", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putTestMember(t, s, testAgent("m-a"))
		connectOnline(t, s, ServerSelfHost)
		s.dispatchRobustStopNow("m-a")
		frames := drainFrames(t, s, ServerSelfHost)
		if len(frames) != 1 || frames[0].RPC != "stop" || frames[0].Args["member_id"] != "m-a" {
			t.Fatalf("frames: %+v", frames)
		}
	})

	t.Run("fails closed when the warden is unreachable or reconcile is off", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putTestMember(t, s, testAgent("m-a"))
		s.dispatchRobustStopNow("m-a") // warden offline
		s.noReconcile = true
		connectOnline(t, s, ServerSelfHost)
		s.dispatchRobustStopNow("m-a") // kill-switch on
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("no dispatch expected: %+v", frames)
		}
	})
}

// ── consumeUninstallOnDisconnect ─────────────────────────────────────────────

func TestConsumeUninstallOnDisconnect(t *testing.T) {
	newBox := func(t *testing.T, desired string) *apiServer {
		s := newReconcileTestServer(t)
		putTestMember(t, s, Member{
			ID: "m-box", Name: "box", Kind: KindWarden, Effort: "medium",
			DesiredState: desired, RosterStatus: RosterStatusActive,
		})
		return s
	}

	t.Run("consumes the intent for an offline desired-uninstall warden", func(t *testing.T) {
		s := newBox(t, DesiredStateUninstall)
		s.consumeUninstallOnDisconnect("m-box")
		m, _ := s.dal.GetMember("m-box")
		if m.DesiredState != DesiredStateOffline || m.RosterStatus != RosterStatusActive {
			t.Fatalf("intent must fold to offline, record kept: %+v", m)
		}
	})

	t.Run("leaves a still-online warden's intent alone", func(t *testing.T) {
		s := newBox(t, DesiredStateUninstall)
		connectOnline(t, s, "m-box")
		s.consumeUninstallOnDisconnect("m-box")
		m, _ := s.dal.GetMember("m-box")
		if m.DesiredState != DesiredStateUninstall {
			t.Fatalf("online warden's intent must stay live: %+v", m)
		}
	})

	t.Run("ignores non-uninstall intents and non-warden members", func(t *testing.T) {
		s := newBox(t, DesiredStateOffline)
		agent := testAgent("m-a")
		agent.DesiredState = DesiredStateUninstall // junk on an agent — untouched
		putTestMember(t, s, agent)
		s.consumeUninstallOnDisconnect("m-box")
		s.consumeUninstallOnDisconnect("m-a")
		box, _ := s.dal.GetMember("m-box")
		a, _ := s.dal.GetMember("m-a")
		if box.DesiredState != DesiredStateOffline || a.DesiredState != DesiredStateUninstall {
			t.Fatalf("nothing should change: box=%+v agent=%+v", box, a)
		}
	})

	t.Run("gated off wholesale by --no-reconcile", func(t *testing.T) {
		s := newBox(t, DesiredStateUninstall)
		s.noReconcile = true
		s.consumeUninstallOnDisconnect("m-box")
		m, _ := s.dal.GetMember("m-box")
		if m.DesiredState != DesiredStateUninstall {
			t.Fatalf("kill-switch must suppress the intent write: %+v", m)
		}
	})
}
