package main

// api_tasks_step_note_cap_setting_t119_test.go — T-119: the step note's ceiling
// is the `task.step_note_cap_chars` SETTING, not the chatBodyMaxChars constant.
//
// 🔴 Every case here is written so it CANNOT pass on the pre-change code, and
// the shape of the ticket is what makes that possible: the number MOVED (4000 →
// 10000) and it became ADJUSTABLE. So each case drives the value to something
// the constant never was and then observes the consequence, rather than
// asserting a field exists.
//
// The two CONTROL cases are the other half of the owner's ruling
// (rc-c8cc527bfed3): a chat message body and the task-level handover note were
// the constant's other two users and they were deliberately left behind. They
// are pinned against a step-note cap driven to its CEILING, so a change that
// pointed either of them at the new setting reddens here instead of quietly
// widening two faces nobody asked to widen.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// patchStepNoteCap drives the REAL settings write face — the one the settings
// page calls — rather than assigning the field, so the PATCH handler, the DB
// row and the validation range are all in the assertion.
func patchStepNoteCap(t *testing.T, api *apiServer, n int) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateSettingsApiSettingsPatch(rec,
		taskReq(t, http.MethodPatch, "/api/settings",
			map[string]any{"step_note_cap_chars": n}, "owner", "owner"))
	return rec
}

// stepNoteCapFromGetTask reads the ceiling a step ROW advertises — the number an
// agent budgeting a note actually sees when it reads the ticket.
func stepNoteCapFromGetTask(t *testing.T, api *apiServer, taskID, stepID string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, http.MethodGet, "/api/tasks/"+taskID, nil, "m-exec", "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	for _, st := range decodeBody[taskDTO](t, rec).Steps {
		if st.ID == stepID {
			return st.NoteCapChars
		}
	}
	t.Fatalf("step %q missing from get_task", stepID)
	return 0
}

// stepNoteCapFromGetTaskStep reads the ceiling the single-step view advertises.
func stepNoteCapFromGetTaskStep(t *testing.T, api *apiServer, taskID, stepID string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskStepApiTasksTaskIdStepsStepIdGet(rec,
		taskReq(t, http.MethodGet, "/api/tasks/"+taskID+"/steps/"+stepID, nil, "m-exec", "agent"),
		taskID, stepID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task step: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskStepDetailDTO](t, rec).NoteCapChars
}

// seedT119Step gives every case one open task with one step to write on.
func seedT119Step(t *testing.T, api *apiServer) (*apiServer, string, string) {
	t.Helper()
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec",
		[]map[string]any{{"name": "one", "dod": "d1"}})
	return api, task.ID, view.Steps[0].ID
}

// TestStepNoteCap_ReportedCeilingIsTheOneThatRefusesTheWrite is the ticket's
// core promise. Under a cap nobody ships (1500 — not the old constant, not the
// new default), every face that REPORTS a ceiling must say 1500 and a note of
// 1501 runes must be refused by both write faces, with 1500 accepted right
// beside it so the refusal is the ceiling talking.
//
// A build that kept a second literal on the reporting side would still refuse
// at 1500 and still ANSWER 4000 or 10000 — which is the exact drift this ticket
// exists to make impossible, and is why the report and the refusal are asserted
// against the same number in one case rather than in two.
func TestStepNoteCap_ReportedCeilingIsTheOneThatRefusesTheWrite(t *testing.T) {
	api, taskID, stepID := seedT119Step(t, newTasksTestServer(t))
	const cap = 1500
	if rec := patchStepNoteCap(t, api, cap); rec.Code != http.StatusOK {
		t.Fatalf("patch step_note_cap_chars=%d: %d %s", cap, rec.Code, rec.Body.String())
	}

	if got := stepNoteCapFromGetTask(t, api, taskID, stepID); got != cap {
		t.Fatalf("get_task reports note_cap_chars %d, want %d", got, cap)
	}
	if got := stepNoteCapFromGetTaskStep(t, api, taskID, stepID); got != cap {
		t.Fatalf("get_task_step reports note_cap_chars %d, want %d", got, cap)
	}

	atCap := strings.Repeat("備", cap)
	rec := writeStepNote(t, api, taskID, stepID, "m-exec", atCap)
	if rec.Code != http.StatusOK {
		t.Fatalf("a note exactly at the cap must land: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[taskStepNoteReceiptDTO](t, rec).CapChars; got != cap {
		t.Fatalf("write receipt quotes cap_chars %d, want %d", got, cap)
	}
	if rec := writeStepNote(t, api, taskID, stepID, "m-exec",
		strings.Repeat("備", cap+1)); rec.Code != http.StatusBadRequest {
		t.Fatalf("a note one rune over the cap must 400: %d %s", rec.Code, rec.Body.String())
	}

	// The anchor patch face is held to the same number — an uncapped second door
	// onto a capped field would make the reported ceiling a suggestion.
	status, data := patchStepNote(t, api, taskID, stepID, "m-exec", map[string]any{
		"edits": []any{edit("", strings.Repeat("字", 10))},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("a patch whose RESULT clears the cap must 400, got %d: %v", status, data)
	}
	if got := readStepNote(t, api, taskID, stepID); got != atCap {
		t.Fatalf("a refused write must leave the stored note verbatim")
	}
}

// TestStepNoteCap_ChatMessageBodyKeepsItsOwnFourThousand is CONTROL ①. The chat
// body shared the constant and the owner ruled it stays put, so a 4,001-char
// message must still be refused with the step-note cap driven to its ceiling —
// 100000, twenty-five times what a chat body may hold.
func TestStepNoteCap_ChatMessageBodyKeepsItsOwnFourThousand(t *testing.T) {
	api := newTasksTestServer(t)
	if rec := patchStepNoteCap(t, api, maxStepNoteCapChars); rec.Code != http.StatusOK {
		t.Fatalf("patch to the ceiling: %d %s", rec.Code, rec.Body.String())
	}

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.HandlePostChatApiChatPost(rec, taskReq(t, http.MethodPost, "/api/chat",
			map[string]any{"to": wireOwnerID, "body": body}, "m-exec", "agent"))
		return rec
	}
	if rec := post(strings.Repeat("a", chatBodyMaxChars)); rec.Code != http.StatusOK {
		t.Fatalf("a body at the 4,000 boundary must still pass: %d %s", rec.Code, rec.Body.String())
	}
	rec := post(strings.Repeat("a", chatBodyMaxChars+1))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a 4,001-char chat body must still be refused with the step note cap "+
			"at %d — the step note setting does not govern it: %d %s",
			maxStepNoteCapChars, rec.Code, rec.Body.String())
	}
	// The refusal must still quote 4000, not the step-note number: a message that
	// named the wrong limit would send an agent to shorten to the wrong length.
	if !strings.Contains(rec.Body.String(), "4000") {
		t.Fatalf("the chat refusal must still quote its own 4,000 limit: %s", rec.Body.String())
	}
}

// TestStepNoteCap_ReassignHandoverNoteKeepsItsOwnFourThousand is CONTROL ②, the
// twin of the case above for the other field that shared the constant.
func TestStepNoteCap_ReassignHandoverNoteKeepsItsOwnFourThousand(t *testing.T) {
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-old", "Ken", KindStaff)
	putActiveMember(t, api, "m-new", "Rei", KindStaff)
	task := createAdHocTask(t, api, "m-old")
	if rec := patchStepNoteCap(t, api, maxStepNoteCapChars); rec.Code != http.StatusOK {
		t.Fatalf("patch to the ceiling: %d %s", rec.Code, rec.Body.String())
	}

	rec := reassign(t, api, task.ID, map[string]any{
		"target": map[string]any{"kind": "member", "member_id": "m-new"},
		"note":   strings.Repeat("交", chatBodyMaxChars+1),
	}, "owner", "owner")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a 4,001-char handover note must still be refused with the step note "+
			"cap at %d: %d %s", maxStepNoteCapChars, rec.Code, rec.Body.String())
	}
	stored, err := api.dal.GetTask(task.ID)
	if err != nil || stored == nil || stored.ExecutorID != "m-old" || stored.HandoverNote != "" {
		t.Fatalf("the refused reassignment must leave the task untouched: %v %+v", err, stored)
	}
}

// TestStepNoteCap_LoweringLeavesAnExistingLongNoteReadableButUneditable is why
// this cap is allowed to go DOWN at all while every doc.cap_chars.* knob is not.
// The cap is checked only on WRITE, so lowering it below a stored note costs the
// owner nothing he cannot see: the whole text still reads back, and only the
// next edit is refused. A build that measured on READ — truncating, or 404ing —
// would lose an agent's handover record the moment the owner turned a knob.
func TestStepNoteCap_LoweringLeavesAnExistingLongNoteReadableButUneditable(t *testing.T) {
	api, taskID, stepID := seedT119Step(t, newTasksTestServer(t))
	long := strings.Repeat("備", 6000) // legal under the shipped 10000
	if rec := writeStepNote(t, api, taskID, stepID, "m-exec", long); rec.Code != http.StatusOK {
		t.Fatalf("seed a 6,000-rune note: %d %s", rec.Code, rec.Body.String())
	}

	if rec := patchStepNoteCap(t, api, 2000); rec.Code != http.StatusOK {
		t.Fatalf("lowering the cap below a stored note must be allowed: %d %s",
			rec.Code, rec.Body.String())
	}

	if got := readStepNote(t, api, taskID, stepID); got != long {
		t.Fatalf("the over-cap note must still read back IN FULL: got %d runes, want 6000",
			len([]rune(got)))
	}
	if got := stepNoteCapFromGetTaskStep(t, api, taskID, stepID); got != 2000 {
		t.Fatalf("the read must report the NEW ceiling %d, got %d", 2000, got)
	}
	// Uneditable until it is shortened: the same text back is still over the cap.
	if rec := writeStepNote(t, api, taskID, stepID, "m-exec", long); rec.Code != http.StatusBadRequest {
		t.Fatalf("rewriting the over-cap note must 400: %d %s", rec.Code, rec.Body.String())
	}
	// And shortening to under the new cap is the way out.
	short := strings.Repeat("備", 2000)
	if rec := writeStepNote(t, api, taskID, stepID, "m-exec", short); rec.Code != http.StatusOK {
		t.Fatalf("shortening to the new cap must land: %d %s", rec.Code, rec.Body.String())
	}
	if got := readStepNote(t, api, taskID, stepID); got != short {
		t.Fatalf("the shortened note must be what is stored")
	}
}

// TestStepNoteCap_RangeIsOneThousandToOneHundredThousand pins both boundaries
// from the PATCH face. The floor being 1000 rather than the shipped default is
// the whole difference from the doc caps — a build that copied their
// "floor == default" rule would refuse 1000 and pass everything else here.
func TestStepNoteCap_RangeIsOneThousandToOneHundredThousand(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want int
		why  string
	}{
		{999, http.StatusUnprocessableEntity, "one under the floor"},
		{minStepNoteCapChars, http.StatusOK, "the floor itself, BELOW the shipped default"},
		{maxStepNoteCapChars, http.StatusOK, "the ceiling itself"},
		{100001, http.StatusUnprocessableEntity, "one over the ceiling"},
	} {
		api := newTasksTestServer(t)
		rec := patchStepNoteCap(t, api, tc.n)
		if rec.Code != tc.want {
			t.Fatalf("step_note_cap_chars=%d (%s): got %d, want %d — %s",
				tc.n, tc.why, rec.Code, tc.want, rec.Body.String())
		}
		if tc.want != http.StatusOK {
			continue
		}
		if got := api.stepNoteCap(); got != tc.n {
			t.Fatalf("an accepted %d must be live immediately, stepNoteCap() = %d", tc.n, got)
		}
	}

	// A hand-edited DB row outside the range must stop the server at load rather
	// than install a value the PATCH face would have refused.
	api := newTasksTestServer(t)
	if err := api.dal.PutSetting(settingStepNoteCapChars, "500"); err != nil {
		t.Fatalf("seed an illegal row: %v", err)
	}
	if _, err := loadAuthSettings(api.dal, defaultConfig(), func(string) {}); err == nil {
		t.Fatalf("loading an out-of-range task.step_note_cap_chars row must fail")
	}
}

// TestStepNoteCap_UnsetIsTenThousandOnEveryFace pins the shipped default on the
// three faces that have to agree, and it is the case a revert cannot survive:
// the constant it replaced was 4000.
func TestStepNoteCap_UnsetIsTenThousandOnEveryFace(t *testing.T) {
	api, taskID, stepID := seedT119Step(t, newTasksTestServer(t))

	if got := api.stepNoteCap(); got != 10000 {
		t.Fatalf("the unset accessor must be 10000, got %d", got)
	}
	if got := stepNoteCapFromGetTaskStep(t, api, taskID, stepID); got != 10000 {
		t.Fatalf("the unset read face must report 10000, got %d", got)
	}
	rec := httptest.NewRecorder()
	api.HandleGetSettingsApiSettingsGet(rec,
		taskReq(t, http.MethodGet, "/api/settings", nil, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("get settings: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[settingsDTO](t, rec).StepNoteCapChars; got != 10000 {
		t.Fatalf("GET /api/settings must report 10000, got %d", got)
	}
	// The settings wire name is what the cockpit and the MCP descriptor bind to.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if _, ok := raw["step_note_cap_chars"]; !ok {
		t.Fatalf("GET /api/settings must carry step_note_cap_chars: %s", rec.Body.String())
	}

	loaded, err := loadAuthSettings(api.dal, defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.stepNoteCapChars != 10000 {
		t.Fatalf("boot-time load must default to 10000, got %d", loaded.stepNoteCapChars)
	}
}
