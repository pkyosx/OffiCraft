package main

import (
	"reflect"
	"sort"

	"ocserverd/txguard"
)

// activityStore holds each session's latest derived activity, keyed on the
// verified token sub. In memory only, like the context gauge.
type activityStore struct {
	mu      txguard.Mutex
	entries map[string]memberActivityDTO
}

func newActivityStore() *activityStore {
	return &activityStore{entries: map[string]memberActivityDTO{}}
}

func (s *activityStore) Get(id string) (memberActivityDTO, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.entries[id]
	return a, ok
}

// Swap stores next and reports whether it differs from what it replaced,
// ignoring the server stamp. stampedStart says next.BusySince is the server's
// stand-in for a turn start the reporter did not send: an ongoing main turn
// keeps the start it already has, or every report of the same turn would
// restart its clock and re-announce it.
func (s *activityStore) Swap(id string, next memberActivityDTO, stampedStart bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, had := s.entries[id]
	if stampedStart && had && prev.BusySince != nil {
		next.BusySince = prev.BusySince
	}
	s.entries[id] = next
	if !had {
		return true
	}
	prev.TS, next.TS = 0, 0
	return !reflect.DeepEqual(prev, next)
}

func (s *activityStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
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
// stored activity only for the owner and only while presence is online.
func (s *apiServer) ownerActivity(isOwner bool, id, presence string) *memberActivityDTO {
	if !isOwner || presence != MemberPresenceOnline {
		return nil
	}
	a, ok := s.activity.Get(id)
	if !ok {
		return nil
	}
	return &a
}
