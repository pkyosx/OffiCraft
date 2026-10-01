package main

import (
	"net/http"
	"strings"
	"time"
	"unicode"

	"ocserverd/txguard"
)

// The runtime-login relay. Nothing here may reach the DAL, member.last_op* or a
// log line: the code and the sign-in URL are credentials in flight, and the
// login frames are deliberately absent from persistableCommandVerb.

const (
	wardenCmdLoginStart  = "login_start"
	wardenCmdLoginCode   = "login_code"
	wardenCmdLoginCancel = "login_cancel"
)

const (
	runtimeLoginStarting     = "starting"
	runtimeLoginAwaitingCode = "awaiting_code"
	runtimeLoginAwaitingAuth = "awaiting_authorization"
	runtimeLoginVerifying    = "verifying"
	runtimeLoginSucceeded    = "succeeded"
	runtimeLoginFailed       = "failed"
	runtimeLoginExpired      = "expired"
	runtimeLoginCancelled    = "cancelled"
)

// The idle cap must stay above the warden's own login caps (10 minutes for
// claude, 16 for codex: cli/ocwarden/runtimelogin.go), so a live login reports
// its own end first.
const (
	runtimeLoginIdleExpiry  = 20 * time.Minute
	runtimeLoginTerminalTTL = 10 * time.Minute
	runtimeLoginSweepPeriod = time.Minute
)

const (
	runtimeLoginExpiredReason   = "no report from the machine for 20 minutes"
	runtimeLoginCancelledReason = "cancelled by the owner"
	runtimeLoginOfflineMsg      = "machine is offline; its warden cannot run a login"
	runtimeLoginPartialCodeMsg  = "code is incomplete: copy the whole code the sign-in page shows (two parts joined by '#')"
	runtimeLoginSpacedCodeMsg   = "code must not contain spaces, tabs or line breaks"
)

// runtimeLoginStateFits: the in-flight states each runtime's login can be in.
func runtimeLoginStateFits(runtime, state string) bool {
	if runtimeLoginTerminal(state) {
		return true
	}
	switch runtime {
	case RuntimeClaude:
		return state == runtimeLoginAwaitingCode || state == runtimeLoginVerifying
	case RuntimeCodex:
		return state == runtimeLoginAwaitingAuth
	}
	return false
}

func runtimeLoginTerminal(state string) bool {
	switch state {
	case runtimeLoginSucceeded, runtimeLoginFailed, runtimeLoginExpired, runtimeLoginCancelled:
		return true
	}
	return false
}

type runtimeLogin struct {
	id         string
	machineID  string
	runtime    string
	state      string
	authURL    *string
	userCode   *string
	expiresTS  *float64
	account    *runtimeLoginAccountDTO
	reason     *string
	changedAt  time.Time
	reportedAt time.Time
	endedAt    time.Time
}

func (l *runtimeLogin) dto() runtimeLoginDTO {
	return runtimeLoginDTO{
		LoginID:   l.id,
		MachineID: l.machineID,
		Runtime:   l.runtime,
		State:     l.state,
		AuthURL:   l.authURL,
		UserCode:  l.userCode,
		ExpiresTS: l.expiresTS,
		Account:   l.account,
		Reason:    l.reason,
		UpdatedTS: float64(l.changedAt.UnixNano()) / 1e9,
	}
}

func (l *runtimeLogin) moveTo(state string, now time.Time) {
	if l.state != state {
		l.changedAt = now
	}
	l.state = state
	if runtimeLoginTerminal(state) {
		l.endedAt = now
	}
}

type runtimeLoginStore struct {
	mu     txguard.Mutex
	now    func() time.Time
	logins map[string]*runtimeLogin
}

func newRuntimeLoginStore() *runtimeLoginStore {
	return &runtimeLoginStore{now: time.Now, logins: map[string]*runtimeLogin{}}
}

// sweepLocked answers the ids whose state or existence changed, so the caller
// can signal them.
func (st *runtimeLoginStore) sweepLocked(now time.Time) []string {
	var changed []string
	for id, l := range st.logins {
		switch {
		case runtimeLoginTerminal(l.state):
			if now.Sub(l.endedAt) >= runtimeLoginTerminalTTL {
				delete(st.logins, id)
				changed = append(changed, id)
			}
		case now.Sub(l.reportedAt) >= runtimeLoginIdleExpiry:
			reason := runtimeLoginExpiredReason
			l.reason = &reason
			l.moveTo(runtimeLoginExpired, now)
			changed = append(changed, id)
		}
	}
	return changed
}

func (st *runtimeLoginStore) sweep() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.sweepLocked(st.now())
}

// lookupLocked answers nil for a login on another machine as well as an
// unknown one: a caller must not learn that an id exists elsewhere.
func (st *runtimeLoginStore) lookupLocked(machineID, loginID string) *runtimeLogin {
	l := st.logins[loginID]
	if l == nil || l.machineID != machineID {
		return nil
	}
	return l
}

func (st *runtimeLoginStore) inFlightLocked(machineID, runtime string) *runtimeLogin {
	for _, l := range st.logins {
		if l.machineID == machineID && l.runtime == runtime && !runtimeLoginTerminal(l.state) {
			return l
		}
	}
	return nil
}

func (s *apiServer) publishRuntimeLogin(trigger string, ids ...string) {
	for _, id := range ids {
		s.hub.Publish("runtime_login", "signal", "runtime_login", id, nil, audienceOwnerOnly(), trigger)
	}
}

func (s *apiServer) startRuntimeLoginSweep(period time.Duration) {
	go func() {
		for {
			time.Sleep(period)
			s.sweepRuntimeLogins()
		}
	}()
}

func (s *apiServer) sweepRuntimeLogins() {
	s.publishRuntimeLogin(triggerServer, s.runtimeLogins.sweep()...)
}

func runtimeLoginNotFound(w http.ResponseWriter, loginID string) {
	writeError(w, http.StatusNotFound, "runtime login '"+loginID+"' not found")
}

func buildRuntimeLoginFrame(rpc string, args any) ([]byte, bool) {
	frame, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{RPC: rpc, Args: args})
	return frame, err == nil
}

type wardenLoginStartArgs struct {
	MemberID string `json:"member_id"`
	LoginID  string `json:"login_id"`
	Runtime  string `json:"runtime"`
}

// Code is a secret riding in the frame, like a START's member_token.
type wardenLoginCodeArgs struct {
	MemberID string `json:"member_id"`
	LoginID  string `json:"login_id"`
	Code     string `json:"code"`
}

type wardenLoginCancelArgs struct {
	MemberID string `json:"member_id"`
	LoginID  string `json:"login_id"`
}

func (s *apiServer) HandleStartRuntimeLoginApiMachinesMachineIdRuntimeLoginPost(w http.ResponseWriter, r *http.Request, machineId string) {
	var body RuntimeLoginStartDTO
	if !decodeJSONBodyRequired(w, r, &body, "runtime") {
		return
	}
	if !body.Runtime.Valid() {
		writeError(w, http.StatusUnprocessableEntity, "runtime must be 'claude' or 'codex'")
		return
	}
	if _, err := s.resolveMachine(machineId); err != nil {
		writeResolveError(w, err, "machine", machineId)
		return
	}
	runtime := string(body.Runtime)
	trigger := requestTrigger(r)

	st := s.runtimeLogins
	st.mu.Lock()
	now := st.now()
	swept := st.sweepLocked(now)
	if existing := st.inFlightLocked(machineId, runtime); existing != nil {
		out := existing.dto()
		st.mu.Unlock()
		s.publishRuntimeLogin(triggerServer, swept...)
		writeJSON(w, http.StatusOK, out)
		return
	}
	l := &runtimeLogin{
		id:         "rl-" + newHexID(16),
		machineID:  machineId,
		runtime:    runtime,
		state:      runtimeLoginStarting,
		changedAt:  now,
		reportedAt: now,
	}
	frame, built := buildRuntimeLoginFrame(wardenCmdLoginStart, wardenLoginStartArgs{
		MemberID: machineId, LoginID: l.id, Runtime: runtime,
	})
	if !built || !s.enqueueToWarden(machineId, machineId, frame) {
		st.mu.Unlock()
		s.publishRuntimeLogin(triggerServer, swept...)
		writeError(w, http.StatusConflict, runtimeLoginOfflineMsg)
		return
	}
	st.logins[l.id] = l
	out := l.dto()
	st.mu.Unlock()
	s.publishRuntimeLogin(triggerServer, swept...)
	s.publishRuntimeLogin(trigger, l.id)
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) HandleGetRuntimeLoginApiMachinesMachineIdRuntimeLoginLoginIdGet(w http.ResponseWriter, r *http.Request, machineId string, loginId string) {
	st := s.runtimeLogins
	st.mu.Lock()
	swept := st.sweepLocked(st.now())
	l := st.lookupLocked(machineId, loginId)
	var out runtimeLoginDTO
	if l != nil {
		out = l.dto()
	}
	st.mu.Unlock()
	s.publishRuntimeLogin(triggerServer, swept...)
	if l == nil {
		runtimeLoginNotFound(w, loginId)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) HandleSubmitRuntimeLoginCodeApiMachinesMachineIdRuntimeLoginLoginIdCodePost(w http.ResponseWriter, r *http.Request, machineId string, loginId string) {
	var body RuntimeLoginCodeDTO
	if !decodeJSONBodyRequired(w, r, &body, "code") {
		return
	}
	st := s.runtimeLogins
	st.mu.Lock()
	now := st.now()
	swept := st.sweepLocked(now)
	refuse := func(status int, msg string) {
		st.mu.Unlock()
		s.publishRuntimeLogin(triggerServer, swept...)
		writeError(w, status, msg)
	}
	l := st.lookupLocked(machineId, loginId)
	if l == nil {
		refuse(http.StatusNotFound, "runtime login '"+loginId+"' not found")
		return
	}
	if l.state != runtimeLoginAwaitingCode {
		refuse(http.StatusConflict, "runtime login '"+loginId+"' is "+l.state+", not awaiting_code")
		return
	}
	if !s.hub.IsOnline(machineId) {
		refuse(http.StatusConflict, runtimeLoginOfflineMsg)
		return
	}
	code := strings.TrimSpace(body.Code)
	if code == "" {
		refuse(http.StatusUnprocessableEntity, "code must not be blank")
		return
	}
	// The warden writes code+"\n" to the CLI's stdin, so an embedded line break
	// would arrive as two inputs.
	if strings.IndexFunc(code, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		refuse(http.StatusUnprocessableEntity, runtimeLoginSpacedCodeMsg)
		return
	}
	// claude's own check: a code without both halves is refused on its stderr
	// while the process keeps waiting, which a relayed frame cannot see.
	if left, right, found := strings.Cut(code, "#"); !found || left == "" || right == "" || strings.Contains(right, "#") {
		refuse(http.StatusUnprocessableEntity, runtimeLoginPartialCodeMsg)
		return
	}
	frame, built := buildRuntimeLoginFrame(wardenCmdLoginCode, wardenLoginCodeArgs{
		MemberID: machineId, LoginID: loginId, Code: code,
	})
	if !built || !s.enqueueToWarden(machineId, machineId, frame) {
		refuse(http.StatusConflict, runtimeLoginOfflineMsg)
		return
	}
	l.reason = nil
	l.moveTo(runtimeLoginVerifying, now)
	out := l.dto()
	st.mu.Unlock()
	s.publishRuntimeLogin(triggerServer, swept...)
	s.publishRuntimeLogin(requestTrigger(r), loginId)
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) HandleCancelRuntimeLoginApiMachinesMachineIdRuntimeLoginLoginIdCancelPost(w http.ResponseWriter, r *http.Request, machineId string, loginId string) {
	st := s.runtimeLogins
	st.mu.Lock()
	now := st.now()
	swept := st.sweepLocked(now)
	l := st.lookupLocked(machineId, loginId)
	if l == nil {
		st.mu.Unlock()
		s.publishRuntimeLogin(triggerServer, swept...)
		runtimeLoginNotFound(w, loginId)
		return
	}
	if runtimeLoginTerminal(l.state) {
		out := l.dto()
		st.mu.Unlock()
		s.publishRuntimeLogin(triggerServer, swept...)
		writeJSON(w, http.StatusOK, out)
		return
	}
	reason := runtimeLoginCancelledReason
	l.reason = &reason
	l.moveTo(runtimeLoginCancelled, now)
	out := l.dto()
	st.mu.Unlock()
	// Best effort: an offline warden has no login process left to kill once it
	// restarts, and a live one also kills it at its own 10-minute cap.
	if frame, built := buildRuntimeLoginFrame(wardenCmdLoginCancel, wardenLoginCancelArgs{
		MemberID: machineId, LoginID: loginId,
	}); built {
		s.enqueueToWarden(machineId, machineId, frame)
	}
	s.publishRuntimeLogin(triggerServer, swept...)
	s.publishRuntimeLogin(requestTrigger(r), loginId)
	writeJSON(w, http.StatusOK, out)
}

// Any caller that is not the login's own machine gets the same 404 as an unknown
// id; the caller is the verified sub, never a body field.
func (s *apiServer) HandleReportRuntimeLoginApiMonitoringRuntimeLoginPost(w http.ResponseWriter, r *http.Request) {
	var body RuntimeLoginReportDTO
	if !decodeJSONBodyRequired(w, r, &body, "login_id", "state") {
		return
	}
	if !body.State.Valid() {
		writeError(w, http.StatusUnprocessableEntity,
			"state must be one of awaiting_code, awaiting_authorization, verifying, succeeded, failed, expired, cancelled")
		return
	}
	caller := currentActor(r)
	if _, err := s.resolveMachine(caller); err != nil {
		runtimeLoginNotFound(w, body.LoginId)
		return
	}
	st := s.runtimeLogins
	st.mu.Lock()
	now := st.now()
	swept := st.sweepLocked(now)
	l := st.lookupLocked(caller, body.LoginId)
	if l == nil {
		st.mu.Unlock()
		s.publishRuntimeLogin(triggerServer, swept...)
		runtimeLoginNotFound(w, body.LoginId)
		return
	}
	if runtimeLoginTerminal(l.state) {
		out := l.dto()
		st.mu.Unlock()
		s.publishRuntimeLogin(triggerServer, swept...)
		writeJSON(w, http.StatusOK, out)
		return
	}
	state := string(body.State)
	if !runtimeLoginStateFits(l.runtime, state) {
		refused := l.runtime
		st.mu.Unlock()
		s.publishRuntimeLogin(triggerServer, swept...)
		writeError(w, http.StatusConflict,
			"state '"+state+"' does not belong to a "+refused+" login")
		return
	}
	l.reportedAt = now
	if body.AuthUrl != nil {
		url := *body.AuthUrl
		l.authURL = &url
	}
	if body.UserCode != nil {
		code := *body.UserCode
		l.userCode = &code
	}
	if body.ExpiresTs != nil {
		at := *body.ExpiresTs
		l.expiresTS = &at
	}
	if state == runtimeLoginSucceeded && body.Account != nil {
		l.account = &runtimeLoginAccountDTO{Email: body.Account.Email, OrgName: body.Account.OrgName}
	}
	switch {
	case state == runtimeLoginAwaitingCode:
		l.reason = nil
		if body.Reason != nil {
			reason := *body.Reason
			l.reason = &reason
		}
	case runtimeLoginTerminal(state) && state != runtimeLoginSucceeded && body.Reason != nil:
		reason := *body.Reason
		l.reason = &reason
	}
	l.moveTo(state, now)
	out := l.dto()
	st.mu.Unlock()
	s.publishRuntimeLogin(triggerServer, swept...)
	s.publishRuntimeLogin(requestTrigger(r), l.id)
	writeJSON(w, http.StatusOK, out)
}
