package main

// api_perf_params_test.go — the cockpit-perf additive query params:
//   * GET /api/tasks?open=true    (T-2b9d) — drop terminal rows
//   * GET /api/members?fields=light (T-cf91) — identity-only, no unread scan
//   * GET /api/task-manuals (T-ec2c, then T-1170) — no long documents at all
//   * GET /api/roles (T-1170) — no persona bodies at all
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
	"strings"
	"testing"
	"unicode/utf8"
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

func listManuals(t *testing.T, s *apiServer) []taskManualListItemDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListTaskManualsApiTaskManualsGet(rec, perfReq("owner", "owner"))
	if rec.Code != 200 {
		t.Fatalf("list manuals → %d: %s", rec.Code, rec.Body.String())
	}
	var out []taskManualListItemDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// listManualRows reads the SAME response as untyped JSON. Decoding into the DTO
// cannot tell "the key is absent" from "the key is an empty string", and absence
// is the contract here: a served "" in a field that normally holds the SOP reads
// as "this type has no SOP".
func listManualRows(t *testing.T, s *apiServer) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleListTaskManualsApiTaskManualsGet(rec, perfReq("owner", "owner"))
	if rec.Code != 200 {
		t.Fatalf("list manuals → %d: %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rows
}

// The default — and now only — answer drops the two long documents and keeps
// everything else, including the two small bounded values (`fields`,
// `assignee`) the old ?view=list row also blanked. Blanking those bought
// nothing and cost one extra request per row.
func TestListTaskManualsDropsTheLongDocumentsAndKeepsTheRest(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedManuals(t, s)
	rows := listManualRows(t, s)
	if len(rows) != 2 {
		t.Fatalf("manuals count: want 2, got %d", len(rows))
	}
	for _, row := range rows {
		for _, absent := range []string{"sop_md", "learnings"} {
			if _, present := row[absent]; present {
				t.Fatalf("%q must be ABSENT from the listing row, got %v", absent, row[absent])
			}
		}
		// Positive control: without these the absence assertions above would
		// also pass on a handler that returned bare {} for every type.
		for _, key := range []string{"type_key", "display_name", "purpose", "fields",
			"assignee", "sop_md_chars", "learnings_chars",
			"sop_md_cap_chars", "learnings_cap_chars"} {
			if _, present := row[key]; !present {
				t.Fatalf("listing row must still carry %q: %v", key, row)
			}
		}
	}
	for _, m := range listManuals(t, s) {
		if len(m.Fields) == 0 {
			t.Fatalf("fields must be the REAL parsed value, not blanked: %+v", m)
		}
		if m.Assignee["kind"] != "member" {
			t.Fatalf("assignee must be the REAL parsed value, not blanked: %+v", m.Assignee)
		}
	}
}

// The bodies are still reachable — one type at a time, which is the whole point
// of splitting the catalogue from the prose.
func TestGetTaskManualStillCarriesTheLongDocuments(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	seedManuals(t, s)
	rec := httptest.NewRecorder()
	s.HandleGetTaskManualApiTaskManualsTypeKeyGet(rec, perfReq("owner", "owner"), "tm-a")
	if rec.Code != 200 {
		t.Fatalf("get manual → %d: %s", rec.Code, rec.Body.String())
	}
	var got taskManualDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SopMD == "" || got.Learnings == "" {
		t.Fatalf("get_task_manual must still carry sop_md + learnings: %+v", got)
	}
}

// ── T-1170: the role listing is a catalogue, not the personas ────────────────

// The listing drops definition_md and keeps the two numbers that answer "which
// definition is nearly full". Asserted on the RAW JSON: decoding into the DTO
// cannot tell an absent key from an empty one, and absence is the contract —
// a served "" in the field that normally holds the persona reads as "this role
// has no definition".
func TestListRolesDropsThePersonaBodyAndKeepsItsSize(t *testing.T) {
	api := capsTestServer(t, maxDocCapChars, maxDocCapChars, maxDocCapChars)
	role := seedRoleAssistant
	duty := runesDoc(t, 300)
	if rec := writeDutyOn(t, api, role, duty); rec.Code != http.StatusOK {
		t.Fatalf("write duty: %d %s", rec.Code, rec.Body.String())
	}

	rec := httptest.NewRecorder()
	api.HandleListRolesApiRolesGet(rec,
		taskReq(t, http.MethodGet, "/api/roles", nil, "m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("list roles: %d %s", rec.Code, rec.Body.String())
	}
	// The persona really is distinctive, so finding none of it in the bytes is
	// evidence rather than luck.
	if strings.Contains(rec.Body.String(), duty[:32]) {
		t.Fatalf("the listing carries the persona body: %s", rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no roles listed — the assertions below would prove nothing")
	}
	var listed map[string]any
	for _, row := range rows {
		if row["key"] == role {
			listed = row
		}
		if _, present := row["definition_md"]; present {
			t.Fatalf("definition_md must be ABSENT from a listing row: %v", row)
		}
	}
	if listed == nil {
		t.Fatalf("the edited role is not in the listing: %v", rows)
	}
	if got := listed["size_chars"]; got != float64(utf8.RuneCountInString(duty)) {
		t.Fatalf("size_chars = %v, want %d (measured on the document the row omits)",
			got, utf8.RuneCountInString(duty))
	}
	if _, present := listed["cap_chars"]; !present {
		t.Fatalf("cap_chars must ride along so an agent can size an edit: %v", listed)
	}

	// Positive control: the body is still reachable, one role at a time.
	one := httptest.NewRecorder()
	api.HandleGetRoleApiRolesRoleGet(one,
		taskReq(t, http.MethodGet, "/api/roles/"+role, nil, "m-exec", "agent"), role)
	if one.Code != http.StatusOK {
		t.Fatalf("get role: %d %s", one.Code, one.Body.String())
	}
	var single map[string]any
	if err := json.Unmarshal(one.Body.Bytes(), &single); err != nil {
		t.Fatal(err)
	}
	if single["definition_md"] != duty {
		t.Fatalf("get_role must still carry the persona body: %v", single["definition_md"])
	}
}
