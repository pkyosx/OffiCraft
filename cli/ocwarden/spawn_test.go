package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
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
	purges    int
	// promptProbes records every "does this claude take a prompt file" question.
	promptProbes []string
	// present answers the Exists seam; every path asked is recorded.
	present map[string]bool
	asked   []string
	// modTimes answers the ModTime seam; a path not in it is absent.
	modTimes map[string]time.Time
	// presentAt makes a path present from that much time after the launch on.
	presentAt map[string]time.Duration
	// The harness clock runs on what the spawn slept plus captureCost per pane
	// capture; paneAt, when set, answers every capture by that clock.
	captureCost  time.Duration
	paneAt       func(elapsed time.Duration) string
	paneTimeouts []time.Duration
	// reaped answers the leftover-listener reap, which is recorded in the runner's
	// call list so its order against the launch shows.
	reaped    int
	reapStuck bool
	// stopStuck makes the teardown before the notify-mod restart fail; the
	// teardown is recorded in the runner's call list as "stop-attempt …".
	stopStuck bool
	// onStop runs inside that teardown (a test moving markers with the restart).
	onStop func()
	// claudeJSON answers the ReadFile seam for the warden owner's .claude.json;
	// nil is a missing file. claudeJSONReads counts the reads.
	claudeJSON      []byte
	claudeJSONErr   error
	claudeJSONReads []string
}

func (h *spawnHarness) elapsed() time.Duration {
	var total time.Duration
	for _, d := range h.slept {
		total += d
	}
	for _, c := range h.runner.calls {
		if strings.Contains(c, " capture-pane ") {
			total += h.captureCost
		}
	}
	return total
}

// paneRunner answers pane captures from the harness clock; every call still
// lands in the wardenRunner's call list.
type paneRunner struct{ h *spawnHarness }

// Its withTimeout records each budget a call runs under in h.paneTimeouts.
func (r paneRunner) withTimeout(timeout time.Duration) CmdRunner {
	return timedPaneRunner{r, timeout}
}

type timedPaneRunner struct {
	paneRunner
	timeout time.Duration
}

func (t timedPaneRunner) withTimeout(timeout time.Duration) CmdRunner {
	t.timeout = timeout
	return t
}

func (t timedPaneRunner) Run(name string, args ...string) (string, error) {
	t.h.paneTimeouts = append(t.h.paneTimeouts, t.timeout)
	return t.paneRunner.Run(name, args...)
}

func (r paneRunner) Run(name string, args ...string) (string, error) {
	out, err := r.h.runner.Run(name, args...)
	if slices.Contains(args, "capture-pane") {
		// Read after the call is recorded: a capture sees the time it took.
		return r.h.paneAt(r.h.elapsed()), nil
	}
	return out, err
}

func (h *spawnHarness) deps() SpawnDeps {
	var runner CmdRunner = h.runner
	if h.paneAt != nil {
		runner = paneRunner{h}
	}
	return SpawnDeps{
		Runner: runner,
		Base:   "http://127.0.0.1:7755/",
		Socket: "officraft",
		Home:   "/w",
		// The warden's own HOME, deliberately NOT the agents root above: the two
		// are different things and a fixture that spelled them the same could not
		// tell a launch line that exported the wrong one.
		ClaudeHome: claudeHome{Home: "/Users/wardenowner"},
		ClaudeBin:  "/usr/local/bin/claude",
		ClaudeTakesPromptFile: func(promptFile string) (bool, string) {
			h.promptProbes = append(h.promptProbes, promptFile)
			return true, ""
		},
		RepoRoot: "/repo",
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
		Exists: func(path string) bool {
			h.asked = append(h.asked, path)
			if at, ok := h.presentAt[path]; ok && h.elapsed() >= at {
				return true
			}
			return h.present[path]
		},
		ModTime: func(path string) (time.Time, error) {
			if mt, ok := h.modTimes[path]; ok {
				return mt, nil
			}
			return time.Time{}, os.ErrNotExist
		},
		Now: func() time.Time { return spawnLaunchedAt.Add(h.elapsed()) },
		ReapWorkdirListeners: func(workdir string) (int, bool) {
			h.runner.calls = append(h.runner.calls, "reap "+workdir)
			return h.reaped, !h.reapStuck
		},
		StopAttempt: func(socket, session, workdir string) bool {
			h.runner.calls = append(h.runner.calls, "stop-attempt "+socket+" "+session+" "+workdir)
			if h.onStop != nil {
				h.onStop()
			}
			return !h.stopStuck
		},
		ReadFile: func(path string) ([]byte, error) {
			h.claudeJSONReads = append(h.claudeJSONReads, path)
			if h.claudeJSONErr != nil {
				return nil, h.claudeJSONErr
			}
			if h.claudeJSON == nil {
				return nil, os.ErrNotExist
			}
			return h.claudeJSON, nil
		},
		Logf:       func(format string, a ...any) { h.logs = append(h.logs, fmt.Sprintf(format, a...)) },
		Pretrust:   func() error { h.pretrusts++; return h.pretrustE },
		PurgeTrash: func() { h.purges++ },
		Sleep:      func(d time.Duration) { h.slept = append(h.slept, d) },
	}
}

// A wait in which the mod never even starts: each of its 15 polls (every 2 s of
// the 30 s) asks for both markers and captures the pane.
var notifyModPollCaptures = slices.Repeat([]string{"tmux -L officraft capture-pane -p -t member-m1"}, 15)
var notifyModPollAsks = slices.Repeat([]string{"/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-started"}, 15)

// The harness's clock: every spawn launches at this instant.
var spawnLaunchedAt = time.Date(2026, 10, 2, 3, 4, 0, 0, time.UTC)

func newSpawnHarness() *spawnHarness {
	return &spawnHarness{
		runner: &wardenRunner{script: map[string]wardenRun{
			"tmux -L officraft has-session -t member-m1":                    {err: errors.New("can't find session: member-m1")},
			"tmux -L officraft display-message -p -t member-m1 #{pane_pid}": {out: "500\n"},
		}},
		writeErr:  map[string]error{},
		removeErr: map[string]error{},
		// The mod loaded: the route every claude spawn takes unless a case says otherwise.
		present: map[string]bool{"/w/m1/.officraft-mod-loaded": true},
	}
}

func startParamsM1() StartParams {
	return StartParams{MemberID: "m1", PersonaContext: "you are m1", MemberToken: "jwt-m1", Role: "builder"}
}

// goldenClaudePurge is the CLAUDE_* family purge as it must appear on every
// claude launch line — TYPED OUT HERE rather than called from
// claudeEnvPurgeFragment, so a change to the fragment has to be re-justified
// against a literal instead of agreeing with itself.
const goldenClaudePurge = `for __oc_e in $(/usr/bin/env); do case $__oc_e in CLAUDE_CODE_USE_BEDROCK=*|CLAUDE_CODE_USE_VERTEX=*|CLAUDE_CODE_OAUTH_TOKEN=*) continue;; CLAUDE_*=*) ;; *) continue;; esac; __oc_n=${__oc_e%%=*}; case $__oc_n in *[!A-Za-z0-9_]*) continue;; esac; unset "$__oc_n"; done; unset __oc_e __oc_n; `

const goldenInlineSettings = `{"skipDangerousModePermissionPrompt":true,"tui":"fullscreen","statusLine":{"type":"command","command":"ocagent context-report"},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"ocagent guard-bash"}]}],"PermissionRequest":[{"hooks":[{"type":"command","command":"ocagent guard-permission"}]}],"Stop":[{"hooks":[{"type":"command","command":"ocagent model-call-report"}]}],"StopFailure":[{"hooks":[{"type":"command","command":"ocagent model-call-report"}]}]}}`

var goldenLaunchM1 = `cd /w/m1; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
	`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft ` +
	`HOME=/Users/wardenowner; ` +
	`export PATH=/w/m1:"$PATH"; ` +
	`exec /usr/local/bin/claude --dangerously-skip-permissions ` +
	`--disallowedTools AskUserQuestion --mcp-config /w/m1/.mcp.json --effort medium ` +
	`--append-system-prompt-file /w/m1/system-prompt.md --settings ` + shellQuote(goldenInlineSettings) +
	` --plugin-dir /w/m1/.officraft-mod`

// notifyModFile is one file of the notification mod as it ships (mod/ beside
// this package): the warden must lay it down byte for byte.
func notifyModFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("mod", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func notifyModWrites(t *testing.T) []writtenFile {
	return []writtenFile{
		{"/w/m1/.officraft-mod/.claude-plugin/plugin.json", notifyModFile(t, ".claude-plugin/plugin.json"), 0o600},
		{"/w/m1/.officraft-mod/hooks/hooks.json", notifyModFile(t, "hooks/hooks.json"), 0o600},
		{"/w/m1/.officraft-mod/hooks/register.ts", notifyModFile(t, "hooks/register.ts"), 0o600},
		// Every name the mod uses comes from here; it spells none of them itself.
		{"/w/m1/.officraft-mod/officraft.json",
			`{"boot_prompt":"開始。","started_marker":"/w/m1/.officraft-mod-started","loaded_marker":"/w/m1/.officraft-mod-loaded",` +
				`"disabled_marker":"/w/m1/.officraft-mod-disabled","booted_marker":"/w/m1/.officraft-mod-booted",` +
				`"ready_prefixes":["[ocagent] listen: connected","[ocagent] listen: disconnected"],` +
				`"listener":{"argv":["/w/m1/ocagent","listen","--deliver-socket"],"cwd":"/w/m1"}}` + "\n",
			0o600},
	}
}

const goldenNotifyClaudeTooOldReason = "notify_claude_too_old: 這台機器的 Claude Code 是 2.1.286，低於 2.1.287，" +
	"Claude 成員無法上線。請到監控頁的機器分頁升級這台機器的 Claude Code。"

const goldenNotifyModNotLoadedReason = "notify_mod_not_loaded: 通知模組沒有載入，成員收不到 OffiCraft 訊息，已停止上線。" +
	"常見原因：工作目錄未信任、disableAllHooks、--safe-mode、受管設定擋掉 --plugin-dir。"

// After the one restart, with no ~/.claude.json (the harness default) before either launch.
const goldenNotifyModRetriedReason = "notify_mod_not_loaded: 通知模組沒有載入，成員收不到 OffiCraft 訊息，已停止上線。" +
	"已自動重啟 Claude Code 一次仍沒載入；啟動前快取的開關：第 1 次 absent、第 2 次 absent。" +
	"常見原因：工作目錄未信任、disableAllHooks、--safe-mode、受管設定擋掉 --plugin-dir。"

// The read-only log of Claude Code's cached hook-modules flag, for a warden
// owner with no ~/.claude.json (the harness default).
const (
	flagAbsentAttempt1 = "m1: notify-mod: attempt 1/2: /Users/wardenowner/.claude.json tengu_plugin_hooks_modules=absent (no such file)"
	flagAbsentAttempt2 = "m1: notify-mod: attempt 2/2: /Users/wardenowner/.claude.json tengu_plugin_hooks_modules=absent (no such file)"
	flagAbsentAtGiveUp = "m1: notify-mod: at give-up: /Users/wardenowner/.claude.json tengu_plugin_hooks_modules=absent (no such file)"
)

func countCalls(calls []string, part string) int {
	n := 0
	for _, c := range calls {
		if strings.Contains(c, part) {
			n++
		}
	}
	return n
}

type funcRunner func(name string, args ...string) (string, error)

func (f funcRunner) Run(name string, args ...string) (string, error) { return f(name, args...) }

// goldenSystemPromptM1 is what the member reads as its appended system prompt:
// the header, then the persona verbatim.
const goldenSystemPromptM1 = "你是 m1(role=builder)。以下「---」之後是你的 OffiCraft 開機檔全文" +
	"(與 /w/m1/persona.md 內容相同),它已經在你的 system prompt 裡:不要再用任何工具讀那個檔。" +
	"開機時照開機檔最後的「啟動步驟」逐步執行。\n---\nyou are m1"

const goldenFallbackPromptM1 = "你是 m1(role=builder)。你的完整身分、操作準則與啟動步驟都由 " +
	"launcher 預抓在本地檔 /w/m1/persona.md。第一步:用 Read 工具從第一行讀到最後一行;" +
	"一次 Read 讀不完時,用 offset/limit 從上一次停下的那一行接著讀,直到讀到最後一行。" +
	"不要用 cat/head/tail/sed 或任何終端機指令讀它:終端機輸出只有開頭一小段會進到你的 context," +
	"其餘會被靜默丟棄而且不會有任何錯誤訊息,而「啟動步驟」在整份檔案的最後面。" +
	"整份讀完後,照裡面「啟動步驟」段逐步執行。"

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
  "skipDangerousModePermissionPrompt": true,
  "tui": "fullscreen",
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
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "ocagent model-call-report"
          }
        ]
      }
    ],
    "StopFailure": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "ocagent model-call-report"
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
		t.Fatalf("the settings passed to every member are not valid JSON: %v", err)
	}
	compact, err := compactSettingsJSON(buildStatuslineSettings())
	if err != nil {
		t.Fatalf("compact settings: %v", err)
	}
	if want := goldenInlineSettings; compact != want {
		t.Errorf("compact settings = %q, want %q", compact, want)
	}
}

func TestBuildClaudeSystemPrompt(t *testing.T) {
	if got := buildClaudeSystemPrompt("m1", "builder", "/w/m1/persona.md", "you are m1"); got != goldenSystemPromptM1 {
		t.Errorf("prompt =\n%q\nwant\n%q", got, goldenSystemPromptM1)
	}
}

func TestBuildAppendSystemPrompt(t *testing.T) {
	if got := buildAppendSystemPrompt("m1", "builder", "/w/m1/persona.md"); got != goldenFallbackPromptM1 {
		t.Errorf("prompt =\n%q\nwant\n%q", got, goldenFallbackPromptM1)
	}
}

func TestWithRunTimeout(t *testing.T) {
	if got := withRunTimeout(execRunner{timeout: 5 * time.Second}, 2*time.Second); got != CmdRunner(execRunner{timeout: 2 * time.Second}) {
		t.Errorf("the real runner = %#v, want its timeout cut to 2s", got)
	}
	fake := &wardenRunner{}
	if got := withRunTimeout(fake, 2*time.Second); got != CmdRunner(fake) {
		t.Errorf("a runner without a timeout = %#v, want it returned as is", got)
	}
}

// timeoutRecordingRunner is a wardenRunner whose withTimeout hands back a
// DIFFERENT runner; only calls made through that one land in timeouts, so a
// caller that asks for a timeout and then runs on the original records nothing.
type timeoutRecordingRunner struct {
	*wardenRunner
	timeouts []time.Duration
}

func (r *timeoutRecordingRunner) withTimeout(timeout time.Duration) CmdRunner {
	return timedRecordingRunner{owner: r, timeout: timeout}
}

type timedRecordingRunner struct {
	owner   *timeoutRecordingRunner
	timeout time.Duration
}

func (t timedRecordingRunner) withTimeout(timeout time.Duration) CmdRunner {
	t.timeout = timeout
	return t
}

func (t timedRecordingRunner) Run(name string, args ...string) (string, error) {
	t.owner.timeouts = append(t.owner.timeouts, t.timeout)
	return t.owner.wardenRunner.Run(name, args...)
}

func TestClaudeAcceptsPromptFile(t *testing.T) {
	const probe = "/c/claude --append-system-prompt-file /w/m1/system-prompt.md --oc-probe-unsupported-flag"
	cases := []struct {
		name    string
		run     wardenRun
		want    bool
		wantWhy string
	}{
		{"the parser got past the file flag and rejected the sentinel",
			wardenRun{err: errors.New("exit status 1: error: unknown option '--oc-probe-unsupported-flag'")}, true, ""},
		{"an old claude rejects the file flag itself",
			wardenRun{err: errors.New("exit status 1: error: unknown option '--append-system-prompt-file'")}, false,
			"claude rejected --append-system-prompt-file"},
		{"a claude that cannot run at all",
			wardenRun{err: errors.New("fork/exec /c/claude: no such file or directory")}, false, "the probe failed"},
		{"a claude that accepted an unknown flag and exited 0", wardenRun{}, false,
			"the probe exited 0 on an unknown flag"},
		{"a claude that hung until the probe budget ran out",
			wardenRun{err: errors.New("timeout after 2s")}, false, "the probe timed out"},
		{"a claude that answers in another language without naming the sentinel",
			wardenRun{err: errors.New("exit status 1: 錯誤：無法辨識的選項")}, false, "the probe failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &wardenRunner{script: map[string]wardenRun{probe: tc.run}}

			got, why := claudeAcceptsPromptFile(r, "/c/claude", "/w/m1/system-prompt.md")
			if got != tc.want || why != tc.wantWhy {
				t.Errorf("claudeAcceptsPromptFile = (%v, %q), want (%v, %q)", got, why, tc.want, tc.wantWhy)
			}
			if want := []string{probe}; !reflect.DeepEqual(r.calls, want) {
				t.Errorf("calls = %v, want %v", r.calls, want)
			}
		})
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
	got := buildLaunchCommand("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
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
		`--model opus --settings '{"hooks":{}}'`
	got = buildLaunchCommand("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
		"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft",
		"opus", "high", `{"hooks":{}}`, claudeHome{Home: "/Users/wardenowner"})
	if got != wantFull {
		t.Errorf("launch line =\n%s\nwant\n%s", got, wantFull)
	}
}

func TestBuildLaunchCommandWithEnv(t *testing.T) {
	home := claudeHome{Home: "/Users/wardenowner"}
	want := `cd /w/m1; [ -f /w/m1/.oc-env ] && . /w/m1/.oc-env; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; ` +
		`export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
		`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft-lab ` +
		`OC_AGENT_HOME=/w HOME=/Users/wardenowner; ` +
		`export PATH=/w/m1:"$PATH"; ` +
		`exec /usr/local/bin/claude --dangerously-skip-permissions --disallowedTools AskUserQuestion ` +
		`--mcp-config /w/m1/.mcp.json --effort medium --append-system-prompt APPEND ` +
		`--settings '{"hooks":{}}'`
	got := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
		"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft-lab", "", "medium",
		`{"hooks":{}}`, [][2]string{{"OC_AGENT_HOME", "/w"}}, "/w/m1/.oc-env", home, "")
	if got != want {
		t.Errorf("launch line =\n%s\nwant\n%s", got, want)
	}

	plain := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
		"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "", home, "")
	if plain != buildLaunchCommand("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
		"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", home) {
		t.Errorf("no extra env must be byte-identical to the plain line, got\n%s", plain)
	}

	spaced := buildLaunchCommandWithEnv("/opt/my claude/claude", "/w/a b", "/w/a b/.mcp.json", claudeSystemPromptInline("it's me"),
		"/w/a b/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "/w/a b/.oc-env",
		claudeHome{Home: "/Users/warden owner"}, "")
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
		line := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
			"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "/w/m1/.oc-env",
			claudeHome{Home: "/Users/wardenowner", ConfigDir: "/tmp/box"}, "")
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
		line := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
			"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "/w/m1/.oc-env",
			claudeHome{Home: "/Users/wardenowner", ConfigDir: "/tmp/box"}, "")
		source := strings.Index(line, ". /w/m1/.oc-env")
		pinHome := strings.Index(line, "HOME=/Users/wardenowner")
		pinDir := strings.Index(line, "CLAUDE_CONFIG_DIR=/tmp/box")
		if source < 0 || pinHome < 0 || pinDir < 0 {
			t.Fatalf("line is missing the source or a pin:\n%s", line)
		}
		if source > pinHome || source > pinDir {
			t.Errorf("source at %d must precede the pins (HOME=%d, CLAUDE_CONFIG_DIR=%d):\n%s", source, pinHome, pinDir, line)
		}
		unsetLine := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
			"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, "/w/m1/.oc-env",
			claudeHome{Home: "/Users/wardenowner"}, "")
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
		line := buildLaunchCommandWithEnv("/usr/local/bin/claude", workdir, "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
			"/dev/null", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "", nil, render,
			claudeHome{Home: "/Users/wardenowner"}, "")
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
		line := buildLaunchCommandWithEnv("/usr/local/bin/claude", "/w/m1", "/w/m1/.mcp.json", claudeSystemPromptInline("APPEND"),
			"/w/m1/.oc-token", "m1", "http://127.0.0.1:7755", "member-m1", "officraft", "", "", "",
			[][2]string{{"HOME", "/Volumes/scratch/home"}}, "", claudeHome{Home: "/Users/wardenowner"}, "")
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
  "hasCompletedOnboarding": true,
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
  "hasCompletedOnboarding": true,
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

	t.Run("a config left with onboarding cleared is marked completed and keeps the owner's own settings", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".claude.json")
		seed := `{"hasCompletedOnboarding":false,"theme":"light","oauthAccount":{"emailAddress":"seth@example.com"}}`
		if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := pretrustWorkdir(path, "/w/m1"); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := `{
  "hasCompletedOnboarding": true,
  "oauthAccount": {
    "emailAddress": "seth@example.com"
  },
  "projects": {
    "/w/m1": {
      "hasTrustDialogAccepted": true
    }
  },
  "theme": "light"
}
`
		if got := read(t, path); got != want {
			t.Errorf("claude.json =\n%s\nwant\n%s", got, want)
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
			want := "{\n  \"hasCompletedOnboarding\": true,\n  \"projects\": {\n    \"/w/m1\": {\n      \"hasTrustDialogAccepted\": true\n    }\n  }\n}\n"
			if got := read(t, path); got != want {
				t.Errorf("%q: claude.json =\n%s\nwant\n%s", corrupt, got, want)
			}
		}
	})

	t.Run("a workdir reached through a symlink is trusted under the path claude resolves", func(t *testing.T) {
		box, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatalf("resolve tempdir: %v", err)
		}
		real := filepath.Join(box, "real", "m1")
		if err := os.MkdirAll(real, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Symlink(filepath.Join(box, "real"), filepath.Join(box, "link")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		path := filepath.Join(box, ".claude.json")
		if err := pretrustWorkdir(path, filepath.Join(box, "link", "m1")); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		got := read(t, path)
		if !strings.Contains(got, `"`+real+`": {`) {
			t.Errorf("the trusted key must be the resolved path %s — claude looks up the cwd it resolves, so a literal key is written where it never reads. got\n%s", real, got)
		}
		if strings.Contains(got, filepath.Join(box, "link")) {
			t.Errorf("the literal symlink path is not a key claude ever looks up:\n%s", got)
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

	perPretrust, perPurge := 0, 0
	got := base.withPerSpawn(
		func() error { perPretrust++; return errors.New("boom") },
		func() { perPurge++ })

	if err := got.Pretrust(); err == nil || err.Error() != "boom" {
		t.Errorf("Pretrust err = %v, want boom", err)
	}
	got.PurgeTrash()
	got.Sleep(time.Second)
	if perPretrust != 1 || perPurge != 1 || baseClock != 1 {
		t.Errorf("perPretrust=%d perPurge=%d baseClock=%d, want 1/1/1 (the base clock is carried through)",
			perPretrust, perPurge, baseClock)
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
	t.Run("a claude spawn writes the workdir and the notification mod and launches with the mod", func(t *testing.T) {
		h := newSpawnHarness()
		got := h.deps().start(startParamsM1())

		if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
			t.Errorf("outcome = %+v, want %+v", got, want)
		}
		wantMkdirs := []string{"/w/m1", "/w/m1/.officraft-mod/.claude-plugin", "/w/m1/.officraft-mod/hooks",
			"/w/m1/.officraft-mod/hooks"}
		if !reflect.DeepEqual(h.mkdirs, wantMkdirs) {
			t.Errorf("mkdirs = %v, want %v", h.mkdirs, wantMkdirs)
		}
		wantWrites := append([]writtenFile{
			{"/w/m1/persona.md", "you are m1", 0o600},
			{"/w/m1/.mcp.json", buildMCPConfig("http://127.0.0.1:7755", "jwt-m1"), 0o600},
			{"/w/m1/settings.json", buildStatuslineSettings(), 0o600},
			{"/w/m1/.oc-token", "jwt-m1", 0o600},
			{"/w/m1/system-prompt.md", goldenSystemPromptM1, 0o600},
		}, notifyModWrites(t)...)
		if !reflect.DeepEqual(h.writes, wantWrites) {
			t.Errorf("writes =\n%+v\nwant\n%+v", h.writes, wantWrites)
		}
		// Every marker goes before the launch: a stale loaded marker would pass the
		// check for a mod that never loaded, a stale disabled one would mute it, a
		// stale started one would date a not-loaded diagnosis to the previous session.
		wantRemoves := []string{"/w/m1/ocagent", "/w/m1/.oc-env", "/w/m1/.officraft-mod-started", "/w/m1/.officraft-mod-loaded",
			"/w/m1/.officraft-mod-disabled", "/w/m1/.officraft-mod-booted"}
		if !reflect.DeepEqual(h.removes, wantRemoves) {
			t.Errorf("removes = %v, want %v", h.removes, wantRemoves)
		}
		wantLink := [][2]string{{"/Users/eva/.officraft/warden/ocagent", "/w/m1/ocagent"}}
		if !reflect.DeepEqual(h.symlinks, wantLink) {
			t.Errorf("symlinks = %v, want %v", h.symlinks, wantLink)
		}
		if h.pretrusts != 1 || h.purges != 1 {
			t.Errorf("pretrusts=%d purges=%d, want 1/1", h.pretrusts, h.purges)
		}
		wantCalls := []string{
			"tmux -L officraft has-session -t member-m1",
			"/usr/local/bin/claude --version",
			"reap /w/m1",
			"tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " + goldenLaunchM1,
			"tmux -L officraft set-option -t member-m1 window-size manual",
			"tmux -L officraft resize-window -t member-m1 -x 160 -y 50",
			// Nothing is pasted: the mod submits the boot prompt itself.
			"tmux -L officraft display-message -p -t member-m1 #{pane_pid}",
		}
		if !reflect.DeepEqual(h.runner.calls, wantCalls) {
			t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
		}
		// The wait's first poll finds the mod loaded, and the start checks it again.
		if want := []string{"/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-loaded"}; !reflect.DeepEqual(h.asked, want) {
			t.Errorf("exists asked = %v, want %v", h.asked, want)
		}
		if strings.Contains(h.runner.calls[3], "--settings /w/m1/settings.json") {
			t.Fatal("the launch still reads a workdir settings file that can be changed after the pre-trust gate")
		}
		if !strings.Contains(h.runner.calls[3], "--settings "+shellQuote(goldenInlineSettings)) {
			t.Fatal("the generated settings must ride the launch argv as inline JSON")
		}
		// The wait polls every 2 s and ends at the first poll that finds the mod loaded.
		if want := []time.Duration{time.Second, time.Second}; !reflect.DeepEqual(h.slept, want) {
			t.Errorf("slept %v, want %v", h.slept, want)
		}
		if want := []string{flagAbsentAttempt1}; !reflect.DeepEqual(h.logs, want) {
			t.Errorf("logs = %q, want %q", h.logs, want)
		}
	})

	t.Run("under a Claude Code older than 2.1.287, the start fails with the upgrade reason and launches nothing", func(t *testing.T) {
		h := newSpawnHarness()
		h.runner.script["/usr/local/bin/claude --version"] = wardenRun{out: "2.1.286 (Claude Code)\n"}
		got := h.deps().start(startParamsM1())

		if want := (SpawnOutcome{OK: false, Reason: goldenNotifyClaudeTooOldReason}); got != want {
			t.Errorf("outcome = %+v, want %+v", got, want)
		}
		wantCalls := []string{
			"tmux -L officraft has-session -t member-m1",
			"/usr/local/bin/claude --version",
		}
		if !reflect.DeepEqual(h.runner.calls, wantCalls) {
			t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
		}
		if len(h.mkdirs)+len(h.writes)+len(h.removes)+len(h.symlinks)+len(h.asked) != 0 || h.pretrusts != 0 {
			t.Errorf("mkdirs=%v writes=%v removes=%v symlinks=%v asked=%v pretrusts=%d, want nothing touched",
				h.mkdirs, h.writes, h.removes, h.symlinks, h.asked, h.pretrusts)
		}
		if want := []string{"m1: Claude Code 2.1.286 is older than 2.1.287; not starting"}; !reflect.DeepEqual(h.logs, want) {
			t.Errorf("logs = %v, want %v", h.logs, want)
		}
	})

	t.Run("under a mod that did not load, the member is stopped and the start fails", func(t *testing.T) {
		baseWrites := func() []writtenFile {
			return append([]writtenFile{
				{"/w/m1/persona.md", "you are m1", 0o600},
				{"/w/m1/.mcp.json", buildMCPConfig("http://127.0.0.1:7755", "jwt-m1"), 0o600},
				{"/w/m1/settings.json", buildStatuslineSettings(), 0o600},
				{"/w/m1/.oc-token", "jwt-m1", 0o600},
				{"/w/m1/system-prompt.md", goldenSystemPromptM1, 0o600},
			}, notifyModWrites(t)...)
		}
		disabledWrite := writtenFile{"/w/m1/.officraft-mod-disabled", "the warden gave up on this session\n", 0o600}
		launch := []string{
			"tmux -L officraft has-session -t member-m1",
			"/usr/local/bin/claude --version",
			"reap /w/m1",
			"tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " + goldenLaunchM1,
			"tmux -L officraft set-option -t member-m1 window-size manual",
			"tmux -L officraft resize-window -t member-m1 -x 160 -y 50",
		}
		const capture = "tmux -L officraft capture-pane -p -t member-m1"
		const stopAttempt = "stop-attempt officraft member-m1 /w/m1"
		const pid = "tmux -L officraft display-message -p -t member-m1 #{pane_pid}"
		emptyPaneDiag := []string{
			"m1: notify-mod-not-loaded: .officraft-mod-started absent: the mod's session.start never ran with its config",
			"m1: notify-mod-not-loaded: pane member-m1, last 40 lines:",
			"m1: notify-mod-not-loaded pane| ",
		}

		t.Run("under a mod that never ran in either attempt, the restart is stopped and the reason names both flag values", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			got := h.deps().start(startParamsM1())

			if want := (SpawnOutcome{OK: false, Reason: goldenNotifyModRetriedReason}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			// Disabled before the restart (a late session.start stands down), cleared
			// with the other markers once attempt 1 is gone, written again at the give-up.
			wantWrites := append(baseWrites(), disabledWrite, disabledWrite)
			if !reflect.DeepEqual(h.writes, wantWrites) {
				t.Errorf("writes =\n%+v\nwant\n%+v", h.writes, wantWrites)
			}
			wantRemoves := []string{"/w/m1/ocagent", "/w/m1/.oc-env",
				"/w/m1/.officraft-mod-started", "/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-disabled", "/w/m1/.officraft-mod-booted",
				"/w/m1/.officraft-mod-started", "/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-disabled", "/w/m1/.officraft-mod-booted"}
			if !reflect.DeepEqual(h.removes, wantRemoves) {
				t.Errorf("removes = %v, want %v", h.removes, wantRemoves)
			}
			wantCalls := append(slices.Clone(launch), notifyModPollCaptures...)
			wantCalls = append(wantCalls,
				capture, // attempt 1's screen, before it is torn down
				stopAttempt,
				// The same launch line again.
				launch[3], launch[4], launch[5])
			wantCalls = append(wantCalls, notifyModPollCaptures...)
			wantCalls = append(wantCalls, capture, stopAttempt)
			if !reflect.DeepEqual(h.runner.calls, wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
			}
			wantAsked := append(slices.Clone(notifyModPollAsks),
				"/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-started", "/w/m1/.officraft-mod-booted")
			wantAsked = append(wantAsked, notifyModPollAsks...)
			wantAsked = append(wantAsked, "/w/m1/.officraft-mod-loaded")
			if !reflect.DeepEqual(h.asked, wantAsked) {
				t.Errorf("exists asked = %v, want %v", h.asked, wantAsked)
			}
			if want := slices.Repeat([]time.Duration{time.Second}, 60); !reflect.DeepEqual(h.slept, want) {
				t.Errorf("slept %v, want %v", h.slept, want)
			}
			wantLogs := []string{flagAbsentAttempt1, "m1: notify-mod: attempt 1 did not run the mod; restarting Claude Code once"}
			wantLogs = append(wantLogs, emptyPaneDiag...)
			wantLogs = append(wantLogs, flagAbsentAttempt2,
				"m1: the notification mod did not load; stopping the member", flagAbsentAtGiveUp)
			wantLogs = append(wantLogs, emptyPaneDiag...)
			if !reflect.DeepEqual(h.logs, wantLogs) {
				t.Errorf("logs =\n%q\nwant\n%q", h.logs, wantLogs)
			}
			// One read before each launch and one at the give-up, all of the file
			// the launch line pins.
			wantReads := slices.Repeat([]string{"/Users/wardenowner/.claude.json"}, 3)
			if !reflect.DeepEqual(h.claudeJSONReads, wantReads) {
				t.Errorf("claude.json reads = %v, want %v", h.claudeJSONReads, wantReads)
			}
		})

		t.Run("under a mod that never ran in attempt 1, a restart that runs it starts the member", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			// Attempt 1 is dead by 30 s; the restart's first poll (32 s) finds the mod loaded.
			h.presentAt = map[string]time.Duration{"/w/m1/.officraft-mod-loaded": 31 * time.Second}
			h.claudeJSON = []byte(`{"cachedGrowthBookFeatures":{"tengu_plugin_hooks_modules":false},"cachedGrowthBookFeaturesAt":1790909940000}`)
			h.onStop = func() {
				// What attempt 1 wrote back once it fetched the remote value.
				h.claudeJSON = []byte(`{"cachedGrowthBookFeatures":{"tengu_plugin_hooks_modules":true},"cachedGrowthBookFeaturesAt":1790910269000}`)
			}
			got := h.deps().start(startParamsM1())

			if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			wantCalls := append(slices.Clone(launch), notifyModPollCaptures...)
			wantCalls = append(wantCalls, capture, stopAttempt, launch[3], launch[4], launch[5], pid)
			if !reflect.DeepEqual(h.runner.calls, wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
			}
			// The disabled marker attempt 1 got is cleared before the restart, and no
			// give-up writes it again: the restarted mod must not stand down.
			wantWrites := append(baseWrites(), disabledWrite)
			if !reflect.DeepEqual(h.writes, wantWrites) {
				t.Errorf("writes =\n%+v\nwant\n%+v", h.writes, wantWrites)
			}
			if last := h.removes[len(h.removes)-2]; last != "/w/m1/.officraft-mod-disabled" {
				t.Errorf("removes = %v, want the disabled marker cleared after the teardown", h.removes)
			}
			wantLogs := []string{
				"m1: notify-mod: attempt 1/2: /Users/wardenowner/.claude.json tengu_plugin_hooks_modules=false " +
					"cachedGrowthBookFeaturesAt=2026-10-02T02:59:00Z (5m0s before this read)",
				"m1: notify-mod: attempt 1 did not run the mod; restarting Claude Code once",
			}
			wantLogs = append(wantLogs, emptyPaneDiag...)
			wantLogs = append(wantLogs, "m1: notify-mod: attempt 2/2: /Users/wardenowner/.claude.json tengu_plugin_hooks_modules=true "+
				"cachedGrowthBookFeaturesAt=2026-10-02T03:04:29Z (1s before this read)")
			if !reflect.DeepEqual(h.logs, wantLogs) {
				t.Errorf("logs =\n%q\nwant\n%q", h.logs, wantLogs)
			}
			if want := slices.Repeat([]time.Duration{time.Second}, 32); !reflect.DeepEqual(h.slept, want) {
				t.Errorf("slept %v, want %v", h.slept, want)
			}
		})

		const hasSession = "tmux -L officraft has-session -t member-m1"
		for _, tc := range []struct {
			name  string
			probe wardenRun
		}{
			// The kill did not take: attempt 1's session is still there.
			{"alive", wardenRun{}},
			// The probe itself broke (nil): not proof the session is gone.
			{"unknown", wardenRun{err: errors.New("permission denied")}},
		} {
			t.Run("under a teardown that fails with has-session "+tc.name+", there is no restart and the give-up stops the member again", func(t *testing.T) {
				h := newSpawnHarness()
				h.present = map[string]bool{}
				h.stopStuck = true
				h.onStop = func() { h.runner.script[hasSession] = tc.probe }
				got := h.deps().start(startParamsM1())

				if want := (SpawnOutcome{OK: false, Reason: goldenNotifyModNotLoadedReason}); got != want {
					t.Errorf("outcome = %+v, want %+v", got, want)
				}
				wantCalls := append(slices.Clone(launch), notifyModPollCaptures...)
				wantCalls = append(wantCalls, capture, stopAttempt, hasSession, capture, stopAttempt)
				if !reflect.DeepEqual(h.runner.calls, wantCalls) {
					t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
				}
				// Written once: the marker the restart check put down still stands.
				if want := append(baseWrites(), disabledWrite); !reflect.DeepEqual(h.writes, want) {
					t.Errorf("writes =\n%+v\nwant\n%+v", h.writes, want)
				}
				for _, want := range []string{
					"m1: notify-mod: attempt 1 could not be torn down; no restart",
					"m1: notify-mod: the member could not be torn down after the mod did not load",
				} {
					if !slices.Contains(h.logs, want) {
						t.Errorf("logs = %q, want %q", h.logs, want)
					}
				}
			})
		}

		t.Run("a teardown that killed the session but left a sweep survivor reports the spawn failed", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			// stop() reports false (a pid still answers after SIGKILL) though the
			// session is gone: the harness's has-session keeps answering absent.
			h.stopStuck = true
			got := h.deps().start(startParamsM1())

			want := SpawnOutcome{OK: false, Reason: "spawn_exec_failed: restarting after the notification mod did not load: " +
				"attempt 1's tmux session is gone but its teardown did not finish (a process survived the sweep); no restart"}
			if got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			wantCalls := append(slices.Clone(launch), notifyModPollCaptures...)
			wantCalls = append(wantCalls, capture, stopAttempt, hasSession)
			if !reflect.DeepEqual(h.runner.calls, wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
			}
		})

		t.Run("a restart whose launch fails reports the spawn failed", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			launches := 0
			d := h.deps()
			d.Runner = funcRunner(func(name string, args ...string) (string, error) {
				out, err := h.runner.Run(name, args...)
				if slices.Contains(args, "new-session") {
					if launches++; launches == 2 {
						return "", errors.New("server exited unexpectedly")
					}
				}
				return out, err
			})
			got := d.start(startParamsM1())

			want := SpawnOutcome{OK: false, Reason: "spawn_exec_failed: tmux new-session (restarting after the notification mod did not load): server exited unexpectedly"}
			if got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
		})

		t.Run("under a mod that booted the member, there is no restart and the member is stopped", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{"/w/m1/.officraft-mod-booted": true}
			got := h.deps().start(startParamsM1())

			if want := (SpawnOutcome{OK: false, Reason: goldenNotifyModNotLoadedReason}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			if want := append(baseWrites(), disabledWrite); !reflect.DeepEqual(h.writes, want) {
				t.Errorf("writes =\n%+v\nwant\n%+v", h.writes, want)
			}
			wantCalls := append(slices.Clone(launch), notifyModPollCaptures...)
			wantCalls = append(wantCalls, capture, stopAttempt)
			if !reflect.DeepEqual(h.runner.calls, wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
			}
			wantAsked := append(slices.Clone(notifyModPollAsks),
				"/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-started", "/w/m1/.officraft-mod-booted")
			if !reflect.DeepEqual(h.asked, wantAsked) {
				t.Errorf("exists asked = %v, want %v", h.asked, wantAsked)
			}
			if want := slices.Repeat([]time.Duration{time.Second}, 30); !reflect.DeepEqual(h.slept, want) {
				t.Errorf("slept %v, want %v", h.slept, want)
			}
			wantLogs := []string{flagAbsentAttempt1,
				"m1: notify-mod: the mod ran in attempt 1 but did not load; no restart (it may have booted the member)",
				"m1: the notification mod did not load; stopping the member", flagAbsentAtGiveUp}
			wantLogs = append(wantLogs, emptyPaneDiag...)
			if !reflect.DeepEqual(h.logs, wantLogs) {
				t.Errorf("logs =\n%q\nwant\n%q", h.logs, wantLogs)
			}
		})

		t.Run("a mod whose session.start lands just after the restart check finds the disabled marker and does not boot", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			d := h.deps()
			// The mod writes its started marker, then reads the disabled one. Here it
			// writes started right after the warden's post-wait started check (the
			// 16th ask, after the 15 polls), so only a disabled marker already on
			// disk can stop it booting a member the warden is about to kill.
			exists, startedAsks, modBooted := d.Exists, 0, false
			d.Exists = func(path string) bool {
				got := exists(path)
				if path == "/w/m1/.officraft-mod-started" {
					if startedAsks++; startedAsks == 16 {
						modBooted = !slices.Contains(h.writes, disabledWrite)
					}
				}
				return got
			}
			d.start(startParamsM1())

			if startedAsks < 16 {
				t.Fatalf("the started marker was asked %d times, want the post-wait check too", startedAsks)
			}
			if modBooted {
				t.Error("the disabled marker was written after the started check: a session.start in between boots the member the restart kills")
			}
		})

		t.Run("under a mod that started late in attempt 1, there is no restart and the one launch is stopped", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{"/w/m1/.officraft-mod-started": true}
			got := h.deps().start(startParamsM1())

			if want := (SpawnOutcome{OK: false, Reason: goldenNotifyModNotLoadedReason}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			if n, stops := countCalls(h.runner.calls, "new-session -d -s member-m1"), countCalls(h.runner.calls, "stop-attempt"); n != 1 || stops != 1 {
				t.Errorf("calls = %v, want one launch and one teardown", h.runner.calls)
			}
		})

		t.Run("under no teardown wired, there is no restart and the give-up kills the session", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			d := h.deps()
			d.StopAttempt = nil
			got := d.start(startParamsM1())

			if want := (SpawnOutcome{OK: false, Reason: goldenNotifyModNotLoadedReason}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			wantCalls := append(slices.Clone(launch), notifyModPollCaptures...)
			wantCalls = append(wantCalls, capture, "tmux -L officraft kill-session -t member-m1", "tmux -L officraft has-session -t member-m1")
			if !reflect.DeepEqual(h.runner.calls, wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
			}
		})
	})

	t.Run("under a mod that did not load, the warden log carries the started marker and the member's pane", func(t *testing.T) {
		const capture = "tmux -L officraft capture-pane -p -t member-m1"
		// 45 lines: the first five fall outside the last 40.
		var pane45 strings.Builder
		for i := 1; i <= 45; i++ {
			fmt.Fprintf(&pane45, "line %02d\n", i)
		}
		wantLast40 := []string{}
		for i := 6; i <= 45; i++ {
			wantLast40 = append(wantLast40, fmt.Sprintf("m1: notify-mod-not-loaded pane| line %02d", i))
		}
		// A 5001-byte line, 2500 "é" (2 bytes each) then "z": its last 4096 bytes
		// would start mid-rune, so the cut keeps 4095, 2047 "é" and the "z".
		wide := strings.Repeat("é", 2500) + "z"
		for _, tc := range []struct {
			name     string
			modTimes map[string]time.Time
			run      wardenRun
			want     []string
		}{
			{"a session.start 12.3 s late and a pane of what the member saw",
				map[string]time.Time{"/w/m1/.officraft-mod-started": spawnLaunchedAt.Add(12340 * time.Millisecond)},
				wardenRun{out: "╭ Claude Code ╮\n> 開始。\n\n\n"},
				[]string{
					"m1: notify-mod-not-loaded: .officraft-mod-started written 12.3s after launch",
					"m1: notify-mod-not-loaded: pane member-m1, last 40 lines:",
					"m1: notify-mod-not-loaded pane| ╭ Claude Code ╮",
					"m1: notify-mod-not-loaded pane| > 開始。",
				}},
			{"a pane taller than 40 lines keeps its last 40",
				nil,
				wardenRun{out: pane45.String()},
				append([]string{
					"m1: notify-mod-not-loaded: .officraft-mod-started absent: the mod's session.start never ran with its config",
					"m1: notify-mod-not-loaded: pane member-m1, last 40 lines:",
				}, wantLast40...)},
			{"a pane over 4 KiB keeps its last 4096 bytes, cut on a rune",
				nil,
				wardenRun{out: wide},
				[]string{
					"m1: notify-mod-not-loaded: .officraft-mod-started absent: the mod's session.start never ran with its config",
					"m1: notify-mod-not-loaded: pane member-m1, last 40 lines, cut to its last 4096 bytes:",
					"m1: notify-mod-not-loaded pane| " + strings.Repeat("é", 2047) + "z",
				}},
			{"a capture that fails is logged and the give-up goes on",
				nil,
				wardenRun{err: errors.New("can't find pane: member-m1")},
				[]string{
					"m1: notify-mod-not-loaded: .officraft-mod-started absent: the mod's session.start never ran with its config",
					"m1: notify-mod-not-loaded: capture-pane of member-m1 failed: can't find pane: member-m1",
				}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h := newSpawnHarness()
				h.present = map[string]bool{"/w/m1/.officraft-mod-booted": true}
				h.modTimes = tc.modTimes
				h.runner.script[capture] = tc.run
				d := h.deps()
				rec := &timeoutRecordingRunner{wardenRunner: h.runner}
				d.Runner = rec
				got := d.start(startParamsM1())

				// The pane stays out of the owner-facing reason.
				if want := (SpawnOutcome{OK: false, Reason: goldenNotifyModNotLoadedReason}); got != want {
					t.Errorf("outcome = %+v, want %+v", got, want)
				}
				wantLogs := append([]string{flagAbsentAttempt1,
					"m1: notify-mod: the mod ran in attempt 1 but did not load; no restart (it may have booted the member)",
					"m1: the notification mod did not load; stopping the member",
					flagAbsentAtGiveUp}, tc.want...)
				if !reflect.DeepEqual(h.logs, wantLogs) {
					t.Errorf("logs =\n%q\nwant\n%q", h.logs, wantLogs)
				}
				wantCalls := append([]string{
					"tmux -L officraft has-session -t member-m1",
					"/usr/local/bin/claude --version",
					"reap /w/m1",
					"tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " + goldenLaunchM1,
					"tmux -L officraft set-option -t member-m1 window-size manual",
					"tmux -L officraft resize-window -t member-m1 -x 160 -y 50",
				}, notifyModPollCaptures...)
				wantCalls = append(wantCalls, capture, "stop-attempt officraft member-m1 /w/m1")
				if !reflect.DeepEqual(h.runner.calls, wantCalls) {
					t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
				}
				// The version probe's 2s, then each capture's (15 polls, the give-up's):
				// a hung tmux must not stall the spawn past its line in
				// startReceiptDeadlineSecs (server/ocserverd/receipt_watch.go).
				if want := slices.Repeat([]time.Duration{2 * time.Second}, 17); !reflect.DeepEqual(rec.timeouts, want) {
					t.Errorf("timeouts = %v, want %v", rec.timeouts, want)
				}
			})
		}
	})

	t.Run("under Claude Code holding its plugins after a startup sync, the wait answers its banner with /reload-plugins once", func(t *testing.T) {
		// Claude Code's own words, typed out: the warden matches this banner.
		const banner = "  ⎿  Plugins changed. Run /reload-plugins to activate.\n"
		launch := []string{
			"tmux -L officraft has-session -t member-m1",
			"/usr/local/bin/claude --version",
			"reap /w/m1",
			"tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " + goldenLaunchM1,
			"tmux -L officraft set-option -t member-m1 window-size manual",
			"tmux -L officraft resize-window -t member-m1 -x 160 -y 50",
		}
		const capture = "tmux -L officraft capture-pane -p -t member-m1"
		const stopAttempt = "stop-attempt officraft member-m1 /w/m1"
		reload := []string{
			"tmux -L officraft copy-mode -q -t member-m1",
			"tmux -L officraft send-keys -t member-m1 -l /reload-plugins",
			"tmux -L officraft send-keys -t member-m1 Enter",
		}
		const reloadLog = "m1: notify-mod: plugins changed during startup; sent /reload-plugins"

		t.Run("a banner at 4 s gets one /reload-plugins, the mod then loads and the member starts", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			h.presentAt = map[string]time.Duration{
				"/w/m1/.officraft-mod-started": 5 * time.Second,
				"/w/m1/.officraft-mod-loaded":  7 * time.Second,
			}
			h.paneAt = func(elapsed time.Duration) string {
				if elapsed >= 4*time.Second {
					return banner
				}
				return "╭ Claude Code ╮\n"
			}
			got := h.deps().start(startParamsM1())

			if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			// 2 s: no banner yet; 4 s: the banner, answered; 6 s: started, no
			// capture; 8 s: loaded, the wait ends.
			wantCalls := append(slices.Clone(launch), capture, capture)
			wantCalls = append(wantCalls, reload...)
			wantCalls = append(wantCalls, "tmux -L officraft display-message -p -t member-m1 #{pane_pid}")
			if !reflect.DeepEqual(h.runner.calls, wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
			}
			// Once the reload is sent, polls only look for the load marker.
			wantAsked := []string{
				"/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-started",
				"/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-started",
				"/w/m1/.officraft-mod-loaded",
				"/w/m1/.officraft-mod-loaded",
				"/w/m1/.officraft-mod-loaded",
			}
			if !reflect.DeepEqual(h.asked, wantAsked) {
				t.Errorf("exists asked = %v, want %v", h.asked, wantAsked)
			}
			if want := slices.Repeat([]time.Duration{time.Second}, 8); !reflect.DeepEqual(h.slept, want) {
				t.Errorf("slept %v, want %v", h.slept, want)
			}
			if want := []string{flagAbsentAttempt1, reloadLog}; !reflect.DeepEqual(h.logs, want) {
				t.Errorf("logs = %q, want %q", h.logs, want)
			}
			// The version probe, two captures and the three send calls, each under
			// its 2 s: the wait's overrun in startReceiptDeadlineSecs counts on it.
			if want := slices.Repeat([]time.Duration{2 * time.Second}, 6); !reflect.DeepEqual(h.paneTimeouts, want) {
				t.Errorf("timeouts = %v, want %v", h.paneTimeouts, want)
			}
			for _, w := range h.writes {
				if w.path == "/w/m1/.officraft-mod-disabled" {
					t.Errorf("the mod was told to stand down: %+v", w)
				}
			}
		})

		t.Run("a banner still on screen after the reload gets no second one in that attempt, the restart gets its own, and a mod that never starts fails the start", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			h.paneAt = func(time.Duration) string { return banner }
			got := h.deps().start(startParamsM1())

			if want := (SpawnOutcome{OK: false, Reason: goldenNotifyModRetriedReason}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			// Per attempt: one capture at 2 s, answered; the next 14 polls capture nothing.
			wantCalls := append(slices.Clone(launch), capture)
			wantCalls = append(wantCalls, reload...)
			wantCalls = append(wantCalls, capture, stopAttempt, launch[3], launch[4], launch[5], capture)
			wantCalls = append(wantCalls, reload...)
			wantCalls = append(wantCalls, capture, stopAttempt)
			if !reflect.DeepEqual(h.runner.calls, wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
			}
			wait := append([]string{"/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-started"},
				slices.Repeat([]string{"/w/m1/.officraft-mod-loaded"}, 14)...)
			wantAsked := append(slices.Clone(wait), "/w/m1/.officraft-mod-loaded", "/w/m1/.officraft-mod-started", "/w/m1/.officraft-mod-booted")
			wantAsked = append(wantAsked, wait...)
			wantAsked = append(wantAsked, "/w/m1/.officraft-mod-loaded")
			if !reflect.DeepEqual(h.asked, wantAsked) {
				t.Errorf("exists asked = %v, want %v", h.asked, wantAsked)
			}
			if want := slices.Repeat([]time.Duration{time.Second}, 60); !reflect.DeepEqual(h.slept, want) {
				t.Errorf("slept %v, want %v", h.slept, want)
			}
			diag := []string{
				"m1: notify-mod-not-loaded: .officraft-mod-started absent: the mod's session.start never ran with its config",
				"m1: notify-mod-not-loaded: pane member-m1, last 40 lines:",
				"m1: notify-mod-not-loaded pane|   ⎿  Plugins changed. Run /reload-plugins to activate.",
			}
			wantLogs := []string{flagAbsentAttempt1, reloadLog, "m1: notify-mod: attempt 1 did not run the mod; restarting Claude Code once"}
			wantLogs = append(wantLogs, diag...)
			wantLogs = append(wantLogs, flagAbsentAttempt2, reloadLog,
				"m1: the notification mod did not load; stopping the member", flagAbsentAtGiveUp)
			wantLogs = append(wantLogs, diag...)
			if !reflect.DeepEqual(h.logs, wantLogs) {
				t.Errorf("logs =\n%q\nwant\n%q", h.logs, wantLogs)
			}
		})

		t.Run("a banner that holds the mod through attempt 1 is answered again after the restart, which then loads", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			restarted := false
			h.onStop = func() { restarted = true }
			// The restart's own banner at 32 s, answered; the mod loads at 35 s.
			h.presentAt = map[string]time.Duration{"/w/m1/.officraft-mod-loaded": 35 * time.Second}
			h.paneAt = func(time.Duration) string { return banner }
			got := h.deps().start(startParamsM1())

			if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			if !restarted {
				t.Fatal("attempt 1 was not torn down")
			}
			if n := countCalls(h.runner.calls, "-l /reload-plugins"); n != 2 {
				t.Errorf("/reload-plugins sent %d times, want once per attempt", n)
			}
		})

		t.Run("a mod that started captures nothing, and the wait ends at the first poll that finds it loaded", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{}
			h.presentAt = map[string]time.Duration{
				"/w/m1/.officraft-mod-started": time.Second,
				"/w/m1/.officraft-mod-loaded":  4 * time.Second,
			}
			h.paneAt = func(time.Duration) string { return banner }
			got := h.deps().start(startParamsM1())

			if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			wantCalls := append(slices.Clone(launch), "tmux -L officraft display-message -p -t member-m1 #{pane_pid}")
			if !reflect.DeepEqual(h.runner.calls, wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
			}
			if want := slices.Repeat([]time.Duration{time.Second}, 4); !reflect.DeepEqual(h.slept, want) {
				t.Errorf("slept %v, want %v", h.slept, want)
			}
			if want := []string{flagAbsentAttempt1}; !reflect.DeepEqual(h.logs, want) {
				t.Errorf("logs = %q, want %q", h.logs, want)
			}
		})

		t.Run("under a clock that does not move, the wait still ends after 30 sleeps", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{"/w/m1/.officraft-mod-booted": true}
			h.paneAt = func(time.Duration) string { return "╭ Claude Code ╮\n" }
			d := h.deps()
			d.Now = func() time.Time { return spawnLaunchedAt }
			d.start(startParamsM1())

			if want := slices.Repeat([]time.Duration{time.Second}, 30); !reflect.DeepEqual(h.slept, want) {
				t.Errorf("slept %d times, want 30", len(h.slept))
			}
		})

		t.Run("under a nil Sleep, the wait paces on real time (nudgeClock), not a no-op", func(t *testing.T) {
			h := newSpawnHarness() // the mod is loaded: the first poll, 2 real seconds in, ends the wait
			d := h.deps()
			d.Sleep = nil
			began := time.Now()
			if got := d.start(startParamsM1()); !got.OK {
				t.Errorf("outcome = %+v, want the member started", got)
			}
			if took := time.Since(began); took < 2*time.Second {
				t.Errorf("the wait took %v, want at least 2 s of real time", took)
			}
		})

		t.Run("captures that take 3 s each end the wait at its 30 s deadline, not after 30 sleeps", func(t *testing.T) {
			h := newSpawnHarness()
			h.present = map[string]bool{"/w/m1/.officraft-mod-booted": true}
			h.captureCost = 3 * time.Second
			h.paneAt = func(time.Duration) string { return "╭ Claude Code ╮\n" }
			got := h.deps().start(startParamsM1())

			if want := (SpawnOutcome{OK: false, Reason: goldenNotifyModNotLoadedReason}); got != want {
				t.Errorf("outcome = %+v, want %+v", got, want)
			}
			// Polls at 2, 7, 12, 17, 22 and 27 s of the harness clock, each capture
			// adding 3 s: the poll ending at 30 s is the last.
			if want := slices.Repeat([]time.Duration{time.Second}, 12); !reflect.DeepEqual(h.slept, want) {
				t.Errorf("slept %v, want %v", h.slept, want)
			}
			wantCalls := append(slices.Clone(launch), slices.Repeat([]string{capture}, 7)...)
			wantCalls = append(wantCalls, stopAttempt)
			if !reflect.DeepEqual(h.runner.calls, wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
			}
		})
	})

	t.Run("under a configured nudge, that text is the boot prompt the mod submits", func(t *testing.T) {
		h := newSpawnHarness()
		d := h.deps()
		d.Nudge = "請開機。"
		if got := d.start(startParamsM1()); !got.OK {
			t.Fatalf("outcome = %+v, want OK", got)
		}
		want := `{"boot_prompt":"請開機。","started_marker":"/w/m1/.officraft-mod-started","loaded_marker":"/w/m1/.officraft-mod-loaded",` +
			`"disabled_marker":"/w/m1/.officraft-mod-disabled","booted_marker":"/w/m1/.officraft-mod-booted",` +
			`"ready_prefixes":["[ocagent] listen: connected","[ocagent] listen: disconnected"],` +
			`"listener":{"argv":["/w/m1/ocagent","listen","--deliver-socket"],"cwd":"/w/m1"}}` + "\n"
		var got string
		for _, w := range h.writes {
			if w.path == "/w/m1/.officraft-mod/officraft.json" {
				got = w.content
			}
		}
		if got != want {
			t.Errorf("officraft.json =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("under leftover ocagent processes in the workdir, they are reaped before the launch and logged", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			stuck bool
			log   string
		}{
			{"reaped", false, "m1: reaped 2 leftover ocagent process(es) in /w/m1"},
			{"still alive", true, "m1: 2 leftover ocagent process(es) in /w/m1 did not exit; spawning anyway"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h := newSpawnHarness()
				h.reaped, h.reapStuck = 2, tc.stuck
				got := h.deps().start(startParamsM1())

				if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
					t.Errorf("outcome = %+v, want %+v", got, want)
				}
				if want := []string{tc.log, flagAbsentAttempt1}; !reflect.DeepEqual(h.logs, want) {
					t.Errorf("logs = %v, want %v", h.logs, want)
				}
			})
		}
	})

	t.Run("under a Claude Code at or past 2.1.287, or one whose version cannot be read, the member starts with the mod", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			version wardenRun
		}{
			{"exactly the minimum", wardenRun{out: "2.1.287 (Claude Code)\n"}},
			{"a later major", wardenRun{out: "3.0.0 (Claude Code)\n"}},
			{"a version that is no version", wardenRun{out: "Claude Code nightly\n"}},
			{"a probe that failed", wardenRun{err: errors.New("timeout after 2s")}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h := newSpawnHarness()
				h.runner.script["/usr/local/bin/claude --version"] = tc.version
				got := h.deps().start(startParamsM1())

				if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
					t.Errorf("outcome = %+v, want %+v", got, want)
				}
				launch := "tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " + goldenLaunchM1
				if !slices.Contains(h.runner.calls, launch) {
					t.Errorf("calls = %v, want the launch with the mod", h.runner.calls)
				}
			})
		}
	})

	t.Run("the claude launch asks about the prompt file it is about to pass", func(t *testing.T) {
		h := newSpawnHarness()

		if got := h.deps().start(startParamsM1()); !got.OK {
			t.Fatalf("outcome = %+v, want OK", got)
		}
		if want := []string{"/w/m1/system-prompt.md"}; !reflect.DeepEqual(h.promptProbes, want) {
			t.Errorf("prompt-file probes = %v, want %v", h.promptProbes, want)
		}
	})

	t.Run("a claude without the prompt-file flag boots by reading the persona file", func(t *testing.T) {
		wantLaunch := "tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " +
			`cd /w/m1; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
			`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft ` +
			`HOME=/Users/wardenowner; ` +
			`export PATH=/w/m1:"$PATH"; ` +
			`exec /usr/local/bin/claude --dangerously-skip-permissions ` +
			`--disallowedTools AskUserQuestion --mcp-config /w/m1/.mcp.json --effort medium ` +
			`--append-system-prompt ` + shellQuote(goldenFallbackPromptM1) +
			` --settings ` + shellQuote(goldenInlineSettings) + ` --plugin-dir /w/m1/.officraft-mod`
		for _, tc := range []struct {
			name    string
			probe   func(string) (bool, string)
			wantLog string
		}{
			{"the probe says no", func(string) (bool, string) { return false, "the probe timed out" },
				"m1 boots by reading persona.md itself, not via --append-system-prompt-file (/usr/local/bin/claude): the probe timed out"},
			{"no probe is wired", nil,
				"m1 boots by reading persona.md itself, not via --append-system-prompt-file (/usr/local/bin/claude): no prompt-file probe is wired"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h := newSpawnHarness()
				d := h.deps()
				d.ClaudeTakesPromptFile = tc.probe

				if got := d.start(startParamsM1()); !got.OK {
					t.Fatalf("outcome = %+v, want OK", got)
				}
				if h.runner.calls[3] != wantLaunch {
					t.Errorf("launch call =\n%s\nwant\n%s", h.runner.calls[3], wantLaunch)
				}
				for _, w := range h.writes {
					if w.path == "/w/m1/system-prompt.md" {
						t.Errorf("wrote %s for a claude that cannot take it", w.path)
					}
				}
				if !slices.Contains(h.logs, tc.wantLog) {
					t.Errorf("warden logs = %q, want them to carry %q", h.logs, tc.wantLog)
				}
			})
		}
	})

	t.Run("a boot file far past one Read call reaches the prompt file whole", func(t *testing.T) {
		h := newSpawnHarness()
		p := startParamsM1()
		p.PersonaContext = strings.Repeat("一行很長的開機檔內容。\n", 4000) + "# 啟動步驟\nTAIL-MARKER-7f3c\n"

		if got := h.deps().start(p); !got.OK {
			t.Fatalf("outcome = %+v, want OK", got)
		}
		prompt := ""
		for _, w := range h.writes {
			if w.path == "/w/m1/system-prompt.md" {
				prompt = w.content
			}
		}
		if len(p.PersonaContext) < 100_000 {
			t.Fatalf("fixture is only %d bytes; it must be larger than a real boot file", len(p.PersonaContext))
		}
		if !strings.HasSuffix(prompt, "\n---\n"+p.PersonaContext) {
			t.Errorf("system prompt (%d bytes) does not end with the %d-byte boot file verbatim",
				len(prompt), len(p.PersonaContext))
		}
	})

	t.Run("a prompt file that cannot be written refuses the spawn", func(t *testing.T) {
		h := newSpawnHarness()
		h.writeErr["/w/m1/system-prompt.md"] = errors.New("disk full")

		got := h.deps().start(startParamsM1())

		if want := (SpawnOutcome{OK: false, Reason: "write_file_failed: system-prompt.md: disk full"}); got != want {
			t.Errorf("outcome = %+v, want %+v", got, want)
		}
		for _, call := range h.runner.calls {
			if strings.Contains(call, "new-session") {
				t.Errorf("a member was launched without its prompt file: %s", call)
			}
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
		wantLaunch := "tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " +
			`cd /w/m1; [ -f /w/m1/.oc-env ] && . /w/m1/.oc-env; ` + goldenClaudePurge + `unset CLAUDE_CONFIG_DIR; ` +
			`export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
			`OC_BASE=http://127.0.0.1:7755 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft ` +
			`OC_AGENT_HOME=/w HOME=/Users/wardenowner; ` +
			`export PATH=/w/m1:"$PATH"; ` +
			`exec /usr/local/bin/claude --dangerously-skip-permissions --disallowedTools AskUserQuestion ` +
			`--mcp-config /w/m1/.mcp.json --effort high ` +
			`--append-system-prompt-file /w/m1/system-prompt.md --model opus --settings ` + shellQuote(goldenInlineSettings) +
			` --plugin-dir /w/m1/.officraft-mod`
		if h.runner.calls[3] != wantLaunch {
			t.Errorf("launch call =\n%s\nwant\n%s", h.runner.calls[3], wantLaunch)
		}
		for _, line := range h.logs {
			if strings.Contains(line, "ghp_abc") {
				t.Errorf("a credential value reached the log: %q", line)
			}
		}
	})

	t.Run("under a captured interactive shell env, the spawn leaves that layer for the login check", func(t *testing.T) {
		h := newSpawnHarness()
		d := h.deps()
		d.CaptureEnv = func() (string, error) { return "FROM_SHELL=v1\x00EDITOR=vim\x00", nil }
		d.LaunchEnv = &launchEnvCache{}
		if got := d.start(startParamsM1()); !got.OK {
			t.Fatalf("outcome = %+v, want OK", got)
		}
		want := []agentEnvPair{{"FROM_SHELL", "v1"}, {"EDITOR", "vim"}}
		if got := d.LaunchEnv.interactive(); !reflect.DeepEqual(got, want) {
			t.Errorf("cached launch env = %v, want %v", got, want)
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
			if strings.Contains(call, "new-session -d -s member-") {
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
			if strings.Contains(call, "new-session -d -s member-") {
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
			if strings.Contains(call, "new-session -d -s member-") {
				launched = true
			}
		}
		if !launched {
			t.Errorf("no session was started: %v", h.runner.calls)
		}
	})

	t.Run("under an unknown login verdict, the spawn launches, for claude and codex alike", func(t *testing.T) {
		for _, runtime := range []string{"claude", "codex"} {
			t.Run(runtime, func(t *testing.T) {
				h := newSpawnHarness()
				d := h.deps()
				d.CodexBin = "/usr/local/bin/codex"
				d.WardenBin = "/Users/eva/.officraft/warden/ocwarden"
				var asked []string
				d.LoginCheck = func(rt string) *bool {
					asked = append(asked, rt)
					return nil
				}
				p := startParamsM1()
				p.Runtime = runtime
				p.Model = "gpt-5"

				got := d.start(p)
				if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
					t.Errorf("outcome = %+v, want %+v", got, want)
				}
				if !reflect.DeepEqual(asked, []string{runtime}) {
					t.Errorf("login checked for %v, want [%s]", asked, runtime)
				}
			})
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
				d.LoginCheck = loginVerdicts(map[string]*bool{"claude": boolRef(false)})
			}, "claude_not_logged_in: `claude auth status` reports logged out on this host. " +
				"Fix any one: set this member's 執行環境 to Codex; log in with `claude` as this user; " +
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
				d.LoginCheck = loginVerdicts(map[string]*bool{"codex": boolRef(false)})
			}, "codex_not_logged_in: `codex login status` failed on this host"},
			{"a codex that lists no model of the chosen family", func(h *spawnHarness, d *SpawnDeps, p *StartParams) {
				p.Runtime = "codex"
				p.Model = "terra"
				d.CodexBin = "/usr/local/bin/codex"
				d.WardenBin = "/Users/eva/.officraft/warden/ocwarden"
				d.CodexModels = func(string) ([]codexModelEntry, error) {
					return []codexModelEntry{
						{ID: "gpt-6-astra"}, {ID: "gpt-6-sol"}, {ID: "gpt-6-terra", Hidden: true}, {ID: "gpt-5.5"},
					}, nil
				}
				h.runner.script["/usr/local/bin/codex --version"] = wardenRun{out: "codex-cli 0.159.2\n"}
			}, "codex_model_family_unavailable: this machine's Codex (version 0.159.2) lists no terra model; " +
				"available: gpt-6-astra, gpt-6-sol, gpt-5.5"},
			{"a codex whose model list cannot be read", func(h *spawnHarness, d *SpawnDeps, p *StartParams) {
				p.Runtime = "codex"
				p.Model = "sol"
				d.CodexBin = "/usr/local/bin/codex"
				d.WardenBin = "/Users/eva/.officraft/warden/ocwarden"
				d.CodexModels = func(string) ([]codexModelEntry, error) {
					return nil, errors.New("model/list timed out after 15s")
				}
				h.runner.script["/usr/local/bin/codex --version"] = wardenRun{err: errors.New("exit status 1")}
			}, "codex_model_family_unavailable: could not read the model list of this machine's Codex " +
				"(version unknown) to pick the newest sol model"},
			{"a warden built with no codex model lister", func(_ *spawnHarness, d *SpawnDeps, p *StartParams) {
				p.Runtime = "codex"
				p.Model = "luna"
				d.CodexBin = "/usr/local/bin/codex"
				d.WardenBin = "/Users/eva/.officraft/warden/ocwarden"
			}, "codex_model_family_unavailable: could not read the model list of this machine's Codex " +
				"(version unknown) to pick the newest luna model"},
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
			{"a stale mod-loaded marker that cannot be cleared", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.removeErr["/w/m1/.officraft-mod-loaded"] = errors.New("permission denied")
			}, "write_file_failed: clearing stale .officraft-mod-loaded: permission denied"},
			{"a notification mod that cannot be written", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.writeErr["/w/m1/.officraft-mod/hooks/register.ts"] = errors.New("no space left on device")
			}, "write_file_failed: .officraft-mod/hooks/register.ts: no space left on device"},
			{"a notification mod config that cannot be written", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.writeErr["/w/m1/.officraft-mod/officraft.json"] = errors.New("no space left on device")
			}, "write_file_failed: .officraft-mod/officraft.json: no space left on device"},
			{"a pretrust that failed", func(h *spawnHarness, _ *SpawnDeps, _ *StartParams) {
				h.pretrustE = errors.New("permission denied")
			}, "pretrust_failed: marking workdir trusted in claude.json: permission denied"},
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
		d.CodexModels = func(string) ([]codexModelEntry, error) {
			t.Error("a full model id was looked up in the Codex model list")
			return nil, nil
		}

		got := d.start(p)
		if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
			t.Errorf("outcome = %+v, want %+v", got, want)
		}
		wantLaunch := "tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " +
			`cd /w/m1; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
			`OC_BASE=http://127.0.0.1:7755 OC_ID=m1 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft; ` +
			`export PATH=/w/m1:"$PATH"; ` +
			`exec /Users/eva/.officraft/warden/ocwarden codex-session ` +
			`--codex-bin /usr/local/bin/codex --workdir /w/m1 --persona /w/m1/persona.md ` +
			`--agent-id m1 --model gpt-5 --effort high`
		wantCalls := []string{
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
		if len(h.promptProbes) != 0 {
			t.Errorf("a codex spawn probed claude for a prompt file: %v", h.promptProbes)
		}
		for _, line := range h.logs {
			if strings.Contains(line, "--append-system-prompt-file") {
				t.Errorf("a codex spawn logged a claude prompt-file fallback: %q", line)
			}
		}
		for _, w := range h.writes {
			if w.path == "/w/m1/system-prompt.md" || strings.Contains(w.path, ".officraft-mod") {
				t.Errorf("a codex spawn wrote %s", w.path)
			}
		}
		if want := []string{"/w/m1"}; !reflect.DeepEqual(h.mkdirs, want) {
			t.Errorf("mkdirs = %v, want %v: a codex spawn gets no notification mod", h.mkdirs, want)
		}
		if len(h.slept) != 0 {
			t.Errorf("slept %v, want none", h.slept)
		}
	})

	t.Run("a codex family word launches the newest full model this machine's Codex lists", func(t *testing.T) {
		fullList := []codexModelEntry{
			{ID: "gpt-5.6-sol"}, {ID: "gpt-6-sol"}, {ID: "gpt-6.1-sol"}, {ID: "gpt-7-sol", Hidden: true},
			{ID: "gpt-6.2-sol-mini"}, {ID: "gpt-6-luna"}, {ID: "gpt-5.6-luna"}, {ID: "gpt-5.6-terra"},
			{ID: "gpt-10-astra"}, {ID: "gpt-9.9-astra"}, {ID: "gpt-5.5"}, {ID: "codex-auto-review", Hidden: true},
		}
		cases := []struct {
			name      string
			family    string
			models    []codexModelEntry
			wantModel string
		}{
			{"sol skips a hidden newer id and a suffixed variant", "sol", fullList, "gpt-6.1-sol"},
			{"luna listed at 6 and 5.6 launches the 6", "luna", fullList, "gpt-6-luna"},
			{"terra listed only at 5.6 launches the 5.6", "terra", fullList, "gpt-5.6-terra"},
			{"astra compares versions numerically, so 10 beats 9.9", "astra", fullList, "gpt-10-astra"},
			{"two spellings of one version launch the one listed first", "sol",
				[]codexModelEntry{{ID: "gpt-6-sol"}, {ID: "gpt-6.0-sol"}}, "gpt-6-sol"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				h := newSpawnHarness()
				d := h.deps()
				d.CodexBin = "/usr/local/bin/codex"
				d.WardenBin = "/Users/eva/.officraft/warden/ocwarden"
				var asked []string
				d.CodexModels = func(bin string) ([]codexModelEntry, error) {
					asked = append(asked, bin)
					return c.models, nil
				}
				p := startParamsM1()
				p.Runtime = "codex"
				p.Model = c.family
				p.Effort = "high"

				got := d.start(p)
				if want := (SpawnOutcome{OK: true, SessionID: "member-m1", PID: "500"}); got != want {
					t.Errorf("outcome = %+v, want %+v", got, want)
				}
				if want := []string{"/usr/local/bin/codex"}; !reflect.DeepEqual(asked, want) {
					t.Errorf("model list asked of %v, want %v", asked, want)
				}
				wantCalls := []string{
					"tmux -L officraft has-session -t member-m1",
					"tmux -L officraft new-session -d -s member-m1 -x 160 -y 50 " +
						`cd /w/m1; export OC_TOKEN="$(/bin/cat /w/m1/.oc-token)" ` +
						`OC_BASE=http://127.0.0.1:7755 OC_ID=m1 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft; ` +
						`export PATH=/w/m1:"$PATH"; ` +
						`exec /Users/eva/.officraft/warden/ocwarden codex-session ` +
						`--codex-bin /usr/local/bin/codex --workdir /w/m1 --persona /w/m1/persona.md ` +
						`--agent-id m1 --model ` + c.wantModel + ` --effort high`,
					"tmux -L officraft set-option -t member-m1 window-size manual",
					"tmux -L officraft resize-window -t member-m1 -x 160 -y 50",
					"tmux -L officraft display-message -p -t member-m1 #{pane_pid}",
				}
				if !reflect.DeepEqual(h.runner.calls, wantCalls) {
					t.Errorf("calls =\n%v\nwant\n%v", h.runner.calls, wantCalls)
				}
			})
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
			`OC_BASE=http://127.0.0.1:7755 OC_ID=m1 OC_SESSION=member-m1 OC_TMUX_SOCKET=officraft; ` +
			`export PATH=/w/m1:"$PATH"; ` +
			`exec /Users/eva/.officraft/warden/ocwarden codex-session ` +
			`--codex-bin /usr/local/bin/codex --workdir /w/m1 --persona /w/m1/persona.md ` +
			`--agent-id m1 --model gpt-5 --effort medium`
		if h.runner.calls[1] != wantLaunch {
			t.Errorf("launch call =\n%s\nwant\n%s", h.runner.calls[1], wantLaunch)
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
		if !strings.Contains(h.runner.calls[3], "OC_SESSION=custom-session") {
			t.Errorf("launch call =\n%s\nwant the custom session", h.runner.calls[3])
		}
		prompt := ""
		for _, w := range h.writes {
			if w.path == "/w/m1/system-prompt.md" {
				prompt = w.content
			}
		}
		if !strings.HasPrefix(prompt, "你是 m1(role=agent)。") {
			t.Errorf("system prompt =\n%q\nwant the default role", prompt)
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
