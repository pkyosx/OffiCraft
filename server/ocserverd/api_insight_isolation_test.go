package main

// Node 4's DoD, verbatim: "把 Insight 寫到 doc.cap_chars 上限，Duty 與 Learning
// 的內容與可寫性完全不受影響".
//
// This is the test that says the three blocks are actually separate. Before
// T-3809 there were two documents; the ticket's whole claim is that filling one
// of them cannot squeeze the others.
//
// 🔴 THE POSITIVE CONTROL IS THE POINT OF THIS FILE, not a formality. Without
// it, all three assertions pass VACUOUSLY in the world where the write never
// reached the cap at all: Duty unchanged, Learning unchanged, both still
// writable — because nothing happened. "Nothing happened" and "the isolation
// holds" produce identical output, so the control has to prove the cap was
// really pressed against before the other assertions mean anything.
//
// The cap comes from s.docCap(), the same read the product uses. A number this
// test invents would test the number, not the product.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFillingInsightToTheCapLeavesDutyAndLearningUntouched(t *testing.T) {
	api := newTasksTestServer(t)
	role := seedRoleAssistant
	req := func(method, path string, body any) *http.Request {
		return taskReq(t, method, path, body, "owner", "owner")
	}

	// ── baselines, taken from the product's own read faces ────────────────
	writeDuty := func(text string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandleUpdateRoleApiRolesRolePost(rec,
			req(http.MethodPost, "/api/roles/"+role, map[string]any{"definition_md": text}), role)
		return rec
	}
	writeLearning := func(text string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost(rec,
			req(http.MethodPost, "/api/lessons/"+role+"/general", map[string]any{"text": text}),
			role, "general")
		return rec
	}
	writeInsight := func(text string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandleReplaceInsightApiInsightRoleKeyPost(rec,
			req(http.MethodPost, "/api/insight/"+role, map[string]any{"text": text}), role)
		return rec
	}

	if rec := writeDuty("duty baseline — this role owns X"); rec.Code != http.StatusOK {
		t.Fatalf("seed duty: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := writeLearning("learning baseline — the port is 8791"); rec.Code != http.StatusOK {
		t.Fatalf("seed learning: status=%d body=%s", rec.Code, rec.Body.String())
	}

	duty, err := api.foldRoleDefDTO(role)
	if err != nil {
		t.Fatal(err)
	}
	dutyBefore := duty.DefinitionMD
	learning, err := api.foldLessonsDTO(role, "general")
	if err != nil {
		t.Fatal(err)
	}
	learningBefore := learning.Text

	// ── fill insight exactly to the cap the product enforces ──────────────
	cap := api.docCap()
	if cap <= 0 {
		t.Fatalf("docCap() = %d — a non-positive cap makes this test meaningless", cap)
	}
	atCap := strings.Repeat("界", cap) // multi-byte on purpose: the cap counts RUNES
	if utf8.RuneCountInString(atCap) != cap {
		t.Fatalf("fixture is %d runes, want exactly the cap %d", utf8.RuneCountInString(atCap), cap)
	}
	if rec := writeInsight(atCap); rec.Code != http.StatusOK {
		t.Fatalf("writing insight AT the cap was refused: status=%d body=%s "+
			"(the cap is an upper bound, not an exclusive one)", rec.Code, rec.Body.String())
	}

	// 🔴 POSITIVE CONTROL — prove the cap was actually reached. One more rune
	// must be refused. If this passes, every assertion below is measuring a
	// document that really is full; if it fails, they were measuring nothing.
	if rec := writeInsight(atCap + "界"); rec.Code != http.StatusBadRequest {
		t.Fatalf("control: writing cap+1 runes of insight returned %d, want 400. "+
			"The cap was never pressed against, so the isolation assertions below "+
			"would pass for the trivial reason that nothing happened.", rec.Code)
	}

	// ── (a) Duty unchanged, byte for byte ─────────────────────────────────
	duty, err = api.foldRoleDefDTO(role)
	if err != nil {
		t.Fatal(err)
	}
	if duty.DefinitionMD != dutyBefore {
		t.Fatalf("Duty changed while Insight was filled to the cap:\n before=%q\n after =%q",
			dutyBefore, duty.DefinitionMD)
	}

	// ── (b) Learning unchanged, byte for byte ─────────────────────────────
	learning, err = api.foldLessonsDTO(role, "general")
	if err != nil {
		t.Fatal(err)
	}
	if learning.Text != learningBefore {
		t.Fatalf("Learning changed while Insight was filled to the cap:\n before=%q\n after =%q",
			learningBefore, learning.Text)
	}

	// ── (c) both are still WRITABLE ───────────────────────────────────────
	// Content equality alone would not catch a shared budget: the damage a
	// shared cap does is refusing the NEXT write, not rewriting the old one.
	if rec := writeDuty(dutyBefore + " (still writable)"); rec.Code != http.StatusOK {
		t.Fatalf("Duty became unwritable while Insight sat at the cap: status=%d body=%s",
			rec.Code, rec.Body.String())
	}
	if rec := writeLearning(learningBefore + " (still writable)"); rec.Code != http.StatusOK {
		t.Fatalf("Learning became unwritable while Insight sat at the cap: status=%d body=%s",
			rec.Code, rec.Body.String())
	}
}

func TestInsightWriteAuthzNamesInsightNotLessons(t *testing.T) {
	// The 403 body is a DoD condition of node 5, and it is the reason
	// insightWriteAuthz exists instead of a call into lessonsWriteAuthz.
	// An agent told to go and look at its lessons doc has been sent to the
	// wrong place with confidence.
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	// A scratch agent whose roster row carries no role at all: the comparison
	// against a non-empty path role_key can never match, which is exactly the
	// posture every outsource worker sits in.
	api.HandleReplaceInsightApiInsightRoleKeyPost(rec,
		taskReq(t, http.MethodPost, "/api/insight/"+seedRoleAssistant,
			map[string]any{"text": "nope"}, "nobody", "agent"), seedRoleAssistant)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("roleless agent writing insight: status=%d body=%s, want 403", rec.Code, rec.Body.String())
	}
	const want = "an agent may only write its own role's insight"
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("403 body = %s\nwant it to contain %q — borrowing the lessons wording would "+
			"name the wrong document", rec.Body.String(), want)
	}
	if strings.Contains(rec.Body.String(), "lessons") {
		t.Fatalf("403 body mentions \"lessons\" while refusing an INSIGHT write: %s", rec.Body.String())
	}
}
