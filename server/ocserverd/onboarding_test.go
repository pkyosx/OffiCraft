// Skeleton generated from server/ocserverd/onboarding.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWardenAlreadyInstalledHere(t *testing.T) {
	cases := []struct {
		name       string
		namespace  string
		home       string
		label      bool
		file       bool
		want       bool
		wantPath   string
		wantLabels int
	}{
		{name: "a loaded launchd label blocks installation", home: "/tmp/home", label: true, want: true, wantLabels: 1},
		{name: "an existing token file blocks installation", home: "/tmp/home", file: true, want: true, wantPath: "/tmp/home/.officraft/warden/exec-warden.tok", wantLabels: 1},
		{name: "an absent token file permits installation", home: "/tmp/home", want: false, wantPath: "/tmp/home/.officraft/warden/exec-warden.tok", wantLabels: 1},
		{name: "a namespaced token file uses the namespace root", namespace: "blue", home: "/tmp/home", file: true, want: true, wantPath: "/tmp/home/.officraft-blue/warden/exec-warden.tok", wantLabels: 1},
		{name: "an unknown home refuses installation", label: false, want: true, wantLabels: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &apiServer{namespace: tc.namespace}
			labelCalls := 0
			statCalls := 0
			got := api.wardenAlreadyInstalledHere(
				func(string) string { return tc.home },
				func(path string) (os.FileInfo, error) {
					statCalls++
					if path != tc.wantPath {
						t.Errorf("stat path = %q, want %q", path, tc.wantPath)
					}
					if tc.file {
						return nil, nil
					}
					return nil, os.ErrNotExist
				},
				func(label string) bool {
					labelCalls++
					want := wardenLaunchdLabel(tc.namespace)
					if label != want {
						t.Errorf("label = %q, want %q", label, want)
					}
					return tc.label
				},
			)
			if got != tc.want {
				t.Fatalf("wardenAlreadyInstalledHere() = %v, want %v", got, tc.want)
			}
			if labelCalls != tc.wantLabels {
				t.Fatalf("label calls = %d, want %d", labelCalls, tc.wantLabels)
			}
			wantStats := 1
			if tc.label || tc.home == "" {
				wantStats = 0
			}
			if statCalls != wantStats {
				t.Fatalf("stat calls = %d, want %d", statCalls, wantStats)
			}
		})
	}
}

func TestOfficraftRootPath(t *testing.T) {
	if got := officraftRootPath("/Users/eva", ""); got != "/Users/eva/.officraft" {
		t.Fatalf("default root = %q", got)
	}
	if got := officraftRootPath("/Users/eva", "blue"); got != "/Users/eva/.officraft-blue" {
		t.Fatalf("namespaced root = %q", got)
	}
}

func TestWardenLaunchdLabel(t *testing.T) {
	if got := wardenLaunchdLabel(""); got != "com.officraft.ocwarden" {
		t.Fatalf("default label = %q", got)
	}
	if got := wardenLaunchdLabel("blue"); got != "com.officraft.ocwarden.blue" {
		t.Fatalf("namespaced label = %q", got)
	}
}

func TestLaunchdLabelLoaded(t *testing.T) {
	if launchdLabelLoaded("com.officraft.test.definitely-not-installed") {
		t.Fatal("an absent launchd label was reported as loaded")
	}
}

func TestKickFirstRunOnboarding(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	api.kickFirstRunOnboarding()
	stored, err := d.GetSetting(settingOnboardingReport)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if stored != nil {
		t.Fatalf("disabled onboarding wrote a report: %q", *stored)
	}
}

func TestKickFirstRunOnboardingWith(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	t.Setenv("OC_NO_ONBOARDING", "")
	var installCalls atomic.Int32
	run := onboardingRunner{
		installWarden: func(Member) (bootstrapResultDTO, error) {
			installCalls.Add(1)
			return bootstrapResultDTO{}, errors.New("installer unavailable")
		},
		wardenInstalled: func() bool { return false },
	}
	api.kickFirstRunOnboardingWith(run)
	report := onboardingTestWaitForTerminalReport(t, api)
	if report.State != onboardingStateFailed || len(report.Steps) != 1 {
		t.Fatalf("report = %#v", report)
	}
	step := report.Steps[0]
	if step.Name != onboardingStepInstallWarden || step.Code != onboardingCodeInstallerUnrunnable || step.Reason != "could not run the warden installer on this host: installer unavailable" {
		t.Fatalf("step = %#v", step)
	}
	if got := installCalls.Load(); got != 1 {
		t.Fatalf("install calls = %d, want 1", got)
	}
	api.kickFirstRunOnboardingWith(run)
	time.Sleep(10 * time.Millisecond)
	if got := installCalls.Load(); got != 1 {
		t.Fatalf("second kick installed again: %d calls", got)
	}
}

func TestNewOnboardingRunner(t *testing.T) {
	api := &apiServer{hub: NewHub(), namespace: "blue"}
	run := api.newOnboardingRunner()
	if run.installWarden == nil || run.wardenOnline == nil || run.wardenInstalled == nil || run.sleep == nil || run.now == nil {
		t.Fatal("newOnboardingRunner left a production seam nil")
	}
	if run.waitBudget != wardenOnlineWait {
		t.Fatalf("wait budget = %v, want %v", run.waitBudget, wardenOnlineWait)
	}
}

func TestRunFirstRunOnboarding(t *testing.T) {
	t.Run("an unrunnable installer fails closed before waking the assistant", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.noReconcile = true
		run := onboardingRunner{
			wardenInstalled: func() bool { return false },
			installWarden: func(Member) (bootstrapResultDTO, error) {
				return bootstrapResultDTO{}, errors.New("no installer")
			},
		}
		got := api.runFirstRunOnboarding(run, onboardingReportDTO{StartedAt: 10})
		if got.State != onboardingStateFailed || len(got.Steps) != 1 {
			t.Fatalf("report = %#v", got)
		}
		if got.Steps[0].Code != onboardingCodeInstallerUnrunnable || got.Steps[0].Reason != "could not run the warden installer on this host: no installer" {
			t.Fatalf("step = %#v", got.Steps[0])
		}
		if stored := api.onboardingReport(); stored == nil || stored.State != onboardingStateFailed {
			t.Fatalf("stored report = %#v", stored)
		}
	})

	t.Run("an installed and reachable warden lets the seeded assistant wake", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		api.noReconcile = true
		run := onboardingRunner{
			wardenInstalled: func() bool { return true },
			wardenOnline:    func(string) bool { return true },
			sleep:           func(time.Duration) {},
			now:             func() float64 { return 100 },
			waitBudget:      time.Second,
		}
		got := api.runFirstRunOnboarding(run, onboardingReportDTO{StartedAt: 10})
		if got.State != onboardingStateOK || len(got.Steps) != 2 {
			t.Fatalf("report = %#v", got)
		}
		if !got.Steps[0].OK || got.Steps[0].Reason != "this machine already has a warden installed — left untouched" {
			t.Fatalf("install step = %#v", got.Steps[0])
		}
		if !got.Steps[1].OK || got.Steps[1].Reason != "the assistant is waking on this machine" {
			t.Fatalf("wake step = %#v", got.Steps[1])
		}
		m := apiTestMemberRow(t, d, seedMiraID)
		if m.DesiredState != DesiredStateOnline {
			t.Fatalf("Mira desired state = %q, want %q", m.DesiredState, DesiredStateOnline)
		}
	})
}

func TestWakeAssistantStep(t *testing.T) {
	t.Run("an unreachable warden records an undispatched wake after the bounded wait", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.noReconcile = true
		nowCalls := 0
		run := onboardingRunner{
			wardenOnline: func(string) bool { return false },
			sleep:        func(time.Duration) {},
			now: func() float64 {
				nowCalls++
				if nowCalls == 1 {
					return 100
				}
				return 102
			},
			waitBudget: time.Second,
		}
		got := api.wakeAssistantStep(run, onboardingReportDTO{StartedAt: 10}, nil)
		if got.State != onboardingStateFailed || len(got.Steps) != 1 {
			t.Fatalf("report = %#v", got)
		}
		step := got.Steps[0]
		if step.Name != onboardingStepWakeAssistant || step.Code != onboardingCodeWakeUndispatched || !strings.Contains(step.Reason, "no start command has been dispatched yet") {
			t.Fatalf("step = %#v", step)
		}
	})

	t.Run("a reachable warden records the wake as successful", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		api.noReconcile = true
		run := onboardingRunner{
			wardenOnline: func(string) bool { return true },
			sleep:        func(time.Duration) {},
			now:          func() float64 { return 100 },
			waitBudget:   time.Second,
		}
		got := api.wakeAssistantStep(run, onboardingReportDTO{StartedAt: 10}, nil)
		if got.State != onboardingStateOK || len(got.Steps) != 1 || !got.Steps[0].OK {
			t.Fatalf("report = %#v", got)
		}
		if got.Steps[0].Reason != "the assistant is waking on this machine" {
			t.Fatalf("step = %#v", got.Steps[0])
		}
	})
}

func TestFinishOnboarding(t *testing.T) {
	cases := []struct {
		name  string
		steps []onboardingStepDTO
		state string
	}{
		{name: "all steps succeeded", steps: []onboardingStepDTO{{Name: "install", OK: true}}, state: onboardingStateOK},
		{name: "one step failed", steps: []onboardingStepDTO{{Name: "install", Code: "failed"}}, state: onboardingStateFailed},
		{name: "no steps failed", state: onboardingStateFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, _, _, _ := newAPITestServer(t)
			got := api.finishOnboarding(onboardingReportDTO{StartedAt: 10}, tc.steps)
			if got.State != tc.state || got.FinishedAt <= 0 {
				t.Fatalf("report = %#v", got)
			}
			stored := api.onboardingReport()
			if stored == nil || stored.State != tc.state || len(stored.Steps) != len(tc.steps) {
				t.Fatalf("stored report = %#v", stored)
			}
		})
	}
}

func TestRecoverStaleOnboarding(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	if err := api.putOnboardingReport(onboardingReportDTO{State: onboardingStateRunning, StartedAt: 10, DismissedAt: 20}); err != nil {
		t.Fatalf("put report: %v", err)
	}

	api.recoverStaleOnboarding()
	got := api.onboardingReport()
	if got == nil || got.State != onboardingStateFailed || got.FinishedAt <= 0 || got.DismissedAt != 0 || len(got.Steps) != 1 {
		t.Fatalf("recovered report = %#v", got)
	}
	step := got.Steps[0]
	if step.Name != onboardingStepInstallWarden || step.Code != onboardingCodeInterrupted || !strings.Contains(step.Reason, "automatic first-run setup was interrupted") {
		t.Fatalf("recovery step = %#v", step)
	}
}

func TestSetOnboardingDismissed(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	if err := api.putOnboardingReport(onboardingReportDTO{State: onboardingStateFailed}); err != nil {
		t.Fatalf("put report: %v", err)
	}
	if err := api.setOnboardingDismissed(true); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	got := api.onboardingReport()
	if got == nil || got.DismissedAt <= 0 {
		t.Fatalf("dismissed report = %#v", got)
	}
	if err := api.setOnboardingDismissed(false); err != nil {
		t.Fatalf("clear dismissal: %v", err)
	}
	got = api.onboardingReport()
	if got == nil || got.DismissedAt != 0 {
		t.Fatalf("cleared report = %#v", got)
	}
	if err := api.putOnboardingReport(onboardingReportDTO{State: onboardingStateOK}); err != nil {
		t.Fatalf("put success report: %v", err)
	}
	if err := api.setOnboardingDismissed(true); !errors.Is(err, errNoOnboardingBanner) {
		t.Fatalf("dismiss non-failed report: %v", err)
	}
}

func TestPutOnboardingReport(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	want := onboardingReportDTO{
		State: onboardingStateFailed, StartedAt: 10, FinishedAt: 20, DismissedAt: 30,
		Steps: []onboardingStepDTO{{Name: onboardingStepInstallWarden, Code: onboardingCodeInstallFailed, Reason: "failed", Detail: "log"}},
	}
	if err := api.putOnboardingReport(want); err != nil {
		t.Fatalf("put report: %v", err)
	}
	got := api.onboardingReport()
	if got == nil {
		t.Fatal("put report was not readable")
	}
	apiTestWantEqual(t, "report", *got, want)
}

func TestOnboardingReport(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	if got := api.onboardingReport(); got != nil {
		t.Fatalf("absent report = %#v, want nil", got)
	}
	if err := d.PutSetting(settingOnboardingReport, "not json"); err != nil {
		t.Fatalf("put malformed report: %v", err)
	}
	if got := api.onboardingReport(); got != nil {
		t.Fatalf("malformed report = %#v, want nil", got)
	}
	want := onboardingReportDTO{State: onboardingStateRunning, StartedAt: 10}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := d.PutSetting(settingOnboardingReport, string(raw)); err != nil {
		t.Fatalf("put valid report: %v", err)
	}
	got := api.onboardingReport()
	if got == nil {
		t.Fatal("valid report = nil")
	}
	apiTestWantEqual(t, "valid report", *got, want)
}

func TestOnboardingLog(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = writer
	onboardingLog("setup %s", "failed")
	_ = writer.Close()
	os.Stderr = old
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if string(data) != "[reconcile] [onboarding] setup failed\n" {
		t.Fatalf("log = %q", data)
	}
}

func onboardingTestWaitForTerminalReport(t *testing.T, api *apiServer) onboardingReportDTO {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if report := api.onboardingReport(); report != nil && report.State != onboardingStateRunning {
			return *report
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("onboarding report did not reach a terminal state")
	return onboardingReportDTO{}
}
