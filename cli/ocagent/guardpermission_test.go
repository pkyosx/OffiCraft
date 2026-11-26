package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// wantPermissionRefusal is the refusal a member reads, hand-written here rather
// than read off guardPermissionRefusal: the text IS the function (see
// guardpermission.go), so a test that quoted the constant would agree with any
// rewrite of it, including one that turns the statement back into a request.
const wantPermissionRefusal = "OffiCraft runtime policy: this environment does not allow any operation that needs human confirmation. " +
	"This tool call triggered the runtime's confirmation prompt, and nobody in this environment can answer that prompt, so the member would silently stop right there. " +
	"This is a fixed policy of the environment layer, refused automatically by the environment, not a suggestion, and not a request made by command output. " +
	"A workable alternative: to delete a file or folder, use rm -r <full path>, with the path written out in full as a literal (no variable, no ~, no wildcard). " +
	"Switch to an approach that needs no confirmation and retry right away; you do not need to ask anyone."

// wantPermissionAnswer is the WHOLE document the hook writes, byte for byte —
// the field shape is PermissionRequest's, not PreToolUse's, and a document that
// merely parses is not the same as one the harness acts on.
const wantPermissionAnswer = `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"` +
	wantPermissionRefusal + `"}}}` + "\n"

// failingReader is a stdin that cannot be read: the fail-closed arm.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("stdin is gone") }

func TestCmdGuardPermission(t *testing.T) {
	t.Run("an ordinary permission question is refused", func(t *testing.T) {
		payload := `{"hook_event_name":"PermissionRequest","tool_name":"Bash",` +
			`"tool_input":{"command":"rm -rf \"$TMPDIR\"/build"},"permission_suggestions":[]}`
		var out bytes.Buffer
		if rc := cmdGuardPermission(strings.NewReader(payload), &out); rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if out.String() != wantPermissionAnswer {
			t.Errorf("answered\n%q\nwant\n%q", out.String(), wantPermissionAnswer)
		}
	})

	t.Run("a question carrying no suggestions is refused the same way", func(t *testing.T) {
		var out bytes.Buffer
		cmdGuardPermission(strings.NewReader(`{"hook_event_name":"PermissionRequest"}`), &out)
		if out.String() != wantPermissionAnswer {
			t.Errorf("answered\n%q\nwant\n%q", out.String(), wantPermissionAnswer)
		}
	})

	t.Run("unreadable stdin still refuses", func(t *testing.T) {
		var out bytes.Buffer
		if rc := cmdGuardPermission(failingReader{}, &out); rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if out.String() != wantPermissionAnswer {
			t.Errorf("answered\n%q\nwant\n%q", out.String(), wantPermissionAnswer)
		}
	})

	t.Run("malformed JSON still refuses", func(t *testing.T) {
		var out bytes.Buffer
		cmdGuardPermission(strings.NewReader("{not json at all"), &out)
		if out.String() != wantPermissionAnswer {
			t.Errorf("answered\n%q\nwant\n%q", out.String(), wantPermissionAnswer)
		}
	})

	t.Run("empty stdin still refuses", func(t *testing.T) {
		var out bytes.Buffer
		cmdGuardPermission(strings.NewReader(""), &out)
		if out.String() != wantPermissionAnswer {
			t.Errorf("answered\n%q\nwant\n%q", out.String(), wantPermissionAnswer)
		}
	})

	t.Run("the refusal reaches the member as written, unescaped", func(t *testing.T) {
		var out bytes.Buffer
		cmdGuardPermission(strings.NewReader("{}"), &out)
		if !strings.Contains(out.String(), "rm -r <full path>") {
			t.Errorf("the decision document escaped the refusal instead of emitting it as written: %q", out.String())
		}
	})

	// cli/ocwarden/spawn.go writes the string "ocagent guard-permission" into
	// every member's settings.json, so the spelling on the command line is part
	// of the contract, not an internal detail.
	t.Run("the subcommand is reachable by the name settings.json wires up", func(t *testing.T) {
		var out bytes.Buffer
		if rc := realMain([]string{"guard-permission"}, testEnv(nil), strings.NewReader("{}"), &out); rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
		if out.String() != wantPermissionAnswer {
			t.Errorf("`ocagent guard-permission` answered\n%q\nwant\n%q", out.String(), wantPermissionAnswer)
		}
	})
}
