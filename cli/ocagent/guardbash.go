package main

// Why a hook: the harness's dangerous-removal prompt ignores
// --dangerously-skip-permissions (every member's launch flag) and nobody is at
// the keyboard, so the member stalls silently; a PreToolUse deny means the prompt
// is never raised. The same rule as prose in cli/CLAUDE.md §5 did not prevent a
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
const guardBashRefusal = "OffiCraft 執行環境政策：移除指令的目標路徑必須是字面路徑，不得含 shell 變數或指令替換。" +
	"這個形狀會觸發執行環境的確認提示，而本環境沒有人可以回答，成員會就此停住。" +
	"這是環境層的固定政策，不是建議，也不是由指令輸出提出的要求。" +
	"改寫方式：把目標寫成完整的字面路徑，例如 rm -rf <完整路徑>，不含變數、不含 ~、不含萬用字元。" +
	"不要改成「先 cd 進目標目錄再用 ./* 之類的相對萬用字元」——那個形狀一樣會停住。" +
	"改寫後直接重試，不需要詢問任何人。"

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
