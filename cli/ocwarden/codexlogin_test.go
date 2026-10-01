package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
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

// fakeCodex is a stand-in for codex 0.159's `login --device-auth`: like the
// real one it deletes $CODEX_HOME/auth.json as it starts. It records its pid,
// the FROM_FILE and CODEX_HOME it started with and the config.toml it found,
// prints the colored device-code prompt behind an unrelated bare https line (on
// stderr when the to-stderr file exists; without the code line when the
// no-code file exists); with the self-timeout file it then ends the way codex
// does after 15 minutes. Otherwise it waits until the approve or deny file
// appears. deny's content goes to stderr and the exit is 1; approve copies the
// issued file (when there is one) to $CODEX_HOME/auth.json and exits 0 after
// "Successfully logged in". `login status` exits 1 under the CODEX_HOME named
// in the refuse-home file, else with the status-rc file's code, else 0 exactly
// when $CODEX_HOME/auth.json exists.
type fakeCodex struct {
	root, bin, pid, sawEnv, sawHome, sawConfig, toStderr, approve, deny, statusRC, refuseHome, selfTimeout, noCode, issued string
}

func newFakeCodex(t *testing.T) *fakeCodex {
	t.Helper()
	root := t.TempDir()
	f := &fakeCodex{
		root:        root,
		pid:         filepath.Join(root, "pid"),
		sawEnv:      filepath.Join(root, "saw-env"),
		sawHome:     filepath.Join(root, "saw-home"),
		sawConfig:   filepath.Join(root, "saw-config"),
		toStderr:    filepath.Join(root, "to-stderr"),
		approve:     filepath.Join(root, "approve"),
		deny:        filepath.Join(root, "deny"),
		statusRC:    filepath.Join(root, "status-rc"),
		refuseHome:  filepath.Join(root, "refuse-home"),
		selfTimeout: filepath.Join(root, "self-timeout"),
		noCode:      filepath.Join(root, "no-code"),
		issued:      filepath.Join(root, "issued", "auth.json"),
	}
	codeLine := `printf '   \033[94m` + fakeCodexCode + `\033[0m\n'`
	prompt := `printf '\033[1mWelcome to Codex\033[0m [v0.159.2]\n` +
		`Docs:\n   https://developers.openai.com/codex\n\n` +
		`Follow these steps to sign in with ChatGPT using device code authorization:\n\n` +
		`1. Open this link in your browser and sign in to your account\n   \033[94m` + fakeCodexURL + `\033[0m\n\n` +
		`2. Enter this one-time code \033[90m(expires in 15 minutes)\033[0m\n'; ` +
		`[ -f '` + f.noCode + `' ] || ` + codeLine + `; ` +
		`printf '\n\033[90mContinue only if you started this login in Codex. If a website or another person gave you this code, cancel.\033[0m\n'`
	f.bin = stageBinary(t, filepath.Join(root, "bin", "codex"), "#!/bin/sh\n"+
		`__home="${CODEX_HOME:-/nonexistent-codex-home}"`+"\n"+
		`if [ "$1 $2" = "login status" ]; then [ -f '`+f.refuseHome+`' ] && [ "$(/bin/cat '`+f.refuseHome+`')" = "$__home" ] && exit 1; [ -f '`+f.statusRC+`' ] && exit "$(/bin/cat '`+f.statusRC+`')"; `+
		`[ -f "$__home/auth.json" ] || { echo 'Not logged in' >&2; exit 1; }; echo 'Logged in using ChatGPT' >&2; exit 0; fi`+"\n"+
		`echo $$ > '`+f.pid+`'`+"\n"+
		`printf '%s' "${FROM_FILE-unset}" > '`+f.sawEnv+`'`+"\n"+
		`printf '%s' "${CODEX_HOME-unset}" > '`+f.sawHome+`'`+"\n"+
		`[ -f "$__home/config.toml" ] && /bin/cp "$__home/config.toml" '`+f.sawConfig+`'`+"\n"+
		`/bin/rm -f "$__home/auth.json"`+"\n"+
		`if [ -f '`+f.toStderr+`' ]; then { `+prompt+`; } >&2; else `+prompt+`; fi`+"\n"+
		`if [ -f '`+f.selfTimeout+`' ]; then echo 'Error logging in with device code: device auth timed out after 15 minutes' >&2; exit 1; fi`+"\n"+
		`while [ ! -f '`+f.approve+`' ] && [ ! -f '`+f.deny+`' ]; do /bin/sleep 0.05; done`+"\n"+
		`if [ -f '`+f.deny+`' ]; then /bin/cat '`+f.deny+`' >&2; exit 1; fi`+"\n"+
		`[ -f '`+f.issued+`' ] && /bin/cp '`+f.issued+`' "$__home/auth.json"`+"\n"+
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

// newCodexHarness points the member's CODEX_HOME at a temp dir through the env
// file and gives it a working login (old@example.test), so every test can see
// whether that login survived.
func newCodexHarness(t *testing.T) (*relayHarness, string) {
	t.Helper()
	h := newRelayHarness(t)
	codexHome := filepath.Join(h.codex.root, "codex-home")
	h.codex.write(t, h.prober.envFile, "CODEX_HOME="+codexHome+"\nFROM_FILE=file-value\n")
	codexAuthFile(t, h.codex, codexHome, "old@example.test")
	if err := os.Chmod(filepath.Join(codexHome, "auth.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	return h, codexHome
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// wantCodexHomeUntouched checks the member's auth.json is byte-for-byte the one
// it had before the login, and that no staging home is left.
func wantCodexHomeUntouched(t *testing.T, h *relayHarness, codexHome string, before []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if err != nil {
		t.Fatalf("the existing codex login is gone: %v", err)
	}
	if !bytes.Equal(got, before) {
		t.Error("the existing codex login was rewritten")
	}
	wantNoStaging(t, h)
}

func stagingHomes(t *testing.T, h *relayHarness) []string {
	t.Helper()
	homes, err := filepath.Glob(filepath.Join(h.prober.agentHome, codexStagingPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	return homes
}

func wantNoStaging(t *testing.T, h *relayHarness) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(stagingHomes(t, h)) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("a codex staging home was left behind: %v", stagingHomes(t, h))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCodexLoginRelay(t *testing.T) {
	awaiting := func(id string) loginReport {
		return loginReport{LoginID: id, State: "awaiting_authorization", AuthURL: fakeCodexURL,
			UserCode: fakeCodexCode, ExpiresInS: 900}
	}

	t.Run("under an approved device code, the relay reports awaiting_authorization with URL, code and expiry, then succeeded with the email and kicks a heartbeat", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		codexAuthFile(t, h.codex, filepath.Dir(h.codex.issued), "owner@example.test")
		h.relay.Start("rl-c1", "codex")
		if got := h.next(t); got != awaiting("rl-c1") {
			t.Fatalf("first report = %+v, want %+v", got, awaiting("rl-c1"))
		}
		if raw, _ := os.ReadFile(h.codex.sawEnv); string(raw) != "file-value" {
			t.Fatalf("control: the login process saw FROM_FILE=%q", raw)
		}
		if _, err := os.Stat(filepath.Join(codexHome, "auth.json")); err != nil {
			t.Fatalf("the login running touched the member's auth.json: %v", err)
		}
		if _, err := os.Stat(filepath.Join(h.prober.agentHome, loginRenderPrefix+"rl-c1")); !os.IsNotExist(err) {
			t.Errorf("the env render is still on disk while the login runs (stat err %v)", err)
		}
		h.codex.write(t, h.codex.approve, "")
		got := h.next(t)
		want := loginReport{LoginID: "rl-c1", State: "succeeded", Account: &loginAccount{Email: "owner@example.test", Plan: "plus"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		if !bytes.Equal(readFile(t, filepath.Join(codexHome, "auth.json")), readFile(t, h.codex.issued)) {
			t.Error("the member's auth.json is not the one the login issued")
		}
		if info, err := os.Stat(filepath.Join(codexHome, "auth.json")); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("installed auth.json mode = %v (err %v), want 0600", info.Mode().Perm(), err)
		}
		if left, _ := filepath.Glob(filepath.Join(codexHome, ".auth.json*")); len(left) > 0 {
			t.Errorf("an install temp file was left behind: %v", left)
		}
		select {
		case <-h.prober.kicked():
		default:
			t.Error("a finished codex login did not kick the heartbeat")
		}
		h.waitEnded(t, "rl-c1")
		wantNoStaging(t, h)
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

	t.Run("under the login running, codex sees a 0700 staging CODEX_HOME carrying the member's config.toml, not the member's home", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		config := "chatgpt_base_url = \"https://chatgpt.example.test\"\ncli_auth_credentials_store = \"file\"\n"
		h.codex.write(t, filepath.Join(codexHome, "config.toml"), config)
		h.relay.Start("rl-cs", "codex")
		h.next(t)
		saw := string(readFile(t, h.codex.sawHome))
		homes := stagingHomes(t, h)
		if len(homes) != 1 || saw != homes[0] {
			t.Fatalf("the login ran under CODEX_HOME=%q, want the one staging home %v", saw, homes)
		}
		if info, err := os.Stat(saw); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("staging home mode = %v (err %v), want 0700", info.Mode().Perm(), err)
		}
		if got := string(readFile(t, h.codex.sawConfig)); got != config {
			t.Errorf("the staged config.toml = %q, want the member's %q", got, config)
		}
		if got := string(readFile(t, filepath.Join(saw, codexRealHomeMarker))); got != codexHome {
			t.Errorf("the staging home names %q as the real home, want %q", got, codexHome)
		}
	})

	t.Run("under codex exiting 0 without writing an auth.json, the relay reports failed and the existing login survives", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		before := readFile(t, filepath.Join(codexHome, "auth.json"))
		h.codex.write(t, h.codex.statusRC, "0")
		h.relay.Start("rl-c3", "codex")
		h.next(t)
		h.codex.write(t, h.codex.approve, "")
		if got, want := h.next(t), (loginReport{LoginID: "rl-c3", State: "failed",
			Reason: "codex finished the login but wrote no auth.json"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		wantCodexHomeUntouched(t, h, codexHome, before)
	})

	for _, tc := range []struct{ store, reason string }{
		{"keyring", "這台機器的 Codex 把登入存在系統鑰匙圈，OffiCraft 目前不支援"},
		{"auto", "這台機器的 Codex 把登入存在系統鑰匙圈，OffiCraft 目前不支援"},
		{"ephemeral", `這台機器的 Codex 設定 cli_auth_credentials_store = "ephemeral"，不把登入存成檔案，OffiCraft 目前不支援`},
	} {
		t.Run("under a "+tc.store+" credential store, the relay reports failed without running codex or staging a home", func(t *testing.T) {
			h, codexHome := newCodexHarness(t)
			before := readFile(t, filepath.Join(codexHome, "auth.json"))
			h.codex.write(t, filepath.Join(codexHome, "config.toml"), "model = \"gpt-5\"\n  cli_auth_credentials_store = '"+tc.store+"'\n")
			h.relay.Start("rl-ck", "codex")
			if got, want := h.next(t), (loginReport{LoginID: "rl-ck", State: "failed", Reason: tc.reason}); got != want {
				t.Fatalf("report = %+v, want %+v", got, want)
			}
			if _, err := os.Stat(h.codex.pid); !os.IsNotExist(err) {
				t.Error("codex login ran under a credential store the warden cannot stage")
			}
			if homes := stagingHomes(t, h); len(homes) > 0 {
				t.Errorf("a staging home was made: %v", homes)
			}
			wantCodexHomeUntouched(t, h, codexHome, before)
		})
	}

	t.Run("under codex login status still failing after exit 0, the relay reports failed and the existing login survives", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		before := readFile(t, filepath.Join(codexHome, "auth.json"))
		codexAuthFile(t, h.codex, filepath.Dir(h.codex.issued), "new@example.test")
		h.codex.write(t, h.codex.statusRC, "1")
		h.relay.Start("rl-c4", "codex")
		h.next(t)
		h.codex.write(t, h.codex.approve, "")
		if got, want := h.next(t), (loginReport{LoginID: "rl-c4", State: "failed",
			Reason: "codex login status still reports logged out after the login finished"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		wantCodexHomeUntouched(t, h, codexHome, before)
	})

	t.Run("under codex in the member environment still logged out after the install, the relay reports the install and that it did not take", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		codexAuthFile(t, h.codex, filepath.Dir(h.codex.issued), "new@example.test")
		h.codex.write(t, h.codex.refuseHome, codexHome)
		h.relay.Start("rl-ci", "codex")
		h.next(t)
		h.codex.write(t, h.codex.approve, "")
		if got, want := h.next(t), (loginReport{LoginID: "rl-ci", State: "failed",
			Reason: "the new login was installed but codex in the member environment still reports logged out"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		if !bytes.Equal(readFile(t, filepath.Join(codexHome, "auth.json")), readFile(t, h.codex.issued)) {
			t.Error("control: the issued login was not installed")
		}
	})

	t.Run("under a self-update during an install, Abandon returns only after the install is done", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		codexAuthFile(t, h.codex, filepath.Dir(h.codex.issued), "new@example.test")
		entered, release := make(chan struct{}), make(chan struct{})
		h.relay.installAuth = func(home string, auth []byte) error {
			close(entered)
			<-release
			return installCodexAuth(home, auth)
		}
		h.relay.Start("rl-cu", "codex")
		h.next(t)
		h.codex.write(t, h.codex.approve, "")
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("the install never started")
		}
		abandoned := make(chan struct{})
		go func() { h.relay.Abandon(); close(abandoned) }()
		select {
		case <-abandoned:
			t.Fatal("Abandon returned while an install was in progress")
		case <-time.After(300 * time.Millisecond):
		}
		close(release)
		select {
		case <-abandoned:
		case <-time.After(10 * time.Second):
			t.Fatal("Abandon never returned after the install finished")
		}
		if !bytes.Equal(readFile(t, filepath.Join(codexHome, "auth.json")), readFile(t, h.codex.issued)) {
			t.Error("the install did not complete")
		}
	})

	t.Run("under a refused device login followed by a hint, the reason is the Error logging in line and the existing login survives", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		before := readFile(t, filepath.Join(codexHome, "auth.json"))
		h.relay.Start("rl-c5", "codex")
		h.next(t)
		h.codex.write(t, h.codex.deny,
			"Error logging in with device code: device code request failed with status 403\nTry the browser login instead\n")
		if got, want := h.next(t), (loginReport{LoginID: "rl-c5", State: "failed",
			Reason: "Error logging in with device code: device code request failed with status 403"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		wantCodexHomeUntouched(t, h, codexHome, before)
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

	t.Run("under codex timing its own one-time code out, the relay reports expired with codex's line and the existing login survives", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		before := readFile(t, filepath.Join(codexHome, "auth.json"))
		h.codex.write(t, h.codex.selfTimeout, "")
		h.relay.Start("rl-ct", "codex")
		if got := h.next(t); got != awaiting("rl-ct") {
			t.Fatalf("first report = %+v, want %+v", got, awaiting("rl-ct"))
		}
		if got, want := h.next(t), (loginReport{LoginID: "rl-ct", State: "expired",
			Reason: "Error logging in with device code: device auth timed out after 15 minutes"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		wantCodexHomeUntouched(t, h, codexHome, before)
	})

	t.Run("under a URL but no readable one-time code, the relay reports failed at once and kills the process", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.relay.codeWait = 300 * time.Millisecond
		h.codex.write(t, h.codex.noCode, "")
		h.relay.Start("rl-cn", "codex")
		if got, want := h.next(t), (loginReport{LoginID: "rl-cn", State: "failed",
			Reason: "could not read the one-time code from codex's output"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		h.waitEnded(t, "rl-cn")
		if pidAlive(t, h.codex.pid) {
			t.Error("the codex login process survived the failed report")
		}
		h.wantNoReport(t)
	})

	t.Run("under an issued auth.json over 1 MiB, the login installs it without reading an account", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		h.codex.write(t, h.codex.issued,
			`{"tokens":{"id_token":"h.`+base64.RawURLEncoding.EncodeToString([]byte(`{"email":"big@example.test"}`))+
				`.s"},"pad":"`+strings.Repeat("x", 1<<20)+`"}`)
		h.relay.Start("rl-cb", "codex")
		h.next(t)
		h.codex.write(t, h.codex.approve, "")
		if got, want := h.next(t), (loginReport{LoginID: "rl-cb", State: "succeeded"}); !reflect.DeepEqual(got, want) {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
		if !bytes.Equal(readFile(t, filepath.Join(codexHome, "auth.json")), readFile(t, h.codex.issued)) {
			t.Error("the member's auth.json is not the one the login issued")
		}
	})

	t.Run("under an unrelated timeout from codex, the login stays failed rather than expired", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.relay.Start("rl-cx", "codex")
		h.next(t)
		h.codex.write(t, h.codex.deny, "Error logging in with device code: error sending request: operation timed out\n")
		if got, want := h.next(t), (loginReport{LoginID: "rl-cx", State: "failed",
			Reason: "Error logging in with device code: error sending request: operation timed out"}); got != want {
			t.Fatalf("final report = %+v, want %+v", got, want)
		}
	})

	t.Run("under a cancel, the codex login process is killed, nothing more is reported and the existing login survives", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		before := readFile(t, filepath.Join(codexHome, "auth.json"))
		h.relay.Start("rl-c7", "codex")
		h.next(t)
		h.relay.Cancel("rl-c7")
		h.waitEnded(t, "rl-c7")
		if pidAlive(t, h.codex.pid) {
			t.Error("the codex login process survived the cancel")
		}
		h.wantNoReport(t)
		wantCodexHomeUntouched(t, h, codexHome, before)
	})

	t.Run("under a self-update abandoning the login, the existing login survives and the staging home is removed", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		before := readFile(t, filepath.Join(codexHome, "auth.json"))
		h.relay.Start("rl-ca", "codex")
		h.next(t)
		h.relay.Abandon()
		if got, want := h.next(t), (loginReport{LoginID: "rl-ca", State: "failed",
			Reason: "the warden restarted to update itself; start the login again"}); got != want {
			t.Fatalf("report = %+v, want %+v", got, want)
		}
		wantCodexHomeUntouched(t, h, codexHome, before)
	})

	t.Run("under no approval within the cap, the process is killed, the relay reports expired and the existing login survives", func(t *testing.T) {
		h, codexHome := newCodexHarness(t)
		before := readFile(t, filepath.Join(codexHome, "auth.json"))
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
		wantCodexHomeUntouched(t, h, codexHome, before)
	})

	t.Run("under codex not installed, the relay reports failed and runs nothing", func(t *testing.T) {
		h, _ := newCodexHarness(t)
		h.relay.resolveBin = func(string) string { return "" }
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

func TestInstallCodexAuthRefusesARealHomeInATestBinary(t *testing.T) {
	if os.Getenv("OCWARDEN_REFUSAL_CHILD") == "1" {
		_ = installCodexAuth("/nonexistent-officraft-test/codex-home", []byte("x"))
		fmt.Println("installCodexAuth went ahead on a home outside the temp dir")
		return
	}
	code, out := runRefusalChild(t, "TestInstallCodexAuthRefusesARealHomeInATestBinary")
	if code != 1 || !strings.Contains(out, "FATAL: refusing to write a real codex home (/nonexistent-officraft-test/codex-home) inside a test binary.") {
		t.Errorf("exit code = %d, output =\n%s", code, out)
	}
	if strings.Contains(out, "went ahead") {
		t.Error("a test binary was allowed to install a codex login outside the temp dir")
	}
	home := filepath.Join(t.TempDir(), "not-yet", "codex-home")
	if err := installCodexAuth(home, []byte("x")); err != nil {
		t.Fatalf("control: a temp-dir home that does not exist yet was refused: %v", err)
	}
}

func TestCodexAuthAccount(t *testing.T) {
	payload := func(json string) string {
		return `{"tokens":{"id_token":"h.` + base64.RawURLEncoding.EncodeToString([]byte(json)) + `.s"}}`
	}
	cases := []struct {
		name, in string
		want     loginAccount
	}{
		{"under an id_token with email and plan claims, both",
			payload(`{"email":"a@b.test","https://api.openai.com/auth":{"chatgpt_plan_type":"team"}}`),
			loginAccount{Email: "a@b.test", Plan: "team"}},
		{"under an id_token with an email but no plan, the email only", payload(`{"email":"a@b.test"}`), loginAccount{Email: "a@b.test"}},
		{"under an id_token segment with base64 = padding, the claims still decode",
			`{"tokens":{"id_token":"h.` + base64.URLEncoding.EncodeToString([]byte(`{"email":"a@c.test"}`)) + `.s"}}`,
			loginAccount{Email: "a@c.test"}},
		{"under an id_token without either claim, nothing", payload(`{"sub":"x"}`), loginAccount{}},
		{"under an API-key auth.json without tokens, nothing", `{"OPENAI_API_KEY":"sk-x"}`, loginAccount{}},
		{"under a malformed token, nothing", `{"tokens":{"id_token":"not-a-jwt"}}`, loginAccount{}},
		{"under no auth.json output, nothing", "", loginAccount{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := codexAuthAccount(c.in); got != c.want {
				t.Errorf("codexAuthAccount = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestCodexLoginRunsUnderTheMemberSpawnEnvironment runs a real codex member
// launch line (its sidecar replaced by an env dump) and a real codex login
// against one environment, and compares what each process saw.
func TestCodexLoginRunsUnderTheMemberSpawnEnvironment(t *testing.T) {
	t.Run("under the same env file and shell layer, the login sees the member's environment less the member's own OC_* and workdir PATH entry, with BROWSER and a staging CODEX_HOME the deliberate differences, and its login lands in the member's CODEX_HOME", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("PATH", "/usr/bin:/bin")
		agents := filepath.Join(root, "agents")
		envFile := filepath.Join(root, "env")
		if err := os.WriteFile(envFile, []byte("FROM_FILE=file-value\nCODEX_HOME="+root+"/codex-home\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		dumper := `/usr/bin/env > '` + root + `/dump.'"$1$2"` + "\n"
		wardenBin := stageBinary(t, filepath.Join(root, "bin", "ocwarden"), "#!/bin/sh\n"+dumper)
		codexBin := stageBinary(t, filepath.Join(root, "bin", "codex"), "#!/bin/sh\n"+dumper+
			`[ "$1 $2" = "login --device-auth" ] && printf 'issued-login' > "$CODEX_HOME/auth.json"`+"\n"+"exit 0\n")
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
		case rep := <-reports:
			if rep.State != "succeeded" {
				t.Fatalf("the codex login ended %+v, want succeeded", rep)
			}
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
		if !strings.HasPrefix(login["CODEX_HOME"], filepath.Join(agents, codexStagingPrefix+"rl-env-")) {
			t.Errorf("the login ran under CODEX_HOME=%q, want a staging home under %s", login["CODEX_HOME"], agents)
		}
		if got, err := os.ReadFile(filepath.Join(member["CODEX_HOME"], "auth.json")); err != nil || string(got) != "issued-login" {
			t.Errorf("the member's CODEX_HOME holds %q (err %v), want the login the staging home issued", got, err)
		}
		for _, k := range []string{"BROWSER", "CODEX_HOME", "OC_TOKEN", "OC_BASE", "OC_ID", "OC_SESSION", "OC_TMUX_SOCKET", "PATH", "PWD", "OLDPWD", "SHLVL", "_"} {
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
