package main

// auto_update.go — the OPT-IN background self-upgrade (owner decision D2; the
// `updater.auto_update` setting, default OFF). An armed tick runs upgrade.go's
// runUpgrade — the same verified body as the owner's POST /api/update/upgrade.
// Agents cannot reach its effect: the manual route stays MCPExclude and this
// loop reads only the owner-written setting.

import (
	"log"
	"time"
)

// autoUpdateCadence: the underlying GitHub check is cached with its own 5-minute
// TTL, so most ticks cost two mutex reads and nothing else.
const autoUpdateCadence = time.Minute

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
	if !s.autoUpdateEnabled() {
		return false
	}
	available, latest := s.updateStatus()
	if !available {
		return false
	}
	version, exePath, fail := s.runUpgrade()
	if fail != nil {
		log.Printf("[auto-update] upgrade to %s not performed: %s", derefOr(latest, "?"), fail.message)
		return false
	}
	log.Printf("[auto-update] auto-update is ON — upgraded to %s, restarting", version)
	s.scheduleUpgradeRestart(exePath)
	return true
}

func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}
