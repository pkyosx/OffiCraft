package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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

	// `claude auth login` never exits on stdin EOF, so this cap is the only thing
	// that ends a login nobody finishes. The server's idle cap (15 minutes,
	// server/ocserverd/runtime_login.go) must stay above it.
	loginProcessCap = 10 * time.Minute

	loginRenderPrefix = ".oc-login-env-"
)

var loginIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type LoginSeam interface {
	Start(loginID, runtime string)
	Code(loginID, code string)
	Cancel(loginID string)
}

type loginReport struct {
	LoginID string
	State   string
	AuthURL string
	Account *loginAccount
	Reason  string
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
	logf     func(string, ...any)

	mu       sync.Mutex
	sessions map[string]*loginSession
}

type loginSession struct {
	id   string
	proc *loginProc
	done chan struct{}

	mu        sync.Mutex
	code      string
	cancelled bool
	timedOut  bool
}

func newLoginRelay(prober *loginProber, progress loginReporter, logf func(string, ...any)) *loginRelay {
	return &loginRelay{
		prober:   prober,
		progress: progress,
		start:    startLoginProcess,
		remove:   os.Remove,
		cap:      loginProcessCap,
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
	if runtime != "claude" {
		r.fail(loginID, fmt.Sprintf("this warden cannot log in runtime %q", runtime))
		return
	}
	if r.session(loginID) != nil {
		return
	}
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
	script, rendered := r.prober.claudeCommand(bin, loginRenderPrefix+loginID, "auth login --claudeai")
	proc, err := r.start(r.prober.shell(), script)
	if err != nil {
		if rendered != "" {
			_ = r.remove(rendered)
		}
		r.fail(loginID, "the login process could not start: "+err.Error())
		return
	}
	s := &loginSession{id: loginID, proc: proc, done: make(chan struct{})}
	r.mu.Lock()
	r.sessions[loginID] = s
	r.mu.Unlock()
	r.log("%s: claude login process started", loginID)
	go r.watch(s, rendered)
}

func (r *loginRelay) watch(s *loginSession, rendered string) {
	var lastOut, lastErr string
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		sawURL := false
		scanner := bufio.NewScanner(s.proc.stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if !sawURL && strings.HasPrefix(line, claudeLoginURLPrefix) {
				sawURL = true
				url := strings.TrimSpace(strings.TrimPrefix(line, claudeLoginURLPrefix))
				r.log("%s: awaiting the code", s.id)
				r.relay(s, loginReport{LoginID: s.id, State: "awaiting_code", AuthURL: url})
				continue
			}
			lastOut = line
		}
	}()
	go func() {
		defer readers.Done()
		scanner := bufio.NewScanner(s.proc.stderr)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				lastErr = line
			}
		}
	}()
	timer := time.AfterFunc(r.cap, func() {
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
	cancelled, timedOut, code := s.cancelled, s.timedOut, s.code
	s.code = ""
	s.mu.Unlock()

	switch {
	case cancelled:
		r.log("%s: login process ended on cancel", s.id)
	case timedOut:
		r.log("%s: login process killed at its %s cap", s.id, r.cap)
		r.progress(loginReport{LoginID: s.id, State: "expired",
			Reason: fmt.Sprintf("no code arrived within %s", r.cap)})
	case waitErr == nil:
		r.log("%s: login process exited 0", s.id)
		r.concludeSucceeded(s.id)
	default:
		r.log("%s: login process exited (%v)", s.id, waitErr)
		reason := lastErr
		if reason == "" {
			reason = lastOut
		}
		if reason == "" {
			reason = "claude auth login exited: " + waitErr.Error()
		}
		if code != "" {
			reason = strings.ReplaceAll(reason, code, "…")
		}
		r.progress(loginReport{LoginID: s.id, State: "failed", Reason: reason})
	}
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
	s.code = code
	s.mu.Unlock()
	if _, err := io.WriteString(s.proc.stdin, code+"\n"); err != nil {
		r.log("%s: could not hand the code to the login process", loginID)
		s.proc.kill()
		return
	}
	r.log("%s: code handed to the login process", loginID)
	r.relay(s, loginReport{LoginID: loginID, State: "verifying"})
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
