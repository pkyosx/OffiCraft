package main

// api_write_guards_2d99_test.go — T-2d99: the write-type tools that reported
// success while silently doing nothing (or worse, silently wiping the doc).
//
// The bug was three defects stacked:
//   1. no DisallowUnknownFields — an unrecognised JSON key was dropped in
//      silence, so a typo'd/renamed field never reached the DTO;
//   2. strOrEmpty folded nil (absent) and "" (explicitly empty) into the same
//      value, so "the field never parsed" was indistinguishable from "the
//      caller asked for empty";
//   3. an empty Old meant APPEND rather than "refuse", so a batch whose keys
//      never parsed impersonated a legitimate append of nothing.
//
// The realised loss: write_task_learnings takes `text`, but update_task_manual
// calls the SAME document `learnings`. A caller sent `learnings:` → unknown key
// dropped → body.Text nil → "" → the entire manual's learnings wiped, and the
// 200 response echoed learnings: "" as though that had been asked for.
//
// EVERY test here asserts on a READ-BACK of the stored document, never on the
// status code alone — a test that only checks "it returned 2xx" would repeat
// the exact mistake these fixes exist to correct.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ── write_task_learnings ─────────────────────────────────────────────────────

// seedManualWithLearnings creates a manual carrying a known learnings doc and
// returns its minted type key.
func seedManualWithLearnings(t *testing.T, api *apiServer, learnings string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateTaskManualApiTaskManualsPost(rec, taskReq(t, "POST",
		"/api/task-manuals", map[string]any{"display_name": "T-2d99 manual"},
		"m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create manual: %d %s", rec.Code, rec.Body.String())
	}
	var dto struct {
		TypeKey string `json:"type_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("create response: %v", err)
	}
	m, err := api.dal.GetTaskManual(dto.TypeKey)
	if err != nil || m == nil {
		t.Fatalf("readback minted manual: %+v %v", m, err)
	}
	m.Learnings = learnings
	if err := api.dal.PutTaskManual(*m); err != nil {
		t.Fatalf("seed learnings: %v", err)
	}
	return dto.TypeKey
}

func storedLearnings(t *testing.T, api *apiServer, typeKey string) string {
	t.Helper()
	m, err := api.dal.GetTaskManual(typeKey)
	if err != nil || m == nil {
		t.Fatalf("read manual %s: %+v %v", typeKey, m, err)
	}
	return m.Learnings
}

func writeLearnings(t *testing.T, api *apiServer, typeKey string, body any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost(rec, taskReq(t, "POST",
		"/api/task-manuals/"+typeKey+"/learnings", body, "m-exec", "agent"), typeKey)
	return rec
}

// TestWriteTaskLearningsRejectsTheLearningsKey reproduces the incident that
// actually destroyed a manual: the caller used update_task_manual's field name
// (`learnings`) on write_task_learnings (which takes `text`). Before the fix
// this answered 200 and left the doc empty. It must now refuse AND leave the
// stored doc byte-identical.
func TestWriteTaskLearningsRejectsTheLearningsKey(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "accumulated learnings that must survive a malformed write"
	key := seedManualWithLearnings(t, api, seeded)

	rec := writeLearnings(t, api, key, map[string]any{"learnings": "brand new doc"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the wrong-key write must be refused (422), got %d %s", rec.Code, rec.Body.String())
	}
	// The load-bearing assertion: the document itself, read back from storage.
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("learnings must be untouched by a refused write:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestWriteTaskLearningsMissingTextIsRefused: absent `text` must never be read
// as "the caller wants it empty".
func TestWriteTaskLearningsMissingTextIsRefused(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "learnings that a keyless request must not erase"
	key := seedManualWithLearnings(t, api, seeded)

	rec := writeLearnings(t, api, key, map[string]any{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing text must be refused (422), got %d %s", rec.Code, rec.Body.String())
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("learnings must survive a keyless write:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestWriteTaskLearningsHappyPathPersists: the fix must not have turned the
// tool into a no-op — a well-formed write still lands and reads back.
func TestWriteTaskLearningsHappyPathPersists(t *testing.T) {
	api := newTasksTestServer(t)
	key := seedManualWithLearnings(t, api, "old doc")

	const want = "the folded-in experience from this task"
	rec := writeLearnings(t, api, key, map[string]any{"text": want})
	if rec.Code != http.StatusOK {
		t.Fatalf("well-formed write must land, got %d %s", rec.Code, rec.Body.String())
	}
	if got := storedLearnings(t, api, key); got != want {
		t.Fatalf("write did not persist:\n got: %q\nwant: %q", got, want)
	}
}

// TestWriteTaskLearningsWipeNeedsAllowShrink: even a well-formed {"text": ""}
// must not silently erase an existing doc — and allow_shrink must still be
// able to do it on purpose.
func TestWriteTaskLearningsWipeNeedsAllowShrink(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "learnings worth protecting from an accidental blank write"
	key := seedManualWithLearnings(t, api, seeded)

	rec := writeLearnings(t, api, key, map[string]any{"text": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unguarded wipe must be refused (400), got %d %s", rec.Code, rec.Body.String())
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("learnings must survive a refused wipe:\n got: %q\nwant: %q", got, seeded)
	}

	// Explicit intent still works — the guard is a speed bump, not a wall.
	rec = writeLearnings(t, api, key, map[string]any{"text": "", "allow_shrink": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("allow_shrink wipe must land, got %d %s", rec.Code, rec.Body.String())
	}
	if got := storedLearnings(t, api, key); got != "" {
		t.Fatalf("allow_shrink wipe must empty the doc, got %q", got)
	}
}

// TestWriteTaskLearningsRejectsUnknownKeyAlongsideText isolates
// DisallowUnknownFields from the required-key check. The tests above would
// still pass on a lenient decoder, because dropping `learnings` also leaves
// `text` missing and the required check fires. Here `text` IS present, so the
// ONLY thing that can reject the request is the unknown-field guard — and it
// must, because a caller sending both names does not know which one wins.
func TestWriteTaskLearningsRejectsUnknownKeyAlongsideText(t *testing.T) {
	api := newTasksTestServer(t)
	const seeded = "learnings guarded by the unknown-field check alone"
	key := seedManualWithLearnings(t, api, seeded)

	rec := writeLearnings(t, api, key, map[string]any{
		"text": "which of us wins?", "learnings": "or me?",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown key alongside a valid text must be refused (422), got %d %s",
			rec.Code, rec.Body.String())
	}
	if got := storedLearnings(t, api, key); got != seeded {
		t.Fatalf("learnings must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// ── patch_lessons ────────────────────────────────────────────────────────────

// TestPatchLessonsRejectsUnknownEditKeys pins the nested face of
// DisallowUnknownFields: the malformed edit shape from the incident report
// ({old_text, new_text}) used to be dropped key-by-key, leaving {nil, nil},
// which the empty-old APPEND branch turned into a perfect no-op behind a 200.
func TestPatchLessonsRejectsUnknownEditKeys(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	const seeded = "line one\nline two\n"
	seedLessonsOverlay(t, dal, "assistant", "general", seeded)

	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old_text":"line two","new_text":"line two changed"}]}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown edit keys must be refused (422), got %d: %v", status, data)
	}
	if got := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchLessonsRejectsUnknownKeyAlongsideValidEdit isolates the NESTED
// unknown-field guard. The test above would still pass on a lenient decoder
// (dropping both keys leaves {nil,nil}, which the neither-old-nor-new check
// catches). Here the edit carries a perfectly valid old/new PLUS a stray key,
// so only DisallowUnknownFields descending into the array element can reject
// it — and it must, since the stray key is evidence the caller's model of the
// shape is wrong.
func TestPatchLessonsRejectsUnknownKeyAlongsideValidEdit(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	const seeded = "line one\nline two\n"
	seedLessonsOverlay(t, dal, "assistant", "general", seeded)

	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"line two","new":"line two changed","old_text":"stray"}]}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("a stray key on an otherwise valid edit must be refused (422), got %d: %v",
			status, data)
	}
	if got := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchLessonsRejectsEmptyEdit: an edit carrying neither old nor new is
// malformed, not a request to append nothing.
func TestPatchLessonsRejectsEmptyEdit(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	const seeded = "line one\nline two\n"
	seedLessonsOverlay(t, dal, "assistant", "general", seeded)

	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general", `{"edits":[{}]}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("an edit with neither old nor new must be refused (422), got %d: %v", status, data)
	}
	if got := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchLessonsAnchorNotFoundIs400: a non-empty old that matches nothing
// rejects the whole batch and writes nothing.
func TestPatchLessonsAnchorNotFoundIs400(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	const seeded = "line one\nline two\n"
	seedLessonsOverlay(t, dal, "assistant", "general", seeded)

	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"a line that is simply not there","new":"x"}]}`)
	if status != http.StatusBadRequest {
		t.Fatalf("a missing anchor must be a flat 400, got %d: %v", status, data)
	}
	if got := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); got != seeded {
		t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
	}
}

// TestPatchLessonsAppliedEditsCountsWhatLanded: applied_edits used to report
// len(edits) — the count REQUESTED, structurally incapable of being 0 and so
// carrying no information about whether anything landed. It must now report
// the edits that actually changed the doc.
func TestPatchLessonsAppliedEditsCountsWhatLanded(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	const seeded = "line one\nline two\n"
	seedLessonsOverlay(t, dal, "assistant", "general", seeded)

	// Two edits, only one of which changes anything: a real replace plus an
	// append of the empty string (the exact no-op the old code counted as 1).
	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"line two","new":"line two changed"},{"old":"","new":""}]}`)
	if status != http.StatusOK {
		t.Fatalf("patch must land, got %d: %v", status, data)
	}
	applied, _ := data["applied_edits"].(float64)
	if applied != 1 {
		t.Fatalf("applied_edits must count only the edit that changed the doc, got %v", data["applied_edits"])
	}
	want := "line one\nline two changed\n"
	if got := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); got != want {
		t.Fatalf("patched doc mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// ── replace_lessons / replace_global_context ─────────────────────────────────

// TestReplaceLessonsGuards: replace_lessons was the one destructive whole-doc
// seam with NO guard at all. Missing `text` and an unguarded wipe must both be
// refused with the doc intact; a real write must still land.
func TestReplaceLessonsGuards(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	const seeded = "a lessons doc that took a long time to accumulate\n"
	seedLessonsOverlay(t, dal, "assistant", "general", seeded)
	path := srv.URL + "/api/lessons/assistant/general"

	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"missing text", `{}`, http.StatusUnprocessableEntity},
		{"unknown key", `{"lessons":"oops wrong field name"}`, http.StatusUnprocessableEntity},
		{"unguarded wipe", `{"text":""}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, data := doJSON(t, "POST", path, ownerTok, tc.body)
			if status != tc.want {
				t.Fatalf("want %d, got %d: %v", tc.want, status, data)
			}
			if got := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); got != seeded {
				t.Fatalf("doc must be untouched:\n got: %q\nwant: %q", got, seeded)
			}
		})
	}

	// A real replace still lands and reads back.
	const want = "the rewritten doc\n"
	if status, data := doJSON(t, "POST", path, ownerTok, `{"text":"the rewritten doc\n"}`); status != 200 {
		t.Fatalf("well-formed replace must land, got %d: %v", status, data)
	}
	if got := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); got != want {
		t.Fatalf("replace did not persist:\n got: %q\nwant: %q", got, want)
	}
}

// TestReplaceGlobalContextGuards: same posture on the user-custom block.
func TestReplaceGlobalContextGuards(t *testing.T) {
	srv, _, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	path := srv.URL + "/api/global-context"

	const seeded = "the owner's custom boot block\n"
	if status, data := doJSON(t, "POST", path, ownerTok,
		`{"text":"the owner's custom boot block\n"}`); status != 200 {
		t.Fatalf("seed write must land, got %d: %v", status, data)
	}

	readBack := func() string {
		t.Helper()
		status, data := doJSON(t, "GET", path, ownerTok, "")
		if status != 200 {
			t.Fatalf("get global-context: %d", status)
		}
		text, _ := data["text"].(string)
		return text
	}
	if got := readBack(); got != seeded {
		t.Fatalf("seed did not persist:\n got: %q\nwant: %q", got, seeded)
	}

	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"missing text", `{}`, http.StatusUnprocessableEntity},
		{"unknown key", `{"context":"oops wrong field name"}`, http.StatusUnprocessableEntity},
		{"unguarded wipe", `{"text":""}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, data := doJSON(t, "POST", path, ownerTok, tc.body)
			if status != tc.want {
				t.Fatalf("want %d, got %d: %v", tc.want, status, data)
			}
			if got := readBack(); got != seeded {
				t.Fatalf("block must be untouched:\n got: %q\nwant: %q", got, seeded)
			}
		})
	}
}
