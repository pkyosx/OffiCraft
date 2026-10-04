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
	"testing"
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

// After the process ended, its output pipes are closed once every reader has
// sat waiting with nothing to read for pipeDrainGrace, or pipeDrainCap after
// the exit whatever the readers are doing. Vars only so tests can shorten them.
var (
	pipeDrainGrace = 2 * time.Second
	// An escaped descendant that never stops writing keeps the readers busy, so
	// quiet alone could hold the login or upgrade open for as long as it lives.
	pipeDrainCap = 30 * time.Second
)

// refuseRealLoginInTest is the test-binary tripwire for the one process this
// file starts: a real `codex login --device-auth` asks OpenAI for a device
// code, and a real `claude auth login` opens a sign-in. Under `go test` only a
// fake staged in a temp dir may run.
func refuseRealLoginInTest(bin string) {
	refuseRealPathInTest("login", bin)
}

// refuseRealPathInTest exits a test binary handed a path outside the temp dir:
// a login binary that would really sign in, a claude that would really update
// itself, or a codex home whose auth.json a test would overwrite.
func refuseRealPathInTest(what, path string) {
	if !testing.Testing() {
		return
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		tmp = os.TempDir()
	}
	if strings.HasPrefix(resolveExisting(path), tmp+string(filepath.Separator)) {
		return
	}
	switch what {
	case "login":
		fmt.Fprintf(os.Stderr, "\nFATAL: refusing to run a real %s login inside a test binary.\n"+
			"Login tests must stage a fake CLI in a temp dir and inject it.\n", path)
	case "upgrade":
		fmt.Fprintf(os.Stderr, "\nFATAL: refusing to run a real %s update inside a test binary.\n"+
			"Upgrade tests must stage a fake CLI in a temp dir and inject it.\n", path)
	default:
		fmt.Fprintf(os.Stderr, "\nFATAL: refusing to write a real %s (%s) inside a test binary.\n", what, path)
	}
	os.Exit(1)
}

// resolveExisting resolves symlinks through the deepest ancestor of path that
// exists; a codex home may not exist yet.
func resolveExisting(path string) string {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			rel, _ := filepath.Rel(p, filepath.Clean(path))
			return filepath.Join(resolved, rel)
		}
		if filepath.Dir(p) == p {
			return path
		}
	}
}

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
// cannot read it. The one other difference is codex's staging CODEX_HOME, whose
// auth.json is installed into the member's CODEX_HOME on success
// (TestCodexLoginRunsUnderTheMemberSpawnEnvironment pins that).
var loginBrowserOverride = [2]string{"BROWSER", "/usr/bin/true"}

type LoginSeam interface {
	Start(loginID, runtime string)
	Code(loginID, code string)
	Cancel(loginID string)
	Abandon()
}

type loginReport struct {
	LoginID    string
	State      string
	AuthURL    string
	UserCode   string
	ExpiresInS float64
	Account    *loginAccount
	Reason     string
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

type loginStarter func(shell, script, bin string) (*loginProc, error)

func startLoginProcess(shell, script, bin string) (*loginProc, error) {
	refuseRealLoginInTest(bin)
	return startGroupProcess(shell, script)
}

// startGroupProcess puts the process in its own process group so a kill
// reaches whatever claude itself started.
//
// 🔴 Its output pipes are closed by this helper after the process ended (see
// pipeDrainGrace), not left to EOF: a descendant that escaped the group (its
// own session, or forked as the kill went out) can hold them open for as long
// as it lives, and a reader waiting on EOF would then keep the login or
// upgrade "running" forever, cap included. Quiet is judged by readers blocked
// in Read, not by time since the last byte: a reader busy with an earlier line
// (a progress POST) has not read what is still in the pipe yet.
func startGroupProcess(shell, script string) (*loginProc, error) {
	cmd := exec.Command(shell, "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	startErr := cmd.Start()
	outW.Close()
	errW.Close()
	if startErr != nil {
		outR.Close()
		errR.Close()
		return nil, startErr
	}
	stdout, stderr := &drainReader{f: outR}, &drainReader{f: errR}
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(exited)
		closeWhenDrained(stdout, stderr)
	}()
	return &loginProc{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		wait: func() error {
			<-exited
			return waitErr
		},
		kill: func() {
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
				_ = cmd.Process.Kill()
			}
		},
	}, nil
}

// drainReader records whether its reader is blocked waiting for output, and
// since when.
type drainReader struct {
	f *os.File

	mu      sync.Mutex
	waiting time.Time
	done    bool
}

func (d *drainReader) Read(p []byte) (int, error) {
	d.mu.Lock()
	d.waiting = time.Now()
	d.mu.Unlock()
	n, err := d.f.Read(p)
	d.mu.Lock()
	d.waiting = time.Time{}
	if err != nil {
		d.done = true
	}
	d.mu.Unlock()
	return n, err
}

func (d *drainReader) quiet(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.done || (!d.waiting.IsZero() && now.Sub(d.waiting) >= pipeDrainGrace)
}

func closeWhenDrained(readers ...*drainReader) {
	deadline := time.Now().Add(pipeDrainCap)
	tick := pipeDrainGrace / 10
	if tick <= 0 {
		tick = time.Millisecond
	}
	for {
		now := time.Now()
		all := true
		for _, r := range readers {
			all = all && r.quiet(now)
		}
		if all || !now.Before(deadline) {
			break
		}
		time.Sleep(tick)
	}
	for _, r := range readers {
		_ = r.f.Close()
	}
}

type loginRelay struct {
	prober      *loginProber
	progress    loginReporter
	start       loginStarter
	resolveBin  func(runtime string) string
	remove      func(string) error
	cap         time.Duration
	codexCap    time.Duration
	codeWait    time.Duration
	installAuth func(home string, auth []byte) error
	// Held across an install, so Abandon can wait one out before the warden
	// execs over it.
	installMu sync.Mutex
	logf      func(string, ...any)

	mu       sync.Mutex
	sessions map[string]*loginSession
}

// loginFlow is what one runtime's login CLI prints and how its end is read.
// line runs on the stdout and the stderr reader goroutines at once.
type loginFlow interface {
	line(s *loginSession, stderr bool, raw string)
	// failureReason explains a non-zero exit; "" leaves it to the exit itself.
	failureReason(waitErr error) string
	// failedState is the state a non-zero exit reports: codex's own expiry of
	// the one-time code is an exit 1 that means `expired`, not `failed`.
	failedState() string
	expiredReason(limit time.Duration) string
	conclude(s *loginSession)
	// finish runs once the login is over, however it ended.
	finish()
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
	r := &loginRelay{
		prober:   prober,
		progress: progress,
		start:    startLoginProcess,
		remove:   os.Remove,
		cap:      loginProcessCap,
		codexCap: codexLoginProcessCap,
		codeWait: codexCodeWait,
		logf:     logf,
		sessions: map[string]*loginSession{},
	}
	r.installAuth = installCodexAuth
	r.resolveBin = func(runtime string) string {
		if runtime == "codex" {
			return resolveCodexBin(prober.env)
		}
		return resolveClaudeBin(prober.env)
	}
	return r
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
	bin := r.resolveBin("claude")
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
	r.launch(loginID, "claude", bin, script, rendered, &claudeLoginFlow{relay: r}, r.cap)
}

func (r *loginRelay) launch(loginID, runtime, bin, script, rendered string, flow loginFlow, limit time.Duration) {
	proc, err := r.start(r.prober.shell(), script, bin)
	if err != nil {
		if rendered != "" {
			_ = r.remove(rendered)
		}
		flow.finish()
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
	defer s.flow.finish()

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
		reason := s.flow.failureReason(waitErr)
		if reason == "" {
			reason = "the login process exited: " + waitErr.Error()
		}
		r.progress(loginReport{LoginID: s.id, State: s.flow.failedState(), Reason: s.mask(reason)})
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

func (f *claudeLoginFlow) failureReason(error) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, reason := range []string{f.loginFailed, f.lastErr, f.lastOut} {
		if reason != "" {
			return reason
		}
	}
	return ""
}

func (f *claudeLoginFlow) failedState() string { return "failed" }

func (f *claudeLoginFlow) expiredReason(limit time.Duration) string {
	return fmt.Sprintf("no code arrived within %s", limit)
}

func (f *claudeLoginFlow) conclude(s *loginSession) {
	f.relay.concludeSucceeded(s.id)
}

func (f *claudeLoginFlow) finish() {}

func (r *loginRelay) concludeSucceeded(loginID string) {
	verdict, account := r.prober.checkClaudeAccount()
	if verdict != nil && !*verdict {
		r.progress(loginReport{LoginID: loginID, State: "failed",
			Reason: "claude auth login finished but claude auth status still reports logged out"})
		return
	}
	rep := loginReport{LoginID: loginID, State: "succeeded"}
	if account.Email != "" || account.OrgName != "" || account.Plan != "" {
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
	// A login past its process (concluding) is no longer in sessions. An install
	// that starts after this still leaves auth.json whole (one rename) and at
	// worst a temp file the next startup sweep removes.
	r.installMu.Lock()
	r.installMu.Unlock()
}

// sweepStaleLoginFiles removes what a previous warden process left behind when
// it was exec'd or killed while a login ran: env renders (each a 0600 copy of
// the env file's credentials) and codex staging homes.
func (r *loginRelay) sweepStaleLoginFiles() {
	dir := r.prober.agentHome
	if dir == "" {
		return
	}
	renders, _ := filepath.Glob(filepath.Join(dir, loginRenderPrefix+"*"))
	for _, path := range renders {
		_ = r.remove(path)
	}
	if len(renders) > 0 {
		r.log("removed %d env render(s) a previous warden process left behind", len(renders))
	}
	homes, _ := filepath.Glob(filepath.Join(dir, codexStagingPrefix+"*"))
	for _, path := range homes {
		sweepCodexStaging(path)
	}
	if len(homes) > 0 {
		r.log("removed %d codex login staging home(s) a previous warden process left behind", len(homes))
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
	if rep.ExpiresInS > 0 {
		payload["expires_in_s"] = rep.ExpiresInS
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
		if rep.Account.Plan != "" {
			account["plan"] = rep.Account.Plan
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
