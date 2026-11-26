package main

// auto_update.go — the background self-upgrade (owner decision D2; the
// `updater.auto_update` setting: a never-set row reads off, and a release
// build's first-run claim turns it on). An armed tick runs upgrade.go's
// runUpgrade — the same verified body as the owner's POST /api/update/upgrade.
// Agents cannot reach its effect: the manual route stays MCPExclude and this
// loop reads only the owner-written setting.

import (
	"log"
	"os"
	"strings"
	"time"

	"ocserverd/txguard"
)

// autoUpdateCadence: the underlying GitHub check is cached for
// updater_check_interval_secs, so most ticks cost a few mutex reads and nothing else.
const autoUpdateCadence = time.Minute

// A failed attempt re-downloads the whole release, so consecutive failures
// for the same tag wait 5m, 10m, 20m … up to 6h instead of retrying every tick.
const (
	autoUpdateBackoffBase = 5 * time.Minute
	autoUpdateBackoffMax  = 6 * time.Hour
)

// upgradeHandoffEnv carries "<version before> <tag installed>" across the
// re-exec, so the next process can tell whether the upgrade actually took.
const upgradeHandoffEnvKey = "OC_UPGRADE_HANDOFF"

// autoUpdateGate is in memory on purpose: a supervisor restart starts clean,
// and the one restart that must remember (the re-exec) carries the handoff.
type autoUpdateGate struct {
	mu txguard.Mutex
	// installed is the release this process already swapped onto disk; until
	// a restart loads it, every further attempt would only re-download and
	// overwrite the .bak rollback with the new build.
	installed string
	// held is a tag auto-update must not try again: a restart into it came
	// back on the old version.
	held      string
	failedTag string
	failures  int
	retryAt   time.Time
}

func autoUpdateBackoff(failures int) time.Duration {
	d := autoUpdateBackoffBase
	for i := 1; i < failures && d < autoUpdateBackoffMax; i++ {
		d *= 2
	}
	return min(d, autoUpdateBackoffMax)
}

func (s *apiServer) holdAutoUpdate(tag string) {
	g := &s.autoUpdateGate
	g.mu.Lock()
	defer g.mu.Unlock()
	g.held, g.failedTag, g.failures, g.retryAt = tag, "", 0, time.Time{}
}

func (s *apiServer) noteUpgradeInstalled(tag string) {
	g := &s.autoUpdateGate
	g.mu.Lock()
	defer g.mu.Unlock()
	g.installed, g.failedTag, g.failures, g.retryAt = tag, "", 0, time.Time{}
}

func (s *apiServer) upgradeInstalledAwaitingRestart() string {
	g := &s.autoUpdateGate
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.installed
}

// resetAutoUpdateGate: the owner's manual upgrade click re-arms auto-update.
// It leaves installed alone: only a restart clears that.
func (s *apiServer) resetAutoUpdateGate() {
	g := &s.autoUpdateGate
	g.mu.Lock()
	defer g.mu.Unlock()
	g.held, g.failedTag, g.failures, g.retryAt = "", "", 0, time.Time{}
}

func (s *apiServer) autoUpdateAllowed(latest string, now time.Time) bool {
	g := &s.autoUpdateGate
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.installed != "" || latest == g.held {
		return false
	}
	if latest != g.failedTag {
		g.failedTag, g.failures, g.retryAt = "", 0, time.Time{}
		return true
	}
	return !now.Before(g.retryAt)
}

func (s *apiServer) noteAutoUpdateFailure(tag string, now time.Time) time.Time {
	g := &s.autoUpdateGate
	g.mu.Lock()
	defer g.mu.Unlock()
	if tag != g.failedTag {
		g.failedTag, g.failures = tag, 0
	}
	g.failures++
	g.retryAt = now.Add(autoUpdateBackoff(g.failures))
	return g.retryAt
}

func upgradeHandoffEnv(environ []string, from, tag string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, upgradeHandoffEnvKey+"=") {
			out = append(out, kv)
		}
	}
	return append(out, upgradeHandoffEnvKey+"="+from+" "+tag)
}

// adoptUpgradeHandoff runs once at startup with the inherited handoff (""
// after a normal start).
func (s *apiServer) adoptUpgradeHandoff(handoff, running string) {
	from, tag, ok := strings.Cut(handoff, " ")
	if !ok || from != running {
		return
	}
	log.Printf("[auto-update] CRITICAL: restarted after installing %s but this process still runs %s — the new binary did not take over; auto-update will not retry %s until a newer release or a manual upgrade (restarting the service also loads it)",
		tag, running, tag)
	s.holdAutoUpdate(tag)
}

// Unset after reading so agents and helpers spawned from this process do not
// inherit it.
func (s *apiServer) adoptInheritedUpgradeHandoff() {
	s.adoptUpgradeHandoff(os.Getenv(upgradeHandoffEnvKey), appVersion)
	_ = os.Unsetenv(upgradeHandoffEnvKey)
}

func (s *apiServer) autoUpdateEnabled() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.updaterAutoUpdate
}

// startAutoUpdateCadence is always mounted: the toggle gates ACTION, not the
// loop, so flipping it on via PATCH /api/settings works without a restart.
func (s *apiServer) startAutoUpdateCadence(interval time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			surviveLockInTx("auto-update cadence", func() { s.autoUpdateTick() })
		}
	}()
}

func (s *apiServer) autoUpdateTick() (acted bool) {
	return s.autoUpdateTickAt(time.Now())
}

func (s *apiServer) autoUpdateTickAt(now time.Time) (acted bool) {
	if !s.autoUpdateEnabled() {
		return false
	}
	available, latest := s.updateStatus()
	if !available {
		return false
	}
	target := derefOr(latest, "?")
	if !s.autoUpdateAllowed(target, now) {
		return false
	}
	version, exePath, fail := s.runUpgrade(false)
	if fail != nil && fail.busy {
		log.Printf("[auto-update] skipped this tick: %s", fail.message)
		return false
	}
	if fail != nil {
		retryAt := s.noteAutoUpdateFailure(target, now)
		log.Printf("[auto-update] upgrade to %s not performed: %s (next attempt after %s)",
			target, fail.message, retryAt.Format(time.RFC3339))
		return false
	}
	log.Printf("[auto-update] auto-update is ON — upgraded to %s, restarting", version)
	s.scheduleUpgradeRestart(exePath, version)
	return true
}

func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}
