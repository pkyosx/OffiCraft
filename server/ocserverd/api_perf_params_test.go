package main

// api_perf_params_test.go — the cockpit-perf additive query params:
//   * GET /api/tasks?open=true    (T-2b9d) — drop terminal rows
//   * GET /api/members?fields=light (T-cf91) — identity-only, no unread scan
//   * GET /api/task-manuals?view=list (T-ec2c) — drop the heavy authored blobs
//
// The iron rule under test throughout: the DEFAULT (no new param) path is
// unchanged, and the light path is a STRICT behavioural narrowing — it must
// actually drop the terminal rows / the unread count / the sop_md, not merely
// happen to look smaller. The negative assertions below are the load-bearing
// ones (see the per-test mutant notes).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func perfReq(sub, scope string) *http.Request {
	req := httptest.NewRequest("GET", "/", nil)
	claims := map[string]any{"sub": sub, "scope": scope}
	return req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
}

func strptr(s string) *string { return &s }

// ── T-2b9d: /api/tasks?open=true ─────────────────────────────────────────────

func seedTasksMix(t *testing.T, s *apiServer) (openIDs, terminalIDs []string) {
	t.Helper()
	mk := func(id, status string) {
		closed := 0.0
		if TaskIsTerminal(status) {
			closed = 2000.0
		}
		if err := s.dal.PutTask(Task{
			ID: id, TypeKey: "tm-x", Title: id, Status: status,
			Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
			ExecutorID: "m-1", CreatedTS: 1000, UpdatedTS: 1000, ClosedTS: closed,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// The owner scenario: a few live tasks + a long tail of finished history,
	// covering all three terminal statuses (done / terminated / duplicated).
	for _, id := range []string{"t-open1", "t-open2", "t-open3", "t-open4"} {
		status := map[string]string{
			"t-open1": TaskStatusNotStarted, "t-open2": TaskStatusInProgress,
			"t-open3": TaskStatusWaitingOwner, "t-open4": TaskStatusWaitingExternal,
		}[id]
		mk(id, status)
		openIDs = append(openIDs, id)
	}
	for _, id := range []string{"t-done1", "t-term1", "t-dup1"} {
		status := map[string]string{
			"t-done1": TaskStatusDone, "t-term1": TaskStatusTerminated,
			"t-dup1": TaskStatusDuplicated,
		}[id]
		mk(id, status)
		terminalIDs = append(terminalIDs, id)
	}
	return
}

func listTasksIDs(t *testing.T, s *apiServer, params HandleListTasksApiTasksGetParams) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListTasksApiTasksGet(rec, perfReq("owner", "owner"), params)
	if rec.Code != 200 {
		t.Fatalf("list tasks → %d: %s", rec.Code, rec.Body.String())
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func TestTasksOpenParamDropsTerminalRowsOnly(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	openIDs, terminalIDs := seedTasksMix(t, s)

	// Default (no param) → the FULL population, terminals INCLUDED (the additive
	// iron rule: an omitted param is byte-for-byte the old behaviour).
	full := listTasksIDs(t, s, HandleListTasksApiTasksGetParams{})
	if len(full) != len(openIDs)+len(terminalIDs) {
		t.Fatalf("default list must carry every task: want %d, got %d (%v)",
			len(openIDs)+len(terminalIDs), len(full), full)
	}

	// ?open=true → the non-terminal rows ONLY.
	open := listTasksIDs(t, s, HandleListTasksApiTasksGetParams{Open: strptr("true")})
	got := map[string]bool{}
	for _, id := range open {
		got[id] = true
	}
	// MUTANT: change the handler guard to `if openOnly && !TaskIsTerminal(...)`
	// (or delete the `continue`) and THIS negative assertion goes red — a
	// terminal row leaks into the open view.
	for _, id := range terminalIDs {
		if got[id] {
			t.Fatalf("open=true leaked terminal task %s: %v", id, open)
		}
	}
	for _, id := range openIDs {
		if !got[id] {
			t.Fatalf("open=true dropped live task %s: %v", id, open)
		}
	}
	if len(open) != len(openIDs) {
		t.Fatalf("open=true count: want %d, got %d", len(openIDs), len(open))
	}
}

func TestTasksOpenParamOnlyLiteralTrueFilters(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	openIDs, terminalIDs := seedTasksMix(t, s)
	total := len(openIDs) + len(terminalIDs)
	// Any value other than the literal "true" is NOT the open filter — it leaves
	// the full list intact (guards against a truthy-ish parse that would make
	// ?open=false silently hide history).
	for _, v := range []string{"false", "1", "yes", "TRUE", ""} {
		ids := listTasksIDs(t, s, HandleListTasksApiTasksGetParams{Open: strptr(v)})
		if len(ids) != total {
			t.Fatalf("open=%q must not filter (want %d, got %d)", v, total, len(ids))
		}
	}
}

// ── T-cf91: /api/members?fields=light ────────────────────────────────────────

func seedMembersWithChat(t *testing.T, s *apiServer) {
	t.Helper()
	ok := true
	for _, id := range []string{"m-1", "m-2"} {
		if err := s.dal.PutMember(Member{
			ID: id, Name: "Name " + id, Kind: "assistant", RoleKey: "assistant",
			Model: "opus", Effort: "high", DesiredState: DesiredStateOnline,
			DesiredMachineID: "m-host", RosterStatus: RosterStatusActive,
			LastOp: "start", LastOpOK: &ok, LastOpLog: "a long operator log line",
			LastOpReason: "a reason string",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Unread the owner has NOT read: m-1 → owner. The full path counts this; the
	// light path must not even look at the chat stream.
	for i, id := range []string{"c-1", "c-2", "c-3"} {
		if err := s.dal.PutChat(ChatMessage{
			ID: id, Sender: "m-1", Recipient: "owner", Body: "hi", TS: float64(10 + i),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func listMembers(t *testing.T, s *apiServer, params HandleListMembersApiMembersGetParams) []memberDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListMembersApiMembersGet(rec, perfReq("owner", "owner"), params)
	if rec.Code != 200 {
		t.Fatalf("list members → %d: %s", rec.Code, rec.Body.String())
	}
	var out []memberDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestMembersFullPathComputesUnread(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), telemetry: newMemStore()}
	seedMembersWithChat(t, s)
	full := listMembers(t, s, HandleListMembersApiMembersGetParams{})
	var m1 *memberDTO
	for i := range full {
		if full[i].ID == "m-1" {
			m1 = &full[i]
		}
	}
	if m1 == nil {
		t.Fatal("m-1 missing from full roster")
	}
	// The default path still carries the unread count + operator log — proof the
	// light path below is a genuine narrowing, not the baseline.
	if m1.UnreadCount != 3 {
		t.Fatalf("full path unread: want 3, got %d", m1.UnreadCount)
	}
	if m1.LastOpLog == "" {
		t.Fatal("full path must carry last_op_log")
	}
}

func TestMembersLightSkipsUnreadAndHeavyFields(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), telemetry: newMemStore()}
	seedMembersWithChat(t, s)
	light := listMembers(t, s, HandleListMembersApiMembersGetParams{Fields: strptr("light")})
	if len(light) != 2 {
		t.Fatalf("light roster count: want 2, got %d", len(light))
	}
	for _, m := range light {
		// Identity + role ARE served (the 請示卡頁 reads these).
		if m.Name == "" || m.RoleName == "" || m.RoleKey == "" {
			t.Fatalf("light must keep identity+role: %+v", m)
		}
		// MUTANT: route the light branch through newMemberDTO (with the unread
		// scan) and these honest-empty assertions go red. unread_count is the
		// load-bearing one — m-1 has 3 genuinely-unread messages, so a non-zero
		// here proves the expensive whole-chat scan ran.
		if m.UnreadCount != 0 {
			t.Fatalf("light must NOT compute unread (honest-empty): %s = %d", m.ID, m.UnreadCount)
		}
		if m.LastOpLog != "" || m.LastOpReason != "" {
			t.Fatalf("light must drop last_op* text: %+v", m)
		}
		if m.Presence != "" || m.Machine != "" {
			t.Fatalf("light must not derive presence/machine: %+v", m)
		}
	}
}

// ── T-ec2c: /api/task-manuals?view=list ──────────────────────────────────────

func seedManuals(t *testing.T, s *apiServer) {
	t.Helper()
	for _, k := range []string{"tm-a", "tm-b"} {
		if err := s.dal.PutTaskManual(TaskManual{
			TypeKey: k, DisplayName: "Display " + k,
			Purpose:   "why this type exists",
			Fields:    `[{"name":"pr","required":true,"is_key":true}]`,
			SopMD:     "## huge SOP markdown body that the list view never shows",
			Learnings: "## accumulated learnings the list view never shows",
			Assignee:  `{"kind":"member","member_id":"m-1"}`,
			UpdatedTS: 1234,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func listManuals(t *testing.T, s *apiServer, params HandleListTaskManualsApiTaskManualsGetParams) []taskManualDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListTaskManualsApiTaskManualsGet(rec, perfReq("owner", "owner"), params)
	if rec.Code != 200 {
		t.Fatalf("list manuals → %d: %s", rec.Code, rec.Body.String())
	}
	var out []taskManualDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestManualsFullPathCarriesBody(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedManuals(t, s)
	// Default → the full body (proof the list view below is a real narrowing).
	full := listManuals(t, s, HandleListTaskManualsApiTaskManualsGetParams{})
	if len(full) != 2 {
		t.Fatalf("full manuals count: want 2, got %d", len(full))
	}
	for _, m := range full {
		if m.SopMD == "" || m.Learnings == "" || len(m.Fields) == 0 {
			t.Fatalf("full path must carry sop/learnings/fields: %+v", m)
		}
	}
}

func TestManualsListViewDropsHeavyBlobs(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedManuals(t, s)
	list := listManuals(t, s, HandleListTaskManualsApiTaskManualsGetParams{View: strptr("list")})
	if len(list) != 2 {
		t.Fatalf("list-view count: want 2, got %d", len(list))
	}
	for _, m := range list {
		// The type identity the 類型 filter reads IS served.
		if m.TypeKey == "" || m.DisplayName == "" || m.Purpose == "" {
			t.Fatalf("list view must keep type identity: %+v", m)
		}
		// MUTANT: route the list branch through newTaskManualDTO and these go
		// red — the heavy authored markdown leaks into the light list.
		if m.SopMD != "" {
			t.Fatalf("list view must drop sop_md: %q", m.SopMD)
		}
		if m.Learnings != "" {
			t.Fatalf("list view must drop learnings: %q", m.Learnings)
		}
		if len(m.Fields) != 0 {
			t.Fatalf("list view must drop fields: %+v", m.Fields)
		}
		if len(m.Assignee) != 0 {
			t.Fatalf("list view must drop assignee: %+v", m.Assignee)
		}
	}
}
