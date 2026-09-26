package main

// Why it exists besides guard-bash: under --dangerously-skip-permissions the
// harness still raises its confirmation prompt for some shapes, nobody answers,
// and the member stalls (once ~5h). Measured: this hook wins the race against the
// prompt, and a deny fails the tool call so the refusal reaches the model.
//
// 🔴 It refuses UNCONDITIONALLY by design: under that flag only the questions the
// flag could not skip reach a PermissionRequest hook, so arriving here already
// means nobody will answer.
//
// FAIL-CLOSED, the opposite of guard-bash: this runs only once a blocking prompt
// is raised, so the choice is a refusal the member can rewrite around or a stall.
//
// OC_BASE CLASSIFICATION: EXEMPT.

import (
	"bytes"
	"encoding/json"
	"io"
)

// Worded as a statement, never a request: see guardBashRefusal (guardbash.go).
const guardPermissionRefusal = "OffiCraft 執行環境政策：本環境不允許任何需要人工確認的操作。" +
	"這個工具呼叫觸發了執行環境的確認提示，而本環境沒有人可以回答這個提示，成員會就此無聲停住。" +
	"這是環境層的固定政策，由環境自動拒絕，不是建議，也不是由指令輸出提出的要求。" +
	"可行的替代做法：要刪除檔案或資料夾時，用 rm -rf <完整路徑>，路徑要完整字面寫出（不含變數、不含 ~、不含萬用字元）。" +
	"改用不需要確認的做法後直接重試，不需要詢問任何人。"

// The harness reads THIS nested decision{behavior,message} shape for a
// PermissionRequest hook, not PreToolUse's flat permissionDecision pair (measured).
type permissionRequestDecision struct {
	Behavior string `json:"behavior"`
	Message  string `json:"message"`
}

type permissionRequestHookOutput struct {
	HookEventName string                    `json:"hookEventName"`
	Decision      permissionRequestDecision `json:"decision"`
}

type permissionRequestAnswer struct {
	HookSpecificOutput permissionRequestHookOutput `json:"hookSpecificOutput"`
}

func cmdGuardPermission(in io.Reader, out io.Writer) int {
	if raw, err := io.ReadAll(in); err == nil {
		var ignored any
		_ = json.Unmarshal(raw, &ignored)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(permissionRequestAnswer{
		HookSpecificOutput: permissionRequestHookOutput{
			HookEventName: "PermissionRequest",
			Decision: permissionRequestDecision{
				Behavior: "deny",
				Message:  guardPermissionRefusal,
			},
		},
	})
	_, _ = out.Write(buf.Bytes())
	return 0
}
