package main

import (
	"reflect"
	"time"
)

// Both live on the member's telemetry entry, so a re-exec forgets them like
// every other telemetry block (MemberDTO.model_call_last_success_ts says so).
const (
	modelCallSuccessKey = "model_call_last_success_ts"
	modelCallFailureKey = "model_call_last_failure"
)

func modelCallSuccessOf(entry map[string]any) float64 {
	ts, _ := entry[modelCallSuccessKey].(float64)
	return ts
}

func modelCallFailureOf(entry map[string]any) (modelCallFailureDTO, bool) {
	f, ok := entry[modelCallFailureKey].(modelCallFailureDTO)
	return f, ok
}

func validModelCallReport(report *ModelCallReportDTO) bool {
	return report.LastFailure == nil || report.LastFailure.Kind.Valid()
}

// mergeModelCall keeps the LARGEST time of each kind, which is what makes a
// repeated, reordered or lost report harmless. It answers whether anything
// was stored, and the failure when that was stored.
func mergeModelCall(entry map[string]any, report *ModelCallReportDTO) (bool, *modelCallFailureDTO) {
	changed := false
	if report.LastSuccessTs != nil && *report.LastSuccessTs > modelCallSuccessOf(entry) {
		entry[modelCallSuccessKey] = *report.LastSuccessTs
		changed = true
	}
	if f := report.LastFailure; f != nil {
		stored, has := modelCallFailureOf(entry)
		if !has || f.Ts > stored.Ts {
			next := modelCallFailureDTO{
				Ts: f.Ts, Kind: string(f.Kind), Code: f.Code, ResetsAt: copyFloat(f.ResetsAt),
			}
			entry[modelCallFailureKey] = next
			return true, &next
		}
	}
	return changed, nil
}

func copyFloat(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func (f modelCallFailureDTO) carriesReset() bool {
	return f.Kind == string(ModelCallFailureDTOKindRateLimit) && f.ResetsAt != nil
}

type modelCallAccount struct {
	lastSuccess float64
	limit       *modelCallFailureDTO
}

// modelCallBoard is one read's judgement over every member's two stored
// times: per-member warnings and per-account limits come from the same view.
type modelCallBoard struct {
	now      float64
	tele     map[string]map[string]any
	accounts map[string]modelCallAccount
}

func newModelCallBoard(members []Member, tele map[string]map[string]any, now float64) modelCallBoard {
	b := modelCallBoard{now: now, tele: tele, accounts: map[string]modelCallAccount{}}
	type limitCandidate struct {
		account string
		f       modelCallFailureDTO
	}
	var candidates []limitCandidate
	for _, m := range members {
		entry := tele[m.ID]
		account := telemetryAccount(entry, m.Runtime)
		if account == "" {
			continue
		}
		acct := b.accounts[account]
		if ts := modelCallSuccessOf(entry); ts > acct.lastSuccess {
			acct.lastSuccess = ts
		}
		b.accounts[account] = acct
		if f, ok := modelCallFailureOf(entry); ok && f.carriesReset() && now < *f.ResetsAt {
			candidates = append(candidates, limitCandidate{account: account, f: f})
		}
	}
	for _, c := range candidates {
		acct := b.accounts[c.account]
		if c.f.Ts <= acct.lastSuccess {
			continue
		}
		if acct.limit == nil || c.f.Ts > acct.limit.Ts {
			f := c.f
			acct.limit = &f
		}
		b.accounts[c.account] = acct
	}
	return b
}

func (b modelCallBoard) lastSuccess(memberID string) float64 {
	return modelCallSuccessOf(b.tele[memberID])
}

func (b modelCallBoard) limit(account string) *modelCallFailureDTO {
	return b.accounts[account].limit
}

func (b modelCallBoard) runtimeOf(m Member) string {
	if ValidRuntime(m.ActualRuntime) {
		return m.ActualRuntime
	}
	if reported, _ := b.tele[m.ID]["runtime"].(string); ValidRuntime(reported) {
		return reported
	}
	return NormalizeRuntime(m.Runtime)
}

func (b modelCallBoard) warnings(m Member, login []RuntimeLoginWarningDTO) []modelCallWarningDTO {
	out := []modelCallWarningDTO{}
	entry := b.tele[m.ID]
	account := telemetryAccount(entry, m.Runtime)
	runtime := b.runtimeOf(m)
	ownLimit := false
	if f, ok := modelCallFailureOf(entry); ok && f.Ts > modelCallSuccessOf(entry) && b.ownFailureShows(f, account, runtime, login) {
		ownLimit = f.carriesReset()
		out = append(out, modelCallWarningOf(f, runtime, false))
	}
	if limit := b.limit(account); limit != nil && !ownLimit {
		out = append(out, modelCallWarningOf(*limit, runtime, true))
	}
	return out
}

func (b modelCallBoard) ownFailureShows(f modelCallFailureDTO, account, runtime string, login []RuntimeLoginWarningDTO) bool {
	if f.carriesReset() {
		if b.now >= *f.ResetsAt {
			return false
		}
		// The account's limit is lifted for everyone the moment any member of the
		// account gets a call through.
		if account != "" && b.accounts[account].lastSuccess >= f.Ts {
			return false
		}
	}
	if f.Kind == string(ModelCallFailureDTOKindAuth) {
		for _, w := range login {
			if !w.Pending && string(w.Runtime) == runtime {
				return false
			}
		}
	}
	return true
}

func modelCallWarningOf(f modelCallFailureDTO, runtime string, accountWide bool) modelCallWarningDTO {
	return modelCallWarningDTO{
		AccountWide: accountWide,
		Code:        f.Code,
		Kind:        f.Kind,
		ResetsAt:    copyFloat(f.ResetsAt),
		Runtime:     runtime,
		SinceTs:     f.Ts,
	}
}

func (s *apiServer) modelCallClock() float64 {
	if s.modelCallNow != nil {
		return s.modelCallNow()
	}
	return nowSecs()
}

func (s *apiServer) modelCallAfter(d time.Duration, fire func()) {
	if s.modelCallAfterFunc != nil {
		s.modelCallAfterFunc(d, fire)
		return
	}
	time.AfterFunc(d, fire)
}

// modelCallMayChangeWarnings runs after the report's account pairing is
// applied to entry and BEFORE its model_call is merged; a pairing change is
// judged by the caller (modelCallPairingOf). A report that only moves the
// success time forward can change a warning only
// through a failure newer than the old success: the reporter's own, or an
// account limit in force. Every context report is such a report, so this is
// what keeps the roster diff off the common path. The account match is on the
// raw key, a superset of the board's runtime-checked pairing.
func (s *apiServer) modelCallMayChangeWarnings(entry map[string]any, report *ModelCallReportDTO) bool {
	if report.LastFailure != nil {
		return true
	}
	since := modelCallSuccessOf(entry)
	if f, ok := modelCallFailureOf(entry); ok && f.Ts > since {
		return true
	}
	account, _ := entry["account"].(string)
	if account == "" {
		return false
	}
	now := s.modelCallClock()
	return s.telemetry.Any(func(other map[string]any) bool {
		if raw, _ := other["account"].(string); raw != account {
			return false
		}
		f, ok := modelCallFailureOf(other)
		return ok && f.carriesReset() && f.Ts > since && now < *f.ResetsAt
	})
}

type modelCallPairing struct {
	account, runtime string
}

// modelCallPairingOf is read before and after applyAccountReport: a member
// moving between accounts gains or loses that account's limit, and its
// success stops or starts lifting it, with or without a model_call.
func modelCallPairingOf(entry map[string]any) modelCallPairing {
	account, _ := entry["account"].(string)
	runtime, _ := entry[accountRuntimeKey].(string)
	return modelCallPairing{account: account, runtime: runtime}
}

func (s *apiServer) loadModelCallBoard() (modelCallBoard, error) {
	members, err := s.dal.ListMembers()
	if err != nil {
		return modelCallBoard{}, err
	}
	return newModelCallBoard(members, s.telemetry.Snapshot(), s.modelCallClock()), nil
}

// scheduleModelCallReset: nothing else happens at a reset time, so without
// this push a lifted limit stays on screen until some unrelated delta.
func (s *apiServer) scheduleModelCallReset(f modelCallFailureDTO) {
	if !f.carriesReset() {
		return
	}
	resetsAt := *f.ResetsAt
	wait := resetsAt - s.modelCallClock()
	if wait <= 0 {
		return
	}
	// One timer per reset time: the fire re-judges every member, so a second
	// one for the same instant would only repeat it.
	s.modelCallResetsMu.Lock()
	if s.modelCallResetsArmed == nil {
		s.modelCallResetsArmed = map[float64]bool{}
	}
	armed := s.modelCallResetsArmed[resetsAt]
	s.modelCallResetsArmed[resetsAt] = true
	s.modelCallResetsMu.Unlock()
	if armed {
		return
	}
	// One second past the reset: the timer runs on the monotonic clock and the
	// judgement on the wall clock, and firing a hair early would push nothing.
	s.modelCallAfter(time.Duration((wait+1)*float64(time.Second)), func() {
		s.modelCallResetsMu.Lock()
		delete(s.modelCallResetsArmed, resetsAt)
		s.modelCallResetsMu.Unlock()
		before := s.telemetry.Snapshot()
		if s.publishModelCallChanges(before, resetsAt-0.001, triggerServer) {
			s.hub.Publish("monitoring", "signal", "monitoring", "", nil, audienceOwnerOnly(), triggerServer)
		}
	})
}

// publishModelCallChanges sends a member patch to every row whose
// model_call_warnings differ between `before` (judged at beforeNow) and the
// store now; the warnings are derived, so no row write announces them. It
// answers whether any warning or account limit changed.
func (s *apiServer) publishModelCallChanges(before map[string]map[string]any, beforeNow float64, trigger string) bool {
	if s.modelCallDiffObserved != nil {
		s.modelCallDiffObserved()
	}
	members, err := s.dal.ListMembers()
	if err != nil {
		return false
	}
	aliases, err := s.dal.MachineDisplayNames()
	if err != nil {
		return false
	}
	dir := newMachineDirectory(members, aliases)
	was := newModelCallBoard(members, before, beforeNow)
	is := newModelCallBoard(members, s.telemetry.Snapshot(), s.modelCallClock())
	changed := !reflect.DeepEqual(accountLimits(was), accountLimits(is))
	for _, m := range members {
		row, pairs, listed := s.loginPairsOf(dir, m)
		if !listed {
			continue
		}
		login := s.runtimeLoginWarnings(dir, pairs)
		if reflect.DeepEqual(was.warnings(row, login), is.warnings(row, login)) {
			continue
		}
		changed = true
		s.publishMemberOwnerOnly(row, trigger)
	}
	return changed
}

func accountLimits(b modelCallBoard) map[string]modelCallFailureDTO {
	out := map[string]modelCallFailureDTO{}
	for account, acct := range b.accounts {
		if acct.limit != nil {
			out[account] = *acct.limit
		}
	}
	return out
}
