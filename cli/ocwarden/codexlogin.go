package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	// What codex prints to walk the owner through the sign-in. None of it says
	// why a login failed, and the last of it ("Continue only if you started this
	// login…") reads like a phishing warning when shown as the reason.
	codexPromptLine = regexp.MustCompile(`^(Welcome to Codex|OpenAI's command-line coding agent|Docs:|` +
		`Follow these steps|Continue only if|Successfully logged in|\d+\. |https?://)`)
	codexStoreSetting = regexp.MustCompile(`(?m)^[ \t]*cli_auth_credentials_store[ \t]*=[ \t]*["']([^"'\n]*)["']`)
)

const (
	codexDeviceLoginCommand = "login --device-auth"
	codexLoggedOutReason    = "codex login status still reports logged out after the login finished"
	codexInstalledButOut    = "the new login was installed but codex in the member environment still reports logged out"
	codexNoCodeReason       = "could not read the one-time code from codex's output"
	codexNoHomeReason       = "the warden cannot state the CODEX_HOME codex would use"
	codexNoAuthFileReason   = "codex finished the login but wrote no auth.json"
	codexKeyringReason      = "這台機器的 Codex 把登入存在系統鑰匙圈，OffiCraft 目前不支援"
	codexDeviceURLPrefix    = "https://auth.openai.com/"
	codexStagingPrefix      = ".oc-codex-login-"
	codexInstallTempPrefix  = ".auth.json.oc-"
	// Inside each staging home: the real CODEX_HOME the login installs into, so
	// the startup sweep can find an install temp file a killed warden left there.
	codexRealHomeMarker = "oc-real-codex-home"
	// Only display claims are read from auth.json; a larger file is still
	// installed but yields no account.
	codexAuthFileMax = 1 << 20
	// The code is printed a line or two after the URL; past this the output is
	// not one this warden can read, and the owner should hear so at once.
	codexCodeWait = 5 * time.Second
)

type codexLoginFlow struct {
	relay    *loginRelay
	realHome string
	staging  string

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
	realHome, reason := r.codexRealHome(loginID)
	if reason == "" {
		var staging string
		if staging, reason = r.stageCodexHome(loginID, realHome); reason == "" {
			// 🔴 `codex login` deletes the credentials already in its CODEX_HOME as
			// it starts, so a cancelled or failed login run against the real home
			// logs the machine out. It runs against a staging home instead, and
			// only a finished login replaces the real auth.json.
			script, rendered := r.prober.codexCommand(loginRenderPrefix+loginID,
				"exec "+shellQuote(bin)+" "+codexDeviceLoginCommand,
				claudeCommandOpts{selfDeleteRender: true, extra: [][2]string{loginBrowserOverride, {"CODEX_HOME", staging}}})
			flow := &codexLoginFlow{relay: r, realHome: realHome, staging: staging}
			r.launch(loginID, "codex", bin, script, rendered, flow, r.codexCap)
			return
		}
	}
	r.fail(loginID, reason)
}

// codexRealHome answers the CODEX_HOME a member's codex uses, read under the
// member's own env layers (the env file may set it).
func (r *loginRelay) codexRealHome(loginID string) (string, string) {
	p := r.prober
	if p.keep == nil {
		return "", codexNoHomeReason
	}
	script, rendered := p.codexCommand(loginRenderPrefix+loginID+"-home",
		`printf '%s' "${CODEX_HOME:-$HOME/.codex}"`, claudeCommandOpts{selfDeleteRender: true})
	out, err := p.keep.RunKeepStdout(p.shell(), "-c", script)
	if rendered != "" {
		_ = r.remove(rendered)
	}
	if err != nil || !filepath.IsAbs(out) {
		return "", codexNoHomeReason
	}
	return filepath.Clean(out), ""
}

// stageCodexHome makes the 0700 home the login writes into, carrying the real
// home's config.toml so the login talks to the same server with the same
// settings.
func (r *loginRelay) stageCodexHome(loginID, realHome string) (string, string) {
	configPath := filepath.Join(realHome, "config.toml")
	config, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", "could not read " + configPath + ": " + err.Error()
	}
	if reason := codexStoreRefusal(config); reason != "" {
		return "", reason
	}
	dir := r.prober.agentHome
	if dir == "" {
		return "", "the warden has no agent home to stage the codex login in"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "could not stage the codex login: " + err.Error()
	}
	staging, err := os.MkdirTemp(dir, codexStagingPrefix+loginID+"-")
	if err != nil {
		return "", "could not stage the codex login: " + err.Error()
	}
	if err := os.WriteFile(filepath.Join(staging, codexRealHomeMarker), []byte(realHome), 0o600); err != nil {
		_ = os.RemoveAll(staging)
		return "", "could not stage the codex login: " + err.Error()
	}
	if config != nil {
		if err := os.WriteFile(filepath.Join(staging, "config.toml"), config, 0o600); err != nil {
			_ = os.RemoveAll(staging)
			return "", "could not stage the codex login: " + err.Error()
		}
	}
	return staging, ""
}

// codexStoreRefusal refuses any credential store but the file one: only
// auth.json can be staged and installed, and a keyring login run here would
// write the keyring entry of the staging home, not the real one.
func codexStoreRefusal(config []byte) string {
	for _, m := range codexStoreSetting.FindAllSubmatch(config, -1) {
		switch store := string(m[1]); store {
		case "file":
		case "keyring", "auto":
			return codexKeyringReason
		default:
			return `這台機器的 Codex 設定 cli_auth_credentials_store = "` + store + `"，不把登入存成檔案，OffiCraft 目前不支援`
		}
	}
	return ""
}

func (f *codexLoginFlow) line(s *loginSession, stderr bool, raw string) {
	line := strings.TrimSpace(stripTerminalEscapes(raw))
	if line == "" {
		return
	}
	f.mu.Lock()
	switch {
	case codexPromptLine.MatchString(line) || codexDeviceCodeLine.MatchString(line):
	case stderr:
		f.lastErr = line
	default:
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
	if strings.Contains(f.preferred, "device auth timed out") {
		return "expired"
	}
	return "failed"
}

func (f *codexLoginFlow) failureReason(waitErr error) string {
	var exit *exec.ExitError
	isExit := errors.As(waitErr, &exit)
	if isExit {
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return "the codex login process was terminated (signal: " + ws.Signal().String() + ")"
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, reason := range []string{f.preferred, f.lastErr, f.lastOut} {
		if reason != "" {
			return reason
		}
	}
	if isExit {
		return "codex login exited with status " + strconv.Itoa(exit.ExitCode())
	}
	return ""
}

func (f *codexLoginFlow) expiredReason(limit time.Duration) string {
	return "the one-time code was not approved within " + limit.String()
}

// conclude confirms the staged login, installs its auth.json into the real
// home, and confirms again under the member's environment. The ID token is
// decoded here and dropped: only the email and plan claims leave the machine.
func (f *codexLoginFlow) conclude(s *loginSession) {
	r := f.relay
	bin := r.resolveBin("codex")
	if !f.statusOK(s.id+"-staged", bin, f.staging) {
		r.progress(loginReport{LoginID: s.id, State: "failed", Reason: codexLoggedOutReason})
		return
	}
	auth, err := os.ReadFile(filepath.Join(f.staging, "auth.json"))
	if err != nil {
		r.progress(loginReport{LoginID: s.id, State: "failed", Reason: codexNoAuthFileReason})
		return
	}
	r.installMu.Lock()
	err = r.installAuth(f.realHome, auth)
	r.installMu.Unlock()
	if err != nil {
		r.log("%s: could not install the new codex login: %v", s.id, err)
		r.progress(loginReport{LoginID: s.id, State: "failed",
			Reason: "could not write the new login into " + f.realHome + ": " + err.Error()})
		return
	}
	if !f.statusOK(s.id+"-status", bin, "") {
		r.progress(loginReport{LoginID: s.id, State: "failed", Reason: codexInstalledButOut})
		return
	}
	r.prober.checkNow("codex")
	rep := loginReport{LoginID: s.id, State: "succeeded"}
	if len(auth) <= codexAuthFileMax {
		if account := codexAuthAccount(string(auth)); account != (loginAccount{}) {
			rep.Account = &account
		}
	}
	r.progress(rep)
}

// statusOK runs `codex login status` under the member's environment, with
// CODEX_HOME replaced by home when it is set.
func (f *codexLoginFlow) statusOK(renderSuffix, bin, home string) bool {
	r := f.relay
	if r.prober.keep == nil {
		return false
	}
	var extra [][2]string
	if home != "" {
		extra = [][2]string{{"CODEX_HOME", home}}
	}
	script, rendered := r.prober.codexCommand(loginRenderPrefix+renderSuffix,
		"exec "+shellQuote(bin)+" login status >/dev/null 2>&1",
		claudeCommandOpts{selfDeleteRender: true, extra: extra})
	_, err := r.prober.keep.RunKeepStdout(r.prober.shell(), "-c", script)
	if rendered != "" {
		_ = r.remove(rendered)
	}
	return err == nil
}

func (f *codexLoginFlow) finish() {
	if f.staging != "" {
		_ = os.RemoveAll(f.staging)
	}
}

// installCodexAuth replaces home/auth.json in one rename, so a member's codex
// reading it mid-install sees either the old login or the new one.
func installCodexAuth(home string, auth []byte) error {
	refuseRealPathInTest("codex home", home)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(home, codexInstallTempPrefix)
	if err != nil {
		return err
	}
	name := tmp.Name()
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(auth); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// ⚠️ An auth.json that is a symlink is replaced by a regular file here; its
	// target keeps the old login.
	if err := os.Rename(name, filepath.Join(home, "auth.json")); err != nil {
		return err
	}
	installed = true
	if d, err := os.Open(home); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// codexAuthAccount answers the email and plan claims of auth.json's
// tokens.id_token; empty when there is none (an API-key login).
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

// sweepCodexStaging removes a staging home a previous warden process left, and
// any install temp file it had made in the real CODEX_HOME.
func sweepCodexStaging(staging string) {
	if realHome, err := os.ReadFile(filepath.Join(staging, codexRealHomeMarker)); err == nil &&
		filepath.IsAbs(string(realHome)) {
		temps, _ := filepath.Glob(filepath.Join(string(realHome), codexInstallTempPrefix+"*"))
		for _, path := range temps {
			if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
				_ = os.Remove(path)
			}
		}
	}
	_ = os.RemoveAll(staging)
}
