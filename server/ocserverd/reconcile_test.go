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
	return newAPIServer(dal, NewHub(), []byte("reconcile-test-secret"), 3600, "../..")
}

func testAgent(id string) Member {
	return Member{
		ID: id, Name: id, Kind: KindAssistant, Effort: "medium",
		DesiredState:     DesiredStateOnline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
	}
}

func putTestMember(t *testing.T, s *apiServer, m Member) {
	t.Helper()
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("put member %s: %v", m.ID, err)
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
	for _, raw := range s.hub.DrainWardenCommands(wardenID) {
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

	t.Run("desired offline and online arms the grace clock without a command", func(t *testing.T) {
		d := reconcileDecide(obsOf("m", DesiredStateOffline, true), newReconcileState(), cfg, 1000)
		if d.Command != reconcileCmdNone || d.State.Phase != reconcilePhaseStopping ||
			d.State.StopDeadline != 1000+cfg.StopGrace {
			t.Fatalf("decision: %+v", d)
		}
		// Within the grace window: keep waiting, still no command.
		d2 := reconcileDecide(obsOf("m", DesiredStateOffline, true), d.State, cfg, 1000+cfg.StopGrace-1)
		if d2.Command != reconcileCmdNone {
			t.Fatalf("within grace: %+v", d2)
		}
	})

	t.Run("grace elapsed dispatches the single robust STOP with stop_retry dedupe", func(t *testing.T) {
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

	t.Run("recycle grace elapsed force-stops a stuck dump", func(t *testing.T) {
		obs := obsOf("m", DesiredStateOnline, true)
		obs.RefocusSince = 1000
		d := reconcileDecide(obs, newReconcileState(), cfg, 1000+cfg.RecycleGrace)
		if d.Command != reconcileCmdStop {
			t.Fatalf("grace elapsed must force-stop: %+v", d)
		}
	})

	// ── relocation: owner re-pinned a LIVE member's desired_machine (kyle-62b2) ──

	t.Run("online with a machine mismatch robust-stops toward the RUNNING machine", func(t *testing.T) {
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
		// on top of it. The refocus STOP routes normally (no DispatchWarden override).
		obs := obsRelocate("m", "mach-old", "mach-new")
		obs.RefocusSince = 1000
		obs.AgentStopped = true
		d := reconcileDecide(obs, newReconcileState(), cfg, 1010)
		if d.Command != reconcileCmdStop || d.DispatchWarden != "" {
			t.Fatalf("refocus must own the recycle, not the relocation path: %+v", d)
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

	t.Run("reaped zombie respawns clean on the next tick", func(t *testing.T) {
		st := newReconcileState()
		st.LastCommand = reconcileCmdStart
		st.LastCommandAt = 1000
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
	orphan := testAgent("m-orphan")
	orphan.DesiredMachineID = "m-no-such-warden"
	putTestMember(t, s, orphan)
	if got := s.wardenTargetOf("m-orphan"); got != "m-no-such-warden" {
		t.Fatalf("no active warden falls back to the raw host key: %q", got)
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

		s.runReconcileTick(1000) // arms the grace clock
		s.runReconcileTick(1030) // within grace
		if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
			t.Fatalf("grace window must dispatch nothing: %+v", frames)
		}
		s.runReconcileTick(1000 + s.reconcileCfg.StopGrace)
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

		s.runReconcileTick(1000)
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
		s.runReconcileTick(1000 + s.reconcileCfg.StopRetry)
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

	t.Run("skips the WARN band and an offline member", func(t *testing.T) {
		s, members := newHot(t)
		connectOnline(t, s, "m-hot")
		s.gauge.Set("m-hot", freshGauge(now, float64(s.ctxhigh.WarnPct)))
		s.stampContextHighRecycle(members, now)
		if members[0].RefocusSince != 0 {
			t.Fatal("WARN alone must not recycle")
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
		s.clearStaleStoppingOnOnline(members)
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
		s.clearStaleStoppingOnOnline(members)
		if members[0].StoppingSince != 900 || members[1].StoppingSince != 900 {
			t.Fatalf("no false clears: %+v", members)
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
