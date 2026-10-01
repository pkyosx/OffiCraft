package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	fakeCodexURL  = "https://auth.openai.com/codex/device"
	fakeCodexCode = "ABCD-EFGHI"
)

// fakeCodex is a stand-in for codex 0.159's `login --device-auth`: it records
// its pid and the FROM_FILE it started with, prints the colored device-code
// prompt (on stderr when the to-stderr file exists), then waits until the
// approve or deny file appears. deny's content goes to stderr and the exit is
// 1; approve exits 0 after "Successfully logged in". `login status` exits with
// the status-rc file's code (0 when absent).
type fakeCodex struct {
	root, bin, pid, sawEnv, toStderr, approve, deny, statusRC string
}

func newFakeCodex(t *testing.T) *fakeCodex {
	t.Helper()
	root := t.TempDir()
	f := &fakeCodex{
		root:     root,
		pid:      filepath.Join(root, "pid"),
		sawEnv:   filepath.Join(root, "saw-env"),
		toStderr: filepath.Join(root, "to-stderr"),
		approve:  filepath.Join(root, "approve"),
		deny:     filepath.Join(root, "deny"),
		statusRC: filepath.Join(root, "status-rc"),
	}
	prompt := `printf '\033[1mWelcome to Codex\033[0m [v0.159.2]\n\n` +
		`Follow these steps to sign in with ChatGPT using device code authorization:\n\n` +
		`1. Open this link in your browser and sign in to your account\n   \033[94m` + fakeCodexURL + `\033[0m\n\n` +
		`2. Enter this one-time code \033[90m(expires in 15 minutes)\033[0m\n   \033[94m` + fakeCodexCode + `\033[0m\n\n` +
		`\033[90mContinue only if you started this login in Codex. If a website or another person gave you this code, cancel.\033[0m\n'`
	f.bin = stageBinary(t, filepath.Join(root, "bin", "codex"), "#!/bin/sh\n"+
		`if [ "$1 $2" = "login status" ]; then [ -f '`+f.statusRC+`' ] && exit "$(/bin/cat '`+f.statusRC+`')"; echo 'Logged in using ChatGPT' >&2; exit 0; fi`+"\n"+
		`echo $$ > '`+f.pid+`'`+"\n"+
		`printf '%s' "${FROM_FILE-unset}" > '`+f.sawEnv+`'`+"\n"+
		`if [ -f '`+f.toStderr+`' ]; then `+prompt+` >&2; else `+prompt+`; fi`+"\n"+
		`while [ ! -f '`+f.approve+`' ] && [ ! -f '`+f.deny+`' ]; do /bin/sleep 0.05; done`+"\n"+
		`if [ -f '`+f.deny+`' ]; then /bin/cat '`+f.deny+`' >&2; exit 1; fi`+"\n"+
		`echo 'Successfully logged in'; exit 0`+"\n")
	return f
}

func (f *fakeCodex) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// codexAuthFile writes an auth.json whose id_token payload carries email.
func codexAuthFile(t *testing.T, f *fakeCodex, dir, email string) {
	t.Helper()
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"email":"` + email + `","https://api.openai.com/auth":{"chatgpt_plan_type":"plus"}}`))
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	f.write(t, filepath.Join(dir, "auth.json"),
		`{"auth_mode":"chatgpt","tokens":{"id_token":"`+header+`.`+payload+`.sig",`+
			`"access_token":"secret-access","refresh_token":"secret-refresh"}}`)
}

// newCodexHarness points the login's CODEX_HOME at a temp dir through the env
// file, so the status/auth.json read is proven to run under the member's env.
func newCodexHarness(t *testing.T) (*relayHarness, string) {
	t.Helper()
	h := newRelayHarness(t)
	h.relay.now = func() time.Time { return time.Unix(1_800_000_000, 0) }
	codexHome := filepath.Join(h.codex.root, "codex-home")
	h.codex.write(t, h.prober.envFile, "CODEX_HOME="+codexHome+"\nFROM_FILE=file-value\n")
	return h, codexHome
}

func TestCodexLoginRelay(t *testing.T) {
	awaiting := func(id string) loginReport {
		return loginReport{LoginID: id, State: "awaiting_authorization", AuthURL: fakeCodexURL,
			UserCode: fakeCodexCode, ExpiresTS: 1_800_000_900}
	}

	t.Run("under an approved device code, the relay reports awaiting_authorization with URL, code and expiry, then succeeded with the email and kicks a heartbeat", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		codexAuthFile(t, h.codex, codexHome, "owner@example.test")
		h.relay.Start("rl-c1", "codex")
		if got := h.next(t); got != awaiting("rl-c1") {
			t.Fatalf("first report = %+v, want %+v", got, awaiting("rl-c1"))
		}
		if raw, _ := os.ReadFile(h.codex.sawEnv); string(raw) != "file-value" {
			t.Fatalf("control: the login process saw FROM_FILE=%q", raw)
		}
		if _, err := os.Stat(filepath.Join(h.prober.agentHome, loginRenderPrefix+"rl-c1")); !os.IsNotExist(err) {
			t.Errorf("the env render is still on disk while the login runs (stat err %v)", err)
		}
		h.codex.write(t, h.codex.approve, "")
		got := h.next(t)
		want := loginReport{LoginID: "rl-c1", State: "succeeded", Account: &loginAccount{Email: "owner@example.test"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		select {
		case <-h.prober.kicked():
		default:
			t.Error("a finished codex login did not kick the heartbeat")
		}
		h.waitEnded(t, "rl-c1")
		entries, _ := os.ReadDir(h.prober.agentHome)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), loginRenderPrefix) {
				t.Errorf("an env render was left behind: %s", e.Name())
			}
		}
		h.wantLogsFree(t, fakeCodexCode, "secret-access", "secret-refresh", "owner@example.test")
	})

	t.Run("under a prompt printed on stderr, the same awaiting_authorization is reported", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.codex.write(t, h.codex.toStderr, "")
		h.relay.Start("rl-c2", "codex")
		if got := h.next(t); got != awaiting("rl-c2") {
			t.Fatalf("first report = %+v, want %+v", got, awaiting("rl-c2"))
		}
	})

	t.Run("under no auth.json (a keyring credential store), the login still succeeds, with no account", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.relay.Start("rl-c3", "codex")
		h.next(t)
		h.codex.write(t, h.codex.approve, "")
		if got, want := h.next(t), (loginReport{LoginID: "rl-c3", State: "succeeded"}); !reflect.DeepEqual(got, want) {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
	})

	t.Run("under codex login status still failing after exit 0, the relay reports failed", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.codex.write(t, h.codex.statusRC, "1")
		h.relay.Start("rl-c4", "codex")
		h.next(t)
		h.codex.write(t, h.codex.approve, "")
		if got, want := h.next(t), (loginReport{LoginID: "rl-c4", State: "failed",
			Reason: "codex login status still reports logged out after the login finished"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
	})

	t.Run("under a refused device login followed by a hint, the reason is the Error logging in line", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.relay.Start("rl-c5", "codex")
		h.next(t)
		h.codex.write(t, h.codex.deny,
			"Error logging in with device code: device code request failed with status 403\nTry the browser login instead\n")
		if got, want := h.next(t), (loginReport{LoginID: "rl-c5", State: "failed",
			Reason: "Error logging in with device code: device code request failed with status 403"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
	})

	t.Run("under a workspace without Codex followed by a hint, the reason is the not-enabled line", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.relay.Start("rl-c6", "codex")
		h.next(t)
		h.codex.write(t, h.codex.deny,
			"Codex is not enabled for your workspace. Contact your workspace administrator to request access to Codex.\nRun codex login again once access is granted\n")
		if got, want := h.next(t), (loginReport{LoginID: "rl-c6", State: "failed",
			Reason: "Codex is not enabled for your workspace. Contact your workspace administrator to request access to Codex."}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
	})

	t.Run("under a cancel, the codex login process is killed and nothing more is reported", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.relay.Start("rl-c7", "codex")
		h.next(t)
		h.relay.Cancel("rl-c7")
		h.waitEnded(t, "rl-c7")
		if pidAlive(t, h.codex.pid) {
			t.Error("the codex login process survived the cancel")
		}
		h.wantNoReport(t)
	})

	t.Run("under no approval within the cap, the process is killed and the relay reports expired", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.relay.codexCap = 2 * time.Second
		h.relay.Start("rl-c8", "codex")
		h.next(t)
		if got, want := h.next(t), (loginReport{LoginID: "rl-c8", State: "expired",
			Reason: "the one-time code was not approved within 2s"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		h.waitEnded(t, "rl-c8")
		if pidAlive(t, h.codex.pid) {
			t.Error("the codex login process survived its cap")
		}
	})

	t.Run("under codex not installed, the relay reports failed and runs nothing", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.prober.env = envMap(map[string]string{"HOME": h.prober.claudeHome.Home})
		h.relay.Start("rl-c9", "codex")
		if got, want := h.next(t), (loginReport{LoginID: "rl-c9", State: "failed",
			Reason: "Codex is not installed on this machine"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
	})
}

func pidAlive(t *testing.T, pidFile string) bool {
	t.Helper()
	f := &fakeClaude{pid: pidFile}
	return f.alive(t)
}

func TestCodexAuthEmail(t *testing.T) {
	payload := func(json string) string {
		return `{"tokens":{"id_token":"h.` + base64.RawURLEncoding.EncodeToString([]byte(json)) + `.s"}}`
	}
	cases := []struct{ name, in, want string }{
		{"under an id_token with an email claim, the email", payload(`{"email":"a@b.test"}`), "a@b.test"},
		{"under an id_token without an email claim, nothing", payload(`{"sub":"x"}`), ""},
		{"under an API-key auth.json without tokens, nothing", `{"OPENAI_API_KEY":"sk-x"}`, ""},
		{"under a malformed token, nothing", `{"tokens":{"id_token":"not-a-jwt"}}`, ""},
		{"under no auth.json output, nothing", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := codexAuthEmail(c.in); got != c.want {
				t.Errorf("codexAuthEmail = %q, want %q", got, c.want)
			}
		})
	}
}

// TestCodexLoginRunsUnderTheMemberSpawnEnvironment runs a real codex member
// launch line (its sidecar replaced by an env dump) and a real codex login
// against one environment, and compares what each process saw.
func TestCodexLoginRunsUnderTheMemberSpawnEnvironment(t *testing.T) {
	t.Run("under the same env file and shell layer, the login sees the member's environment less the member's own OC_* and workdir PATH entry, with BROWSER the one deliberate difference", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("PATH", "/usr/bin:/bin")
		agents := filepath.Join(root, "agents")
		envFile := filepath.Join(root, "env")
		if err := os.WriteFile(envFile, []byte("FROM_FILE=file-value\nCODEX_HOME="+root+"/codex-home\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		dumper := `/usr/bin/env > '` + root + `/dump.'"$1$2"` + "\n"
		wardenBin := stageBinary(t, filepath.Join(root, "bin", "ocwarden"), "#!/bin/sh\n"+dumper)
		codexBin := stageBinary(t, filepath.Join(root, "bin", "codex"), "#!/bin/sh\n"+dumper)
		cache := &launchEnvCache{}
		capture := func() (string, error) {
			return "FROM_SHELL=shell-value\x00BROWSER=firefox\x00PATH=/usr/bin:/bin\x00", nil
		}
		home := filepath.Join(root, "home")
		runner := &spawnRunner{}
		spawn := SpawnDeps{
			Runner: runner, Base: "http://127.0.0.1:7755", Socket: "officraft", Home: agents,
			EnvFile: envFile, CaptureEnv: capture, LaunchEnv: cache,
			CodexBin: codexBin, WardenBin: wardenBin, ClaudeHome: claudeHome{Home: home}, RepoRoot: root,
			ResolveOcAgentBin: func() (string, bool) { return wardenBin, true },
			WriteFile:         osWriteFile, MkdirAll: os.MkdirAll, Symlink: os.Symlink, Remove: os.Remove,
			Sleep: func(time.Duration) {},
		}
		if out := spawn.start(StartParams{MemberID: "m1", PersonaContext: "you are m1", MemberToken: "jwt-m1", Runtime: "codex"}); !out.OK {
			t.Fatalf("spawn refused: %+v", out)
		}
		if out, err := exec.Command("/bin/sh", "-c", runner.launchLine(t, "member-m1")).CombinedOutput(); err != nil {
			t.Fatalf("run member launch line: %v %s", err, out)
		}

		env := envMap(map[string]string{"HOME": home, "OC_CODEX_BIN": codexBin})
		prober := newLoginProber(env, &wardenRunner{}, shellKeep{}, "linux", cache, nil)
		prober.claudeHome = claudeHome{Home: home}
		prober.agentHome = agents
		prober.envFile = envFile
		prober.captureEnv = capture
		reports := make(chan loginReport, 4)
		relay := newLoginRelay(prober, func(rep loginReport) string { reports <- rep; return rep.State }, nil)
		relay.Start("rl-env", "codex")
		select {
		case <-reports:
		case <-time.After(10 * time.Second):
			t.Fatal("the codex login never finished")
		}

		member := readEnvDump(t, filepath.Join(root, "dump.codex-session--codex-bin"))
		login := readEnvDump(t, filepath.Join(root, "dump.login--device-auth"))
		if member["FROM_FILE"] != "file-value" || member["FROM_SHELL"] != "shell-value" || member["CODEX_HOME"] != root+"/codex-home" {
			t.Fatal("control: the member did not get both env layers")
		}
		workdir := filepath.Join(agents, "m1")
		if member["PATH"] != workdir+":"+login["PATH"] {
			t.Error("the member's PATH is not the workdir in front of the login's PATH")
		}
		if member["BROWSER"] != "firefox" || login["BROWSER"] != "/usr/bin/true" {
			t.Errorf("BROWSER: member=%q login=%q, want firefox and /usr/bin/true", member["BROWSER"], login["BROWSER"])
		}
		for _, k := range []string{"BROWSER", "OC_TOKEN", "OC_BASE", "OC_ID", "OC_SESSION", "OC_TMUX_SOCKET", "PATH", "PWD", "OLDPWD", "SHLVL", "_"} {
			delete(member, k)
			delete(login, k)
		}
		var diff []string
		for k := range member {
			if member[k] != login[k] {
				diff = append(diff, k)
			}
		}
		for k := range login {
			if _, ok := member[k]; !ok {
				diff = append(diff, k)
			}
		}
		if len(diff) > 0 {
			// Names only: the inherited environment can carry credentials.
			t.Errorf("the codex login's environment differs from the member's in: %s", strings.Join(diff, " "))
		}
	})
}
