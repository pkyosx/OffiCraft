package main

// The ONE silent surface in T-3809.
//
// publishDocumentHistoryRestore is a switch with no default branch. Omit the
// insight case and a restore still succeeds: the row is written, the DTO comes
// back, HTTP 200, no error, no other test goes red — and the only symptom is
// that every surface holding the document keeps showing the old text until
// someone reloads by hand. role_definition already shipped exactly that bug
// (its case carries the note explaining why "role" fanned nothing), which is
// the reason this file exists rather than a comment asking people to remember.
//
// The lessons half is not decoration: without it, "no frame arrived" could just
// mean the fixture never wired a listener, and the insight assertion would pass
// vacuously in a world where restores fan nothing at all.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRestoringAnInsightDocFansAnInsightDelta(t *testing.T) {
	f := newHistoryFixture(t)
	role := seedRoleAssistant

	writeInsight := func(text string) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandleReplaceInsightApiInsightRoleKeyPost(rec,
			f.req(http.MethodPost, "/api/insight/"+role, map[string]any{"text": text}), role)
		if rec.Code != http.StatusOK {
			t.Fatalf("replace insight %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}
	writeLessons := func(text string) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost(rec,
			f.req(http.MethodPost, "/api/lessons/"+role+"/general", map[string]any{"text": text}),
			role, "general")
		if rec.Code != http.StatusOK {
			t.Fatalf("replace lessons %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}

	// Two writes each, so each document has a retained revision to restore.
	writeInsight("first insight")
	writeInsight("second insight")
	writeLessons("first lessons")
	writeLessons("second lessons")

	// Connect AFTER the writes so the queue holds only the restore frames.
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}

	// Positive control FIRST: lessons is a kind whose case has always been
	// present, so if this does not fan, the fixture is broken and the insight
	// assertion below would be measuring nothing.
	lessonsHistory := f.list("lessons", role+"::general")
	if len(lessonsHistory) == 0 {
		t.Fatal("lessons kept no revision to restore — the control cannot run")
	}
	f.restore("lessons", role+"::general", lessonsHistory[0].Id)
	raw := listener.pop()
	if raw == nil {
		t.Fatal("control: restoring lessons fanned NO frame — the listener or the publish seam is broken, " +
			"so a missing insight frame below would prove nothing")
	}
	if _, envelope := parseSSEFrame(t, raw); envelope["topic"] != "lessons" {
		t.Fatalf("control: restoring lessons fanned topic=%v, want \"lessons\"", envelope["topic"])
	}

	// The real assertion.
	insightHistory := f.list("insight", role)
	if len(insightHistory) == 0 {
		t.Fatal("insight kept no revision to restore")
	}
	f.restore("insight", role, insightHistory[0].Id)
	raw = listener.pop()
	if raw == nil {
		t.Fatal("restoring an insight doc fanned NO frame: the restore answered 200 and changed the " +
			"database, so nothing else in the build will tell you — every open surface is now showing " +
			"stale text. Add `case \"insight\"` to publishDocumentHistoryRestore.")
	}
	_, envelope := parseSSEFrame(t, raw)
	if envelope["topic"] != "insight" {
		t.Fatalf("restore fanned topic=%v, want \"insight\" (a topic outside the closed set in "+
			"sseTopics is dropped SILENTLY at the publish seam)", envelope["topic"])
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("frame data is not an object: %v", envelope["data"])
	}
	if want := wireOwnerID + "::" + role; data["key"] != want {
		t.Fatalf("frame key = %v, want %q — insight's history key is the BARE role_key, "+
			"with no task_type segment", data["key"], want)
	}
}
