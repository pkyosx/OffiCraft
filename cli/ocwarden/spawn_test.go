package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writtenFile is one file the spawn asked its write seam to lay down.
type writtenFile struct {
	path    string
	content string
	mode    os.FileMode
}

// spawnHarness records every side effect a spawn is allowed to have and hands
// back a SpawnDeps wired entirely to those recorders.
type spawnHarness struct {
	runner    *wardenRunner
	writes    []writtenFile
	writeErr  map[string]error
	mkdirs    []string
	mkdirErr  error
	symlinks  [][2]string
	symlinkEr error
	removes   []string
	removeErr map[string]error
	logs      []string
	slept     []time.Duration
	pretrusts int
	pretrustE error
	verifies  [][2]string
	verifyE   error
	purges    int
}

func (h *spawnHarness) deps() SpawnDeps {
	return SpawnDeps{
		Runner: h.runner,
		Base:   "http://127.0.0.1:7755/",
		Socket: "officraft",
		Home:   "/w",
		// The warden's own HOME, deliberately NOT the agents root above: the two
		// are different things and a fixture that spelled them the same could not
		// tell a launch line that exported the wrong one.
		ClaudeHome: claudeHome{Home: "/Users/wardenowner"},
		ClaudeBin:  "/usr/local/bin/claude",
		RepoRoot:   "/repo",
		ResolveOcAgentBin: func() (string, bool) {
			return "/Users/eva/.officraft/warden/ocagent", true
		},
		WriteFile: func(path, content string, mode os.FileMode) error {
			if err := h.writeErr[path]; err != nil {
				return err
			}
			h.writes = append(h.writes, writtenFile{path, content, mode})
			return nil
		},
		MkdirAll: func(path string, _ os.FileMode) error {
			h.mkdirs = append(h.mkdirs, path)
			return h.mkdirErr
		},
		Symlink: func(oldname, newname string) error {
			h.symlinks = append(h.symlinks, [2]string{oldname, newname})
			return h.symlinkEr
		},
		Remove: func(name string) error {
			h.removes = append(h.removes, name)
			if err := h.removeErr[name]; err != nil {
				return err
			}
			return os.ErrNotExist
		},
		Logf:     func(format string, a ...any) { h.logs = append(h.logs, fmt.Sprintf(format, a...)) },
		Pretrust: func() error { h.pretrusts++; return h.pretrustE },
		VerifyPretrust: func(workdir, envRendered string) error {
			h.verifies = append(h.verifies, [2]string{workdir, envRendered})
			return h.verifyE
		},
		PurgeTrash: func() { h.purges++ },
		Sleep:      func(d time.Duration) { h.slept = append(h.slept, d) },
	}
}

func newSpawnHarness() *spawnHarness {
	return &spawnHarness{
		runner: &wardenRunner{script: map[string]wardenRun{
			"tmux -L officraft has-session -t member-m1":                    {err: errors.New("can't find session: member-m1")},
			"tmux -L officraft display-message -p -t member-m1 #{pane_pid}": {out: "500\n"},
		}},
		writeErr:  map[string]error{},
		removeErr: map[string]error{},
	}
}

func startParamsM1() StartParams {
	return StartParams{MemberID: "m1", PersonaContext: "you are m1", MemberToken: "jwt-m1", Role: "builder"}
}

// goldenClaudePurge is the CLAUDE_* family purge as it must appear on every
// claude launch line — TYPED OUT HERE rather than called from
// claudeEnvPurgeFragment, so a change to the fragment has to be re-justified
// against a literal instead of agreeing with itself.
const goldenClaudePurge = `for __oc_e in $(/usr/bin/env); do case $__oc_e in CLAUDE_CODE_USE_BEDROCK=*|CLAUDE_CODE_USE_VERTEX=*) continue;; CLAUDE_*=*) ;; *) continue;; esac; __oc_n=${__oc_e%%=*}; case $__oc_n in *[!A-Za-z0-9_]*) continue;; esac; unset "$__oc_n"; done; unset __oc_e __oc_n; `

const goldenLaunchM1 = `cd /w/m1; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
	`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft OC_EFFORT=medium ` +
	`HOME=/Users/wardenowner; ` +
	`export PATH=/w/m1:"$PATH"; ` +
	`exec /usr/local/bin/claude --dangerously-skip-permissions ` +
	`--disallowedTools AskUserQuestion --mcp-config /w/m1/.mcp.json --effort medium ` +
	`--append-system-prompt '你是 m1(role=builder)。你的完整身分、操作準則與開機程序都由 launcher 預抓在本地檔 ` +
	`/w/m1/persona.md。第一步:用 Read 工具把它從頭到尾整份讀完 —— 不要帶 offset/limit,不要只讀開頭,` +
	`也不准用 cat/head/tail/sed 或任何終端機指令讀它:這個檔有數萬字元,終端機輸出只有開頭一小段會進到你的 context,` +
	`其餘會被靜默丟棄而且不會有任何錯誤訊息,而「開機程序」在整份檔案的最後面。` +
	`整份讀完後,照裡面「開機程序」段逐步執行。' --settings /w/m1/settings.json`

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "''"},
		{"abc", "abc"},
		{"a@b%c+d=e:f,g.h/i-j_k", "a@b%c+d=e:f,g.h/i-j_k"},
		{"/Users/eva/.officraft/agents/m1", "/Users/eva/.officraft/agents/m1"},
		{"a b", "'a b'"},
		{"it's", `'it'"'"'s'`},
		{"'", `''"'"''`},
		{"$(rm -rf /)", "'$(rm -rf /)'"},
		{"開始。", "'開始。'"},
		{"a\nb", "'a\nb'"},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestJsonStr(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", `""`},
		{"Bearer jwt-m1", `"Bearer jwt-m1"`},
		{"開始。", `"開始。"`},
		{"a<b>&c", `"a<b>&c"`},
		{"q\"x\n\t", `"q\"x\n\t"`},
		{`C:\path`, `"C:\\path"`},
	}
	for _, c := range cases {
		if got := jsonStr(c.in); got != c.want {
			t.Errorf("jsonStr(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestBuildMCPConfig(t *testing.T) {
	want := `{
  "mcpServers": {
    "officraft": {
      "type": "http",
      "url": "http://127.0.0.1:7755/api/mcp",
      "headers": {
        "Authorization": "Bearer jwt-m1"
      }
    }
  }
}`
	if got := buildMCPConfig("http://127.0.0.1:7755", "jwt-m1"); got != want {
		t.Errorf("buildMCPConfig =\n%s\nwant\n%s", got, want)
	}

	wantTokenless := `{
  "mcpServers": {
    "officraft": {
      "type": "http",
      "url": "http://127.0.0.1:7755/api/mcp"
    }
  }
}`
	if got := buildMCPConfig("http://127.0.0.1:7755", ""); got != wantTokenless {
		t.Errorf("a token-less config =\n%s\nwant\n%s", got, wantTokenless)
	}
}

func TestBuildStatuslineSettings(t *testing.T) {
	want := `{
  "statusLine": {
    "type": "command",
    "command": "ocagent context-report"
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "ocagent guard-bash"
          }
        ]
      }
    ],
    "PermissionRequest": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "ocagent guard-permission"
          }
        ]
      }
    ]
  }
}
`
	if got := buildStatuslineSettings(); got != want {
		t.Errorf("buildStatuslineSettings =\n%q\nwant\n%q", got, want)
	}

	// The golden only says the two strings are equal, so a comma dropped from
	// this hand-assembled JSON survives it the moment somebody refreshes the
	// golden from the output. An unparsable settings.json is rejected whole:
	// statusLine and both guard hooks go down together.
	var parsed any
	if err := json.Unmarshal([]byte(buildStatuslineSettings()), &parsed); err != nil {
		t.Fatalf("the settings.json written for every member is not valid JSON: %v", err)
	}
}

func TestBuildAppendSystemPrompt(t *testing.T) {
	want := "你是 m1(role=builder)。你的完整身分、操作準則與開機程序都由 launcher 預抓在本地檔 " +
		"/w/m1/persona.md。第一步:用 Read 工具把它從頭到尾整份讀完 —— " +
		"不要帶 offset/limit,不要只讀開頭,也不准用 cat/head/tail/sed 或任何終端機指令讀它:" +
		"這個檔有數萬字元,終端機輸出只有開頭一小段會進到你的 context," +
		"其餘會被靜默丟棄而且不會有任何錯誤訊息,而「開機程序」在整份檔案的最後面。" +
		"整份讀完後,照裡面「開機程序」段逐步執行。"
	if got := buildAppendSystemPrompt("m1", "builder", "/w/m1/persona.md"); got != want {
		t.Errorf("prompt =\n%q\nwant\n%q", got, want)
	}
	if got := buildAppendSystemPrompt("m1", "builder", "/w/m1/persona.md"); strings.Contains(got, "you are m1") {
		t.Error("the persona itself must ride the file, never this prompt")
	}
}

func TestOcAgentSymlinkTarget(t *testing.T) {
	if got := ocAgentSymlinkTarget("/repo", "/Users/eva/.officraft/warden/ocagent"); got != "/Users/eva/.officraft/warden/ocagent" {
		t.Errorf("the resolved binary wins, got %q", got)
	}
	if got := ocAgentSymlinkTarget("/repo", ""); got != "/repo/cli/ocagent/ocagent" {
		t.Errorf("the dev fallback = %q, want /repo/cli/ocagent/ocagent", got)
	}
}

func TestOcAgentTarget(t *testing.T) {
	d := SpawnDeps{ResolveOcAgentBin: func() (string, bool) { return "/home/.officraft/warden/ocagent", true }}
	if path, ok := d.ocAgentTarget(); path != "/home/.officraft/warden/ocagent" || !ok {
		t.Errorf("got (%q, %v), want (\"/home/.officraft/warden/ocagent\", true)", path, ok)
	}
	d = SpawnDeps{ResolveOcAgentBin: func() (string, bool) { return "/gone/ocagent", false }}
	if path, ok := d.ocAgentTarget(); path != "/gone/ocagent" || ok {
		t.Errorf("got (%q, %v), want (\"/gone/ocagent\", false)", path, ok)
	}
	if path, ok := (SpawnDeps{}).ocAgentTarget(); path != "" || ok {
		t.Errorf("an unwired resolver = (%q, %v), want (\"\", false)", path, ok)
	}
}

func TestBuildLaunchCommand(t *testing.T) {
	want := `cd /w/m1; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
		`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft ` +
		`HOME=/Users/wardenowner; ` +
		`export PATH=/w/m1:"$PATH"; ` +
		`exec /usr/local/bin/claude --dangerously-skip-permissions --disallowedTools AskUserQuestion ` +
		`--mcp-config /w/m1/.mcp.json --effort medium --append-system-prompt APPEND`
	got := buildLaunchCommand("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", "APPEND",
		"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "",
		claudeHome{Home: "/Users/wardenowner"})
	if got != want {
		t.Errorf("launch line =\n%s\nwant\n%s", got, want)
	}

	wantFull := `cd /w/m1; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
		`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft ` +
		`HOME=/Users/wardenowner; ` +
		`export PATH=/w/m1:"$PATH"; ` +
		`exec /usr/local/bin/claude --dangerously-skip-permissions --disallowedTools AskUserQuestion ` +
		`--mcp-config /w/m1/.mcp.json --effort high --append-system-prompt APPEND ` +
		`--model opus --settings /w/m1/settings.json`
	got = buildLaunchCommand("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", "APPEND",
		"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft",
		"opus", "high", "/w/m1/settings.json", claudeHome{Home: "/Users/wardenowner"})
	if got != wantFull {
		t.Errorf("launch line =\n%s\nwant\n%s", got, wantFull)
	}
}

func TestBuildLaunchCommandWithEnv(t *testing.T) {
	home := claudeHome{Home: "/Users/wardenowner"}
	want := `cd /w/m1; [ -f /w/m1/.oc-env ] && . /w/m1/.oc-env; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; ` +
		`export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
		`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft-lab ` +
		`OC_AGENT_HOME=/w OC_EFFORT=medium HOME=/Users/wardenowner; ` +
		`export PATH=/w/m1:"$PATH"; ` +
		`exec /usr/local/bin/claude --dangerously-skip-permissions --disallowedTools AskUserQuestion ` +
		`--mcp-config /w/m1/.mcp.json --effort medium --append-system-prompt APPEND ` +
		`--settings /w/m1/settings.json`
	got := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", "APPEND",
		"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft-lab", "", "medium",
		"/w/m1/settings.json", [][2]string{{"OC_AGENT_HOME", "/w"}, {"OC_EFFORT", "medium"}}, "/w/m1/.oc-env", home)
	if got != want {
		t.Errorf("launch line =\n%s\nwant\n%s", got, want)
	}

	plain := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", "APPEND",
		"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "", home)
	if plain != buildLaunchCommand("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", "APPEND",
		"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", home) {
		t.Errorf("no extra env must be byte-identical to the plain line, got\n%s", plain)
	}

	spaced := buildLaunchCommandWithEnv("/opt/my claude/claude", "/w/a b", "/w/a b/.mcp.json", "it's me",
		"/w/a b/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "/w/a b/.oc-env",
		claudeHome{Home: "/Users/warden owner"})
	wantSpaced := `cd '/w/a b'; [ -f '/w/a b/.oc-env' ] && . '/w/a b/.oc-env'; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; ` +
		`export OC_TOKEN="$(/bin/cat '/w/a b/.oc-token')" ` +
		`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft ` +
		`HOME='/Users/warden owner'; ` +
		`export PATH='/w/a b':"$PATH"; ` +
		`exec '/opt/my claude/claude' --dangerously-skip-permissions --disallowedTools AskUserQuestion ` +
		`--mcp-config '/w/a b/.mcp.json' --effort medium --append-system-prompt 'it'"'"'s me'`
	if spaced != wantSpaced {
		t.Errorf("launch line =\n%s\nwant\n%s", spaced, wantSpaced)
	}

	t.Run("a redirected config home is exported instead of unset", func(t *testing.T) {
		line := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", "APPEND",
			"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "/w/m1/.oc-env",
			claudeHome{Home: "/Users/wardenowner", ConfigDir: "/tmp/box"})
		if !strings.Contains(line, "CLAUDE_CONFIG_DIR=/tmp/box") {
			t.Errorf("a stated config dir must be exported:\n%s", line)
		}
		if strings.Contains(line, "unset CLAUDE_CONFIG_DIR") {
			t.Errorf("exporting it and unsetting it are exclusive:\n%s", line)
		}
	})

	t.Run("the config-home pins come after the sourced agent env", func(t *testing.T) {
		// The whole guarantee is positional: these exports overrule the owner's
		// file because they run later. Emitted before the source line they are
		// silently erased by it, and every other assertion here still passes.
		line := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", "APPEND",
			"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "/w/m1/.oc-env",
			claudeHome{Home: "/Users/wardenowner", ConfigDir: "/tmp/box"})
		source := strings.Index(line, ". /w/m1/.oc-env")
		pinHome := strings.Index(line, "HOME=/Users/wardenowner")
		pinDir := strings.Index(line, "CLAUDE_CONFIG_DIR=/tmp/box")
		if source < 0 || pinHome < 0 || pinDir < 0 {
			t.Fatalf("line is missing the source or a pin:\n%s", line)
		}
		if source > pinHome || source > pinDir {
			t.Errorf("source at %d must precede the pins (HOME=%d, CLAUDE_CONFIG_DIR=%d):\n%s", source, pinHome, pinDir, line)
		}
		unsetLine := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", "APPEND",
			"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "/w/m1/.oc-env",
			claudeHome{Home: "/Users/wardenowner"})
		if src, un := strings.Index(unsetLine, ". /w/m1/.oc-env"), strings.Index(unsetLine, "unset CLAUDE_CONFIG_DIR"); src < 0 || un < 0 || src > un {
			t.Errorf("the unset must follow the source (source=%d unset=%d):\n%s", src, un, unsetLine)
		}
	})

	t.Run("an unknown CLAUDE_ variable does not survive into the child environment", func(t *testing.T) {
		// THE FALSIFIABLE CONDITION for the whole shape. Three reviews each broke
		// the previous version by finding one more variable that redirects the
		// child's config read, so this test names a variable that does not exist
		// and never will: if the line only deleted the names we know about, this
		// is where that shows up.
		//
		// It runs the REAL launch line in a REAL shell rather than asserting on
		// the string. A string assertion cannot tell a purge that works from one
		// that word-splits wrong, matches the wrong pattern, or resolves no
		// binary — all of which look identical in the emitted text.
		//
		// AND IN EVERY SHELL THAT COULD RUN IT, not just /bin/sh. tmux runs the
		// launch line under its default-shell, which on these machines is
		// /bin/zsh — so a suite pinned to /bin/sh measures a dialect production
		// never uses, and claudehome.go's "verified under zsh/bash/sh/dash" line
		// would be prose nobody re-runs. The loop is what makes that sentence a
		// measurement.
		workdir := t.TempDir()
		render := filepath.Join(workdir, ".oc-env")
		if err := os.WriteFile(render, []byte("export CLAUDE_FROM_THE_ENV_FILE=1\n"), 0o600); err != nil {
			t.Fatalf("seed render: %v", err)
		}
		line := buildLaunchCommandWithEnv("/usr/local/bin/claude", workdir, "/w/m1/.mcp.json", "APPEND",
			"/dev/null", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, render,
			claudeHome{Home: "/Users/wardenowner"})
		execAt := strings.Index(line, "; exec ")
		if execAt < 0 {
			t.Fatalf("launch line has no exec clause:\n%s", line)
		}
		// Everything the line does to the environment, then a dump instead of claude.
		script := line[:execAt] + "; /usr/bin/env"
		for _, shell := range []string{"/bin/zsh", "/bin/bash", "/bin/sh", "/bin/dash"} {
			if _, err := os.Stat(shell); err != nil {
				continue
			}
			t.Run(shell, func(t *testing.T) { assertPurgedUnder(t, shell, script) })
		}
	})

	t.Run("a pin cannot be overridden by an extra env pair of the same name", func(t *testing.T) {
		line := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", "APPEND",
			"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "",
			[][2]string{{"HOME", "/Volumes/scratch/home"}}, "", claudeHome{Home: "/Users/wardenowner"})
		early := strings.Index(line, "HOME=/Volumes/scratch/home")
		late := strings.Index(line, "HOME=/Users/wardenowner")
		if late < 0 || (early >= 0 && early > late) {
			t.Errorf("the stated HOME must be the LAST assignment in the export list:\n%s", line)
		}
	})
}

// assertPurgedUnder runs the launch line's env prologue under one shell and
// checks what survived into the child's environment.
func assertPurgedUnder(t *testing.T, shell, script string) {
	t.Helper()
	cmd := exec.Command(shell, "-c", script)
	cmd.Env = []string{
		// PATH IS DELIBERATELY BROKEN. The owner's env file is sourced
		// earlier on this same line and may leave PATH in any state at all
		// (the measured reason OC_TOKEN uses an absolute /bin/cat). With a
		// resolvable PATH this test passes just as happily against a purge
		// written with a bare `env`, which on a real host would be a SILENT
		// no-op and the whole defect back.
		"PATH=/nonexistent",
		"HOME=/Volumes/scratch/home",
		"CLAUDE_SOMETHING_NEW=redirect-me",
		"CLAUDE_CODE_CUSTOM_OAUTH_URL=https://example.invalid",
		"CLAUDE_CONFIG_DIR=/Volumes/scratch/cfg",
		"CLAUDE_WEIRD=a b c",
		// A value carrying its own `=`: the name is everything before the
		// FIRST one, and a shortest-suffix strip silently yields a
		// non-identifier that the purge then skips.
		"CLAUDE_HAS_EQUALS=a=b",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"ANTHROPIC_API_KEY=sk-keep-me",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the launch line's env prologue: %v\n%s", err, out)
	}
	var survivors []string
	var home, anthropic string
	for _, kv := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(kv, "CLAUDE_"):
			survivors = append(survivors, kv)
		case strings.HasPrefix(kv, "HOME="):
			home = kv
		case strings.HasPrefix(kv, "ANTHROPIC_API_KEY="):
			anthropic = kv
		}
	}
	if want := []string{"CLAUDE_CODE_USE_BEDROCK=1"}; !reflect.DeepEqual(survivors, want) {
		t.Errorf("surviving CLAUDE_* = %v, want exactly %v — anything else is a variable the child could read a config from", survivors, want)
	}
	if home != "HOME=/Users/wardenowner" {
		t.Errorf("%q, want the stated HOME", home)
	}
	if anthropic != "ANTHROPIC_API_KEY=sk-keep-me" {
		t.Errorf("%q, want ANTHROPIC_* untouched — purging it logs the child out and no measurement says it moves the config read", anthropic)
	}
}

func TestTmuxNewSession(t *testing.T) {
	r := &wardenRunner{}
	if err := tmuxNewSession(r, "officraft", "member-m1", "exec claude"); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	want := []string{
		"tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 exec claude",
		"tmux -L officraft set-option -t member-m1 window-size manual",
		"tmux -L officraft resize-window -t member-m1 -x 160 -y 50",
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls =\n%v\nwant\n%v", r.calls, want)
	}

	failing := &wardenRunner{fallback: wardenRun{err: errors.New("duplicate session: member-m1")}}
	err := tmuxNewSession(failing, "officraft", "member-m1", "exec claude")
	if err == nil || err.Error() != "duplicate session: member-m1" {
		t.Errorf("err = %v, want the new-session failure", err)
	}
	if len(failing.calls) != 1 {
		t.Errorf("a failed new-session must not pin geometry, calls = %v", failing.calls)
	}

	best := &wardenRunner{script: map[string]wardenRun{
		"tmux -L officraft set-option -t member-m1 window-size manual": {err: errors.New("unknown option")},
	}}
	if err := tmuxNewSession(best, "officraft", "member-m1", "exec claude"); err != nil {
		t.Errorf("a failed set-option must not fail the spawn, err = %v", err)
	}
	if len(best.calls) != 3 {
		t.Errorf("calls = %v, want all three", best.calls)
	}
}

func TestNudgeClock(t *testing.T) {
	var slept []time.Duration
	clock := nudgeClock(func(d time.Duration) { slept = append(slept, d) })
	clock(5 * time.Millisecond)
	if want := []time.Duration{5 * time.Millisecond}; !reflect.DeepEqual(slept, want) {
		t.Errorf("an injected clock must be used verbatim, got %v", slept)
	}

	start := time.Now()
	nudgeClock(nil)(30 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("a nil clock must PACE FOR REAL, waited %v", elapsed)
	}
}

func TestTmuxDeliverNudge(t *testing.T) {
	r := &wardenRunner{}
	var slept []time.Duration
	tmuxDeliverNudge(r, func(d time.Duration) { slept = append(slept, d) }, "officraft", "member-m1", "開始。")

	want := []string{
		"tmux -L officraft set-buffer -b oc-spawn-nudge 開始。",
		"tmux -L officraft paste-buffer -t member-m1 -b oc-spawn-nudge -d -p",
	}
	for i := 0; i < 30; i++ {
		want = append(want, "tmux -L officraft send-keys -t member-m1 Enter")
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Errorf("calls (%d) =\n%v\nwant (%d)\n%v", len(r.calls), r.calls, len(want), want)
	}
	if len(slept) != 30 {
		t.Fatalf("slept %d times, want 30", len(slept))
	}
	for i, d := range slept {
		if d != time.Second {
			t.Fatalf("settle %d = %v, want 1s", i, d)
		}
	}

	old := &wardenRunner{script: map[string]wardenRun{
		"tmux -L officraft paste-buffer -t member-m1 -b oc-spawn-nudge -d -p": {err: errors.New("unknown flag: -p")},
	}}
	tmuxDeliverNudge(old, func(time.Duration) {}, "officraft", "member-m1", "開始。")
	if old.calls[2] != "tmux -L officraft paste-buffer -t member-m1 -b oc-spawn-nudge" {
		t.Errorf("a rejected paste must retry bare-flag, call 2 = %q", old.calls[2])
	}
	if len(old.calls) != 33 {
		t.Errorf("calls = %d, want 33 (set-buffer + 2 pastes + 30 Enters)", len(old.calls))
	}
}

func TestDefaultAgentHome(t *testing.T) {
	t.Setenv("HOME", "/Users/eva")
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"main instance", map[string]string{}, "/Users/eva/.officraft/agents"},
		{"namespaced", map[string]string{"OC_NAMESPACE": "lab"}, "/Users/eva/.officraft-lab/agents"},
		{"invalid namespace degrades to the main instance", map[string]string{"OC_NAMESPACE": "LAB"}, "/Users/eva/.officraft/agents"},
		{"OC_AGENT_HOME overrides outright", map[string]string{
			"OC_AGENT_HOME": "/srv/oc/agents", "OC_NAMESPACE": "lab",
		}, "/srv/oc/agents"},
	}
	for _, c := range cases {
		if got := defaultAgentHome(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%s: defaultAgentHome = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDefaultAgentEnvFile(t *testing.T) {
	t.Setenv("HOME", "/Users/eva")
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"main instance", map[string]string{}, "/Users/eva/.officraft/env"},
		{"namespaced instances read their own file", map[string]string{"OC_NAMESPACE": "lab"}, "/Users/eva/.officraft-lab/env"},
		{"OC_AGENT_ENV_FILE overrides outright", map[string]string{
			"OC_AGENT_ENV_FILE": "/tmp/fixture-env", "OC_NAMESPACE": "lab",
		}, "/tmp/fixture-env"},
		{"OC_AGENT_HOME does not move it", map[string]string{"OC_AGENT_HOME": "/srv/oc/agents"}, "/Users/eva/.officraft/env"},
	}
	for _, c := range cases {
		if got := defaultAgentEnvFile(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%s: defaultAgentEnvFile = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDefaultCaptureEnv(t *testing.T) {
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.txt")
	shell := filepath.Join(dir, "fake-zsh")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> " + argvLog + "; done\n" +
		"printf 'FOO=bar\\0BAZ=qux\\0'\n"
	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake shell: %v", err)
	}

	capture := defaultCaptureEnv(func(k string) string {
		if k == "OC_AGENT_ENV_SHELL" {
			return shell
		}
		return ""
	})
	if capture == nil {
		t.Fatal("capture = nil, want a wired seam")
	}
	raw, err := capture()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if want := "FOO=bar\x00BAZ=qux\x00"; raw != want {
		t.Errorf("raw = %q, want %q", raw, want)
	}
	argv, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatalf("read argv log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(argv), "\n"), "\n")
	if len(lines) != 3 || lines[0] != "-i" || lines[1] != "-c" {
		t.Fatalf("the shell was run with %v, want [-i -c <dumper>]", lines)
	}
	if !strings.Contains(lines[2], "env") {
		t.Errorf("the dumper argument was %q, want an env dump", lines[2])
	}

	for _, off := range []string{"0", "false", "no", "OFF", " off "} {
		got := defaultCaptureEnv(func(k string) string {
			if k == "OC_AGENT_ENV_INHERIT" {
				return off
			}
			return shell
		})
		if got != nil {
			t.Errorf("OC_AGENT_ENV_INHERIT=%q must disable inheritance", off)
		}
	}
	if defaultCaptureEnv(func(k string) string {
		if k == "OC_AGENT_ENV_INHERIT" {
			return "1"
		}
		return shell
	}) == nil {
		t.Error("OC_AGENT_ENV_INHERIT=1 must keep inheritance on")
	}
}

func TestInteractiveEnvPairs(t *testing.T) {
	t.Run("a usable capture becomes pairs and a value-free log line", func(t *testing.T) {
		var logs []string
		d := SpawnDeps{
			CaptureEnv: func() (string, error) { return "FOO=bar\x00TOKEN=s3cret\x00", nil },
			Logf:       func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) },
		}
		got := d.interactiveEnvPairs()
		want := []agentEnvPair{{Key: "FOO", Value: "bar"}, {Key: "TOKEN", Value: "s3cret"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pairs = %+v, want %+v", got, want)
		}
		if len(logs) != 1 || logs[0] != "interactive env: inherited 2 var(s): FOO TOKEN" {
			t.Errorf("logs = %q, want the names-only line", logs)
		}
		for _, line := range logs {
			if strings.Contains(line, "s3cret") {
				t.Errorf("a value reached the log: %q", line)
			}
		}
	})

	t.Run("every degraded capture spawns on the minimal environment", func(t *testing.T) {
		cases := []struct {
			name    string
			capture func() (string, error)
			wantLog string
		}{
			{"shell failed", func() (string, error) { return "", errors.New("exit status 127") },
				"interactive env: capture failed (exit status 127); spawning with the minimal environment " +
					"— the agent will be missing whatever ~/.zshrc exports"},
			{"nothing usable printed", func() (string, error) { return "", nil },
				"interactive env: capture produced no usable variables; spawning with the minimal environment"},
			{"only unusable records", func() (string, error) { return "not-an-assignment\x00", nil },
				"interactive env: capture produced no usable variables; spawning with the minimal environment"},
		}
		for _, c := range cases {
			var logs []string
			d := SpawnDeps{CaptureEnv: c.capture, Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }}
			if got := d.interactiveEnvPairs(); got != nil {
				t.Errorf("%s: pairs = %+v, want nil", c.name, got)
			}
			if len(logs) == 0 || logs[len(logs)-1] != c.wantLog {
				t.Errorf("%s: logs = %q, want %q", c.name, logs, c.wantLog)
			}
		}
	})

	t.Run("an unwired seam is silent", func(t *testing.T) {
		var logs []string
		d := SpawnDeps{Logf: func(f string, a ...any) { logs = append(logs, f) }}
		if got := d.interactiveEnvPairs(); got != nil || len(logs) != 0 {
			t.Errorf("pairs = %+v, logs = %v, want nil and none", got, logs)
		}
	})
}

func TestOsWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".oc-token")
	if err := osWriteFile(path, "jwt-m1", 0o600); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "jwt-m1" {
		t.Fatalf("content = %q (%v), want %q", raw, err, "jwt-m1")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %04o, want 0600", fi.Mode().Perm())
	}

	if err := osWriteFile(path, "jwt-fresh", 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "jwt-fresh" {
		t.Errorf("content = %q, want the fresh token", raw)
	}

	wide := filepath.Join(dir, "wide")
	if err := os.WriteFile(wide, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := osWriteFile(wide, "new", 0o600); err != nil {
		t.Fatalf("rewrite wide: %v", err)
	}
	fi, _ = os.Stat(wide)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("an existing file's mode = %04o, want 0600", fi.Mode().Perm())
	}

	if err := osWriteFile(filepath.Join(dir, "nope", "x"), "x", 0o600); err == nil {
		t.Error("writing into a missing directory must fail")
	}
}

func TestPretrustWorkdir(t *testing.T) {
	read := func(t *testing.T, path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(raw)
	}

	t.Run("a missing claude.json is created with only this workdir trusted", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".claude.json")
		if err := pretrustWorkdir(path, "/w/m1"); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := `{
  "projects": {
    "/w/m1": {
      "hasTrustDialogAccepted": true
    }
  }
}
`
		if got := read(t, path); got != want {
			t.Errorf("claude.json =\n%s\nwant\n%s", got, want)
		}
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %04o, want 0600", fi.Mode().Perm())
		}
	})

	t.Run("existing keys and other projects survive, and re-trusting is idempotent", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".claude.json")
		seed := `{"numStartups":7,"projects":{"/w/m0":{"hasTrustDialogAccepted":true,"history":["a"]}}}`
		if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := pretrustWorkdir(path, "/w/m1"); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := `{
  "numStartups": 7,
  "projects": {
    "/w/m0": {
      "hasTrustDialogAccepted": true,
      "history": [
        "a"
      ]
    },
    "/w/m1": {
      "hasTrustDialogAccepted": true
    }
  }
}
`
		first := read(t, path)
		if first != want {
			t.Errorf("claude.json =\n%s\nwant\n%s", first, want)
		}
		if err := pretrustWorkdir(path, "/w/m1"); err != nil {
			t.Fatalf("second err = %v, want nil", err)
		}
		if second := read(t, path); second != first {
			t.Errorf("re-trusting changed the file:\n%s", second)
		}
	})

	t.Run("an unparsable file restarts from an empty config", func(t *testing.T) {
		for _, corrupt := range []string{"{not json", "[1,2]", ""} {
			path := filepath.Join(t.TempDir(), ".claude.json")
			if err := os.WriteFile(path, []byte(corrupt), 0o600); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := pretrustWorkdir(path, "/w/m1"); err != nil {
				t.Fatalf("%q: err = %v, want nil", corrupt, err)
			}
			want := "{\n  \"projects\": {\n    \"/w/m1\": {\n      \"hasTrustDialogAccepted\": true\n    }\n  }\n}\n"
			if got := read(t, path); got != want {
				t.Errorf("%q: claude.json =\n%s\nwant\n%s", corrupt, got, want)
			}
		}
	})

	t.Run("an unreadable file is surfaced, never clobbered", func(t *testing.T) {
		dir := t.TempDir()
		if err := pretrustWorkdir(dir, "/w/m1"); err == nil {
			t.Error("err = nil, want the read fault")
		}
	})
}

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := atomicWriteFile(path, []byte("{\"a\":1}"), 0o600); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"a":1}` {
		t.Errorf("content = %q, want %q", raw, `{"a":1}`)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %04o, want 0600", fi.Mode().Perm())
	}

	if err := atomicWriteFile(path, []byte("{\"a\":2}"), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"a":2}` {
		t.Errorf("content = %q, want the replacement", raw)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir holds %v, want only config.json (no temp leftovers)", names)
	}

	if err := atomicWriteFile(filepath.Join(dir, "nope", "x"), []byte("x"), 0o600); err == nil {
		t.Error("writing into a missing directory must fail")
	}
}

func TestWithPerSpawn(t *testing.T) {
	baseClock := 0
	basePretrust := 0
	basePurge := 0
	base := SpawnDeps{
		Home:      "/w",
		ClaudeBin: "/usr/local/bin/claude",
		Sleep:     func(time.Duration) { baseClock++ },
		Pretrust:  func() error { basePretrust++; return nil },
		PurgeTrash: func() {
			basePurge++
		},
	}

	perPretrust, perVerify, perPurge := 0, 0, 0
	got := base.withPerSpawn(
		func() error { perPretrust++; return errors.New("boom") },
		func(string, string) error { perVerify++; return nil },
		func() { perPurge++ })

	if err := got.Pretrust(); err == nil || err.Error() != "boom" {
		t.Errorf("Pretrust err = %v, want boom", err)
	}
	if got.VerifyPretrust == nil {
		t.Fatal("the per-spawn verifier must be bound alongside the write it verifies")
	}
	_ = got.VerifyPretrust("/w/m1", "")
	got.PurgeTrash()
	got.Sleep(time.Second)
	if perPretrust != 1 || perVerify != 1 || perPurge != 1 || baseClock != 1 {
		t.Errorf("perPretrust=%d perVerify=%d perPurge=%d baseClock=%d, want 1/1/1/1 (the base clock is carried through)",
			perPretrust, perVerify, perPurge, baseClock)
	}
	if basePretrust != 0 || basePurge != 0 {
		t.Errorf("the base seams were called: pretrust=%d purge=%d", basePretrust, basePurge)
	}
	if got.Home != "/w" || got.ClaudeBin != "/usr/local/bin/claude" {
		t.Errorf("the rest of the deps changed: %+v", got)
	}

	_ = base.Pretrust()
	if basePretrust != 1 {
		t.Error("the receiver's own seams must be left intact")
	}
}

func TestStart(t *testing.T) {
	t.Run("a claude spawn writes the workdir, publishes ocagent, launches and nudges", func(t *testing.T) {
		h := newSpawnHarness()
		got := h.deps().start(startParamsM1())

		if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
			t.Errorf("outcome = %+v, want %+v", got, want)
		}
		if want := []string{"/w/m1"}; !reflect.DeepEqual(h.mkdirs, want) {
			t.Errorf("mkdirs = %v, want %v", h.mkdirs, want)
		}
		wantWrites := []writtenFile{
			{"/w/m1/persona.md", "you are m1", 0o600},
			{"/w/m1/.mcp.json", buildMCPConfig("http://127.0.0.1:7755", "jwt-m1"), 0o600},
			{"/w/m1/settings.json", buildStatuslineSettings(), 0o600},
			{"/w/m1/.oc-token", "jwt-m1", 0o600},
		}
		if !reflect.DeepEqual(h.writes, wantWrites) {
			t.Errorf("writes =\n%+v\nwant\n%+v", h.writes, wantWrites)
		}
		if want := []string{"/w/m1/ocagent", "/w/m1/.oc-env"}; !reflect.DeepEqual(h.removes, want) {
			t.Errorf("removes = %v, want %v", h.removes, want)
		}
		wantLink := [][2]string{{"/Users/eva/.officraft/warden/ocagent", "/w/m1/ocagent"}}
		if !reflect.DeepEqual(h.symlinks, wantLink) {
			t.Errorf("symlinks = %v, want %v", h.symlinks, wantLink)
		}
		if h.pretrusts != 1 || h.purges != 1 {
			t.Errorf("pretrusts=%d purges=%d, want 1/1", h.pretrusts, h.purges)
		}
		if want := [][2]string{{"/w/m1", ""}}; !reflect.DeepEqual(h.verifies, want) {
			t.Errorf("verifies = %v, want %v", h.verifies, want)
		}
		wantCalls := []string{
			"tmux -L officraft has-session -t member-m1",
			"tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " + goldenLaunchM1,
			"tmux -L officraft set-option -t member-m1 window-size manual",
			"tmux -L officraft resize-window -t member-m1 -x 160 -y 50",
			"tmux -L officraft set-buffer -b oc-spawn-nudge 開始。",
			"tmux -L officraft paste-buffer -t member-m1 -b oc-spawn-nudge -d -p",
		}
		for i := 0; i < 30; i++ {
			wantCalls = append(wantCalls, "tmux -L officraft send-keys -t member-m1 Enter")
		}
		wantCalls = append(wantCalls, "tmux -L officraft display-message -p -t member-m1 #{pane_pid}")
		if !reflect.DeepEqual(h.runner.calls, wantCalls) {
			t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
		}
		if len(h.slept) != 30 {
			t.Errorf("slept %d times, want 30", len(h.slept))
		}
	})

	t.Run("the owner's env file is rendered into the workdir and sourced by the launch line", func(t *testing.T) {
		dir := t.TempDir()
		envFile := filepath.Join(dir, "env")
		if err := os.WriteFile(envFile, []byte("GH_TOKEN=ghp_abc\nEDITOR=vim\n"), 0o600); err != nil {
			t.Fatalf("seed env file: %v", err)
		}
		h := newSpawnHarness()
		d := h.deps()
		d.EnvFile = envFile
		d.Namespace = "lab"
		p := startParamsM1()
		p.Effort = "high"
		p.Model = "opus"

		if got := d.start(p); !got.OK {
			t.Fatalf("outcome = %+v, want OK", got)
		}
		var rendered *writtenFile
		for i := range h.writes {
			if h.writes[i].path == "/w/m1/.oc-env" {
				rendered = &h.writes[i]
			}
		}
		if rendered == nil {
			t.Fatal("no .oc-env was rendered")
		}
		want := "# rendered by ocwarden from the agent env file — do not edit\n" +
			"export GH_TOKEN=ghp_abc\nexport EDITOR=vim\n"
		if rendered.content != want || rendered.mode != 0o600 {
			t.Errorf(".oc-env = %q mode %04o, want %q mode 0600", rendered.content, rendered.mode, want)
		}
		// The pre-trust probe has to be asked under the SAME render the launch
		// line sources. Asked without it, it measures an environment no child
		// ever runs in and still answers yes.
		if want := [][2]string{{"/w/m1", "/w/m1/.oc-env"}}; !reflect.DeepEqual(h.verifies, want) {
			t.Errorf("verifies = %v, want %v", h.verifies, want)
		}
		wantLaunch := "tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " +
			`cd /w/m1; [ -f /w/m1/.oc-env ] && . /w/m1/.oc-env; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; ` +
			`export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
			`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft ` +
			`OC_AGENT_HOME=/w OC_EFFORT=high HOME=/Users/wardenowner; ` +
			`export PATH=/w/m1:"$PATH"; ` +
			`exec /usr/local/bin/claude --dangerously-skip-permissions --disallowedTools AskUserQuestion ` +
			`--mcp-config /w/m1/.mcp.json --effort high ` +
			`--append-system-prompt '你是 m1(role=builder)。你的完整身分、操作準則與開機程序都由 launcher 預抓在本地檔 ` +
			`/w/m1/persona.md。第一步:用 Read 工具把它從頭到尾整份讀完 —— 不要帶 offset/limit,不要只讀開頭,` +
			`也不准用 cat/head/tail/sed 或任何終端機指令讀它:這個檔有數萬字元,終端機輸出只有開頭一小段會進到你的 context,` +
			`其餘會被靜默丟棄而且不會有任何錯誤訊息,而「開機程序」在整份檔案的最後面。` +
			`整份讀完後,照裡面「開機程序」段逐步執行。' --model opus --settings /w/m1/settings.json`
		if h.runner.calls[1] != wantLaunch {
			t.Errorf("launch call =\n%s\nwant\n%s", h.runner.calls[1], wantLaunch)
		}
		for _, line := range h.logs {
			if strings.Contains(line, "ghp_abc") {
				t.Errorf("a credential value reached the log: %q", line)
			}
		}
	})

	t.Run("an agent env that moves the config home is overwritten, not obeyed", func(t *testing.T) {
		dir := t.TempDir()
		envFile := filepath.Join(dir, "env")
		// Both variables that decide which claude.json the child reads, set by the
		// owner's own file — the exact pair that walked through the previous shape.
		if err := os.WriteFile(envFile, []byte("HOME=/Volumes/scratch/home\nCLAUDE_CONFIG_DIR=/Volumes/scratch/cfg\n"), 0o600); err != nil {
			t.Fatalf("seed env file: %v", err)
		}
		h := newSpawnHarness()
		d := h.deps()
		d.EnvFile = envFile

		if got := d.start(startParamsM1()); !got.OK {
			t.Fatalf("outcome = %+v, want OK — the launch line states the config home, so this spawn is safe", got)
		}
		var line string
		for _, call := range h.runner.calls {
			if strings.Contains(call, "new-session") {
				line = call
			}
		}
		if line == "" {
			t.Fatal("no session was started")
		}
		source := strings.Index(line, ". /w/m1/.oc-env")
		unset := strings.Index(line, "unset CLAUDE_CONFIG_DIR")
		pin := strings.Index(line, "HOME=/Users/wardenowner")
		if source < 0 || unset < 0 || pin < 0 {
			t.Fatalf("launch line is missing the source or the pins:\n%s", line)
		}
		// ORDER, not mere presence: a pin emitted BEFORE the source is erased by
		// the very file it exists to overrule, and the line still contains both.
		if !(source < unset && source < pin) {
			t.Errorf("the config-home pins must come AFTER the agent env is sourced (source=%d unset=%d pin=%d):\n%s",
				source, unset, pin, line)
		}
		if strings.Contains(line, "HOME=/Volumes/scratch/home") || strings.Contains(line, "CLAUDE_CONFIG_DIR=/Volumes/scratch/cfg") {
			t.Errorf("the owner's values must not be exported by the launch line itself:\n%s", line)
		}
	})

	t.Run("a claude spawn with no stated config home is refused", func(t *testing.T) {
		h := newSpawnHarness()
		d := h.deps()
		d.ClaudeHome = claudeHome{}

		got := d.start(startParamsM1())
		if got.OK || !strings.Contains(got.Reason, "claude_home_unresolved") {
			t.Fatalf("outcome = %+v, want a claude_home_unresolved refusal — an unstated config home leaves the child inheriting one", got)
		}
		if h.pretrusts != 0 {
			t.Errorf("pretrusts = %d, want 0 — nothing may be written into a file we cannot point the child at", h.pretrusts)
		}
		for _, call := range h.runner.calls {
			if strings.Contains(call, "new-session") {
				t.Errorf("a session was started anyway: %q", call)
			}
		}
	})

	t.Run("a codex spawn with no stated config home still launches", func(t *testing.T) {
		// SISTER OF THE REFUSAL ABOVE, and the reason it exists: with only that
		// one, deleting `runtimeName == "claude" &&` from the config-home gate
		// leaves every test green while every CODEX member on a host with no
		// stated HOME becomes unstartable — refused over a claude.json codex
		// never reads. A guard that cannot tell the two runtimes apart is not a
		// guard, it is an outage waiting for a host with an empty HOME.
		h := newSpawnHarness()
		d := h.deps()
		d.ClaudeHome = claudeHome{}
		d.CodexBin = "/usr/local/bin/codex"
		d.WardenBin = "/Users/eva/.officraft/warden/ocwarden"
		p := startParamsM1()
		p.Runtime = "codex"

		got := d.start(p)
		if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
			t.Fatalf("outcome = %+v, want %+v — codex does not read claude.json, so an unstated claude config home is none of its business", got, want)
		}
		launched := false
		for _, call := range h.runner.calls {
			if strings.Contains(call, "new-session") {
				launched = true
			}
		}
		if !launched {
			t.Errorf("no session was started: %v", h.runner.calls)
		}
	})

	t.Run("a live session is never clobbered", func(t *testing.T) {
		h := newSpawnHarness()
		h.runner.script["tmux -L officraft has-session -t member-m1"] = wardenRun{}
		got := h.deps().start(startParamsM1())
		want := SpawnOutcome{Reason: `session_already_exists: tmux session "member-m1" is already live (clobber-guard refused to stomp it)`}
		if got != want {
			t.Errorf("outcome = %+v, want %+v", got, want)
		}
		if len(h.mkdirs)+len(h.writes)+len(h.symlinks) != 0 {
			t.Errorf("a refused spawn touched the workdir: %+v", h)
		}
		if len(h.runner.calls) != 1 {
			t.Errorf("calls = %v, want the probe alone", h.runner.calls)
		}
	})

	t.Run("a broken session probe does not block the spawn", func(t *testing.T) {
		h := newSpawnHarness()
		h.runner.script["tmux -L officraft has-session -t member-m1"] = wardenRun{
			err: errors.New("exec: \"tmux\": executable file not found in $PATH"),
		}
		if got := h.deps().start(startParamsM1()); !got.OK {
			t.Errorf("outcome = %+v, want OK", got)
		}
	})

	t.Run("every refusal names its cause and launches nothing", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*spawnHarness, *SpawnDeps, *StartParams)
			want   string
		}{
			{"an unsupported runtime", func(_ *spawnHarness, _ *SpawnDeps, p *StartParams) {
				p.Runtime = "python"
			}, "runtime_unsupported: expected claude or codex"},
			{"no claude on the machine", func(_ *spawnHarness, d *SpawnDeps, _ *StartParams) {
				d.ClaudeBin = ""
			}, "claude_bin_unresolved: no Claude Code on this machine. " +
				"Fix any one: set this member's 執行環境 to Codex; " +
				"install Claude Code here; or re-install the warden with OC_CLAUDE_BIN=<path>."},
			{"a signed-out claude", func(_ *spawnHarness, d *SpawnDeps, _ *StartParams) {
				d.ClaudeCreds = func() claudeCredStatus {
					return claudeCredStatus{Summary: "cred_file=unset keychain=unset"}
				}
			}, "claude_not_logged_in: no claude credential here (cred_file=unset keychain=unset). " +
				"Fix any one: set this member's 執行環境 to Codex; run `claude` once as this user; " +
				"or re-install the warden with OC_CLAUDE_CRED_CHECK=0 (shell exports do not reach it)."},
			{"no codex on the machine", func(_ *spawnHarness, d *SpawnDeps, p *StartParams) {
				p.Runtime = "codex"
				d.CodexBin = ""
			}, "codex_bin_unresolved: set OC_CODEX_BIN or put codex on the daemon PATH"},
			{"no warden binary for the codex sidecar", func(_ *spawnHarness, d *SpawnDeps, p *StartParams) {
				p.Runtime = "codex"
				d.CodexBin = "/usr/local/bin/codex"
			}, "warden_bin_unresolved: cannot launch codex-session sidecar"},
			{"a signed-out codex", func(h *spawnHarness, d *SpawnDeps, p *StartParams) {
				p.Runtime = "codex"
				d.CodexBin = "/usr/local/bin/codex"
				d.WardenBin = "/Users/eva/.officraft/warden/ocwarden"
				h.runner.script["/usr/local/bin/codex login status"] = wardenRun{err: errors.New("exit status 1")}
			}, "codex_not_logged_in: `codex login status` failed on this host"},
			{"a workdir that cannot be made", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.mkdirErr = errors.New("permission denied")
			}, "mkdir_failed: workdir /w/m1: permission denied"},
			{"a persona that cannot be written", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.writeErr["/w/m1/persona.md"] = errors.New("no space left on device")
			}, "write_file_failed: persona.md: no space left on device"},
			{"an .mcp.json that cannot be written", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.writeErr["/w/m1/.mcp.json"] = errors.New("no space left on device")
			}, "write_file_failed: .mcp.json: no space left on device"},
			{"a settings.json that cannot be written", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.writeErr["/w/m1/settings.json"] = errors.New("no space left on device")
			}, "write_file_failed: settings.json: no space left on device"},
			{"a token file that cannot be written", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.writeErr["/w/m1/.oc-token"] = errors.New("no space left on device")
			}, "write_file_failed: .oc-token: no space left on device"},
			{"a stale ocagent link that cannot be cleared", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.removeErr["/w/m1/ocagent"] = errors.New("permission denied")
			}, "symlink_failed: clearing stale ocagent link: permission denied"},
			{"an ocagent binary that is not there", func(_ *spawnHarness, d *SpawnDeps, _ *StartParams) {
				d.ResolveOcAgentBin = func() (string, bool) { return "/Users/eva/.officraft/warden/ocagent", false }
			}, "ocagent_not_found: no ocagent binary at /Users/eva/.officraft/warden/ocagent. " +
				"The agent would start but could never connect. If this machine was just installed, " +
				"the download may still be running — the next spawn picks it up with no warden restart. " +
				"Otherwise re-install the warden on this machine."},
			{"a warden built with no ocagent resolver", func(_ *spawnHarness, d *SpawnDeps, _ *StartParams) {
				d.ResolveOcAgentBin = nil
			}, "ocagent_not_found: no ocagent binary at <no path: this warden was built without an ocagent resolver>. " +
				"The agent would start but could never connect. If this machine was just installed, " +
				"the download may still be running — the next spawn picks it up with no warden restart. " +
				"Otherwise re-install the warden on this machine."},
			{"an ocagent link that cannot be published", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.symlinkEr = errors.New("read-only file system")
			}, "symlink_failed: publishing workdir ocagent link: read-only file system"},
			{"a pretrust that failed", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.pretrustE = errors.New("permission denied")
			}, "pretrust_failed: marking workdir trusted in claude.json: permission denied"},
			// Writing the flag is not the guarantee; the child reading THAT file
			// is. A spawn that wrote one nobody reads is the exact shape four
			// reviews kept finding, and it must cost the spawn, not a log line.
			{"a trust flag the child would not read", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.verifyE = errors.New("claude reads a different file")
			}, "pretrust_unverified: claude reads a different file"},
			{"a warden built with no way to verify the flag", func(_ *spawnHarness, d *SpawnDeps, _ *StartParams) {
				d.VerifyPretrust = nil
			}, "pretrust_unverified: a trust flag was written but no verifier is wired, so nothing establishes that the spawned claude reads the file it was written to"},
			{"a tmux that refused the session", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.runner.script["tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 "+goldenLaunchM1] =
					wardenRun{err: errors.New("no server running")}
			}, "spawn_exec_failed: tmux new-session: no server running"},
		}
		for _, c := range cases {
			h := newSpawnHarness()
			d := h.deps()
			p := startParamsM1()
			c.mutate(h, &d, &p)
			got := d.start(p)
			if got.OK || got.SessionID != "" || got.PID != "" {
				t.Errorf("%s: outcome = %+v, want a refusal", c.name, got)
			}
			if got.Reason != c.want {
				t.Errorf("%s: reason =\n%q\nwant\n%q", c.name, got.Reason, c.want)
			}
			for _, call := range h.runner.calls {
				if strings.Contains(call, "send-keys") {
					t.Errorf("%s: a refused spawn nudged a session: %v", c.name, h.runner.calls)
				}
			}
		}
	})

	t.Run("a codex spawn runs the sidecar and never sends terminal keystrokes", func(t *testing.T) {
		h := newSpawnHarness()
		d := h.deps()
		d.CodexBin = "/usr/local/bin/codex"
		d.WardenBin = "/Users/eva/.officraft/warden/ocwarden"
		p := startParamsM1()
		p.Runtime = "codex"
		p.Model = "gpt-5"
		p.Effort = "high"

		got := d.start(p)
		if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
			t.Errorf("outcome = %+v, want %+v", got, want)
		}
		wantLaunch := "tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " +
			`cd /w/m1; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
			`OC_BASE=http://127.0.0.1:7755 OC_ID=m1 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft ` +
			`OC_EFFORT=high; export PATH=/w/m1:"$PATH"; ` +
			`exec /Users/eva/.officraft/warden/ocwarden codex-session ` +
			`--codex-bin /usr/local/bin/codex --workdir /w/m1 --persona /w/m1/persona.md ` +
			`--agent-id m1 --model gpt-5 --effort high`
		wantCalls := []string{
			"/usr/local/bin/codex login status",
			"tmux -L officraft has-session -t member-m1",
			wantLaunch,
			"tmux -L officraft set-option -t member-m1 window-size manual",
			"tmux -L officraft resize-window -t member-m1 -x 160 -y 50",
			"tmux -L officraft display-message -p -t member-m1 #{pane_pid}",
		}
		if !reflect.DeepEqual(h.runner.calls, wantCalls) {
			t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
		}
		if h.pretrusts != 0 {
			t.Errorf("pretrusts = %d, want 0 (claude.json is not codex's gate)", h.pretrusts)
		}
		if len(h.slept) != 0 {
			t.Errorf("slept %v, want none", h.slept)
		}
	})

	t.Run("a codex spawn at an effort this warden does not know launches at medium and logs it", func(t *testing.T) {
		h := newSpawnHarness()
		d := h.deps()
		d.CodexBin = "/usr/local/bin/codex"
		d.WardenBin = "/Users/eva/.officraft/warden/ocwarden"
		p := startParamsM1()
		p.Runtime = "codex"
		p.Model = "gpt-5"
		p.Effort = "xxhigh"

		if got := d.start(p); !got.OK {
			t.Fatalf("outcome = %+v, want OK", got)
		}
		wantLaunch := "tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " +
			`cd /w/m1; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
			`OC_BASE=http://127.0.0.1:7755 OC_ID=m1 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft ` +
			`OC_EFFORT=xxhigh; export PATH=/w/m1:"$PATH"; ` +
			`exec /Users/eva/.officraft/warden/ocwarden codex-session ` +
			`--codex-bin /usr/local/bin/codex --workdir /w/m1 --persona /w/m1/persona.md ` +
			`--agent-id m1 --model gpt-5 --effort medium`
		if h.runner.calls[2] != wantLaunch {
			t.Errorf("launch call =\n%s\nwant\n%s", h.runner.calls[2], wantLaunch)
		}
		wantLog := `codex launch: effort "xxhigh" is not a level this warden knows; ` +
			`launching at "medium". The cockpit will keep showing "xxhigh", so this line is the ` +
			`only place the difference is visible — upgrade the warden if the server has grown a level.`
		if !reflect.DeepEqual(h.logs, []string{wantLog}) {
			t.Errorf("warden logs =\n%q\nwant\n%q", h.logs, []string{wantLog})
		}
	})

	t.Run("an explicit session name and role default are honoured", func(t *testing.T) {
		h := newSpawnHarness()
		h.runner.script["tmux -L officraft has-session -t custom-session"] =
			wardenRun{err: errors.New("can't find session: custom-session")}
		h.runner.script["tmux -L officraft display-message -p -t custom-session #{pane_pid}"] = wardenRun{out: "700"}
		p := startParamsM1()
		p.SessionName = "custom-session"
		p.Role = ""

		got := h.deps().start(p)
		if want := (SpawnOutcome{OK: true, SessionID: "custom-session", PID: "700"}); got != want {
			t.Errorf("outcome = %+v, want %+v", got, want)
		}
		if !strings.Contains(h.runner.calls[1], "OC_SESSION=custom-session") ||
			!strings.Contains(h.runner.calls[1], "你是 m1(role=agent)") {
			t.Errorf("launch call =\n%s\nwant the custom session and the default role", h.runner.calls[1])
		}
	})

	t.Run("a pane pid that cannot be read still reports the executed spawn", func(t *testing.T) {
		h := newSpawnHarness()
		h.runner.script["tmux -L officraft display-message -p -t member-m1 #{pane_pid}"] =
			wardenRun{err: errors.New("can't find session")}
		if got := (h.deps().start(startParamsM1())); got != (SpawnOutcome{OK: true, SessionID: "member-m1"}) {
			t.Errorf("outcome = %+v, want OK with an empty pid", got)
		}
	})
}

func TestClaudeChildEnvPrologue(t *testing.T) {
	t.Run("the family purge runs on every path, redirected or not", func(t *testing.T) {
		// Mu-B, from the fourth review: wrapping the purge in "only when
		// OC_CLAUDE_JSON did not redirect us" left the whole suite green. The
		// redirect moves WHERE the trust file is; it says nothing about whether
		// the owner's shell is carrying a variable that moves it again, so the
		// two have to stay independent.
		purge := claudeEnvPurgeFragment()
		for _, ch := range []claudeHome{
			{Home: "/Users/owner"},
			{Home: "/Users/owner", ConfigDir: "/tmp/box"},
		} {
			got := claudeChildEnvPrologue("/w/m1", "/w/m1/.oc-env", ch)
			if !strings.Contains(got, purge) {
				t.Errorf("ConfigDir=%q prologue does not purge the CLAUDE_* family:\n%s", ch.ConfigDir, got)
			}
			if src := strings.Index(got, ". /w/m1/.oc-env"); src < 0 || src > strings.Index(got, purge) {
				t.Errorf("ConfigDir=%q purges before sourcing the owner's env, which clears nothing:\n%s", ch.ConfigDir, got)
			}
		}
	})

	t.Run("only the default layout unsets CLAUDE_CONFIG_DIR", func(t *testing.T) {
		if got := claudeChildEnvPrologue("/w/m1", "", claudeHome{Home: "/Users/owner"}); !strings.Contains(got, "unset CLAUDE_CONFIG_DIR") {
			t.Errorf("the default layout must unset it even if the purge no-ops:\n%s", got)
		}
		if got := claudeChildEnvPrologue("/w/m1", "", claudeHome{Home: "/Users/owner", ConfigDir: "/tmp/box"}); strings.Contains(got, "unset CLAUDE_CONFIG_DIR") {
			t.Errorf("a redirected layout exports it; unsetting it too is contradictory:\n%s", got)
		}
	})
}
