package main

import "fmt"

// ---------------------------------------------------------------------------
// pyfmt: tiny helpers that reproduce Python's f-string rendering for the two
// human status lines whose exact text is the parity contract (bootstrap's
// verdict line, chat-send's echo/FAILED line). Kept in one place so the
// divergence surface is auditable.
// ---------------------------------------------------------------------------

// pyBool renders a Go bool as Python does in an f-string ("True"/"False", not
// Go's "true"/"false"). The bootstrap verdict line prints four bools, so this is
// load-bearing for byte-for-byte parity with agent/oc_agent.py _cmd_bootstrap.
func pyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// pyStr renders a JSON-decoded value as Python's f-string does for the chat-send
// lines: a JSON null → "None" (Python None); a string → the string itself; any
// other scalar → its natural text. A JSON object/array falls back to Go's %v
// (a DELIBERATE, honest divergence from Python's dict/list repr — this only ever
// hits the diagnostic chat-send FAILED line, where the value is human-facing
// context, never machine-parsed).
func pyStr(v any) string {
	if v == nil {
		return "None"
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
