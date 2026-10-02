package main

import (
	"net/http"
	"time"

	"ocserverd/txguard"
)

// The runtime-upgrade relay: the owner asks a machine's warden to run
// `claude update`, the warden reports the version before and after. Held in
// memory only; the frame is deliberately absent from persistableCommandVerb.

const wardenCmdRuntimeUpgrade = "runtime_upgrade"

const (
	runtimeUpgradeStarting  = "starting"
	runtimeUpgradeRunning   = "running"
	runtimeUpgradeSucceeded = "succeeded"
	runtimeUpgradeFailed    = "failed"
	runtimeUpgradeExpired   = "expired"
)

// The idle cap must stay above the warden's own cap on `claude update`
// (cli/ocwarden/runtimeupgrade.go), so a live upgrade reports its own end first.
const (
	runtimeUpgradeIdleExpiry  = 20 * time.Minute
	runtimeUpgradeTerminalTTL = 10 * time.Minute
	runtimeUpgradeSweepPeriod = time.Minute
)

const (
	runtimeUpgradeExpiredReason = "no report from the machine for 20 minutes"
	runtimeUpgradeOfflineMsg    = "machine is offline; its warden cannot run an upgrade"
)

func runtimeUpgradeTerminal(state string) bool {
	switch state {
	case runtimeUpgradeSucceeded, runtimeUpgradeFailed, runtimeUpgradeExpired:
		return true
	}
	return false
}

type runtimeUpgrade struct {
	id          string
	machineID   string
	runtime     string
	state       string
	fromVersion *string
	toVersion   *string
	reason      *string
	startedAt   time.Time
	changedAt   time.Time
	reportedAt  time.Time
	endedAt     time.Time
}

func epochSecs(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }

func (u *runtimeUpgrade) dto() runtimeUpgradeDTO {
	return runtimeUpgradeDTO{
		UpgradeID:   u.id,
		MachineID:   u.machineID,
		Runtime:     u.runtime,
		State:       u.state,
		FromVersion: u.fromVersion,
		ToVersion:   u.toVersion,
		Reason:      u.reason,
		StartedTS:   epochSecs(u.startedAt),
		UpdatedTS:   epochSecs(u.changedAt),
	}
}

func (u *runtimeUpgrade) moveTo(state string, now time.Time) {
	if u.state != state {
		u.changedAt = now
	}
	u.state = state
	if runtimeUpgradeTerminal(state) {
		u.endedAt = now
	}
}

type runtimeUpgradeStore struct {
	mu       txguard.Mutex
	now      func() time.Time
	upgrades map[string]*runtimeUpgrade
}

func newRuntimeUpgradeStore() *runtimeUpgradeStore {
	return &runtimeUpgradeStore{now: time.Now, upgrades: map[string]*runtimeUpgrade{}}
}

// sweepLocked answers the ids whose state or existence changed, so the caller
// can signal them.
func (st *runtimeUpgradeStore) sweepLocked(now time.Time) []string {
	var changed []string
	for id, u := range st.upgrades {
		switch {
		case runtimeUpgradeTerminal(u.state):
			if now.Sub(u.endedAt) >= runtimeUpgradeTerminalTTL {
				delete(st.upgrades, id)
				changed = append(changed, id)
			}
		case now.Sub(u.reportedAt) >= runtimeUpgradeIdleExpiry:
			reason := runtimeUpgradeExpiredReason
			u.reason = &reason
			u.moveTo(runtimeUpgradeExpired, now)
			changed = append(changed, id)
		}
	}
	return changed
}

func (st *runtimeUpgradeStore) sweep() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.sweepLocked(st.now())
}

// lookupLocked answers nil for an upgrade on another machine as well as an
// unknown one: a caller must not learn that an id exists elsewhere.
func (st *runtimeUpgradeStore) lookupLocked(machineID, upgradeID string) *runtimeUpgrade {
	u := st.upgrades[upgradeID]
	if u == nil || u.machineID != machineID {
		return nil
	}
	return u
}

func (st *runtimeUpgradeStore) inFlightLocked(machineID, runtime string) *runtimeUpgrade {
	for _, u := range st.upgrades {
		if u.machineID == machineID && u.runtime == runtime && !runtimeUpgradeTerminal(u.state) {
			return u
		}
	}
	return nil
}

func (s *apiServer) publishRuntimeUpgrade(trigger string, ids ...string) {
	for _, id := range ids {
		s.hub.Publish("runtime_upgrade", "signal", "runtime_upgrade", id, nil, audienceOwnerOnly(), trigger)
	}
}

func (s *apiServer) startRuntimeUpgradeSweep(period time.Duration) {
	go func() {
		for {
			time.Sleep(period)
			s.sweepRuntimeUpgrades()
		}
	}()
}

func (s *apiServer) sweepRuntimeUpgrades() {
	s.publishRuntimeUpgrade(triggerServer, s.runtimeUpgrades.sweep()...)
}

func runtimeUpgradeNotFound(w http.ResponseWriter, upgradeID string) {
	writeError(w, http.StatusNotFound, "runtime upgrade '"+upgradeID+"' not found")
}

type wardenRuntimeUpgradeArgs struct {
	MemberID  string `json:"member_id"`
	UpgradeID string `json:"upgrade_id"`
	Runtime   string `json:"runtime"`
}

func (s *apiServer) HandleStartRuntimeUpgradeApiMachinesMachineIdRuntimeUpgradePost(w http.ResponseWriter, r *http.Request, machineId string) {
	var body RuntimeUpgradeStartDTO
	if !decodeJSONBodyRequired(w, r, &body, "runtime") {
		return
	}
	if !body.Runtime.Valid() {
		writeError(w, http.StatusUnprocessableEntity, "runtime must be 'claude'")
		return
	}
	if _, err := s.resolveMachine(machineId); err != nil {
		writeResolveError(w, err, "machine", machineId)
		return
	}
	runtime := string(body.Runtime)

	st := s.runtimeUpgrades
	st.mu.Lock()
	now := st.now()
	swept := st.sweepLocked(now)
	if existing := st.inFlightLocked(machineId, runtime); existing != nil {
		out := existing.dto()
		st.mu.Unlock()
		s.publishRuntimeUpgrade(triggerServer, swept...)
		writeJSON(w, http.StatusOK, out)
		return
	}
	u := &runtimeUpgrade{
		id:         "ru-" + newHexID(16),
		machineID:  machineId,
		runtime:    runtime,
		state:      runtimeUpgradeStarting,
		startedAt:  now,
		changedAt:  now,
		reportedAt: now,
	}
	frame, built := buildRuntimeLoginFrame(wardenCmdRuntimeUpgrade, wardenRuntimeUpgradeArgs{
		MemberID: machineId, UpgradeID: u.id, Runtime: runtime,
	})
	if !built || !s.enqueueToWarden(machineId, machineId, frame) {
		st.mu.Unlock()
		s.publishRuntimeUpgrade(triggerServer, swept...)
		writeError(w, http.StatusConflict, runtimeUpgradeOfflineMsg)
		return
	}
	st.upgrades[u.id] = u
	out := u.dto()
	st.mu.Unlock()
	s.publishRuntimeUpgrade(triggerServer, swept...)
	s.publishRuntimeUpgrade(requestTrigger(r), u.id)
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) HandleGetRuntimeUpgradeApiMachinesMachineIdRuntimeUpgradeUpgradeIdGet(w http.ResponseWriter, r *http.Request, machineId string, upgradeId string) {
	st := s.runtimeUpgrades
	st.mu.Lock()
	swept := st.sweepLocked(st.now())
	u := st.lookupLocked(machineId, upgradeId)
	var out runtimeUpgradeDTO
	if u != nil {
		out = u.dto()
	}
	st.mu.Unlock()
	s.publishRuntimeUpgrade(triggerServer, swept...)
	if u == nil {
		runtimeUpgradeNotFound(w, upgradeId)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// Any caller that is not the upgrade's own machine gets the same 404 as an
// unknown id; the caller is the verified sub, never a body field.
func (s *apiServer) HandleReportRuntimeUpgradeApiMonitoringRuntimeUpgradePost(w http.ResponseWriter, r *http.Request) {
	var body RuntimeUpgradeReportDTO
	if !decodeJSONBodyRequired(w, r, &body, "upgrade_id", "state") {
		return
	}
	if !body.State.Valid() {
		writeError(w, http.StatusUnprocessableEntity, "state must be one of running, succeeded, failed")
		return
	}
	caller := currentActor(r)
	if _, err := s.resolveMachine(caller); err != nil {
		runtimeUpgradeNotFound(w, body.UpgradeId)
		return
	}
	st := s.runtimeUpgrades
	st.mu.Lock()
	now := st.now()
	swept := st.sweepLocked(now)
	u := st.lookupLocked(caller, body.UpgradeId)
	if u == nil {
		st.mu.Unlock()
		s.publishRuntimeUpgrade(triggerServer, swept...)
		runtimeUpgradeNotFound(w, body.UpgradeId)
		return
	}
	if runtimeUpgradeTerminal(u.state) {
		out := u.dto()
		st.mu.Unlock()
		s.publishRuntimeUpgrade(triggerServer, swept...)
		writeJSON(w, http.StatusOK, out)
		return
	}
	state := string(body.State)
	u.reportedAt = now
	if u.fromVersion == nil && body.FromVersion != nil {
		from := *body.FromVersion
		u.fromVersion = &from
	}
	if runtimeUpgradeTerminal(state) && body.ToVersion != nil {
		to := *body.ToVersion
		u.toVersion = &to
	}
	if state == runtimeUpgradeFailed && body.Reason != nil {
		reason := *body.Reason
		u.reason = &reason
	}
	u.moveTo(state, now)
	out := u.dto()
	st.mu.Unlock()
	s.publishRuntimeUpgrade(triggerServer, swept...)
	s.publishRuntimeUpgrade(requestTrigger(r), u.id)
	writeJSON(w, http.StatusOK, out)
}
