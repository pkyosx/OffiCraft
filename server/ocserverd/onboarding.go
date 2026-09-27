package main

// onboarding.go — automatic first-run onboarding (T-ba62), started right after
// the initial password is set: install THIS host's warden through the same core
// the cockpit's 安裝 button drives (runWardenInstallHere), wait for it to
// connect, then wake the seeded assistant. If the install fails the wake is not
// attempted. Every step's verdict and reason is persisted as the onboarding
// report and served on the owner-gated settings read.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// settingOnboardingReport: absent = onboarding never ran (a database that
// predates T-ba62, or one that already had a password).
const settingOnboardingReport = "onboarding.first_run"

// Onboarding step names — stable machine keys, safe for a UI to branch on.
const (
	onboardingStepInstallWarden = "install_warden"
	onboardingStepWakeAssistant = "wake_assistant"
)

const (
	onboardingStateRunning = "running"
	onboardingStateOK      = "ok"
	onboardingStateFailed  = "failed"
)

// Onboarding failure CODES — a closed wire vocabulary: the cockpit owns the
// translated sentence (same split as backup_health.go's `code` ↔
// frontend/src/lib/backupHealth.ts). `Reason` stays as the fallback a cockpit
// that does not know a code renders verbatim.
const (
	onboardingCodeInstallFailed       = "install_failed"
	onboardingCodeInstallerUnrunnable = "installer_unrunnable"
	onboardingCodeUninstallIntent     = "uninstall_intent"
	onboardingCodeRosterMissing       = "roster_missing"
	onboardingCodeAssistantMissing    = "assistant_missing"
	onboardingCodeWakeNotRecorded     = "wake_not_recorded"
	onboardingCodeWakeUndispatched    = "wake_undispatched"
	onboardingCodeInterrupted         = "interrupted"
	onboardingCodeFaulted             = "faulted"
)

// wardenOnlineWait: a fresh warden connects in about a second; 30s tolerates a
// slow launchd start yet reports a broken one while the owner is still looking.
// Exceeding it is reported, not fatal — the reconcile cadence keeps retrying.
const wardenOnlineWait = 30 * time.Second

const wardenOnlinePoll = 500 * time.Millisecond

// wardenAlreadyInstalledHere is a SAFETY INTERLOCK: onboarding drives
// `ocwarden install --force`, and a launchd label is a singleton in the user's
// GUI domain keyed on uid (not $HOME), so any ocserverd reaching set-password on
// a fresh database (conformance, e2e, a scratch DB) would re-point the
// operator's REAL warden at itself. An automatic action never overwrites an
// existing install; that is what the explicit 安裝 button is for.
func (s *apiServer) wardenAlreadyInstalledHere(
	getenv func(string) string, stat func(string) (os.FileInfo, error), labelLoaded func(string) bool,
) bool {
	// (1) The label is the authoritative axis (uid, namespace). The tokfile is
	// keyed on (HOME, namespace), so alone it lets `HOME=/tmp/x ocserverd serve`
	// pass and boot out gui/<uid>/com.officraft.ocwarden.
	if labelLoaded != nil && labelLoaded(wardenLaunchdLabel(s.namespace)) {
		return true
	}
	// (2) The file axis is what `ocwarden install` itself treats as "a warden
	// lives here", and it still answers when launchd does not.
	home := getenv("HOME")
	if home == "" {
		return true // cannot tell where a warden would live ⇒ refuse to install
	}
	if stat == nil {
		stat = os.Stat
	}
	_, err := stat(wardenTokfilePath(home, s.namespace))
	return err == nil
}

// wardenTokfilePath / wardenLaunchdLabel MIRROR cli/ocwarden/namespace.go's
// tokfileFor + wardenLabelFor (separate go modules, cannot import); the copies
// are held against bin/tests/fixtures/namespace-axes.tsv. If they drift, the
// guard stats a path nobody writes, answers "no warden here", and onboarding
// installs a second warden on top of a live launchd job.
func wardenTokfilePath(home, namespace string) string {
	return filepath.Join(officraftRootPath(home, namespace), "warden", "exec-warden.tok")
}

func officraftRootPath(home, namespace string) string {
	if namespace == "" {
		return filepath.Join(home, ".officraft")
	}
	return filepath.Join(home, ".officraft-"+namespace)
}

func wardenLaunchdLabel(namespace string) string {
	if namespace == "" {
		return "com.officraft.ocwarden"
	}
	return "com.officraft.ocwarden." + namespace
}

func launchdLabelLoaded(label string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	return exec.CommandContext(ctx, "launchctl", "print", target).Run() == nil
}

// onboardingDisabled (OC_NO_ONBOARDING=1) is set by conformance/run.sh's oc_env
// only; the e2e suites pre-seed the password with the `ocserverd set-password`
// subcommand, which never reaches this path. Do not extend that list from
// memory.
func onboardingDisabled(getenv func(string) string) bool {
	return strings.TrimSpace(getenv("OC_NO_ONBOARDING")) == "1"
}

type onboardingRunner struct {
	installWarden func(machine Member) (bootstrapResultDTO, error)

	wardenOnline func(machineID string) bool

	wardenInstalled func() bool
	sleep           func(time.Duration)
	now             func() float64

	waitBudget time.Duration
}

// kickFirstRunOnboarding runs in the BACKGROUND because the set-password
// handler holds settingsMu for its whole body; an inline install plus the
// connect wait would block the owner's first request behind that lock. The
// cockpit reads the report from GET /api/settings.
func (s *apiServer) kickFirstRunOnboarding() {
	s.kickFirstRunOnboardingWith(s.newOnboardingRunner())
}

// kickFirstRunOnboardingWith exists so tests never touch the production seams:
// newOnboardingRunner execs `ocwarden install --force` against the launchd
// domain of whoever runs `go test`, and only a coincidence (an empty
// binCacheDir in the shared fixture) stops it today.
func (s *apiServer) kickFirstRunOnboardingWith(run onboardingRunner) {
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
					Code:   onboardingCodeFaulted,
					Reason: "onboarding faulted — see the server log",
				}})
			}
		}()
		s.runFirstRunOnboarding(run, running)
	}()
}

func (s *apiServer) newOnboardingRunner() onboardingRunner {
	return onboardingRunner{
		installWarden: func(machine Member) (bootstrapResultDTO, error) {
			binPath, err := s.resolveOcwardenBinaryFrom(bindistFS())
			if err != nil {
				return bootstrapResultDTO{}, err
			}
			return s.runWardenInstallHere(machine, binPath, s.selfBase)
		},
		wardenOnline: func(id string) bool { return s.hub.IsOnline(id) },
		wardenInstalled: func() bool {
			return s.wardenAlreadyInstalledHere(os.Getenv, os.Stat, launchdLabelLoaded)
		},
		sleep:      time.Sleep,
		now:        nowSecs,
		waitBudget: wardenOnlineWait,
	}
}

func (s *apiServer) runFirstRunOnboarding(run onboardingRunner, report onboardingReportDTO) onboardingReportDTO {
	steps := []onboardingStepDTO{}

	machine, err := s.dal.GetMember(ServerSelfHost)
	if err != nil || machine == nil {
		steps = append(steps, onboardingStepDTO{
			Name: onboardingStepInstallWarden,
			Code: onboardingCodeRosterMissing,
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
			Code:   onboardingCodeUninstallIntent,
			Reason: "could not clear a residual uninstall intent on this machine: " + err.Error(),
		})
		return s.finishOnboarding(report, steps)
	}
	if run.wardenInstalled() {
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
			Code:   onboardingCodeInstallerUnrunnable,
			Reason: "could not run the warden installer on this host: " + err.Error(),
		})
		return s.finishOnboarding(report, steps)
	}
	if !res.OK {
		steps = append(steps, onboardingStepDTO{
			Name: onboardingStepInstallWarden,
			Code: onboardingCodeInstallFailed,
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

func (s *apiServer) wakeAssistantStep(
	run onboardingRunner, report onboardingReportDTO, steps []onboardingStepDTO,
) onboardingReportDTO {
	// Installed ≠ connected: a START handed to a warden with no live SSE
	// downstream is dropped fail-closed.
	deadline := run.now() + run.waitBudget.Seconds()
	online := run.wardenOnline(ServerSelfHost)
	for !online && run.now() < deadline {
		run.sleep(wardenOnlinePoll)
		online = run.wardenOnline(ServerSelfHost)
	}

	mira, err := s.dal.GetMember(seedMiraID)
	if err != nil || mira == nil {
		steps = append(steps, onboardingStepDTO{
			Name: onboardingStepWakeAssistant,
			Code: onboardingCodeAssistantMissing,
			Reason: "the seeded assistant is missing from the roster — the " +
				"out-of-box seed did not run; restart the server and try again",
		})
		return s.finishOnboarding(report, steps)
	}
	mira.StoppingSince = 0.0
	mira.WakingSince = 0.0
	mira.DesiredState = DesiredStateOnline
	if err := s.persistMemberWindDownAnchors(*mira); err != nil {
		steps = append(steps, onboardingStepDTO{
			Name:   onboardingStepWakeAssistant,
			Code:   onboardingCodeWakeNotRecorded,
			Reason: "could not record the assistant's wind-down anchors: " + err.Error(),
		})
		return s.finishOnboarding(report, steps)
	}
	if err := s.putMember(*mira, triggerServer); err != nil {
		steps = append(steps, onboardingStepDTO{
			Name:   onboardingStepWakeAssistant,
			Code:   onboardingCodeWakeNotRecorded,
			Reason: "could not record the wake intent for the assistant: " + err.Error(),
		})
		return s.finishOnboarding(report, steps)
	}
	// POSITIVE determination: did a START go out (or is she already online)?
	// Listing failure modes (`dec.DispatchUnlanded || !online`) missed one —
	// an unbuildable start frame makes reconcileOne downgrade to none WITHOUT
	// setting DispatchUnlanded, which then reported "waking" with no frame sent.
	dec := s.reconcileMemberNow(mira.ID)
	if dec.Command != reconcileCmdStart && !run.wardenOnline(mira.ID) {
		steps = append(steps, onboardingStepDTO{
			Name: onboardingStepWakeAssistant,
			Code: onboardingCodeWakeUndispatched,
			Reason: "the assistant is set to come online, but no start command has " +
				"been dispatched yet (" + dec.Reason + ") — most often this " +
				"machine's warden has not connected back to the server. The server " +
				"keeps retrying; if she stays offline, check the warden log " +
				"(ocwarden.out.log)",
		})
		return s.finishOnboarding(report, steps)
	}
	steps = append(steps, onboardingStepDTO{
		Name: onboardingStepWakeAssistant, OK: true,
		Reason: "the assistant is waking on this machine",
	})
	return s.finishOnboarding(report, steps)
}

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

// recoverStaleOnboarding (once at serve start): a `running` report at boot is
// stale — its goroutine died with the process. Left alone, kick never re-runs
// and the banner never draws (non-terminal), so nothing signals anywhere. It is
// closed out as FAILED, not re-run: re-running would install a launchd job on
// every server start.
func (s *apiServer) recoverStaleOnboarding() {
	report := s.onboardingReport()
	if report == nil || report.State != onboardingStateRunning {
		return
	}
	report.Steps = append(report.Steps, onboardingStepDTO{
		Name: onboardingStepInstallWarden,
		Code: onboardingCodeInterrupted,
		Reason: "automatic first-run setup was interrupted (the server restarted " +
			"while it was still running), so it did not finish. Install this " +
			"machine from 監控 › 機器 › 「安裝」, then bring the assistant online.",
	})
	report.State = onboardingStateFailed
	report.FinishedAt = nowSecs()
	// This is the only path that edits the old blob instead of writing a fresh
	// DTO; an inherited stamp would publish a FAILED report already dismissed.
	report.DismissedAt = 0
	if err := s.putOnboardingReport(*report); err != nil {
		onboardingLog("could not close out the stale onboarding report: %v", err)
		return
	}
	onboardingLog("closed out a stale `running` report from a previous process (interrupted run)")
}

var errNoOnboardingBanner = errors.New(
	"no onboarding banner is up to dismiss — the first-run report is absent or not in a failed state")

// 🔴 ONLY a `failed` report can be dismissed (T-0648), and the read and the
// write share the caller's transaction. PATCH /api/settings floors at
// principalAdminAgent, so an admin assistant could send this while the first
// run is still `running`; a read-modify-write split across connections and
// interleaved with finishOnboarding would write back the pre-verdict copy — the
// failure erased, the report stranded in `running`, no banner, never re-run.
// The stamp rides on the report row on purpose: a newly written report resets
// it to 0.
func setOnboardingDismissedOn(tx *sql.Tx, dismissed bool) error {
	report := onboardingReportOn(tx)
	if report == nil || report.State != onboardingStateFailed {
		return errNoOnboardingBanner
	}
	report.DismissedAt = 0
	if dismissed {
		report.DismissedAt = nowSecs()
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return putSettingOn(tx, settingOnboardingReport, string(raw))
}

func (s *apiServer) putOnboardingReport(report onboardingReportDTO) error {
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return s.dal.PutSetting(settingOnboardingReport, string(raw))
}

func (s *apiServer) onboardingReport() *onboardingReportDTO {
	if s.dal == nil {
		return nil
	}
	return onboardingReportOn(s.dal.rdb)
}

func onboardingReportOn(q sqlRowQuerier) *onboardingReportDTO {
	raw, err := getSettingOn(q, settingOnboardingReport)
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
