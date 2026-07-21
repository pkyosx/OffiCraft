package main

// onboarding.go — the automatic FIRST-RUN onboarding (T-ba62).
//
// THE PROBLEM. After a one-click install the owner set a password and landed in
// an EMPTY cockpit. The database already carried both rows the studio needs —
// the seeded assistant Mira and the seeded server-self warden (dbseed.go) — but
// both are DesiredStateOffline, and nothing on the machine was actually
// installed or running. Turning that into a working studio needed two manual
// acts the product never pointed at: find 監控 › 機器 › 「伺服器這一台」 and press
// 安裝, then find the assistant and activate her. The owner's ask was that
// neither of those should exist.
//
// WHAT THIS DOES. Right after the initial password is set, in-process:
//
//	1. install THIS host's warden — the identical core the cockpit's 安裝 button
//	   drives (runWardenInstallHere), not a second copy;
//	2. wait, bounded, for that warden to actually connect its SSE downstream —
//	   because "installed" and "reachable" are different facts and a START into
//	   an unreachable warden is silently dropped;
//	3. flip the seeded assistant to desired online and reconcile her NOW.
//
// FAIL-CLOSED, LOUDLY. Step 1 failing means the host cannot run agents at all
// (since T-ba62 `ocwarden install` refuses outright when claude is unresolvable),
// so step 3 is NOT attempted: a wake we know cannot land would only produce the
// grey-member-with-no-explanation this whole ticket exists to kill. Nothing is
// created and nothing is half-configured either way — every row involved was
// already seeded, so the failure path leaves the database exactly as the seed
// left it. Every step's verdict and REASON is persisted as the onboarding
// report and served on the owner-gated settings read, so the first thing the
// owner can see is WHY the assistant is not awake.
//
// PRIVILEGE. This runs in-process through the DAL and deliberately does not go
// through the HTTP authz choke — the same shape as seedOutOfBox creating these
// very rows, and as the outsource scheduler minting and onlining its own
// workers. No gate is loosened, no token is minted for it.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"encoding/json"
)

// settingOnboardingReport stores the JSON onboardingReportDTO of the one
// automatic first-run onboarding. Absent = onboarding never ran (a database
// that predates T-ba62, or one that already had a password).
const settingOnboardingReport = "onboarding.first_run"

// Onboarding step names — stable machine keys, safe for a UI to branch on.
const (
	onboardingStepInstallWarden = "install_warden"
	onboardingStepWakeAssistant = "wake_assistant"
)

// Onboarding report states.
const (
	onboardingStateRunning = "running"
	onboardingStateOK      = "ok"
	onboardingStateFailed  = "failed"
)

// wardenOnlineWait bounds step 2. A freshly bootstrapped warden connects in
// about a second; 30s is generous enough that a slow launchd start is not
// mistaken for a broken install, and short enough that a genuinely broken one
// is reported while the owner is still looking at the screen. Exceeding it is
// NOT a hard failure — the reconcile cadence keeps retrying the wake — but it
// IS reported, because "dispatched into the void" must not read as success.
const wardenOnlineWait = 30 * time.Second

// wardenOnlinePoll is the poll interval for that wait.
const wardenOnlinePoll = 500 * time.Millisecond

// wardenAlreadyInstalledHere reports whether THIS host already carries an
// installed warden for this instance (its exec-warden token file exists).
//
// 🔴 WHY THIS GUARD EXISTS — it is a safety interlock, not an optimisation.
// The install this onboarding drives is `ocwarden install --force`, and a
// launchd label is a SINGLETON IN THE USER'S GUI DOMAIN keyed on uid — it does
// not follow $HOME, a temp dir, or a throwaway database. So ANY ocserverd
// process on this machine that reaches set-password on a fresh database (a
// conformance run, an e2e run, a developer poking a scratch DB) would otherwise
// re-point the operator's REAL warden at itself and take the live fleet
// offline. An automatic action must never overwrite an existing installation:
// that is what the cockpit's explicit 安裝 button (with --force) is for.
//
// Note this is deliberately a FILE check, not a launchctl query: the file is
// what `ocwarden install` itself treats as "a warden lives here" (its own
// one-warden-per-machine guard keys on the same path), and it needs no
// subprocess.
func (s *apiServer) wardenAlreadyInstalledHere(getenv func(string) string) bool {
	home := getenv("HOME")
	if home == "" {
		return true // cannot tell where a warden would live ⇒ refuse to install
	}
	root := filepath.Join(home, ".officraft")
	if s.namespace != "" {
		root = filepath.Join(home, ".officraft-"+s.namespace)
	}
	_, err := os.Stat(filepath.Join(root, "warden", "exec-warden.tok"))
	return err == nil
}

// onboardingDisabled is the explicit kill switch (OC_NO_ONBOARDING=1). Test
// harnesses that boot a REAL ocserverd against a throwaway database
// (conformance/run.sh, the e2e suites) set it, so a suite can never reach the
// host-mutating path even by accident. Belt to wardenAlreadyInstalledHere's
// braces — on a CI machine with no warden at all, that guard would pass.
func onboardingDisabled(getenv func(string) string) bool {
	return strings.TrimSpace(getenv("OC_NO_ONBOARDING")) == "1"
}

// onboardingRunner holds the injectable seams of one onboarding run. Every edge
// that touches the OS or the clock is a seam so the tests drive the whole thing
// with no exec, no launchd, and no sleeping.
type onboardingRunner struct {
	// installWarden installs the host warden and returns the SAME result DTO
	// the cockpit button surfaces. Production binds runWardenInstallHere.
	installWarden func(machine Member) (bootstrapResultDTO, error)
	// wardenOnline reports whether the machine holds a live SSE downstream.
	wardenOnline func(machineID string) bool
	// wardenInstalled is the SAFETY INTERLOCK seam: true ⇒ this host already
	// carries a warden and onboarding must not install over it (see
	// wardenAlreadyInstalledHere for why that would be a fleet-hijacking bug).
	wardenInstalled func() bool
	sleep           func(time.Duration)
	now             func() float64
	// waitBudget is wardenOnlineWait in production; tests shrink it.
	waitBudget time.Duration
}

// kickFirstRunOnboarding starts the one automatic onboarding run in the
// BACKGROUND and returns immediately.
//
// Why background: the set-password handler holds settingsMu for its whole body,
// and this run installs a launchd job and then waits up to wardenOnlineWait for
// an SSE connect. Doing that inline would block the owner's very first request
// for tens of seconds behind a lock that every other settings write needs. The
// report is the durable rendezvous point instead — the cockpit reads it from
// GET /api/settings.
//
// Idempotent by report presence: a second call is a no-op, so a re-POST of
// set-password (or a future caller) can never start two installs racing on the
// same launchd label.
func (s *apiServer) kickFirstRunOnboarding() {
	if s.dal == nil {
		return
	}
	if onboardingDisabled(os.Getenv) {
		onboardingLog("OC_NO_ONBOARDING=1 — skipping automatic first-run onboarding")
		return
	}
	existing, err := s.dal.GetSetting(settingOnboardingReport)
	if err != nil || existing != nil {
		return
	}
	// Claim the slot BEFORE going async: two concurrent kicks must not both
	// pass the check above and both install.
	running := onboardingReportDTO{State: onboardingStateRunning, StartedAt: nowSecs()}
	if err := s.putOnboardingReport(running); err != nil {
		onboardingLog("could not claim the onboarding slot: %v", err)
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				onboardingLog("FAULT: %v", r)
				s.finishOnboarding(running, []onboardingStepDTO{{
					Name:   onboardingStepInstallWarden,
					Reason: "onboarding faulted — see the server log",
				}})
			}
		}()
		s.runFirstRunOnboarding(s.newOnboardingRunner(), running)
	}()
}

// newOnboardingRunner wires the production seams.
func (s *apiServer) newOnboardingRunner() onboardingRunner {
	return onboardingRunner{
		installWarden: func(machine Member) (bootstrapResultDTO, error) {
			binPath, err := s.resolveOcwardenBinaryFrom(bindistFS())
			if err != nil {
				return bootstrapResultDTO{}, err
			}
			return s.runWardenInstallHere(machine, binPath, s.selfBase)
		},
		wardenOnline:    func(id string) bool { return s.hub.IsOnline(id) },
		wardenInstalled: func() bool { return s.wardenAlreadyInstalledHere(os.Getenv) },
		sleep:           time.Sleep,
		now:             nowSecs,
		waitBudget:      wardenOnlineWait,
	}
}

// runFirstRunOnboarding executes the two steps and persists the report. Pure of
// HTTP: the caller supplies the seams, so this is exercised end-to-end in tests.
func (s *apiServer) runFirstRunOnboarding(run onboardingRunner, report onboardingReportDTO) onboardingReportDTO {
	steps := []onboardingStepDTO{}

	// ── step 1: this host's warden ───────────────────────────────────────────
	machine, err := s.dal.GetMember(ServerSelfHost)
	if err != nil || machine == nil {
		steps = append(steps, onboardingStepDTO{
			Name: onboardingStepInstallWarden,
			Reason: "this server's machine row is missing from the roster — the " +
				"out-of-box seed did not run; restart the server and try again",
		})
		return s.finishOnboarding(report, steps)
	}
	// 先歸零再裝: a residual uninstall intent would have the fresh warden boot
	// into a standing kill order (same contract as the cockpit install path).
	if err := s.clearResidualUninstall(machine, triggerServer); err != nil {
		steps = append(steps, onboardingStepDTO{
			Name:   onboardingStepInstallWarden,
			Reason: "could not clear a residual uninstall intent on this machine: " + err.Error(),
		})
		return s.finishOnboarding(report, steps)
	}
	if run.wardenInstalled() {
		// Already installed here — say so and move on. Automatic onboarding
		// NEVER re-installs over an existing warden (see the guard's comment:
		// the launchd label is a uid-scoped singleton, so a stomp here would
		// hijack a live fleet).
		steps = append(steps, onboardingStepDTO{
			Name: onboardingStepInstallWarden, OK: true,
			Reason: "this machine already has a warden installed — left untouched",
		})
		return s.wakeAssistantStep(run, report, steps)
	}
	res, err := run.installWarden(*machine)
	if err != nil {
		steps = append(steps, onboardingStepDTO{
			Name:   onboardingStepInstallWarden,
			Reason: "could not run the warden installer on this host: " + err.Error(),
		})
		return s.finishOnboarding(report, steps)
	}
	if !res.OK {
		// The LOG is kept on the failure path AND is the whole point: it names
		// the actual cause (claude_bin_unresolved, a launchd refusal, …).
		steps = append(steps, onboardingStepDTO{
			Name: onboardingStepInstallWarden,
			Reason: "installing this machine's warden failed (exit " +
				strconv.Itoa(res.ExitCode) + ") — the assistant was NOT woken, because a " +
				"wake with no warden to run it would just sit grey with no reason",
			Detail: res.Log,
		})
		return s.finishOnboarding(report, steps)
	}
	steps = append(steps, onboardingStepDTO{
		Name: onboardingStepInstallWarden, OK: true,
		Reason: "this machine's warden is installed",
		Detail: res.Log,
	})
	return s.wakeAssistantStep(run, report, steps)
}

// wakeAssistantStep is steps 2+3 — reachability wait, then the wake. Split out
// so the "a warden is already installed here" path (the safety interlock) and
// the "we just installed one" path run the SAME wake, not two copies of it.
func (s *apiServer) wakeAssistantStep(
	run onboardingRunner, report onboardingReportDTO, steps []onboardingStepDTO,
) onboardingReportDTO {
	// ── step 2: wait for the warden to actually be REACHABLE ─────────────────
	// Installed ≠ connected. A START handed to a warden with no live SSE
	// downstream is dropped fail-closed, so waking before the connect lands
	// would reliably produce exactly the silent non-wake we are removing.
	deadline := run.now() + run.waitBudget.Seconds()
	online := run.wardenOnline(ServerSelfHost)
	for !online && run.now() < deadline {
		run.sleep(wardenOnlinePoll)
		online = run.wardenOnline(ServerSelfHost)
	}

	// ── step 3: bring the seeded assistant online ────────────────────────────
	mira, err := s.dal.GetMember(seedMiraID)
	if err != nil || mira == nil {
		steps = append(steps, onboardingStepDTO{
			Name: onboardingStepWakeAssistant,
			Reason: "the seeded assistant is missing from the roster — the " +
				"out-of-box seed did not run; restart the server and try again",
		})
		return s.finishOnboarding(report, steps)
	}
	mira.StoppingSince = 0.0
	mira.WakingSince = 0.0
	mira.DesiredState = DesiredStateOnline
	if err := s.putMember(*mira, triggerServer); err != nil {
		steps = append(steps, onboardingStepDTO{
			Name:   onboardingStepWakeAssistant,
			Reason: "could not record the wake intent for the assistant: " + err.Error(),
		})
		return s.finishOnboarding(report, steps)
	}
	dec := s.reconcileMemberNow(mira.ID)
	if dec.DispatchUnlanded || !online {
		// The intent IS persisted and the cadence retries, so this is not a
		// rollback case — but it is emphatically not success either, and saying
		// so is the difference between "starting up" and "quietly nothing".
		steps = append(steps, onboardingStepDTO{
			Name: onboardingStepWakeAssistant,
			Reason: "the assistant is set to come online, but this machine's warden " +
				"has not connected back to the server yet, so nothing has been " +
				"dispatched — the server keeps retrying; if she stays offline, " +
				"check the warden log (ocwarden.err.log)",
		})
		return s.finishOnboarding(report, steps)
	}
	steps = append(steps, onboardingStepDTO{
		Name: onboardingStepWakeAssistant, OK: true,
		Reason: "the assistant is waking on this machine",
	})
	return s.finishOnboarding(report, steps)
}

// finishOnboarding stamps the terminal state and persists the report. The state
// is derived from the steps — every step ok ⇒ ok, otherwise failed — so a step
// can never be recorded as a failure while the report claims success.
func (s *apiServer) finishOnboarding(report onboardingReportDTO, steps []onboardingStepDTO) onboardingReportDTO {
	report.Steps = steps
	report.FinishedAt = nowSecs()
	report.State = onboardingStateOK
	for _, st := range steps {
		if !st.OK {
			report.State = onboardingStateFailed
			break
		}
	}
	if len(steps) == 0 {
		report.State = onboardingStateFailed
	}
	if err := s.putOnboardingReport(report); err != nil {
		onboardingLog("could not persist the onboarding report: %v", err)
	}
	for _, st := range steps {
		onboardingLog("step %s ok=%v — %s", st.Name, st.OK, st.Reason)
	}
	return report
}

func (s *apiServer) putOnboardingReport(report onboardingReportDTO) error {
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return s.dal.PutSetting(settingOnboardingReport, string(raw))
}

// onboardingReport reads the stored report (nil = onboarding never ran, or the
// stored blob is unreadable — an honest absence, never a fabricated success).
func (s *apiServer) onboardingReport() *onboardingReportDTO {
	if s.dal == nil {
		return nil
	}
	raw, err := s.dal.GetSetting(settingOnboardingReport)
	if err != nil || raw == nil {
		return nil
	}
	var report onboardingReportDTO
	if json.Unmarshal([]byte(*raw), &report) != nil {
		return nil
	}
	return &report
}

func onboardingLog(format string, args ...any) {
	reconcileLog("[onboarding] "+format, args...)
}
