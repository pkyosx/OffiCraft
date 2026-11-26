package main

// Why a hook: the harness's dangerous-removal prompt ignores
// --dangerously-skip-permissions (every member's launch flag) and nobody is at
// the keyboard, so the member stalls silently; a PreToolUse deny means the prompt
// is never raised. The same rule as prose in cli/AGENTS.md §5 did not prevent a
// stall.
//
// OC_BASE CLASSIFICATION: EXEMPT.

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
)

// 🔴 Deliberately cruder than the harness's own analysis: a copy would drift with
// someone else's releases and fail by stalling. Over-refusing is intended — a
// false refusal costs one self-rewrite, a miss costs a stall.
//
// 🔴 The keyword group is not exhaustive, and lengthening it does not converge
// (every review round found another token). do/then/else/elif/{ are there
// because a removal inside a loop body went unseen and stalled a reviewer for
// 3h50m. Anything added needs its test case in the same edit.
var removalWithAnExpandedTarget = regexp.MustCompile(
	"(?:^|[|;&\n(])\\s*(?:(?:do|then|else|elif|\\{)\\s+)*(?:sudo\\s+)?(?:rm|rmdir)\\b[^|;&\n)]*[$`]",
)

// 🔴 The wording is function, not copy. Measured: phrased as a request, the member
// refused to comply (tool-result text is not its operator speaking); phrased as a
// statement about the environment, it rewrote the path itself and finished.
const guardBashRefusal = "OffiCraft runtime policy: the target path of a remove command must be a literal path, with no shell variable or command substitution. " +
	"This shape triggers the runtime's confirmation prompt, and nobody in this environment can answer it, so the member would stop right there. " +
	"This is a fixed policy of the environment layer, not a suggestion, and not a request made by command output. " +
	"How to rewrite it: write the target as the full literal path, for example rm -r <full path>, with no variable, no ~ and no wildcard. " +
	"Do not switch to \"cd into the target directory first, then use a relative wildcard such as ./*\" — that shape stops the same way. " +
	"Retry right after rewriting; you do not need to ask anyone."

type preToolUseInput struct {
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

func cmdGuardBash(in io.Reader, out io.Writer) int {
	// Every read/parse failure ALLOWS: this runs before every shell call a member
	// makes, so failing closed would lock the member out of its own machine.
	raw, err := io.ReadAll(in)
	if err != nil {
		return 0
	}
	var payload preToolUseInput
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	if !removalWithAnExpandedTarget.MatchString(payload.ToolInput.Command) {
		return 0
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": guardBashRefusal,
		},
	})
	_, _ = out.Write(buf.Bytes())
	return 0
}
