package main

// reconcile_test.go — the behaviour of server/ocserverd/reconcile.go: the exact
// command, reason and next state the pure decider answers with in each of its
// arms, what the dispatch half puts on a warden's FIFO (and what it refuses to),
// the durable receipts and wind-down anchors the stamps leave on a member row,
// and the stderr line each observability path writes.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// reconcileTestNow is the tick clock every test reads back as a literal. It is
// a plausible wall-clock epoch because the token-expiry derivation subtracts a
// week-long TTL from it and must stay positive.
const reconcileTestNow = 1700000000.0

// reconcileTestServer is one full server over a fresh DB, with one extra warden
// ("m-box") on the roster reporting a ready Claude — the machine most of these
// tests place a member on. The warden holds no SSE connection yet: reachability
// is the thing half of these tests are about, so it is opted into per case with
// reconcileTestOnline.
func reconcileTestServer(t *testing.T) (*apiServer, *DAL) {
	t.Helper()
	api, _, d, _ := newAPITestServer(t)
	reconcileTestPut(t, d, Member{ID: "m-box", Name: "Box", Kind: KindWarden})
	api.telemetry.Set("m-box", map[string]any{"runtimes": map[string]any{
		"claude": map[string]any{"installed": true, "logged_in": true},
	}})
	return api, d
}

// reconcileTestPut writes one roster row, defaulting the status to active.
func reconcileTestPut(t *testing.T, d *DAL, m Member) {
	t.Helper()
	if m.RosterStatus == "" {
		m.RosterStatus = RosterStatusActive
	}
	if err := d.PutMember(m); err != nil {
		t.Fatalf("PutMember(%s): %v", m.ID, err)
	}
}

// reconcileTestOnline gives a member a live SSE connection carrying machineID
// as its claim, for the life of the test.
func reconcileTestOnline(t *testing.T, api *apiServer, memberID, machineID string) *hubListener {
	t.Helper()
	l, err := api.hub.Connect(memberID, machineID)
	if err != nil {
		t.Fatalf("hub.Connect(%q): %v", memberID, err)
	}
	t.Cleanup(func() { api.hub.Disconnect(l) })
	return l
}

// reconcileTestRow reads one roster row back.
func reconcileTestRow(t *testing.T, d *DAL, id string) Member {
	t.Helper()
	m, err := d.GetMember(id)
	if err != nil {
		t.Fatalf("GetMember(%s): %v", id, err)
	}
	if m == nil {
		t.Fatalf("GetMember(%s): no such row", id)
	}
	return *m
}

// reconcileTestWantRow asserts the WHOLE stored row equals want — every column,
// so a field a stamp touches without being named here is a failure.
func reconcileTestWantRow(t *testing.T, d *DAL, id string, want Member) {
	t.Helper()
	got := reconcileTestRow(t, d, id)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("row %s:\n got %+v\nwant %+v", id, got, want)
	}
}

// reconcileTestFalse is the addressable false the receipt columns hold.
func reconcileTestFalse() *bool {
	no := false
	return &no
}

func reconcileTestWantDecision(t *testing.T, got, want reconcileDecision) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decision:\n got %+v\nwant %+v", got, want)
	}
}

func TestDefaultReconcileConfig(t *testing.T) {
	t.Run("the frozen timer table is the shipped one, with the zombie window at twice the start timeout", func(t *testing.T) {
		want := reconcileConfig{
			StartTimeout: 120, StopGrace: 120, StopRetry: 90, RecycleGrace: 120,
			SoftOffboardGrace: 600, BackoffBase: 5, BackoffCap: 300,
			CircuitThreshold: 5, CircuitCooldown: 120, ZombieConfirmGrace: 240,
		}
		if got := defaultReconcileConfig(); got != want {
			t.Fatalf("defaultReconcileConfig()\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an owner-set accelerated grace overrides only the recycle grace on the live config", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		if got := api.reconcileConfigLive(); got != defaultReconcileConfig() {
			t.Fatalf("live config out of the box = %+v, want the defaults", got)
		}
		api.acceleratedGraceSecs = 45
		want := defaultReconcileConfig()
		want.RecycleGrace = 45
		if got := api.reconcileConfigLive(); got != want {
			t.Fatalf("live config\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestParseDesired(t *testing.T) {
	t.Run("the two non-default intents pass through byte for byte", func(t *testing.T) {
		for _, raw := range []string{DesiredStateOnline, DesiredStateUninstall} {
			if got := parseDesired(raw); got != raw {
				t.Fatalf("parseDesired(%q) = %q, want it unchanged", raw, got)
			}
		}
	})

	t.Run("the blank value, the literal offline and anything unrecognised all fold to offline so an unknown intent never spawns", func(t *testing.T) {
		for _, raw := range []string{"", DesiredStateOffline, "ONLINE", "Online", "junk", "uninstalled"} {
			if got := parseDesired(raw); got != DesiredStateOffline {
				t.Fatalf("parseDesired(%q) = %q, want %q", raw, got, DesiredStateOffline)
			}
		}
	})
}

func TestDecisionNone(t *testing.T) {
	t.Run("the no-op decision carries the member id, the reason and the caller's state untouched, and nothing else", func(t *testing.T) {
		st := reconcileState{
			Phase: reconcilePhaseStopping, Attempts: 3, BackoffUntil: 12,
			LastCommand: reconcileCmdStop, LastCommandAt: 7,
		}
		got := decisionNone(memberObservation{MemberID: "kip", Desired: DesiredStateOnline}, st, "why not")
		reconcileTestWantDecision(t, got, reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip", Reason: "why not", State: st,
		})
	})

	t.Run("the zero observation yields a blank member id rather than inventing one", func(t *testing.T) {
		got := decisionNone(memberObservation{}, reconcileState{}, "")
		reconcileTestWantDecision(t, got, reconcileDecision{Command: reconcileCmdNone})
	})
}

func TestRobustStopRetryStep(t *testing.T) {
	t.Run("an unarmed marker and a session that is no longer alive both answer done, so the marker is disarmed", func(t *testing.T) {
		for _, c := range []struct {
			name         string
			dispatchedAt float64
			alive        bool
		}{
			{"never dispatched", 0, true},
			{"a negative stamp", -1, true},
			{"dispatched but the session is gone", 1000, false},
			{"neither", 0, false},
		} {
			if got := robustStopRetryStep(c.dispatchedAt, c.alive, 90, 2000); got != robustStopDone {
				t.Fatalf("%s: step = %v, want robustStopDone", c.name, got)
			}
		}
	})

	t.Run("a live session inside the retry window waits, and one at or past the window is re-sent", func(t *testing.T) {
		for _, c := range []struct {
			now  float64
			want robustStopStep
		}{
			{1000, robustStopWait},
			{1089, robustStopWait},
			{1090, robustStopResend},
			{1091, robustStopResend},
		} {
			if got := robustStopRetryStep(1000, true, 90, c.now); got != c.want {
				t.Fatalf("now=%v: step = %v, want %v", c.now, got, c.want)
			}
		}
	})
}

func TestRecycleGraceFor(t *testing.T) {
	t.Run("the two 加速停止 causes are the only clocked ones and both get the configured recycle grace", func(t *testing.T) {
		cfg := defaultReconcileConfig()
		for _, op := range []string{refocusOpContextHigh, refocusOpAcceleratedStop} {
			grace, clocked := recycleGraceFor(op, cfg)
			if !clocked || grace != 120 {
				t.Fatalf("recycleGraceFor(%q) = %v %v, want 120 true", op, grace, clocked)
			}
		}
		tuned := cfg
		tuned.RecycleGrace = 45
		if grace, clocked := recycleGraceFor(refocusOpContextHigh, tuned); !clocked || grace != 45 {
			t.Fatalf("a tuned grace = %v %v, want 45 true", grace, clocked)
		}
	})

	t.Run("every other cause — including the first context threshold and token expiry — runs on no clock at all", func(t *testing.T) {
		cfg := defaultReconcileConfig()
		for _, op := range []string{
			"", "refocus", refocusOpContextNotice, refocusOpTokenExpiry, memberOpRelocate, "made-up",
		} {
			grace, clocked := recycleGraceFor(op, cfg)
			if clocked || grace != 0 {
				t.Fatalf("recycleGraceFor(%q) = %v %v, want 0 false", op, grace, clocked)
			}
		}
	})
}

func TestRegisterStartFailure(t *testing.T) {
	t.Run("one failure bumps the attempt count, arms an exponential backoff and forgets the dispatched command", func(t *testing.T) {
		cfg := defaultReconcileConfig()
		for _, c := range []struct {
			attempts int
			want     float64
		}{{0, 1005}, {1, 1010}, {2, 1020}, {3, 1040}, {4, 1080}} {
			got := registerStartFailure(reconcileState{Attempts: c.attempts}, cfg, 1000, false)
			want := reconcileState{
				Attempts: c.attempts + 1, BackoffUntil: c.want,
				LastCommand: reconcileCmdNone,
			}
			if got != want {
				t.Fatalf("attempts=%d:\n got %+v\nwant %+v", c.attempts, got, want)
			}
		}
	})

	t.Run("repeated silent timeouts saturate the backoff at the cap instead of overflowing", func(t *testing.T) {
		cfg := defaultReconcileConfig()
		for _, attempts := range []int{9, 99, 9999} {
			got := registerStartFailure(reconcileState{Attempts: attempts}, cfg, 1000, false)
			if got.BackoffUntil != 1300 {
				t.Fatalf("attempts=%d: BackoffUntil = %v, want the 300s cap", attempts, got.BackoffUntil)
			}
			if got.CircuitOpen {
				t.Fatalf("attempts=%d: a silent timeout must never trip the breaker", attempts)
			}
		}
	})

	t.Run("a circuit-eligible failure trips the sticky breaker only once the threshold is reached, and arms its cooldown", func(t *testing.T) {
		cfg := defaultReconcileConfig()
		below := registerStartFailure(reconcileState{Attempts: 3}, cfg, 1000, true)
		if below != (reconcileState{Attempts: 4, BackoffUntil: 1040, LastCommand: reconcileCmdNone}) {
			t.Fatalf("the fourth failure = %+v, want the breaker still closed", below)
		}
		at := registerStartFailure(reconcileState{Attempts: 4}, cfg, 1000, true)
		want := reconcileState{
			Attempts: 5, BackoffUntil: 1080, CircuitOpen: true,
			CircuitCooldownUntil: 1120, LastCommand: reconcileCmdNone,
		}
		if at != want {
			t.Fatalf("the fifth failure:\n got %+v\nwant %+v", at, want)
		}
	})

	t.Run("the fold overwrites a previous cooldown rather than leaving a stale one standing", func(t *testing.T) {
		cfg := defaultReconcileConfig()
		got := registerStartFailure(
			reconcileState{Attempts: 0, CircuitCooldownUntil: 999, LastCommandAt: 500}, cfg, 1000, false)
		if got.CircuitCooldownUntil != 0 || got.LastCommandAt != 0 {
			t.Fatalf("stale fields survived: %+v", got)
		}
	})
}

func TestReconcileLog(t *testing.T) {
	t.Run("every line is prefixed, formatted with the caller's arguments and terminated with one newline", func(t *testing.T) {
		got := hubTestStderr(t, func() { reconcileLog("%s: desired=%s command=%s", "kip", "online", "start") })
		want := "[reconcile] kip: desired=online command=start\n"
		if got != want {
			t.Fatalf("stderr:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("a format with no arguments is written verbatim, and two calls are two lines", func(t *testing.T) {
		got := hubTestStderr(t, func() {
			reconcileLog("tick: 0 candidate(s)")
			reconcileLog("tick FAULT: %v", "boom")
		})
		want := "[reconcile] tick: 0 candidate(s)\n[reconcile] tick FAULT: boom\n"
		if got != want {
			t.Fatalf("stderr:\n got %q\nwant %q", got, want)
		}
	})
}

func TestIsStopgapRetryReason(t *testing.T) {
	t.Run("the two retry-loop codes are the class, and only when they carry the code separator", func(t *testing.T) {
		for _, reason := range []string{"backoff: waiting", "circuit_open: too many failed starts"} {
			if !isStopgapRetryReason(reason) {
				t.Fatalf("%q must be a stopgap retry reason", reason)
			}
		}
		for _, reason := range []string{"backoff", "circuit_open", "backoffish: x"} {
			if isStopgapRetryReason(reason) {
				t.Fatalf("%q must not match — the code separator is part of the code", reason)
			}
		}
	})

	t.Run("a diagnosis of what is actually wrong is never in the class, blank included", func(t *testing.T) {
		for _, reason := range []string{
			"", "zombie_suspect: a session is still alive", wakeTimeoutReasonCode + ": never came up",
			placementReasonNoMachine + ": no machine", spawnReasonWardenLost + ": gone",
		} {
			if isStopgapRetryReason(reason) {
				t.Fatalf("%q must not be a stopgap retry reason", reason)
			}
		}
	})
}

func TestIsPlacementBlockedReason(t *testing.T) {
	t.Run("every code in the closed we-did-not-dispatch set matches when it carries the separator", func(t *testing.T) {
		for _, code := range spawnBlockedReasonCodes {
			if !isPlacementBlockedReason(code + ": something") {
				t.Fatalf("%q is in spawnBlockedReasonCodes but did not match", code)
			}
			if isPlacementBlockedReason(code) {
				t.Fatalf("%q with no separator must not match", code)
			}
		}
		if len(spawnBlockedReasonCodes) != 13 {
			t.Fatalf("the closed set has %d codes: %v", len(spawnBlockedReasonCodes), spawnBlockedReasonCodes)
		}
	})

	t.Run("the codes a landed START must NOT invalidate — a wake lapse, an uncollected frame, a standing 停止 — stay outside it", func(t *testing.T) {
		for _, reason := range []string{
			"", wakeTimeoutReasonCode + ": never came up",
			spawnReasonNeverCollected + ": the warden never picked it up",
			spawnReasonHeldDown + ": the owner stopped it",
		} {
			if isPlacementBlockedReason(reason) {
				t.Fatalf("%q must not be treated as placement-blocked", reason)
			}
		}
	})
}

func TestReceiptRendersAsFailure(t *testing.T) {
	t.Run("a populated op with a non-true verdict is the red line, nil verdict included", func(t *testing.T) {
		if !receiptRendersAsFailure(reconcileCmdStart, 5, nil) {
			t.Fatalf("a nil verdict beside a populated op is the wordless red block")
		}
		if !receiptRendersAsFailure(reconcileCmdStart, 5, reconcileTestFalse()) {
			t.Fatalf("an explicit false must render as a failure")
		}
	})

	t.Run("a success receipt, and any row the panel hides entirely, are not failures", func(t *testing.T) {
		yes := true
		if receiptRendersAsFailure(reconcileCmdStart, 5, &yes) {
			t.Fatalf("a success receipt must not be deleted by the converged clears")
		}
		for _, c := range []struct {
			op string
			at float64
		}{{"", 0}, {"", 5}, {reconcileCmdStart, 0}, {reconcileCmdStart, -1}} {
			if receiptRendersAsFailure(c.op, c.at, nil) {
				t.Fatalf("op=%q at=%v: the panel hides the block, so nothing renders", c.op, c.at)
			}
		}
	})
}

func TestBootStormTripped(t *testing.T) {
	t.Run("a boot younger than the guard trips it and one at or past the line does not", func(t *testing.T) {
		for _, c := range []struct {
			secs float64
			want bool
		}{{0, true}, {59, true}, {60, false}, {61, false}, {10000, false}} {
			secs := c.secs
			if got := bootStormTripped(&secs, 60); got != c.want {
				t.Fatalf("bootStormTripped(%v, 60) = %v, want %v", c.secs, got, c.want)
			}
		}
	})

	t.Run("missing or negative data never trips it, and a non-positive minimum disables the guard outright", func(t *testing.T) {
		if bootStormTripped(nil, 60) {
			t.Fatalf("no boot_ts must fail open")
		}
		negative := -1.0
		if bootStormTripped(&negative, 60) {
			t.Fatalf("a negative age must fail open")
		}
		fresh := 1.0
		for _, min := range []float64{0, -5} {
			if bootStormTripped(&fresh, min) {
				t.Fatalf("minBootSecs=%v must disable the guard", min)
			}
		}
	})
}

func TestQuietSince(t *testing.T) {
	t.Run("the later of the close-out anchor and the gauge's report ts wins", func(t *testing.T) {
		m := Member{StoppingSince: 100}
		if got := quietSince(m, map[string]any{"ts": 50.0}); got != 100 {
			t.Fatalf("an older report = %v, want the anchor 100", got)
		}
		if got := quietSince(m, map[string]any{"ts": 500.0}); got != 500 {
			t.Fatalf("a newer report = %v, want 500", got)
		}
		if got := quietSince(Member{}, map[string]any{"ts": 500.0}); got != 500 {
			t.Fatalf("with no anchor = %v, want the report 500", got)
		}
	})

	t.Run("an absent or unusable gauge is no opinion, so the anchor's own age decides", func(t *testing.T) {
		m := Member{StoppingSince: 100}
		for _, gauge := range []map[string]any{nil, {}, {"ts": "not a number"}, {"ts": nil}} {
			if got := quietSince(m, gauge); got != 100 {
				t.Fatalf("gauge %v: quietSince = %v, want the anchor 100", gauge, got)
			}
		}
		if got := quietSince(Member{}, nil); got != 0 {
			t.Fatalf("no anchor and no gauge = %v, want 0", got)
		}
	})
}

func TestTokenExpiryOf(t *testing.T) {
	t.Run("a staff or outsource session expires one TTL after the connect anchor", func(t *testing.T) {
		for _, kind := range []string{KindStaff, KindOutsource} {
			got := tokenExpiryOf(Member{Kind: kind, SessionBootTS: 1000}, 3600)
			if got != 4600 {
				t.Fatalf("kind %s: tokenExpiryOf = %v, want 4600", kind, got)
			}
		}
	})

	t.Run("a warden, an un-anchored session and a non-positive TTL are all not derivable and answer zero", func(t *testing.T) {
		for _, c := range []struct {
			name string
			m    Member
			ttl  int64
		}{
			{"warden — its token carries no exp at all", Member{Kind: KindWarden, SessionBootTS: 1000}, 3600},
			{"no session anchor", Member{Kind: KindStaff}, 3600},
			{"a negative anchor", Member{Kind: KindStaff, SessionBootTS: -5}, 3600},
			{"a zero TTL", Member{Kind: KindStaff, SessionBootTS: 1000}, 0},
			{"a negative TTL", Member{Kind: KindStaff, SessionBootTS: 1000}, -5},
		} {
			if got := tokenExpiryOf(c.m, c.ttl); got != 0 {
				t.Fatalf("%s: tokenExpiryOf = %v, want 0", c.name, got)
			}
		}
	})
}

func TestGaugeNumForDiag(t *testing.T) {
	t.Run("a numeric key renders at full precision, integral values without a decimal point", func(t *testing.T) {
		for _, c := range []struct {
			value any
			want  string
		}{
			{42.0, "42"}, {42, "42"}, {42.5, "42.5"}, {1700000000.0, "1700000000"}, {-3.25, "-3.25"},
		} {
			got := gaugeNumForDiag(map[string]any{"context_pct": c.value}, "context_pct")
			if got != c.want {
				t.Fatalf("gaugeNumForDiag(%#v) = %q, want %q", c.value, got, c.want)
			}
		}
	})

	t.Run("a nil gauge, an absent key and a non-numeric value all render the dash", func(t *testing.T) {
		if got := gaugeNumForDiag(nil, "context_pct"); got != "-" {
			t.Fatalf("nil gauge = %q, want %q", got, "-")
		}
		for _, record := range []map[string]any{
			{}, {"context_pct": "55"}, {"context_pct": nil}, {"other": 1.0},
		} {
			if got := gaugeNumForDiag(record, "context_pct"); got != "-" {
				t.Fatalf("record %v = %q, want %q", record, got, "-")
			}
		}
	})
}

func TestSecsSinceBootForDiag(t *testing.T) {
	t.Run("a usable boot_ts renders the guard's own seconds-since-boot to one decimal, negative included", func(t *testing.T) {
		for _, c := range []struct {
			bootTS float64
			want   string
		}{{1000, "500.0"}, {1499.5, "0.5"}, {2000, "-500.0"}, {0, "1500.0"}} {
			got := secsSinceBootForDiag(map[string]any{"boot_ts": c.bootTS}, 1500)
			if got != c.want {
				t.Fatalf("boot_ts=%v: got %q, want %q", c.bootTS, got, c.want)
			}
		}
	})

	t.Run("the guard's fail-open case — no boot_ts to read — renders the dash", func(t *testing.T) {
		for _, record := range []map[string]any{nil, {}, {"boot_ts": "x"}, {"boot_ts": nil}} {
			if got := secsSinceBootForDiag(record, 1500); got != "-" {
				t.Fatalf("record %v = %q, want %q", record, got, "-")
			}
		}
	})
}

func TestCanPromoteToAcceleratedStop(t *testing.T) {
	t.Run("only a notice epoch crossing the second threshold is promoted", func(t *testing.T) {
		m := Member{RefocusOp: refocusOpContextNotice}
		if !canPromoteToAcceleratedStop(m, refocusOpContextHigh) {
			t.Fatalf("context_notice → context_high must promote")
		}
	})

	t.Run("an owner-opened or agent-opened epoch, a repeat of the same threshold, and a session that already reported stopped are all refused", func(t *testing.T) {
		for _, c := range []struct {
			name string
			m    Member
			op   string
		}{
			{"no epoch at all", Member{}, refocusOpContextHigh},
			{"an owner 改機器 epoch", Member{RefocusOp: memberOpRelocate}, refocusOpContextHigh},
			{"a plain 重新聚焦 epoch", Member{RefocusOp: "refocus"}, refocusOpContextHigh},
			{"the first threshold again", Member{RefocusOp: refocusOpContextNotice}, refocusOpContextNotice},
			{"already reported stopped", Member{RefocusOp: refocusOpContextNotice, StoppedSince: 5}, refocusOpContextHigh},
		} {
			if canPromoteToAcceleratedStop(c.m, c.op) {
				t.Fatalf("%s must not promote", c.name)
			}
		}
	})
}

func TestStampOpReceipt(t *testing.T) {
	t.Run("the five columns are written together: the op, a non-nil false verdict, a blanked log, the reason and the clock", func(t *testing.T) {
		yes := true
		op, log, reason := "previous", "the previous op's log", "the previous reason"
		verdict := &yes
		at := 5.0
		stampOpReceipt(&op, &verdict, &log, &reason, &at, reconcileCmdUninstall, "the machine refused", 1234.5)
		if op != reconcileCmdUninstall {
			t.Fatalf("op = %q, want %q", op, reconcileCmdUninstall)
		}
		if verdict == nil || *verdict {
			t.Fatalf("verdict = %v, want a non-nil false", verdict)
		}
		if log != "" {
			t.Fatalf("log = %q, want it cleared with the op it belonged to", log)
		}
		if reason != "the machine refused" || at != 1234.5 {
			t.Fatalf("reason=%q at=%v", reason, at)
		}
	})

	t.Run("the verdict pointer is fresh rather than the caller's, so two stamps never share one bool", func(t *testing.T) {
		var opA, logA, reasonA string
		var verdictA *bool
		var atA float64
		stampOpReceipt(&opA, &verdictA, &logA, &reasonA, &atA, reconcileCmdStart, "a", 1)
		var opB, logB, reasonB string
		var verdictB *bool
		var atB float64
		stampOpReceipt(&opB, &verdictB, &logB, &reasonB, &atB, reconcileCmdStart, "b", 2)
		if verdictA == verdictB {
			t.Fatalf("the two stamps share one bool")
		}
		*verdictB = true
		if *verdictA {
			t.Fatalf("mutating one verdict changed the other")
		}
	})
}

func TestStampMemberOpReceipt(t *testing.T) {
	t.Run("a staff receipt is always stamped against the start verb, whatever the member was doing", func(t *testing.T) {
		yes := true
		m := Member{
			ID: "kip", LastOp: reconcileCmdStop, LastOpOK: &yes,
			LastOpLog: "an earlier warden log", LastOpReason: "an earlier reason", LastOpAt: 5,
		}
		stampMemberOpReceipt(&m, "no machine is selected", reconcileTestNow)
		want := Member{
			ID: "kip", LastOp: reconcileCmdStart, LastOpOK: reconcileTestFalse(),
			LastOpLog: "", LastOpReason: "no machine is selected", LastOpAt: reconcileTestNow,
		}
		if !reflect.DeepEqual(m, want) {
			t.Fatalf("member:\n got %+v\nwant %+v", m, want)
		}
	})

	t.Run("it mutates in memory only — nothing else on the member is touched", func(t *testing.T) {
		m := Member{ID: "kip", Name: "Kip", Kind: KindStaff, DesiredState: DesiredStateOnline, RefocusSince: 9}
		before := m
		stampMemberOpReceipt(&m, "why", 1)
		before.LastOp = reconcileCmdStart
		before.LastOpOK = reconcileTestFalse()
		before.LastOpReason = "why"
		before.LastOpAt = 1
		if !reflect.DeepEqual(m, before) {
			t.Fatalf("member:\n got %+v\nwant %+v", m, before)
		}
	})
}

func TestRuntimeCapabilityReady(t *testing.T) {
	t.Run("installed with no login verdict, or with a positive one, is ready", func(t *testing.T) {
		yes := true
		if !runtimeCapabilityReady(RuntimeCapabilityDTO{Installed: &yes}) {
			t.Fatalf("installed with no login probe must read ready")
		}
		if !runtimeCapabilityReady(RuntimeCapabilityDTO{Installed: &yes, LoggedIn: &yes}) {
			t.Fatalf("installed and logged in must read ready")
		}
	})

	t.Run("an unreported entry, a measured not-installed, and a known logged-out are all not ready", func(t *testing.T) {
		yes, no := true, false
		for _, c := range []struct {
			name string
			dto  RuntimeCapabilityDTO
		}{
			{"nothing reported", RuntimeCapabilityDTO{}},
			{"measured not installed", RuntimeCapabilityDTO{Installed: &no}},
			{"not installed but logged in", RuntimeCapabilityDTO{Installed: &no, LoggedIn: &yes}},
			{"installed but known logged out", RuntimeCapabilityDTO{Installed: &yes, LoggedIn: &no}},
		} {
			if runtimeCapabilityReady(c.dto) {
				t.Fatalf("%s must not read ready", c.name)
			}
		}
	})
}

func TestShouldAutoRefocus(t *testing.T) {
	t.Run("a claude session at or over the handover percentage is actionable, and one below it is not", func(t *testing.T) {
		cfg := defaultSseContextHigh()
		for _, c := range []struct {
			pct  float64
			want bool
		}{{49, false}, {50, true}, {55, true}, {100, true}} {
			record := map[string]any{"context_pct": c.pct, "context_pct_ts": 100.0, "boot_ts": 50.0}
			if got := shouldAutoRefocus(RuntimeClaude, record, cfg, 3); got != c.want {
				t.Fatalf("pct=%v: shouldAutoRefocus = %v, want %v", c.pct, got, c.want)
			}
			if got := shouldAutoRefocus("", record, cfg, 3); got != c.want {
				t.Fatalf("pct=%v: an unset runtime must read as claude, got %v", c.pct, got)
			}
		}
	})

	t.Run("a stale gauge — no report ts, or one no newer than the connection's boot — is never actionable", func(t *testing.T) {
		cfg := defaultSseContextHigh()
		for _, record := range []map[string]any{
			nil,
			{"context_pct": 95.0},
			{"context_pct": 95.0, "context_pct_ts": 50.0, "boot_ts": 50.0},
			{"context_pct": 95.0, "context_pct_ts": 40.0, "boot_ts": 50.0},
			{"context_pct_ts": 100.0, "boot_ts": 50.0},
		} {
			if shouldAutoRefocus(RuntimeClaude, record, cfg, 3) {
				t.Fatalf("record %v must not be actionable", record)
			}
		}
	})

	t.Run("codex reads its compaction count against its own threshold, and a non-int count is not a reading at all", func(t *testing.T) {
		cfg := defaultSseContextHigh()
		if !shouldAutoRefocus(RuntimeCodex, map[string]any{"compaction_count": 3}, cfg, 3) {
			t.Fatalf("a count at the threshold must be actionable")
		}
		if shouldAutoRefocus(RuntimeCodex, map[string]any{"compaction_count": 2}, cfg, 3) {
			t.Fatalf("a count below the threshold must not be actionable")
		}
		if shouldAutoRefocus(RuntimeCodex, map[string]any{"compaction_count": 3.0}, cfg, 3) {
			t.Fatalf("a float count is not the int this read accepts")
		}
		if shouldAutoRefocus(RuntimeCodex, nil, cfg, 3) {
			t.Fatalf("no gauge must not be actionable")
		}
		if !shouldAutoRefocus(RuntimeCodex, map[string]any{"compaction_count": defaultCodexCompactionThreshold}, cfg, 0) {
			t.Fatalf("a non-positive threshold must fall back to the default %d", defaultCodexCompactionThreshold)
		}
		high := map[string]any{"context_pct": 95.0, "context_pct_ts": 100.0, "boot_ts": 50.0}
		if shouldAutoRefocus(RuntimeCodex, high, cfg, 3) {
			t.Fatalf("codex must not read the fill gauge at all")
		}
	})
}

func TestShouldNoticeRefocus(t *testing.T) {
	t.Run("a claude session at or over the notice percentage is due, below it is not, and the handover band is due too", func(t *testing.T) {
		cfg := defaultSseContextHigh()
		for _, c := range []struct {
			pct  float64
			want bool
		}{{39, false}, {40, true}, {45, true}, {55, true}} {
			record := map[string]any{"context_pct": c.pct, "context_pct_ts": 100.0, "boot_ts": 50.0}
			if got := shouldNoticeRefocus(RuntimeClaude, record, cfg, 2, 3); got != c.want {
				t.Fatalf("pct=%v: shouldNoticeRefocus = %v, want %v", c.pct, got, c.want)
			}
		}
	})

	t.Run("a stale gauge and a disabled notice threshold are both not due", func(t *testing.T) {
		cfg := defaultSseContextHigh()
		for _, record := range []map[string]any{
			nil, {"context_pct": 95.0}, {"context_pct": 95.0, "context_pct_ts": 40.0, "boot_ts": 50.0},
		} {
			if shouldNoticeRefocus(RuntimeClaude, record, cfg, 2, 3) {
				t.Fatalf("record %v must not be due", record)
			}
		}
		off := cfg
		off.NoticePct = 0
		live := map[string]any{"context_pct": 95.0, "context_pct_ts": 100.0, "boot_ts": 50.0}
		if shouldNoticeRefocus(RuntimeClaude, live, off, 2, 3) {
			t.Fatalf("a zero notice threshold disables the first band")
		}
		if !shouldNoticeRefocus(RuntimeClaude, live, cfg, 2, 3) {
			t.Fatalf("the contrast case must be due")
		}
	})

	t.Run("codex asks its own compaction-round predicate rather than the fill gauge", func(t *testing.T) {
		cfg := defaultSseContextHigh()
		high := map[string]any{"context_pct": 95.0, "context_pct_ts": 100.0, "boot_ts": 50.0}
		if shouldNoticeRefocus(RuntimeCodex, high, cfg, 2, 3) {
			t.Fatalf("a codex session must not be noticed off the fill gauge")
		}
		if shouldNoticeRefocus(RuntimeCodex, map[string]any{"compaction_count": 3}, cfg, 2, 3) {
			t.Fatalf("a bare compaction count with no round progress is not the notice signal")
		}
	})
}

// reconcileTestUpObs is a desired-online observation for "kip".
func reconcileTestUpObs() memberObservation {
	return memberObservation{MemberID: "kip", Desired: DesiredStateOnline}
}

func TestDecideUp(t *testing.T) {
	cfg := defaultReconcileConfig()
	const now = reconcileTestNow

	t.Run("an offline member with nothing in flight is started, and the first offline observation arms the zombie-confirm anchor", func(t *testing.T) {
		reconcileTestWantDecision(t, decideUp(reconcileTestUpObs(), newReconcileState(), cfg, now),
			reconcileDecision{
				Command: reconcileCmdStart, MemberID: "kip",
				Reason: "spawn: desired_state online, no live session",
				State: reconcileState{
					Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
					LastCommandAt: now, OfflineSince: now,
				},
			})
	})

	t.Run("an online member with no marker is converged: the failure bookkeeping is reset and the tick reports the recovery", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.Online = true
		st := reconcileState{
			Phase: reconcilePhaseBackoff, Attempts: 4, BackoffUntil: now + 50,
			CircuitOpen: true, CircuitCooldownUntil: now + 999,
			LastCommand: reconcileCmdStop, LastCommandAt: now - 5,
			StopDeadline: now + 5, OfflineSince: now - 500,
		}
		reconcileTestWantDecision(t, decideUp(obs, st, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip", Reason: "online: converged",
			State:           reconcileState{Phase: reconcilePhaseOnline, LastCommand: reconcileCmdNone},
			ConvergedOnline: true,
		})
	})

	t.Run("an online member carrying an unclocked refocus marker waits for the agent's own dump, dispatching nothing", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.Online = true
		obs.RefocusSince = now - 10
		obs.RefocusOp = "refocus"
		reconcileTestWantDecision(t, decideUp(obs, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "recycle: awaiting agent dump (stopping)",
			State:  reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone},
		})
	})

	t.Run("the agent's dump-done report collects the epoch with a recycle STOP addressed to the machine it is actually running on", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.Online = true
		obs.RefocusSince = now - 10
		obs.RefocusOp = "refocus"
		obs.AgentStopped = true
		obs.RunningMachine = "m-old"
		reconcileTestWantDecision(t, decideUp(obs, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindRecycle,
			Reason: "recycle: refocus marker + agent dump done — robust stop",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now,
			},
			DispatchWarden: "m-old",
		})
	})

	t.Run("a clocked 加速停止 epoch whose grace has elapsed is force-stopped even with no dump report", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.Online = true
		obs.RefocusSince = now - 121
		obs.RefocusOp = refocusOpContextHigh
		reconcileTestWantDecision(t, decideUp(obs, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindRecycle,
			Reason: "recycle: refocus grace elapsed (dump stuck) — force stop",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now,
			},
		})
		inside := obs
		inside.RefocusSince = now - 119
		reconcileTestWantDecision(t, decideUp(inside, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "recycle: awaiting agent dump (stopping)",
			State:  reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone},
		})
	})

	t.Run("a recycle STOP already dispatched is not repeated inside stop_retry and is re-dispatched once past it", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.Online = true
		obs.RefocusSince = now - 10
		obs.AgentStopped = true
		within := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now - 89}
		reconcileTestWantDecision(t, decideUp(obs, within, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "recycle: robust stop dispatched — awaiting warden kill (within stop_retry)",
			State:  within,
		})
		past := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now - 90}
		reconcileTestWantDecision(t, decideUp(obs, past, cfg, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindRecycle,
			Reason: "recycle: re-dispatch robust stop (still online past stop_retry — prior STOP unlanded)",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now,
			},
		})
	})

	t.Run("a live member re-pinned to another machine opens a wind-down when one can be armed, dispatching nothing this tick", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.Online = true
		obs.TargetMachine = "m-new"
		obs.RunningMachine = "m-old"
		obs.HandoverArmable = true
		reconcileTestWantDecision(t, decideUp(obs, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "relocate: desired_machine changed (running m-old != target m-new) — " +
				"opening a wind-down; the refocus arm collects it on the agent's hand-off",
			State:         reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone},
			ArmHandoverOp: memberOpRelocate,
		})
	})

	t.Run("a relocation with no hand-off to wait for kills the old session on the spot, addressed to the machine it runs on", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.Online = true
		obs.TargetMachine = "m-new"
		obs.RunningMachine = "m-old"
		reconcileTestWantDecision(t, decideUp(obs, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindRelocate,
			Reason: "relocate: desired_machine changed (running m-old != target m-new) — " +
				"robust stop old session to recycle onto new machine",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now,
			},
			DispatchWarden: "m-old",
		})
		past := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now - 90}
		reconcileTestWantDecision(t, decideUp(obs, past, cfg, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindRelocate,
			Reason: "relocate: re-dispatch robust stop (still on old machine past stop_retry — " +
				"prior STOP unlanded)",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now,
			},
			DispatchWarden: "m-old",
		})
		within := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now - 89}
		reconcileTestWantDecision(t, decideUp(obs, within, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "relocate: robust stop dispatched — awaiting warden kill (within stop_retry)",
			State:  within,
		})
	})

	t.Run("a claim-less session and a member already on its target are both converged rather than relocated", func(t *testing.T) {
		converged := reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip", Reason: "online: converged",
			State:           reconcileState{Phase: reconcilePhaseOnline, LastCommand: reconcileCmdNone},
			ConvergedOnline: true,
		}
		claimless := reconcileTestUpObs()
		claimless.Online = true
		claimless.TargetMachine = "m-new"
		reconcileTestWantDecision(t, decideUp(claimless, newReconcileState(), cfg, now), converged)
		same := reconcileTestUpObs()
		same.Online = true
		same.TargetMachine = "m-one"
		same.RunningMachine = "m-one"
		reconcileTestWantDecision(t, decideUp(same, newReconcileState(), cfg, now), converged)
		unpinned := reconcileTestUpObs()
		unpinned.Online = true
		unpinned.RunningMachine = "m-old"
		reconcileTestWantDecision(t, decideUp(unpinned, newReconcileState(), cfg, now), converged)
	})

	t.Run("a dispatched START inside its start window waits for presence and dispatches nothing", func(t *testing.T) {
		st := reconcileState{Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: now - 120}
		reconcileTestWantDecision(t, decideUp(reconcileTestUpObs(), st, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip", Reason: "starting: awaiting presence",
			State: reconcileState{
				Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
				LastCommandAt: now - 120, OfflineSince: now,
			},
		})
	})

	t.Run("a START that lapsed its window folds into backoff, flags the lapse for the receipt, and never trips the breaker", func(t *testing.T) {
		st := reconcileState{Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: now - 121}
		reconcileTestWantDecision(t, decideUp(reconcileTestUpObs(), st, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip", Reason: "backoff: awaiting retry window",
			State: reconcileState{
				Phase: reconcilePhaseBackoff, Attempts: 1, BackoffUntil: now + 5,
				LastCommand: reconcileCmdNone, OfflineSince: now,
			},
			StartTimedOut: true,
			ReasonCode: spawnReasonBackoff + ": the last start did not come up, so the next " +
				"attempt is waiting out a back-off window — nothing is wrong with the button " +
				"you pressed, the retry has not come round yet",
		})
	})

	t.Run("a START that bounced off the clobber guard withholds the takeover while the reconnect-confirm window is open", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.LastOpKind = reconcileCmdStart
		obs.LastOpReason = spawnClobberReasonPrefix + ": pid 4242 already running"
		st := reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
			LastCommandAt: now - 10, OfflineSince: now - 239,
		}
		reconcileTestWantDecision(t, decideUp(obs, st, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "zombie suspect: START clobbered a live presence-deaf session — " +
				"withholding takeover stop inside the reconnect-confirm grace",
			State: st,
			ReasonCode: spawnReasonZombieSuspect + ": a session for this member is still alive " +
				"on its machine but is not answering, so the start bounced off it. The server " +
				"waits to be sure it is not simply reconnecting before it takes the slot back",
		})
	})

	t.Run("once the reconnect-confirm window lapses the squatting session is reaped with a takeover STOP", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.LastOpKind = reconcileCmdStart
		obs.LastOpReason = spawnClobberReasonPrefix + ": pid 4242 already running"
		st := reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
			LastCommandAt: now - 10, OfflineSince: now - 240,
		}
		reconcileTestWantDecision(t, decideUp(obs, st, cfg, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindZombieTakeover,
			Reason: "zombie takeover: START clobbered a live presence-deaf session — " +
				"robust stop to reap it before respawn",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop,
				LastCommandAt: now, OfflineSince: now - 240,
			},
		})
	})

	t.Run("an online observation clears the continuous-offline anchor, so a reconnect can never be taken over off a stale clock", func(t *testing.T) {
		obs := reconcileTestUpObs()
		obs.Online = true
		st := reconcileState{Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, OfflineSince: now - 9999}
		got := decideUp(obs, st, cfg, now)
		if got.State.OfflineSince != 0 {
			t.Fatalf("OfflineSince = %v, want it cleared by the live observation", got.State.OfflineSince)
		}
		if !got.ConvergedOnline {
			t.Fatalf("the live observation must stand the takeover down: %+v", got)
		}
	})

	t.Run("an open breaker refuses to respawn and a live backoff window waits, each naming its own owner-facing code", func(t *testing.T) {
		open := reconcileState{Phase: reconcilePhaseOffline, LastCommand: reconcileCmdNone, CircuitOpen: true, CircuitCooldownUntil: now + 10}
		reconcileTestWantDecision(t, decideUp(reconcileTestUpObs(), open, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip", Reason: "circuit open: respawn disabled",
			State: reconcileState{
				Phase: reconcilePhaseCircuitOpen, LastCommand: reconcileCmdNone,
				CircuitOpen: true, CircuitCooldownUntil: now + 10, OfflineSince: now,
			},
			ReasonCode: spawnReasonCircuitOpen + ": too many failed starts in a row, so the " +
				"server has stopped retrying this member for now — it will try again by " +
				"itself; fix what is failing on its machine, or 停止 and 活化 to start over",
		})
		backoff := reconcileState{Phase: reconcilePhaseOffline, LastCommand: reconcileCmdNone, BackoffUntil: now + 5}
		reconcileTestWantDecision(t, decideUp(reconcileTestUpObs(), backoff, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip", Reason: "backoff: awaiting retry window",
			State: reconcileState{
				Phase: reconcilePhaseBackoff, LastCommand: reconcileCmdNone,
				BackoffUntil: now + 5, OfflineSince: now,
			},
			ReasonCode: spawnReasonBackoff + ": the last start did not come up, so the next " +
				"attempt is waiting out a back-off window — nothing is wrong with the button " +
				"you pressed, the retry has not come round yet",
		})
	})
}

func TestDecideDown(t *testing.T) {
	cfg := defaultReconcileConfig()
	const now = reconcileTestNow
	obs := memberObservation{MemberID: "kip", Desired: DesiredStateOffline}

	t.Run("an offline member is converged: the stop bookkeeping resets while the breaker fields are left alone", func(t *testing.T) {
		st := reconcileState{
			Phase: reconcilePhaseStopping, Attempts: 3, BackoffUntil: now + 5,
			CircuitOpen: true, CircuitCooldownUntil: now + 99,
			LastCommand: reconcileCmdStop, LastCommandAt: now - 5, StopDeadline: now + 5,
		}
		reconcileTestWantDecision(t, decideDown(obs, st, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip", Reason: "offline: converged",
			State: reconcileState{
				Phase: reconcilePhaseOffline, LastCommand: reconcileCmdNone,
				CircuitOpen: true, CircuitCooldownUntil: now + 99,
			},
		})
	})

	t.Run("a plain 停止 runs no clock at all: the member is left to work its offboard sequence indefinitely", func(t *testing.T) {
		online := obs
		online.Online = true
		want := reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "stopping: agent is working its offboard sequence — collection is the " +
				"agent's stopped report, or the owner's force-stop",
			State: reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone},
		}
		reconcileTestWantDecision(t, decideDown(online, newReconcileState(), cfg, now), want)
		aged := newReconcileState()
		reconcileTestWantDecision(t, decideDown(online, aged, cfg, now+100000), want)
	})

	t.Run("an owner-pressed 加速停止 waits out the grace it opened, then falls through to the single robust stop", func(t *testing.T) {
		inside := obs
		inside.Online = true
		inside.RefocusOp = refocusOpAcceleratedStop
		inside.StoppingSince = now - 119
		reconcileTestWantDecision(t, decideDown(inside, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "stopping: 加速停止 — within the grace the owner opened; collection is " +
				"the agent's stopped report, or this deadline",
			State: reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone},
		})
		lapsed := inside
		lapsed.StoppingSince = now - 120
		reconcileTestWantDecision(t, decideDown(lapsed, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindWinddown,
			Reason: "robust stop: 加速停止 grace elapsed, still online",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now,
			},
		})
	})

	t.Run("a 加速停止 cause with no stopping anchor is not on the clock — it falls back to the plain 停止 wait", func(t *testing.T) {
		noAnchor := obs
		noAnchor.Online = true
		noAnchor.RefocusOp = refocusOpAcceleratedStop
		reconcileTestWantDecision(t, decideDown(noAnchor, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "stopping: agent is working its offboard sequence — collection is the " +
				"agent's stopped report, or the owner's force-stop",
			State: reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone},
		})
	})

	t.Run("a lapsed 加速停止 STOP is deduped inside stop_retry and re-dispatched with the retry wording past it", func(t *testing.T) {
		lapsed := obs
		lapsed.Online = true
		lapsed.RefocusOp = refocusOpAcceleratedStop
		lapsed.StoppingSince = now - 200
		within := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now - 89}
		reconcileTestWantDecision(t, decideDown(lapsed, within, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "stopping: robust stop dispatched — awaiting warden kill (within stop_retry)",
			State:  within,
		})
		past := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now - 90}
		reconcileTestWantDecision(t, decideDown(lapsed, past, cfg, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindWinddown,
			Reason: "robust stop: re-dispatch (still online past stop_retry — prior STOP unlanded)",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop, LastCommandAt: now,
			},
		})
	})

	t.Run("with the soft grace compiled out the legacy timed wind-down arms its deadline from the observation, waits it out, then stops", func(t *testing.T) {
		timed := cfg
		timed.SoftOffboardGrace = 0
		online := obs
		online.Online = true
		reconcileTestWantDecision(t, decideDown(online, newReconcileState(), timed, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "stopping: grace window opened — awaiting agent selfstop",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone, StopDeadline: now + 120,
			},
		})
		armed := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone, StopDeadline: now + 1}
		reconcileTestWantDecision(t, decideDown(online, armed, timed, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "stopping: within grace window — awaiting agent selfstop",
			State:  armed,
		})
		lapsed := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone, StopDeadline: now}
		reconcileTestWantDecision(t, decideDown(online, lapsed, timed, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindWinddown,
			Reason: "robust stop: grace elapsed, still online",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop,
				LastCommandAt: now, StopDeadline: now,
			},
		})
	})
}

func TestDecideUninstall(t *testing.T) {
	cfg := defaultReconcileConfig()
	const now = reconcileTestNow
	obs := memberObservation{MemberID: "m-box", Desired: DesiredStateUninstall}

	t.Run("an offline warden has converged — the box holds no live warden, which is the goal state", func(t *testing.T) {
		st := reconcileState{
			Phase: reconcilePhaseStopping, Attempts: 2, BackoffUntil: now + 5,
			LastCommand: reconcileCmdUninstall, LastCommandAt: now - 5, StopDeadline: now + 5,
		}
		reconcileTestWantDecision(t, decideUninstall(obs, st, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "m-box",
			Reason: "uninstall: converged (warden offline)",
			State:  reconcileState{Phase: reconcilePhaseOffline, LastCommand: reconcileCmdNone},
		})
	})

	t.Run("an online warden is sent the uninstall immediately — no grace window, this is an explicit owner action", func(t *testing.T) {
		online := obs
		online.Online = true
		reconcileTestWantDecision(t, decideUninstall(online, newReconcileState(), cfg, now), reconcileDecision{
			Command: reconcileCmdUninstall, MemberID: "m-box",
			Reason: "uninstall: desired_state uninstall, warden online — dispatch uninstall",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdUninstall, LastCommandAt: now,
			},
		})
	})

	t.Run("a dispatched uninstall is deduped inside stop_retry and re-dispatched once past it", func(t *testing.T) {
		online := obs
		online.Online = true
		within := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdUninstall, LastCommandAt: now - 89}
		reconcileTestWantDecision(t, decideUninstall(online, within, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "m-box",
			Reason: "uninstall: dispatched — awaiting warden removal (within stop_retry)",
			State:  within,
		})
		past := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdUninstall, LastCommandAt: now - 90}
		reconcileTestWantDecision(t, decideUninstall(online, past, cfg, now), reconcileDecision{
			Command: reconcileCmdUninstall, MemberID: "m-box",
			Reason: "uninstall: re-dispatch (still online past stop_retry — prior UNINSTALL unlanded)",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdUninstall, LastCommandAt: now,
			},
		})
	})
}

func TestReconcileDecide(t *testing.T) {
	cfg := defaultReconcileConfig()
	const now = reconcileTestNow

	t.Run("the parsed intent picks the arm, and anything that is not online or uninstall lands on the offline arm", func(t *testing.T) {
		online := memberObservation{MemberID: "kip", Desired: DesiredStateOnline, Online: true}
		reconcileTestWantDecision(t, reconcileDecide(online, newReconcileState(), cfg, now),
			decideUp(online, newReconcileState(), cfg, now))
		uninstall := memberObservation{MemberID: "m-box", Desired: DesiredStateUninstall, Online: true}
		reconcileTestWantDecision(t, reconcileDecide(uninstall, newReconcileState(), cfg, now),
			decideUninstall(uninstall, newReconcileState(), cfg, now))
		for _, desired := range []string{DesiredStateOffline, "", "gibberish"} {
			obs := memberObservation{MemberID: "kip", Desired: desired}
			reconcileTestWantDecision(t, reconcileDecide(obs, newReconcileState(), cfg, now),
				reconcileDecision{
					Command: reconcileCmdNone, MemberID: "kip", Reason: "offline: converged",
					State: reconcileState{Phase: reconcilePhaseOffline, LastCommand: reconcileCmdNone},
				})
		}
	})

	t.Run("a lapsed breaker cooldown half-opens before the arms run: a fresh retry budget and an immediate START", func(t *testing.T) {
		st := reconcileState{
			Phase: reconcilePhaseCircuitOpen, Attempts: 7, BackoffUntil: now + 9999,
			CircuitOpen: true, CircuitCooldownUntil: now, LastCommand: reconcileCmdNone,
		}
		reconcileTestWantDecision(t, reconcileDecide(reconcileTestUpObs(), st, cfg, now), reconcileDecision{
			Command: reconcileCmdStart, MemberID: "kip",
			Reason: "spawn: desired_state online, no live session",
			State: reconcileState{
				Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
				LastCommandAt: now, CircuitCooldownUntil: now, OfflineSince: now,
			},
		})
	})

	t.Run("an armed out-of-band robust STOP is judged before the intent switch: wait, re-send, or disarm on the first offline observation", func(t *testing.T) {
		online := reconcileTestUpObs()
		online.Online = true
		online.RunningMachine = "m-old"

		waiting := reconcileState{Phase: reconcilePhaseOffline, LastCommand: reconcileCmdNone, RobustStopPendingAt: now - 89}
		reconcileTestWantDecision(t, reconcileDecide(online, waiting, cfg, now), reconcileDecision{
			Command: reconcileCmdNone, MemberID: "kip",
			Reason: "robust stop dispatched out-of-band — awaiting warden kill (within stop_retry)",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone, RobustStopPendingAt: now - 89,
			},
		})

		stale := reconcileState{Phase: reconcilePhaseOffline, LastCommand: reconcileCmdNone, RobustStopPendingAt: now - 90}
		reconcileTestWantDecision(t, reconcileDecide(online, stale, cfg, now), reconcileDecision{
			Command: reconcileCmdStop, MemberID: "kip", StopKind: stopKindRobustResend,
			Reason: "robust stop: re-dispatch (out-of-band STOP unlanded — still online past stop_retry)",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdStop,
				LastCommandAt: now, RobustStopPendingAt: now,
			},
			DispatchWarden: "m-old",
		})

		offline := reconcileTestUpObs()
		reconcileTestWantDecision(t, reconcileDecide(offline, waiting, cfg, now), reconcileDecision{
			Command: reconcileCmdStart, MemberID: "kip",
			Reason: "spawn: desired_state online, no live session",
			State: reconcileState{
				Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
				LastCommandAt: now, OfflineSince: now,
			},
		})
	})
}

func TestWardenTargetOf(t *testing.T) {
	t.Run("a warden addresses itself and a pinned agent routes to the active warden on its machine", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "pinned", Name: "Pinned", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-box"})
		if got := api.wardenTargetOf("m-box"); got != "m-box" {
			t.Fatalf("wardenTargetOf(m-box) = %q, want itself", got)
		}
		if got := api.wardenTargetOf("pinned"); got != "m-box" {
			t.Fatalf("wardenTargetOf(pinned) = %q, want m-box", got)
		}
	})

	t.Run("no pin, an unknown member, a pin to a removed warden and a pin that is not a warden at all each answer the blank destination", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-gone", Name: "Gone", Kind: KindWarden, RosterStatus: RosterStatusRemoved})
		reconcileTestPut(t, d, Member{ID: "unpinned", Name: "U", Kind: KindStaff, RoleKey: "engineer"})
		reconcileTestPut(t, d, Member{ID: "removed-pin", Name: "R", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-gone"})
		reconcileTestPut(t, d, Member{ID: "literal-pin", Name: "L", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "auto"})
		reconcileTestPut(t, d, Member{ID: "staff-pin", Name: "S", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "unpinned"})
		for _, id := range []string{"unpinned", "removed-pin", "literal-pin", "staff-pin", "nobody", ""} {
			if got := api.wardenTargetOf(id); got != "" {
				t.Fatalf("wardenTargetOf(%q) = %q, want the blank destination", id, got)
			}
		}
	})

	t.Run("asked about a warden row directly the roster status is not consulted, unlike the same row reached through a pin", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-gone", Name: "Gone", Kind: KindWarden, RosterStatus: RosterStatusRemoved})
		reconcileTestPut(t, d, Member{ID: "via-pin", Name: "V", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-gone"})
		if got := api.wardenTargetOf("m-gone"); got != "m-gone" {
			t.Fatalf("wardenTargetOf(m-gone) = %q, want itself", got)
		}
		if got := api.wardenTargetOf("via-pin"); got != "" {
			t.Fatalf("wardenTargetOf(via-pin) = %q, want the blank destination", got)
		}
	})
}

func TestMemberKillTargetWarden(t *testing.T) {
	t.Run("a kill is addressed to the machine the session actually claims, not the machine it is pinned to", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "pinned", Name: "Pinned", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-box"})
		reconcileTestOnline(t, api, "pinned", "m-old")
		if got := api.memberKillTargetWarden("pinned"); got != "m-old" {
			t.Fatalf("memberKillTargetWarden(pinned) = %q, want the running machine m-old", got)
		}
	})

	t.Run("with no live claim it falls back to the pin, and with neither there is nowhere to address the kill", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "pinned", Name: "Pinned", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-box"})
		reconcileTestPut(t, d, Member{ID: "unpinned", Name: "U", Kind: KindStaff, RoleKey: "engineer"})
		if got := api.memberKillTargetWarden("pinned"); got != "m-box" {
			t.Fatalf("memberKillTargetWarden(pinned) = %q, want the pin m-box", got)
		}
		if got := api.memberKillTargetWarden("unpinned"); got != "" {
			t.Fatalf("memberKillTargetWarden(unpinned) = %q, want the blank destination", got)
		}
	})

	t.Run("a claim-less connection does not shadow the pin — the blank claim is not an address", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "pinned", Name: "Pinned", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-box"})
		reconcileTestOnline(t, api, "pinned", "")
		if got := api.memberKillTargetWarden("pinned"); got != "m-box" {
			t.Fatalf("memberKillTargetWarden(pinned) = %q, want the pin m-box", got)
		}
	})
}

func TestEnqueueToWarden(t *testing.T) {
	t.Run("a reachable warden takes the frame tagged with the member it acts on", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		reconcileTestOnline(t, api, "m-box", "")
		frame := []byte("data: {}\n\n")
		out := hubTestStderr(t, func() {
			if !api.enqueueToWarden("kip", "m-box", frame) {
				t.Fatalf("a reachable warden must accept the frame")
			}
		})
		if out != "" {
			t.Fatalf("an accepted dispatch logs nothing, got %q", out)
		}
		if got := api.hub.DrainWardenCommands("m-box"); !reflect.DeepEqual(got, []wardenCmd{{Subject: "kip", Frame: frame}}) {
			t.Fatalf("queued = %+v", got)
		}
	})

	t.Run("a warden with no live stream, and the blank destination, are both refused fail-closed and queue nothing", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		out := hubTestStderr(t, func() {
			if api.enqueueToWarden("kip", "m-box", []byte("F")) {
				t.Fatalf("an offline warden must be refused")
			}
			if api.enqueueToWarden("kip", "", []byte("F")) {
				t.Fatalf("the blank destination must be refused")
			}
		})
		want := "[reconcile] kip: target warden \"m-box\" NOT reachable (no live SSE downstream) — " +
			"fail-closed, not dispatching, will retry when the warden connects\n" +
			"[reconcile] kip: target warden \"\" NOT reachable (no live SSE downstream) — " +
			"fail-closed, not dispatching, will retry when the warden connects\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if got := api.hub.PendingWardenCommands("m-box"); got != 0 {
			t.Fatalf("a refused dispatch queued %d frame(s)", got)
		}
		if got := api.hub.DrainWardenCommands(""); got != nil {
			t.Fatalf("the blank warden queue holds %+v", got)
		}
	})

	t.Run("enqueueWardenFrame resolves the destination from the member's pin before applying the same gate", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "pinned", Name: "Pinned", Kind: KindStaff, RoleKey: "engineer", DesiredMachineID: "m-box"})
		hubTestStderr(t, func() {
			if api.enqueueWardenFrame("pinned", []byte("F")) {
				t.Fatalf("the pinned warden is offline, so this must be refused")
			}
		})
		reconcileTestOnline(t, api, "m-box", "")
		frame := []byte("data: {}\n\n")
		if !api.enqueueWardenFrame("pinned", frame) {
			t.Fatalf("once the pinned warden is online the frame must be accepted")
		}
		if got := api.hub.DrainWardenCommands("m-box"); !reflect.DeepEqual(got, []wardenCmd{{Subject: "pinned", Frame: frame}}) {
			t.Fatalf("queued = %+v", got)
		}
	})
}

func TestBuildTargetFrame(t *testing.T) {
	t.Run("the frame is the member_id-only command envelope on the warden-command topic", func(t *testing.T) {
		got, ok := buildTargetFrame(reconcileCmdStop, "kip")
		if !ok {
			t.Fatalf("buildTargetFrame must succeed")
		}
		want := "data: {\"topic\":\"warden-command\",\"data\":{\"rpc\":\"stop\",\"args\":{\"member_id\":\"kip\"}}}\n\n"
		if string(got) != want {
			t.Fatalf("frame:\n got %q\nwant %q", got, want)
		}
		digest, decoded := decodeWardenCommandFrame(got)
		if !decoded || digest != (wardenCommandDigest{Verb: reconcileCmdStop, MemberID: "kip"}) {
			t.Fatalf("the frame must read back as its own digest, got %+v (%v)", digest, decoded)
		}
	})

	t.Run("the verb is carried through verbatim and a blank member id is built rather than refused", func(t *testing.T) {
		for _, verb := range []string{reconcileCmdUninstall, reconcileCmdRenew, reconcileCmdUpdate} {
			got, ok := buildTargetFrame(verb, "m-box")
			if !ok {
				t.Fatalf("buildTargetFrame(%q) must succeed", verb)
			}
			want := "data: {\"topic\":\"warden-command\",\"data\":{\"rpc\":\"" + verb +
				"\",\"args\":{\"member_id\":\"m-box\"}}}\n\n"
			if string(got) != want {
				t.Fatalf("frame for %q:\n got %q\nwant %q", verb, got, want)
			}
		}
		got, ok := buildTargetFrame(reconcileCmdStop, "")
		if !ok {
			t.Fatalf("a blank member id is still a buildable frame")
		}
		want := "data: {\"topic\":\"warden-command\",\"data\":{\"rpc\":\"stop\",\"args\":{\"member_id\":\"\"}}}\n\n"
		if string(got) != want {
			t.Fatalf("frame:\n got %q\nwant %q", got, want)
		}
	})
}

func TestBuildStartFrame(t *testing.T) {
	t.Run("an active member with a known role gets a START frame carrying its persona, a minted token and its launch values", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "nova", Name: "Nova", Kind: KindStaff, RoleKey: "assistant",
			Runtime: RuntimeCodex, Model: "gpt-5", Effort: "high",
		})
		frame, ok := api.buildStartFrame(reconcileTestRow(t, d, "nova"))
		if !ok {
			t.Fatalf("buildStartFrame(nova) must succeed")
		}
		digest, decoded := decodeWardenCommandFrame(frame)
		if !decoded || digest != (wardenCommandDigest{Verb: reconcileCmdStart, MemberID: "nova"}) {
			t.Fatalf("digest = %+v (%v)", digest, decoded)
		}
		text := string(frame)
		for _, want := range []string{
			"data: {\"topic\":\"warden-command\",\"data\":{\"rpc\":\"start\",\"args\":{\"member_id\":\"nova\",",
			"\"role\":\"assistant\"",
			"\"runtime\":\"codex\"",
			"\"model\":\"gpt-5\"",
			"\"effort\":\"high\"",
			"\"session_name\":\"\"",
			"\n\n",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("the frame does not carry %q:\n%.400s", want, text)
			}
		}
		var payload struct {
			Data struct {
				Args wardenStartArgs `json:"args"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(text, "data: "), "\n\n")), &payload); err != nil {
			t.Fatalf("the frame is not JSON: %v", err)
		}
		if payload.Data.Args.PersonaContext == "" {
			t.Fatalf("the START must carry the folded persona")
		}
		claims, _, err := verifyJWTAnyKey(api.keys, payload.Data.Args.MemberToken, time.Now().Unix())
		if err != nil {
			t.Fatalf("the riding credential must verify against the server's own ring: %v", err)
		}
		if claims["sub"] != "nova" {
			t.Fatalf("the minted token names %v, want nova", claims["sub"])
		}
	})

	t.Run("a removed member, a role with no definition and an installation with no signing secret are each refused fail-closed and silently", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		removed := reconcileTestRow(t, d, "mira")
		removed.ID = "left"
		removed.RosterStatus = RosterStatusRemoved
		reconcileTestPut(t, d, removed)
		out := hubTestStderr(t, func() {
			if frame, ok := api.buildStartFrame(reconcileTestRow(t, d, "left")); ok || frame != nil {
				t.Fatalf("a removed member must not be booted, got %v %v", frame, ok)
			}
			if frame, ok := api.buildStartFrame(reconcileTestRow(t, d, "kip")); ok || frame != nil {
				t.Fatalf("a role with no definition must fail closed, got %v %v", frame, ok)
			}
		})
		if out != "" {
			t.Fatalf("both refusals are silent, stderr = %q", out)
		}
		bare, _, bareDAL, _ := newAPITestStackWithoutSigningSecret(t)
		if frame, ok := bare.buildStartFrame(reconcileTestRow(t, bareDAL, "mira")); ok || frame != nil {
			t.Fatalf("with no signing secret nothing may be minted, got %v %v", frame, ok)
		}
	})
}

func TestMachineLacksClaudeButHasCodex(t *testing.T) {
	t.Run("only a machine that measured claude absent and reports codex ready answers yes", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		api.telemetry.Set("m-cx", map[string]any{"runtimes": map[string]any{
			"claude": map[string]any{"installed": false},
			"codex":  map[string]any{"installed": true, "logged_in": true},
		}})
		if !api.machineLacksClaudeButHasCodex("m-cx") {
			t.Fatalf("a codex-only box must answer yes")
		}
	})

	t.Run("a blank id, a machine that has never reported, a claude that IS installed, and a codex that is not ready all answer no", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		if api.machineLacksClaudeButHasCodex("") {
			t.Fatalf("the blank machine id must answer no")
		}
		if api.machineLacksClaudeButHasCodex("m-never") {
			t.Fatalf("a machine with no capability report must answer no")
		}
		if api.machineLacksClaudeButHasCodex("m-box") {
			t.Fatalf("a machine reporting claude ready must answer no")
		}
		api.telemetry.Set("m-nocodex", map[string]any{"runtimes": map[string]any{
			"claude": map[string]any{"installed": false},
			"codex":  map[string]any{"installed": false},
		}})
		if api.machineLacksClaudeButHasCodex("m-nocodex") {
			t.Fatalf("with no live codex option there is nothing to point the owner at")
		}
		api.telemetry.Set("m-noclaude-key", map[string]any{"runtimes": map[string]any{
			"codex": map[string]any{"installed": true, "logged_in": true},
		}})
		if api.machineLacksClaudeButHasCodex("m-noclaude-key") {
			t.Fatalf("a missing claude entry is not a measurement and must answer no")
		}
	})
}

func TestWakeTimeoutReason(t *testing.T) {
	t.Run("on a codex-only machine a claude member's lapse names the machine and the two ways out of it", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		api.telemetry.Set("m-cx", map[string]any{"runtimes": map[string]any{
			"claude": map[string]any{"installed": false},
			"codex":  map[string]any{"installed": true, "logged_in": true},
		}})
		got := api.wakeTimeoutReason(Member{ID: "kip", DesiredMachineID: "m-cx"})
		want := wakeTimeoutReasonCode + ": the START was dispatched but the agent never came " +
			"online within the start window — machine 'm-cx' reports no Claude Code installed, " +
			"so this member cannot boot there. Fix any one: set this member's 執行環境 to Codex " +
			"(that machine has it ready); or install Claude Code on that machine " +
			"(warden log: ocwarden.out.log)"
		if got != want {
			t.Fatalf("reason:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("otherwise the sentence names the member's own runtime, with an unset runtime reading as claude", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		api.telemetry.Set("m-cx", map[string]any{"runtimes": map[string]any{
			"claude": map[string]any{"installed": false},
			"codex":  map[string]any{"installed": true, "logged_in": true},
		}})
		generic := func(runtime string) string {
			return wakeTimeoutReasonCode + ": the START was dispatched but the agent never came " +
				"online within the start window — check that " + runtime + " runs and is logged " +
				"in on the target machine (warden log: ocwarden.out.log)"
		}
		for _, c := range []struct {
			name string
			m    Member
			want string
		}{
			{"a codex member on the same codex-only machine", Member{ID: "kip", DesiredMachineID: "m-cx", Runtime: RuntimeCodex}, generic(RuntimeCodex)},
			{"an unset runtime on a machine that has claude", Member{ID: "kip", DesiredMachineID: "m-box"}, generic(RuntimeClaude)},
			{"a member with no machine at all", Member{ID: "kip"}, generic(RuntimeClaude)},
		} {
			if got := c.want; api.wakeTimeoutReason(c.m) != got {
				t.Fatalf("%s:\n got %q\nwant %q", c.name, api.wakeTimeoutReason(c.m), got)
			}
		}
	})
}

func TestResolveEmptyRuntimeForPlacement(t *testing.T) {
	t.Run("an unset runtime resolves to the ready runtime the target machine reports, and is persisted on the row", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "fresh", Name: "F", Kind: KindStaff, RoleKey: "assistant"})
		before := reconcileTestRow(t, d, "fresh")
		m := before
		out := hubTestStderr(t, func() { api.resolveEmptyRuntimeForPlacement(&m, "m-box") })
		if out != "" {
			t.Fatalf("a clean resolution logs nothing, got %q", out)
		}
		if m.Runtime != RuntimeClaude {
			t.Fatalf("in-memory runtime = %q, want claude", m.Runtime)
		}
		want := before
		want.Runtime = RuntimeClaude
		reconcileTestWantRow(t, d, "fresh", want)

		api.telemetry.Set("m-cx", map[string]any{"runtimes": map[string]any{
			"claude": map[string]any{"installed": false},
			"codex":  map[string]any{"installed": true, "logged_in": true},
		}})
		reconcileTestPut(t, d, Member{ID: "fresh2", Name: "F2", Kind: KindStaff, RoleKey: "assistant"})
		second := reconcileTestRow(t, d, "fresh2")
		codex := second
		api.resolveEmptyRuntimeForPlacement(&codex, "m-cx")
		if codex.Runtime != RuntimeCodex {
			t.Fatalf("in-memory runtime = %q, want codex", codex.Runtime)
		}
		wantSecond := second
		wantSecond.Runtime = RuntimeCodex
		reconcileTestWantRow(t, d, "fresh2", wantSecond)
	})

	t.Run("a warden, an already-chosen runtime, a machine that has not reported, and a machine with nothing ready are all left untouched", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		api.telemetry.Set("m-none", map[string]any{"runtimes": map[string]any{
			"claude": map[string]any{"installed": false},
			"codex":  map[string]any{"installed": false},
		}})
		reconcileTestPut(t, d, Member{ID: "chosen", Name: "C", Kind: KindStaff, RoleKey: "assistant", Runtime: RuntimeCodex})
		reconcileTestPut(t, d, Member{ID: "silent", Name: "S", Kind: KindStaff, RoleKey: "assistant"})
		reconcileTestPut(t, d, Member{ID: "barren", Name: "B", Kind: KindStaff, RoleKey: "assistant"})
		for _, c := range []struct {
			name   string
			id     string
			warden string
			want   string
		}{
			{"a warden row", "m-box", "m-box", ""},
			{"a runtime the owner already chose", "chosen", "m-box", RuntimeCodex},
			{"a machine that has never heartbeat", "silent", "m-never", ""},
			{"a machine where nothing is ready", "barren", "m-none", ""},
		} {
			before := reconcileTestRow(t, d, c.id)
			m := before
			out := hubTestStderr(t, func() { api.resolveEmptyRuntimeForPlacement(&m, c.warden) })
			if out != "" {
				t.Fatalf("%s: stderr = %q, want nothing", c.name, out)
			}
			if m.Runtime != c.want {
				t.Fatalf("%s: in-memory runtime = %q, want %q", c.name, m.Runtime, c.want)
			}
			reconcileTestWantRow(t, d, c.id, before)
		}
	})

	t.Run("the legacy claude installed-but-logged-out shape declines to choose and says so, rather than pinning the member to codex", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		api.telemetry.Set("m-legacy", map[string]any{"runtimes": map[string]any{
			"claude": map[string]any{"installed": true, "logged_in": false},
			"codex":  map[string]any{"installed": true, "logged_in": true},
		}})
		reconcileTestPut(t, d, Member{ID: "undecided", Name: "U", Kind: KindStaff, RoleKey: "assistant"})
		before := reconcileTestRow(t, d, "undecided")
		m := before
		out := hubTestStderr(t, func() { api.resolveEmptyRuntimeForPlacement(&m, "m-legacy") })
		want := "[reconcile] undecided: machine \"m-legacy\" reports claude installed with " +
			"logged_in:false — a shape only a warden older than v0.5.211-beta.1 emits, where it " +
			"means \"no credential evidence found\", NOT \"signed out\". Declining to auto-resolve " +
			"this member to codex, because persisting that choice is irreversible and this machine " +
			"may well run claude (env-carried key, Bedrock/Vertex managed auth, or " +
			"OC_CLAUDE_CRED_CHECK=0). Leaving 執行環境 unset: the start still goes out as claude, " +
			"and if it really is signed out the spawn will say so and name the Codex exit. To " +
			"choose deliberately instead: upgrade that machine's warden, or set this member's " +
			"執行環境 by hand.\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if m.Runtime != "" {
			t.Fatalf("in-memory runtime = %q, want it left unset", m.Runtime)
		}
		reconcileTestWantRow(t, d, "undecided", before)
	})

	t.Run("a concurrent owner edit landing mid-tick wins: the tick's copy adopts the stored choice and writes nothing", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "raced", Name: "R", Kind: KindStaff, RoleKey: "assistant"})
		m := reconcileTestRow(t, d, "raced")
		if err := d.SetMemberRuntime("raced", RuntimeCodex); err != nil {
			t.Fatalf("SetMemberRuntime: %v", err)
		}
		want := reconcileTestRow(t, d, "raced")
		api.resolveEmptyRuntimeForPlacement(&m, "m-box")
		if m.Runtime != RuntimeCodex {
			t.Fatalf("in-memory runtime = %q, want the owner's codex", m.Runtime)
		}
		reconcileTestWantRow(t, d, "raced", want)
	})
}

func TestStampMemberPlacementBlocked(t *testing.T) {
	t.Run("an unplaced member is told there is no automatic placement, and a member pinned to a dead machine is told to choose another", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "unplaced", Name: "U", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		reconcileTestPut(t, d, Member{ID: "misplaced", Name: "M", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-gone"})
		for _, c := range []struct {
			id   string
			want string
		}{
			{"unplaced", placementReasonNoMachine + ": no machine is selected for this member — " +
				"choose one (改機器) before waking it; there is no automatic placement"},
			{"misplaced", placementReasonUnavailable + ": machine 'm-gone' is not an active machine — " +
				"choose another one (改機器); no other machine is substituted"},
		} {
			before := reconcileTestRow(t, d, c.id)
			m := before
			api.stampMemberPlacementBlocked(&m, reconcileTestNow)
			want := before
			want.LastOp = reconcileCmdStart
			want.LastOpOK = reconcileTestFalse()
			want.LastOpReason = c.want
			want.LastOpAt = reconcileTestNow
			reconcileTestWantRow(t, d, c.id, want)
		}
	})

	t.Run("re-stamping the same cause does not move the clock, while a changed cause always writes", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "stalled", Name: "S", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		m := reconcileTestRow(t, d, "stalled")
		api.stampMemberPlacementBlocked(&m, reconcileTestNow)
		stamped := reconcileTestRow(t, d, "stalled")
		api.stampMemberPlacementBlocked(&m, reconcileTestNow+500)
		reconcileTestWantRow(t, d, "stalled", stamped)

		reconcileTestPut(t, d, Member{ID: "moved", Name: "M", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-gone"})
		moved := reconcileTestRow(t, d, "moved")
		api.stampMemberPlacementBlocked(&moved, reconcileTestNow)
		first := reconcileTestRow(t, d, "moved")
		if first.LastOpAt != reconcileTestNow {
			t.Fatalf("the first stamp did not land: %+v", first)
		}
	})

	t.Run("the reason names the pin on the row being written, not the one the tick snapshotted", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "relocated", Name: "R", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		snapshot := reconcileTestRow(t, d, "relocated")
		if err := d.SetMemberDesiredMachineID("relocated", "m-later"); err != nil {
			t.Fatalf("SetMemberDesiredMachineID: %v", err)
		}
		api.stampMemberPlacementBlocked(&snapshot, reconcileTestNow)
		got := reconcileTestRow(t, d, "relocated")
		want := placementReasonUnavailable + ": machine 'm-later' is not an active machine — " +
			"choose another one (改機器); no other machine is substituted"
		if got.LastOpReason != want {
			t.Fatalf("reason:\n got %q\nwant %q", got.LastOpReason, want)
		}
		if got.DesiredMachineID != "m-later" {
			t.Fatalf("the mid-tick placement was reverted: %q", got.DesiredMachineID)
		}
	})

	t.Run("a removed member and a member that no longer exists are both left alone", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "left", Name: "L", Kind: KindStaff, RoleKey: "assistant", RosterStatus: RosterStatusRemoved})
		before := reconcileTestRow(t, d, "left")
		m := before
		api.stampMemberPlacementBlocked(&m, reconcileTestNow)
		reconcileTestWantRow(t, d, "left", before)
		ghost := Member{ID: "never-existed"}
		api.stampMemberPlacementBlocked(&ghost, reconcileTestNow)
		if row, _ := d.GetMember("never-existed"); row != nil {
			t.Fatalf("a missing member must not be created: %+v", row)
		}
	})
}

func TestStampMemberOpBlocked(t *testing.T) {
	t.Run("a stall code is written onto the row as a failed start receipt, and a blank code writes nothing", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "stalled", Name: "S", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		before := reconcileTestRow(t, d, "stalled")
		api.stampMemberOpBlocked("stalled", "", reconcileTestNow)
		reconcileTestWantRow(t, d, "stalled", before)

		api.stampMemberOpBlocked("stalled", spawnReasonCircuitOpen+": too many failed starts", reconcileTestNow)
		want := before
		want.LastOp = reconcileCmdStart
		want.LastOpOK = reconcileTestFalse()
		want.LastOpReason = spawnReasonCircuitOpen + ": too many failed starts"
		want.LastOpAt = reconcileTestNow
		reconcileTestWantRow(t, d, "stalled", want)
	})

	t.Run("the same cause re-decided every tick does not churn the row, while a different cause replaces it", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "stalled", Name: "S", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		api.stampMemberOpBlocked("stalled", spawnReasonBackoff+": waiting", reconcileTestNow)
		stamped := reconcileTestRow(t, d, "stalled")
		api.stampMemberOpBlocked("stalled", spawnReasonBackoff+": waiting", reconcileTestNow+500)
		reconcileTestWantRow(t, d, "stalled", stamped)

		api.stampMemberOpBlocked("stalled", spawnReasonZombieSuspect+": a session is still alive", reconcileTestNow+500)
		want := stamped
		want.LastOpReason = spawnReasonZombieSuspect + ": a session is still alive"
		want.LastOpAt = reconcileTestNow + 500
		reconcileTestWantRow(t, d, "stalled", want)
	})

	t.Run("the retry loop describing its own wait yields to a standing wake-lapse diagnosis, while a fresh finding replaces it", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "lapsed", Name: "L", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		api.stampMemberOpBlocked("lapsed", wakeTimeoutReasonCode+": the agent never came up", reconcileTestNow)
		diagnosed := reconcileTestRow(t, d, "lapsed")
		for _, retry := range []string{spawnReasonBackoff + ": waiting", spawnReasonCircuitOpen + ": disabled"} {
			api.stampMemberOpBlocked("lapsed", retry, reconcileTestNow+10)
			reconcileTestWantRow(t, d, "lapsed", diagnosed)
		}
		api.stampMemberOpBlocked("lapsed", spawnReasonZombieSuspect+": a session is still alive", reconcileTestNow+20)
		want := diagnosed
		want.LastOpReason = spawnReasonZombieSuspect + ": a session is still alive"
		want.LastOpAt = reconcileTestNow + 20
		reconcileTestWantRow(t, d, "lapsed", want)
	})

	t.Run("a removed member and an id with no row are both left alone", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "left", Name: "L", Kind: KindStaff, RoleKey: "assistant", RosterStatus: RosterStatusRemoved})
		before := reconcileTestRow(t, d, "left")
		api.stampMemberOpBlocked("left", spawnReasonBackoff+": waiting", reconcileTestNow)
		reconcileTestWantRow(t, d, "left", before)
		api.stampMemberOpBlocked("never-existed", spawnReasonBackoff+": waiting", reconcileTestNow)
		if row, _ := d.GetMember("never-existed"); row != nil {
			t.Fatalf("a missing member must not be created: %+v", row)
		}
	})
}

func TestClearMemberConvergedFailureReceipt(t *testing.T) {
	t.Run("a row the cockpit would paint red has all five receipt columns cleared", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "recovered", Name: "R", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		api.stampMemberOpBlocked("recovered", spawnReasonBackoff+": waiting", reconcileTestNow)
		failed := reconcileTestRow(t, d, "recovered")
		api.clearMemberConvergedFailureReceipt("recovered", failed)
		want := failed
		want.LastOp = ""
		want.LastOpOK = nil
		want.LastOpLog = ""
		want.LastOpReason = ""
		want.LastOpAt = 0
		reconcileTestWantRow(t, d, "recovered", want)
	})

	t.Run("a clean row, a success receipt and an already-cleared row are all left untouched, so converged ticks never churn", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "healthy", Name: "H", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		clean := reconcileTestRow(t, d, "healthy")
		api.clearMemberConvergedFailureReceipt("healthy", clean)
		reconcileTestWantRow(t, d, "healthy", clean)

		yes := true
		succeeded := clean
		succeeded.LastOp = reconcileCmdStart
		succeeded.LastOpOK = &yes
		succeeded.LastOpAt = reconcileTestNow
		if err := d.SetMemberOpReceipt("healthy", succeeded.LastOp, succeeded.LastOpOK,
			succeeded.LastOpLog, succeeded.LastOpReason, succeeded.LastOpAt); err != nil {
			t.Fatalf("SetMemberOpReceipt: %v", err)
		}
		stored := reconcileTestRow(t, d, "healthy")
		api.clearMemberConvergedFailureReceipt("healthy", stored)
		reconcileTestWantRow(t, d, "healthy", stored)
	})

	t.Run("a receipt written after the tick's snapshot survives: the clear is judged on the re-read row", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "raced", Name: "R", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		api.stampMemberOpBlocked("raced", spawnReasonBackoff+": waiting", reconcileTestNow)
		snapshot := reconcileTestRow(t, d, "raced")
		api.clearMemberConvergedFailureReceipt("raced", snapshot)
		cleared := reconcileTestRow(t, d, "raced")
		api.clearMemberConvergedFailureReceipt("raced", snapshot)
		reconcileTestWantRow(t, d, "raced", cleared)
	})

	t.Run("a removed member is left alone even while its snapshot renders as a failure", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "left", Name: "L", Kind: KindStaff, RoleKey: "assistant",
			RosterStatus: RosterStatusRemoved, LastOp: reconcileCmdStart, LastOpAt: reconcileTestNow,
		})
		before := reconcileTestRow(t, d, "left")
		if !receiptRendersAsFailure(before.LastOp, before.LastOpAt, before.LastOpOK) {
			t.Fatalf("the fixture must start as a rendered failure: %+v", before)
		}
		api.clearMemberConvergedFailureReceipt("left", before)
		reconcileTestWantRow(t, d, "left", before)
	})
}

func TestStampWakeObservability(t *testing.T) {
	t.Run("a landed START stamps the waking anchor so a failed wake stops rendering as a member nobody woke", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "woken", Name: "W", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		before := reconcileTestRow(t, d, "woken")
		m := before
		api.stampWakeObservability(&m, reconcileDecision{Command: reconcileCmdStart}, reconcileTestNow)
		if m.WakingSince != reconcileTestNow {
			t.Fatalf("in-memory WakingSince = %v", m.WakingSince)
		}
		want := before
		want.WakingSince = reconcileTestNow
		reconcileTestWantRow(t, d, "woken", want)
	})

	t.Run("a landed START retires a placement-blocked explanation but leaves the failed-start verdict standing", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "blocked", Name: "B", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		m := reconcileTestRow(t, d, "blocked")
		api.stampMemberPlacementBlocked(&m, reconcileTestNow)
		stamped := reconcileTestRow(t, d, "blocked")
		fresh := stamped
		api.stampWakeObservability(&fresh, reconcileDecision{Command: reconcileCmdStart}, reconcileTestNow+10)
		want := stamped
		want.WakingSince = reconcileTestNow + 10
		want.LastOpReason = ""
		want.LastOpLog = ""
		reconcileTestWantRow(t, d, "blocked", want)
	})

	t.Run("a START that lapsed its window writes the wake-timeout receipt and clears the now-false waking anchor", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "lapsed", Name: "L", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box", WakingSince: reconcileTestNow,
		})
		before := reconcileTestRow(t, d, "lapsed")
		m := before
		api.stampWakeObservability(&m, reconcileDecision{StartTimedOut: true}, reconcileTestNow+300)
		want := before
		want.WakingSince = 0
		want.LastOp = reconcileCmdStart
		want.LastOpOK = reconcileTestFalse()
		want.LastOpAt = reconcileTestNow + 300
		want.LastOpReason = wakeTimeoutReasonCode + ": the START was dispatched but the agent " +
			"never came online within the start window — check that claude runs and is logged " +
			"in on the target machine (warden log: ocwarden.out.log)"
		reconcileTestWantRow(t, d, "lapsed", want)
	})

	t.Run("a lapse whose frame was dropped mid-delivery names the connection instead of sending the owner to the machine", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "lost", Name: "L", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box", WakingSince: reconcileTestNow,
		})
		frame, _ := buildTargetFrame(reconcileCmdStart, "lost")
		hubTestStderr(t, func() {
			api.hub.ReturnUndeliveredCommands("m-box", []wardenCmd{{Subject: "lost", Frame: frame}})
		})
		before := reconcileTestRow(t, d, "lost")
		m := before
		api.stampWakeObservability(&m, reconcileDecision{StartTimedOut: true}, reconcileTestNow+300)
		want := before
		want.WakingSince = 0
		want.LastOp = reconcileCmdStart
		want.LastOpOK = reconcileTestFalse()
		want.LastOpAt = reconcileTestNow + 300
		want.LastOpReason = wakeTimeoutReasonCode + ": the START never reached machine \"m-box\" — " +
			"its SSE stream failed mid-delivery and the frame was dropped server-side, so nothing " +
			"on that machine was ever asked to start; do not go looking at claude there, the " +
			"machine's connection is the suspect"
		reconcileTestWantRow(t, d, "lost", want)
	})

	t.Run("a converged tick clears the failure receipt and writes nothing else, while a tick with nothing to say writes nothing at all", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "back", Name: "B", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		api.stampMemberOpBlocked("back", spawnReasonBackoff+": waiting", reconcileTestNow)
		failed := reconcileTestRow(t, d, "back")
		m := failed
		api.stampWakeObservability(&m, reconcileDecision{Command: reconcileCmdNone, ConvergedOnline: true}, reconcileTestNow+10)
		want := failed
		want.LastOp = ""
		want.LastOpOK = nil
		want.LastOpLog = ""
		want.LastOpReason = ""
		want.LastOpAt = 0
		reconcileTestWantRow(t, d, "back", want)

		quiet := reconcileTestRow(t, d, "back")
		api.stampWakeObservability(&quiet, reconcileDecision{Command: reconcileCmdNone}, reconcileTestNow+20)
		reconcileTestWantRow(t, d, "back", want)
	})

	t.Run("a removed member is never written, whichever half of the stamp the decision asks for", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "left", Name: "L", Kind: KindStaff, RoleKey: "assistant", RosterStatus: RosterStatusRemoved})
		before := reconcileTestRow(t, d, "left")
		for _, decision := range []reconcileDecision{
			{Command: reconcileCmdStart},
			{StartTimedOut: true},
		} {
			m := before
			api.stampWakeObservability(&m, decision, reconcileTestNow)
			reconcileTestWantRow(t, d, "left", before)
		}
	})
}

func TestReconcileOne(t *testing.T) {
	t.Run("a decided START is built, enqueued on the target warden's FIFO and leaves the advanced state standing", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestOnline(t, api, "m-box", "")
		reconcileTestPut(t, d, Member{ID: "runner", Name: "Runner", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		out := hubTestStderr(t, func() {
			got := api.reconcileOne(reconcileTestRow(t, d, "runner"), newReconcileState(), reconcileTestNow)
			reconcileTestWantDecision(t, got, reconcileDecision{
				Command: reconcileCmdStart, MemberID: "runner",
				Reason: "spawn: desired_state online, no live session",
				State: reconcileState{
					Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
					LastCommandAt: reconcileTestNow, OfflineSince: reconcileTestNow,
				},
			})
		})
		if out != "" {
			t.Fatalf("a clean dispatch logs nothing, got %q", out)
		}
		queued := api.hub.DrainWardenCommands("m-box")
		if len(queued) != 1 || queued[0].Subject != "runner" {
			t.Fatalf("queued = %+v, want one frame tagged runner", queued)
		}
		if digest, ok := decodeWardenCommandFrame(queued[0].Frame); !ok ||
			digest != (wardenCommandDigest{Verb: reconcileCmdStart, MemberID: "runner"}) {
			t.Fatalf("queued frame digest = %+v (%v)", digest, ok)
		}
	})

	t.Run("a member with no machine is downgraded to a no-op that reports unlanded, keeps the prior state and stamps the row", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "nowhere", Name: "Nowhere", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		before := reconcileTestRow(t, d, "nowhere")
		got := api.reconcileOne(before, newReconcileState(), reconcileTestNow)
		reconcileTestWantDecision(t, got, reconcileDecision{
			Command: reconcileCmdNone, MemberID: "nowhere", Reason: "no machine selected",
			State:            newReconcileState(),
			DispatchUnlanded: true,
		})
		want := before
		want.LastOp = reconcileCmdStart
		want.LastOpOK = reconcileTestFalse()
		want.LastOpAt = reconcileTestNow
		want.LastOpReason = placementReasonNoMachine + ": no machine is selected for this member — " +
			"choose one (改機器) before waking it; there is no automatic placement"
		reconcileTestWantRow(t, d, "nowhere", want)
	})

	t.Run("a target machine that does not report the member's runtime refuses the dispatch and says which runtime it wanted", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-cx", Name: "CX", Kind: KindWarden})
		api.telemetry.Set("m-cx", map[string]any{"runtimes": map[string]any{
			"codex": map[string]any{"installed": true, "logged_in": true},
		}})
		reconcileTestOnline(t, api, "m-cx", "")
		reconcileTestPut(t, d, Member{
			ID: "wrongbox", Name: "Wrongbox", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-cx", Runtime: RuntimeClaude,
		})
		out := hubTestStderr(t, func() {
			got := api.reconcileOne(reconcileTestRow(t, d, "wrongbox"), newReconcileState(), reconcileTestNow)
			reconcileTestWantDecision(t, got, reconcileDecision{
				Command: reconcileCmdNone, MemberID: "wrongbox",
				Reason: "selected runtime unavailable on target machine",
				State:  newReconcileState(), DispatchUnlanded: true,
			})
		})
		want := "[reconcile] wrongbox: target warden \"m-cx\" does not report runtime \"claude\" ready — fail-closed\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if got := api.hub.PendingWardenCommands("m-cx"); got != 0 {
			t.Fatalf("a refused dispatch queued %d frame(s)", got)
		}
	})

	t.Run("a START whose payload cannot be assembled is refused loudly and its state is rolled back to offline", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestOnline(t, api, "m-box", "")
		reconcileTestPut(t, d, Member{
			ID: "roleless", Name: "Roleless", Kind: KindStaff, RoleKey: "engineer",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box", Runtime: RuntimeClaude,
		})
		out := hubTestStderr(t, func() {
			got := api.reconcileOne(reconcileTestRow(t, d, "roleless"), newReconcileState(), reconcileTestNow)
			reconcileTestWantDecision(t, got, reconcileDecision{
				Command: reconcileCmdNone, MemberID: "roleless",
				Reason: "no start payload (persona/token) — fail-closed",
				State:  newReconcileState(),
			})
		})
		want := "[reconcile] roleless: no START payload (persona/token) — fail-closed, not dispatching\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if got := api.hub.PendingWardenCommands("m-box"); got != 0 {
			t.Fatalf("a refused dispatch queued %d frame(s)", got)
		}
	})

	t.Run("a decided UNINSTALL for an online warden is enqueued on that warden's own FIFO", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-doomed", Name: "Doomed", Kind: KindWarden, DesiredState: DesiredStateUninstall})
		reconcileTestOnline(t, api, "m-doomed", "")
		got := api.reconcileOne(reconcileTestRow(t, d, "m-doomed"), newReconcileState(), reconcileTestNow)
		reconcileTestWantDecision(t, got, reconcileDecision{
			Command: reconcileCmdUninstall, MemberID: "m-doomed",
			Reason: "uninstall: desired_state uninstall, warden online — dispatch uninstall",
			State: reconcileState{
				Phase: reconcilePhaseStopping, LastCommand: reconcileCmdUninstall,
				LastCommandAt: reconcileTestNow,
			},
		})
		frame, _ := buildTargetFrame(reconcileCmdUninstall, "m-doomed")
		queued := api.hub.DrainWardenCommands("m-doomed")
		if !reflect.DeepEqual(queued, []wardenCmd{{Subject: "m-doomed", Frame: frame}}) {
			t.Fatalf("queued = %+v", queued)
		}
	})

	t.Run("a STOP the warden cannot take is downgraded to a no-op that keeps the prior state so the next tick re-dispatches", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-away", Name: "Away", Kind: KindWarden})
		reconcileTestPut(t, d, Member{
			ID: "leaving", Name: "Leaving", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOffline, DesiredMachineID: "m-away",
		})
		reconcileTestOnline(t, api, "leaving", "m-away")
		prior := reconcileState{Phase: reconcilePhaseStopping, LastCommand: reconcileCmdNone, StopDeadline: reconcileTestNow - 1}
		timed := api.reconcileCfg
		timed.SoftOffboardGrace = 0
		api.reconcileCfg = timed
		out := hubTestStderr(t, func() {
			got := api.reconcileOne(reconcileTestRow(t, d, "leaving"), prior, reconcileTestNow)
			reconcileTestWantDecision(t, got, reconcileDecision{
				Command: reconcileCmdNone, MemberID: "leaving",
				Reason: "robust stop: grace elapsed, still online",
				State:  prior, DispatchUnlanded: true,
				// The downgrade clears the command; the STOP flavour rides out unchanged.
				StopKind: stopKindWinddown,
			})
		})
		want := "[reconcile] leaving: target warden \"m-away\" NOT reachable (no live SSE downstream) — " +
			"fail-closed, not dispatching, will retry when the warden connects\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if got := api.hub.PendingWardenCommands("m-away"); got != 0 {
			t.Fatalf("a refused dispatch queued %d frame(s)", got)
		}
	})
}

func TestReconcileTickMemberLocked(t *testing.T) {
	t.Run("one member's tick logs its decision, stores the next state and stamps the wake anchor a landed START earns", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestOnline(t, api, "m-box", "")
		reconcileTestPut(t, d, Member{ID: "runner", Name: "Runner", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		before := reconcileTestRow(t, d, "runner")
		out := hubTestStderr(t, func() {
			api.reconcileMu.Lock()
			defer api.reconcileMu.Unlock()
			got := api.reconcileTickMemberLocked(before, reconcileTestNow)
			if got.Command != reconcileCmdStart {
				t.Fatalf("command = %q, want start", got.Command)
			}
		})
		want := "[reconcile] runner: desired=online command=start — spawn: desired_state online, no live session\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		wantState := reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart,
			LastCommandAt: reconcileTestNow, OfflineSince: reconcileTestNow,
		}
		if api.lifecycleStates["runner"] != wantState {
			t.Fatalf("stored state:\n got %+v\nwant %+v", api.lifecycleStates["runner"], wantState)
		}
		wantRow := before
		wantRow.WakingSince = reconcileTestNow
		wantRow.Runtime = RuntimeClaude
		reconcileTestWantRow(t, d, "runner", wantRow)
		if got := api.hub.PendingWardenCommandsFor("m-box", "runner"); got != 1 {
			t.Fatalf("the warden's per-subject backlog = %d, want the one START", got)
		}
	})

	t.Run("a stall the decider names is stamped onto the row the cockpit reads", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "stalled", Name: "Stalled", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		api.lifecycleStates["stalled"] = reconcileState{
			Phase: reconcilePhaseBackoff, LastCommand: reconcileCmdNone, BackoffUntil: reconcileTestNow + 5,
		}
		before := reconcileTestRow(t, d, "stalled")
		out := hubTestStderr(t, func() {
			api.reconcileMu.Lock()
			defer api.reconcileMu.Unlock()
			api.reconcileTickMemberLocked(before, reconcileTestNow)
		})
		want := "[reconcile] stalled: desired=online command=none — backoff: awaiting retry window\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		wantRow := before
		wantRow.LastOp = reconcileCmdStart
		wantRow.LastOpOK = reconcileTestFalse()
		wantRow.LastOpAt = reconcileTestNow
		wantRow.LastOpReason = spawnReasonBackoff + ": the last start did not come up, so the next " +
			"attempt is waiting out a back-off window — nothing is wrong with the button you " +
			"pressed, the retry has not come round yet"
		reconcileTestWantRow(t, d, "stalled", wantRow)
	})

	t.Run("a wake lapse keeps the execution diagnosis: the same tick's back-off code is not allowed to overwrite it", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "lapsed", Name: "Lapsed", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box", WakingSince: reconcileTestNow - 200,
		})
		api.lifecycleStates["lapsed"] = reconcileState{
			Phase: reconcilePhaseStarting, LastCommand: reconcileCmdStart, LastCommandAt: reconcileTestNow - 121,
		}
		before := reconcileTestRow(t, d, "lapsed")
		hubTestStderr(t, func() {
			api.reconcileMu.Lock()
			defer api.reconcileMu.Unlock()
			got := api.reconcileTickMemberLocked(before, reconcileTestNow)
			if !got.StartTimedOut {
				t.Fatalf("the tick must observe the lapse: %+v", got)
			}
		})
		wantRow := before
		wantRow.WakingSince = 0
		wantRow.LastOp = reconcileCmdStart
		wantRow.LastOpOK = reconcileTestFalse()
		wantRow.LastOpAt = reconcileTestNow
		wantRow.LastOpReason = wakeTimeoutReasonCode + ": the START was dispatched but the agent " +
			"never came online within the start window — check that claude runs and is logged " +
			"in on the target machine (warden log: ocwarden.out.log)"
		reconcileTestWantRow(t, d, "lapsed", wantRow)
	})
}

func TestArmDecidedHandover(t *testing.T) {
	t.Run("a relocation decision opens the wind-down epoch on the member's row and says so", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "moving", Name: "Moving", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		reconcileTestOnline(t, api, "moving", "m-old")
		before := reconcileTestRow(t, d, "moving")
		out := hubTestStderr(t, func() {
			api.armDecidedHandover("moving", reconcileDecision{ArmHandoverOp: memberOpRelocate})
		})
		want := "[reconcile] recycle: relocate moving — wind-down opened " +
			"(collect on stopped-report or force-stop; no clock)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		got := reconcileTestRow(t, d, "moving")
		if got.RefocusOp != memberOpRelocate || got.RefocusSince <= 0 {
			t.Fatalf("the epoch was not opened: refocus_op=%q refocus_since=%v", got.RefocusOp, got.RefocusSince)
		}
		expected := before
		expected.RefocusOp = memberOpRelocate
		expected.RefocusSince = got.RefocusSince
		reconcileTestWantRow(t, d, "moving", expected)
	})

	t.Run("a decision that asks for no epoch writes nothing at all", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "quiet", Name: "Quiet", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		reconcileTestOnline(t, api, "quiet", "m-box")
		before := reconcileTestRow(t, d, "quiet")
		out := hubTestStderr(t, func() {
			api.armDecidedHandover("quiet", reconcileDecision{Command: reconcileCmdStart})
		})
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		reconcileTestWantRow(t, d, "quiet", before)
	})

	t.Run("a refusing gate — an offline member, a warden row, a removed row, a missing row — leaves the roster untouched", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "asleep", Name: "Asleep", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		reconcileTestPut(t, d, Member{ID: "left", Name: "Left", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, RosterStatus: RosterStatusRemoved})
		for _, id := range []string{"asleep", "m-box", "left"} {
			before := reconcileTestRow(t, d, id)
			out := hubTestStderr(t, func() {
				api.armDecidedHandover(id, reconcileDecision{ArmHandoverOp: memberOpRelocate})
			})
			if out != "" {
				t.Fatalf("%s: stderr = %q, want nothing", id, out)
			}
			reconcileTestWantRow(t, d, id, before)
		}
		api.armDecidedHandover("never-existed", reconcileDecision{ArmHandoverOp: memberOpRelocate})
		if row, _ := d.GetMember("never-existed"); row != nil {
			t.Fatalf("a missing member must not be created: %+v", row)
		}
	})
}

func TestNoteContextGateSkip(t *testing.T) {
	t.Run("the line names the gate and every gauge input the guards read, and the live-connection fact", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		reconcileTestOnline(t, api, "kip", "m-box")
		record := map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0}
		out := hubTestStderr(t, func() { api.noteContextGateSkip("kip", "offline", record, 20000) })
		want := "[reconcile] recycle: gate skip kip gate=offline pct=55 pct_ts=19900 " +
			"boot_ts=19000 boot_secs=1000.0 online=true\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
	})

	t.Run("a missing gauge renders every input as a dash rather than a number nobody measured", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		out := hubTestStderr(t, func() { api.noteContextGateSkip("kip", "no-actionable-pct", nil, 20000) })
		want := "[reconcile] recycle: gate skip kip gate=no-actionable-pct pct=- pct_ts=- " +
			"boot_ts=- boot_secs=- online=false\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
	})

	t.Run("the same actor on the same gate is silenced for five minutes, while a change of gate speaks immediately", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		record := map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0}
		line := func(gate string, secs string) string {
			return "[reconcile] recycle: gate skip kip gate=" + gate + " pct=55 pct_ts=19900 " +
				"boot_ts=19000 boot_secs=" + secs + " online=false\n"
		}
		if got := hubTestStderr(t, func() { api.noteContextGateSkip("kip", "offline", record, 20000) }); got != line("offline", "1000.0") {
			t.Fatalf("the first line:\n got %q\nwant %q", got, line("offline", "1000.0"))
		}
		if got := hubTestStderr(t, func() { api.noteContextGateSkip("kip", "offline", record, 20299) }); got != "" {
			t.Fatalf("inside the window the same gate must be silent, got %q", got)
		}
		if got := hubTestStderr(t, func() { api.noteContextGateSkip("kip", "boot-storm", record, 20010) }); got != line("boot-storm", "1010.0") {
			t.Fatalf("a change of gate must speak:\n got %q\nwant %q", got, line("boot-storm", "1010.0"))
		}
		if got := hubTestStderr(t, func() { api.noteContextGateSkip("kip", "boot-storm", record, 20310) }); got != line("boot-storm", "1310.0") {
			t.Fatalf("past the window the same gate speaks again:\n got %q\nwant %q", got, line("boot-storm", "1310.0"))
		}
	})

	t.Run("the throttle is per actor: a second member on the same gate is not silenced by the first", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		hubTestStderr(t, func() { api.noteContextGateSkip("kip", "offline", nil, 20000) })
		out := hubTestStderr(t, func() { api.noteContextGateSkip("mira", "offline", nil, 20001) })
		want := "[reconcile] recycle: gate skip mira gate=offline pct=- pct_ts=- boot_ts=- " +
			"boot_secs=- online=false\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
	})
}

func TestStampContextHighRecycle(t *testing.T) {
	t.Run("a live session over the handover line has an accelerated wind-down stamped on it, in the slice and on the row", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "hot", Name: "Hot", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		reconcileTestOnline(t, api, "hot", "m-box")
		api.gauge.Set("hot", map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0})
		before := reconcileTestRow(t, d, "hot")
		members := []Member{before}
		out := hubTestStderr(t, func() { api.stampContextHighRecycle(members, 20000) })
		if out != "[reconcile] recycle: auto-stamp refocus_since for hot (claude, context_high)\n" {
			t.Fatalf("stderr = %q", out)
		}
		if members[0].RefocusSince != 20000 || members[0].RefocusOp != refocusOpContextHigh {
			t.Fatalf("the slice member must carry the marker for this same tick: %+v", members[0])
		}
		want := before
		want.RefocusSince = 20000
		want.RefocusOp = refocusOpContextHigh
		reconcileTestWantRow(t, d, "hot", want)
	})

	t.Run("a live session over only the first line gets a plain 停止 instead", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "warm", Name: "Warm", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		reconcileTestOnline(t, api, "warm", "m-box")
		api.gauge.Set("warm", map[string]any{"context_pct": 45.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0})
		before := reconcileTestRow(t, d, "warm")
		out := hubTestStderr(t, func() { api.stampContextHighRecycle([]Member{before}, 20000) })
		if out != "[reconcile] recycle: auto-stamp refocus_since for warm (claude, context_notice)\n" {
			t.Fatalf("stderr = %q", out)
		}
		want := before
		want.RefocusSince = 20000
		want.RefocusOp = refocusOpContextNotice
		reconcileTestWantRow(t, d, "warm", want)
	})

	t.Run("a notice epoch crossing the second line is promoted in place with a fresh deadline, keeping the close-out anchors", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "promoted", Name: "Promoted", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box",
			RefocusSince: 19500, RefocusOp: refocusOpContextNotice, StoppingSince: 19600,
		})
		reconcileTestOnline(t, api, "promoted", "m-box")
		api.gauge.Set("promoted", map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0})
		before := reconcileTestRow(t, d, "promoted")
		out := hubTestStderr(t, func() { api.stampContextHighRecycle([]Member{before}, 20000) })
		if out != "[reconcile] recycle: promoted promoted to context_high (claude)\n" {
			t.Fatalf("stderr = %q", out)
		}
		want := before
		want.RefocusSince = 20000
		want.RefocusOp = refocusOpContextHigh
		reconcileTestWantRow(t, d, "promoted", want)
	})

	t.Run("each gate names itself on stderr and writes nothing: a stale gauge, a fresh boot over the line, and an offline member", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "stale", Name: "Stale", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		reconcileTestPut(t, d, Member{ID: "booting", Name: "Booting", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		reconcileTestPut(t, d, Member{ID: "gone", Name: "Gone", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		api.gauge.Set("booting", map[string]any{"context_pct": 55.0, "context_pct_ts": 19990.0, "boot_ts": 19950.0})
		api.gauge.Set("gone", map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0})
		for _, c := range []struct {
			id   string
			want string
		}{
			{"stale", "[reconcile] recycle: gate skip stale gate=no-actionable-pct pct=- pct_ts=- boot_ts=- boot_secs=- online=false\n"},
			{"booting", "[reconcile] recycle: gate skip booting gate=boot-storm pct=55 pct_ts=19990 boot_ts=19950 boot_secs=50.0 online=false\n"},
			{"gone", "[reconcile] recycle: gate skip gone gate=offline pct=55 pct_ts=19900 boot_ts=19000 boot_secs=1000.0 online=false\n"},
		} {
			before := reconcileTestRow(t, d, c.id)
			out := hubTestStderr(t, func() { api.stampContextHighRecycle([]Member{before}, 20000) })
			if out != c.want {
				t.Fatalf("%s stderr:\n got %q\nwant %q", c.id, out, c.want)
			}
			reconcileTestWantRow(t, d, c.id, before)
		}
	})

	t.Run("a session that already reported it is finished, and one the server no longer wants online, are skipped silently", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "finished", Name: "Finished", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box", StoppedSince: 19500,
		})
		reconcileTestPut(t, d, Member{
			ID: "leaving", Name: "Leaving", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOffline, DesiredMachineID: "m-box",
		})
		reconcileTestOnline(t, api, "finished", "m-box")
		reconcileTestOnline(t, api, "leaving", "m-box")
		gauge := map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0}
		api.gauge.Set("finished", gauge)
		api.gauge.Set("leaving", gauge)
		for _, id := range []string{"finished", "leaving"} {
			before := reconcileTestRow(t, d, id)
			out := hubTestStderr(t, func() { api.stampContextHighRecycle([]Member{before}, 20000) })
			if out != "" {
				t.Fatalf("%s stderr = %q, want nothing", id, out)
			}
			reconcileTestWantRow(t, d, id, before)
		}
	})

	t.Run("an epoch that is not a promotable notice is its own cooldown and is left where it is", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "winding", Name: "Winding", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box",
			RefocusSince: 19500, RefocusOp: memberOpRelocate,
		})
		reconcileTestOnline(t, api, "winding", "m-box")
		api.gauge.Set("winding", map[string]any{"context_pct": 55.0, "context_pct_ts": 19900.0, "boot_ts": 19000.0})
		before := reconcileTestRow(t, d, "winding")
		out := hubTestStderr(t, func() { api.stampContextHighRecycle([]Member{before}, 20000) })
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		reconcileTestWantRow(t, d, "winding", before)
	})
}

func TestStampTokenExpiryWinddown(t *testing.T) {
	t.Run("a live session inside the last hour of its token gets a plain 停止 naming the derived expiry", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		ttl := float64(api.agentTokenTTLValue())
		reconcileTestPut(t, d, Member{
			ID: "expiring", Name: "Expiring", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box",
			SessionBootTS: reconcileTestNow - ttl + 1800,
		})
		reconcileTestOnline(t, api, "expiring", "m-box")
		before := reconcileTestRow(t, d, "expiring")
		out := hubTestStderr(t, func() { api.stampTokenExpiryWinddown([]Member{before}, reconcileTestNow) })
		want := "[reconcile] recycle: token-expiry 停止 for expiring (token estimated to expire " +
			"at 1700001800, lead 3600s)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		wantRow := before
		wantRow.RefocusSince = reconcileTestNow
		wantRow.RefocusOp = refocusOpTokenExpiry
		reconcileTestWantRow(t, d, "expiring", wantRow)
	})

	t.Run("a session outside the lead, one already past the derived expiry, and one with no derivable expiry are all left alone", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		ttl := float64(api.agentTokenTTLValue())
		for _, c := range []struct {
			id   string
			boot float64
			kind string
		}{
			{"early", reconcileTestNow, KindStaff},
			{"expired", reconcileTestNow - ttl - 10, KindStaff},
			{"unanchored", 0, KindStaff},
		} {
			reconcileTestPut(t, d, Member{
				ID: c.id, Name: c.id, Kind: c.kind, RoleKey: "assistant",
				DesiredState: DesiredStateOnline, DesiredMachineID: "m-box", SessionBootTS: c.boot,
			})
			reconcileTestOnline(t, api, c.id, "m-box")
			before := reconcileTestRow(t, d, c.id)
			out := hubTestStderr(t, func() { api.stampTokenExpiryWinddown([]Member{before}, reconcileTestNow) })
			if out != "" {
				t.Fatalf("%s: stderr = %q, want nothing", c.id, out)
			}
			reconcileTestWantRow(t, d, c.id, before)
		}
	})

	t.Run("an epoch already open, a session that reported finished, an offline member and a desired-offline member are each skipped", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		ttl := float64(api.agentTokenTTLValue())
		boot := reconcileTestNow - ttl + 1800
		for _, c := range []struct {
			id      string
			seed    Member
			connect bool
		}{
			{"winding", Member{RefocusSince: reconcileTestNow - 100, RefocusOp: "refocus"}, true},
			{"finished", Member{StoppedSince: reconcileTestNow - 100}, true},
			{"offline", Member{}, false},
			{"leaving", Member{DesiredState: DesiredStateOffline}, true},
		} {
			seed := c.seed
			seed.ID = c.id
			seed.Name = c.id
			seed.Kind = KindStaff
			seed.RoleKey = "assistant"
			seed.DesiredMachineID = "m-box"
			seed.SessionBootTS = boot
			if seed.DesiredState == "" {
				seed.DesiredState = DesiredStateOnline
			}
			reconcileTestPut(t, d, seed)
			if c.connect {
				reconcileTestOnline(t, api, c.id, "m-box")
			}
			api.gauge.Set(c.id, map[string]any{"boot_ts": reconcileTestNow - 200})
			before := reconcileTestRow(t, d, c.id)
			out := hubTestStderr(t, func() { api.stampTokenExpiryWinddown([]Member{before}, reconcileTestNow) })
			if out != "" {
				t.Fatalf("%s: stderr = %q, want nothing", c.id, out)
			}
			reconcileTestWantRow(t, d, c.id, before)
		}
	})
}

func TestConsumeUninstallIntentOnOffline(t *testing.T) {
	t.Run("an offline warden still carrying the intent has converged: the intent is spent and the record kept", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-doomed", Name: "Doomed", Kind: KindWarden, DesiredState: DesiredStateUninstall})
		before := reconcileTestRow(t, d, "m-doomed")
		out := hubTestStderr(t, func() { api.consumeUninstallIntentOnOffline([]Member{before}) })
		want := "[reconcile] uninstall: consumed one-shot intent for offline warden m-doomed " +
			"(desired_state → offline; record kept)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		wantRow := before
		wantRow.DesiredState = DesiredStateOffline
		reconcileTestWantRow(t, d, "m-doomed", wantRow)
	})

	t.Run("a warden still online belongs to the dispatch arm, and a non-warden row is never this pass's business", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-live", Name: "Live", Kind: KindWarden, DesiredState: DesiredStateUninstall})
		reconcileTestOnline(t, api, "m-live", "")
		reconcileTestPut(t, d, Member{ID: "staffer", Name: "Staffer", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateUninstall})
		reconcileTestPut(t, d, Member{ID: "m-idle", Name: "Idle", Kind: KindWarden, DesiredState: DesiredStateOffline})
		for _, id := range []string{"m-live", "staffer", "m-idle"} {
			before := reconcileTestRow(t, d, id)
			out := hubTestStderr(t, func() { api.consumeUninstallIntentOnOffline([]Member{before}) })
			if out != "" {
				t.Fatalf("%s: stderr = %q, want nothing", id, out)
			}
			reconcileTestWantRow(t, d, id, before)
		}
	})
}

func TestConsumeUninstallOnDisconnect(t *testing.T) {
	t.Run("the disconnect edge consumes the intent immediately rather than waiting a cadence window", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-doomed", Name: "Doomed", Kind: KindWarden, DesiredState: DesiredStateUninstall})
		before := reconcileTestRow(t, d, "m-doomed")
		out := hubTestStderr(t, func() { api.consumeUninstallOnDisconnect("m-doomed") })
		want := "[reconcile] uninstall: consumed one-shot intent on warden m-doomed disconnect " +
			"(desired_state → offline; record kept)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		wantRow := before
		wantRow.DesiredState = DesiredStateOffline
		reconcileTestWantRow(t, d, "m-doomed", wantRow)
	})

	t.Run("a warden that still holds a stream, a non-warden, a warden with no intent and an id with no row all change nothing", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-live", Name: "Live", Kind: KindWarden, DesiredState: DesiredStateUninstall})
		reconcileTestOnline(t, api, "m-live", "")
		reconcileTestPut(t, d, Member{ID: "staffer", Name: "Staffer", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateUninstall})
		reconcileTestPut(t, d, Member{ID: "m-idle", Name: "Idle", Kind: KindWarden, DesiredState: DesiredStateOffline})
		for _, id := range []string{"m-live", "staffer", "m-idle"} {
			before := reconcileTestRow(t, d, id)
			out := hubTestStderr(t, func() { api.consumeUninstallOnDisconnect(id) })
			if out != "" {
				t.Fatalf("%s: stderr = %q, want nothing", id, out)
			}
			reconcileTestWantRow(t, d, id, before)
		}
		api.consumeUninstallOnDisconnect("never-existed")
		if row, _ := d.GetMember("never-existed"); row != nil {
			t.Fatalf("a missing member must not be created: %+v", row)
		}
	})

	t.Run("with the producer disabled the intent is left standing — this is one of the writes --no-reconcile owns", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		api.noReconcile = true
		reconcileTestPut(t, d, Member{ID: "m-doomed", Name: "Doomed", Kind: KindWarden, DesiredState: DesiredStateUninstall})
		before := reconcileTestRow(t, d, "m-doomed")
		out := hubTestStderr(t, func() { api.consumeUninstallOnDisconnect("m-doomed") })
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		reconcileTestWantRow(t, d, "m-doomed", before)
		api.noReconcile = false
		hubTestStderr(t, func() { api.consumeUninstallOnDisconnect("m-doomed") })
		wantRow := before
		wantRow.DesiredState = DesiredStateOffline
		reconcileTestWantRow(t, d, "m-doomed", wantRow)
	})
}

func TestClearStaleStoppingOnOnline(t *testing.T) {
	t.Run("a desired-online member observed online and quiet for the whole window has its stopping anchor cleared", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "survived", Name: "Survived", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, StoppingSince: reconcileTestNow - 600,
		})
		reconcileTestOnline(t, api, "survived", "m-box")
		before := reconcileTestRow(t, d, "survived")
		out := hubTestStderr(t, func() { api.clearStaleStoppingOnOnline([]Member{before}, reconcileTestNow) })
		want := "[reconcile] revive: auto-cleared stale stopping_since on observed-online survived " +
			"(survived stop / SSE reconnect)\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		wantRow := before
		wantRow.StoppingSince = 0
		reconcileTestWantRow(t, d, "survived", wantRow)
	})

	t.Run("a member still filing context reports is still saying something, so its close-out is left visible", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "working", Name: "Working", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, StoppingSince: reconcileTestNow - 6000,
		})
		reconcileTestOnline(t, api, "working", "m-box")
		api.gauge.Set("working", map[string]any{"ts": reconcileTestNow - 10})
		before := reconcileTestRow(t, d, "working")
		out := hubTestStderr(t, func() { api.clearStaleStoppingOnOnline([]Member{before}, reconcileTestNow) })
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		reconcileTestWantRow(t, d, "working", before)
	})

	t.Run("a desired-offline wind-down, a member with no anchor and an offline member are all left alone", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "leaving", Name: "Leaving", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOffline, StoppingSince: reconcileTestNow - 6000,
		})
		reconcileTestOnline(t, api, "leaving", "m-box")
		reconcileTestPut(t, d, Member{ID: "clean", Name: "Clean", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		reconcileTestOnline(t, api, "clean", "m-box")
		reconcileTestPut(t, d, Member{
			ID: "stopped", Name: "Stopped", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, StoppingSince: reconcileTestNow - 6000,
		})
		for _, id := range []string{"leaving", "clean", "stopped"} {
			before := reconcileTestRow(t, d, id)
			out := hubTestStderr(t, func() { api.clearStaleStoppingOnOnline([]Member{before}, reconcileTestNow) })
			if out != "" {
				t.Fatalf("%s: stderr = %q, want nothing", id, out)
			}
			reconcileTestWantRow(t, d, id, before)
		}
	})
}

func TestRunReconcileTick(t *testing.T) {
	t.Run("one tick runs the roster passes, names its candidate count and decides each candidate in name order", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestOnline(t, api, "m-box", "")
		reconcileTestPut(t, d, Member{ID: "runner", Name: "Runner", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		before := reconcileTestRow(t, d, "runner")
		out := hubTestStderr(t, func() { api.runReconcileTick(reconcileTestNow) })
		want := "[reconcile] recycle: gate skip kip gate=no-actionable-pct pct=- pct_ts=- boot_ts=- boot_secs=- online=false\n" +
			"[reconcile] recycle: gate skip mira gate=no-actionable-pct pct=- pct_ts=- boot_ts=- boot_secs=- online=false\n" +
			"[reconcile] recycle: gate skip runner gate=no-actionable-pct pct=- pct_ts=- boot_ts=- boot_secs=- online=false\n" +
			"[reconcile] tick: 3 candidate(s)\n" +
			"[reconcile] kip: desired=offline command=none — offline: converged\n" +
			"[reconcile] mira: desired=offline command=none — offline: converged\n" +
			"[reconcile] runner: desired=online command=start — spawn: desired_state online, no live session\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if got := api.hub.PendingWardenCommandsFor("m-box", "runner"); got != 1 {
			t.Fatalf("the START backlog for runner = %d, want 1", got)
		}
		wantRow := before
		wantRow.WakingSince = reconcileTestNow
		wantRow.Runtime = RuntimeClaude
		reconcileTestWantRow(t, d, "runner", wantRow)
		for _, id := range []string{"kip", "mira"} {
			if st := api.lifecycleStates[id]; st.Phase != reconcilePhaseOffline {
				t.Fatalf("%s state = %+v, want the converged offline state", id, st)
			}
		}
	})

	t.Run("a plain warden is not a candidate while one carrying the uninstall intent is, and the intent is consumed while it is offline", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-doomed", Name: "Doomed", Kind: KindWarden, DesiredState: DesiredStateUninstall})
		out := hubTestStderr(t, func() { api.runReconcileTick(reconcileTestNow) })
		if !strings.Contains(out, "[reconcile] tick: 3 candidate(s)\n") {
			t.Fatalf("candidate count line missing from:\n%s", out)
		}
		if !strings.Contains(out, "[reconcile] uninstall: consumed one-shot intent for offline warden m-doomed") {
			t.Fatalf("the intent was not consumed:\n%s", out)
		}
		if !strings.Contains(out, "[reconcile] m-doomed: desired=offline command=none — offline: converged\n") {
			t.Fatalf("the uninstall warden must still be decided this tick:\n%s", out)
		}
		if strings.Contains(out, "m-box:") || strings.Contains(out, "m-server-self:") {
			t.Fatalf("a plain warden must not be a candidate:\n%s", out)
		}
		if got := reconcileTestRow(t, d, "m-doomed").DesiredState; got != DesiredStateOffline {
			t.Fatalf("desired_state = %q, want the consumed intent", got)
		}
	})

	t.Run("a removed member is filtered out before the decide pass and is neither logged nor written", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "left", Name: "Left", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, RosterStatus: RosterStatusRemoved,
		})
		before := reconcileTestRow(t, d, "left")
		out := hubTestStderr(t, func() { api.runReconcileTick(reconcileTestNow) })
		if strings.Contains(out, "left") {
			t.Fatalf("a removed member reached the tick:\n%s", out)
		}
		if !strings.Contains(out, "[reconcile] tick: 2 candidate(s)\n") {
			t.Fatalf("candidate count line:\n%s", out)
		}
		reconcileTestWantRow(t, d, "left", before)
	})

	t.Run("a fault inside the tick is caught and named rather than raised into the cadence loop", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		api.lifecycleStates = nil
		out := hubTestStderr(t, func() { api.runReconcileTick(reconcileTestNow) })
		if !strings.Contains(out, "[reconcile] tick FAULT: ") {
			t.Fatalf("the fault was not caught and logged:\n%s", out)
		}
	})
}

func TestReconcileMemberNow(t *testing.T) {
	t.Run("the event-driven tick decides one member immediately and shares the cadence's state store", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestOnline(t, api, "m-box", "")
		reconcileTestPut(t, d, Member{ID: "runner", Name: "Runner", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		var got reconcileDecision
		out := hubTestStderr(t, func() { got = api.reconcileMemberNow("runner") })
		if got.Command != reconcileCmdStart || got.Reason != "spawn: desired_state online, no live session" {
			t.Fatalf("decision = %+v", got)
		}
		if !strings.Contains(out, "[reconcile] instant tick: member runner\n") {
			t.Fatalf("stderr:\n%s", out)
		}
		if !strings.Contains(out, "[reconcile] runner: desired=online command=start — spawn: desired_state online, no live session\n") {
			t.Fatalf("stderr:\n%s", out)
		}
		if st := api.lifecycleStates["runner"]; st.LastCommand != reconcileCmdStart {
			t.Fatalf("the shared store did not record the dispatch: %+v", st)
		}
		if n := api.hub.PendingWardenCommandsFor("m-box", "runner"); n != 1 {
			t.Fatalf("the START backlog = %d, want 1", n)
		}
	})

	t.Run("an unlanded move is reported back to the caller instead of passing as a silent success", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "nowhere", Name: "Nowhere", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline})
		var got reconcileDecision
		hubTestStderr(t, func() { got = api.reconcileMemberNow("nowhere") })
		if got.Command != reconcileCmdNone || !got.DispatchUnlanded || got.Reason != "no machine selected" {
			t.Fatalf("decision = %+v", got)
		}
	})

	t.Run("a missing member, a removed one and a warden the member FSM does not drive each yield the zero decision", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "left", Name: "Left", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, RosterStatus: RosterStatusRemoved,
		})
		for _, id := range []string{"never-existed", "left", "m-box"} {
			before, _ := d.GetMember(id)
			var got reconcileDecision
			out := hubTestStderr(t, func() { got = api.reconcileMemberNow(id) })
			if !reflect.DeepEqual(got, reconcileDecision{}) {
				t.Fatalf("%s: decision = %+v, want the zero decision", id, got)
			}
			if out != "" {
				t.Fatalf("%s: stderr = %q, want nothing", id, out)
			}
			if before != nil {
				reconcileTestWantRow(t, d, id, *before)
			}
			if _, seen := api.lifecycleStates[id]; seen {
				t.Fatalf("%s must not get a store entry", id)
			}
		}
	})

	t.Run("with the producer disabled nothing is decided, dispatched or stored", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		api.noReconcile = true
		reconcileTestOnline(t, api, "m-box", "")
		reconcileTestPut(t, d, Member{ID: "runner", Name: "Runner", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		before := reconcileTestRow(t, d, "runner")
		var got reconcileDecision
		out := hubTestStderr(t, func() { got = api.reconcileMemberNow("runner") })
		if !reflect.DeepEqual(got, reconcileDecision{}) || out != "" {
			t.Fatalf("decision = %+v stderr = %q", got, out)
		}
		reconcileTestWantRow(t, d, "runner", before)
		if n := api.hub.PendingWardenCommands("m-box"); n != 0 {
			t.Fatalf("the warden queue holds %d frame(s)", n)
		}
	})
}

func TestNoteRobustStopDispatched(t *testing.T) {
	t.Run("the marker is armed on a member with no store entry yet, leaving the rest of a fresh state untouched", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		api.noteRobustStopDispatched("kip", reconcileTestNow)
		want := newReconcileState()
		want.RobustStopPendingAt = reconcileTestNow
		if got := api.lifecycleStates["kip"]; got != want {
			t.Fatalf("state:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an existing entry keeps everything else and only the marker moves to the newest dispatch", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		existing := reconcileState{
			Phase: reconcilePhaseOnline, Attempts: 2, BackoffUntil: 5, CircuitOpen: true,
			CircuitCooldownUntil: 9, LastCommand: reconcileCmdStart, LastCommandAt: 3,
			StopDeadline: 4, RobustStopPendingAt: 1, OfflineSince: 2,
		}
		api.lifecycleStates["kip"] = existing
		api.noteRobustStopDispatched("kip", reconcileTestNow)
		want := existing
		want.RobustStopPendingAt = reconcileTestNow
		if got := api.lifecycleStates["kip"]; got != want {
			t.Fatalf("state:\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestDispatchRobustStopNow(t *testing.T) {
	t.Run("the STOP goes to the warden of the machine the session is on, the retry is armed and the boot anchor is dropped", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-old", Name: "Old", Kind: KindWarden})
		reconcileTestOnline(t, api, "m-old", "")
		reconcileTestPut(t, d, Member{
			ID: "collect", Name: "Collect", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box", SessionBootTS: reconcileTestNow - 50,
		})
		reconcileTestOnline(t, api, "collect", "m-old")
		before := reconcileTestRow(t, d, "collect")
		out := hubTestStderr(t, func() { api.dispatchRobustStopNow("collect") })
		if out != "" {
			t.Fatalf("a landed dispatch logs nothing, got %q", out)
		}
		frame, _ := buildTargetFrame(reconcileCmdStop, "collect")
		if got := api.hub.DrainWardenCommands("m-old"); !reflect.DeepEqual(got, []wardenCmd{{Subject: "collect", Frame: frame}}) {
			t.Fatalf("the old machine's queue = %+v", got)
		}
		if n := api.hub.PendingWardenCommands("m-box"); n != 0 {
			t.Fatalf("the pinned machine must not be addressed, it holds %d frame(s)", n)
		}
		if got := api.lifecycleStates["collect"].RobustStopPendingAt; got <= 0 {
			t.Fatalf("the at-least-once retry was not armed: %v", got)
		}
		wantRow := before
		wantRow.SessionBootTS = 0
		reconcileTestWantRow(t, d, "collect", wantRow)
	})

	t.Run("an unreachable warden still arms the retry — that is the case the backstop exists for", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{
			ID: "stranded", Name: "Stranded", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box",
		})
		out := hubTestStderr(t, func() { api.dispatchRobustStopNow("stranded") })
		want := "[reconcile] stranded: target warden \"m-box\" NOT reachable (no live SSE downstream) — " +
			"fail-closed, not dispatching, will retry when the warden connects\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if n := api.hub.PendingWardenCommands("m-box"); n != 0 {
			t.Fatalf("nothing may be queued, got %d frame(s)", n)
		}
		if got := api.lifecycleStates["stranded"].RobustStopPendingAt; got <= 0 {
			t.Fatalf("the retry must be armed anyway: %v", got)
		}
	})

	t.Run("with the producer disabled nothing is dispatched and no retry is armed", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		api.noReconcile = true
		reconcileTestOnline(t, api, "m-box", "")
		reconcileTestPut(t, d, Member{
			ID: "kept", Name: "Kept", Kind: KindStaff, RoleKey: "assistant",
			DesiredState: DesiredStateOnline, DesiredMachineID: "m-box", SessionBootTS: reconcileTestNow,
		})
		before := reconcileTestRow(t, d, "kept")
		out := hubTestStderr(t, func() { api.dispatchRobustStopNow("kept") })
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		if n := api.hub.PendingWardenCommands("m-box"); n != 0 {
			t.Fatalf("the warden queue holds %d frame(s)", n)
		}
		if _, seen := api.lifecycleStates["kept"]; seen {
			t.Fatalf("no retry may be armed")
		}
		reconcileTestWantRow(t, d, "kept", before)
	})
}

func TestConnectionIsTheGenuineArticle(t *testing.T) {
	t.Run("a claim equal to the member's pinned machine is the 正身, and any other claim is a wanderer", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		m := Member{ID: "kip", Kind: KindStaff, DesiredMachineID: "m-box"}
		if !api.connectionIsTheGenuineArticle(m, "m-box") {
			t.Fatalf("the pinned machine's claim must be the 正身")
		}
		if api.connectionIsTheGenuineArticle(m, "m-other") {
			t.Fatalf("another machine's claim must not be the 正身")
		}
	})

	t.Run("an unverifiable pairing — a blank claim, or a member with no expected machine — is fail-safe false", func(t *testing.T) {
		api, _ := reconcileTestServer(t)
		if api.connectionIsTheGenuineArticle(Member{ID: "kip", Kind: KindStaff, DesiredMachineID: "m-box"}, "") {
			t.Fatalf("a blank claim must never verify")
		}
		if api.connectionIsTheGenuineArticle(Member{ID: "kip", Kind: KindStaff}, "m-box") {
			t.Fatalf("a member with no pin must never verify")
		}
		if api.connectionIsTheGenuineArticle(Member{ID: "ow-1", Kind: KindOutsource}, "m-box") {
			t.Fatalf("a worker the server never dispatched must never verify")
		}
		if !api.connectionIsTheGenuineArticle(Member{ID: "ow-1", Kind: KindOutsource, DesiredMachineID: "m-box"}, "m-box") {
			t.Fatalf("a worker with a concrete pin verifies on that pin")
		}
	})
}

func TestDispatchIdentitySweepNow(t *testing.T) {
	t.Run("every OTHER reachable warden is told to stop the residual session, and the confirmed machine is never swept", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-other", Name: "Other", Kind: KindWarden})
		reconcileTestOnline(t, api, "m-box", "")
		reconcileTestOnline(t, api, "m-other", "")
		reconcileTestPut(t, d, Member{ID: "twin", Name: "Twin", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		out := hubTestStderr(t, func() {
			api.reconcileMu.Lock()
			defer api.reconcileMu.Unlock()
			api.dispatchIdentitySweepNow("twin", "m-box", reconcileTestNow)
		})
		want := "[reconcile] identity-sweep: twin confirmed on desired machine m-box — " +
			"robust stop residual session on m-other\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		frame, _ := buildTargetFrame(reconcileCmdStop, "twin")
		if got := api.hub.DrainWardenCommands("m-other"); !reflect.DeepEqual(got, []wardenCmd{{Subject: "twin", Frame: frame}}) {
			t.Fatalf("the other machine's queue = %+v", got)
		}
		if n := api.hub.PendingWardenCommands("m-box"); n != 0 {
			t.Fatalf("the confirmed machine holds %d frame(s), want none", n)
		}
		if api.identitySweepAt["twin"] != reconcileTestNow {
			t.Fatalf("the dedupe stamp = %v", api.identitySweepAt["twin"])
		}
	})

	t.Run("a sweep inside the dedupe window is not re-broadcast, and one past it is", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-other", Name: "Other", Kind: KindWarden})
		reconcileTestOnline(t, api, "m-other", "")
		reconcileTestPut(t, d, Member{ID: "twin", Name: "Twin", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		sweep := func(now float64) string {
			return hubTestStderr(t, func() {
				api.reconcileMu.Lock()
				defer api.reconcileMu.Unlock()
				api.dispatchIdentitySweepNow("twin", "m-box", now)
			})
		}
		if sweep(reconcileTestNow) == "" {
			t.Fatalf("the first sweep must broadcast")
		}
		api.hub.DrainWardenCommands("m-other")
		if got := sweep(reconcileTestNow + 89); got != "" {
			t.Fatalf("inside the dedupe window: stderr = %q, want nothing", got)
		}
		if n := api.hub.PendingWardenCommands("m-other"); n != 0 {
			t.Fatalf("a deduped sweep queued %d frame(s)", n)
		}
		if got := sweep(reconcileTestNow + 90); got == "" {
			t.Fatalf("past the dedupe window the sweep must broadcast again")
		}
		if n := api.hub.PendingWardenCommands("m-other"); n != 1 {
			t.Fatalf("the re-broadcast queued %d frame(s), want 1", n)
		}
	})

	t.Run("an offline warden, a removed warden, a non-warden row, a blank member and a disabled producer all sweep nothing", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-asleep", Name: "Asleep", Kind: KindWarden})
		reconcileTestPut(t, d, Member{ID: "m-left", Name: "LeftBox", Kind: KindWarden, RosterStatus: RosterStatusRemoved})
		reconcileTestOnline(t, api, "m-left", "")
		reconcileTestOnline(t, api, "kip", "")
		reconcileTestPut(t, d, Member{ID: "twin", Name: "Twin", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		out := hubTestStderr(t, func() {
			api.reconcileMu.Lock()
			defer api.reconcileMu.Unlock()
			api.dispatchIdentitySweepNow("twin", "m-box", reconcileTestNow)
			api.dispatchIdentitySweepNow("", "m-box", reconcileTestNow)
		})
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		for _, id := range []string{"m-asleep", "m-left", "kip", "m-box"} {
			if n := api.hub.PendingWardenCommands(id); n != 0 {
				t.Fatalf("%s holds %d frame(s), want none", id, n)
			}
		}
		if _, stamped := api.identitySweepAt["twin"]; stamped {
			t.Fatalf("a sweep that reached nobody must not stamp the dedupe window")
		}
		api.noReconcile = true
		reconcileTestOnline(t, api, "m-asleep", "")
		hubTestStderr(t, func() {
			api.reconcileMu.Lock()
			defer api.reconcileMu.Unlock()
			api.dispatchIdentitySweepNow("twin", "m-box", reconcileTestNow)
		})
		if n := api.hub.PendingWardenCommands("m-asleep"); n != 0 {
			t.Fatalf("--no-reconcile must dispatch nothing, got %d frame(s)", n)
		}
	})
}

func TestIdentitySweepOnConnect(t *testing.T) {
	t.Run("the 正身 connecting on its expected machine sweeps every other reachable warden", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-other", Name: "Other", Kind: KindWarden})
		reconcileTestOnline(t, api, "m-other", "")
		reconcileTestPut(t, d, Member{ID: "twin", Name: "Twin", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		out := hubTestStderr(t, func() { api.identitySweepOnConnect("twin", "m-box") })
		want := "[reconcile] identity-sweep: twin confirmed on desired machine m-box — " +
			"robust stop residual session on m-other\n"
		if out != want {
			t.Fatalf("stderr:\n got %q\nwant %q", out, want)
		}
		if n := api.hub.PendingWardenCommandsFor("m-other", "twin"); n != 1 {
			t.Fatalf("the residual machine's backlog for twin = %d, want 1", n)
		}
	})

	t.Run("a wanderer whose claim is not its expected machine initiates nothing — it is the target of the real sweep", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-other", Name: "Other", Kind: KindWarden})
		reconcileTestOnline(t, api, "m-other", "")
		reconcileTestOnline(t, api, "m-box", "")
		reconcileTestPut(t, d, Member{ID: "twin", Name: "Twin", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		out := hubTestStderr(t, func() { api.identitySweepOnConnect("twin", "m-other") })
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		for _, id := range []string{"m-box", "m-other"} {
			if n := api.hub.PendingWardenCommands(id); n != 0 {
				t.Fatalf("%s holds %d frame(s), want none", id, n)
			}
		}
	})

	t.Run("a blank member or claim, a warden sub, a member the owner does not want online, and a disabled producer all sweep nothing", func(t *testing.T) {
		api, d := reconcileTestServer(t)
		reconcileTestPut(t, d, Member{ID: "m-other", Name: "Other", Kind: KindWarden})
		reconcileTestOnline(t, api, "m-other", "")
		reconcileTestPut(t, d, Member{ID: "resting", Name: "Resting", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOffline, DesiredMachineID: "m-box"})
		reconcileTestPut(t, d, Member{ID: "twin", Name: "Twin", Kind: KindStaff, RoleKey: "assistant", DesiredState: DesiredStateOnline, DesiredMachineID: "m-box"})
		out := hubTestStderr(t, func() {
			api.identitySweepOnConnect("", "m-box")
			api.identitySweepOnConnect("twin", "")
			api.identitySweepOnConnect("m-box", "m-box")
			api.identitySweepOnConnect("resting", "m-box")
			api.identitySweepOnConnect("never-existed", "m-box")
		})
		if out != "" {
			t.Fatalf("stderr = %q, want nothing", out)
		}
		if n := api.hub.PendingWardenCommands("m-other"); n != 0 {
			t.Fatalf("m-other holds %d frame(s), want none", n)
		}
		api.noReconcile = true
		hubTestStderr(t, func() { api.identitySweepOnConnect("twin", "m-box") })
		if n := api.hub.PendingWardenCommands("m-other"); n != 0 {
			t.Fatalf("--no-reconcile must sweep nothing, got %d frame(s)", n)
		}
		api.noReconcile = false
		hubTestStderr(t, func() { api.identitySweepOnConnect("twin", "m-box") })
		if n := api.hub.PendingWardenCommandsFor("m-other", "twin"); n != 1 {
			t.Fatalf("the contrast case must sweep, backlog = %d", n)
		}
	})
}
