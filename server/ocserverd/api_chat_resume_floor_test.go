package main

// api_chat_resume_floor_test.go — T-1b09: the studio floor a waking agent lands
// on (roster + machines) inside the wake snapshot.
//
// Owner rulings under test (verbatim, 2026-08-03):
//   - rc-4e98c0481852 "All members and contractors and their online / offline
//     status" — EVERY member and EVERY contractor, offline ones included.
//   - rc-09476f535b59 ① machine list + which one you are on, and deliberately
//     NOT a per-machine grouping of who is where.
//   - 「1000字 多的截斷」 — duty carried as written, capped, `…` marks the cut.
//   - 「之後應該給 duty 就好，不要給 insight / learning」 (2026-08-02).
//
// Values in the fixtures below are chosen so that NO expected value is a
// substring of another (ids m-alpha/m-bravo/ow-charlie, machines m-host-one /
// m-host-two): a substring-tolerant assertion is close to a tautology, and this
// file asserts exact equality everywhere for that reason.

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func floorTestServer(t *testing.T) *apiServer {
	t.Helper()
	s := newTasksTestServer(t)
	// Two machines. Machine rows are warden-kind members: they are the
	// machine block's source AND the thing the roster must never show as a
	// colleague.
	seedMachine(t, s, "m-host-one")
	seedMachine(t, s, "m-host-two")
	return s
}

func putFloorMember(t *testing.T, s *apiServer, m Member) {
	t.Helper()
	if m.RosterStatus == "" {
		m.RosterStatus = RosterStatusActive
	}
	if err := s.dal.PutMember(m); err != nil {
		t.Fatal(err)
	}
}

func resumeFor(t *testing.T, s *apiServer, actor string) resumeSummaryDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleResumeSummaryApiResumeSummaryGet(rec, perfReq(actor, "agent"))
	if rec.Code != 200 {
		t.Fatalf("resume-summary → %d: %s", rec.Code, rec.Body.String())
	}
	var out resumeSummaryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func rosterRow(t *testing.T, rows []resumeRosterMemberDTO, id string) resumeRosterMemberDTO {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("roster is missing %q; got %d rows: %+v", id, len(rows), rows)
	return resumeRosterMemberDTO{}
}

// TestResumeRosterCarriesEveryMemberAndContractor is the owner's ruling stated
// as a test: an OFFLINE member is still a colleague you may need to reach, and
// a contractor is still someone whose work you may be about to duplicate.
//
// MUTANT: filter the roster loop to online members only (or to
// KindStaff only) — the offline member / the contractor disappears and
// this test goes red on the exact row it names.
func TestResumeRosterCarriesEveryMemberAndContractor(t *testing.T) {
	s := floorTestServer(t)
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindStaff, RoleKey: "assistant"})
	putFloorMember(t, s, Member{ID: "m-bravo", Name: "Bravo", Kind: KindStaff, RoleKey: "assistant"})
	putFloorMember(t, s, Member{ID: "ow-charlie", Name: "O-77", Kind: KindOutsource})

	got := resumeFor(t, s, "m-alpha")
	if len(got.Roster) != 3 {
		t.Fatalf("roster count: want exactly 3 (2 members + 1 contractor), got %d: %+v", len(got.Roster), got.Roster)
	}
	// Nobody online in this fixture: presence must still be reported, and the
	// offline rows must still be PRESENT — that is the whole ruling.
	for _, id := range []string{"m-alpha", "m-bravo", "ow-charlie"} {
		if p := rosterRow(t, got.Roster, id).Presence; p == "" {
			t.Fatalf("%s: presence must be reported, got empty", id)
		}
	}
	if k := rosterRow(t, got.Roster, "ow-charlie").Kind; k != KindOutsource {
		t.Fatalf("contractor kind: want %q, got %q", KindOutsource, k)
	}
}

// TestResumeRosterExcludesMachineRows: a warden row IS a machine. Showing it
// among colleagues would put two machines in the answer to "who can I ask".
//
// MUTANT: drop the `m.Kind == machineKind` continue in resumeFloorParts — the
// roster grows to 5 and the machine block empties; both halves go red.
func TestResumeRosterExcludesMachineRows(t *testing.T) {
	s := floorTestServer(t)
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindStaff, RoleKey: "assistant"})

	got := resumeFor(t, s, "m-alpha")
	if len(got.Roster) != 1 {
		t.Fatalf("roster must hold ONLY the colleague, got %d rows: %+v", len(got.Roster), got.Roster)
	}
	for _, r := range got.Roster {
		if r.Kind == machineKind {
			t.Fatalf("machine row leaked into the roster: %+v", r)
		}
	}
	if got.Machines == nil {
		t.Fatal("machines block missing")
	}
	if len(got.Machines.List) != 2 {
		t.Fatalf("machine list: want exactly 2, got %d: %+v", len(got.Machines.List), got.Machines.List)
	}
}

// TestResumeRosterOmitsInsightAndOperationalFields pins the field set EXACTLY.
//
// This is the load-bearing guard in this file, and it guards two different
// things at once:
//
//  1. The owner's "duty only, no insight / no learning" ruling. That absence is
//     a DECISION, not a limitation — role insight is readable by any
//     authenticated identity — so nothing but a test stops a future reader from
//     "helpfully" adding it back.
//  2. The cost line. The cheapest way to write this block is to reuse the full
//     member projection, and that projection computes unread counts through a
//     full chat-table scan — on a payload every agent pulls on every wake. If
//     anyone swaps in that projection, `unread_count` (and the operator-log
//     fields) appear in this JSON and this test goes red.
//
// It asserts an EXACT key set rather than a blacklist of forbidden names: a
// blacklist only ever catches the fields someone already thought of.
//
// MUTANT: build the roster from newMemberDTO instead of resumeRosterMemberDTO —
// the key set gains unread_count / last_op_* / desired_* and this goes red.
func TestResumeRosterOmitsInsightAndOperationalFields(t *testing.T) {
	s := floorTestServer(t)
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindStaff, RoleKey: "assistant"})
	// Unread chat exists in this fixture. The full member path would count it;
	// this payload must not even carry a field for it.
	if err := s.dal.PutChat(ChatMessage{ID: "c-floor-1", Sender: "m-alpha", Recipient: "owner", Body: "hi", TS: 10}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.HandleResumeSummaryApiResumeSummaryGet(rec, perfReq("m-alpha", "agent"))
	var raw struct {
		Roster []map[string]json.RawMessage `json:"roster"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw.Roster) != 1 {
		t.Fatalf("want 1 roster row, got %d", len(raw.Roster))
	}
	want := map[string]bool{
		"id": true, "name": true, "kind": true, "role_name": true,
		"duty": true, "current_task": true, "machine": true, "presence": true,
		"task_status": true, "waiting_reason": true,
		"progress_done": true, "progress_total": true,
	}
	for key := range raw.Roster[0] {
		if !want[key] {
			t.Fatalf("unexpected field %q in a roster row — the wake snapshot carries "+
				"duty only (owner 2026-08-02: 不要給 insight / learning), and reusing the "+
				"full member projection would also drag in the unread scan", key)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("roster row is missing required fields: %v", want)
	}
}

// TestResumeDutyIsCappedAndMarked — owner 2026-08-03「1000字 多的截斷」plus
// "Append … to let others know this is truncated".
//
// MUTANT: drop the cap in dutyText (return the definition as-is) — the length
// assertion goes red. Drop only the ellipsis — the marker assertion goes red.
// The two are asserted separately so one cannot mask the other.
func TestResumeDutyIsCappedAndMarked(t *testing.T) {
	s := floorTestServer(t)
	long := strings.Repeat("職", resumeDutyPreview+250)
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-verbose", Name: "Verbose Role", DefinitionMD: long}); err != nil {
		t.Fatal(err)
	}
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-terse", Name: "Terse Role", DefinitionMD: "接電話"}); err != nil {
		t.Fatal(err)
	}
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindStaff, RoleKey: "r-verbose"})
	putFloorMember(t, s, Member{ID: "m-bravo", Name: "Bravo", Kind: KindStaff, RoleKey: "r-terse"})

	got := resumeFor(t, s, "m-alpha")
	verbose := rosterRow(t, got.Roster, "m-alpha").Duty
	// Runes, not bytes: one CJK char is 3 bytes, so a byte-length assertion
	// here would pass for a payload three times over the cap.
	if n := len([]rune(verbose)); n != resumeDutyPreview+1 {
		t.Fatalf("capped duty length: want %d runes (cap + the ellipsis), got %d", resumeDutyPreview+1, n)
	}
	if !strings.HasSuffix(verbose, "…") {
		t.Fatal("a truncated duty must end in … so a reader can tell it was cut")
	}
	// The sentinel: a SHORT duty must come through whole and unmarked,
	// otherwise "everything ends in …" would satisfy the assertion above.
	terse := rosterRow(t, got.Roster, "m-bravo").Duty
	if terse != "接電話" {
		t.Fatalf("short duty must be carried verbatim and unmarked, got %q", terse)
	}
}

// TestResumeDutyDropsItsOwnTitleOnly — role docs open with their own title
// (「# 助理 — Mira」), which would otherwise spend the duty budget restating the
// role name the row already carries.
//
// MUTANT: make dutyText cap the raw text again (skip stripLeadingTitle) — the
// first assertion goes red on the leading title.
//
// The other three assertions are the sentinels that keep this from degenerating
// into "strip every leading heading", which was the first implementation and
// which review showed eats real content: a role doc written as an OUTLINE has
// its 「## 負責…」 lines as the duty itself, so a sub-heading right after the
// title must survive, and so must a heading in the middle.
func TestResumeDutyDropsItsOwnTitleOnly(t *testing.T) {
	s := floorTestServer(t)
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-titled", Name: "Titled Role",
		DefinitionMD: "# 標題甲\n\n## 副標乙\n\n負責丙\n\n### 段中丁\n\n負責戊"}); err != nil {
		t.Fatal(err)
	}
	// A doc that is nothing but a title: it must NOT come back empty, because
	// an empty duty means "no role at all", a different fact.
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-onlytitle", Name: "Only Title",
		DefinitionMD: "# 只有標題己"}); err != nil {
		t.Fatal(err)
	}
	// 「#1 順位」 is BODY text, not a heading: ATX syntax needs a space after
	// the hashes. A HasPrefix("#") test deletes this line silently.
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-hash", Name: "Hash Role",
		DefinitionMD: "#1 順位庚\n\n然後辛"}); err != nil {
		t.Fatal(err)
	}
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindStaff, RoleKey: "r-titled"})
	putFloorMember(t, s, Member{ID: "m-charlie", Name: "Charlie", Kind: KindStaff, RoleKey: "r-onlytitle"})
	putFloorMember(t, s, Member{ID: "m-delta", Name: "Delta", Kind: KindStaff, RoleKey: "r-hash"})

	got := resumeFor(t, s, "m-alpha")
	titled := rosterRow(t, got.Roster, "m-alpha").Duty
	// Exact prefix, not "contains": the title must be GONE and the sub-heading
	// under it must be the first thing left.
	if !strings.HasPrefix(titled, "## 副標乙") {
		t.Fatalf("the doc's own title must be stripped and nothing else, got %q", titled)
	}
	if strings.Contains(titled, "標題甲") {
		t.Fatalf("no title text may survive, got %q", titled)
	}
	if !strings.Contains(titled, "段中丁") {
		t.Fatalf("a heading in the MIDDLE is body text and must survive, got %q", titled)
	}
	onlyTitle := rosterRow(t, got.Roster, "m-charlie").Duty
	if onlyTitle != "# 只有標題己" {
		t.Fatalf("a title-only doc must come back whole, not empty, got %q", onlyTitle)
	}
	hashed := rosterRow(t, got.Roster, "m-delta").Duty
	if !strings.HasPrefix(hashed, "#1 順位庚") {
		t.Fatalf("#1 is body text (no space after the hash) and must survive, got %q", hashed)
	}
}

// TestResumeDutyStripsBeforeCapping pins the ORDER, which nothing else does.
//
// MUTANT: swap dutyText to stripLeadingTitle(truncateRunes(...)) — cap first,
// strip second. Every other test stays green, because their fixtures are short:
// the regression only shows on a doc LONGER than the cap that opens with a
// title — which is why this test builds that shape itself with strings.Repeat
// rather than leaning on a seed: the role docs this repo SHIPS are far shorter
// than the cap, so shipped reality cannot furnish an example of this shape at
// all. Cap-then-strip spends the whole budget including the title, then deletes
// the title, returning a short duty.
func TestResumeDutyStripsBeforeCapping(t *testing.T) {
	s := floorTestServer(t)
	body := strings.Repeat("職", resumeDutyPreview+250)
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-longtitled", Name: "Long Titled",
		DefinitionMD: "# 標題壬\n\n" + body}); err != nil {
		t.Fatal(err)
	}
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindStaff, RoleKey: "r-longtitled"})

	duty := rosterRow(t, resumeFor(t, s, "m-alpha").Roster, "m-alpha").Duty
	// The full budget must be spent on BODY: cap + the ellipsis, with no title
	// text in it. Cap-then-strip yields cap+1 minus the title's runes instead.
	if n := len([]rune(duty)); n != resumeDutyPreview+1 {
		t.Fatalf("duty must fill the cap with body text: want %d runes (cap + ellipsis), got %d",
			resumeDutyPreview+1, n)
	}
	if strings.Contains(duty, "標題壬") {
		t.Fatalf("the title must not reach the output at all, got prefix %q", string([]rune(duty)[:12]))
	}
}

// TestResumeContractorCarriesTaskTitleAndMemberDoesNot — owner ruling
// rc-a02d8bc7fe23: 正職給職責、外包給任務標題. A contractor id is minted per task,
// so its task title IS its duty.
//
// MUTANT: drop the title cap — the length assertion goes red.
//
// MUTANT (member half), and the honest limit of it: hoisting the
// contractorTaskTitle call out of the contractor branch so members get one too
// does NOT turn this red on its own — the lookup goes through the outsource
// binding, which a member does not have, so it returns "" anyway. What DOES
// turn it red is the realistic degradation: resolving the title by executor
// (ListOpenTasksByExecutor) AND filling it for everyone — verified, this test
// then reports the member carrying "成員自己的任務標題".
//
// ⚠️ So state the coverage precisely rather than claiming more: this guards
// "members must not be given a task title by a lookup that can find one". It
// does NOT guard against someone switching to the executor-based lookup for
// contractors only — that variant keeps every assertion here green while
// quietly MULTIPLYING the full task-table scans (task.executor_id has no
// index) on the boot path by the contractor count; the path already runs two
// of them for the caller's own tasks. That risk is held by the comment on
// contractorTaskTitle and by review, not by this test.
func TestResumeContractorCarriesTaskTitleAndMemberDoesNot(t *testing.T) {
	s := floorTestServer(t)
	longTitle := strings.Repeat("務", resumeTaskTitlePreview+60)
	if err := s.dal.PutTask(Task{
		ID: "t-floor-1", TypeKey: "tm-x", Title: longTitle, Status: TaskStatusWaitingExternal,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-charlie",
		WaitingReason: "等對方回覆", CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	taskID := "t-floor-1"
	if err := s.dal.PutTaskStep(TaskStep{ID: "st-floor-1", TaskID: taskID, OrderIdx: 0, Name: "step one", Status: StepStatusDone}); err != nil {
		t.Fatal(err)
	}
	if err := s.dal.PutTaskStep(TaskStep{ID: "st-floor-2", TaskID: taskID, OrderIdx: 1, Name: "step two", Status: StepStatusWaitingExternal, WaitingReason: "等對方回覆"}); err != nil {
		t.Fatal(err)
	}
	putFloorMember(t, s, Member{ID: "ow-charlie", Name: "O-77", Kind: KindOutsource, LinkedTaskID: &taskID})
	// ⚠️ The member below deliberately carries a task binding too — but be
	// precise about what it buys, because the obvious claim is FALSE and was
	// caught by review re-running the mutant by hand: hoisting the
	// contractorTaskTitle call out of the contractor branch stays GREEN with or
	// without this binding, because that lookup goes through GetOutsourceWorker
	// (`WHERE id = ? AND kind = 'outsource'`), which a member row never matches.
	// What this binding DOES buy is discriminating power against the realistic
	// degradation named in the header: resolving the title by executor
	// (ListOpenTasksByExecutor) AND filling it for everyone. Without a member
	// task row sitting on the executor side, that variant would also come out
	// empty here and stay green.
	memberTaskID := "t-floor-2"
	if err := s.dal.PutTask(Task{
		ID: memberTaskID, TypeKey: "tm-x", Title: "成員自己的任務標題", Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember, ExecutorID: "m-alpha",
		CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindStaff,
		RoleKey: "assistant", LinkedTaskID: &memberTaskID})

	got := resumeFor(t, s, "m-alpha")
	contractor := rosterRow(t, got.Roster, "ow-charlie")
	if n := len([]rune(contractor.CurrentTask)); n != resumeTaskTitlePreview+1 {
		t.Fatalf("contractor task title: want %d runes (cap + ellipsis), got %d (%q)",
			resumeTaskTitlePreview+1, n, contractor.CurrentTask)
	}
	if !strings.HasSuffix(contractor.CurrentTask, "…") {
		t.Fatal("a truncated task title must end in …")
	}
	// A member's duty is stable and answers "is this the right person to ask";
	// its task changes daily and would churn every agent's boot for less signal.
	member := rosterRow(t, got.Roster, "m-alpha")
	if member.CurrentTask != "" {
		t.Fatalf("a member must not carry current_task, got %q", member.CurrentTask)
	}
	if contractor.Duty != "" {
		t.Fatalf("a contractor has no role, so no duty; got %q", contractor.Duty)
	}
	// T-925f: the contractor's bound task's status/waiting_reason/progress ride
	// for free off the same GetTask row that built CurrentTask, and progress
	// comes from the roster-wide AllTaskStepProgress call.
	if contractor.TaskStatus != TaskStatusWaitingExternal {
		t.Fatalf("contractor task_status: want %q, got %q", TaskStatusWaitingExternal, contractor.TaskStatus)
	}
	if contractor.WaitingReason != "等對方回覆" {
		t.Fatalf("contractor waiting_reason: want %q, got %q", "等對方回覆", contractor.WaitingReason)
	}
	if contractor.ProgressDone != 1 || contractor.ProgressTotal != 2 {
		t.Fatalf("contractor progress: want 1/2, got %d/%d", contractor.ProgressDone, contractor.ProgressTotal)
	}
	// The member side stays bare (owner ruling rc-a02d8bc7fe23 / rc-6935feeb293a
	// 選①): no task binding, so all four progress fields stay zero.
	if member.TaskStatus != "" || member.WaitingReason != "" || member.ProgressDone != 0 || member.ProgressTotal != 0 {
		t.Fatalf("a member must not carry task progress fields, got %+v", member)
	}
}

// TestResumeContractorZeroProgressSeparatesNoStepsFromNoTask pins the DOUBLE
// MEANING of a contractor row reading 0/0: it is either "bound to a task that
// has no steps yet" or "bound to no task at all". Both come out of
// contractorTaskFields with the same zero progress, so progress alone cannot
// be read as "hasn't started" — task_status is the discriminator ("not_started"
// vs ""), and this test is what says so in executable form rather than in
// prose that nothing checks.
//
// The stepless task is created through the REAL create path, not PutTask with
// a hand-picked status: the whole claim is that the status a stepless task
// actually carries on the wire is non-empty, and a fixture that assigns that
// status itself would prove nothing.
//
// MUTANT: blank task_status (or current_task) out for a stepless task, or fill
// either one for a contractor with no task — the discriminator assertion goes
// red naming the two rows it could no longer tell apart.
func TestResumeContractorZeroProgressSeparatesNoStepsFromNoTask(t *testing.T) {
	s := floorTestServer(t)
	s.noOutsource = true // no scheduler spawn: this fixture binds its worker by hand
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindStaff, RoleKey: "assistant"})

	rec := createTaskAs(t, s, map[string]any{
		"title":  "尚未排步驟的外包任務",
		"target": map[string]any{"kind": "outsource", "model": "sonnet", "effort": "high"},
	}, "m-alpha", "agent")
	if rec.Code != 200 {
		t.Fatalf("create stepless task: %d %s", rec.Code, rec.Body.String())
	}
	stepless := createdTaskView(t, s, rec)
	if len(stepless.Steps) != 0 {
		t.Fatalf("fixture must have NO steps, got %d", len(stepless.Steps))
	}
	putFloorMember(t, s, Member{ID: "ow-stepless", Name: "O-11", Kind: KindOutsource, LinkedTaskID: &stepless.ID})
	putFloorMember(t, s, Member{ID: "ow-taskless", Name: "O-22", Kind: KindOutsource})

	got := resumeFor(t, s, "m-alpha")
	withTask := rosterRow(t, got.Roster, "ow-stepless")
	noTask := rosterRow(t, got.Roster, "ow-taskless")

	// Scenario A — a task with no steps: title and status are carried, progress
	// is 0/0 because the task is absent from the grouped step-count map.
	if withTask.CurrentTask != "尚未排步驟的外包任務" {
		t.Fatalf("stepless contractor current_task: want %q, got %q", "尚未排步驟的外包任務", withTask.CurrentTask)
	}
	if withTask.TaskStatus != TaskStatusNotStarted {
		t.Fatalf("stepless contractor task_status: want %q, got %q", TaskStatusNotStarted, withTask.TaskStatus)
	}
	if withTask.WaitingReason != "" {
		t.Fatalf("stepless contractor waiting_reason: want empty, got %q", withTask.WaitingReason)
	}
	if withTask.ProgressDone != 0 || withTask.ProgressTotal != 0 {
		t.Fatalf("stepless contractor progress: want 0/0, got %d/%d", withTask.ProgressDone, withTask.ProgressTotal)
	}
	// Scenario B — no task at all: every field degrades to its zero value.
	if noTask.CurrentTask != "" || noTask.TaskStatus != "" || noTask.WaitingReason != "" ||
		noTask.ProgressDone != 0 || noTask.ProgressTotal != 0 {
		t.Fatalf("taskless contractor must carry all-zero task fields, got %+v", noTask)
	}
	// The ambiguity itself: progress is byte-identical across the two rows.
	if withTask.ProgressDone != noTask.ProgressDone || withTask.ProgressTotal != noTask.ProgressTotal {
		t.Fatalf("progress is expected to be INDISTINGUISHABLE (%d/%d vs %d/%d)",
			withTask.ProgressDone, withTask.ProgressTotal, noTask.ProgressDone, noTask.ProgressTotal)
	}
	// …and the one field that resolves it.
	if withTask.TaskStatus == "" || noTask.TaskStatus != "" {
		t.Fatalf("task_status must separate the two 0/0 rows: stepless=%q taskless=%q",
			withTask.TaskStatus, noTask.TaskStatus)
	}
}

// TestResumeYouAreOnSurvivesTheRosterFilters pins the capture ORDER inside
// resumeFloorParts: "where am I" is read off the caller's row BEFORE the
// roster-status and warden filters, not after.
//
// It matters because this route admits callers the ROSTER deliberately drops:
// a warden token (Requires: principalMachine) and a member that was just
// deactivated. Both must still be told which machine they are standing on —
// that answer is not derivable client-side, since our hosts report the same
// name as each other.
//
// MUTANT: move the `m.ID == actor` capture below either filter — one of these
// two assertions goes red with an empty you_are_on. Before this test the
// invariant was held by a comment only: review moved the capture down and the
// whole suite stayed green.
func TestResumeYouAreOnSurvivesTheRosterFilters(t *testing.T) {
	s := floorTestServer(t)
	putFloorMember(t, s, Member{
		ID: "m-bravo", Name: "Bravo", Kind: KindStaff, RoleKey: "assistant",
		RosterStatus: RosterStatusRemoved, LastMachineID: "m-host-two",
	})
	s.telemetry.Set("m-bravo", map[string]any{"machine": "m-host-two"})

	// A warden IS a machine, so its own id is its answer.
	if got := resumeFor(t, s, "m-host-one"); got.Machines == nil || got.Machines.YouAreOn != "m-host-one" {
		t.Fatalf("a warden caller must still learn its machine, got %+v", got.Machines)
	}
	// A deactivated member is off the roster but still gets a real answer.
	if got := resumeFor(t, s, "m-bravo"); got.Machines == nil || got.Machines.YouAreOn != "m-host-two" {
		t.Fatalf("a deactivated caller must still learn its machine, got %+v", got.Machines)
	}
}

// TestResumeMachinesYouAreOnIsTheServerBinding — the caller's machine comes
// from the server-recorded binding, never from a name a host reports for
// itself: our hosts report the SAME name as each other, so a hostname-derived
// answer picks the wrong box silently.
//
// MUTANT: resolve you_are_on from anything but the caller's own binding (e.g.
// hardcode the first machine in the list) — this goes red because the fixture
// deliberately puts the caller on the SECOND machine.
func TestResumeMachinesYouAreOnIsTheServerBinding(t *testing.T) {
	s := floorTestServer(t)
	putFloorMember(t, s, Member{
		ID: "m-alpha", Name: "Alpha", Kind: KindStaff, RoleKey: "assistant",
		LastMachineID: "m-host-two",
	})
	s.telemetry.Set("m-alpha", map[string]any{"machine": "m-host-two"})

	got := resumeFor(t, s, "m-alpha")
	if got.Machines == nil {
		t.Fatal("machines block missing")
	}
	if got.Machines.YouAreOn != "m-host-two" {
		t.Fatalf("you_are_on: want m-host-two (the caller's binding), got %q", got.Machines.YouAreOn)
	}
	ids := map[string]bool{}
	for _, m := range got.Machines.List {
		ids[m.MachineID] = true
	}
	if !ids["m-host-one"] || !ids["m-host-two"] {
		t.Fatalf("machine list must carry both machines, got %+v", got.Machines.List)
	}
}

// TestResumePeekReportsTheFloorItWouldCarry — the peek and the payload are
// assembled by ONE function so their numbers cannot drift. This asserts the
// property that matters: the sizes the peek reports are the sizes of the blocks
// a real pull carries.
//
// MUTANT: compute roster_chars from anything other than the roster actually
// returned (e.g. a constant, or the untruncated duty) — the equality goes red.
func TestResumePeekReportsTheFloorItWouldCarry(t *testing.T) {
	s := floorTestServer(t)
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-verbose", Name: "Verbose Role",
		DefinitionMD: strings.Repeat("職", resumeDutyPreview+250)}); err != nil {
		t.Fatal(err)
	}
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindStaff, RoleKey: "r-verbose"})

	full := resumeFor(t, s, "m-alpha")
	if full.Overview.RosterChars != rosterChars(full.Roster) {
		t.Fatalf("roster_chars must size the roster this payload carries: reported %d, actual %d",
			full.Overview.RosterChars, rosterChars(full.Roster))
	}
	if full.Machines == nil {
		t.Fatal("machines block missing")
	}
	if full.Overview.MachinesChars != machinesChars(*full.Machines) {
		t.Fatalf("machines_chars mismatch: reported %d, actual %d",
			full.Overview.MachinesChars, machinesChars(*full.Machines))
	}
	// The peek must report the SAME numbers without carrying the content.
	rec := httptest.NewRecorder()
	s.HandlePeekResumeSummarySizeApiResumeSummarySizeGet(rec, perfReq("m-alpha", "agent"))
	if rec.Code != 200 {
		t.Fatalf("peek → %d: %s", rec.Code, rec.Body.String())
	}
	var peek resumeSummarySizeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &peek); err != nil {
		t.Fatalf("decode peek: %v", err)
	}
	if peek.Overview.RosterChars != full.Overview.RosterChars {
		t.Fatalf("peek roster_chars %d != payload roster_chars %d — the peek must not drift",
			peek.Overview.RosterChars, full.Overview.RosterChars)
	}
	if peek.Overview.MachinesChars != full.Overview.MachinesChars {
		t.Fatalf("peek machines_chars %d != payload machines_chars %d",
			peek.Overview.MachinesChars, full.Overview.MachinesChars)
	}
	// And it must stay a PEEK: sizes without content.
	if strings.Contains(rec.Body.String(), "roster\"") {
		t.Fatal("the peek must not carry the roster itself")
	}
}
