package main

// auto_update.go — the OPT-IN background self-upgrade cadence (owner decision
// D2, 2026-07-14; the `updater.auto_update` DB setting, default OFF).
//
// Shape: one goroutine ticks every autoUpdateCadence. A tick is a no-op
// unless the toggle is ON; an armed tick consults the CACHED update check
// (updateStatus — which itself kicks the background refresh when stale, so
// the cadence never blocks on the network either) and, when a newer version
// is known on the followed channel, runs upgrade.go's runUpgrade — the exact
// same precondition gates + verified execution body (pin → download → double
// digest verify → smoke test → .bak backup → atomic swap) as the owner's
// explicit POST /api/update/upgrade, then re-execs in place. TryLock inside
// runUpgrade means an auto tick can never race a manual click into a second
// concurrent swap — whoever loses reads an honest "already in progress".
//
// Honesty posture: this loop acts ONLY because the owner armed the toggle.
// Failures are logged loudly and retried on the natural cadence (the update
// check's own TTL keeps a dead updater from being hammered); nothing here
// escalates, retries in a tight loop, or falls back to unverified bytes.
// Agents cannot reach the toggle's effect: the manual route stays MCPExclude
// and this loop reads only the owner-written DB setting.

import (
	"log"
	"time"
)

// autoUpdateCadence is how often the armed loop re-evaluates. One minute:
// fast enough that "promote → prod picks it up" feels immediate-ish, slow
// enough to be noise-free (the underlying check is cached with its own
// 5-minute TTL, so most ticks cost two mutex reads and nothing else).
const autoUpdateCadence = time.Minute

// autoUpdateEnabled reads the live toggle under the settings snapshot lock.
func (s *apiServer) autoUpdateEnabled() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.updaterAutoUpdate
}

// startAutoUpdateCadence mounts the background loop (sleep-then-tick, like
// the reconcile/outsource cadences). Always mounted by cmdServe — the toggle
// gates ACTION, not the loop, so flipping it on via PATCH /api/settings works
// without a restart.
func (s *apiServer) startAutoUpdateCadence(interval time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			s.autoUpdateTick()
		}
	}()
}

// autoUpdateTick is ONE armed evaluation (split out for tests). acted=true
// means a verified swap landed and the restart was scheduled.
func (s *apiServer) autoUpdateTick() (acted bool) {
	if !s.autoUpdateEnabled() {
		return false
	}
	available, latest := s.updateStatus()
	if !available {
		return false
	}
	version, exePath, fail := s.runUpgrade()
	if fail != nil {
		// Loud, then wait out the cadence — a broken updater/download will
		// answer the same way until it is fixed, and the check cache's TTL
		// already rate-limits the network side.
		log.Printf("[auto-update] upgrade to %s not performed: %s", derefOr(latest, "?"), fail.message)
		return false
	}
	log.Printf("[auto-update] auto-update is ON — upgraded to %s, restarting", version)
	s.scheduleUpgradeRestart(exePath)
	return true
}

// derefOr is the tiny nil-safe string read for log lines.
func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}
