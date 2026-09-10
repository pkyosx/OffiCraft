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

// TestGuardBash_RefusesRemovalWhoseTargetIsNotALiteralPath is one half of this
// guard's stated scope; TestGuardBash_AllowsEverythingElse is the other. Between
// them they are the ONLY statement of what the guard covers — guardbash.go's
// header makes no scope claim, because four review rounds in a row each found a
// false one there with CI green.
func TestGuardBash_RefusesRemovalWhoseTargetIsNotALiteralPath(t *testing.T) {
	// Every one of these stalls a headless member today. "the command that
	// stalled T-163's reviewer" is the verbatim command that froze a member for
	// 3h50m on 2026-09-10 while it was reviewing this very guard.
	for name, command := range map[string]string{
		"start of string":                     `rm -rf $D/x`,
		"quoted variable with a glob":         `rm -f "$D"/*.json`,
		"quoted variable as the whole target": `rm -rf "$D"`,
		"bare variable":                       `rm -f $D/a.txt`,
		"braced variable":                     `rm -f ${DIR}/a.txt`,
		"quoted braced variable":              `rm -rf "${DIR}"/build`,
		"variable with no separator":          `rm -f $TMPFILE`,
		"rmdir rather than rm":                `rmdir "$D"/emptydir`,
		"command substitution target":         `rm -rf $(cat dirpath.txt)`,
		"backtick target":                     "rm -rf `cat dirpath.txt`",

		// The command word has to BE a command: at the start of the string, or
		// after a separator.
		"after a pipe":              `ls | rm -f "$D"/x`,
		"after a semicolon":         `cd /tmp; rm -rf $D`,
		"after &&":                  `cd /tmp && rm -rf $D`,
		"after a background &":      `sleep 1 & rm -rf $D`,
		"after a newline":           "cd /tmp\nrm -rf $D",
		"after an open paren":       `(rm -rf $D)`,
		"assigned on the same line": `D=/tmp/x && rm -rf "$D"/*.json`,
		"assigned with a semicolon": `D=/tmp/x; rm -rf $D/`,

		// A removal is not always the first word after a separator: shell keywords
		// stand between the two, and a loop or conditional body is where a member
		// deletes a batch of files.
		"the command that stalled T-163's reviewer": `./runone.sh D 'for f in a b; do rm -f "$OCPROBEDIR/$f.ocprobe-nomatch"; done'`,
		"inside a for loop body":                    `for f in a b; do rm -f $D/$f; done`,
		"inside a while loop body":                  `while read f; do rm -f $D/$f; done < list`,
		"inside a then branch":                      `if [ -d "$D" ]; then rm -rf "$D"; fi`,
		"inside an else branch":                     `if [ -f x ]; then ls; else rm -rf $D/x; fi`,
		"inside an elif branch":                     `if [ -f x ]; then ls; elif rm -rf $D; then ls; fi`,
		"inside a brace group":                      `{ rm -rf $D/x; }`,
		"brace group after a separator":             `cd /tmp; { rm -rf $D; }`,
		"a keyword at the start of the string":      `do rm -rf $D`,

		"behind sudo":                      `sudo rm -rf $D`,
		"behind sudo with a variable home": `sudo rm -rf $HOME/x`,
		"a keyword in front of sudo":       `for f in a; do sudo rm -rf $D/$f; done`,

		// Refused INCIDENTALLY: they carry a $, which is the only thing the rule
		// looks at. Their literal twins (rm -rf ~, rm -rf ..) are in the allow
		// table below, which is what makes this guard's boundary a spelling and
		// not a hazard class.
		"a critical path carrying a variable":       `rm -rf $HOME`,
		"the working directory carrying a variable": `rm -rf "$PWD"`,
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
	// Allowing is SILENCE. The first group is ordinary member work: a guard that
	// refuses any of it stops work instead of a stall. Everything after it is a
	// removal shape that a member can still stall on — asserted here so the gap is
	// a fact the suite states rather than a claim in a comment. Any of them moving
	// to the deny table is a deliberate edit, not a drift.
	subs := strings.Repeat("$(echo a)", 65)
	for name, command := range map[string]string{
		"the quarantine command":        `ocagent clean /Users/x/.officraft/agents/alice/work/tmp`,
		"relative path":                 `rm -f ./local.txt`,
		"relative glob":                 `rm -f ./*.json`,
		"a named directory":             `rm -rf node_modules`,
		"absolute path":                 `rm -f /tmp/x/a.json`,
		"a variable with no removal":    `ls -la "$D"/`,
		"not a removal at all":          `git status`,
		"the word inside another token": `echo "please rm -f $D/x by hand"`,

		// The removal spelled some other way: the rule only knows the words rm
		// and rmdir, standing alone as the command word.
		"find -delete":         `find "$D" -delete`,
		"find -exec rm":        `find "$D" -exec rm -f {} +`,
		"xargs rm":             `echo "$D" | xargs rm -rf`,
		"absolute path to rm":  `/bin/rm -rf $D`,
		"backslash-escaped rm": `\rm -rf $D`,
		"command rm":           `command rm -rf $D`,
		"env rm":               `env rm -rf $D`,
		"exec rm":              `exec rm -rf $D`,
		"assignment prefix":    `TMPDIR=x rm -rf $D`,

		// A token in front of the command word that is not in the keyword group.
		// Lengthening that group is not the fix; see guardbash.go.
		"keyword if":    `if rm -rf $D/x; then echo gone; fi`,
		"keyword until": `until rm -rf $D/x; do sleep 1; done`,
		"keyword while": `while rm -rf $D/x; do sleep 1; done`,
		"negation bang": `! rm -rf $D/x`,
		"time prefix":   `time rm -rf $D/x`,
		"nohup prefix":  `nohup rm -rf $D/x`,
		// The keyword alternation requires whitespace after the keyword, so a
		// brace written tight against the command word is not a keyword at all.
		"brace with no space after it": `{rm -rf $D;}`,

		// A target with no expansion in it: there is no $ for the rule to match.
		// guardBashRefusal must therefore not tell a member to rewrite into this
		// shape, which is what TestGuardBash_RefusalIsAStatementAboutTheEnvironmentNotARequest pins.
		"tilde glob":               `rm -rf ~/build/*`,
		"cd first, then relative":  `cd "$D" && rm -f ./*.json`,
		"cd literal then rel glob": `cd /tmp/x && rm -rf ./*`,
		"cd via var then rel glob": `D=/tmp/x; cd "$D" && rm -rf ./*`,
		"rmdir with a rel glob":    `rmdir -p ./build/*`,

		// Many command substitutions reached through a command word that is not
		// rm: the rule anchors on the command word and never sees these.
		"65 substitutions via xargs":   `echo ` + subs + ` | xargs rm -rf`,
		"65 substitutions via /bin/rm": `/bin/rm -rf ` + subs,
		"65 substitutions via find":    `find ` + subs + ` -exec rm -f {} +`,

		// A dangerous LITERAL path: no $, nothing for the rule to match. Their
		// variable-carrying twins are in the deny table above.
		"filesystem root":    `rm -rf /`,
		"bare tilde":         `rm -rf ~`,
		"working directory":  `rm -rf .`,
		"parent directory":   `rm -rf ..`,
		"a literal home dir": `rm -rf /Users/alice`,
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
	// The way forward must not be a shape that stalls too. This text used to
	// suggest "cd into the directory and use a relative path"; a member that
	// complies writes `cd "$D" && rm -rf ./*`, which this guard allows — see the
	// allow table in TestGuardBash_AllowsEverythingElse. The guard was handing the
	// member the stall it exists to prevent.
	if !strings.Contains(guardBashRefusal, "一樣會停住") {
		t.Error("the refusal no longer warns that the cd-plus-relative-glob rewrite " +
			"stalls as well; without that warning this text sends the member into " +
			"a shape this guard does not catch")
	}
}

// verdictOf answers what a member would observe: true when the guard refuses.
func verdictOf(t *testing.T, command string) bool {
	t.Helper()
	_, stdout := runGuardBash(t, command)
	return strings.TrimSpace(stdout) != ""
}

func TestGuardBash_ADollarInALaterSegmentDoesNotRefuseALiteralRemoval(t *testing.T) {
	// The $ has to be in the removal's OWN argument segment, up to the next
	// separator. That boundary is the entire control on this guard's false-refusal
	// surface, and a re-audit mutant widened it to `.*` with all 271 tests still
	// green. These two cases are what makes that mutant red.
	if verdictOf(t, `rm -rf /tmp/literal; echo $D`) {
		t.Error("guard reached past the segment boundary: a $ in a LATER segment " +
			"refused a removal whose own target is a literal path")
	}
	if !verdictOf(t, `rm -rf /tmp/literal$D; echo x`) {
		t.Error("POSITIVE CONTROL FAILED: a $ inside the removal's own segment was " +
			"not caught, so the case above proves nothing")
	}
}

func TestGuardBash_IsReachableThroughTheCLIEntryPoint(t *testing.T) {
	// cli/ocwarden/spawn.go writes the string "ocagent guard-bash" into every
	// member's settings.json, and until this test nothing crossed the seam between
	// that string and a subcommand that actually refuses: a re-audit mutant
	// replaced this case's body in main.go with `return 0` — the guard became a
	// permanent no-op — and all 824 tests across cli/ocagent and cli/ocwarden
	// stayed green. Both directions are checked, so neither a hard-wired allow nor
	// a hard-wired deny survives.
	noEnv := func(string) string { return "" }

	var denyOut bytes.Buffer
	if code := realMain([]string{"guard-bash"}, noEnv,
		strings.NewReader(`{"tool_input":{"command":"rm -rf $D/x"}}`), &denyOut); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if got := decisionOf(t, denyOut.String()); got != "deny" {
		t.Errorf("`ocagent guard-bash` answered %q for a stalling shape, want %q — "+
			"the subcommand settings.json points at is not refusing anything", got, "deny")
	}

	var allowOut bytes.Buffer
	if code := realMain([]string{"guard-bash"}, noEnv,
		strings.NewReader(`{"tool_input":{"command":"rm -rf ./local"}}`), &allowOut); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if strings.TrimSpace(allowOut.String()) != "" {
		t.Errorf("`ocagent guard-bash` refused ordinary work: %q", allowOut.String())
	}
}
