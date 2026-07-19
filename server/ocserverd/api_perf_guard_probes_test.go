package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// PROBE 1 (denylist lock / kyle-160e reassigning integration): open=true must
// KEEP any status that is not one of the three terminal states — including a
// status this build has never heard of. This pins the denylist SHAPE so a
// future refactor to an allowlist (enumerate known non-terminal states) fails.
func TestProbeOpenKeepsUnknownNonTerminalStatus(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	// A future non-terminal status (e.g. kyle-160e's "reassigning"), plus a
	// known terminal one as the contrast.
	mk := func(id, status string) {
		if err := s.dal.PutTask(Task{
			ID: id, TypeKey: "tm-x", Title: id, Status: status,
			Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
			ExecutorID: "m-1", CreatedTS: 1000, UpdatedTS: 1000,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("t-reassign", "reassigning") // unknown-to-this-build, NON-terminal
	mk("t-done", TaskStatusDone)    // terminal contrast

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey,
		map[string]any{"sub": "owner", "scope": "owner"}))
	rec := httptest.NewRecorder()
	open := "true"
	s.HandleListTasksApiTasksGet(rec, req, HandleListTasksApiTasksGetParams{Open: &open})
	var rows []struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &rows)
	got := map[string]bool{}
	for _, r := range rows {
		got[r.ID] = true
	}
	if !got["t-reassign"] {
		t.Fatal("open=true dropped an unknown NON-terminal status — filter is allowlist-shaped, will hide kyle-160e's reassigning")
	}
	if got["t-done"] {
		t.Fatal("open=true leaked a terminal task")
	}
}

// PROBE 2 (peek literal-true guard, mirrors TestTasksOpenParamOnlyLiteralTrueFilters):
// only the literal "true" activates peek. Any other value must behave like the
// plain marking list (advance the watermark).
func TestProbePeekOnlyLiteralTrueSkipsMark(t *testing.T) {
	for _, v := range []string{"false", "1", "TRUE", "yes"} {
		s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
		seedTwoConversations(t, s)
		with := "m-1"
		peek := v
		chatGetRec(s, "owner", HandleListChatApiChatGetParams{With: &with, Peek: &peek})
		if wm := ownerWatermark(t, s, "m-1"); wm != 3.0 {
			t.Fatalf("peek=%q must NOT be treated as peek (watermark should advance to 3.0), got %v", v, wm)
		}
	}
}
