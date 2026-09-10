package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// runGuardBash feeds one PreToolUse payload through the subcommand and answers
// what a member would observe: the exit code and whatever landed on stdout.
func runGuardBash(t *testing.T, command string) (int, string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": command},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var out bytes.Buffer
	code := cmdGuardBash(bytes.NewReader(payload), &out)
	return code, out.String()
}

// decisionOf parses the guard's answer. A refusal is a decision document; an
// allow is silence, which this reports as an empty string.
func decisionOf(t *testing.T, stdout string) string {
	t.Helper()
	if strings.TrimSpace(stdout) == "" {
		return ""
	}
	var got struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("guard answered something that is not a hook decision: %q (%v)", stdout, err)
	}
	if got.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want %q", got.HookSpecificOutput.HookEventName, "PreToolUse")
	}
	if got.HookSpecificOutput.PermissionDecisionReason != guardBashRefusal {
		t.Errorf("refusal text diverged:\n got %q\nwant %q",
			got.HookSpecificOutput.PermissionDecisionReason, guardBashRefusal)
	}
	return got.HookSpecificOutput.PermissionDecision
}

func TestGuardBash_RefusesRemovalWhoseTargetIsNotALiteralPath(t *testing.T) {
	// Every one of these stalls a headless member today. "the command that
	// stalled T-163's reviewer" is the verbatim command that froze a member for
	// 3h50m on 2026-09-10 while it was reviewing this very guard.
	for name, command := range map[string]string{
		"quoted variable with a glob":  `rm -f "$D"/*.json`,
		"bare variable":                `rm -f $D/a.txt`,
		"braced variable":              `rm -f ${DIR}/a.txt`,
		"quoted braced variable":       `rm -rf "${DIR}"/build`,
		"assigned on the same line":    `D=/tmp/x && rm -rf "$D"/*.json`,
		"assigned with a semicolon":    `D=/tmp/x; rm -rf $D/`,
		"behind sudo":                  `sudo rm -rf $HOME/x`,
		"rmdir rather than rm":         `rmdir "$D"/emptydir`,
		"second command in a pipeline": `ls | rm -f "$D"/x`,
		"command substitution target":  `rm -rf $(cat dirpath.txt)`,
		"backtick target":              "rm -rf `cat dirpath.txt`",
		"variable with no separator":   `rm -f $TMPFILE`,

		// A removal is not always the first word after a separator: shell keywords
		// stand between the two, and a loop or conditional body is where a member
		// deletes a batch of files.
		"the command that stalled T-163's reviewer": `./runone.sh D 'for f in a b; do rm -f "$OCPROBEDIR/$f.ocprobe-nomatch"; done'`,
		"inside a for loop body":                    `for f in a b; do rm -f $D/$f; done`,
		"inside a while loop body":                  `while read f; do rm -f $D/$f; done < list`,
		"inside a then branch":                      `if [ -d "$D" ]; then rm -rf "$D"; fi`,
		"inside an else branch":                     `if [ -f x ]; then ls; else rm -rf $D/x; fi`,
		"inside a brace group":                      `{ rm -rf $D/x; }`,
		"brace group after a separator":             `cd /tmp; { rm -rf $D; }`,
		"a keyword in front of sudo":                `for f in a; do sudo rm -rf $D/$f; done`,
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout := runGuardBash(t, command)
			if code != 0 {
				t.Errorf("exit code = %d, want 0 (the decision travels on stdout, never the code)", code)
			}
			if got := decisionOf(t, stdout); got != "deny" {
				t.Errorf("permissionDecision = %q, want %q, for command %q", got, "deny", command)
			}
		})
	}
}

func TestGuardBash_AllowsEverythingElse(t *testing.T) {
	// Allowing is SILENCE. These are the commands a member runs all day; a guard
	// that refuses any of them stops ordinary work instead of a stall.
	for name, command := range map[string]string{
		"the quarantine command":        `ocagent clean /Users/x/.officraft/agents/alice/work/tmp`,
		"relative path":                 `rm -f ./local.txt`,
		"relative glob":                 `rm -f ./*.json`,
		"a named directory":             `rm -rf node_modules`,
		"absolute path":                 `rm -f /tmp/x/a.json`,
		"cd first, then relative":       `cd "$D" && rm -f ./*.json`,
		"a variable with no removal":    `ls -la "$D"/`,
		"not a removal at all":          `git status`,
		"the word inside another token": `echo "please rm -f $D/x by hand"`,
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout := runGuardBash(t, command)
			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Errorf("guard spoke where it should have stayed silent: %q, for command %q", stdout, command)
			}
		})
	}
}

func TestGuardBash_FailsOpenOnInputItCannotRead(t *testing.T) {
	// This code runs in front of EVERY shell call a member makes. A guard that
	// refuses when it cannot understand its own input locks the member out of its
	// machine, which is worse than the stall it exists to prevent.
	for name, stdin := range map[string]string{
		"empty stdin":             "",
		"not json":                "this is not json",
		"json but not an object":  `["a", "b"]`,
		"object with no command":  `{"tool_input": {}}`,
		"command is not a string": `{"tool_input": {"command": 7}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if code := cmdGuardBash(strings.NewReader(stdin), &out); code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			if strings.TrimSpace(out.String()) != "" {
				t.Errorf("guard refused on input it could not read: %q", out.String())
			}
		})
	}
}

func TestGuardBash_RefusalIsAStatementAboutTheEnvironmentNotARequest(t *testing.T) {
	// MEASURED, seth-m5, 2026-09-10: worded as a request the member refused to
	// comply, reasoning that text arriving in a tool result must not be allowed to
	// rewrite the command it runs — a correct instinct. Worded as a statement
	// about the environment the same member rewrote the path and finished. This
	// pins the two halves that carry that distinction, so softening the string
	// back into a request is red rather than a silent regression.
	if !strings.Contains(guardBashRefusal, "本環境沒有人可以回答") {
		t.Error("the refusal no longer says WHY the environment forbids this; " +
			"without it the text reads as one more request the member may decline")
	}
	if !strings.Contains(guardBashRefusal, "不是建議") {
		t.Error("the refusal no longer states that it is not a suggestion; " +
			"that clause is what stops the member treating tool output as an instruction to weigh")
	}
	// It also has to say what to do instead, or the member has a refusal and no exit.
	if !strings.Contains(guardBashRefusal, "ocagent clean") {
		t.Error("the refusal no longer names a way forward")
	}
}
