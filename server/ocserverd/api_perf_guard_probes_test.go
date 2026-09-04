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

// PROBE 2 was the peek literal-"true" guard: only that exact string activated
// the read-only view, anything else fell through to the marking list. T-48
// removed both the marking list and the parameter (owner ruling, 2026-09-02),
// so there is no string to parse and no watermark write to skip. The stronger
// replacement — this route never writes a watermark on ANY path — lives in
// api_chat_peek_test.go (TestChatListNeverAdvancesWatermark).
