package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// T-ba62 — the automatic first-run onboarding, driven end-to-end through its
// seams (no exec, no launchd, no sleeping).
//
// The two directions the ticket demands are pinned here:
//   - prerequisites present → the machine is installed AND the assistant is
//     actually set to come online with a START dispatched;
//   - any prerequisite missing → LOUD failure carrying the real reason, the
//     assistant is NOT woken, and no residue is left behind.

// fakeOnboarding builds a runner whose install outcome and warden reachability
// are pinned per case. now/sleep are fake so the bounded wait is instant.
func fakeOnboarding(s *apiServer, res bootstrapResultDTO, err error, online bool) onboardingRunner {
	clock := 1000.0
	return onboardingRunner{
		installWarden: func(Member) (bootstrapResultDTO, error) { return res, err },
		// The default fixture is a host with NO warden yet — the fresh-install
		// shape. The interlock is exercised explicitly below.
		wardenInstalled: func() bool { return false },
		wardenOnline: func(id string) bool {
			// Reachability is asked about THIS host's warden by contract; a
			// runner that polled some other id would read as never-online.
			return online && id == ServerSelfHost
		},
		sleep:      func(d time.Duration) { clock += d.Seconds() },
		now:        func() float64 { return clock },
		waitBudget: 2 * time.Second,
	}
}

func stepByName(r onboardingReportDTO, name string) (onboardingStepDTO, bool) {
	for _, st := range r.Steps {
		if st.Name == name {
			return st, true
		}
	}
	return onboardingStepDTO{}, false
}

// ── direction 1: prerequisites present ──────────────────────────────────────

func TestOnboarding_HappyPathInstallsAndWakes(t *testing.T) {
	s := newReconcileTestServer(t)
	// The seeded server-self warden holds its SSE downstream (what "installed
	// AND reachable" actually means).
	connectOnline(t, s, ServerSelfHost)

	report := s.runFirstRunOnboarding(
		fakeOnboarding(s, bootstrapResultDTO{MachineID: ServerSelfHost, OK: true, Log: "installed"}, nil, true),
		onboardingReportDTO{State: onboardingStateRunning})

	if report.State != onboardingStateOK {
		t.Fatalf("a fully-satisfied onboarding must report ok, got %q (%+v)", report.State, report.Steps)
	}
	if st, ok := stepByName(report, onboardingStepInstallWarden); !ok || !st.OK {
		t.Fatalf("the warden install step must be ok: %+v", report.Steps)
	}
	if st, ok := stepByName(report, onboardingStepWakeAssistant); !ok || !st.OK {
		t.Fatalf("the assistant wake step must be ok: %+v", report.Steps)
	}
	// The DURABLE effect, not just the report: the seeded assistant is now
	// desired-online...
	mira, err := s.dal.GetMember(seedMiraID)
	if err != nil || mira == nil {
		t.Fatalf("seeded assistant missing: %v", err)
	}
	if mira.DesiredState != DesiredStateOnline {
		t.Fatalf("the assistant must be set to come online, got %q", mira.DesiredState)
	}
	// ...and a START really reached the warden (an intent nobody dispatched
	// would satisfy the assertion above while doing nothing).
	frames := drainFrames(t, s, ServerSelfHost)
	if len(frames) == 0 {
		t.Fatalf("expected a START frame on the server-self warden FIFO; got none")
	}
	// The report is readable back through the same accessor the settings read uses.
	stored := s.onboardingReport()
	if stored == nil || stored.State != onboardingStateOK {
		t.Fatalf("the report must be persisted: %+v", stored)
	}
	if stored.FinishedAt <= 0 {
		t.Fatalf("a finished report must carry finished_at, got %v", stored.FinishedAt)
	}
}

// ── direction 2: a prerequisite missing ─────────────────────────────────────

// The headline case: `ocwarden install` refuses because claude is unresolvable
// (its T-ba62 fail-closed behaviour). The onboarding must NOT go on to wake the
// assistant — a wake with no warden to run it is precisely the grey-member-with-
// no-explanation this ticket exists to remove.
func TestOnboarding_InstallFailureIsLoudAndDoesNotWake(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost) // even a reachable warden must not rescue it

	failLog := "[ocwarden install] FATAL: claude_bin_unresolved: no claude CLI on this host"
	report := s.runFirstRunOnboarding(
		fakeOnboarding(s, bootstrapResultDTO{MachineID: ServerSelfHost, OK: false, ExitCode: 1, Log: failLog}, nil, true),
		onboardingReportDTO{State: onboardingStateRunning})

	if report.State != onboardingStateFailed {
		t.Fatalf("a failed install must report failed, got %q", report.State)
	}
	st, ok := stepByName(report, onboardingStepInstallWarden)
	if !ok || st.OK {
		t.Fatalf("the install step must be recorded as failed: %+v", report.Steps)
	}
	// ASSERT THE REASON, not merely the verdict: "wrongly failed" and "correctly
	// refused" share a state string.
	if !strings.Contains(st.Reason, "exit 1") {
		t.Errorf("the reason must name the installer's exit code, got %q", st.Reason)
	}
	if !strings.Contains(st.Detail, "claude_bin_unresolved") {
		t.Errorf("the failing installer log must be KEPT (it carries the actual cause), got %q", st.Detail)
	}
	// NO HALF-STUDIO: the wake step was never attempted...
	if _, attempted := stepByName(report, onboardingStepWakeAssistant); attempted {
		t.Errorf("the assistant must not be woken when the machine cannot run agents: %+v", report.Steps)
	}
	// ...the assistant is untouched (still exactly as the out-of-box seed left her)...
	mira, _ := s.dal.GetMember(seedMiraID)
	if mira == nil || mira.DesiredState != DesiredStateOffline {
		t.Fatalf("the assistant must be left as seeded (offline), got %+v", mira)
	}
	// ...and nothing was dispatched to any warden.
	if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
		t.Fatalf("a failed onboarding must dispatch nothing, got %+v", frames)
	}
}

// The installer could not even be run (no embedded ocwarden / no binary cache).
func TestOnboarding_InstallerUnavailableIsLoud(t *testing.T) {
	s := newReconcileTestServer(t)
	report := s.runFirstRunOnboarding(
		fakeOnboarding(s, bootstrapResultDTO{}, errors.New("no embedded ocwarden copy"), false),
		onboardingReportDTO{State: onboardingStateRunning})

	if report.State != onboardingStateFailed {
		t.Fatalf("want failed, got %q", report.State)
	}
	st, _ := stepByName(report, onboardingStepInstallWarden)
	if !strings.Contains(st.Reason, "no embedded ocwarden copy") {
		t.Errorf("the underlying cause must survive into the reason, got %q", st.Reason)
	}
	mira, _ := s.dal.GetMember(seedMiraID)
	if mira.DesiredState != DesiredStateOffline {
		t.Fatalf("the assistant must be left as seeded, got %q", mira.DesiredState)
	}
}

// The install SUCCEEDED but the warden never connected back inside the window.
// The wake intent is still persisted (the cadence retries it — dropping it would
// leave the studio permanently asleep), but the report must say plainly that
// nothing was dispatched. A clean "ok" here would be the same silent
// false-success in a new place.
func TestOnboarding_WardenNeverConnectsReportsUnlanded(t *testing.T) {
	s := newReconcileTestServer(t)
	// deliberately NOT connected

	report := s.runFirstRunOnboarding(
		fakeOnboarding(s, bootstrapResultDTO{MachineID: ServerSelfHost, OK: true}, nil, false),
		onboardingReportDTO{State: onboardingStateRunning})

	if report.State != onboardingStateFailed {
		t.Fatalf("an undispatched wake must not report ok, got %q", report.State)
	}
	st, ok := stepByName(report, onboardingStepWakeAssistant)
	if !ok || st.OK {
		t.Fatalf("the wake step must be recorded as not-ok: %+v", report.Steps)
	}
	for _, want := range []string{"has not connected", "retrying", "ocwarden.err.log"} {
		if !strings.Contains(st.Reason, want) {
			t.Errorf("the reason must contain %q so the owner knows what to check, got %q", want, st.Reason)
		}
	}
	// The intent IS persisted — this is a "not yet", not a rollback.
	mira, _ := s.dal.GetMember(seedMiraID)
	if mira.DesiredState != DesiredStateOnline {
		t.Fatalf("the wake intent must persist for the cadence to retry, got %q", mira.DesiredState)
	}
}

// ── the kick: idempotence + the never-ran contract ──────────────────────────

func TestOnboardingReport_AbsentUntilItRuns(t *testing.T) {
	s := newReconcileTestServer(t)
	if r := s.onboardingReport(); r != nil {
		t.Fatalf("a database where onboarding never ran must report nothing, got %+v", r)
	}
	if s.settingsView().Onboarding != nil {
		t.Fatalf("the settings read must carry a null onboarding until it runs")
	}
}

func TestKickFirstRunOnboarding_IsIdempotent(t *testing.T) {
	s := newReconcileTestServer(t)
	// Pre-claim the slot with a finished report: a second kick must be a no-op,
	// so a re-POST of set-password can never race two installs on one launchd
	// label.
	done := onboardingReportDTO{State: onboardingStateOK, StartedAt: 1, FinishedAt: 2}
	if err := s.putOnboardingReport(done); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	s.kickFirstRunOnboarding()
	got := s.onboardingReport()
	if got == nil || got.State != onboardingStateOK || got.StartedAt != 1 {
		t.Fatalf("an existing report must be left untouched, got %+v", got)
	}
}

// The report reaches the owner through GET /api/settings — that is the surface
// the cockpit reads to explain a missing assistant.
func TestSettingsView_CarriesOnboardingReport(t *testing.T) {
	s := newReconcileTestServer(t)
	report := onboardingReportDTO{
		State: onboardingStateFailed,
		Steps: []onboardingStepDTO{{
			Name:   onboardingStepInstallWarden,
			Reason: "installing this machine's warden failed (exit 1)",
			Detail: "claude_bin_unresolved",
		}},
	}
	if err := s.putOnboardingReport(report); err != nil {
		t.Fatalf("put report: %v", err)
	}
	view := s.settingsView()
	if view.Onboarding == nil {
		t.Fatalf("the settings read must carry the onboarding report")
	}
	if view.Onboarding.State != onboardingStateFailed {
		t.Fatalf("state = %q", view.Onboarding.State)
	}
	if len(view.Onboarding.Steps) != 1 ||
		!strings.Contains(view.Onboarding.Steps[0].Reason, "failed") {
		t.Fatalf("the failure reason must reach the owner: %+v", view.Onboarding.Steps)
	}
}

// ── the safety interlock ────────────────────────────────────────────────────

// 🔴 The most dangerous thing in this ticket: onboarding runs `ocwarden install
// --force`, and a launchd label is a uid-scoped singleton that does NOT follow
// $HOME or a throwaway database. So ANY ocserverd that reaches set-password on
// a fresh DB — a conformance run, an e2e run, a scratch database on the
// operator's own laptop — would re-point the REAL warden at itself and take a
// live fleet offline. An automatic action must never install over an existing
// one.
func TestOnboarding_NeverInstallsOverAnExistingWarden(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)

	installed := false
	run := fakeOnboarding(s, bootstrapResultDTO{OK: true}, nil, true)
	run.wardenInstalled = func() bool { return true }
	run.installWarden = func(Member) (bootstrapResultDTO, error) {
		installed = true
		return bootstrapResultDTO{OK: true}, nil
	}

	report := s.runFirstRunOnboarding(run, onboardingReportDTO{State: onboardingStateRunning})

	if installed {
		t.Fatalf("automatic onboarding must NOT install over an existing warden")
	}
	st, ok := stepByName(report, onboardingStepInstallWarden)
	if !ok || !st.OK {
		t.Fatalf("an already-installed host is a fine starting point: %+v", report.Steps)
	}
	if !strings.Contains(st.Reason, "already has a warden") {
		t.Errorf("the reason must say the existing install was left alone, got %q", st.Reason)
	}
	// and the run still goes on to do the useful half (waking the assistant).
	if wake, ok := stepByName(report, onboardingStepWakeAssistant); !ok || !wake.OK {
		t.Fatalf("the wake must still run on an already-installed host: %+v", report.Steps)
	}
}

// The kill switch, asserted through the real claim/skip decision.
//
// 🔴 Both legs inject the runner (kickFirstRunOnboardingWith). The earlier
// version called kickFirstRunOnboarding for its positive control, which binds
// newOnboardingRunner — i.e. a `go test` on any machine would have consulted
// the REAL $HOME and could have exec'd `ocwarden install --force` against the
// developer's own launchd domain. The only thing that stopped it was the test
// fixture happening to leave binCacheDir empty; one line elsewhere
// (s.binCacheDir = t.TempDir(), which api_machines_test.go already does) would
// have turned a unit test into a fleet-hijacking install. A test must not be
// one edit away from mutating the host it runs on.
func TestKickFirstRunOnboarding_HonoursKillSwitch(t *testing.T) {
	s := newReconcileTestServer(t)
	installed := false
	run := fakeOnboarding(s, bootstrapResultDTO{OK: true}, nil, true)
	run.installWarden = func(Member) (bootstrapResultDTO, error) {
		installed = true
		return bootstrapResultDTO{OK: true}, nil
	}

	t.Setenv("OC_NO_ONBOARDING", "1")
	s.kickFirstRunOnboardingWith(run)
	if r := s.onboardingReport(); r != nil {
		t.Fatalf("OC_NO_ONBOARDING=1 must not even claim the slot, got %+v", r)
	}
	if installed {
		t.Fatalf("the kill switch must prevent the install outright")
	}
	// Positive control: without the switch the slot IS claimed, so the leg above
	// cannot be passing merely because nothing ever runs.
	t.Setenv("OC_NO_ONBOARDING", "")
	s.kickFirstRunOnboardingWith(run)
	if r := s.onboardingReport(); r == nil {
		t.Fatalf("without the kill switch, onboarding must claim its slot")
	}
}

// ── R4: a dispatched START is determined POSITIVELY ─────────────────────────

// The failure mode the first cut missed entirely: the warden is online, so
// `!online` is false, and buildStartFrame's downgrade does NOT set
// DispatchUnlanded — so the old `dec.DispatchUnlanded || !online` test was
// false on both terms and reported "the assistant is waking" while zero frames
// left the server. Here the assistant has no assemblable payload (her role
// definition names a role that does not exist), which is exactly that shape.
func TestOnboarding_OnlineWardenButUnbuildableFrameIsNotReportedAsWaking(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)

	// Break the START payload without touching the warden's reachability.
	mira, err := s.dal.GetMember(seedMiraID)
	if err != nil || mira == nil {
		t.Fatalf("seeded assistant missing: %v", err)
	}
	mira.RoleKey = "no-such-role-exists"
	putTestMember(t, s, *mira)

	report := s.runFirstRunOnboarding(
		fakeOnboarding(s, bootstrapResultDTO{MachineID: ServerSelfHost, OK: true}, nil, true),
		onboardingReportDTO{State: onboardingStateRunning})

	// Sanity: nothing was actually dispatched (otherwise this test would be
	// asserting against a case that never arose).
	if frames := drainFrames(t, s, ServerSelfHost); len(frames) != 0 {
		t.Fatalf("fixture is wrong — a START did land: %+v", frames)
	}
	st, ok := stepByName(report, onboardingStepWakeAssistant)
	if !ok || st.OK {
		t.Fatalf("an undispatched wake must NOT be reported as ok: %+v", report.Steps)
	}
	if report.State != onboardingStateFailed {
		t.Fatalf("state = %q, want failed", report.State)
	}
	if !strings.Contains(st.Reason, "no start command has been dispatched") {
		t.Errorf("the reason must say nothing was dispatched, got %q", st.Reason)
	}
}

// ── R2: a `running` report left by a dead process must not wedge the studio ──

func TestRecoverStaleOnboarding_ClosesOutAnInterruptedRun(t *testing.T) {
	s := newReconcileTestServer(t)
	if err := s.putOnboardingReport(onboardingReportDTO{
		State: onboardingStateRunning, StartedAt: 1,
	}); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	s.recoverStaleOnboarding()

	got := s.onboardingReport()
	if got == nil || got.State != onboardingStateFailed {
		t.Fatalf("an interrupted run must be closed out as failed, got %+v", got)
	}
	if got.FinishedAt <= 0 {
		t.Errorf("a closed-out report must carry finished_at")
	}
	// The REASON is the whole point: without it the owner sees a dead studio and
	// no explanation, which is the exact bug this ticket exists to remove.
	st, ok := stepByName(*got, onboardingStepInstallWarden)
	if !ok || st.OK {
		t.Fatalf("the interrupted step must be recorded as not-ok: %+v", got.Steps)
	}
	for _, want := range []string{"interrupted", "安裝"} {
		if !strings.Contains(st.Reason, want) {
			t.Errorf("the reason must contain %q (what happened / what to do), got %q", want, st.Reason)
		}
	}
	// And it is now a TERMINAL state, so the cockpit banner can actually draw it.
	if got.State == onboardingStateRunning {
		t.Errorf("a non-terminal state stays invisible to the banner")
	}
}

// Positive control: a FINISHED report must be left exactly as it is — a
// recovery that rewrote every report would pass the test above for the wrong
// reason and would destroy a real success record on every restart.
func TestRecoverStaleOnboarding_LeavesTerminalReportsAlone(t *testing.T) {
	s := newReconcileTestServer(t)
	done := onboardingReportDTO{State: onboardingStateOK, StartedAt: 1, FinishedAt: 2}
	if err := s.putOnboardingReport(done); err != nil {
		t.Fatalf("seed report: %v", err)
	}
	s.recoverStaleOnboarding()
	got := s.onboardingReport()
	if got == nil || got.State != onboardingStateOK || got.FinishedAt != 2 || len(got.Steps) != 0 {
		t.Fatalf("a terminal report must be untouched, got %+v", got)
	}
}

// ── R5: the interlock PREDICATE itself (not just the seam in front of it) ────

// The launchd label is the axis the damage happens on: its GUI domain is keyed
// on uid and does NOT follow $HOME. So a registered label must veto the install
// even when the file check would happily say "nothing here" — which is exactly
// what `HOME=/tmp/x ocserverd serve` produces.
func TestWardenAlreadyInstalledHere_LabelRegisteredVetoesEvenWithAForeignHOME(t *testing.T) {
	s := newReconcileTestServer(t)
	asked := ""
	got := s.wardenAlreadyInstalledHere(
		envOfMap(map[string]string{"HOME": "/tmp/definitely-not-the-real-home"}),
		func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }, // file axis: absent
		func(label string) bool { asked = label; return true },           // label axis: registered
	)
	if !got {
		t.Fatalf("a registered launchd label must veto the install regardless of HOME")
	}
	if asked != "com.officraft.ocwarden" {
		t.Errorf("must ask about the main-instance label, asked %q", asked)
	}
}

// The file axis still answers when launchd does not (unloaded job / exec fault).
func TestWardenAlreadyInstalledHere_TokfileAxis(t *testing.T) {
	s := newReconcileTestServer(t)
	var statted string
	got := s.wardenAlreadyInstalledHere(
		envOfMap(map[string]string{"HOME": "/Users/someone"}),
		func(p string) (os.FileInfo, error) { statted = p; return nil, nil },
		func(string) bool { return false },
	)
	if !got {
		t.Fatalf("an existing tokfile must veto the install")
	}
	if statted != "/Users/someone/.officraft/warden/exec-warden.tok" {
		t.Errorf("stat'd %q — not the path ocwarden writes", statted)
	}
	// Positive control: both axes clear ⇒ the install is allowed. Without this,
	// a predicate hardwired to true would pass every assertion above.
	if s.wardenAlreadyInstalledHere(
		envOfMap(map[string]string{"HOME": "/Users/someone"}),
		func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		func(string) bool { return false },
	) {
		t.Errorf("a host with neither a label nor a tokfile must be installable")
	}
}

// A namespaced instance keys BOTH axes off its namespace — it must neither be
// blocked by the main instance's warden nor able to stomp it.
func TestWardenAlreadyInstalledHere_NamespaceKeysBothAxes(t *testing.T) {
	s := newReconcileTestServer(t)
	s.namespace = "e2e7"
	var statted, asked string
	s.wardenAlreadyInstalledHere(
		envOfMap(map[string]string{"HOME": "/Users/someone"}),
		func(p string) (os.FileInfo, error) { statted = p; return nil, os.ErrNotExist },
		func(l string) bool { asked = l; return false },
	)
	if statted != "/Users/someone/.officraft-e2e7/warden/exec-warden.tok" {
		t.Errorf("namespaced tokfile path = %q", statted)
	}
	if asked != "com.officraft.ocwarden.e2e7" {
		t.Errorf("namespaced label = %q", asked)
	}
}

// HOME unset ⇒ we cannot tell where a warden would live ⇒ REFUSE. Fail-closed
// in the only direction that is safe: guessing "nothing installed" here is what
// lets an install proceed blind.
func TestWardenAlreadyInstalledHere_NoHomeFailsClosed(t *testing.T) {
	s := newReconcileTestServer(t)
	if !s.wardenAlreadyInstalledHere(
		envOfMap(map[string]string{}),
		func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		func(string) bool { return false },
	) {
		t.Fatalf("an unknowable HOME must refuse the install, not permit it")
	}
}

// 🔴 The cross-binary contract. wardenTokfilePath / wardenLaunchdLabel are
// hand-copied from cli/ocwarden/namespace.go (separate go modules — no shared
// constant is possible). If that derivation moves, this guard would stat a path
// nobody writes and answer "no warden here" for a host that has one. These
// literals ARE the contract; changing them requires changing ocwarden too.
func TestWardenPaths_MirrorTheOcwardenDerivation(t *testing.T) {
	cases := []struct{ home, ns, tokfile, label string }{
		{"/Users/x", "", "/Users/x/.officraft/warden/exec-warden.tok", "com.officraft.ocwarden"},
		{"/Users/x", "e2e1", "/Users/x/.officraft-e2e1/warden/exec-warden.tok", "com.officraft.ocwarden.e2e1"},
	}
	for _, c := range cases {
		if got := wardenTokfilePath(c.home, c.ns); got != c.tokfile {
			t.Errorf("wardenTokfilePath(%q,%q) = %q, want %q (cli/ocwarden namespace.go tokfileFor)", c.home, c.ns, got, c.tokfile)
		}
		if got := wardenLaunchdLabel(c.ns); got != c.label {
			t.Errorf("wardenLaunchdLabel(%q) = %q, want %q (cli/ocwarden namespace.go wardenLabelFor)", c.ns, got, c.label)
		}
	}
}

func envOfMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}
