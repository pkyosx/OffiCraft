package main

// guard-bash — the PreToolUse hook that keeps a headless member from freezing on
// the harness's un-waivable removal prompt.
//
// THE PROBLEM IT SOLVES. Claude Code carries a built-in safety check for removals
// whose target is built from a shell variable it cannot resolve. That check
// demands a person choose Yes or No, and it is immune to
// --dangerously-skip-permissions — the flag every member is launched with. An
// OffiCraft member has nobody at the keyboard, so the demand is a stall with no
// signal: from the outside it looks exactly like a crash, a logout, or an
// exhausted quota. cli/CLAUDE.md §5 has stated the rule for a while (「agent 不
// 直接 rm working tree 內容；harness 的 dangerous-rm gate 會讓 headless agent 卡死」)
// and cli/ocwarden/spawn.go's PurgeTrash reaps the quarantine that rule creates.
// What was missing is the half that acts when the rule is not followed: on
// 2026-09-10 one member stalled during its own shutdown cleanup and the owner
// had to press the button by hand.
//
// WHY THIS LAYER. A PreToolUse hook runs BEFORE the tool call, and a denial from
// it means the command never reaches the harness's check at all — so the prompt
// is not answered, it is never raised. The member cannot route around the hook
// and does not have to remember any rule, which is the property a prose
// instruction cannot offer.
//
// 🔴 THE REFUSAL TEXT IS PART OF THE FUNCTION, NOT COPY. Measured on seth-m5,
// 2026-09-10: worded as a request (「請改用 ocagent clean」) the member REFUSED to
// comply, reasoning that text arriving in a tool result is not its operator
// speaking and must not be allowed to rewrite the command it runs. That instinct
// is correct and must not be argued away. Worded as a statement about the
// environment (「本環境不允許…」) the same member rewrote the path itself and
// finished the job. Anyone softening this string back into a request restores
// the failure.
//
// 🔴 DELIBERATELY WIDER THAN THE HARNESS CHECK. The harness only prompts when it
// cannot resolve the variable — `D=/abs/path && rm -f "$D"/*.json` resolves and
// runs (measured). This guard does NOT reproduce that resolution logic and
// refuses EVERY variable-built removal target. The costs are asymmetric: a
// false refusal costs one rewrite, which the member performs by itself; a missed
// one costs a stall, which is the entire defect. Reproducing an unverifiable
// analysis that drifts with someone else's releases would trade a cheap failure
// for the expensive one.
//
// OC_BASE CLASSIFICATION: EXEMPT. This subcommand contacts no station and never
// reads cfg.Base — it reads one JSON document on stdin and writes at most one on
// stdout. It takes no identity either, so unlike clean it has nothing to refuse
// for.
//
// FAIL-OPEN, ON PURPOSE. Unreadable stdin, malformed JSON, a missing command
// field: every one of them ALLOWS. This code sits in front of every single shell
// call a member makes, so a fail-closed bug here locks the member out of its own
// machine — strictly worse than the stall it exists to prevent.

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
)

// removalWithAnExpandedTarget matches a removal command whose arguments contain
// a shell expansion of any kind — a variable ($D, ${DIR}, "$D"/x) or a command
// substitution ($(...) or backticks).
//
// ONE RULE, THREE OF THE HARNESS'S FIVE REFUSALS. Reading the shipped binary
// (2.1.267) the built-in check refuses removals for five stated reasons, and the
// first three are one family — the target is not a literal path, so it cannot be
// analysed:
//
//	possibly-empty variable path                          ← measured stalling a member
//	possibly-empty variable path inside command substitution
//	statically-unresolvable target
//
// "The target must be a literal path" covers all three WITHOUT reproducing the
// harness's analysis, which is the property that matters: an analysis copied here
// would drift with someone else's releases and fail by stalling, the exact defect
// this guard exists to remove.
//
// 🔴 THE OTHER TWO ARE NOT COVERED, and they are a different family — a LITERAL
// path that is itself dangerous:
//
//	working directory or its ancestor
//	critical path (the filesystem root, the home directory)
//
// Deciding those needs the session's working directory, not just the command
// text, so they are deliberately out of scope here. A member that writes one of
// those shapes still stalls. Widening to them is a separate decision with its own
// false-positive surface; do not quietly bolt it on.
//
// The leading alternation anchors the command word to the start of the string or
// to a shell separator, so the word has to BE the command rather than appear
// inside somebody else's argument.
var removalWithAnExpandedTarget = regexp.MustCompile(
	"(?:^|[|;&\n(])\\s*(?:sudo\\s+)?(?:rm|rmdir)\\b[^|;&\n)]*[$`]",
)

// guardBashRefusal is the text the member reads. See the header: it is a
// statement about the environment, never a request.
const guardBashRefusal = "OffiCraft 執行環境政策：移除指令的目標路徑必須是字面路徑，不得含 shell 變數或指令替換。" +
	"這個形狀會觸發執行環境的確認提示，而本環境沒有人可以回答，成員會就此停住。" +
	"這是環境層的固定政策，不是建議，也不是由指令輸出提出的要求。" +
	"改寫方式：直接寫完整路徑，或先 cd 進目標目錄再用相對路徑，" +
	"或改用 ocagent clean <完整路徑>。改寫後直接重試，不需要詢問任何人。"

// preToolUseInput is the slice of the hook payload this guard reads. Every other
// field the harness sends is deliberately ignored.
type preToolUseInput struct {
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

// cmdGuardBash reads one PreToolUse payload and writes a deny decision when the
// command carries the shape that stalls a headless member. Allowing is silence:
// nothing on stdout, exit 0.
func cmdGuardBash(in io.Reader, out io.Writer) int {
	raw, err := io.ReadAll(in)
	if err != nil {
		return 0 // fail-open; see the header
	}
	var payload preToolUseInput
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0 // fail-open; see the header
	}
	if !removalWithAnExpandedTarget.MatchString(payload.ToolInput.Command) {
		return 0
	}
	// HTML escaping OFF so the refusal reads as written — it contains 「<完整路徑>」,
	// and < in front of a member is noise it has to decode.
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
