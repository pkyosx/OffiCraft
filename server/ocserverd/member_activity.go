package main

import (
	"math"
	"reflect"
	"sort"

	"ocserverd/txguard"
)

// activityStore holds each session's latest derived activity, keyed on the
// verified token sub. In memory only, like the context gauge.
type activityStore struct {
	mu      txguard.Mutex
	entries map[string]activityEntry
	woken   map[string]activityWake
}

// activityEntry pairs a report with the iat of the token that sent it, so
// report_waking can tell the previous session's report from its own.
type activityEntry struct {
	activity memberActivityDTO
	iat      float64
}

// activityWake is a session's report_waking: the iat it raised the floor to and
// when, so a later wake dispatch (a newer waking_since) is not read as that
// session's.
type activityWake struct {
	iat, at float64
}

func newActivityStore() *activityStore {
	return &activityStore{entries: map[string]activityEntry{}, woken: map[string]activityWake{}}
}

func (s *activityStore) Get(id string) (memberActivityDTO, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	return e.activity, ok
}

// Swap stores next and reports whether it differs from what it replaced,
// ignoring the server stamp. stampedStart says next.BusySince is the server's
// stand-in for a turn start the reporter did not send: an ongoing main turn
// keeps the start it already has, or every report of the same turn would
// restart its clock and re-announce it. A report from a token older than the
// stored one's is dropped: an older session must not overwrite a newer one.
func (s *activityStore) Swap(id string, next memberActivityDTO, iat float64, stampedStart bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, had := s.entries[id]
	if had && iat < cur.iat {
		return false
	}
	prev := cur.activity
	if stampedStart && had && prev.BusySince != nil {
		next.BusySince = prev.BusySince
	}
	s.entries[id] = activityEntry{activity: next, iat: iat}
	if !had {
		return true
	}
	prev.TS, next.TS = 0, 0
	return !reflect.DeepEqual(prev, next)
}

// DeleteOlderThan drops id's entry when a token older than iat reported it.
// An iat <= 0 (none on the caller's token) drops it unconditionally.
func (s *activityStore) DeleteOlderThan(id string, iat float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok && (iat <= 0 || e.iat < iat) {
		delete(s.entries, id)
	}
}

func (s *activityStore) MarkWoken(id string, iat, at float64) {
	if iat <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.woken[id] = activityWake{iat: iat, at: at}
}

// GetWaking answers the stored activity only when the booting session sent it.
// The previous session's report_waking set floor to that session's own iat, so a
// report at the floor is this session's only once this session has itself
// reported waking since the wake was dispatched at wakingSince. A START's token is
// minted after the dispatch stamps waking_since, so a report on a token issued
// before that second is an earlier, aborted boot's.
func (s *activityStore) GetWaking(id string, floor, wakingSince float64) (memberActivityDTO, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return memberActivityDTO{}, false
	}
	if w, woke := s.woken[id]; woke && floor > 0 && e.iat == floor && w.iat == floor && w.at >= wakingSince {
		return e.activity, true
	}
	if e.iat > floor && e.iat >= math.Floor(wakingSince) {
		return e.activity, true
	}
	return memberActivityDTO{}, false
}

func deriveMemberActivity(busy bool, busySince *float64, subagents []activitySubagentDTO,
	waits []activityWaitDTO, ts float64,
) memberActivityDTO {
	sorted := append([]activitySubagentDTO{}, subagents...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartedTS < sorted[j].StartedTS })
	a := memberActivityDTO{
		State:     "idle",
		Subagents: sorted,
		Waits:     append([]activityWaitDTO{}, waits...),
		TS:        ts,
	}
	// A null busy_since under busy reads as "only sub-agents running" on the
	// wire, so a main turn without a reported start is dated by the report.
	if busy {
		a.BusySince = busySince
		if a.BusySince == nil {
			stamped := ts
			a.BusySince = &stamped
		}
	}
	switch {
	case busy || len(sorted) > 0:
		a.State = "busy"
	case len(waits) > 0:
		a.State = "waiting"
	}
	return a
}

// ownerActivity answers what MemberDTO.activity carries for this read: the
// stored activity only for the owner and only while the session is up and
// reporting (online or stopping), or waking with a report from the booting
// session itself.
func (s *apiServer) ownerActivity(isOwner bool, m Member, presence string) *memberActivityDTO {
	if !isOwner {
		return nil
	}
	var a memberActivityDTO
	var ok bool
	switch presence {
	case MemberPresenceOnline, MemberPresenceStopping:
		a, ok = s.activity.Get(m.ID)
	case MemberPresenceWaking:
		a, ok = s.activity.GetWaking(m.ID, m.AgentIatFloor, m.WakingSince)
	}
	if !ok {
		return nil
	}
	return &a
}
