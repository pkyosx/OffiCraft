package main

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// `codex login --device-auth` (codex 0.159) prints, in color:
//
//  1. Open this link in your browser and sign in to your account
//     https://auth.openai.com/codex/device
//  2. Enter this one-time code (expires in 15 minutes)
//     ABCD-EFGHI
//
// and then waits until the code is approved on that page (exit 0) or expires:
// exit 1 with "Error logging in with device code: device auth timed out after
// 15 minutes". It reads nothing from stdin.
var (
	codexDeviceCodeLine = regexp.MustCompile(`^[A-Z0-9]{4,}-[A-Z0-9]{4,}$`)
	codexExpiresIn      = regexp.MustCompile(`expires in (\d+) minutes?`)
	codexFailurePrefix  = []string{"Error logging in with device code:", "Codex is not enabled"}
)

const (
	codexDeviceLoginCommand = "login --device-auth"
	codexLoggedOutReason    = "codex login status still reports logged out after the login finished"
	codexNoCodeReason       = "could not read the one-time code from codex's output"
	codexDeviceURLPrefix    = "https://auth.openai.com/"
	// Only display claims are read from auth.json; a larger file is cut here,
	// and a cut file no longer parses, so it yields no account.
	codexAuthFileMax = 1 << 20
	// The code is printed a line or two after the URL; past this the output is
	// not one this warden can read, and the owner should hear so at once.
	codexCodeWait = 5 * time.Second
)

type codexLoginFlow struct {
	relay *loginRelay

	mu        sync.Mutex
	url       string
	userCode  string
	minutes   int
	reported  bool
	waiting   bool
	lastOut   string
	lastErr   string
	preferred string
}

func (r *loginRelay) startCodex(loginID string) {
	bin := r.resolveBin("codex")
	if bin == "" {
		r.fail(loginID, "Codex is not installed on this machine")
		return
	}
	r.prober.prepareLaunchEnv()
	script, rendered := r.prober.codexCommand(loginRenderPrefix+loginID,
		"exec "+shellQuote(bin)+" "+codexDeviceLoginCommand,
		claudeCommandOpts{selfDeleteRender: true, extra: [][2]string{loginBrowserOverride}})
	r.launch(loginID, "codex", bin, script, rendered, &codexLoginFlow{relay: r}, r.codexCap)
}

func (f *codexLoginFlow) line(s *loginSession, stderr bool, raw string) {
	line := strings.TrimSpace(stripTerminalEscapes(raw))
	if line == "" {
		return
	}
	f.mu.Lock()
	if stderr {
		f.lastErr = line
	} else {
		f.lastOut = line
	}
	for _, prefix := range codexFailurePrefix {
		if strings.HasPrefix(line, prefix) {
			f.preferred = line
		}
	}
	if f.preferred == "" && strings.Contains(strings.ToLower(line), "device code") &&
		!strings.HasPrefix(line, "Follow these steps") {
		f.preferred = line
	}
	switch {
	case f.url == "" && strings.HasPrefix(line, codexDeviceURLPrefix):
		f.url = line
	case f.userCode == "" && codexDeviceCodeLine.MatchString(line):
		f.userCode = line
	}
	if m := codexExpiresIn.FindStringSubmatch(line); m != nil {
		f.minutes, _ = strconv.Atoi(m[1])
	}
	ready := !f.reported && f.url != "" && f.userCode != ""
	if ready {
		f.reported = true
	}
	startWait := f.url != "" && f.userCode == "" && !f.waiting
	if startWait {
		f.waiting = true
	}
	url, code, minutes := f.url, f.userCode, f.minutes
	f.mu.Unlock()
	if startWait {
		time.AfterFunc(f.relay.codeWait, func() { f.noCode(s) })
	}
	if !ready {
		return
	}
	rep := loginReport{LoginID: s.id, State: "awaiting_authorization", AuthURL: url, UserCode: code}
	if minutes > 0 {
		rep.ExpiresInS = float64(minutes * 60)
	}
	f.relay.log("%s: awaiting authorization", s.id)
	f.relay.relay(s, rep)
}

func (f *codexLoginFlow) noCode(s *loginSession) {
	if f.relay.session(s.id) != s {
		return
	}
	f.mu.Lock()
	missing := !f.reported
	if missing {
		f.reported = true
	}
	f.mu.Unlock()
	if !missing {
		return
	}
	f.relay.log("%s: codex printed a sign-in URL but no one-time code this warden can read", s.id)
	f.relay.relay(s, loginReport{LoginID: s.id, State: "failed", Reason: codexNoCodeReason})
}

func (f *codexLoginFlow) failedState() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(f.preferred, "timed out") {
		return "expired"
	}
	return "failed"
}

func (f *codexLoginFlow) failureReason() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, reason := range []string{f.preferred, f.lastErr, f.lastOut} {
		if reason != "" {
			return reason
		}
	}
	return ""
}

func (f *codexLoginFlow) expiredReason(limit time.Duration) string {
	return "the one-time code was not approved within " + limit.String()
}

// conclude confirms the login under the member's environment, then reads the
// account from the ID token codex stored. The token is decoded here and
// dropped: only the email and plan claims leave the machine.
func (f *codexLoginFlow) conclude(s *loginSession) {
	r := f.relay
	bin := r.resolveBin("codex")
	script, rendered := r.prober.codexCommand(loginRenderPrefix+s.id+"-status",
		shellQuote(bin)+` login status >/dev/null 2>&1 || exit 3; `+
			`__oc_f="${CODEX_HOME:-$HOME/.codex}/auth.json"; [ -f "$__oc_f" ] && /usr/bin/head -c `+
			strconv.Itoa(codexAuthFileMax)+` "$__oc_f"; exit 0`,
		claudeCommandOpts{selfDeleteRender: true})
	var out string
	var err error
	if r.prober.keep != nil {
		out, err = r.prober.keep.RunKeepStdout(r.prober.shell(), "-c", script)
	}
	if rendered != "" {
		_ = r.remove(rendered)
	}
	if r.prober.keep == nil || err != nil {
		r.progress(loginReport{LoginID: s.id, State: "failed", Reason: codexLoggedOutReason})
		return
	}
	r.prober.checkNow("codex")
	rep := loginReport{LoginID: s.id, State: "succeeded"}
	if account := codexAuthAccount(out); account != (loginAccount{}) {
		rep.Account = &account
	}
	r.progress(rep)
}

// codexAuthAccount answers the email and plan claims of auth.json's
// tokens.id_token; empty when there is none (an API-key login, or a keyring
// credential store that writes no auth.json).
func codexAuthAccount(authJSON string) loginAccount {
	var auth struct {
		Tokens struct {
			IDToken string `json:"id_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal([]byte(authJSON), &auth) != nil {
		return loginAccount{}
	}
	parts := strings.Split(auth.Tokens.IDToken, ".")
	if len(parts) != 3 {
		return loginAccount{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return loginAccount{}
	}
	var claims struct {
		Email string `json:"email"`
		Auth  struct {
			Plan string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return loginAccount{}
	}
	return loginAccount{Email: claims.Email, Plan: claims.Auth.Plan}
}
