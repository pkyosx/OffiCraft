package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	fakeLoginURL  = "https://claude.ai/oauth/authorize?code=true&state=fake-state-77"
	fakeLoginCode = "fake-code-31#fake-state-77"
)

// shellKeep runs the `<shell> -c <script>` the prober hands it, under /bin/sh,
// keeping stdout on a non-zero exit like the production runner.
type shellKeep struct{}

func (shellKeep) RunKeepStdout(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("/bin/sh", args...)
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

// fakeClaude is a stand-in for the claude CLI, shaped on claude 2.1.286:
// `auth login` records its pid and the FROM_FILE it was started with, prints the
// sign-in URL line (dimmed, as an OSC 8 hyperlink whose label is shortened,
// when the osc file exists), then reads
// stdin line by line, touching the read-line file for each. A line that is not two non-empty halves joined by `#` is
// refused on stderr and the next is read; an accepted one is recorded, echoed
// back on stderr when the echo file exists, followed by the stderr file, and
// the process exits with the code in the rc file. `auth status` prints the
// status reply.
type fakeClaude struct {
	root, bin, pid, gotCode, rc, stderr, status, osc, echo, sawEnv, readLine string
}

func newFakeClaude(t *testing.T) *fakeClaude {
	t.Helper()
	root := t.TempDir()
	f := &fakeClaude{
		root:     root,
		pid:      filepath.Join(root, "pid"),
		gotCode:  filepath.Join(root, "got-code"),
		rc:       filepath.Join(root, "rc"),
		stderr:   filepath.Join(root, "stderr"),
		status:   filepath.Join(root, "status"),
		osc:      filepath.Join(root, "osc"),
		echo:     filepath.Join(root, "echo"),
		sawEnv:   filepath.Join(root, "saw-env"),
		readLine: filepath.Join(root, "read-line"),
	}
	f.bin = stageBinary(t, filepath.Join(root, "bin", "claude"), "#!/bin/sh\n"+
		`if [ "$1 $2" = "auth status" ]; then /bin/cat '`+f.status+`'; exit 0; fi`+"\n"+
		`echo $$ > '`+f.pid+`'`+"\n"+
		`printf '%s' "${FROM_FILE-unset}" > '`+f.sawEnv+`'`+"\n"+
		`echo 'Opening browser to sign in…'`+"\n"+
		`if [ -f '`+f.osc+`' ]; then printf "\\033[2mIf the browser didn't open, visit: \\033]8;;%s\\007claude.ai/oauth/authorize\\033]8;;\\007\\033[0m\\n" '`+fakeLoginURL+`'; `+
		`else echo "If the browser didn't open, visit: `+fakeLoginURL+`"; fi`+"\n"+
		`while IFS= read -r code; do`+"\n"+
		`  : > '`+f.readLine+`'`+"\n"+
		`  case "$code" in ?*'#'?*) ;; *) echo 'Invalid code. Please make sure the full code was copied.' >&2; continue;; esac`+"\n"+
		`  printf '%s' "$code" > '`+f.gotCode+`'`+"\n"+
		`  [ -f '`+f.echo+`' ] && printf 'Login failed: code %s rejected (%s / %s)\nRun claude auth login again\n' "$code" "${code%%#*}" "${code#*#}" >&2`+"\n"+
		`  [ -f '`+f.stderr+`' ] && /bin/cat '`+f.stderr+`' >&2`+"\n"+
		`  exit "$(/bin/cat '`+f.rc+`')"`+"\n"+
		`done`+"\n")
	f.write(t, f.rc, "0")
	f.write(t, f.status, `{"loggedIn":true,"authMethod":"claude.ai","email":"owner@example.test","orgName":"Example Org","subscriptionType":"max"}`)
	return f
}

func (f *fakeClaude) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeClaude) alive(t *testing.T) bool {
	t.Helper()
	raw, err := os.ReadFile(f.pid)
	if err != nil {
		t.Fatalf("the fake login never recorded its pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return syscall.Kill(pid, 0) == nil
}

type relayHarness struct {
	relay   *loginRelay
	prober  *loginProber
	claude  *fakeClaude
	codex   *fakeCodex
	reports chan loginReport
	answer  func(loginReport) string
	mu      sync.Mutex
	logs    []string
}

func newRelayHarness(t *testing.T) *relayHarness {
	t.Helper()
	h := &relayHarness{claude: newFakeClaude(t), codex: newFakeCodex(t), reports: make(chan loginReport, 16)}
	// No real claude or codex may ever be found: a real `codex login
	// --device-auth` asks OpenAI for a device code over the network.
	t.Setenv("PATH", filepath.Join(h.claude.root, "no-binaries-here"))
	home := filepath.Join(h.claude.root, "home")
	env := envMap(map[string]string{"HOME": home, "OC_CLAUDE_BIN": h.claude.bin, "OC_CODEX_BIN": h.codex.bin})
	logf := func(format string, a ...any) {
		h.mu.Lock()
		h.logs = append(h.logs, fmt.Sprintf(format, a...))
		h.mu.Unlock()
	}
	h.prober = newLoginProber(env, &wardenRunner{}, shellKeep{}, "linux", &launchEnvCache{}, logf)
	h.prober.claudeHome = claudeHome{Home: home}
	h.prober.agentHome = filepath.Join(h.claude.root, "agents")
	h.prober.envFile = filepath.Join(h.claude.root, "env")
	h.prober.captureEnv = nil
	h.relay = newLoginRelay(h.prober, func(rep loginReport) string {
		h.reports <- rep
		if h.answer != nil {
			return h.answer(rep)
		}
		return rep.State
	}, logf)
	h.relay.resolveBin = func(runtime string) string {
		if runtime == "codex" {
			return h.codex.bin
		}
		return h.claude.bin
	}
	t.Cleanup(func() {
		for _, s := range h.sessions() {
			s.proc.kill()
			<-s.done
		}
	})
	return h
}

func (h *relayHarness) sessions() []*loginSession {
	h.relay.mu.Lock()
	defer h.relay.mu.Unlock()
	var out []*loginSession
	for _, s := range h.relay.sessions {
		out = append(out, s)
	}
	return out
}

func (h *relayHarness) next(t *testing.T) loginReport {
	t.Helper()
	select {
	case rep := <-h.reports:
		return rep
	case <-time.After(10 * time.Second):
		t.Fatal("no login report arrived within 10s")
		return loginReport{}
	}
}

func (h *relayHarness) wantNoReport(t *testing.T) {
	t.Helper()
	select {
	case rep := <-h.reports:
		t.Fatalf("an unexpected report arrived: %+v", rep)
	case <-time.After(200 * time.Millisecond):
	}
}

func (h *relayHarness) waitEnded(t *testing.T, loginID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h.relay.session(loginID) == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("login %s still has a running process after 10s", loginID)
}

func (h *relayHarness) wantLogsFree(t *testing.T, secrets ...string) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.logs) == 0 {
		t.Fatal("control: the relay logged nothing, so a clean log proves nothing")
	}
	for _, line := range h.logs {
		for _, secret := range secrets {
			if strings.Contains(line, secret) {
				t.Errorf("a log line carries %q: %s", secret, line)
			}
		}
	}
}

func TestLoginRelay(t *testing.T) {
	t.Run("under a code the CLI accepts, the relay reports awaiting_code with the URL, hands the code to stdin, reports verifying, then succeeded with the account and kicks a heartbeat", func(t *testing.T) {
		h := newRelayHarness(t)
		h.relay.Start("rl-1", "claude")
		if got, want := h.next(t), (loginReport{LoginID: "rl-1", State: "awaiting_code", AuthURL: fakeLoginURL}); got != want {
			t.Fatalf("first report = %+v, want %+v", got, want)
		}
		h.relay.Code("rl-1", fakeLoginCode)
		if got, want := h.next(t), (loginReport{LoginID: "rl-1", State: "verifying"}); got != want {
			t.Fatalf("second report = %+v, want %+v", got, want)
		}
		got := h.next(t)
		want := loginReport{LoginID: "rl-1", State: "succeeded",
			Account: &loginAccount{Email: "owner@example.test", OrgName: "Example Org", Plan: "max"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("third report = %+v, want %+v", got, want)
		}
		if raw, _ := os.ReadFile(h.claude.gotCode); string(raw) != fakeLoginCode {
			t.Errorf("the login process read %q from stdin, want %q", raw, fakeLoginCode)
		}
		select {
		case <-h.prober.kicked():
		default:
			t.Error("a finished login did not kick the heartbeat")
		}
		h.waitEnded(t, "rl-1")
		h.wantLogsFree(t, fakeLoginCode, fakeLoginURL, "fake-state-77")
	})

	t.Run("under a code the CLI refuses, the relay reports failed with the CLI's stderr line", func(t *testing.T) {
		h := newRelayHarness(t)
		h.claude.write(t, h.claude.rc, "1")
		h.claude.write(t, h.claude.stderr, "Login failed: Request failed with status code 400\n")
		h.relay.Start("rl-2", "claude")
		h.next(t)
		h.relay.Code("rl-2", fakeLoginCode)
		h.next(t)
		if got, want := h.next(t), (loginReport{LoginID: "rl-2", State: "failed",
			Reason: "Login failed: Request failed with status code 400"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		h.wantLogsFree(t, fakeLoginCode, fakeLoginURL)
	})

	t.Run("under a sign-in URL printed dimmed as an OSC 8 hyperlink with a shorter label, the reported URL is the link target", func(t *testing.T) {
		h := newRelayHarness(t)
		h.claude.write(t, h.claude.osc, "")
		h.relay.Start("rl-osc", "claude")
		if got, want := h.next(t), (loginReport{LoginID: "rl-osc", State: "awaiting_code", AuthURL: fakeLoginURL}); got != want {
			t.Fatalf("first report = %+v, want %+v", got, want)
		}
	})

	t.Run("under a partial code, the CLI's refusal returns the login to awaiting_code and a full code then succeeds", func(t *testing.T) {
		h := newRelayHarness(t)
		h.relay.Start("rl-p", "claude")
		h.next(t)
		h.relay.Code("rl-p", "fake-code-31")
		if got, want := h.next(t), (loginReport{LoginID: "rl-p", State: "verifying"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		if got, want := h.next(t), (loginReport{LoginID: "rl-p", State: "awaiting_code",
			Reason: "Invalid code. Please make sure the full code was copied."}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		h.relay.Code("rl-p", fakeLoginCode)
		if got, want := h.next(t), (loginReport{LoginID: "rl-p", State: "verifying"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		if got := h.next(t); got.State != "succeeded" {
			t.Fatalf("final report = %+v, want succeeded", got)
		}
		if raw, _ := os.ReadFile(h.claude.gotCode); string(raw) != fakeLoginCode {
			t.Errorf("the login process accepted %q, want %q", raw, fakeLoginCode)
		}
	})

	t.Run("under a refused code, verifying reaches the server before the CLI has the code, so the refusal's awaiting_code is the last word", func(t *testing.T) {
		h := newRelayHarness(t)
		var cliHadCode []bool
		h.answer = func(rep loginReport) string {
			if rep.State == "verifying" {
				// Long enough for a CLI that already got the code to have read it.
				time.Sleep(300 * time.Millisecond)
				_, err := os.Stat(h.claude.readLine)
				cliHadCode = append(cliHadCode, err == nil)
			}
			return rep.State
		}
		h.relay.Start("rl-o", "claude")
		h.next(t)
		h.relay.Code("rl-o", "fake-code-31")
		if got, want := h.next(t), (loginReport{LoginID: "rl-o", State: "verifying"}); got != want {
			t.Fatalf("second report = %+v, want %+v", got, want)
		}
		if got, want := h.next(t), (loginReport{LoginID: "rl-o", State: "awaiting_code",
			Reason: "Invalid code. Please make sure the full code was copied."}); got != want {
			t.Fatalf("third report = %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(cliHadCode, []bool{false}) {
			t.Errorf("CLI had read the code when verifying was reported = %v, want [false]", cliHadCode)
		}
		if _, err := os.Stat(h.claude.readLine); err != nil {
			t.Error("control: the CLI never read the code at all")
		}
		h.wantNoReport(t)
	})

	t.Run("under a CLI that echoes the code on stderr, the failure reason masks the code and each half", func(t *testing.T) {
		h := newRelayHarness(t)
		h.claude.write(t, h.claude.rc, "1")
		h.claude.write(t, h.claude.echo, "")
		h.relay.Start("rl-m", "claude")
		h.next(t)
		h.relay.Code("rl-m", fakeLoginCode)
		h.next(t)
		if got, want := h.next(t), (loginReport{LoginID: "rl-m", State: "failed",
			Reason: "Login failed: code … rejected (… / …)"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
	})

	t.Run("under a failure followed by a hint line, the reason is the Login failed line", func(t *testing.T) {
		h := newRelayHarness(t)
		h.claude.write(t, h.claude.rc, "1")
		h.claude.write(t, h.claude.stderr, "Login failed: Request failed with status code 400\nRun claude auth login to try again\n")
		h.relay.Start("rl-h", "claude")
		h.next(t)
		h.relay.Code("rl-h", fakeLoginCode)
		h.next(t)
		if got, want := h.next(t), (loginReport{LoginID: "rl-h", State: "failed",
			Reason: "Login failed: Request failed with status code 400"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
	})

	t.Run("under an env file, the process starts with it but its 0600 render is gone while the login still runs", func(t *testing.T) {
		h := newRelayHarness(t)
		h.claude.write(t, h.prober.envFile, "FROM_FILE=file-value\n")
		h.relay.Start("rl-r", "claude")
		h.next(t)
		if raw, _ := os.ReadFile(h.claude.sawEnv); string(raw) != "file-value" {
			t.Fatalf("control: the login process saw FROM_FILE=%q, so no render was sourced", raw)
		}
		if !h.claude.alive(t) {
			t.Fatal("control: the login process is not running")
		}
		if _, err := os.Stat(filepath.Join(h.prober.agentHome, loginRenderPrefix+"rl-r")); !os.IsNotExist(err) {
			t.Errorf("the env render is still on disk while the login runs (stat err %v)", err)
		}
	})

	t.Run("under a cancel, the login process is killed and nothing more is reported", func(t *testing.T) {
		h := newRelayHarness(t)
		h.relay.Start("rl-3", "claude")
		h.next(t)
		if !h.claude.alive(t) {
			t.Fatal("control: the login process is not running before the cancel")
		}
		h.relay.Cancel("rl-3")
		h.waitEnded(t, "rl-3")
		if h.claude.alive(t) {
			t.Error("the login process survived the cancel")
		}
		h.wantNoReport(t)
	})

	t.Run("under no code within the cap, the process is killed and the relay reports expired", func(t *testing.T) {
		h := newRelayHarness(t)
		h.relay.cap = 2 * time.Second
		h.relay.Start("rl-4", "claude")
		if got := h.next(t); got.State != "awaiting_code" {
			t.Fatalf("first report = %+v, want awaiting_code", got)
		}
		if got, want := h.next(t), (loginReport{LoginID: "rl-4", State: "expired",
			Reason: "no code arrived within 2s"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		h.waitEnded(t, "rl-4")
		if h.claude.alive(t) {
			t.Error("the login process survived its cap")
		}
	})

	t.Run("under a server that answers a report with cancelled, the process is killed", func(t *testing.T) {
		h := newRelayHarness(t)
		h.answer = func(loginReport) string { return "cancelled" }
		h.relay.Start("rl-5", "claude")
		h.next(t)
		h.waitEnded(t, "rl-5")
		if h.claude.alive(t) {
			t.Error("the login process survived the server's cancelled answer")
		}
		h.wantNoReport(t)
	})

	t.Run("under a runtime this warden cannot log in, the relay reports failed and runs nothing", func(t *testing.T) {
		h := newRelayHarness(t)
		h.relay.Start("rl-6", "gemini")
		if got, want := h.next(t), (loginReport{LoginID: "rl-6", State: "failed",
			Reason: `this warden cannot log in runtime "gemini"`}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		if _, err := os.Stat(h.claude.pid); !os.IsNotExist(err) {
			t.Error("a login process ran for an unsupported runtime")
		}
	})

	t.Run("under a code for a login with no process, the relay reports failed", func(t *testing.T) {
		h := newRelayHarness(t)
		h.relay.Code("rl-7", fakeLoginCode)
		if got, want := h.next(t), (loginReport{LoginID: "rl-7", State: "failed",
			Reason: "no login process for this login is running on the machine"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
	})
}

// recordingLogin is a LoginSeam that only records what reached it.
type recordingLogin struct{ calls []string }

func (r *recordingLogin) Start(id, runtime string) {
	r.calls = append(r.calls, "start "+id+" "+runtime)
}
func (r *recordingLogin) Code(id, code string) { r.calls = append(r.calls, "code "+id+" "+code) }
func (r *recordingLogin) Cancel(id string)     { r.calls = append(r.calls, "cancel "+id) }
func (r *recordingLogin) Abandon()             { r.calls = append(r.calls, "abandon") }

func TestDispatchLoginCommands(t *testing.T) {
	frame := func(rpc, args string) []byte {
		return []byte(`{"topic":"warden-command","data":{"rpc":"` + rpc + `","args":` + args + `}}`)
	}
	t.Run("under the three login frames, each reaches the login seam with its arguments", func(t *testing.T) {
		seam := &recordingLogin{}
		for _, raw := range [][]byte{
			frame("login_start", `{"member_id":"m-box","login_id":"rl-1","runtime":"claude"}`),
			frame("login_code", `{"member_id":"m-box","login_id":"rl-1","code":"abc#def"}`),
			frame("login_cancel", `{"member_id":"m-box","login_id":"rl-1"}`),
		} {
			cmd, err := parseCommandFrame(raw)
			if err != nil {
				t.Fatalf("parse %s: %v", raw, err)
			}
			if err := dispatchCommand(cmd, CommandDeps{Login: seam}); err != nil {
				t.Fatalf("dispatch %s: %v", raw, err)
			}
		}
		want := []string{"start rl-1 claude", "code rl-1 abc#def", "cancel rl-1"}
		if !reflect.DeepEqual(seam.calls, want) {
			t.Errorf("seam calls = %v, want %v", seam.calls, want)
		}
	})

	t.Run("under a login frame with a malformed login_id, dispatch refuses it without quoting the code", func(t *testing.T) {
		seam := &recordingLogin{}
		cmd, err := parseCommandFrame(frame("login_code", `{"member_id":"m-box","login_id":"../x","code":"abc#def"}`))
		if err != nil {
			t.Fatal(err)
		}
		err = dispatchCommand(cmd, CommandDeps{Login: seam})
		if err == nil || err.Error() != "command: login frame missing a valid login_id" {
			t.Fatalf("err = %v", err)
		}
		if len(seam.calls) != 0 {
			t.Errorf("the seam was reached: %v", seam.calls)
		}
	})

	t.Run("under no login seam, dispatch refuses and sends no receipt", func(t *testing.T) {
		var receipts []CommandResult
		cmd, err := parseCommandFrame(frame("login_start", `{"member_id":"m-box","login_id":"rl-1","runtime":"claude"}`))
		if err != nil {
			t.Fatal(err)
		}
		err = dispatchCommand(cmd, CommandDeps{Report: func(cr CommandResult) error {
			receipts = append(receipts, cr)
			return nil
		}})
		if err == nil || err.Error() != "command: login_start refused: login seam not wired" {
			t.Fatalf("err = %v", err)
		}
		if len(receipts) != 0 {
			t.Errorf("a login frame produced a command_result receipt: %+v", receipts)
		}
	})
}

// spawnRunner answers the tmux probes a spawn makes and keeps the argv of
// every call, so the member's launch line can be taken out whole.
type spawnRunner struct{ argv [][]string }

func (r *spawnRunner) Run(name string, args ...string) (string, error) {
	r.argv = append(r.argv, append([]string{name}, args...))
	if len(args) > 2 && args[2] == "has-session" {
		return "", fmt.Errorf("can't find session")
	}
	if len(args) > 2 && args[2] == "display-message" {
		return "500\n", nil
	}
	return "", nil
}

func (r *spawnRunner) launchLine(t *testing.T, session string) string {
	t.Helper()
	for _, argv := range r.argv {
		if len(argv) > 7 && argv[0] == "tmux" && argv[3] == "new-session" && argv[6] == session {
			return argv[len(argv)-1]
		}
	}
	t.Fatalf("no new-session for %s in %v", session, r.argv)
	return ""
}

func readEnvDump(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env dump %s: %v", path, err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			out[k] = v
		}
	}
	return out
}

// TestLoginRunsUnderTheMemberSpawnEnvironment runs a real member launch line and
// a real login command against one fake claude that dumps its environment, and
// compares what each process saw.
func TestLoginRunsUnderTheMemberSpawnEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configDir bool
	}{
		{"under the default config home, the login sees exactly the environment a member sees, less the member's own OC_* and workdir PATH entry, with BROWSER the one deliberate difference", false},
		{"under a redirected CLAUDE_CONFIG_DIR, the login sees exactly the environment a member sees, less the member's own OC_* and workdir PATH entry, with BROWSER the one deliberate difference", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			agents := filepath.Join(root, "agents")
			envFile := filepath.Join(root, "env")
			if err := os.WriteFile(envFile, []byte("FROM_FILE=file-value\nCLAUDE_STRAY=stray\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			claudeBin := stageBinary(t, filepath.Join(root, "bin", "claude"), "#!/bin/sh\n"+
				`/usr/bin/env > '`+root+`/dump.'"$1$2"`+"\n"+
				`if [ "$1 $2" = "auth status" ]; then echo '{"loggedIn":true}'; fi`+"\n")
			ch := claudeHome{Home: home}
			if tc.configDir {
				ch.ConfigDir = filepath.Join(root, "claude-config")
			}
			cache := &launchEnvCache{}
			capture := func() (string, error) {
				return "FROM_SHELL=shell-value\x00CLAUDE_FROM_SHELL=leak\x00BROWSER=firefox\x00PATH=/usr/bin:/bin\x00", nil
			}

			runner := &spawnRunner{}
			spawn := SpawnDeps{
				Runner: runner, Base: "http://127.0.0.1:7755", Socket: "officraft", Home: agents,
				EnvFile: envFile, CaptureEnv: capture, LaunchEnv: cache,
				ClaudeBin: claudeBin, ClaudeHome: ch, RepoRoot: root,
				ResolveOcAgentBin: func() (string, bool) { return claudeBin, true },
				WriteFile:         osWriteFile, MkdirAll: os.MkdirAll, Symlink: os.Symlink, Remove: os.Remove,
				Sleep: func(time.Duration) {},
			}
			if out := spawn.start(StartParams{MemberID: "m1", PersonaContext: "you are m1", MemberToken: "jwt-m1"}); !out.OK {
				t.Fatalf("spawn refused: %+v", out)
			}
			if out, err := exec.Command("/bin/sh", "-c", runner.launchLine(t, "member-m1")).CombinedOutput(); err != nil {
				t.Fatalf("run member launch line: %v %s", err, out)
			}

			env := envMap(map[string]string{"HOME": home, "OC_CLAUDE_BIN": claudeBin})
			prober := newLoginProber(env, &wardenRunner{}, shellKeep{}, "linux", cache, nil)
			prober.claudeHome = ch
			prober.agentHome = agents
			prober.envFile = envFile
			prober.captureEnv = capture
			reports := make(chan loginReport, 4)
			relay := newLoginRelay(prober, func(rep loginReport) string { reports <- rep; return rep.State }, nil)
			relay.Start("rl-env", "claude")
			select {
			case <-reports:
			case <-time.After(10 * time.Second):
				t.Fatal("the login never finished")
			}

			member := readEnvDump(t, filepath.Join(root, "dump.--dangerously-skip-permissions--disallowedTools"))
			login := readEnvDump(t, filepath.Join(root, "dump.authlogin"))
			if member["FROM_FILE"] != "file-value" || member["FROM_SHELL"] != "shell-value" {
				t.Fatalf("control: the member did not get both env layers: %v", member)
			}
			workdir := filepath.Join(agents, "m1")
			if member["PATH"] != workdir+":"+login["PATH"] {
				t.Error("the member's PATH is not the workdir in front of the login's PATH")
			}
			if member["BROWSER"] != "firefox" || login["BROWSER"] != "/usr/bin/true" {
				t.Errorf("BROWSER: member=%q login=%q, want firefox and /usr/bin/true", member["BROWSER"], login["BROWSER"])
			}
			delete(member, "BROWSER")
			delete(login, "BROWSER")
			memberOnly := []string{"OC_TOKEN", "OC_BASE", "OC_SESSION", "OC_TMUX_SOCKET", "PATH", "PWD", "OLDPWD", "SHLVL", "_"}
			for _, k := range memberOnly {
				delete(member, k)
				delete(login, k)
			}
			if !reflect.DeepEqual(member, login) {
				keys := map[string]bool{}
				for k := range member {
					keys[k] = true
				}
				for k := range login {
					keys[k] = true
				}
				var diff []string
				for k := range keys {
					if member[k] != login[k] {
						diff = append(diff, k)
					}
				}
				sort.Strings(diff)
				// Names only: the inherited environment can carry credentials.
				t.Errorf("the login's environment differs from the member's in: %s", strings.Join(diff, " "))
			}
			wantConfigDir := ""
			if tc.configDir {
				wantConfigDir = ch.ConfigDir
			}
			if login["CLAUDE_CONFIG_DIR"] != wantConfigDir || login["HOME"] != home {
				t.Errorf("login CLAUDE_CONFIG_DIR=%q HOME=%q, want %q and %q",
					login["CLAUDE_CONFIG_DIR"], login["HOME"], wantConfigDir, home)
			}
		})
	}
}

func TestLoginRelaySweepStaleLoginFiles(t *testing.T) {
	t.Run("under renders and codex staging homes a previous warden process left, the sweep removes them and leaves other files", func(t *testing.T) {
		h := newRelayHarness(t)
		dir := h.prober.agentHome
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{loginRenderPrefix + "rl-old", loginRenderPrefix + "rl-older", loginCheckEnvName, "m1"} {
			h.claude.write(t, filepath.Join(dir, name), "SECRET=x\n")
		}
		staged := filepath.Join(dir, codexStagingPrefix+"rl-old-123")
		h.codex.write(t, filepath.Join(staged, "auth.json"), `{"tokens":{}}`)
		h.relay.sweepStaleLoginFiles()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var left []string
		for _, e := range entries {
			left = append(left, e.Name())
		}
		if want := []string{loginCheckEnvName, "m1"}; !reflect.DeepEqual(left, want) {
			t.Errorf("left after the sweep = %v, want %v", left, want)
		}
	})
}

func TestUpdaterExecAbandonsRunningLogins(t *testing.T) {
	t.Run("under a running login, the self-update exec first kills it and reports it failed", func(t *testing.T) {
		h := newRelayHarness(t)
		h.relay.Start("rl-x", "claude")
		h.next(t)
		if !h.claude.alive(t) {
			t.Fatal("control: the login process is not running")
		}
		var aliveAtExec []bool
		up := &updater{execSelf: func() error {
			aliveAtExec = append(aliveAtExec, h.claude.alive(t))
			return fmt.Errorf("exec refused in test")
		}}
		transport := &sseTransport{deps: CommandDeps{Login: h.relay}}
		wireUpdaterSeams(transport, up)
		if err := up.execSelf(); err == nil || err.Error() != "exec refused in test" {
			t.Fatalf("execSelf = %v", err)
		}
		if !reflect.DeepEqual(aliveAtExec, []bool{false}) {
			t.Errorf("login process alive at exec = %v, want [false]", aliveAtExec)
		}
		if got, want := h.next(t), (loginReport{LoginID: "rl-x", State: "failed",
			Reason: "the warden restarted to update itself; start the login again"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		h.wantNoReport(t)
	})
}

func TestStartLoginProcessRefusesARealBinaryInATestBinary(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		_, _ = startLoginProcess("/bin/sh", "exit 0", "/usr/bin/true")
		fmt.Println("startLoginProcess ran a binary outside the temp dir")
		return
	}
	code, out := runRefusalChild(t, "TestStartLoginProcessRefusesARealBinaryInATestBinary")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	want := "\nFATAL: refusing to run a real /usr/bin/true login inside a test binary.\n" +
		"Login tests must stage a fake CLI in a temp dir and inject it.\n"
	if !strings.Contains(out, want) {
		t.Errorf("child output =\n%s\nwant it to contain\n%s", out, want)
	}
	if strings.Contains(out, "startLoginProcess ran a binary outside the temp dir") {
		t.Error("a test binary was allowed to start a login process from a real binary")
	}
	fake := stageBinary(t, filepath.Join(t.TempDir(), "claude"), "#!/bin/sh\nexit 0\n")
	proc, err := startLoginProcess("/bin/sh", "exec "+fake, fake)
	if err != nil {
		t.Fatalf("control: a temp-dir fake could not start: %v", err)
	}
	if err := proc.wait(); err != nil {
		t.Fatalf("control: the fake did not exit 0: %v", err)
	}
}
