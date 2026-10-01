package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The runtime-login relay: the owner starts a login from the cockpit, the
// server sends login_start / login_code / login_cancel, and this file runs the
// runtime's own login process and reports its progress back. Neither the code
// nor the sign-in URL is ever written to a file or a log line.

const (
	rpcLoginStart  = "login_start"
	rpcLoginCode   = "login_code"
	rpcLoginCancel = "login_cancel"

	runtimeLoginPath = "/api/monitoring/runtime-login"

	claudeLoginURLPrefix = "If the browser didn't open, visit: "
	// claude prints this on stderr and keeps waiting for another line.
	claudeInvalidCodePrefix = "Invalid code"
	claudeLoginFailedPrefix = "Login failed:"

	// `claude auth login` never exits on stdin EOF, so this cap is the only thing
	// that ends a login nobody finishes. The server's idle cap (20 minutes,
	// server/ocserverd/runtime_login.go) must stay above it.
	loginProcessCap = 10 * time.Minute
	// codex prints a one-time code that lasts 15 minutes and times itself out
	// then; this backstop only has to outlast it.
	codexLoginProcessCap = 16 * time.Minute

	loginRenderPrefix = ".oc-login-env-"
)

var loginIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// claude prints the sign-in URL as an OSC 8 hyperlink even without a TTY.
var (
	osc8Link     = regexp.MustCompile(`\x1b\]8;[^;\x07\x1b]*;([^\x07\x1b]*)(?:\x07|\x1b\\)`)
	oscSequence  = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	csiSequence  = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	lone2charEsc = regexp.MustCompile(`\x1b[@-_]`)
)

func stripTerminalEscapes(line string) string {
	line = oscSequence.ReplaceAllString(line, "")
	line = csiSequence.ReplaceAllString(line, "")
	return lone2charEsc.ReplaceAllString(line, "")
}

// loginURLFromLine answers the URL on claude's sign-in line: the OSC 8 target
// when there is one, else the visible text.
func loginURLFromLine(raw string) (string, bool) {
	visible := strings.TrimSpace(stripTerminalEscapes(raw))
	if !strings.HasPrefix(visible, claudeLoginURLPrefix) {
		return "", false
	}
	for _, m := range osc8Link.FindAllStringSubmatch(raw, -1) {
		if target := strings.TrimSpace(m[1]); target != "" {
			return target, true
		}
	}
	return strings.TrimSpace(strings.TrimPrefix(visible, claudeLoginURLPrefix)), true
}

// The browser claude would open is on the warden's machine, where nobody is
// looking. ⚠️ BROWSER is the ONLY variable in which a login's environment is
// allowed to differ from a member's (TestLoginRunsUnderTheMemberSpawnEnvironment
// pins that): claude 2.1.286 opens `$BROWSER <url>` (else `open`), and BROWSER
// moves no credential. Anything else here would write the login where members
// cannot read it.
var loginBrowserOverride = [2]string{"BROWSER", "/usr/bin/true"}

type LoginSeam interface {
	Start(loginID, runtime string)
	Code(loginID, code string)
	Cancel(loginID string)
	Abandon()
}

type loginReport struct {
	LoginID   string
	State     string
	AuthURL   string
	UserCode  string
	ExpiresTS float64
	Account   *loginAccount
	Reason    string
}

// loginReporter answers the state the server now holds ("" when the POST did
// not land).
type loginReporter func(loginReport) string

type loginProc struct {
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader
	wait   func() error
	kill   func()
}

type loginStarter func(shell, script string) (*loginProc, error)

// startLoginProcess puts the login in its own process group so a kill reaches
// whatever claude itself started.
func startLoginProcess(shell, script string) (*loginProc, error) {
	cmd := exec.Command(shell, "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &loginProc{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		wait:   cmd.Wait,
		kill: func() {
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
				_ = cmd.Process.Kill()
			}
		},
	}, nil
}

type loginRelay struct {
	prober   *loginProber
	progress loginReporter
	start    loginStarter
	remove   func(string) error
	cap      time.Duration
	codexCap time.Duration
	now      func() time.Time
	logf     func(string, ...any)

	mu       sync.Mutex
	sessions map[string]*loginSession
}

// loginFlow is what one runtime's login CLI prints and how its end is read.
// line runs on the stdout and the stderr reader goroutines at once.
type loginFlow interface {
	line(s *loginSession, stderr bool, raw string)
	failureReason() string
	expiredReason(limit time.Duration) string
	conclude(s *loginSession)
}

type loginSession struct {
	id    string
	proc  *loginProc
	done  chan struct{}
	flow  loginFlow
	limit time.Duration

	mu        sync.Mutex
	codes     []string
	codeSent  bool
	cancelled bool
	timedOut  bool
}

// mask hides every code this login was handed, and each half of one, from a
// line about to leave the machine.
func (s *loginSession) mask(line string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, code := range s.codes {
		parts := []string{code}
		if left, right, ok := strings.Cut(code, "#"); ok {
			parts = append(parts, left, right)
		}
		for _, part := range parts {
			if len(part) >= 4 {
				line = strings.ReplaceAll(line, part, "…")
			}
		}
	}
	return line
}

func newLoginRelay(prober *loginProber, progress loginReporter, logf func(string, ...any)) *loginRelay {
	return &loginRelay{
		prober:   prober,
		progress: progress,
		start:    startLoginProcess,
		remove:   os.Remove,
		cap:      loginProcessCap,
		codexCap: codexLoginProcessCap,
		now:      time.Now,
		logf:     logf,
		sessions: map[string]*loginSession{},
	}
}

func (r *loginRelay) log(format string, args ...any) {
	if r.logf != nil {
		r.logf("[ocwarden login] "+format, args...)
	}
}

func (r *loginRelay) session(loginID string) *loginSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[loginID]
}

func (r *loginRelay) fail(loginID, reason string) {
	r.log("%s: failed before the login process ran", loginID)
	r.progress(loginReport{LoginID: loginID, State: "failed", Reason: reason})
}

func (r *loginRelay) Start(loginID, runtime string) {
	if r.session(loginID) != nil {
		return
	}
	switch runtime {
	case "claude":
		r.startClaude(loginID)
	case "codex":
		r.startCodex(loginID)
	default:
		r.fail(loginID, fmt.Sprintf("this warden cannot log in runtime %q", runtime))
	}
}

func (r *loginRelay) startClaude(loginID string) {
	bin := resolveClaudeBin(r.prober.env)
	if bin == "" {
		r.fail(loginID, "Claude Code is not installed on this machine")
		return
	}
	if r.prober.claudeHome.Home == "" {
		r.fail(loginID, "the warden cannot state the config home claude would use (HOME unresolved)")
		return
	}
	r.prober.prepareLaunchEnv()
	script, rendered := r.prober.claudeCommand(bin, loginRenderPrefix+loginID, "auth login --claudeai",
		claudeCommandOpts{selfDeleteRender: true, extra: [][2]string{loginBrowserOverride}})
	r.launch(loginID, "claude", script, rendered, &claudeLoginFlow{relay: r}, r.cap)
}

func (r *loginRelay) launch(loginID, runtime, script, rendered string, flow loginFlow, limit time.Duration) {
	proc, err := r.start(r.prober.shell(), script)
	if err != nil {
		if rendered != "" {
			_ = r.remove(rendered)
		}
		r.fail(loginID, "the login process could not start: "+err.Error())
		return
	}
	s := &loginSession{id: loginID, proc: proc, done: make(chan struct{}), flow: flow, limit: limit}
	r.mu.Lock()
	r.sessions[loginID] = s
	r.mu.Unlock()
	r.log("%s: %s login process started", loginID, runtime)
	go r.watch(s, rendered)
}

func (r *loginRelay) watch(s *loginSession, rendered string) {
	var readers sync.WaitGroup
	readers.Add(2)
	read := func(stream io.Reader, stderr bool) {
		defer readers.Done()
		scanner := bufio.NewScanner(stream)
		for scanner.Scan() {
			s.flow.line(s, stderr, scanner.Text())
		}
	}
	go read(s.proc.stdout, false)
	go read(s.proc.stderr, true)
	timer := time.AfterFunc(s.limit, func() {
		s.mu.Lock()
		s.timedOut = true
		s.mu.Unlock()
		s.proc.kill()
	})
	readers.Wait()
	waitErr := s.proc.wait()
	timer.Stop()
	if rendered != "" {
		_ = r.remove(rendered)
	}
	r.mu.Lock()
	delete(r.sessions, s.id)
	r.mu.Unlock()
	defer close(s.done)

	s.mu.Lock()
	cancelled, timedOut := s.cancelled, s.timedOut
	s.mu.Unlock()

	switch {
	case cancelled:
		r.log("%s: login process ended on cancel", s.id)
	case timedOut:
		r.log("%s: login process killed at its %s cap", s.id, s.limit)
		r.progress(loginReport{LoginID: s.id, State: "expired", Reason: s.flow.expiredReason(s.limit)})
	case waitErr == nil:
		r.log("%s: login process exited 0", s.id)
		s.flow.conclude(s)
	default:
		r.log("%s: login process exited (%v)", s.id, waitErr)
		reason := s.flow.failureReason()
		if reason == "" {
			reason = "the login process exited: " + waitErr.Error()
		}
		r.progress(loginReport{LoginID: s.id, State: "failed", Reason: s.mask(reason)})
	}
}

type claudeLoginFlow struct {
	relay *loginRelay

	mu          sync.Mutex
	sawURL      bool
	lastOut     string
	lastErr     string
	loginFailed string
}

func (f *claudeLoginFlow) line(s *loginSession, stderr bool, raw string) {
	r := f.relay
	if !stderr {
		f.mu.Lock()
		seen := f.sawURL
		f.mu.Unlock()
		if !seen {
			if url, ok := loginURLFromLine(raw); ok {
				f.mu.Lock()
				f.sawURL = true
				f.mu.Unlock()
				r.log("%s: awaiting the code", s.id)
				r.relay(s, loginReport{LoginID: s.id, State: "awaiting_code", AuthURL: url})
				return
			}
		}
	}
	line := strings.TrimSpace(stripTerminalEscapes(raw))
	if line == "" {
		return
	}
	f.mu.Lock()
	if stderr {
		f.lastErr = line
		if strings.HasPrefix(line, claudeLoginFailedPrefix) {
			f.loginFailed = line
		}
	} else {
		f.lastOut = line
	}
	f.mu.Unlock()
	if stderr && strings.HasPrefix(line, claudeInvalidCodePrefix) {
		s.mu.Lock()
		sent := s.codeSent
		s.codeSent = false
		s.mu.Unlock()
		if sent {
			r.log("%s: the login process refused the code; awaiting another", s.id)
			r.relay(s, loginReport{LoginID: s.id, State: "awaiting_code", Reason: s.mask(line)})
		}
	}
}

func (f *claudeLoginFlow) failureReason() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, reason := range []string{f.loginFailed, f.lastErr, f.lastOut} {
		if reason != "" {
			return reason
		}
	}
	return ""
}

func (f *claudeLoginFlow) expiredReason(limit time.Duration) string {
	return fmt.Sprintf("no code arrived within %s", limit)
}

func (f *claudeLoginFlow) conclude(s *loginSession) {
	f.relay.concludeSucceeded(s.id)
}

func (r *loginRelay) concludeSucceeded(loginID string) {
	verdict, account := r.prober.checkClaudeAccount()
	if verdict != nil && !*verdict {
		r.progress(loginReport{LoginID: loginID, State: "failed",
			Reason: "claude auth login finished but claude auth status still reports logged out"})
		return
	}
	rep := loginReport{LoginID: loginID, State: "succeeded"}
	if account.Email != "" || account.OrgName != "" {
		rep.Account = &account
	}
	r.progress(rep)
}

// relay is a report whose answer may say the owner already ended the login.
func (r *loginRelay) relay(s *loginSession, rep loginReport) {
	switch r.progress(rep) {
	case "cancelled", "expired", "failed", "succeeded":
		r.Cancel(s.id)
	}
}

func (r *loginRelay) Code(loginID, code string) {
	s := r.session(loginID)
	if s == nil {
		r.progress(loginReport{LoginID: loginID, State: "failed",
			Reason: "no login process for this login is running on the machine"})
		return
	}
	s.mu.Lock()
	s.codes = append(s.codes, code)
	s.codeSent = true
	s.mu.Unlock()
	// Reported BEFORE the write: claude's refusal of this code arrives on
	// stderr as an awaiting_code report, which must land after this one.
	r.relay(s, loginReport{LoginID: loginID, State: "verifying"})
	if _, err := io.WriteString(s.proc.stdin, code+"\n"); err != nil {
		r.log("%s: could not hand the code to the login process", loginID)
		s.proc.kill()
		return
	}
	r.log("%s: code handed to the login process", loginID)
}

// Abandon ends every login this process is running, before the warden execs
// itself in place: the exec would orphan them (and their 0600 env render).
func (r *loginRelay) Abandon() {
	r.mu.Lock()
	running := make([]*loginSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		running = append(running, s)
	}
	r.mu.Unlock()
	for _, s := range running {
		s.mu.Lock()
		s.cancelled = true
		s.mu.Unlock()
		s.proc.kill()
		r.progress(loginReport{LoginID: s.id, State: "failed",
			Reason: "the warden restarted to update itself; start the login again"})
	}
	for _, s := range running {
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
		}
	}
}

// sweepStaleRenders removes env renders a previous warden process left behind
// (it was exec'd or killed while a login ran): each is a 0600 copy of the env
// file's credentials.
func (r *loginRelay) sweepStaleRenders() {
	dir := r.prober.agentHome
	if dir == "" {
		return
	}
	matches, err := filepath.Glob(filepath.Join(dir, loginRenderPrefix+"*"))
	if err != nil {
		return
	}
	for _, path := range matches {
		_ = r.remove(path)
	}
	if len(matches) > 0 {
		r.log("removed %d env render(s) a previous warden process left behind", len(matches))
	}
}

func (r *loginRelay) Cancel(loginID string) {
	s := r.session(loginID)
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cancelled = true
	s.mu.Unlock()
	s.proc.kill()
}

func newLoginReporter(cfg Config) loginReporter {
	post := httpPoster(&http.Client{Timeout: commandReportTimeout}, cfg.Base, cfg.Token)
	return func(rep loginReport) string {
		if cfg.Token == "" {
			return ""
		}
		_, body := post(runtimeLoginPath, loginReportPayload(rep))
		state, _ := body["state"].(string)
		return state
	}
}

func loginReportPayload(rep loginReport) map[string]any {
	payload := map[string]any{"login_id": rep.LoginID, "state": rep.State}
	if rep.AuthURL != "" {
		payload["auth_url"] = rep.AuthURL
	}
	if rep.UserCode != "" {
		payload["user_code"] = rep.UserCode
	}
	if rep.ExpiresTS > 0 {
		payload["expires_ts"] = rep.ExpiresTS
	}
	if rep.Reason != "" {
		payload["reason"] = rep.Reason
	}
	if rep.Account != nil {
		account := map[string]any{}
		if rep.Account.Email != "" {
			account["email"] = rep.Account.Email
		}
		if rep.Account.OrgName != "" {
			account["org_name"] = rep.Account.OrgName
		}
		payload["account"] = account
	}
	return payload
}

var errLoginArgs = errors.New("command: login frame missing a valid login_id")

func loginIDFromArgs(args map[string]any) (string, error) {
	id, _ := argString(args, "login_id")
	if !loginIDPattern.MatchString(id) {
		return "", errLoginArgs
	}
	return id, nil
}
