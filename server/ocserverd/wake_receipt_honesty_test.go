package main

import (
	"errors"
	"strings"
	"testing"
)

// ── ① the wake receipts must name the log that actually carries warden lines ──
//
// THE FACT BEING GUARDED (measured on eva-m5, 2026-08-26):
//
//	ocwarden.err.log  "command reader" hits:        0   (4,756 lines, 0 timestamps)
//	ocwarden.out.log  "command reader" hits:  110,784
//
// and the code says why: every warden operational line goes through the `logf`
// closure in cli/ocwarden/main.go:879, which writes to the `out` io.Writer that
// main() hands realMain as os.Stdout (main.go:971). The launchd plist
// (cli/ocwarden/install.go:489) maps StandardOutPath → ocwarden.out.log and
// StandardErrorPath → ocwarden.err.log. So warden's account of itself —
// connect / reconnect / frame received / dispatch refused — lands in out.log,
// ALWAYS, and err.log carries only the two direct os.Stderr writers that remain
// (the `[ocwarden spawn]` env-layer diagnostics at transport.go:594 and the
// trash refusals at trash.go:222): env-var NAMES and nothing time-stamped.
//
// An owner sent to err.log by a wake_timeout receipt therefore reads a file that
// cannot contain the evidence, finds nothing wrong, and concludes the machine is
// fine. The receipt did not merely omit help — it actively spent the owner's
// attention on a file with no failure record in it.
//
// MUTANT (the acceptance for ①): put "ocwarden.err.log" back into any one of the
// four product-code sentences (reconcile.go:1040, reconcile.go:1044,
// worker_spawn.go:1012, onboarding.go:443) → this test goes RED on that arm.
const (
	wardenOperationalLog = "ocwarden.out.log"
	wardenStderrLog      = "ocwarden.err.log"
)

// assertPointsAtOutLog is the single rule, applied to every owner-facing
// sentence that names a warden log: name out.log, and never name err.log.
// Both halves are load-bearing. Only asserting "contains out.log" would stay
// green on a sentence that names BOTH files — which is precisely the
// zero-discrimination shape this whole change removes from the e2e greps, and
// it must not be reintroduced here.
func assertPointsAtOutLog(t *testing.T, where, sentence string) {
	t.Helper()
	if !strings.Contains(sentence, wardenOperationalLog) {
		t.Errorf("%s: the receipt must send the owner to %s — the only file that "+
			"carries the warden's own account of itself.\ngot: %q",
			where, wardenOperationalLog, sentence)
	}
	if strings.Contains(sentence, wardenStderrLog) {
		t.Errorf("%s: the receipt still names %s, a file whose only contents are "+
			"spawn env-var names — an owner who follows this pointer finds a "+
			"healthy-looking file and stops looking.\ngot: %q",
			where, wardenStderrLog, sentence)
	}
}

// The member producer's wake_timeout stamp, BOTH arms of wakeTimeoutReason: the
// codex-only sentence (which names the runtime switch) and the ordinary one.
// Two arms, one rule — a fix applied to only one of them is a half-fix, and
// reconcile.go carries the pointer twice for exactly that reason.
func TestWakeTimeoutReason_BothArmsPointAtTheWardenOperationalLog(t *testing.T) {
	s := newReconcileTestServer(t)

	codexOnly := lapseAStartOn(t, s, "mach-codex-only", codexOnlyRuntimes)
	hasClaude := lapseAStartOn(t, s, "mach-has-claude", bothRuntimes)

	assertPointsAtOutLog(t, "wakeTimeoutReason / codex-only arm", codexOnly.LastOpReason)
	assertPointsAtOutLog(t, "wakeTimeoutReason / has-claude arm", hasClaude.LastOpReason)

	// The fixture only discriminates if it really reached both arms.
	if codexOnly.LastOpReason == hasClaude.LastOpReason {
		t.Fatalf("fixture is blind: both machines produced the same sentence %q",
			codexOnly.LastOpReason)
	}
}

// The WORKER producer's wake_timeout stamp (worker_spawn.go's `default` arm).
// A separate sentence in a separate file: the member fix does not reach it.
func TestWorkerWakeTimeoutReceipt_PointsAtTheWardenOperationalLog(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := fsmWorkerFixture(t, s, "ow-logptr", WorkerStatusAssigned, 0)

	base := nowSecs()
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base)
	s.outsourceMu.Unlock()
	// Draining is what routes tick 2 to the `default` arm rather than to
	// never_collected: an empty backlog means the frame WAS collected.
	if len(s.hub.DrainWardenCommands(ServerSelfHost)) != 1 {
		t.Fatal("precondition: the first tick must dispatch a START")
	}

	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
	s.outsourceMu.Unlock()

	got, err := s.dal.GetOutsourceWorker("ow-logptr")
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if !strings.HasPrefix(got.LastOpReason, spawnReasonWakeTimeout+":") {
		t.Fatalf("precondition: want a %s receipt, got %q",
			spawnReasonWakeTimeout, got.LastOpReason)
	}
	assertPointsAtOutLog(t, "worker wake_timeout receipt", got.LastOpReason)
}

// The FIRST-RUN onboarding report's "warden never connected" step. Same wrong
// pointer, and the worst possible audience for it: someone whose very first
// contact with the product is being sent to an empty file.
func TestOnboardingUnlandedWake_PointsAtTheWardenOperationalLog(t *testing.T) {
	s := newReconcileTestServer(t)
	// deliberately NOT connected — this is the unlanded-wake path.

	report := s.runFirstRunOnboarding(
		fakeOnboarding(s, bootstrapResultDTO{MachineID: ServerSelfHost, OK: true}, nil, false),
		onboardingReportDTO{State: onboardingStateRunning})

	st, ok := stepByName(report, onboardingStepWakeAssistant)
	if !ok {
		t.Fatalf("precondition: no wake step in the report: %+v", report.Steps)
	}
	if st.Code != onboardingCodeWakeUndispatched {
		t.Fatalf("precondition: want the undispatched-wake step, got code %q (%+v)",
			st.Code, st)
	}
	assertPointsAtOutLog(t, "onboarding undispatched-wake step", st.Reason)
}

// Keeps the compile honest about the errors import if the file is trimmed later.
var _ = errors.New

// ── ③ wake_timeout must not stomp the warden's OWN receipt ───────────────────
//
// THE ASYMMETRY. clearWorkerPlacementBlock (worker_spawn.go:436) states the rule
// out loud — "Only a placement stamp is cleared; a warden's own receipt is never
// touched" — and enforces it with isPlacementBlockedReason. Its twin,
// stampWorkerPlacementBlocked (worker_spawn.go:408), enforces NOTHING: its only
// guard is the anti-churn "same exact string" check, so any different string
// wins. The protection is therefore ONE-WAY. That asymmetry is real and this
// test pins the guard that removes it.
//
// 🔴 BUT READ THIS BEFORE TRUSTING WHAT THIS TEST PROVES — IT USES A SHAPE
// PRODUCTION DOES NOT PRODUCE.
//
// The fixture below seeds the warden's clobber receipt onto the ROW, then hands
// reconcileWorkerLiveness the pre-seed snapshot `w`, whose LastOpReason is still
// "". Production never does that: outsource_sched.go:447 passes the tick's own
// snapshot and :492 passes a deliberate re-read, and BOTH carry the clobber
// prefix when it is present.
//
// That difference decides the outcome. reconcile.go sets `startTimedOut = true`
// at exactly one place (reconcile.go:636), and the clobber check at
// reconcile.go:587 sits ahead of it inside the same block with BOTH arms
// returning early (zombie-suspect inside the confirm grace, zombie-takeover
// after it). So a row that carries the prefix never reaches line 636, and the
// wake_timeout stamp — the only caller that can trip this let-pass — is never
// produced on that tick. Measured with a fresh-row probe: composed? false at
// t=+91s and still false at t=+400s, past ZombieConfirmGrace.
//
// ⇒ The condition this let-pass fires on and the condition that routes the tick
// away from wake_timeout are the SAME CONDITION. This test reaches the compose
// only because its stale `w` hides the prefix from decideUp.
//
// It is kept as a unit-level guard on the let-pass rule, and it is honestly
// labelled as one. It is NOT evidence that the overwrite happens in the field —
// no reachable production instance has been constructed. See the retraction at
// the head of wakeTimeoutOverWardenReceipt for why the code stays anyway.
//
// SCOPE. This fixes the let-pass rule only — no state machine, no receipt
// storage, no new column. The composed sentence KEEPS the warden's line verbatim
// as its prefix, which matters beyond politeness: reconcile.go:588 and
// api_monitoring.go:191 both dispatch on HasPrefix(reason,
// spawnClobberReasonPrefix), so a rewrite that dropped the prefix would silently
// disarm the zombie-takeover path.
//
// MUTANT (the acceptance for ③): delete the let-pass in
// stampWorkerPlacementBlocked → this test goes RED.
func TestWakeTimeout_DoesNotStompTheWardensSessionAliveReceipt(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := fsmWorkerFixture(t, s, "ow-alive", WorkerStatusAssigned, 0)

	base := nowSecs()
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base)
	s.outsourceMu.Unlock()
	if len(s.hub.DrainWardenCommands(ServerSelfHost)) != 1 {
		t.Fatal("precondition: the first tick must dispatch a START")
	}

	// The warden answers the START with its clobber-guard refusal. This is a
	// warden RECEIPT — an execution fact reported by the machine — not a
	// server-side placement guess.
	const wardenReceipt = `session_already_exists: tmux session "worker-ow-alive" ` +
		`is already live (clobber-guard refused to stomp it)`
	live, err := s.dal.GetOutsourceWorker("ow-alive")
	if err != nil || live == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	// Seeded through the receipt's SOLE writer (T-55): the five last_op* columns
	// left PutMember's DO UPDATE SET, so a whole-row write can no longer plant
	// this fixture — it would land nothing and the test would then be asserting
	// that a stamp did not erase a receipt that was never there.
	no := false
	if err := s.dal.SetMemberOpReceipt(live.ID, reconcileCmdStart, &no, "", wardenReceipt,
		base); err != nil {
		t.Fatalf("seed the warden receipt: %v", err)
	}

	// …and then the start window lapses, which is what fires the wake_timeout
	// stamp on top of it.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
	s.outsourceMu.Unlock()

	got, err := s.dal.GetOutsourceWorker("ow-alive")
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}

	// (a) THE LET-PASS: the warden's own words survive, verbatim and in front —
	// both because they are the truth and because the prefix is a live contract
	// (reconcile.go:588's zombie-takeover dispatch reads it).
	if !strings.HasPrefix(got.LastOpReason, spawnClobberReasonPrefix+":") {
		t.Errorf("the wake_timeout stamp erased the warden's own receipt.\n"+
			" got: %q\nwant: something still starting with %q — the warden REPORTED "+
			"why this worker did not start, and a later server-side guess must not "+
			"overwrite an execution fact (clearWorkerPlacementBlock already says so "+
			"in the other direction)",
			got.LastOpReason, spawnClobberReasonPrefix+":")
	}
	if !strings.Contains(got.LastOpReason, wardenReceipt) {
		t.Errorf("the warden's sentence must survive intact.\n got: %q\nwant it to contain: %q",
			got.LastOpReason, wardenReceipt)
	}

	// (b) THE PLAIN SENTENCE: it must say THIS is the situation — the session is
	// still alive — instead of sending the owner to inspect a runtime that is
	// not at fault.
	lower := strings.ToLower(got.LastOpReason)
	for _, want := range []string{"still running", "not a runtime failure"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the receipt must say in plain words that the OLD SESSION is "+
				"still alive; missing %q.\ngot: %q", want, got.LastOpReason)
		}
	}
	// The active misdirection this whole item exists to delete.
	if strings.Contains(lower, "is logged in on that machine") {
		t.Errorf("the receipt still tells the owner to go check whether the runtime "+
			"runs and is logged in — the one thing that is demonstrably NOT the "+
			"problem here.\ngot: %q", got.LastOpReason)
	}

	// (c) The row is STABLE across the ticks that follow.
	//
	// ⚠️ HONEST NOTE ON WHAT THIS DOES AND DOES NOT PROVE. Measured: mutating
	// away the idempotence sentinel in wakeTimeoutOverWardenReceipt leaves this
	// assertion GREEN. The reason is that once the row carries the
	// session_already_exists prefix, reconcile.go:588 routes the next tick into
	// the zombie-takeover arm rather than back into the wake_timeout stamp — so
	// this fixture never re-enters the compose path at all, and the assertion is
	// vacuous as an idempotence check. It is kept because "the receipt does not
	// churn on later ticks" is still a property worth pinning, but the real
	// idempotence guard is the direct unit test below
	// (TestWakeTimeoutOverWardenReceipt_ComposesOnceAndOnlyForWakeTimeout), which
	// does kill that mutant.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base+WakingTTLSecs*3)
	s.outsourceMu.Unlock()
	again, _ := s.dal.GetOutsourceWorker("ow-alive")
	if again.LastOpReason != got.LastOpReason {
		t.Errorf("the composed receipt is not stable across ticks — it must compose "+
			"once and then match the anti-churn guard.\ntick1: %q\ntick2: %q",
			got.LastOpReason, again.LastOpReason)
	}
}

// TestWakeTimeoutOverWardenReceipt_ComposesOnceAndOnlyForWakeTimeout drives the
// let-pass DIRECTLY, because the FSM fixture above cannot reach two of its three
// rules: after the first compose the row's clobber prefix reroutes the next tick
// into the zombie-takeover arm, so neither a second compose nor a non-wake_timeout
// stamp landing on a warden receipt is reachable from there. Both were left
// unguarded by the behavioural test alone — measured, not assumed: mutating out
// the sentinel and mutating out the wake_timeout gate both survived it.
//
// The function is pure, so this costs nothing and closes both holes.
func TestWakeTimeoutOverWardenReceipt_ComposesOnceAndOnlyForWakeTimeout(t *testing.T) {
	const wardenReceipt = `session_already_exists: tmux session "worker-ow-x" is already live`
	clobbered := OutsourceWorker{LastOp: reconcileCmdStart, LastOpReason: wardenReceipt}
	wakeTimeout := spawnReasonWakeTimeout + ": the start was collected but nothing came online"

	// (1) It composes: warden's line first, plain sentence appended.
	once := wakeTimeoutOverWardenReceipt(clobbered, wakeTimeout)
	if once != wardenReceipt+sessionAliveWakeNote {
		t.Fatalf("compose = %q, want the warden receipt followed by the plain sentence", once)
	}

	// (2) IDEMPOTENCE — the rule the FSM fixture cannot reach. Feed the composed
	// string back in, as the 30s cadence would on any tick that DOES re-enter the
	// stamp: it must return the row's own string unchanged so the anti-churn
	// guard matches and writes nothing. Without this, the receipt grows by one
	// paragraph per tick and fans an SSE delta each time.
	twice := wakeTimeoutOverWardenReceipt(
		OutsourceWorker{LastOp: reconcileCmdStart, LastOpReason: once}, wakeTimeout)
	if twice != once {
		t.Errorf("composing twice must be a no-op.\nonce:  %q\ntwice: %q", once, twice)
	}
	if n := strings.Count(twice, sessionAliveWakeNote); n != 1 {
		t.Errorf("the plain sentence appears %d times — the receipt is growing per tick", n)
	}

	// (3) NARROWNESS — the other rule the fixture cannot reach. The let-pass is
	// for wake_timeout ONLY. Any other stamp landing on a warden receipt keeps
	// today's behaviour verbatim; composing "the previous session is still
	// running" onto, say, a machine_unavailable stamp would invent a cause.
	for _, other := range []string{
		placementReasonUnavailable + ": the pinned machine is offline",
		placementReasonNoMachine + ": no machine pinned",
		spawnReasonNeverCollected + ": the frame was never picked up",
		spawnReasonBackoff + ": awaiting retry window",
	} {
		if got := wakeTimeoutOverWardenReceipt(clobbered, other); got != other {
			t.Errorf("the let-pass fired for a non-wake_timeout reason.\n in: %q\nout: %q",
				other, got)
		}
	}

	// (4) And it does nothing when there is no warden receipt to protect.
	plain := OutsourceWorker{LastOp: reconcileCmdStart, LastOpReason: ""}
	if got := wakeTimeoutOverWardenReceipt(plain, wakeTimeout); got != wakeTimeout {
		t.Errorf("with no warden receipt the reason must pass through untouched, got %q", got)
	}
	// …nor when the warden receipt sits under a different verb.
	otherVerb := OutsourceWorker{LastOp: reconcileCmdStop, LastOpReason: wardenReceipt}
	if got := wakeTimeoutOverWardenReceipt(otherVerb, wakeTimeout); got != wakeTimeout {
		t.Errorf("a receipt under a non-start verb is not this rule's subject, got %q", got)
	}
}

// The other half of the let-pass, and the reason it is a let-pass rather than a
// blanket "wake_timeout never writes": an ORDINARY wake timeout, with no warden
// receipt in the way, must still produce its ordinary receipt. A fix that
// silenced wake_timeout everywhere would trade one silence for another.
func TestWakeTimeout_StillStampsWhenThereIsNoWardenReceiptToProtect(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := fsmWorkerFixture(t, s, "ow-plain", WorkerStatusAssigned, 0)

	base := nowSecs()
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base)
	s.outsourceMu.Unlock()
	if len(s.hub.DrainWardenCommands(ServerSelfHost)) != 1 {
		t.Fatal("precondition: the first tick must dispatch a START")
	}
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
	s.outsourceMu.Unlock()

	got, _ := s.dal.GetOutsourceWorker("ow-plain")
	if !strings.HasPrefix(got.LastOpReason, spawnReasonWakeTimeout+":") {
		t.Fatalf("a plain lapsed start must still get its wake_timeout receipt, got %q",
			got.LastOpReason)
	}
}
