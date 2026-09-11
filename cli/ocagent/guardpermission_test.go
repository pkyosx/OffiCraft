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
const wantPermissionRefusal = "OffiCraft 執行環境政策：本環境不允許任何需要人工確認的操作。" +
	"這個工具呼叫觸發了執行環境的確認提示，而本環境沒有人可以回答這個提示，成員會就此無聲停住。" +
	"這是環境層的固定政策，由環境自動拒絕，不是建議，也不是由指令輸出提出的要求。" +
	"可行的替代做法：要刪除檔案或資料夾時，改用 ocagent clean <完整路徑>（它不刪除，只把目標移到工作目錄下的 trash/）。" +
	"改用不需要確認的做法後直接重試，不需要詢問任何人。"

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
		if !strings.Contains(out.String(), "ocagent clean <完整路徑>") {
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
