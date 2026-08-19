package main

// The NEGATIVE half of the patch-publish surface: a patch that wrote nothing
// must announce nothing.
//
// api_insight_patch_publish_test.go pins the positive direction — a patch that
// really changed the doc MUST fan an SSE frame, because a write nobody
// announces leaves every open surface showing stale text. These pin the
// direction nobody was watching: a patch that changed NOTHING must not fan
// either, because a frame nobody has a change for is the same silent lie
// pointing the other way. Every listening cockpit refetches, the refetch
// returns the text it already had, and the only trace is load nobody ordered.
//
// Why this file exists at all, stated plainly: when the persistence gate moved
// from `applied > 0` to `next != current.Text`, the publish call sat INSIDE the
// gate and moved with it — but nothing in the build would have noticed if it
// had not. Hoisting `s.hub.Publish` (lessons, insight) or `s.publishTaskManual`
// (task manual) back out of the gate leaves the whole go suite green, because
// every existing publish test asks only "did a real write fan a frame".
//
// This repo has already been bitten by exactly that blind spot, in this exact
// seam. api_insight_patch_publish_test.go's own header records it: someone
// deleted patch_insight's single hub.Publish line and ran the full go suite
// plus all 1061 conformance cases, and everything stayed GREEN. That was the
// missing-frame direction. This file closes the extra-frame direction, for
// every anchor-patch seam rather than only insight.
//
// patch_task_sop and patch_step_note (T-1667) are the fourth and fifth seams and
// both shipped announcing unconditionally, so both are pinned here too. The step
// note is the one that has nothing to do with document history — it keeps no
// versions, so the SSE frame and the task's updated_ts are the ENTIRE cost of a
// write nobody needed, and this file is the only place that cost is watched.
//
// The positive control in every test is load-bearing and must stay first. A
// listener that was never wired, a hub that fans nothing at all, and a
// correctly-silent skipped write are indistinguishable from "pop() returned
// nil" — so each test first proves a REAL patch on the SAME doc, through the
// SAME handler, on the SAME listener, does fan.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// drainHubFrames empties the listener so the next pop() can only return a frame
// caused by the write made after it.
func drainHubFrames(l *hubListener) {
	for l.pop() != nil {
	}
}

// assertNoFrame fails with the topic of whatever arrived — the topic is what
// tells a maintainer WHICH publish seam leaked past the gate.
func assertNoFrame(t *testing.T, l *hubListener, what string) {
	t.Helper()
	raw := l.pop()
	if raw == nil {
		return
	}
	_, envelope := parseSSEFrame(t, raw)
	t.Errorf("%s fanned an SSE frame (topic=%v): nothing was written, so this announces a "+
		"change that never happened and every listening surface refetches for nothing",
		what, envelope["topic"])
}

func TestCancellingBatchPatchLessonsFansNoFrame(t *testing.T) {
	f := newHistoryFixture(t)
	const role, taskType = seedRoleAssistant, "general"
	const original = "LESSON: widen the anchor until it is unique.\n"
	if err := f.api.dal.PutLessons(Lessons{RoleKey: role, TaskType: taskType, Text: original}); err != nil {
		t.Fatalf("seed lessons: %v", err)
	}
	// Connect AFTER seeding: every frame popped below belongs to a write this
	// test made through the handler.
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}
	patch := func(body any) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandlePatchLessonsApiLessonsRoleKeyTaskTypePatchPost(rec,
			f.req(http.MethodPost, "/api/lessons/"+role+"/"+taskType+"/patch", body), role, taskType)
		if rec.Code != http.StatusOK {
			t.Fatalf("patch: status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	patch(map[string]any{"edits": []any{map[string]any{"old": "unique.", "new": "provably unique."}}})
	if listener.pop() == nil {
		t.Fatal("control: a real lessons patch fanned NO frame — the listener or the publish " +
			"seam is broken, so a missing frame below would prove nothing")
	}
	drainHubFrames(listener)

	patch(cancellingBatch("provably unique", "demonstrably unique"))
	assertNoFrame(t, listener, "a cancelling lessons batch")
}

func TestCancellingBatchPatchInsightFansNoFrame(t *testing.T) {
	f := newHistoryFixture(t)
	const role = seedRoleAssistant
	const original = "INSIGHT: prefer a slow correct split to a fast wrong one.\n"
	if err := f.api.dal.PutInsight(Insight{RoleKey: role, Text: original}); err != nil {
		t.Fatalf("seed insight: %v", err)
	}
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}
	patch := func(body any) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandlePatchInsightApiInsightRoleKeyPatchPost(rec,
			f.req(http.MethodPost, "/api/insight/"+role+"/patch", body), role)
		if rec.Code != http.StatusOK {
			t.Fatalf("patch: status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	patch(map[string]any{"edits": []any{map[string]any{"old": "wrong one", "new": "wrong merge"}}})
	if listener.pop() == nil {
		t.Fatal("control: a real insight patch fanned NO frame — the listener or the publish " +
			"seam is broken, so a missing frame below would prove nothing")
	}
	drainHubFrames(listener)

	patch(cancellingBatch("fast wrong merge", "fast mistaken merge"))
	assertNoFrame(t, listener, "a cancelling insight batch")
}

func TestCancellingBatchPatchTaskLearningsFansNoFrame(t *testing.T) {
	f := newHistoryFixture(t)
	const original = "LEARNING: the fixture seeds one manual per test.\n"
	key := seedManualWithLearnings(t, f.api, original)
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}

	if status, data := patchLearnings(t, f.api, key, map[string]any{
		"edits": []any{edit("seeds one manual", "seeds exactly one manual")},
	}); status != http.StatusOK {
		t.Fatalf("control edit must land, got %d: %v", status, data)
	}
	if listener.pop() == nil {
		t.Fatal("control: a real learnings patch fanned NO frame — the listener or the publish " +
			"seam is broken, so a missing frame below would prove nothing")
	}
	drainHubFrames(listener)

	if status, data := patchLearnings(t, f.api, key,
		cancellingBatch("exactly one manual", "precisely one manual")); status != http.StatusOK {
		t.Fatalf("cancelling batch must answer 200, got %d: %v", status, data)
	}
	assertNoFrame(t, listener, "a cancelling learnings batch")

	// Belt and braces: the premise these rest on is that the batch really was a
	// no-op. If the doc moved, a frame would be CORRECT and the assertion above
	// would be wrong to demand silence.
	if got := storedLearnings(t, f.api, key); got != "LEARNING: the fixture seeds exactly one manual per test.\n" {
		t.Fatalf("premise broken — the cancelling batch changed the doc: %q", got)
	}
}

func TestCancellingBatchPatchTaskSopFansNoFrame(t *testing.T) {
	f := newHistoryFixture(t)
	const edited = "## SOP\n1. 先讀 spec 再改 handler\n"
	key := seedManualWithSop(t, f.api, "## SOP\n1. 先讀 spec\n")
	// Connect AFTER seeding: the seed goes through the wholesale face, which fans
	// a frame of its own and would otherwise be waiting in the listener.
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}

	if status, data := patchSop(t, f.api, key, map[string]any{
		"edits": []any{edit("先讀 spec", "先讀 spec 再改 handler")},
	}); status != http.StatusOK {
		t.Fatalf("control edit must land, got %d: %v", status, data)
	}
	if listener.pop() == nil {
		t.Fatal("control: a real sop patch fanned NO frame — the listener or the publish " +
			"seam is broken, so a missing frame below would prove nothing")
	}
	drainHubFrames(listener)

	if status, data := patchSop(t, f.api, key,
		cancellingBatch("再改 handler", "再改 routes")); status != http.StatusOK {
		t.Fatalf("cancelling batch must answer 200, got %d: %v", status, data)
	}
	assertNoFrame(t, listener, "a cancelling sop batch")

	if got := storedSop(t, f.api, key); got != edited {
		t.Fatalf("premise broken — the cancelling batch changed the doc: %q", got)
	}
}

// TestCancellingBatchPatchStepNoteFansNoFrame is the one seam with no document
// history behind it, so the frame and the task's updated_ts are the whole of
// what a pointless write costs — and both are asserted here rather than split
// across two files.
func TestCancellingBatchPatchStepNoteFansNoFrame(t *testing.T) {
	f := newHistoryFixture(t)
	const edited = "做到哪：conformance 跑到第三關\n下一步：接 auth matrix"
	taskID, stepID := seedStepWithNote(t, f.api,
		"做到哪：conformance 跑到第三關\n下一步：接前端 i18n")
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}

	if status, data := patchStepNote(t, f.api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit("接前端 i18n", "接 auth matrix")},
	}); status != http.StatusOK {
		t.Fatalf("control edit must land, got %d: %v", status, data)
	}
	if listener.pop() == nil {
		t.Fatal("control: a real step-note patch fanned NO frame — the listener or the publish " +
			"seam is broken, so a missing frame below would prove nothing")
	}
	drainHubFrames(listener)
	// Read updated_ts after the control, which is supposed to have moved it.
	before, err := f.api.dal.GetTask(taskID)
	if err != nil || before == nil {
		t.Fatalf("read task: %+v %v", before, err)
	}

	if status, data := patchStepNote(t, f.api, taskID, stepID, "m-exec",
		cancellingBatch("接 auth matrix", "接 auth 矩陣")); status != http.StatusOK {
		t.Fatalf("cancelling batch must answer 200, got %d: %v", status, data)
	}
	assertNoFrame(t, listener, "a cancelling step-note batch")

	if got := readStepNote(t, f.api, taskID, stepID); got != edited {
		t.Errorf("premise broken — the cancelling batch changed the note: %q", got)
	}
	// updated_ts is what makes an already-open cockpit card re-read its steps, so
	// bumping it for a note that never moved orders the same refetch the missing
	// frame above would have.
	after, err := f.api.dal.GetTask(taskID)
	if err != nil || after == nil {
		t.Fatalf("re-read task: %+v %v", after, err)
	}
	if after.UpdatedTS != before.UpdatedTS {
		t.Errorf("a cancelling batch moved the task's updated_ts from %v to %v",
			before.UpdatedTS, after.UpdatedTS)
	}
}
