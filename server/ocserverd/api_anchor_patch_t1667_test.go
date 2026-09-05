package main

// api_anchor_patch_t1667_test.go — T-1667: anchor-addressed patch of the two
// write paths that had none, a step's working note (MCP patch_step_note) and a
// task manual's SOP (MCP patch_task_sop).
//
// Why they exist: both fields had exactly ONE write face and it was a whole-doc
// replace, so a caller working from a stale copy silently deletes whatever a
// second writer added in between — and because the stale copy is typically the
// LONGER one, not even the shrink guard fires. The loss lands with zero signal.
// These tests therefore lead with the concurrency shape, not with token cost.
//
// ApplyDocEdits is the SHARED engine (generic over the doc text), so the
// anchor/append/atomicity semantics are byte-identical to patch_lessons and
// patch_task_learnings; these tests re-pin them on the two NEW documents, plus
// the guards that are specific to each (the note's rune ceiling, the SOP's cap)
// and the compatibility of the wholesale faces they sit beside.
//
// EVERY test asserts on a READ-BACK of the stored document, never the status
// code alone — a wipe/mis-splice that returned 2xx would otherwise slip through.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// ── step note ────────────────────────────────────────────────────────────────

// patchStepNote drives the handler directly (like writeStepNote) and returns
// the status plus the decoded JSON body.
func patchStepNote(t *testing.T, api *apiServer, taskID, stepID, caller string, body any) (int, map[string]any) {
	t.Helper()
	return patchStepNoteAs(t, api, taskID, stepID, caller, "agent", body)
}

// patchStepNoteAs is patchStepNote with the caller's SCOPE spelled out, for the
// admin-capability path (the executor half of the gate needs no scope).
func patchStepNoteAs(t *testing.T, api *apiServer, taskID, stepID, sub, scope string, body any) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandlePatchTaskStepNoteApiTasksTaskIdStepsStepIdNotePatchPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/steps/"+stepID+"/note/patch",
			body, sub, scope),
		taskID, stepID)
	var data map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &data)
	}
	return rec.Code, data
}

// seedStepWithNote mints a task with one step carrying the given note, written
// through the real wholesale face, and returns the task and step ids.
func seedStepWithNote(t *testing.T, api *apiServer, note string) (string, string) {
	t.Helper()
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "lane", "dod": "d1"},
	})
	stepID := view.Steps[0].ID
	if note != "" {
		if rec := writeStepNote(t, api, task.ID, stepID, "m-exec", note); rec.Code != http.StatusOK {
			t.Fatalf("seed step note: %d %s", rec.Code, rec.Body.String())
		}
	}
	return task.ID, stepID
}

// TestPatchStepNoteUniqueAnchorReplace: a unique-anchor replace lands
// splice-precise, everything around it survives verbatim, and the receipt's
// note/size_chars/sha256/applied_edits describe the RESULTING note.
func TestPatchStepNoteUniqueAnchorReplace(t *testing.T) {
	api := newTasksTestServer(t)
	taskID, stepID := seedStepWithNote(t, api,
		"做到哪：conformance 跑到第三關\n下一步：接前端 i18n\n風險：無")

	status, data := patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit("下一步：接前端 i18n", "下一步：接 auth matrix")},
	})
	if status != http.StatusOK {
		t.Fatalf("unique-anchor patch must land, got %d: %v", status, data)
	}

	want := "做到哪：conformance 跑到第三關\n下一步：接 auth matrix\n風險：無"
	if got := readStepNote(t, api, taskID, stepID); got != want {
		t.Fatalf("patched note mismatch:\n got: %q\nwant: %q", got, want)
	}
	// T-91 removed the `note` echo from this receipt: the caller has the text it
	// spliced, and the sha256 below is the cheap way to confirm the SPLICE
	// landed where it thought. Pinned as an ABSENCE, not merely stopped being
	// pinned — a quiet restoration of the echo is the regression this reshape
	// exists to prevent.
	if _, present := data["note"]; present {
		t.Fatalf("the patch receipt must not carry `note` any more: %v", data["note"])
	}
	sum := sha256.Sum256([]byte(want))
	if got, _ := data["sha256"].(string); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 anchor mismatch: %v", data["sha256"])
	}
	if got, _ := data["size_chars"].(float64); int(got) != utf8.RuneCountInString(want) {
		t.Fatalf("size_chars anchor mismatch: got %v want %d", data["size_chars"], utf8.RuneCountInString(want))
	}
	if got, _ := data["cap_chars"].(float64); int(got) != chatBodyMaxChars {
		t.Fatalf("cap_chars must quote the ceiling the write was judged against: %v", data["cap_chars"])
	}
	if got, _ := data["applied_edits"].(float64); int(got) != 1 {
		t.Fatalf("applied_edits mismatch: %v", data["applied_edits"])
	}
}

// TestPatchStepNoteAppendWithEmptyOld: an empty old appends, joined with a
// single newline when the note does not already end in one — including onto a
// step whose note is still blank.
func TestPatchStepNoteAppendWithEmptyOld(t *testing.T) {
	api := newTasksTestServer(t)
	taskID, stepID := seedStepWithNote(t, api, "第一段")

	if status, data := patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit("", "第二段")},
	}); status != http.StatusOK {
		t.Fatalf("append must land, got %d: %v", status, data)
	}
	if got := readStepNote(t, api, taskID, stepID); got != "第一段\n第二段" {
		t.Fatalf("append must join with one newline, got %q", got)
	}

	blankTask, blankStep := seedStepWithNote(t, api, "")
	if status, data := patchStepNote(t, api, blankTask, blankStep, "m-exec", map[string]any{
		"edits": []any{edit("", "第一次寫")},
	}); status != http.StatusOK {
		t.Fatalf("append onto a blank note must land, got %d: %v", status, data)
	}
	if got := readStepNote(t, api, blankTask, blankStep); got != "第一次寫" {
		t.Fatalf("append onto a blank note must not prepend a newline, got %q", got)
	}
}

// TestPatchStepNoteStaleAnchorIsRefusedWithZeroWrites is the concurrency shape
// this endpoint exists for: a second writer amends the note, and the first
// writer's patch — anchored on the text it read before that landed — is
// refused. The batch that DOES anchor correctly then edits only its own
// section, leaving the concurrent writer's text byte-for-byte intact. The
// wholesale face has no way to express either half.
func TestPatchStepNoteStaleAnchorIsRefusedWithZeroWrites(t *testing.T) {
	api := newTasksTestServer(t)
	const original = "做到哪：改到一半\n下一步：待補"
	taskID, stepID := seedStepWithNote(t, api, original)

	// A second writer lands first, rewording the line the stale reader is
	// holding and adding a section of its own.
	const concurrent = "做到哪：改完了\n下一步：待補\n交接：帳號密碼在 1password"
	if rec := writeStepNote(t, api, taskID, stepID, "m-exec", concurrent); rec.Code != http.StatusOK {
		t.Fatalf("concurrent write: %d %s", rec.Code, rec.Body.String())
	}

	// The stale writer anchors on what it read. Nothing may be written.
	status, data := patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{
			edit("下一步：待補", "下一步：接 auth matrix"),
			edit("做到哪：改到一半", "做到哪：全部改完"),
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("a stale anchor must be a flat 400, got %d: %v", status, data)
	}
	msg := errMessage(data)
	if !strings.Contains(msg, "edits[1]") {
		t.Fatalf("error must name the failing edit index, got: %q", msg)
	}
	if !strings.Contains(msg, "get_task") {
		t.Fatalf("error must name the tool to re-read with, got: %q", msg)
	}
	if got := readStepNote(t, api, taskID, stepID); got != concurrent {
		t.Fatalf("refused batch leaked a write — atomicity broken:\n got: %q\nwant: %q", got, concurrent)
	}

	// Re-anchored on the current text, the patch touches only its own line and
	// the concurrent writer's section survives verbatim.
	if status, data = patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit("下一步：待補", "下一步：接 auth matrix")},
	}); status != http.StatusOK {
		t.Fatalf("re-anchored patch must land, got %d: %v", status, data)
	}
	want := "做到哪：改完了\n下一步：接 auth matrix\n交接：帳號密碼在 1password"
	if got := readStepNote(t, api, taskID, stepID); got != want {
		t.Fatalf("patch must touch only its own section:\n got: %q\nwant: %q", got, want)
	}
}

// TestPatchStepNoteAmbiguousAnchorRejected: an old matching >1 locations is
// ambiguous — the other way a concurrent write can move the ground — and
// rejects the whole batch.
func TestPatchStepNoteAmbiguousAnchorRejected(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "重複\n重複"
	taskID, stepID := seedStepWithNote(t, api, seeded)

	status, data := patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit("重複", "改過")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("ambiguous anchor must 400, got %d: %v", status, data)
	}
	if got := readStepNote(t, api, taskID, stepID); got != seeded {
		t.Fatalf("note must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchStepNoteMalformedBatchesAreRefused: an empty edits array, an edit
// with neither old nor new, and the {old_text,new_text} incident shape are all
// 422 with zero writes — the same three refusals the older patch faces give.
func TestPatchStepNoteMalformedBatchesAreRefused(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "做到哪：一半"
	taskID, stepID := seedStepWithNote(t, api, seeded)

	for _, tc := range []struct {
		name string
		body any
	}{
		{"empty edits", map[string]any{"edits": []any{}}},
		{"neither old nor new", map[string]any{"edits": []any{map[string]any{}}}},
		{"unknown edit keys", map[string]any{
			"edits": []any{map[string]any{"old_text": "做到哪：一半", "new_text": "做到哪：全好"}},
		}},
		{"unknown key alongside a valid edit", map[string]any{
			"edits": []any{map[string]any{"old": "做到哪：一半", "new": "做到哪：全好", "old_text": "stray"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, data := patchStepNote(t, api, taskID, stepID, "m-exec", tc.body)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("must be refused (422), got %d: %v", status, data)
			}
			if got := readStepNote(t, api, taskID, stepID); got != seeded {
				t.Fatalf("note must be untouched:\n got: %q\nwant: %q", got, seeded)
			}
		})
	}
}

// TestPatchStepNoteWipeNeedsAllowShrink: a patch that empties the note is
// refused without allow_shrink; the flag lets it through on purpose.
func TestPatchStepNoteWipeNeedsAllowShrink(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "交接內容,值得保護,不該被一個手滑的 patch 清掉"
	taskID, stepID := seedStepWithNote(t, api, seeded)

	status, data := patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit(seeded, "")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("unguarded wipe must be refused (400), got %d: %v", status, data)
	}
	if got := readStepNote(t, api, taskID, stepID); got != seeded {
		t.Fatalf("note must survive a refused wipe:\n got: %q\nwant: %q", got, seeded)
	}

	if status, data = patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit(seeded, "")}, "allow_shrink": true,
	}); status != http.StatusOK {
		t.Fatalf("allow_shrink wipe must land, got %d: %v", status, data)
	}
	if got := readStepNote(t, api, taskID, stepID); got != "" {
		t.Fatalf("allow_shrink wipe must empty the note, got %q", got)
	}
}

// TestPatchStepNoteResultIsHeldToTheSameCeiling: the rune limit is judged on
// the RESULT of the patch, so the patch face is not an uncapped door onto a
// capped field. The refused write leaves the note verbatim.
func TestPatchStepNoteResultIsHeldToTheSameCeiling(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "做到哪：一半"
	taskID, stepID := seedStepWithNote(t, api, seeded)

	oversize := strings.Repeat("字", chatBodyMaxChars)
	status, data := patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit("", oversize)},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("a patch whose RESULT is over the limit must 400, got %d: %v", status, data)
	}
	if got := readStepNote(t, api, taskID, stepID); got != seeded {
		t.Fatalf("note must be untouched:\n got: %q\nwant: %q", got, seeded)
	}

	// Positive control: the same shape one rune under the ceiling lands, so the
	// refusal above is the ceiling talking and not the append branch failing.
	fits := strings.Repeat("字", chatBodyMaxChars-utf8.RuneCountInString(seeded)-1)
	if status, data = patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit("", fits)},
	}); status != http.StatusOK {
		t.Fatalf("an append that fits must land, got %d: %v", status, data)
	}
}

// TestPatchStepNoteSharesTheWholesaleGuards: the patch face is reachable by
// exactly the callers and task states the wholesale face is — a foreign agent
// is a 403, an unknown task or step a 404, and a closed task a 409 — and every
// refusal leaves the stored note verbatim.
func TestPatchStepNoteSharesTheWholesaleGuards(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "做到哪：一半"
	taskID, stepID := seedStepWithNote(t, api, seeded)
	body := map[string]any{"edits": []any{edit("做到哪：一半", "做到哪：全好")}}

	if status, data := patchStepNote(t, api, taskID, stepID, "m-other", body); status != http.StatusForbidden {
		t.Fatalf("a non-executor must be 403, got %d: %v", status, data)
	}
	// Existence and authz are settled BEFORE the edits are judged, the order the
	// older patch faces take: a caller pointed at a lane it may not touch hears
	// that, not a verdict on its edits.
	if status, data := patchStepNote(t, api, taskID, stepID, "m-other",
		map[string]any{"edits": []any{map[string]any{}}}); status != http.StatusForbidden {
		t.Fatalf("a non-executor with malformed edits must still be 403, got %d: %v", status, data)
	}
	if status, _ := patchStepNote(t, api, "T-nope", stepID, "m-exec", body); status != http.StatusNotFound {
		t.Fatalf("an unknown task must be 404, got %d", status)
	}
	if status, _ := patchStepNote(t, api, taskID, "step-nope", "m-exec", body); status != http.StatusNotFound {
		t.Fatalf("an unknown step must be 404, got %d", status)
	}
	if got := readStepNote(t, api, taskID, stepID); got != seeded {
		t.Fatalf("no refusal may write:\n got: %q\nwant: %q", got, seeded)
	}

	// Report the only step done — the task auto-closes — and the note freezes
	// for both faces alike.
	if rec := reportStepStatus(t, api, taskID, stepID, "m-exec", "in_progress", ""); rec.Code != http.StatusOK {
		t.Fatalf("start lane: %d %s", rec.Code, rec.Body.String())
	}
	if rec := reportStepStatus(t, api, taskID, stepID, "m-exec", "done", ""); rec.Code != http.StatusOK {
		t.Fatalf("finish lane: %d %s", rec.Code, rec.Body.String())
	}
	if status, data := patchStepNote(t, api, taskID, stepID, "m-exec", body); status != http.StatusConflict {
		t.Fatalf("a closed task must be 409, got %d: %v", status, data)
	}
	if rec := writeStepNote(t, api, taskID, stepID, "m-exec", "anything"); rec.Code != http.StatusConflict {
		t.Fatalf("the wholesale face must 409 on a closed task too, got %d", rec.Code)
	}
	if got := readStepNote(t, api, taskID, stepID); got != seeded {
		t.Fatalf("a closed task's note must be frozen:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestUpdateStepNoteStillReplacesWholesale is the compatibility pin: existing
// callers of the wholesale face keep the exact semantics they had — the body's
// note replaces whatever was there, "" clears it, no edits vocabulary involved.
func TestUpdateStepNoteStillReplacesWholesale(t *testing.T) {
	api := newTasksTestServer(t)
	taskID, stepID := seedStepWithNote(t, api, "第一版：這一整段都會被換掉")

	rec := writeStepNote(t, api, taskID, stepID, "m-exec", "第二版：換掉了")
	if rec.Code != http.StatusOK {
		t.Fatalf("wholesale replace must land: %d %s", rec.Code, rec.Body.String())
	}
	// T-91: the receipt no longer ECHOES the note — it HASHES it. The claim
	// this line makes is the same one it always made (the write is verifiable
	// at the write, without a second round trip); what changed is the price:
	// 64 characters instead of the document. The reason the old echo was
	// defensible — "a step note is bounded" — was never a reason it was
	// USEFUL, since the caller had just sent the text.
	if got := decodeBody[taskStepNoteReceiptDTO](t, rec).Sha256; got != receiptSha256("第二版：換掉了") {
		t.Fatalf("wholesale receipt sha256 = %q, want the hash of the stored note", got)
	}
	if got := readStepNote(t, api, taskID, stepID); got != "第二版：換掉了" {
		t.Fatalf("wholesale replace mismatch, got %q", got)
	}

	if rec = writeStepNote(t, api, taskID, stepID, "m-exec", ""); rec.Code != http.StatusOK {
		t.Fatalf("clearing must land: %d %s", rec.Code, rec.Body.String())
	}
	if got := readStepNote(t, api, taskID, stepID); got != "" {
		t.Fatalf(`"" must clear the note, got %q`, got)
	}
}

// ── task manual SOP ──────────────────────────────────────────────────────────

// patchSop drives the handler directly and returns the status plus the decoded
// JSON body.
func patchSop(t *testing.T, api *apiServer, typeKey string, body any) (int, map[string]any) {
	t.Helper()
	return patchSopAs(t, api, typeKey, "m-exec", "agent", body)
}

// patchSopAs is patchSop with the caller spelled out, for the callers ABOVE the
// route's agent floor.
func patchSopAs(t *testing.T, api *apiServer, typeKey, sub, scope string, body any) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandlePatchTaskSopApiTaskManualsTypeKeySopPatchPost(rec, taskReq(t, "POST",
		"/api/task-manuals/"+typeKey+"/sop/patch", body, sub, scope), typeKey)
	var data map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &data)
	}
	return rec.Code, data
}

// updateSopWholesale writes sop_md through update_task_manual — the pre-existing
// write face — and returns the recorder so callers assert their own status.
func updateSopWholesale(t *testing.T, api *apiServer, typeKey, sop string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, "POST",
		"/api/task-manuals/"+typeKey, map[string]any{"sop_md": sop}, "m-exec", "agent"), typeKey)
	return rec
}

// storedSop reads sop_md back through the real read path (get_task_manual), not
// the DAL — the same path the caller re-reads with after a refusal.
func storedSop(t *testing.T, api *apiServer, typeKey string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskManualApiTaskManualsTypeKeyGet(rec, taskReq(t, "GET",
		"/api/task-manuals/"+typeKey, nil, "m-exec", "agent"), typeKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("get manual: %d %s", rec.Code, rec.Body.String())
	}
	var dto struct {
		SopMd string `json:"sop_md"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode manual: %v", err)
	}
	return dto.SopMd
}

// seedManualWithSop mints a manual whose SOP is the given text, written through
// the real wholesale face.
func seedManualWithSop(t *testing.T, api *apiServer, sop string) string {
	t.Helper()
	key := seedManualWithLearnings(t, api, "")
	if rec := updateSopWholesale(t, api, key, sop); rec.Code != http.StatusOK {
		t.Fatalf("seed sop: %d %s", rec.Code, rec.Body.String())
	}
	return key
}

// TestPatchTaskSopUniqueAnchorReplace: a unique-anchor replace lands
// splice-precise and the receipt's size_chars/cap_chars/sha256/applied_edits
// describe the RESULTING SOP.
func TestPatchTaskSopUniqueAnchorReplace(t *testing.T) {
	api := newTasksTestServer(t)
	key := seedManualWithSop(t, api,
		"## 步驟\n1. 收單\n2. 舊做法：人工核對\n3. 出貨\n")

	status, data := patchSop(t, api, key, map[string]any{
		"edits": []any{edit("2. 舊做法：人工核對", "2. 新做法：自動核對")},
	})
	if status != http.StatusOK {
		t.Fatalf("unique-anchor patch must land, got %d: %v", status, data)
	}

	want := "## 步驟\n1. 收單\n2. 新做法：自動核對\n3. 出貨\n"
	if got := storedSop(t, api, key); got != want {
		t.Fatalf("patched sop mismatch:\n got: %q\nwant: %q", got, want)
	}
	sum := sha256.Sum256([]byte(want))
	if got, _ := data["sha256"].(string); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 anchor mismatch: %v", data["sha256"])
	}
	if got, _ := data["size_chars"].(float64); int(got) != utf8.RuneCountInString(want) {
		t.Fatalf("size_chars anchor mismatch: got %v want %d", data["size_chars"], utf8.RuneCountInString(want))
	}
	if got, _ := data["cap_chars"].(float64); int(got) != api.manualSopCap() {
		t.Fatalf("cap_chars must quote the sop cap the write was judged against: %v", data["cap_chars"])
	}
	if got, _ := data["applied_edits"].(float64); int(got) != 1 {
		t.Fatalf("applied_edits mismatch: %v", data["applied_edits"])
	}
	if got, _ := data["type_key"].(string); got != key {
		t.Fatalf("receipt type_key mismatch: got %v want %s", data["type_key"], key)
	}
}

// TestPatchTaskSopLeavesTheLearningsAlone: the two documents a manual carries
// answer to two write faces and one must never move the other.
func TestPatchTaskSopLeavesTheLearningsAlone(t *testing.T) {
	api := newTasksTestServer(t)
	const learnings = "環境 Q&A：DSN 在 oc.toml"
	key := seedManualWithLearnings(t, api, learnings)
	if rec := updateSopWholesale(t, api, key, "## 步驟\n1. 收單\n"); rec.Code != http.StatusOK {
		t.Fatalf("seed sop: %d %s", rec.Code, rec.Body.String())
	}

	if status, data := patchSop(t, api, key, map[string]any{
		"edits": []any{edit("1. 收單", "1. 收單並建檔")},
	}); status != http.StatusOK {
		t.Fatalf("patch must land, got %d: %v", status, data)
	}
	if got := storedLearnings(t, api, key); got != learnings {
		t.Fatalf("an sop patch must not touch the learnings:\n got: %q\nwant: %q", got, learnings)
	}
}

// TestPatchTaskSopStaleAnchorIsRefusedWithZeroWrites is the concurrency shape
// this endpoint exists for: a second author revises a section, and the first
// author's patch — anchored on the text it read before that landed — is
// refused instead of silently deleting the revision. The re-anchored batch then
// edits only its own section.
func TestPatchTaskSopStaleAnchorIsRefusedWithZeroWrites(t *testing.T) {
	api := newTasksTestServer(t)
	const original = "## 步驟\n1. 收單\n2. 核對\n"
	key := seedManualWithSop(t, api, original)

	// A second author lands first: reworded step 2, plus a new section.
	const concurrent = "## 步驟\n1. 收單\n2. 自動核對\n\n## 例外處理\n找銀月\n"
	if rec := updateSopWholesale(t, api, key, concurrent); rec.Code != http.StatusOK {
		t.Fatalf("concurrent write: %d %s", rec.Code, rec.Body.String())
	}

	status, data := patchSop(t, api, key, map[string]any{
		"edits": []any{
			edit("1. 收單", "1. 收單並建檔"),
			edit("2. 核對", "2. 核對兩次"),
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("a stale anchor must be a flat 400, got %d: %v", status, data)
	}
	msg := errMessage(data)
	if !strings.Contains(msg, "edits[1]") {
		t.Fatalf("error must name the failing edit index, got: %q", msg)
	}
	if !strings.Contains(msg, "get_task_manual") {
		t.Fatalf("error must name the tool to re-read with, got: %q", msg)
	}
	if got := storedSop(t, api, key); got != concurrent {
		t.Fatalf("refused batch leaked a write — atomicity broken:\n got: %q\nwant: %q", got, concurrent)
	}

	if status, data = patchSop(t, api, key, map[string]any{
		"edits": []any{edit("1. 收單\n", "1. 收單並建檔\n")},
	}); status != http.StatusOK {
		t.Fatalf("re-anchored patch must land, got %d: %v", status, data)
	}
	want := "## 步驟\n1. 收單並建檔\n2. 自動核對\n\n## 例外處理\n找銀月\n"
	if got := storedSop(t, api, key); got != want {
		t.Fatalf("patch must touch only its own section:\n got: %q\nwant: %q", got, want)
	}
}

// TestPatchTaskSopMultiEditAtomicity: a batch whose later edit misses must 400
// with ZERO writes — no earlier edit's splice may survive — and a batch of
// sequential hits sees each edit's result.
func TestPatchTaskSopMultiEditAtomicity(t *testing.T) {
	api := newTasksTestServer(t)
	const base = "alpha\nbeta\ngamma\n"
	key := seedManualWithSop(t, api, base)

	status, data := patchSop(t, api, key, map[string]any{
		"edits": []any{edit("alpha", "ALPHA"), edit("never-there", "x")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("batch with a missing anchor must 400, got %d: %v", status, data)
	}
	if msg := errMessage(data); !strings.Contains(msg, "edits[1]") {
		t.Fatalf("error must name the failing edit index, got: %q", msg)
	}
	if got := storedSop(t, api, key); got != base {
		t.Fatalf("partial write leaked — atomicity broken:\n got: %q\nwant: %q", got, base)
	}

	if status, _ = patchSop(t, api, key, map[string]any{
		"edits": []any{edit("beta", "beta prime"), edit("beta prime", "beta prime indeed")},
	}); status != http.StatusOK {
		t.Fatalf("sequential edits must land, got %d", status)
	}
	if got := storedSop(t, api, key); !strings.Contains(got, "beta prime indeed") {
		t.Fatalf("sequential edit result missing: %q", got)
	}
}

// TestPatchTaskSopAmbiguousAnchorRejected: an old matching >1 locations is
// ambiguous — the shape a concurrent write leaves behind when it DUPLICATES the
// anchor rather than moving it — and rejects the whole batch with zero writes.
func TestPatchTaskSopAmbiguousAnchorRejected(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "## 步驟\n1. 收單\n\n## 例外處理\n1. 收單\n"
	key := seedManualWithSop(t, api, seeded)

	status, data := patchSop(t, api, key, map[string]any{
		"edits": []any{edit("1. 收單", "1. 收單並建檔")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("ambiguous anchor must 400, got %d: %v", status, data)
	}
	if msg := errMessage(data); !strings.Contains(msg, "edits[0]") {
		t.Fatalf("error must name the failing edit index, got: %q", msg)
	}
	if got := storedSop(t, api, key); got != seeded {
		t.Fatalf("sop must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchTaskSopMalformedBatchesAreRefused: an empty edits array, an edit with
// neither old nor new, and the {old_text,new_text} incident shape are all 422
// with zero writes.
func TestPatchTaskSopMalformedBatchesAreRefused(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "## 步驟\n1. 收單\n"
	key := seedManualWithSop(t, api, seeded)

	for _, tc := range []struct {
		name string
		body any
	}{
		{"empty edits", map[string]any{"edits": []any{}}},
		{"neither old nor new", map[string]any{"edits": []any{map[string]any{}}}},
		{"unknown edit keys", map[string]any{
			"edits": []any{map[string]any{"old_text": "1. 收單", "new_text": "1. 收單並建檔"}},
		}},
		{"unknown key alongside a valid edit", map[string]any{
			"edits": []any{map[string]any{"old": "1. 收單", "new": "1. 收單並建檔", "old_text": "stray"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, data := patchSop(t, api, key, tc.body)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("must be refused (422), got %d: %v", status, data)
			}
			if got := storedSop(t, api, key); got != seeded {
				t.Fatalf("sop must be untouched:\n got: %q\nwant: %q", got, seeded)
			}
		})
	}
}

// TestPatchTaskSopWipeNeedsAllowShrink: a patch that empties the SOP is refused
// without allow_shrink — a guard the wholesale face never had.
func TestPatchTaskSopWipeNeedsAllowShrink(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "## 步驟\n1. 收單\n2. 核對\n3. 出貨\n每一行都值得保護,不該被一個手滑的 patch 清掉\n"
	key := seedManualWithSop(t, api, seeded)

	status, data := patchSop(t, api, key, map[string]any{
		"edits": []any{edit(seeded, "")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("unguarded wipe must be refused (400), got %d: %v", status, data)
	}
	if got := storedSop(t, api, key); got != seeded {
		t.Fatalf("sop must survive a refused wipe:\n got: %q\nwant: %q", got, seeded)
	}

	if status, data = patchSop(t, api, key, map[string]any{
		"edits": []any{edit(seeded, "")}, "allow_shrink": true,
	}); status != http.StatusOK {
		t.Fatalf("allow_shrink wipe must land, got %d: %v", status, data)
	}
	if got := storedSop(t, api, key); got != "" {
		t.Fatalf("allow_shrink wipe must empty the sop, got %q", got)
	}
}

// TestPatchTaskSopResultIsHeldToTheCap: the sop_md cap is judged on the RESULT
// of the patch and allow_shrink is not a bypass, so the patch face is not an
// uncapped door onto a capped document.
func TestPatchTaskSopResultIsHeldToTheCap(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "## 步驟\n1. 收單\n"
	key := seedManualWithSop(t, api, seeded)

	cap := api.manualSopCap()
	oversize := strings.Repeat("字", cap)
	body := map[string]any{"edits": []any{edit("", oversize)}, "allow_shrink": true}
	status, data := patchSop(t, api, key, body)
	if status != http.StatusBadRequest {
		t.Fatalf("a patch whose RESULT is over the cap must 400, got %d: %v", status, data)
	}
	if got := storedSop(t, api, key); got != seeded {
		t.Fatalf("sop must be untouched:\n got: %q\nwant: %q", got, seeded)
	}

	// Positive control: the same shape one rune under the cap lands, so the
	// refusal above is the cap talking.
	fits := strings.Repeat("字", cap-utf8.RuneCountInString(seeded)-1)
	if status, data = patchSop(t, api, key, map[string]any{
		"edits": []any{edit("", fits)},
	}); status != http.StatusOK {
		t.Fatalf("an append that fits must land, got %d: %v", status, data)
	}
}

// TestPatchTaskSopUnknownTypeIs404: a patch against a type that does not exist
// is a 404, not a silent create — INCLUDING when the edits are themselves
// malformed. The target is resolved before its content is judged, the order
// patch_task_learnings takes, so the two faces cannot answer the same request
// with two different codes.
func TestPatchTaskSopUnknownTypeIs404(t *testing.T) {
	api := newTasksTestServer(t)
	for _, tc := range []struct {
		name string
		body any
	}{
		{"well-formed edits", map[string]any{"edits": []any{edit("anything", "x")}}},
		{"neither old nor new", map[string]any{"edits": []any{map[string]any{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, data := patchSop(t, api, "tm-does-not-exist", tc.body)
			if status != http.StatusNotFound {
				t.Fatalf("patch of an unknown type must 404, got %d: %v", status, data)
			}
		})
	}
}

// TestPatchTaskSopRetainsAVersion: the patch face writes through the same
// history seam as the wholesale one, so a patched SOP is still restorable.
func TestPatchTaskSopRetainsAVersion(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "## 步驟\n1. 收單\n"
	key := seedManualWithSop(t, api, seeded)

	if status, data := patchSop(t, api, key, map[string]any{
		"edits": []any{edit("1. 收單", "1. 收單並建檔")},
	}); status != http.StatusOK {
		t.Fatalf("patch must land, got %d: %v", status, data)
	}

	// T-1170 split the history surface: the LISTING carries metadata only, and
	// the body is fetched per version. So asserting on the listing's text would
	// now pass for the wrong reason on an empty document and fail for the right
	// one here — read the version back and compare the CONTENT.
	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, "GET",
		"/api/document-history/task_manual_sop/"+key, nil, "m-exec", "agent"),
		"task_manual_sop", key)
	if rec.Code != http.StatusOK {
		t.Fatalf("list history: %d %s", rec.Code, rec.Body.String())
	}
	var rows []DocumentHistoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode history listing: %v (%s)", err, rec.Body.String())
	}
	if len(rows) != 1 {
		t.Fatalf("the pre-patch sop must be retained as exactly one version, got %+v", rows)
	}
	hydrated := hydrateHistory(t, api, "task_manual_sop", key, "m-exec", "agent", rows)
	if got := hydrated[0].Content["sop_md"]; got != seeded {
		t.Fatalf("the retained version must be the PRE-patch sop verbatim, got %q", got)
	}
}

// TestPatchTaskSopAcceptsCallersAboveTheAgentFloor: manual CONTENT is
// agent-editable, so the route's floor is principalAgent and the handler adds no
// caller gate of its own. Nothing pinned that: an admin-capable caller had no
// coverage on this face at all, and an executor-style check quietly copied in
// from the step-note twin would refuse every owner and admin edit without any
// test noticing. Asserted on both faces onto sop_md, so they cannot diverge.
func TestPatchTaskSopAcceptsCallersAboveTheAgentFloor(t *testing.T) {
	api := newTasksTestServer(t)
	key := seedManualWithSop(t, api, "## 步驟\n1. 收單\n2. 出貨\n")

	status, data := patchSopAs(t, api, key, wireOwnerID, "owner", map[string]any{
		"edits": []any{edit("2. 出貨", "2. 出貨（owner 改的）")},
	})
	if status != http.StatusOK {
		t.Fatalf("owner patch: %d %v, want 200", status, data)
	}
	if got := storedSop(t, api, key); got != "## 步驟\n1. 收單\n2. 出貨（owner 改的）\n" {
		t.Fatalf("sop after owner patch = %q, want the splice to have landed", got)
	}

	rec := httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, "POST",
		"/api/task-manuals/"+key, map[string]any{"sop_md": "## 整份換掉\n"},
		wireOwnerID, "owner"), key)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner wholesale write: %d %s, want 200", rec.Code, rec.Body.String())
	}
	if got := storedSop(t, api, key); got != "## 整份換掉\n" {
		t.Fatalf("sop after owner wholesale write = %q", got)
	}
}

// TestUpdateTaskManualStillReplacesSopWholesale is the compatibility pin:
// existing callers of update_task_manual keep the exact semantics they had —
// sop_md is a whole-doc replace, other fields stay untouched when omitted.
func TestUpdateTaskManualStillReplacesSopWholesale(t *testing.T) {
	api := newTasksTestServer(t)
	const learnings = "環境 Q&A：DSN 在 oc.toml"
	key := seedManualWithLearnings(t, api, learnings)
	if rec := updateSopWholesale(t, api, key, "## 第一版\n整段都會被換掉\n"); rec.Code != http.StatusOK {
		t.Fatalf("first wholesale write: %d %s", rec.Code, rec.Body.String())
	}

	if rec := updateSopWholesale(t, api, key, "## 第二版\n換掉了\n"); rec.Code != http.StatusOK {
		t.Fatalf("second wholesale write: %d %s", rec.Code, rec.Body.String())
	}
	if got := storedSop(t, api, key); got != "## 第二版\n換掉了\n" {
		t.Fatalf("wholesale replace mismatch, got %q", got)
	}
	if got := storedLearnings(t, api, key); got != learnings {
		t.Fatalf("an omitted field must not change:\n got: %q\nwant: %q", got, learnings)
	}
}
