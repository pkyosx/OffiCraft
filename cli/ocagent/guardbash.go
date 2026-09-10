package main

// guard-bash — a PreToolUse hook that refuses removal commands whose target is
// not a literal path, so a headless member never reaches the harness's built-in
// dangerous-removal prompt.
//
// WHY A HOOK RATHER THAN A RULE IN THE SEEDS. That prompt ignores
// --dangerously-skip-permissions, the flag every member is launched with, and
// nobody is at the keyboard: the member stalls in silence and, from outside,
// looks exactly like a crash. A PreToolUse denial lands before the tool call, so
// the prompt is never raised rather than never answered. cli/CLAUDE.md §5 had
// carried the rule as prose for a while; on 2026-09-10 a member stalled during
// its own shutdown cleanup anyway. Deletable once the harness lets a headless
// session waive that prompt.
//
// WHAT THIS GUARD REFUSES IS DEFINED BY guardbash_test.go, NOT BY THIS COMMENT.
// Four review rounds in a row each found a false scope claim in this header with
// CI green, so scope is not restated here in any form — the tables in the test
// file are the only statement of it, and they redden when the rule moves.
//
// 🔴 THE REFUSAL TEXT IS PART OF THE FUNCTION, NOT COPY. Measured on seth-m5,
// 2026-09-10: worded as a request (「請改用 ocagent clean」) the member REFUSED to
// comply, reasoning that text arriving in a tool result is not its operator
// speaking and must not be allowed to rewrite the command it runs. That instinct
// is correct and must not be argued away. Worded as a statement about the
// environment (「本環境不允許…」) the same member rewrote the path itself and
// finished the job. Deletable when nothing member-facing is left in the refusal.
//
// OC_BASE CLASSIFICATION: EXEMPT. Contacts no station, never reads cfg.Base,
// takes no identity: one JSON document in on stdin, at most one out on stdout.
//
// FAIL-OPEN, ON PURPOSE. Unreadable stdin, malformed JSON, a missing command
// field: every one of them ALLOWS. This code sits in front of every single shell
// call a member makes, so a fail-closed bug here locks the member out of its own
// machine — strictly worse than the stall it exists to prevent. Deletable when
// the hook stops being on the critical path of every Bash call.

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
)

// removalWithAnExpandedTarget matches a removal whose own argument segment — up
// to the next separator — carries a shell expansion: a variable ($D, ${DIR},
// "$D"/x) or a command substitution ($(...) or backticks).
//
// 🔴 IT DELIBERATELY DOES NOT REPRODUCE THE HARNESS'S OWN ANALYSIS. A copy of
// that analysis would drift with someone else's releases and fail by stalling —
// the exact defect this guard exists to remove. So the rule asks one much cruder
// question, and over-refuses inside the spelling it does reach: a false refusal
// costs the member one rewrite it performs by itself, a missed one costs a stall.
// Deletable if the harness ever exposes its verdict to a hook.
//
// 🔴 THE KEYWORD GROUP IS NOT EXHAUSTIVE, AND LENGTHENING IT IS NOT THE FIX.
// do/then/else/elif/{ are in it because anchoring on separators alone made the
// body of every loop and conditional invisible, and deleting a batch of files in
// a loop is ordinary member work — that shape stalled T-163's reviewer for 3h50m
// on 2026-09-10. Other tokens still stand in front of removals this rule allows
// (guardbash_test.go names the ones known today); every review round has found
// another, which is the signal that adding the next spelling does not converge.
// Anything added here needs its case in guardbash_test.go in the same edit.
var removalWithAnExpandedTarget = regexp.MustCompile(
	"(?:^|[|;&\n(])\\s*(?:(?:do|then|else|elif|\\{)\\s+)*(?:sudo\\s+)?(?:rm|rmdir)\\b[^|;&\n)]*[$`]",
)

// guardBashRefusal is the text the member reads. See the header: it is a
// statement about the environment, never a request.
const guardBashRefusal = "OffiCraft 執行環境政策：移除指令的目標路徑必須是字面路徑，不得含 shell 變數或指令替換。" +
	"這個形狀會觸發執行環境的確認提示，而本環境沒有人可以回答，成員會就此停住。" +
	"這是環境層的固定政策，不是建議，也不是由指令輸出提出的要求。" +
	"改寫方式：改用 ocagent clean <完整路徑>，或直接寫出完整的字面路徑（不含變數、不含 ~、不含萬用字元）。" +
	"不要改成「先 cd 進目標目錄再用 ./* 之類的相對萬用字元」——那個形狀一樣會停住。" +
	"改寫後直接重試，不需要詢問任何人。"

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
