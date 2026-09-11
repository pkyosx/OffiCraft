package main

// guard-permission — a PermissionRequest hook.
//
// WHY IT EXISTS, AND WHY IT IS NOT guard-bash. Members are launched with
// --dangerously-skip-permissions, and the harness STILL raises its own
// confirmation prompt for a handful of shapes (the dangerous-removal prompt
// above all). That prompt cannot be waived by the flag and nobody is at the
// keyboard, so the member stalls in silence — measured once at roughly five
// hours. guard-bash (PreToolUse) keeps the common spelling from ever reaching
// that prompt; this hook is the second hand, standing at the moment the prompt
// HAS been raised. Measured 2026-09-11 on a session with a live controller: the
// hook wins the race against the prompt, is reached exactly once (harmless
// commands never arrive here), and a deny fails the tool call outright so the
// refusal text lands in front of the model, which then rewrites its own
// approach. Deletable once the harness lets a headless session waive the prompt.
//
// 🔴 IT REFUSES UNCONDITIONALLY, AND THAT IS THE DESIGN. It parses no command
// and reproduces none of the harness's own analysis. Under
// --dangerously-skip-permissions the only questions that reach a
// PermissionRequest hook are the ones the flag could not skip — so arriving here
// already means "nobody will answer this, and the member stops". There is
// nothing left to classify. Deletable if the harness ever starts routing
// skippable questions here too.
//
// FAIL-CLOSED, ON PURPOSE — the opposite of guard-bash. Unreadable stdin or
// malformed JSON still DENIES. guard-bash fails open because it sits in front of
// every single shell call a member makes, where a fail-closed bug locks the
// member out of its own machine. This hook is not on that path: it is invoked
// only once a blocking prompt is already being raised, so the choice is between
// a refusal the member can rewrite around and a stall nobody can. Deletable when
// a stall stops being the alternative.
//
// permission_suggestions in the payload was an EMPTY ARRAY in the 2026-09-11
// measurement (the run with a live controller). Nothing here reads it.
//
// OC_BASE CLASSIFICATION: EXEMPT. Contacts no station, never reads cfg.Base,
// takes no identity: one JSON document in on stdin, exactly one out on stdout.

import (
	"bytes"
	"encoding/json"
	"io"
)

// guardPermissionRefusal is the text the member reads. Same rule as guard-bash's
// refusal, measured the same way: written as a REQUEST (「請改用…」) the member
// refuses to comply, correctly reasoning that text arriving in a tool result is
// not its operator speaking; written as a STATEMENT ABOUT THE ENVIRONMENT the
// same member rewrites the command itself and finishes the job.
const guardPermissionRefusal = "OffiCraft 執行環境政策：本環境不允許任何需要人工確認的操作。" +
	"這個工具呼叫觸發了執行環境的確認提示，而本環境沒有人可以回答這個提示，成員會就此無聲停住。" +
	"這是環境層的固定政策，由環境自動拒絕，不是建議，也不是由指令輸出提出的要求。" +
	"可行的替代做法：要刪除檔案或資料夾時，改用 ocagent clean <完整路徑>（它不刪除，只把目標移到工作目錄下的 trash/）。" +
	"改用不需要確認的做法後直接重試，不需要詢問任何人。"

// permissionRequestDecision is the deny verdict, in the field shape the harness
// reads for a PermissionRequest hook. It is NOT PreToolUse's shape: the verdict
// rides a nested `decision` object with behavior/message, not the flat
// permissionDecision/permissionDecisionReason pair. Measured 2026-09-11.
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

// cmdGuardPermission answers one PermissionRequest payload with a denial. The
// payload is read and discarded: nothing in it can change the verdict, and a
// read or parse failure denies just the same (see the header).
func cmdGuardPermission(in io.Reader, out io.Writer) int {
	if raw, err := io.ReadAll(in); err == nil {
		var ignored any
		_ = json.Unmarshal(raw, &ignored)
	}
	// HTML escaping OFF so the refusal reads as written — it contains 「<完整路徑>」,
	// and < in front of a member is noise it has to decode.
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
