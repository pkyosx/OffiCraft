package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// keepShellRunner really runs the `<shell> -c <script>` it is handed (always under
// /bin/sh, so a darwin-shaped prober runs on any host), with RunKeepStdout's
// semantics: stdout survives a non-zero exit.
type keepShellRunner struct {
	shells       []string
	renderExists []bool
	renderPath   string
}

func (r *keepShellRunner) RunKeepStdout(name string, args ...string) (string, error) {
	r.shells = append(r.shells, name)
	_, statErr := os.Stat(r.renderPath)
	r.renderExists = append(r.renderExists, statErr == nil)
	var out, errb bytes.Buffer
	cmd := exec.Command("/bin/sh", args...)
	cmd.Env = append(os.Environ(), "CLAUDE_FROM_WARDEN=leak")
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%w: %s", err, errb.String())
	}
	return out.String(), nil
}

func TestLoginProberState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(root, "nothing-here"))
	home := filepath.Join(root, "home")
	agentHome := filepath.Join(root, "agents")
	envFile := filepath.Join(root, "env")
	evidence := filepath.Join(root, "evidence")
	reply := filepath.Join(root, "reply")
	rc := filepath.Join(root, "rc")
	claudeBin := stageBinary(t, filepath.Join(root, "bin", "claude"), "#!/bin/sh\n"+
		`printf '%s|%s|%s|%s|%s|%s|%s' "$FROM_SHELL" "$FROM_FILE" "${CLAUDE_STRAY-unset}" "${CLAUDE_FROM_WARDEN-unset}" "${CLAUDE_CONFIG_DIR-unset}" "$HOME" "$*" > '`+evidence+"'\n"+
		`/bin/cat '`+reply+"'\n"+
		`exit "$(/bin/cat '`+rc+`')"`+"\n")
	codexBin := stageBinary(t, filepath.Join(root, "bin", "codex"), "#!/bin/sh\n")
	if err := os.WriteFile(envFile, []byte("FROM_FILE=file-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loggedInReply := "{\n  \"loggedIn\": true,\n  \"authMethod\": \"claude.ai\",\n  \"apiProvider\": \"firstParty\",\n" +
		"  \"email\": \"member@example.test\",\n  \"orgId\": \"00000000-0000-0000-0000-000000000000\",\n  \"orgName\": \"Example Org\"\n}\n"
	keychainArgv := "security show-keychain-info " + filepath.Join(home, "Library", "Keychains", "login.keychain-db")
	codexArgv := codexBin + " login status"
	yes, no := true, false

	type answer struct {
		stdout string
		rc     string
	}
	newProber := func(env map[string]string, goos string, runner *wardenRunner, keep *keepShellRunner,
		cache *launchEnvCache, log *[]string) *loginProber {
		p := newLoginProber(envMap(env), runner, keep, goos, cache,
			func(f string, a ...any) { *log = append(*log, fmt.Sprintf(f, a...)) })
		p.claudeHome = claudeHome{Home: home}
		p.agentHome = agentHome
		p.envFile = envFile
		p.captureEnv = func() (string, error) { return "", errors.New("capture not staged") }
		return p
	}
	stage := func(t *testing.T, a answer) {
		t.Helper()
		_ = os.Remove(evidence)
		if err := os.WriteFile(reply, []byte(a.stdout), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(rc, []byte(a.rc), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name      string
		goos      string
		env       map[string]string
		answer    answer
		script    map[string]wardenRun
		want      loginState
		wantRuns  []string
		wantShell []string
		wantLog   []string
	}{
		{
			name:      "under a logged-in auth status, claude is true and nothing runs for an absent codex",
			goos:      "darwin",
			env:       map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			answer:    answer{loggedInReply, "0"},
			want:      loginState{Claude: &yes},
			wantShell: []string{"/bin/zsh"},
		},
		{
			name:      "under a logged-out auth status on darwin with a readable keychain, claude is false",
			goos:      "darwin",
			env:       map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			answer:    answer{`{"loggedIn":false}`, "1"},
			script:    map[string]wardenRun{keychainArgv: {out: "Keychain settings"}},
			want:      loginState{Claude: &no},
			wantRuns:  []string{keychainArgv},
			wantShell: []string{"/bin/zsh"},
		},
		{
			name:      "under a logged-out auth status on darwin with an unreadable keychain, claude is unknown",
			goos:      "darwin",
			env:       map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			answer:    answer{`{"loggedIn":false}`, "1"},
			script:    map[string]wardenRun{keychainArgv: {err: errors.New("exit status 50")}},
			want:      loginState{},
			wantRuns:  []string{keychainArgv},
			wantShell: []string{"/bin/zsh"},
			wantLog:   []string{"[ocwarden runtimeprobe] claude reports logged out but the login keychain is unreadable; reporting unknown: exit status 50"},
		},
		{
			name:      "under a logged-out auth status off darwin, claude is false with no keychain check",
			goos:      "linux",
			env:       map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			answer:    answer{`{"loggedIn":false}`, "1"},
			want:      loginState{Claude: &no},
			wantShell: []string{"/bin/sh"},
		},
		{
			name:      "under non-JSON auth status output, claude is unknown and the output is not logged",
			goos:      "linux",
			env:       map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			answer:    answer{"Logged in as eva@example.com", "0"},
			want:      loginState{},
			wantShell: []string{"/bin/sh"},
			wantLog:   []string{fmt.Sprintf("[ocwarden runtimeprobe] claude auth status gave no login verdict (bin=%s)", claudeBin)},
		},
		{
			name:      "under auth status JSON without loggedIn, claude is unknown",
			goos:      "linux",
			env:       map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			answer:    answer{`{"authMethod":"none"}`, "1"},
			want:      loginState{},
			wantShell: []string{"/bin/sh"},
			wantLog:   []string{fmt.Sprintf("[ocwarden runtimeprobe] claude auth status gave no login verdict (bin=%s): exit status 1: ", claudeBin)},
		},
		{
			name:      "under a loggedIn that is the string \"false\", claude is unknown",
			goos:      "linux",
			env:       map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			answer:    answer{"{\n  \"loggedIn\": \"false\",\n  \"authMethod\": \"none\"\n}\n", "1"},
			want:      loginState{},
			wantShell: []string{"/bin/sh"},
			wantLog:   []string{fmt.Sprintf("[ocwarden runtimeprobe] claude auth status gave no login verdict (bin=%s): exit status 1: ", claudeBin)},
		},
		{
			name:      "under a null loggedIn, claude is unknown",
			goos:      "linux",
			env:       map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			answer:    answer{"{\n  \"loggedIn\": null\n}\n", "0"},
			want:      loginState{},
			wantShell: []string{"/bin/sh"},
			wantLog:   []string{fmt.Sprintf("[ocwarden runtimeprobe] claude auth status gave no login verdict (bin=%s)", claudeBin)},
		},
		{
			name:     "under a codex whose login status succeeds, codex is true and claude unknown when absent",
			goos:     "linux",
			env:      map[string]string{"HOME": home, "OC_CODEX_BIN": codexBin},
			script:   map[string]wardenRun{codexArgv: {out: "Logged in"}},
			want:     loginState{Codex: &yes},
			wantRuns: []string{codexArgv},
		},
		{
			name:     "under a codex login status that exits non-zero, codex is false and the error reaches the log",
			goos:     "linux",
			env:      map[string]string{"HOME": home, "OC_CODEX_BIN": codexBin},
			script:   map[string]wardenRun{codexArgv: {err: exitStatus1(t, "not logged in")}},
			want:     loginState{Codex: &no},
			wantRuns: []string{codexArgv},
			wantLog:  []string{fmt.Sprintf("[ocwarden runtimeprobe] codex login status failed (bin=%s): exit status 1: not logged in", codexBin)},
		},
		{
			name:     "under a codex login status that timed out, codex is unknown and the error reaches the log",
			goos:     "linux",
			env:      map[string]string{"HOME": home, "OC_CODEX_BIN": codexBin},
			script:   map[string]wardenRun{codexArgv: {err: errors.New("timeout after 10s")}},
			want:     loginState{},
			wantRuns: []string{codexArgv},
			wantLog:  []string{fmt.Sprintf("[ocwarden runtimeprobe] codex login status failed (bin=%s): timeout after 10s", codexBin)},
		},
		{
			name:     "under a codex binary that would not start, codex is unknown",
			goos:     "linux",
			env:      map[string]string{"HOME": home, "OC_CODEX_BIN": codexBin},
			script:   map[string]wardenRun{codexArgv: {err: errors.New("fork/exec " + codexBin + ": exec format error")}},
			want:     loginState{},
			wantRuns: []string{codexArgv},
			wantLog:  []string{fmt.Sprintf("[ocwarden runtimeprobe] codex login status failed (bin=%s): fork/exec %s: exec format error", codexBin, codexBin)},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stage(t, c.answer)
			runner := &wardenRunner{script: c.script, fallback: wardenRun{err: errors.New("unscripted argv")}}
			keep := &keepShellRunner{renderPath: filepath.Join(agentHome, loginCheckEnvName)}
			var log []string
			cache := &launchEnvCache{}
			cache.remember([]agentEnvPair{{"FROM_SHELL", "shell-value"}})
			got := newProber(c.env, c.goos, runner, keep, cache, &log).state()
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("state = %s, want %s", fmtLogin(got), fmtLogin(c.want))
			}
			if !reflect.DeepEqual(runner.calls, c.wantRuns) {
				t.Errorf("ran %v, want %v", runner.calls, c.wantRuns)
			}
			if !reflect.DeepEqual(keep.shells, c.wantShell) {
				t.Errorf("auth status shells = %v, want %v", keep.shells, c.wantShell)
			}
			if !reflect.DeepEqual(log, c.wantLog) {
				t.Errorf("log =\n  %#v\nwant\n  %#v", log, c.wantLog)
			}
		})
	}

	envCases := []struct {
		name      string
		configDir string
		want      string
	}{
		{
			name: "under a spawn's captured shell env, auth status runs with it, the env file and the member's HOME, CLAUDE_* purged, and the render removed",
			want: "shell-value|file-value|unset|unset|unset|" + home + "|auth status",
		},
		{
			name:      "under a relocated claude config dir, CLAUDE_CONFIG_DIR survives the CLAUDE_* purge",
			configDir: filepath.Join(root, "claude-config"),
			want:      "shell-value|file-value|unset|unset|" + filepath.Join(root, "claude-config") + "|" + home + "|auth status",
		},
	}
	for _, c := range envCases {
		t.Run(c.name, func(t *testing.T) {
			stage(t, answer{`{"loggedIn":true}`, "0"})
			cache := &launchEnvCache{}
			cache.remember([]agentEnvPair{{"FROM_SHELL", "shell-value"}, {"CLAUDE_STRAY", "x"}})
			keep := &keepShellRunner{renderPath: filepath.Join(agentHome, loginCheckEnvName)}
			var log []string
			p := newProber(map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin}, "linux",
				&wardenRunner{}, keep, cache, &log)
			p.claudeHome.ConfigDir = c.configDir
			if got := p.state(); !reflect.DeepEqual(got, loginState{Claude: &yes}) {
				t.Errorf("state = %s, want claude=true codex=nil", fmtLogin(got))
			}
			raw, err := os.ReadFile(evidence)
			if err != nil {
				t.Fatalf("auth status never ran: %v", err)
			}
			if string(raw) != c.want {
				t.Errorf("claude saw %q, want %q", raw, c.want)
			}
			if want := []bool{true}; !reflect.DeepEqual(keep.renderExists, want) {
				t.Errorf("render present during the check = %v, want %v", keep.renderExists, want)
			}
			if _, err := os.Stat(filepath.Join(agentHome, loginCheckEnvName)); !os.IsNotExist(err) {
				t.Errorf("render left on disk after the check (stat err %v)", err)
			}
		})
	}

	t.Run("under no spawn yet and a capture that succeeds, auth status runs with the captured env and the shared cache keeps it", func(t *testing.T) {
		stage(t, answer{`{"loggedIn":false}`, "1"})
		cache := &launchEnvCache{}
		keep := &keepShellRunner{renderPath: filepath.Join(agentHome, loginCheckEnvName)}
		var log []string
		p := newProber(map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin}, "linux",
			&wardenRunner{}, keep, cache, &log)
		captures := 0
		p.captureEnv = func() (string, error) {
			captures++
			return "FROM_SHELL=captured\x00OC_TOKEN=stray\x00", nil
		}
		if got := p.state(); !reflect.DeepEqual(got, loginState{Claude: &no}) {
			t.Errorf("state = %s, want claude=false codex=nil", fmtLogin(got))
		}
		raw, _ := os.ReadFile(evidence)
		if want := "captured|file-value|unset|unset|unset|" + home + "|auth status"; string(raw) != want {
			t.Errorf("claude saw %q, want %q", raw, want)
		}
		if want := []agentEnvPair{{"FROM_SHELL", "captured"}}; !reflect.DeepEqual(cache.interactive(), want) {
			t.Errorf("cache = %v, want %v", cache.interactive(), want)
		}
		if captures != 1 {
			t.Errorf("captures = %d, want 1", captures)
		}
		if want := []string{"interactive env: skipped OC_TOKEN — OC_* is warden-reserved (the agent's own identity)"}; !reflect.DeepEqual(log, want) {
			t.Errorf("log =\n  %#v\nwant\n  %#v", log, want)
		}
	})

	t.Run("under no spawn yet and a failing capture, a logged-out verdict is unknown, a logged-in one stays true, and the capture is not retried", func(t *testing.T) {
		stage(t, answer{`{"loggedIn":false}`, "1"})
		keep := &keepShellRunner{renderPath: filepath.Join(agentHome, loginCheckEnvName)}
		var log []string
		p := newProber(map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin}, "linux",
			&wardenRunner{}, keep, &launchEnvCache{}, &log)
		captures := 0
		p.captureEnv = func() (string, error) {
			captures++
			return "", errors.New("timed out after 10s")
		}
		clock := time.Unix(1_000_000, 0)
		p.now = func() time.Time { return clock }
		if got := p.state(); !reflect.DeepEqual(got, loginState{}) {
			t.Errorf("logged-out state = %s, want claude=nil codex=nil", fmtLogin(got))
		}
		stage(t, answer{`{"loggedIn":true}`, "0"})
		clock = clock.Add(defaultLoginCheckInterval)
		if got := p.state(); !reflect.DeepEqual(got, loginState{Claude: &yes}) {
			t.Errorf("logged-in state = %s, want claude=true codex=nil", fmtLogin(got))
		}
		raw, _ := os.ReadFile(evidence)
		if want := "|file-value|unset|unset|unset|" + home + "|auth status"; string(raw) != want {
			t.Errorf("claude saw %q, want %q", raw, want)
		}
		if captures != 1 {
			t.Errorf("captures = %d, want 1", captures)
		}
		want := []string{
			"[ocwarden runtimeprobe] interactive env: capture failed (timed out after 10s)",
			"[ocwarden runtimeprobe] claude reports logged out but the interactive shell env is unavailable; reporting unknown",
		}
		if !reflect.DeepEqual(log, want) {
			t.Errorf("log =\n  %#v\nwant\n  %#v", log, want)
		}
	})

	t.Run("under capture switched off by OC_AGENT_ENV_INHERIT=0, a logged-out verdict stays false", func(t *testing.T) {
		stage(t, answer{`{"loggedIn":false}`, "1"})
		keep := &keepShellRunner{renderPath: filepath.Join(agentHome, loginCheckEnvName)}
		var log []string
		env := map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin, "OC_AGENT_ENV_INHERIT": "0"}
		p := newProber(env, "linux", &wardenRunner{}, keep, &launchEnvCache{}, &log)
		p.captureEnv = defaultCaptureEnv(envMap(env))
		if got := p.state(); !reflect.DeepEqual(got, loginState{Claude: &no}) {
			t.Errorf("state = %s, want claude=false codex=nil", fmtLogin(got))
		}
		raw, _ := os.ReadFile(evidence)
		if want := "|file-value|unset|unset|unset|" + home + "|auth status"; string(raw) != want {
			t.Errorf("claude saw %q, want %q", raw, want)
		}
		if len(log) != 0 {
			t.Errorf("log = %#v, want none", log)
		}
	})

	type beat struct {
		at             time.Duration
		claude         answer
		codex          wardenRun
		want           loginState
		wantClaudeRuns int
		wantCodexRuns  int
	}
	loggedOut := answer{`{"loggedIn":false}`, "1"}
	loggedIn := answer{`{"loggedIn":true}`, "0"}
	codexIn := wardenRun{out: "Logged in"}
	codexOut := wardenRun{err: exitStatus1(t, "not logged in")}
	cadenceCases := []struct {
		name     string
		goos     string
		env      map[string]string
		keychain wardenRun
		recheck  time.Duration
		beats    []beat
	}{
		{
			name: "under a logged-in verdict, the next check runs only once the interval has passed",
			goos: "linux",
			env:  map[string]string{"HOME": home, "OC_CODEX_BIN": codexBin},
			beats: []beat{
				{at: 0, codex: codexIn, want: loginState{Codex: &yes}, wantCodexRuns: 1},
				{at: 30 * time.Second, codex: codexIn, want: loginState{Codex: &yes}, wantCodexRuns: 1},
				{at: 299 * time.Second, codex: codexIn, want: loginState{Codex: &yes}, wantCodexRuns: 1},
				{at: 300 * time.Second, codex: codexIn, want: loginState{Codex: &yes}, wantCodexRuns: 2},
			},
		},
		{
			name: "under a logged-out verdict, claude is re-checked at every next heartbeat",
			goos: "linux",
			env:  map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			beats: []beat{
				{at: 0, claude: loggedOut, want: loginState{Claude: &no}, wantClaudeRuns: 1},
				{at: 30 * time.Second, claude: loggedOut, want: loginState{Claude: &no}, wantClaudeRuns: 2},
				{at: 60 * time.Second, claude: loggedOut, want: loginState{Claude: &no}, wantClaudeRuns: 3},
			},
		},
		{
			name: "under a refused codex login status, codex is re-checked at the next heartbeat",
			goos: "linux",
			env:  map[string]string{"HOME": home, "OC_CODEX_BIN": codexBin},
			beats: []beat{
				{at: 0, codex: codexOut, want: loginState{Codex: &no}, wantCodexRuns: 1},
				{at: 30 * time.Second, codex: codexOut, want: loginState{Codex: &no}, wantCodexRuns: 2},
			},
		},
		{
			name: "under an unknown verdict from non-JSON output, claude is re-checked at the next heartbeat",
			goos: "linux",
			env:  map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			beats: []beat{
				{at: 0, claude: answer{"Logged in as eva@example.com", "0"}, want: loginState{}, wantClaudeRuns: 1},
				{at: 30 * time.Second, claude: answer{"Logged in as eva@example.com", "0"}, want: loginState{}, wantClaudeRuns: 2},
			},
		},
		{
			name:     "under an unknown verdict from an unreadable keychain, claude is re-checked at the next heartbeat",
			goos:     "darwin",
			env:      map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			keychain: wardenRun{err: errors.New("exit status 50")},
			beats: []beat{
				{at: 0, claude: loggedOut, want: loginState{}, wantClaudeRuns: 1},
				{at: 30 * time.Second, claude: loggedOut, want: loginState{}, wantClaudeRuns: 2},
			},
		},
		{
			name:    "under a 90s recheck interval and a logged-out verdict, claude is re-checked only once 90s have passed",
			goos:    "linux",
			env:     map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			recheck: 90 * time.Second,
			beats: []beat{
				{at: 0, claude: loggedOut, want: loginState{Claude: &no}, wantClaudeRuns: 1},
				{at: 30 * time.Second, claude: loggedOut, want: loginState{Claude: &no}, wantClaudeRuns: 1},
				{at: 89 * time.Second, claude: loggedOut, want: loginState{Claude: &no}, wantClaudeRuns: 1},
				{at: 90 * time.Second, claude: loggedOut, want: loginState{Claude: &no}, wantClaudeRuns: 2},
			},
		},
		{
			name:    "under a 90s recheck interval and a logged-in verdict, codex still waits the full interval",
			goos:    "linux",
			env:     map[string]string{"HOME": home, "OC_CODEX_BIN": codexBin},
			recheck: 90 * time.Second,
			beats: []beat{
				{at: 0, codex: codexIn, want: loginState{Codex: &yes}, wantCodexRuns: 1},
				{at: 90 * time.Second, codex: codexIn, want: loginState{Codex: &yes}, wantCodexRuns: 1},
				{at: 300 * time.Second, codex: codexIn, want: loginState{Codex: &yes}, wantCodexRuns: 2},
			},
		},
		{
			name: "under a logged-out verdict followed by a logged-in one, claude returns to the interval",
			goos: "linux",
			env:  map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin},
			beats: []beat{
				{at: 0, claude: loggedOut, want: loginState{Claude: &no}, wantClaudeRuns: 1},
				{at: 30 * time.Second, claude: loggedIn, want: loginState{Claude: &yes}, wantClaudeRuns: 2},
				{at: 60 * time.Second, claude: loggedOut, want: loginState{Claude: &yes}, wantClaudeRuns: 2},
				{at: 329 * time.Second, claude: loggedOut, want: loginState{Claude: &yes}, wantClaudeRuns: 2},
				{at: 330 * time.Second, claude: loggedOut, want: loginState{Claude: &no}, wantClaudeRuns: 3},
			},
		},
		{
			name: "under claude logged out while codex is logged in, only claude is re-checked each heartbeat",
			goos: "linux",
			env:  map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin, "OC_CODEX_BIN": codexBin},
			beats: []beat{
				{at: 0, claude: loggedOut, codex: codexIn, want: loginState{Claude: &no, Codex: &yes}, wantClaudeRuns: 1, wantCodexRuns: 1},
				{at: 30 * time.Second, claude: loggedOut, codex: codexIn, want: loginState{Claude: &no, Codex: &yes}, wantClaudeRuns: 2, wantCodexRuns: 1},
				{at: 60 * time.Second, claude: loggedOut, codex: codexIn, want: loginState{Claude: &no, Codex: &yes}, wantClaudeRuns: 3, wantCodexRuns: 1},
				{at: 300 * time.Second, claude: loggedOut, codex: codexIn, want: loginState{Claude: &no, Codex: &yes}, wantClaudeRuns: 4, wantCodexRuns: 2},
			},
		},
	}
	for _, c := range cadenceCases {
		t.Run(c.name, func(t *testing.T) {
			runner := &wardenRunner{script: map[string]wardenRun{keychainArgv: c.keychain}}
			keep := &keepShellRunner{renderPath: filepath.Join(agentHome, loginCheckEnvName)}
			cache := &launchEnvCache{}
			cache.remember([]agentEnvPair{{"FROM_SHELL", "shell-value"}})
			var log []string
			p := newProber(c.env, c.goos, runner, keep, cache, &log)
			if c.recheck != 0 {
				p.setIntervals(defaultLoginCheckInterval, c.recheck)
			}
			start := time.Unix(1_000_000, 0)
			for _, b := range c.beats {
				stage(t, b.claude)
				runner.script[codexArgv] = b.codex
				p.now = func() time.Time { return start.Add(b.at) }
				got := p.state()
				codexRuns := 0
				for _, argv := range runner.calls {
					if argv == codexArgv {
						codexRuns++
					}
				}
				if !reflect.DeepEqual(got, b.want) || len(keep.shells) != b.wantClaudeRuns || codexRuns != b.wantCodexRuns {
					t.Errorf("at %s: state = %s, claude runs %d, codex runs %d; want %s, %d, %d",
						b.at, fmtLogin(got), len(keep.shells), codexRuns, fmtLogin(b.want), b.wantClaudeRuns, b.wantCodexRuns)
				}
			}
		})
	}

	t.Run("under repeated heartbeats, checks run at start and again only once the receipt's interval has passed", func(t *testing.T) {
		runner := &wardenRunner{script: map[string]wardenRun{codexArgv: {out: "ok"}}}
		var log []string
		p := newProber(map[string]string{"HOME": home, "OC_CODEX_BIN": codexBin}, "linux",
			runner, &keepShellRunner{}, nil, &log)
		clock := time.Unix(1_000_000, 0)
		p.now = func() time.Time { return clock }
		p.state()
		clock = clock.Add(299 * time.Second)
		p.state()
		p.setIntervals(60*time.Second, defaultLoginRecheckInterval)
		p.state()
		clock = clock.Add(59 * time.Second)
		p.state()
		clock = clock.Add(1 * time.Second)
		p.state()
		if want := []string{codexArgv, codexArgv, codexArgv}; !reflect.DeepEqual(runner.calls, want) {
			t.Errorf("ran %v, want %v", runner.calls, want)
		}
	})
}

func fmtLogin(s loginState) string {
	return "claude=" + fmtVerdict(s.Claude) + " codex=" + fmtVerdict(s.Codex)
}

func fmtVerdict(b *bool) string {
	if b == nil {
		return "nil"
	}
	return fmt.Sprint(*b)
}

func boolRef(v bool) *bool { return &v }

// loginVerdicts is a LoginCheck answering from a fixed table; a runtime not in
// it reads unknown.
func loginVerdicts(m map[string]*bool) func(string) *bool {
	return func(runtime string) *bool { return m[runtime] }
}

// exitStatus1 is the error a real `codex login status` gives a logged-out host:
// an *exec.ExitError, wrapped the way execRunner wraps it.
func exitStatus1(t *testing.T, stderr string) error {
	t.Helper()
	falseBin := ""
	for _, candidate := range []string{"/usr/bin/false", "/bin/false"} {
		if _, err := os.Stat(candidate); err == nil {
			falseBin = candidate
			break
		}
	}
	if falseBin == "" {
		t.Fatal("no false(1) at /usr/bin or /bin")
	}
	runErr := exec.Command(falseBin).Run()
	if runErr == nil {
		t.Fatal("false(1) exited 0")
	}
	return fmt.Errorf("%w: %s", runErr, stderr)
}
