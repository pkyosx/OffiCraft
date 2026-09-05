package main

// api_doc_caps_manual_split_t30f1_test.go — T-30f1: the task manual's SOP and
// learnings answer to TWO independent caps, on every door.
//
// 🔴 THE TWO CAPS ARE ALWAYS DIFFERENT NUMBERS HERE, for the reason the T-ae38
// file states about its three: with one shared number, "sop_md was judged by
// the SOP cap" and "sop_md was judged by the one manual cap" are the same
// sentence, and every assertion below would pass on the pre-split code too.
//
// 🔴 THE FIXTURES ARE MULTI-BYTE, same reason as its neighbour: the cap counts
// RUNES, and an ASCII fixture cannot tell a rune cap from a byte cap.
//
// Four doors carry the manual's caps, and each fails differently if it keeps
// reading one number:
//
//   - update_task_manual — one of TWO write faces for sop_md (patch_task_sop is
//     the other) and a SECOND one for learnings, judging both in a single
//     handler;
//   - patch_task_sop (T-1667) — the second sop_md door, judging the SAME cap on
//     the RESULT of its patch. Its cap assertion lives in
//     api_anchor_patch_t1667_test.go, not below: this file has no subtest for
//     it, so do not read the bullet as coverage;
//   - write_task_learnings / patch_task_learnings — learnings only, but their
//     receipt and their response DTO must quote the learnings cap, not the SOP's;
//   - document-history restore — an older, larger revision is still a write, so
//     a restore door reading the wrong cap makes the other cap a suggestion.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// manualCapsTestServer returns a server whose two manual caps are set to TWO
// DIFFERENT numbers — the whole discriminating power of this file.
func manualCapsTestServer(t *testing.T, sop, learnings int) *apiServer {
	t.Helper()
	api := newTasksTestServer(t)
	api.settingsMu.Lock()
	api.docCapCharsManualSop = sop
	api.docCapCharsManualLearnings = learnings
	api.settingsMu.Unlock()
	return api
}

// TestManualSopAndLearningsAreJudgedByTheirOwnCap presses each document to a
// size that is legal under its OWN cap and illegal under the other's. Both
// directions are exercised, so neither a swap nor a shared read can pass.
func TestManualSopAndLearningsAreJudgedByTheirOwnCap(t *testing.T) {
	const sopCap, learningsCap = 1200, 9000
	api := manualCapsTestServer(t, sopCap, learningsCap)
	key := seedManualWithLearnings(t, api, "")

	// Direction 1 — over the SOP cap, comfortably under the learnings cap.
	// Only sop_md may be refused.
	tooBigForSop := runesDoc(t, sopCap+1)
	if rec := updateManual(t, api, key, map[string]any{"sop_md": tooBigForSop}); rec.Code != http.StatusBadRequest {
		t.Fatalf("sop_md at %d runes must be refused by the SOP cap %d: %d %s",
			sopCap+1, sopCap, rec.Code, rec.Body.String())
	}
	if rec := updateManual(t, api, key, map[string]any{"learnings": tooBigForSop}); rec.Code != http.StatusOK {
		t.Fatalf("the SAME text must LAND as learnings (under the learnings cap %d): %d %s",
			learningsCap, rec.Code, rec.Body.String())
	}

	// Direction 2 — over the learnings cap, and therefore also over the SOP
	// cap; a fixture that only ran one way could not tell a swap from a split.
	tooBigForLearnings := runesDoc(t, learningsCap+1)
	if rec := updateManual(t, api, key, map[string]any{"learnings": tooBigForLearnings}); rec.Code != http.StatusBadRequest {
		t.Fatalf("learnings at %d runes must be refused by the learnings cap %d: %d %s",
			learningsCap+1, learningsCap, rec.Code, rec.Body.String())
	}

	// And each is a real cap at its own number: exactly at it lands.
	if rec := updateManual(t, api, key, map[string]any{"sop_md": runesDoc(t, sopCap)}); rec.Code != http.StatusOK {
		t.Fatalf("sop_md exactly at its own cap must land: %d %s", rec.Code, rec.Body.String())
	}
	if rec := updateManual(t, api, key, map[string]any{"learnings": runesDoc(t, learningsCap)}); rec.Code != http.StatusOK {
		t.Fatalf("learnings exactly at its own cap must land: %d %s", rec.Code, rec.Body.String())
	}
}

// TestManualUpdateJudgesEachFieldSeparatelyInOneCall — the shape that a shared
// read gets wrong even when both caps are consulted somewhere: one request
// carrying BOTH fields, where one is legal and the other is not. The handler
// validates before applying anything, so the whole partial update must be
// unwritten.
func TestManualUpdateJudgesEachFieldSeparatelyInOneCall(t *testing.T) {
	const sopCap, learningsCap = 1200, 9000
	api := manualCapsTestServer(t, sopCap, learningsCap)
	key := seedManualWithLearnings(t, api, "")

	legalLearnings := runesDoc(t, sopCap+500) // over the SOP cap, under its own
	if rec := updateManual(t, api, key, map[string]any{
		"sop_md":    runesDoc(t, sopCap+1),
		"learnings": legalLearnings,
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("an over-cap sop_md must refuse the whole call: %d %s", rec.Code, rec.Body.String())
	}
	m, err := api.dal.GetTaskManual(key)
	if err != nil || m == nil {
		t.Fatalf("readback: %+v %v", m, err)
	}
	if m.Learnings != "" || m.SopMD != "" {
		t.Fatalf("a refused call must leave BOTH fields unwritten, got %d/%d runes",
			len([]rune(m.SopMD)), len([]rune(m.Learnings)))
	}

	// Positive control: the same learnings text alone lands, so the refusal
	// above was the SOP cap and not a broken fixture.
	if rec := updateManual(t, api, key, map[string]any{"learnings": legalLearnings}); rec.Code != http.StatusOK {
		t.Fatalf("the learnings half must be legal on its own: %d %s", rec.Code, rec.Body.String())
	}
}

// TestManualReadFacesReportBothCaps — the numbers an agent sizes its next edit
// against. One `cap_chars` cannot answer for two documents; the split fields
// are the answer, and the retained `cap_chars` speaks for learnings only.
func TestManualReadFacesReportBothCaps(t *testing.T) {
	const sopCap, learningsCap = 1200, 9000
	api := manualCapsTestServer(t, sopCap, learningsCap)
	key := seedManualWithLearnings(t, api, "")

	read := func(t *testing.T, path string, h func(http.ResponseWriter, *http.Request)) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		h(rec, taskReq(t, "GET", path, nil, "m-exec", "agent"))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
		var data map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return data
	}

	assertCaps := func(t *testing.T, what string, data map[string]any) {
		t.Helper()
		if got, _ := data["sop_md_cap_chars"].(float64); int(got) != sopCap {
			t.Fatalf("%s: sop_md_cap_chars must be the SOP cap %d, got %v", what, sopCap, data["sop_md_cap_chars"])
		}
		if got, _ := data["learnings_cap_chars"].(float64); int(got) != learningsCap {
			t.Fatalf("%s: learnings_cap_chars must be the learnings cap %d, got %v",
				what, learningsCap, data["learnings_cap_chars"])
		}
		// The deprecated field is retained and carries the LEARNINGS cap. A
		// pre-split client reading it gets a number that is merely narrower
		// than the whole truth, never a zero.
		if got, _ := data["cap_chars"].(float64); int(got) != learningsCap {
			t.Fatalf("%s: the deprecated cap_chars must carry the learnings cap %d, got %v",
				what, learningsCap, data["cap_chars"])
		}
	}

	single := read(t, "/api/task-manuals/"+key, func(w http.ResponseWriter, r *http.Request) {
		api.HandleGetTaskManualApiTaskManualsTypeKeyGet(w, r, key)
	})
	assertCaps(t, "get_task_manual", single)

	rec := httptest.NewRecorder()
	api.HandleListTaskManualsApiTaskManualsGet(rec,
		taskReq(t, "GET", "/api/task-manuals", nil, "m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("list returned no rows — the fixture stopped discriminating")
	}
	for _, row := range rows {
		assertCaps(t, "list_task_manuals", row)
	}
}

// TestLearningsOnlyFacesQuoteTheLearningsCap — write_task_learnings has no
// receipt of its own (it answers with the whole manual DTO, whose cap fields
// come from inside writeTaskManual rather than from the handler that judged
// the write), and patch_task_learnings quotes the cap it judged against. Both
// must speak the LEARNINGS cap; quoting the SOP's would hand an agent the wrong
// budget for the only document these faces can write.
func TestLearningsOnlyFacesQuoteTheLearningsCap(t *testing.T) {
	const sopCap, learningsCap = 1200, 9000
	api := manualCapsTestServer(t, sopCap, learningsCap)
	key := seedManualWithLearnings(t, api, "")

	// write_task_learnings: a doc over the SOP cap but under its own must land.
	body := runesDoc(t, sopCap+500)
	rec := httptest.NewRecorder()
	api.HandleWriteTaskLearningsApiTaskManualsTypeKeyLearningsPost(rec,
		taskReq(t, "POST", "/api/task-manuals/"+key+"/learnings",
			map[string]any{"text": body}, "m-exec", "agent"), key)
	if rec.Code != http.StatusOK {
		t.Fatalf("write_task_learnings under the learnings cap must land: %d %s", rec.Code, rec.Body.String())
	}
	var manual map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &manual); err != nil {
		t.Fatalf("decode write_task_learnings response: %v", err)
	}
	// T-91: write_task_learnings answers its OWN receipt now, not the whole
	// manual — so the cap it quotes is a bare `cap_chars`, and there is no
	// sop_md_cap_chars beside it. That absence is the ticket's point restated,
	// not a loss of coverage: this write never looked at the SOP document, and a
	// response that reported the SOP's cap invited the exact cross-reading the
	// T-30f1 split exists to prevent. The SOP cap is still asserted on the two
	// faces that DO write the SOP, below and in the update_task_manual case.
	if got, _ := manual["cap_chars"].(float64); int(got) != learningsCap {
		t.Fatalf("write_task_learnings must quote the learnings cap %d, got %v",
			learningsCap, manual["cap_chars"])
	}
	if _, present := manual["sop_md_cap_chars"]; present {
		t.Fatalf("a learnings-only write must not report the SOP cap at all: %v",
			manual["sop_md_cap_chars"])
	}

	// patch_task_learnings: the receipt quotes the cap the write was judged
	// against, which is the learnings one.
	rec = httptest.NewRecorder()
	api.HandlePatchTaskLearningsApiTaskManualsTypeKeyLearningsPatchPost(rec,
		taskReq(t, "POST", "/api/task-manuals/"+key+"/learnings/patch",
			map[string]any{"edits": []map[string]any{{"old": "", "new": "尾"}}},
			"m-exec", "agent"), key)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch_task_learnings: %d %s", rec.Code, rec.Body.String())
	}
	var receipt map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil {
		t.Fatalf("decode patch receipt: %v", err)
	}
	if got, _ := receipt["cap_chars"].(float64); int(got) != learningsCap {
		t.Fatalf("the patch receipt must quote the learnings cap %d, got %v",
			learningsCap, receipt["cap_chars"])
	}
}

// TestManualRestoreDoorsUseTheirOwnCap — an older, larger revision is still a
// write. T-ae38 fixed exactly this hole for Duty; here the two manual streams
// were already separate `case`s, with one shared cap between them, so a restore
// judged the SOP by the learnings budget and vice versa.
func TestManualRestoreDoorsUseTheirOwnCap(t *testing.T) {
	const sopCap, learningsCap = 1200, 9000
	api := manualCapsTestServer(t, sopCap, learningsCap)
	key := seedManualWithLearnings(t, api, "")

	// A revision that is legal for learnings and illegal for the SOP.
	big := runesDoc(t, sopCap+500)
	req := taskReq(t, "POST", "/api/document-history/restore", nil, "m-exec", "agent")

	// Restoring it into the LEARNINGS stream is allowed…
	if err := api.restoreDocumentHistory(req, docKindTaskManualLearnings, key,
		map[string]string{"learnings": big}); err != nil {
		t.Fatalf("a revision under the learnings cap must restore: %v", err)
	}

	// …and the same size into the SOP stream is not. Both directions go through
	// the real switch rather than a restated predicate: what is under test is
	// which cap each `case` reads, and only the switch can be wrong about that.
	if err := api.restoreDocumentHistory(req, docKindTaskManualSop, key,
		map[string]string{"sop_md": big}); err != errDocumentHistoryCap {
		t.Fatalf("restoring %d runes of sop_md must hit the SOP cap %d, got %v",
			sopCap+500, sopCap, err)
	}
	m, err := api.dal.GetTaskManual(key)
	if err != nil || m == nil {
		t.Fatalf("readback: %+v %v", m, err)
	}
	if m.SopMD != "" {
		t.Fatalf("a refused restore must write nothing, sop_md is %d runes", len([]rune(m.SopMD)))
	}

	// Positive control: a revision under the SOP cap restores through the same
	// door, so the refusal above was the cap and not a broken call.
	small := runesDoc(t, sopCap-1)
	if err := api.restoreDocumentHistory(req, docKindTaskManualSop, key,
		map[string]string{"sop_md": small}); err != nil {
		t.Fatalf("a revision under the SOP cap must restore: %v", err)
	}
}
