package main

// task_transition_notices_t91_test.go — T-91「任務狀態變了，該知道的人不知道」.
//
// Four transitions, four owner rulings, and the rulings deliberately disagree
// with each other about the CHANNEL. Keeping them in one file is how the next
// reader sees that the disagreement is intentional rather than an inconsistency
// somebody should tidy up:
//
//	Q1 轉派  → on the TICKET (lock + reassigned_from ride the wake snapshot);
//	          the chat notice still goes out, demoted to a reminder.
//	Q2 轉派後 → the predecessor keeps every executor right until the successor
//	          claims; the successor has none but claim_task.
//	Q3 被擋   → on the TICKET ONLY. No message, by explicit ruling.
//	Q4 結案   → a DURABLE MESSAGE, because 開機盤點 lists only tasks that have not
//	          ended — a closed ticket is absent from the list that Q3 relies on.
//
// 🔴 THESE TESTS PIN MECHANISM, NOT PROSE. Where a document is the subject, the
// assertion is on a machine identifier the agent must be handed (`claim_task`,
// `reassigning`, `lock`) or on a whole-document equality — never on a sentence,
// because these texts are owner-editable and are expected to be rewritten.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// ── shared fixtures ──────────────────────────────────────────────────────────

// t91Reassigned sets up the state every Q1/Q2 case needs: an ad-hoc task owned
// by m-old, handed to m-new by the OWNER (so the predecessor is not the caller
// — the case where the predecessor cannot possibly still be authorised by
// having made the call itself). Returns the re-read task row.
func t91Reassigned(t *testing.T, api *apiServer) Task {
	t.Helper()
	putActiveMember(t, api, "m-old", "Old", KindStaff)
	putActiveMember(t, api, "m-new", "New", KindStaff)
	task := createAdHocTask(t, api, "m-old")
	rec := reassign(t, api, task.ID, memberTarget("m-new"), wireOwnerID, "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("reassign fixture must admit, got %d %s", rec.Code, rec.Body.String())
	}
	got, err := api.dal.GetTask(task.ID)
	if err != nil || got == nil {
		t.Fatalf("re-read reassigned task: %v", err)
	}
	if got.Lock != TaskLockReassigning || got.ReassignedFrom != "m-old" {
		t.Fatalf("fixture must leave the task under the reassigning lock with the "+
			"predecessor stamped, got %+v", got)
	}
	return *got
}

// t91ResumeTasks reads one actor's wake snapshot and hands back its task rows
// as raw maps — raw so an ABSENT key and a zero value stay distinguishable,
// which is exactly the difference these projections are about.
func t91ResumeTasks(t *testing.T, api *apiServer, actor string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleResumeSummaryApiResumeSummaryGet(rec,
		taskReq(t, "GET", "/api/resume-summary", nil, actor, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("resume-summary for %s: %d %s", actor, rec.Code, rec.Body.String())
	}
	var snap struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode resume-summary: %v", err)
	}
	return snap.Tasks
}

// t91GetTask reads one task through the ordinary read face as the owner.
func t91GetTask(t *testing.T, api *apiServer, taskID string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, wireOwnerID, "owner"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get_task %s: %d %s", taskID, rec.Code, rec.Body.String())
	}
	var dto map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return dto
}

// t91ChatTo returns every durable chat row addressed to one recipient.
func t91ChatTo(t *testing.T, api *apiServer, recipient string) []ChatMessage {
	t.Helper()
	all, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	var out []ChatMessage
	for _, m := range all {
		if m.Recipient == recipient {
			out = append(out, m)
		}
	}
	return out
}

// ── Q1: the handover is on the ticket, not only in a message ────────────────

// The wake snapshot is the list an agent inventories at boot. Before T-91 it
// was the ONE task projection that dropped `lock` and `reassigned_from` — the
// full taskDTO and the light list row both carried them — so a task that had
// been handed to you looked exactly like a task you had been working on, and
// the only thing that said otherwise was a chat message posted once, at a
// moment you may well have been offline for.
func TestResumeSummaryTaskRowCarriesTheReassignHold(t *testing.T) {
	api := newTasksTestServer(t)
	task := t91Reassigned(t, api)

	rows := t91ResumeTasks(t, api, "m-new")
	var row map[string]any
	for _, r := range rows {
		if r["id"] == task.ID {
			row = r
		}
	}
	if row == nil {
		t.Fatalf("the successor's wake snapshot must list the task it just "+
			"received; rows=%v", rows)
	}
	if row["lock"] != TaskLockReassigning {
		t.Fatalf("resume-summary task row must carry lock=%q so a handover is "+
			"visible at 開機盤點; got %v (row=%v)", TaskLockReassigning, row["lock"], row)
	}
	if row["reassigned_from"] != "m-old" {
		t.Fatalf("resume-summary task row must name the PREDECESSOR to hand over "+
			"with (reassigned_from), got %v (row=%v)", row["reassigned_from"], row)
	}
	if row["reassigned_from_kind"] != TaskExecutorStaff {
		t.Fatalf("resume-summary task row must say HOW to resolve reassigned_from "+
			"(staff vs outsource), got %v", row["reassigned_from_kind"])
	}
}

// The light list row already carried both fields before this ticket. Pinned
// here as the POSITIVE CONTROL for the test above: if it ever went red, the
// snapshot assertion would no longer be evidence that the wake path in
// particular was the gap.
func TestListTaskRowAlreadyCarriedTheReassignHold(t *testing.T) {
	api := newTasksTestServer(t)
	task := t91Reassigned(t, api)

	rec := httptest.NewRecorder()
	api.HandleListTasksApiTasksGet(rec,
		taskReq(t, "GET", "/api/tasks", nil, wireOwnerID, "owner"),
		HandleListTasksApiTasksGetParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks: %d %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, r := range rows {
		if r["id"] != task.ID {
			continue
		}
		if r["lock"] != TaskLockReassigning || r["reassigned_from"] != "m-old" {
			t.Fatalf("the LIGHT LIST row was already carrying the hold before "+
				"T-91 and must keep doing so, got %v", r)
		}
		return
	}
	t.Fatalf("list did not contain the reassigned task")
}

// ONE DOCUMENT, BOTH IDENTITIES. The owner's generalisation requirement,
// verbatim: 「轉交給外包跟轉交給一個目前離線的正職應該要有同樣一套方法」. The staff
// fold and the outsource worker's boot context are built from the SAME
// boot_sequence seed (TestBothBootPathsShareSlots124ByteForByte pins that slots
// 1, 2 and 4 are byte-identical across the two assembly paths), so the takeover
// instruction must be reachable from both — and it must be reachable because it
// is in the shared document, not because someone wrote it twice.
//
// 🔴 THAT CITATION WAS RE-POINTED, NOT JUST RENAMED (T-33, 2026-09-07). It used
// to name TestWorkerBootContextIsTheStaffFoldMinusThePersona, which said the two
// documents are byte-subtractions of ONE ANOTHER. That test is retired: the
// worker's slot 3 is no longer empty, so the subtraction is false. The sentence
// above survives because the takeover instruction lives in slot 4, which the
// replacement still compares byte for byte.
//
// The assertion is on MACHINE IDENTIFIERS (`claim_task`, the `reassigning` lock
// value, the `reassigned_from` field name), never on the sentence around them:
// the prose is owner-editable, the tool name and the lock value are not.
func TestBootSequenceTellsBothIdentitiesToConfirmThenClaim(t *testing.T) {
	s := newWorkerTestServer(t)
	staff, err := s.buildBootContext("", nil)
	if err != nil || staff == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	worker, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: "ow-t91", Codename: "T-91", Model: "opus",
			Effort: "high", Runtime: RuntimeClaude},
		Task{ID: "t-t91t91t91t9", Title: "任務", Priority: TaskPriorityHigh}, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}
	for _, token := range []string{"claim_task", TaskLockReassigning, "reassigned_from"} {
		if !strings.Contains(staff.Context, token) {
			t.Fatalf("the 正職 boot document must hand the agent %q — a takeover "+
				"instruction that names no tool is not an instruction", token)
		}
		if !strings.Contains(worker, token) {
			t.Fatalf("the 外包 boot document must hand the worker %q through the "+
				"SAME shared document; writing a second copy for 外包 is what the "+
				"owner ruled out", token)
		}
	}
}

// ── Q2: the predecessor holds the task until the successor claims it ─────

// handoverFixture is T-1, planned and edited by kip, handed by the owner to rex
// (a plain agent, so no admin bypass hides the rule) and not yet claimed. T-2
// is an unrelated task zed executes. Credentials come from the product's mint.
type handoverFixture struct {
	api                                     *apiServer
	h                                       http.Handler
	owner, predecessor, successor, outsider string
	stepOne, stepTwo, artifact, descVersion string
}

func newHandoverFixture(t *testing.T) handoverFixture {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	for _, id := range []string{"rex", "zed"} {
		if err := d.PutMember(Member{
			ID: id, Name: id, Kind: KindStaff, RoleKey: "engineer",
			RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("PutMember(%s): %v", id, err)
		}
	}
	f := handoverFixture{
		api: api, h: h, owner: owner,
		predecessor: apiTestAgentToken(t, api, "kip", ""),
		successor:   apiTestAgentToken(t, api, "rex", ""),
		outsider:    apiTestAgentToken(t, api, "zed", ""),
	}
	f.must(t, "POST", "/api/tasks", owner,
		`{"title":"Ship it","description":"first scope","executor_member_id":"kip"}`)
	f.must(t, "POST", "/api/tasks", owner, `{"title":"Original","executor_member_id":"zed"}`)
	f.must(t, "POST", "/api/tasks/T-1/plan", f.predecessor,
		`{"steps":[{"name":"one","dod":"d1"},{"name":"two","dod":"d2"}]}`)
	pinned := f.must(t, "POST", "/api/tasks/T-1/artifact", f.predecessor,
		`{"kind":"link","name":"PR 1","url":"https://example.com/pr/1"}`)
	f.artifact, _ = pinned["artifact_id"].(string)
	f.must(t, "POST", "/api/tasks/T-1/description", f.predecessor, `{"description":"second scope"}`)
	history := apiRequest(t, h, "GET", "/api/document-history/task_description/T-1", owner, "")
	var versions []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &versions); err != nil || len(versions) != 1 {
		t.Fatalf("description history must hold the one replaced text: %v %s", err, history.Body.String())
	}
	f.descVersion = strconv.FormatInt(versions[0].ID, 10)
	f.must(t, "POST", "/api/tasks/T-1/reassign", owner, `{"target":{"kind":"staff","member_id":"rex"}}`)
	_, task := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
	steps, _ := task["steps"].([]any)
	if len(steps) != 2 || task["lock"] != TaskLockReassigning || task["reassigned_from"] != "kip" {
		t.Fatalf("fixture must leave T-1 planned and under the hold from kip, got %v", task)
	}
	f.stepOne, _ = steps[0].(map[string]any)["id"].(string)
	f.stepTwo, _ = steps[1].(map[string]any)["id"].(string)
	return f
}

func (f handoverFixture) must(t *testing.T, method, target, token, body string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, f.h, method, target, token, body)
	if status != http.StatusOK {
		t.Fatalf("%s %s: %d %v", method, target, status, data)
	}
	return data
}

func (f handoverFixture) claim(t *testing.T) {
	t.Helper()
	f.must(t, "POST", "/api/tasks/T-1/claim", f.successor, "")
}

// handoverView is T-1 as the successor reads it.
type handoverView struct {
	Title, Description, Priority, Status, Lock, ExecutorKind, ExecutorID string
	ReassignedFrom, ReassignedFromKind                                   string
	Deps, Steps, Artifacts                                               []string
	StepOneNote                                                          string
}

func (f handoverFixture) view(t *testing.T) handoverView {
	t.Helper()
	status, task := apiJSON(t, f.h, "GET", "/api/tasks/T-1", f.successor, "")
	if status != http.StatusOK {
		t.Fatalf("successor read of T-1: %d %v", status, task)
	}
	v := handoverView{Deps: []string{}, Steps: []string{}, Artifacts: []string{}}
	v.Title, _ = task["title"].(string)
	v.Description, _ = task["description"].(string)
	v.Priority, _ = task["priority"].(string)
	v.Status, _ = task["status"].(string)
	v.Lock, _ = task["lock"].(string)
	v.ExecutorKind, _ = task["executor_kind"].(string)
	v.ExecutorID, _ = task["executor_id"].(string)
	v.ReassignedFrom, _ = task["reassigned_from"].(string)
	v.ReassignedFromKind, _ = task["reassigned_from_kind"].(string)
	if strings.HasPrefix(v.ExecutorID, "ow-") {
		v.ExecutorID = "ow-(minted)"
	}
	deps, _ := task["deps"].([]any)
	for _, d := range deps {
		v.Deps = append(v.Deps, d.(string))
	}
	steps, _ := task["steps"].([]any)
	for _, raw := range steps {
		st := raw.(map[string]any)
		v.Steps = append(v.Steps, st["name"].(string)+":"+st["status"].(string))
	}
	_, arts := apiJSON(t, f.h, "GET", "/api/tasks/T-1/artifacts", f.successor, "")
	list, _ := arts["artifacts"].([]any)
	for _, raw := range list {
		a := raw.(map[string]any)
		ref, _ := a["filename"].(string)
		if ref == "" {
			ref, _ = a["url"].(string)
		}
		v.Artifacts = append(v.Artifacts, a["name"].(string)+":"+a["kind"].(string)+":"+ref)
	}
	if status, step := apiJSON(t, f.h, "GET", "/api/tasks/T-1/steps/"+f.stepOne, f.successor, ""); status == http.StatusOK {
		v.StepOneNote, _ = step["note"].(string)
	}
	return v
}

// handoverDoor is one task write. prep runs as the owner (who may drive any
// task) and returns whatever id the call needs; want maps T-1 as it stood
// before the fixture's claim/no-claim phase to T-1 after the write succeeds.
type handoverDoor struct {
	name string
	prep func(t *testing.T, f handoverFixture) string
	call func(t *testing.T, f handoverFixture, token, prepared string) (int, map[string]any)
	want func(before handoverView) handoverView
}

func handoverPost(target, body string) func(*testing.T, handoverFixture, string, string) (int, map[string]any) {
	return func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
		return apiJSON(t, f.h, "POST", target, token, body)
	}
}

func handoverUntouched(lock string) handoverView {
	return handoverView{
		Title: "Ship it", Description: "second scope", Priority: "mid",
		Status: "not_started", Lock: lock, ExecutorKind: "staff", ExecutorID: "rex",
		ReassignedFrom: "kip", ReassignedFromKind: "staff",
		Deps:      []string{},
		Steps:     []string{"one:pending", "two:pending"},
		Artifacts: []string{"PR 1:link:https://example.com/pr/1"},
	}
}

func handoverWith(edit func(v *handoverView)) func(handoverView) handoverView {
	return func(v handoverView) handoverView {
		edit(&v)
		return v
	}
}

func handoverDoors() []handoverDoor {
	return []handoverDoor{
		{name: "insert_step",
			call: handoverPost("/api/tasks/T-1/steps", `{"name":"three","dod":"d3"}`),
			want: handoverWith(func(v *handoverView) { v.Steps = []string{"one:pending", "two:pending", "three:pending"} })},
		{name: "delete_step",
			call: func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
				return apiJSON(t, f.h, "POST", "/api/tasks/T-1/steps/"+f.stepTwo+"/delete", token, "")
			},
			want: handoverWith(func(v *handoverView) { v.Steps = []string{"one:pending"} })},
		{name: "reorder_steps",
			call: func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
				return apiJSON(t, f.h, "POST", "/api/tasks/T-1/steps/reorder", token,
					`{"step_ids":["`+f.stepTwo+`","`+f.stepOne+`"]}`)
			},
			want: handoverWith(func(v *handoverView) { v.Steps = []string{"two:pending", "one:pending"} })},
		{name: "submit_plan",
			call: handoverPost("/api/tasks/T-1/plan", `{"steps":[{"name":"replanned","dod":"d"}]}`),
			want: handoverWith(func(v *handoverView) { v.Steps = []string{"replanned:pending"} })},
		{name: "update_step_status",
			call: func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
				return apiJSON(t, f.h, "POST", "/api/tasks/T-1/steps/"+f.stepOne+"/status", token,
					`{"status":"in_progress"}`)
			},
			want: handoverWith(func(v *handoverView) {
				v.Status = "in_progress"
				v.Steps = []string{"one:in_progress", "two:pending"}
			})},
		{name: "set_task_deps",
			call: handoverPost("/api/tasks/T-1/deps", `{"blocked_by":["T-2"]}`),
			want: handoverWith(func(v *handoverView) { v.Deps = []string{"T-2"} })},
		{name: "set_task_priority",
			call: handoverPost("/api/tasks/T-1/priority", `{"priority":"high"}`),
			want: handoverWith(func(v *handoverView) { v.Priority = "high" })},
		{name: "reassign_task",
			call: handoverPost("/api/tasks/T-1/reassign", `{"target":{"kind":"outsource","model":"sonnet","effort":"high"}}`),
			want: handoverWith(func(v *handoverView) {
				if v.Lock == "" {
					v.ReassignedFrom = "rex"
				}
				v.Lock, v.ExecutorKind, v.ExecutorID = "reassigning", "outsource", "ow-(minted)"
			})},
		{name: "mark_task_duplicated",
			call: handoverPost("/api/tasks/T-1/mark-duplicated", `{"duplicate_of":"T-2"}`),
			want: handoverWith(func(v *handoverView) { v.Status = "duplicated" })},
		{name: "create_reply_card bound to the task",
			prep: func(t *testing.T, f handoverFixture) string {
				f.must(t, "POST", "/api/tasks/T-1/steps/"+f.stepOne+"/status", f.owner, `{"status":"in_progress"}`)
				return ""
			},
			call: func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
				return apiJSON(t, f.h, "POST", "/api/reply-cards", token,
					`{"kind":"decision","summary":"ship?","options":[{"text":"yes"}],`+
						`"linked_task":{"task_id":"T-1","step_id":"`+f.stepOne+`"}}`)
			},
			want: handoverWith(func(v *handoverView) {
				v.Status = "waiting_owner"
				v.Steps = []string{"one:waiting_owner", "two:pending"}
			})},
		{name: "add_task_artifact",
			call: handoverPost("/api/tasks/T-1/artifact", `{"kind":"link","name":"PR 2","url":"https://example.com/pr/2"}`),
			want: handoverWith(func(v *handoverView) {
				v.Artifacts = []string{"PR 1:link:https://example.com/pr/1", "PR 2:link:https://example.com/pr/2"}
			})},
		{name: "remove_task_artifact",
			call: func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
				return apiJSON(t, f.h, "DELETE", "/api/tasks/T-1/artifact/"+f.artifact, token, "")
			},
			want: handoverWith(func(v *handoverView) { v.Artifacts = []string{} })},
		{name: "replace_task_artifact",
			call: func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
				return apiJSON(t, f.h, "POST", "/api/tasks/T-1/artifact/"+f.artifact+"/replace", token,
					`{"url":"https://example.com/pr/9"}`)
			},
			want: handoverWith(func(v *handoverView) { v.Artifacts = []string{"PR 1:link:https://example.com/pr/9"} })},
		{name: "upload_task_artifact",
			call: handoverPost("/api/tasks/T-1/artifacts/upload?name=notes&filename=notes.md&mime=text/markdown", "bytes"),
			want: handoverWith(func(v *handoverView) {
				v.Artifacts = []string{"PR 1:link:https://example.com/pr/1", "notes:file:notes.md"}
			})},
		{name: "replace_task_artifact upload",
			prep: func(t *testing.T, f handoverFixture) string {
				data := f.must(t, "POST", "/api/tasks/T-1/artifacts/upload?name=notes&filename=notes.md&mime=text/markdown",
					f.owner, "bytes")
				id, _ := data["artifact_id"].(string)
				return id
			},
			call: func(t *testing.T, f handoverFixture, token, prepared string) (int, map[string]any) {
				return apiJSON(t, f.h, "POST", "/api/tasks/T-1/artifact/"+prepared+
					"/replace/upload?filename=notes-v2.md&mime=text/markdown", token, "bytes v2")
			},
			want: handoverWith(func(v *handoverView) {
				v.Artifacts = []string{"PR 1:link:https://example.com/pr/1", "notes:file:notes-v2.md"}
			})},
		{name: "update_task",
			call: handoverPost("/api/tasks/T-1", `{"title":"Renamed"}`),
			want: handoverWith(func(v *handoverView) { v.Title = "Renamed" })},
		{name: "update_task_description",
			call: handoverPost("/api/tasks/T-1/description", `{"description":"third scope"}`),
			want: handoverWith(func(v *handoverView) { v.Description = "third scope" })},
		{name: "update_task_title",
			call: handoverPost("/api/tasks/T-1/title", `{"title":"Retitled"}`),
			want: handoverWith(func(v *handoverView) { v.Title = "Retitled" })},
		{name: "restore task_description",
			call: func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
				return apiJSON(t, f.h, "POST", "/api/document-history/task_description/T-1/"+f.descVersion+"/restore", token, "")
			},
			want: handoverWith(func(v *handoverView) { v.Description = "first scope" })},
		{name: "update_step_note",
			call: func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
				return apiJSON(t, f.h, "POST", "/api/tasks/T-1/steps/"+f.stepOne+"/note", token,
					`{"note":"做到一半，下一步是 X"}`)
			},
			want: handoverWith(func(v *handoverView) { v.StepOneNote = "做到一半，下一步是 X" })},
		{name: "patch_step_note",
			prep: func(t *testing.T, f handoverFixture) string {
				f.must(t, "POST", "/api/tasks/T-1/steps/"+f.stepOne+"/note", f.owner, `{"note":"next is X"}`)
				return ""
			},
			call: func(t *testing.T, f handoverFixture, token, _ string) (int, map[string]any) {
				return apiJSON(t, f.h, "POST", "/api/tasks/T-1/steps/"+f.stepOne+"/note/patch", token,
					`{"edits":[{"old":"X","new":"Y"}]}`)
			},
			want: handoverWith(func(v *handoverView) { v.StepOneNote = "next is Y" })},
		{name: "mark_task_terminated",
			call: handoverPost("/api/tasks/T-1/mark-terminated", ""),
			want: handoverWith(func(v *handoverView) { v.Status = "terminated" })},
		{name: "mark_task_done",
			prep: func(t *testing.T, f handoverFixture) string {
				for _, step := range []string{f.stepOne, f.stepTwo} {
					f.must(t, "POST", "/api/tasks/T-1/steps/"+step+"/status", f.owner, `{"status":"in_progress"}`)
					f.must(t, "POST", "/api/tasks/T-1/steps/"+step+"/status", f.owner, `{"status":"done"}`)
				}
				return ""
			},
			call: handoverPost("/api/tasks/T-1/mark-done", ""),
			want: handoverWith(func(v *handoverView) {
				v.Status = "done"
				v.Steps = []string{"one:done", "two:done"}
			})},
	}
}

// Between reassign and claim the predecessor keeps every executor right and the
// successor has none but claim_task (owner ruling 2026-09-17, rc-5ba4a6f802f4 /
// rc-0a0892e3588f). Claiming hands the rights over; nobody else ever has them.
func TestTaskWriteRightsFollowTheReassignHoldUntilTheSuccessorClaims(t *testing.T) {
	refused := func(t *testing.T, door handoverDoor, f handoverFixture, prepared, who, token string) {
		t.Helper()
		before := f.view(t)
		status, data := door.call(t, f, token, prepared)
		if status != http.StatusForbidden {
			t.Fatalf("%s by %s: want 403, got %d %v", door.name, who, status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
		if after := f.view(t); !reflect.DeepEqual(after, before) {
			t.Fatalf("%s by %s was refused but T-1 changed:\nbefore %#v\nafter  %#v", door.name, who, before, after)
		}
	}
	admitted := func(t *testing.T, door handoverDoor, f handoverFixture, prepared, token string, want handoverView) {
		t.Helper()
		if status, data := door.call(t, f, token, prepared); status != http.StatusOK {
			t.Fatalf("%s: want 200, got %d %v", door.name, status, data)
		}
		if got := f.view(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: T-1 as the successor reads it\ngot  %#v\nwant %#v", door.name, got, want)
		}
	}
	prepare := func(t *testing.T, door handoverDoor, f handoverFixture) string {
		t.Helper()
		if door.prep == nil {
			return ""
		}
		return door.prep(t, f)
	}

	for _, door := range handoverDoors() {
		t.Run("under the hold the predecessor may "+door.name, func(t *testing.T) {
			f := newHandoverFixture(t)
			prepared := prepare(t, door, f)
			admitted(t, door, f, prepared, f.predecessor, door.want(handoverUntouched("reassigning")))
		})
		t.Run("under the hold the successor and an outsider may not "+door.name, func(t *testing.T) {
			f := newHandoverFixture(t)
			prepared := prepare(t, door, f)
			refused(t, door, f, prepared, "the successor", f.successor)
			refused(t, door, f, prepared, "an outsider", f.outsider)
		})
		t.Run("after the claim the successor may "+door.name, func(t *testing.T) {
			f := newHandoverFixture(t)
			f.claim(t)
			prepared := prepare(t, door, f)
			admitted(t, door, f, prepared, f.successor, door.want(handoverUntouched("")))
		})
		t.Run("after the claim the predecessor and an outsider may not "+door.name, func(t *testing.T) {
			f := newHandoverFixture(t)
			f.claim(t)
			prepared := prepare(t, door, f)
			refused(t, door, f, prepared, "the predecessor", f.predecessor)
			refused(t, door, f, prepared, "an outsider", f.outsider)
		})
	}

	t.Run("under the hold only the successor may claim", func(t *testing.T) {
		f := newHandoverFixture(t)
		for who, token := range map[string]string{"the predecessor": f.predecessor, "an outsider": f.outsider} {
			status, data := apiJSON(t, f.h, "POST", "/api/tasks/T-1/claim", token, "")
			if status != http.StatusForbidden {
				t.Fatalf("claim by %s: want 403, got %d %v", who, status, data)
			}
			apiWantError(t, data, "forbidden", "caller is not the task's executor")
		}
		if got := f.view(t); !reflect.DeepEqual(got, handoverUntouched("reassigning")) {
			t.Fatalf("refused claims must leave the hold in place, got %#v", got)
		}
		f.claim(t)
		want := handoverUntouched("")
		if got := f.view(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("the successor's claim clears the hold\ngot  %#v\nwant %#v", got, want)
		}
	})
}

// ── Q3: the blocker's side is on the ticket, and is never a message ─────────

// t91Block makes blocked depend on blocker, as blocked's executor.
func t91Block(t *testing.T, api *apiServer, blocked, blocker, executor string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleSetTaskDepsApiTasksTaskIdDepsPost(rec, taskReq(t, "POST",
		"/api/tasks/"+blocked+"/deps",
		map[string]any{"blocked_by": []string{blocker}}, executor, "agent"), blocked)
	if rec.Code != http.StatusOK {
		t.Fatalf("set_task_deps: %d %s", rec.Code, rec.Body.String())
	}
}

// Before T-91 the blocking side had no channel of any kind: set_task_deps
// publishes the delta of the BLOCKED task only, so the person everyone is
// queued behind was told nothing, by any route.
func TestBlockerTicketNamesTheTasksWaitingOnIt(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-blocker", "Blocker", KindStaff)
	putActiveMember(t, api, "m-waiter", "Waiter", KindStaff)
	blocker := createAdHocTask(t, api, "m-blocker")
	waiter := createAdHocTask(t, api, "m-waiter")
	t91Block(t, api, waiter.ID, blocker.ID, "m-waiter")

	dto := t91GetTask(t, api, blocker.ID)
	raw, ok := dto["blocking"].([]any)
	if !ok {
		t.Fatalf("the blocker's ticket must carry `blocking` — the owner ruled this "+
			"side is written on the ticket and never messaged, so an absent field is "+
			"the whole failure; got %v", dto["blocking"])
	}
	if len(raw) != 1 {
		t.Fatalf("the blocker's ticket must name the 1 task waiting on it, got %d: %v",
			len(raw), raw)
	}
	entry, _ := raw[0].(map[string]any)
	if entry["id"] != waiter.ID {
		t.Fatalf("`blocking` must name WHICH ticket is waiting, got %v", entry)
	}
	if entry["title"] != waiter.Title {
		t.Fatalf("`blocking` must resolve the waiting ticket's display facts "+
			"(title), got %v", entry)
	}

	// A ticket that blocks nobody says so honestly, as [] — never absent, never
	// null: "nobody is waiting" and "this field does not exist" must not look
	// the same to the agent reading it.
	other := t91GetTask(t, api, waiter.ID)
	if got, ok := other["blocking"].([]any); !ok || len(got) != 0 {
		t.Fatalf("a ticket nobody waits on must answer blocking=[], got %v", other["blocking"])
	}
}

// The wake snapshot carries the same fact — the boot inventory is where the
// blocker's executor actually looks, and it is the only place they will look
// because Q3's ruling means nothing is sent.
func TestResumeSummaryCarriesTheBlockingIds(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-blocker", "Blocker", KindStaff)
	putActiveMember(t, api, "m-waiter", "Waiter", KindStaff)
	blocker := createAdHocTask(t, api, "m-blocker")
	waiter := createAdHocTask(t, api, "m-waiter")
	t91Block(t, api, waiter.ID, blocker.ID, "m-waiter")

	for _, row := range t91ResumeTasks(t, api, "m-blocker") {
		if row["id"] != blocker.ID {
			continue
		}
		ids, ok := row["blocking"].([]any)
		if !ok || len(ids) != 1 || ids[0] != waiter.ID {
			t.Fatalf("the wake snapshot's task row must carry the ids of the "+
				"tickets waiting on it (nothing is messaged, so this is the whole "+
				"delivery), got %v", row["blocking"])
		}
		return
	}
	t.Fatalf("the blocker's wake snapshot did not list its own task")
}

// 🔴 A CLOSED WAITER IS NOT A WAITER. Q3's whole delivery is this one field, so
// its VALUE is the deliverable, not merely its presence: the blocker's executor
// reads "3 tickets are waiting on me" and acts on it, and the only useful
// reading of that sentence is "3 tickets are STILL waiting". The dependency row
// survives the waiter being terminated (nothing rewrites blocked_by on close),
// so without the terminal filter in blockingTasksOf the count only ever grows
// and every ticket the executor was ever behind stays on the list forever —
// which is precisely the signal-quality problem this ticket exists to fix.
//
// Both faces are asserted because they are two projections of the one helper
// and each is somebody's only view: the ticket for the human, the wake snapshot
// for the agent at 開機盤點.
func TestBlockingSkipsWaitersThatHaveAlreadyClosed(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-blocker", "Blocker", KindStaff)
	putActiveMember(t, api, "m-waiter", "Waiter", KindStaff)
	blocker := createAdHocTask(t, api, "m-blocker")
	live := createAdHocTask(t, api, "m-waiter")
	dead := createAdHocTask(t, api, "m-waiter")
	t91Block(t, api, live.ID, blocker.ID, "m-waiter")
	t91Block(t, api, dead.ID, blocker.ID, "m-waiter")

	rec := httptest.NewRecorder()
	api.HandleMarkTaskTerminatedApiTasksTaskIdMarkTerminatedPost(rec,
		taskReq(t, "POST", "/api/tasks/"+dead.ID+"/mark-terminated", nil, "owner", "owner"),
		dead.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate the waiter: %d %s", rec.Code, rec.Body.String())
	}
	// Positive control for the whole test: the dependency edge must OUTLIVE the
	// close. If it did not, the assertions below would pass for a reason that has
	// nothing to do with the terminal filter.
	stillBlocked, err := api.dal.ListTasksBlockedBy(blocker.ID)
	if err != nil {
		t.Fatalf("read back the reverse dep edge: %v", err)
	}
	if len(stillBlocked) != 2 {
		t.Fatalf("closing a waiter must NOT delete its blocked_by row — this test "+
			"has no subject unless the terminated waiter is still on the edge; got %d",
			len(stillBlocked))
	}

	dto := t91GetTask(t, api, blocker.ID)
	raw, ok := dto["blocking"].([]any)
	if !ok {
		t.Fatalf("the blocker's ticket must carry `blocking`, got %v", dto["blocking"])
	}
	if len(raw) != 1 {
		t.Fatalf("`blocking` must name only the tickets STILL waiting — a closed "+
			"waiter is nobody's blocker and must be filtered out, or the field grows "+
			"monotonically and stops meaning anything; want 1, got %d: %v",
			len(raw), raw)
	}
	if entry, _ := raw[0].(map[string]any); entry["id"] != live.ID {
		t.Fatalf("`blocking` must keep the LIVE waiter (%s) and drop the terminated "+
			"one (%s), got %v", live.ID, dead.ID, raw[0])
	}

	for _, row := range t91ResumeTasks(t, api, "m-blocker") {
		if row["id"] != blocker.ID {
			continue
		}
		ids, ok := row["blocking"].([]any)
		if !ok || len(ids) != 1 || ids[0] != live.ID {
			t.Fatalf("the wake snapshot's blocking ids must exclude the terminated "+
				"waiter too — it is the same helper, and the agent's boot inventory is "+
				"the only place Q3's ruling lets it learn this; want [%s], got %v",
				live.ID, row["blocking"])
		}
		return
	}
	t.Fatalf("the blocker's wake snapshot did not list its own task")
}

// 🔴 THE OWNER RULED THIS ONE THE OPPOSITE WAY FROM Q4, ON PURPOSE: 只寫在票上，
// 不發訊息. A future reader who "completes" the feature by adding a notification
// here is reversing a decision, not filling a gap.
func TestBindingADependencySendsTheBlockerExecutorNothing(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-blocker", "Blocker", KindStaff)
	putActiveMember(t, api, "m-waiter", "Waiter", KindStaff)
	blocker := createAdHocTask(t, api, "m-blocker")
	waiter := createAdHocTask(t, api, "m-waiter")

	before := len(t91ChatTo(t, api, "m-blocker"))
	t91Block(t, api, waiter.ID, blocker.ID, "m-waiter")
	if after := len(t91ChatTo(t, api, "m-blocker")); after != before {
		t.Fatalf("hanging a ticket off a blocker must send its executor NO message "+
			"(owner ruling: 只寫在票上，不發訊息); chat rows went %d → %d", before, after)
	}
}

// ── Q4: the close notice is durable, wider, and says who closed it ──────────

// The two removed gates, as a pure decision. Both asked "does this task have
// learnings worth folding into a manual?" — the wrong question for a notice
// whose real content is "your ticket is closed, every write you make now 409s".
func TestCloseNudgeNoLongerSkipsDuplicateOrAdHocTasks(t *testing.T) {
	for _, c := range []struct {
		what string
		task Task
	}{
		{"a DUPLICATED task", Task{ID: "t-dup", Status: TaskStatusDuplicated,
			TypeKey: "review-pr", ExecutorID: "m-x"}},
		{"an AD-HOC task (no type)", Task{ID: "t-adhoc", Status: TaskStatusDone,
			TypeKey: "", ExecutorID: "m-x"}},
	} {
		if decideTaskCloseNudge(c.task) == nil {
			t.Fatalf("%s must still nudge its executor: the executor of a closed "+
				"ticket needs to know it is closed regardless of whether its type has a "+
				"manual behind it at all", c.what)
		}
	}
	// The one gate that stays, and the reason it stays is addressing, not
	// judgement: an unassigned task has no recipient.
	if decideTaskCloseNudge(Task{ID: "t-none", Status: TaskStatusDone,
		TypeKey: "review-pr", ExecutorID: ""}) != nil {
		t.Fatalf("an UNASSIGNED task must stay silent — there is nobody to address")
	}
	// And an open task is not a close.
	if decideTaskCloseNudge(Task{ID: "t-open", Status: TaskStatusInProgress,
		TypeKey: "review-pr", ExecutorID: "m-x"}) != nil {
		t.Fatalf("a non-terminal task must not nudge")
	}
}

// 🔴 THE DELIVERY GUARANTEE IS THE ACCEPTANCE CRITERION, not the send. The old
// path was hub.PushDirected — at-most-once down a live SSE connection, with the
// function's own comment admitting "an offline executor simply misses the
// reminder". This asserts the notice survives the recipient being absent: it is
// a DURABLE row, addressed to the executor, and therefore readable at its next
// wake. Nothing here connects an SSE client, which is the point.
func TestTaskCloseNudgeIsADurableChatRowTheExecutorReadsAtItsNextWake(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-exec", "Exec", KindStaff)
	task := createAdHocTask(t, api, "m-exec")

	rec := httptest.NewRecorder()
	api.HandleMarkTaskTerminatedApiTasksTaskIdMarkTerminatedPost(rec, taskReq(t, "POST",
		"/api/tasks/"+task.ID+"/mark-terminated", nil, wireOwnerID, "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner terminate: %d %s", rec.Code, rec.Body.String())
	}

	rows := t91ChatTo(t, api, "m-exec")
	var notice *ChatMessage
	for i, m := range rows {
		if id, _ := m.Meta["task_id"].(string); id == task.ID {
			notice = &rows[i]
		}
	}
	if notice == nil {
		t.Fatalf("closing a task must leave a DURABLE chat row for its executor — "+
			"an SSE push reaches nobody who was offline when the task closed; "+
			"rows to m-exec = %d", len(rows))
	}
	if notice.Sender != wireSystemSender {
		t.Fatalf("the close notice is server-authored, got sender %q", notice.Sender)
	}
	// The durability proof: the row is in the store, so the wake path that
	// reads the store hands it back — with no live connection anywhere in this
	// test.
	found := false
	for _, m := range mustResumeChat(t, api, "m-exec") {
		if m.ID == notice.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the close notice must be folded into the executor's WAKE " +
			"snapshot — 'the server sent it' is not the guarantee this ticket asks " +
			"for; 'the recipient sees it at its next boot' is")
	}
}

// mustResumeChat returns the chat block of one actor's wake snapshot.
func mustResumeChat(t *testing.T, api *apiServer, actor string) []ChatMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleResumeSummaryApiResumeSummaryGet(rec,
		taskReq(t, "GET", "/api/resume-summary", nil, actor, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("resume-summary: %d %s", rec.Code, rec.Body.String())
	}
	var snap struct {
		Chat []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"chat"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := make([]ChatMessage, 0, len(snap.Chat))
	for _, c := range snap.Chat {
		out = append(out, ChatMessage{ID: c.ID, Body: c.Body})
	}
	return out
}

// "I closed it myself" and "somebody terminated it under me" are
// opposite situations, and the notice used to render them identically. The
// closer is a DECLARED document variable so the sentence around it stays
// owner-editable — this asserts the value is carried and filled, not the words.
func TestTaskCloseNudgeNamesWhoClosedIt(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-exec", "Exec", KindStaff)
	task := createAdHocTask(t, api, "m-exec")

	rec := httptest.NewRecorder()
	api.HandleMarkTaskTerminatedApiTasksTaskIdMarkTerminatedPost(rec, taskReq(t, "POST",
		"/api/tasks/"+task.ID+"/mark-terminated", nil, wireOwnerID, "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner terminate: %d %s", rec.Code, rec.Body.String())
	}
	var notice *ChatMessage
	rows := t91ChatTo(t, api, "m-exec")
	for i, m := range rows {
		if id, _ := m.Meta["task_id"].(string); id == task.ID {
			notice = &rows[i]
		}
	}
	if notice == nil {
		t.Fatalf("no close notice was written")
	}
	if by, _ := notice.Meta["closed_by"].(string); by != wireOwnerID {
		t.Fatalf("the close notice must carry WHO closed the task as a field "+
			"(closed_by), got %v", notice.Meta["closed_by"])
	}
	if !strings.Contains(notice.Body, wireOwnerID) {
		t.Fatalf("the closer must reach the agent in the notice it READS, not only "+
			"in meta — the document declares {closed_by}; body=%q", notice.Body)
	}
	// The document is still the only source of the words: a kind that declares
	// a variable the code does not fill renders to "" and sends nothing, so the
	// non-empty body above is also the proof that the slot is wired.
	if strings.Contains(notice.Body, "{closed_by}") {
		t.Fatalf("the {closed_by} slot reached the agent unrendered: %q", notice.Body)
	}
}
