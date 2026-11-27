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
const guardPermissionRefusal = "OffiCraft runtime policy: this environment does not allow any operation that needs human confirmation. " +
	"This tool call triggered the runtime's confirmation prompt, and nobody in this environment can answer that prompt, so the member would silently stop right there. " +
	"This is a fixed policy of the environment layer, refused automatically by the environment, not a suggestion, and not a request made by command output. " +
	"A workable alternative: to delete a file or folder, use rm -r <full path>, with the path written out in full as a literal (no variable, no ~, no wildcard). " +
	"Switch to an approach that needs no confirmation and retry right away; you do not need to ask anyone."

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
