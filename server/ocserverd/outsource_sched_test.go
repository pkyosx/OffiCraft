package main

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestOutsourceAwaitingAssignment(t *testing.T) {
	base := Task{
		ExecutorKind: TaskExecutorOutsource,
		Priority:     TaskPriorityMid,
		Status:       TaskStatusNotStarted,
	}
	cases := []struct {
		name string
		task Task
		want bool
	}{
		{name: "a fresh unassigned outsource task is queued", task: base, want: true},
		{name: "a reassigning outsource task is queued even when its derived status is in progress", task: func() Task {
			task := base
			task.Status = TaskStatusInProgress
			task.Lock = TaskLockReassigning
			return task
		}(), want: true},
		{name: "an in-progress task without the reassigning lock is not queued", task: func() Task {
			task := base
			task.Status = TaskStatusInProgress
			return task
		}(), want: false},
		{name: "a task already bound to a worker is not queued", task: func() Task {
			task := base
			task.ExecutorID = "ow-existing"
			return task
		}(), want: false},
		{name: "a staff task is not queued", task: func() Task {
			task := base
			task.ExecutorKind = TaskExecutorStaff
			return task
		}(), want: false},
		{name: "a frozen outsource task is never queued", task: func() Task {
			task := base
			task.Priority = TaskPriorityFrozen
			return task
		}(), want: false},
		{name: "a frozen reassigning task stays out of the queue", task: func() Task {
			task := base
			task.Priority = TaskPriorityFrozen
			task.Status = TaskStatusInProgress
			task.Lock = TaskLockReassigning
			return task
		}(), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := outsourceAwaitingAssignment(tc.task); got != tc.want {
				t.Fatalf("outsourceAwaitingAssignment(%+v) = %v, want %v", tc.task, got, tc.want)
			}
		})
	}
}

func TestTaskPriorityRank(t *testing.T) {
	cases := []struct {
		priority string
		want     int
	}{
		{priority: TaskPriorityHigh, want: 0},
		{priority: TaskPriorityMid, want: 1},
		{priority: TaskPriorityLow, want: 2},
		{priority: TaskPriorityFrozen, want: 3},
		{priority: "unrecognised", want: 3},
	}

	for _, tc := range cases {
		t.Run(tc.priority, func(t *testing.T) {
			if got := taskPriorityRank(tc.priority); got != tc.want {
				t.Fatalf("taskPriorityRank(%q) = %d, want %d", tc.priority, got, tc.want)
			}
		})
	}
}

func TestSortOutsourceQueue(t *testing.T) {
	candidate := func(id, priority string, createdTS float64) outsourceCandidate {
		return outsourceCandidate{TaskID: id, Priority: priority, CreatedTS: createdTS}
	}
	cases := []struct {
		name  string
		cands []outsourceCandidate
		want  []string
	}{
		{
			name: "priority is applied before creation time",
			cands: []outsourceCandidate{
				candidate("low-first", TaskPriorityLow, 1),
				candidate("high-late", TaskPriorityHigh, 30),
				candidate("mid-first", TaskPriorityMid, 0),
				candidate("high-first", TaskPriorityHigh, 10),
			},
			want: []string{"high-first", "high-late", "mid-first", "low-first"},
		},
		{
			name: "the task id breaks equal-priority and equal-time ties",
			cands: []outsourceCandidate{
				candidate("T-20", TaskPriorityMid, 12),
				candidate("T-2", TaskPriorityMid, 12),
				candidate("T-1", TaskPriorityMid, 12),
			},
			want: []string{"T-1", "T-2", "T-20"},
		},
		{
			name: "frozen and unknown priorities share the final rank and use the task id tie-break",
			cands: []outsourceCandidate{
				candidate("unknown-late", "unknown", 20),
				candidate("frozen-first", TaskPriorityFrozen, 1),
				candidate("unknown-first", "unknown", 1),
			},
			want: []string{"frozen-first", "unknown-first", "unknown-late"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sortOutsourceQueue(tc.cands)
			ids := make([]string, len(got))
			for i := range got {
				ids[i] = got[i].TaskID
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("sorted task ids = %v, want %v", ids, tc.want)
			}
		})
	}
}

func TestOutsourceDecide(t *testing.T) {
	candidate := func(id, typeKey, priority string, createdTS float64) outsourceCandidate {
		return outsourceCandidate{
			TaskID: id, TypeKey: typeKey, Priority: priority, CreatedTS: createdTS,
		}
	}
	assignment := func(id, typeKey, runtime, model, effort, machine string, fromTarget bool) outsourceAssignment {
		return outsourceAssignment{
			TaskID: id, TypeKey: typeKey, Runtime: runtime, Model: model,
			Effort: effort, Machine: machine, FromTarget: fromTarget,
		}
	}

	t.Run("a zero global cap pauses every assignment", func(t *testing.T) {
		got := outsourceDecide(
			[]outsourceCandidate{candidate("T-1", "tm-a", TaskPriorityHigh, 1)},
			map[string]outsourceTypeSpec{"tm-a": {Copies: 0, Runtime: RuntimeClaude}},
			map[string]int{}, 0, 0,
		)
		if got != nil {
			t.Fatalf("zero-cap decisions = %+v, want nil", got)
		}
	})

	t.Run("a negative global cap is unlimited and each admission advances the per-type count", func(t *testing.T) {
		cands := []outsourceCandidate{
			candidate("low-a", "tm-a", TaskPriorityLow, 30),
			candidate("high-a", "tm-a", TaskPriorityHigh, 10),
			candidate("mid-b", "tm-b", TaskPriorityMid, 20),
			candidate("high-a", "tm-a", TaskPriorityHigh, 11),
			candidate("frozen", "tm-a", TaskPriorityFrozen, 1),
			candidate("no-manual", "tm-missing", TaskPriorityHigh, 1),
		}
		liveByType := map[string]int{"tm-a": 0, "tm-b": 0}
		before := map[string]int{"tm-a": 0, "tm-b": 0}
		got := outsourceDecide(cands, map[string]outsourceTypeSpec{
			"tm-a": {Copies: 2, Runtime: RuntimeClaude, Model: "sonnet", Effort: "high", Machine: "m-a"},
			"tm-b": {Copies: 1, Runtime: RuntimeCodex, Model: "gpt-5", Effort: "medium", Machine: "m-b"},
		}, liveByType, 0, -1)
		want := []outsourceAssignment{
			assignment("high-a", "tm-a", RuntimeClaude, "sonnet", "high", "m-a", false),
			assignment("mid-b", "tm-b", RuntimeCodex, "gpt-5", "medium", "m-b", false),
			assignment("low-a", "tm-a", RuntimeClaude, "sonnet", "high", "m-a", false),
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("decisions = %+v, want %+v", got, want)
		}
		if !reflect.DeepEqual(liveByType, before) {
			t.Fatalf("liveByType was mutated to %+v, want %+v", liveByType, before)
		}
	})

	t.Run("the global cap is folded after each admitted task", func(t *testing.T) {
		cands := []outsourceCandidate{
			candidate("high", "tm-a", TaskPriorityHigh, 1),
			candidate("mid", "tm-b", TaskPriorityMid, 2),
			candidate("low", "tm-c", TaskPriorityLow, 3),
		}
		specs := map[string]outsourceTypeSpec{
			"tm-a": {Runtime: RuntimeClaude},
			"tm-b": {Runtime: RuntimeClaude},
			"tm-c": {Runtime: RuntimeClaude},
		}
		got := outsourceDecide(cands, specs, map[string]int{}, 1, 3)
		want := []outsourceAssignment{
			assignment("high", "tm-a", RuntimeClaude, "", "medium", "", false),
			assignment("mid", "tm-b", RuntimeClaude, "", "medium", "", false),
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("decisions = %+v, want %+v", got, want)
		}
	})

	t.Run("a full type cap skips that type while another type can still fit", func(t *testing.T) {
		cands := []outsourceCandidate{
			candidate("same-type", "tm-a", TaskPriorityHigh, 1),
			candidate("other-type", "tm-b", TaskPriorityMid, 2),
		}
		got := outsourceDecide(cands, map[string]outsourceTypeSpec{
			"tm-a": {Copies: 1, Runtime: RuntimeClaude},
			"tm-b": {Copies: 1, Runtime: RuntimeCodex},
		}, map[string]int{"tm-a": 1}, 1, -1)
		want := []outsourceAssignment{
			assignment("other-type", "tm-b", RuntimeCodex, "", "medium", "", false),
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("decisions = %+v, want %+v", got, want)
		}
	})

	t.Run("an explicit target supplies its own worker fields but still inherits the type copies cap", func(t *testing.T) {
		c := candidate("target", "tm-a", TaskPriorityHigh, 1)
		c.TargetRuntime = " codex "
		c.TargetModel = "gpt-5"
		c.TargetEffort = "xhigh"
		c.TargetMachine = "m-target"
		c.Dispatched = true
		got := outsourceDecide([]outsourceCandidate{c}, map[string]outsourceTypeSpec{
			"tm-a": {Copies: 1, Runtime: RuntimeClaude, Model: "sonnet", Effort: "low", Machine: "m-manual"},
		}, map[string]int{}, 0, -1)
		want := []outsourceAssignment{
			assignment("target", "tm-a", RuntimeCodex, "gpt-5", "xhigh", "m-target", true),
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("decisions = %+v, want %+v", got, want)
		}

		c.TypeKey = ""
		got = outsourceDecide([]outsourceCandidate{c}, map[string]outsourceTypeSpec{}, map[string]int{}, 0, 1)
		want = []outsourceAssignment{
			assignment("target", "", RuntimeCodex, "gpt-5", "xhigh", "m-target", true),
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("typeless target decisions = %+v, want %+v", got, want)
		}
	})
}

func TestFillTypeSpecFromSnapshot(t *testing.T) {
	candidate := func(runtime, model, effort, machine string) outsourceCandidate {
		return outsourceCandidate{
			TargetRuntime: runtime, TargetModel: model,
			TargetEffort: effort, TargetMachine: machine,
		}
	}
	cases := []struct {
		name string
		spec outsourceTypeSpec
		c    outsourceCandidate
		want outsourceTypeSpec
	}{
		{
			name: "blank manual fields inherit the matching snapshot and preserve copies",
			spec: outsourceTypeSpec{Copies: 2, Runtime: RuntimeClaude},
			c:    candidate(RuntimeClaude, "sonnet", "high", "m-box"),
			want: outsourceTypeSpec{Copies: 2, Runtime: RuntimeClaude, Model: "sonnet", Effort: "high", Machine: "m-box"},
		},
		{
			name: "an empty manual runtime takes the snapshot runtime and model together",
			spec: outsourceTypeSpec{Copies: 0},
			c:    candidate(RuntimeCodex, "gpt-5", "xhigh", "m-codex"),
			want: outsourceTypeSpec{Copies: 0, Runtime: RuntimeCodex, Model: "gpt-5", Effort: "xhigh", Machine: "m-codex"},
		},
		{
			name: "a snapshot from another runtime cannot supply its model",
			spec: outsourceTypeSpec{Copies: 1, Runtime: RuntimeClaude},
			c:    candidate(RuntimeCodex, "gpt-5", "high", "m-codex"),
			want: outsourceTypeSpec{Copies: 1, Runtime: RuntimeClaude, Effort: "high", Machine: "m-codex"},
		},
		{
			name: "manual decisions win and the only defaults are runtime and effort",
			spec: outsourceTypeSpec{Copies: 3, Model: "opus", Machine: "m-manual"},
			c:    candidate("", "sonnet", "", "m-snapshot"),
			want: outsourceTypeSpec{Copies: 3, Runtime: RuntimeClaude, Model: "opus", Effort: "medium", Machine: "m-manual"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fillTypeSpecFromSnapshot(tc.spec, tc.c); got != tc.want {
				t.Fatalf("resolved spec = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestOutsourceSpecOf(t *testing.T) {
	cases := []struct {
		name     string
		assignee string
		want     *outsourceTypeSpec
	}{
		{name: "an empty assignee is unset", assignee: "", want: nil},
		{name: "an empty object is unset", assignee: "{}", want: nil},
		{name: "a staff assignee is not an outsource spec", assignee: `{"kind":"staff","id":"kip"}`, want: nil},
		{name: "invalid JSON is ignored", assignee: `{"kind":"outsource"`, want: nil},
		{
			name:     "omitted fields use the manual defaults",
			assignee: `{"kind":"outsource"}`,
			want:     &outsourceTypeSpec{Copies: 1, Runtime: RuntimeClaude, Effort: "medium"},
		},
		{
			name:     "provided fields are trimmed and copies zero means unlimited",
			assignee: `{"kind":"outsource","runtime":" codex ","model":" gpt-5 ","effort":" high ","copies":0,"machine":" m-box "}`,
			want:     &outsourceTypeSpec{Copies: 0, Runtime: RuntimeCodex, Model: "gpt-5", Effort: "high", Machine: "m-box"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := outsourceSpecOf(TaskManual{Assignee: tc.assignee})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("outsourceSpecOf(%q) = %+v, want %+v", tc.assignee, got, tc.want)
			}
		})
	}
}

func TestOutsourceLog(t *testing.T) {
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	previous := os.Stderr
	os.Stderr = writePipe
	t.Cleanup(func() {
		os.Stderr = previous
		readPipe.Close()
		writePipe.Close()
	})

	outsourceLog("assigned %s (%d)", "T-1", 2)
	if err := writePipe.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	got, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatalf("read captured log: %v", err)
	}
	if string(got) != "[outsource] assigned T-1 (2)\n" {
		t.Fatalf("captured log = %q", got)
	}
}

func outsourceSchedTestQueuedTask(t *testing.T, d *DAL, id, typeKey string) Task {
	t.Helper()
	return dalPutTask(t, d, Task{
		ID:           id,
		TypeKey:      typeKey,
		Title:        "Queued outsource work",
		Inputs:       map[string]any{},
		Status:       TaskStatusNotStarted,
		Priority:     TaskPriorityHigh,
		ExecutorKind: TaskExecutorOutsource,
		CreatorID:    wireOwnerID,
		CreatedTS:    1700000000,
		UpdatedTS:    1700000000,
	})
}

func TestRunOutsourceTick(t *testing.T) {
	t.Run("a queued outsource task gets a freshly minted worker", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		manual := TaskManual{
			TypeKey:     "tm-scheduler",
			DisplayName: "Scheduler manual",
			Fields:      "[]",
			Assignee:    `{"kind":"outsource","runtime":"codex","model":"gpt-5","effort":"high"}`,
		}
		dalPutManual(t, d, manual)
		task := outsourceSchedTestQueuedTask(t, d, "T-scheduler", manual.TypeKey)

		api.runOutsourceTick(1700000100)

		gotTask, err := d.GetTask(task.ID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if gotTask == nil {
			t.Fatal("GetTask: no task")
		}
		if !strings.HasPrefix(gotTask.ExecutorID, "ow-") {
			t.Fatalf("task executor_id = %q, want a minted outsource worker", gotTask.ExecutorID)
		}
		workers, err := d.ListOutsourceWorkers()
		if err != nil {
			t.Fatalf("ListOutsourceWorkers: %v", err)
		}
		if len(workers) != 1 {
			t.Fatalf("worker count = %d, want 1", len(workers))
		}
		worker := workers[0]
		if worker.ID != gotTask.ExecutorID || worker.TaskID != task.ID {
			t.Fatalf("worker binding = (%q, %q), want (%q, %q)", worker.ID, worker.TaskID, gotTask.ExecutorID, task.ID)
		}
		if worker.Runtime != RuntimeCodex || worker.Model != "gpt-5" || worker.Effort != "high" {
			t.Fatalf("worker launch spec = (%q, %q, %q), want (%q, %q, %q)", worker.Runtime, worker.Model, worker.Effort, RuntimeCodex, "gpt-5", "high")
		}
		if worker.Status != WorkerStatusAssigned || worker.DesiredState != DesiredStateOnline {
			t.Fatalf("worker lifecycle = (%q, %q), want (%q, %q)", worker.Status, worker.DesiredState, WorkerStatusAssigned, DesiredStateOnline)
		}
		if worker.LastOp != reconcileCmdStart || !strings.HasPrefix(worker.LastOpReason, "no_machine_selected:") {
			t.Fatalf("worker placement result = (%q, %q), want a no-machine fail-closed start stamp", worker.LastOp, worker.LastOpReason)
		}
	})

	t.Run("past the handover timeout an outsource predecessor is released and loses the hold", func(t *testing.T) {
		f := newHandoverFixture(t)
		f.must(t, "POST", "/api/tasks", f.owner,
			`{"title":"Contracted out","target":{"kind":"outsource","model":"sonnet","effort":"high"}}`)
		_, task := apiJSON(t, f.h, "GET", "/api/tasks/T-3", f.owner, "")
		worker, _ := task["executor_id"].(string)
		if !strings.HasPrefix(worker, "ow-") {
			t.Fatalf("fixture: want a bound worker on T-3, got %v", task)
		}
		pred := apiTestAgentToken(t, f.api, worker, "")
		admin := apiTestAgentToken(t, f.api, "mira", "")
		f.must(t, "POST", "/api/tasks/T-3/plan", pred, `{"steps":[{"name":"a","dod":"d"},{"name":"b","dod":"d"}]}`)
		f.must(t, "POST", "/api/tasks/T-3/reassign", f.owner, `{"target":{"kind":"staff","member_id":"rex"}}`)
		_, task = apiJSON(t, f.h, "GET", "/api/tasks/T-3", f.owner, "")
		steps := task["steps"].([]any)
		stepA, _ := steps[0].(map[string]any)["id"].(string)
		stepB, _ := steps[1].(map[string]any)["id"].(string)

		f.must(t, "POST", "/api/tasks/T-3/steps/"+stepA+"/status", pred, `{"status":"in_progress"}`)
		f.must(t, "POST", "/api/tasks/T-3/steps/"+stepB+"/status", pred, `{"status":"in_progress"}`)
		open := func(token, linked string) string {
			card := f.must(t, "POST", "/api/reply-cards", token,
				`{"kind":"decision","summary":"q","options":[{"text":"yes"}],"linked_task":`+linked+`}`)
			id, _ := card["id"].(string)
			return id
		}
		predBound := open(pred, `{"task_id":"T-3","step_id":"`+stepA+`"}`)
		adminBound := open(admin, `{"task_id":"T-3","step_id":"`+stepB+`"}`)
		predUnbound := open(pred, "null")

		f.api.runOutsourceTick(nowSecs() + 1801)

		_, member := apiJSON(t, f.h, "GET", "/api/members/"+worker, f.owner, "")
		if member["status"] != "released" || member["roster_status"] != "removed" {
			t.Fatalf("the reaped predecessor: want released/removed, got %v/%v", member["status"], member["roster_status"])
		}
		expired := map[string]any{"status": "expired", "expired_ts": apiAnyNumber, "answered_ts": nil, "answer": nil}
		waiting := map[string]any{"status": "waiting", "expired_ts": nil, "answered_ts": nil, "answer": nil}
		for name, c := range map[string]struct {
			id   string
			want map[string]any
		}{
			"the predecessor's card on T-3":  {predBound, expired},
			"the admin's card on T-3":        {adminBound, waiting},
			"the predecessor's unbound card": {predUnbound, waiting},
		} {
			_, card := apiJSON(t, f.h, "GET", "/api/reply-cards/"+c.id, f.owner, "")
			apiWantValue(t, name, any(map[string]any{
				"status": card["status"], "expired_ts": card["expired_ts"],
				"answered_ts": card["answered_ts"], "answer": card["answer"],
			}), any(c.want))
		}

		for _, door := range []struct{ method, path, body string }{
			{"POST", "/api/tasks/T-3/priority", `{"priority":"high"}`},
			{"POST", "/api/tasks/T-3/steps/" + stepA + "/note", `{"note":"late"}`},
			{"POST", "/api/tasks/T-3/steps/" + stepA + "/status", `{"status":"done"}`},
			{"POST", "/api/tasks/T-3/artifact", `{"kind":"link","name":"x","url":"https://example.com/x"}`},
			{"POST", "/api/tasks/T-3/claim", ""},
		} {
			for who, token := range map[string]string{"the reaped predecessor": pred, "the successor": f.successor} {
				if door.path == "/api/tasks/T-3/claim" && who == "the successor" {
					continue
				}
				status, data := apiJSON(t, f.h, door.method, door.path, token, door.body)
				if status != 403 {
					t.Fatalf("%s %s by %s: want 403, got %d %v", door.method, door.path, who, status, data)
				}
				apiWantError(t, data, "forbidden", "caller is not the task's executor")
			}
		}
		f.must(t, "POST", "/api/tasks/T-3/claim", f.successor, "")
		f.must(t, "POST", "/api/tasks/T-3/priority", f.successor, `{"priority":"high"}`)
	})

	t.Run("the handover timeout follows the station setting", func(t *testing.T) {
		f := newHandoverFixture(t)
		f.must(t, "PATCH", "/api/settings", f.owner, `{"reassign_handover_timeout_secs":120}`)
		f.must(t, "POST", "/api/tasks", f.owner,
			`{"title":"Contracted out","target":{"kind":"outsource","model":"sonnet","effort":"high"}}`)
		_, task := apiJSON(t, f.h, "GET", "/api/tasks/T-3", f.owner, "")
		worker, _ := task["executor_id"].(string)
		f.must(t, "POST", "/api/tasks/T-3/reassign", f.owner, `{"target":{"kind":"staff","member_id":"rex"}}`)
		row, err := f.api.dal.GetTask("T-3")
		if err != nil || row == nil {
			t.Fatalf("GetTask: %v", err)
		}
		state := func() string {
			_, member := apiJSON(t, f.h, "GET", "/api/members/"+worker, f.owner, "")
			return member["status"].(string) + "/" + member["roster_status"].(string)
		}

		f.api.runOutsourceTick(row.UpdatedTS + 119)
		if got := state(); got != "assigned/active" {
			t.Fatalf("at 119s the predecessor must stay live, got %s", got)
		}
		f.api.runOutsourceTick(row.UpdatedTS + 120)
		if got := state(); got != "released/removed" {
			t.Fatalf("at 120s the predecessor must be reclaimed, got %s", got)
		}
	})

	t.Run("before the handover timeout an outsource predecessor keeps the hold", func(t *testing.T) {
		f := newHandoverFixture(t)
		f.must(t, "POST", "/api/tasks", f.owner,
			`{"title":"Contracted out","target":{"kind":"outsource","model":"sonnet","effort":"high"}}`)
		_, task := apiJSON(t, f.h, "GET", "/api/tasks/T-3", f.owner, "")
		worker, _ := task["executor_id"].(string)
		pred := apiTestAgentToken(t, f.api, worker, "")
		f.must(t, "POST", "/api/tasks/T-3/reassign", f.owner, `{"target":{"kind":"staff","member_id":"rex"}}`)

		f.api.runOutsourceTick(nowSecs() + 1740)

		_, member := apiJSON(t, f.h, "GET", "/api/members/"+worker, f.owner, "")
		if member["status"] != "assigned" || member["roster_status"] != "active" {
			t.Fatalf("the predecessor must stay live, got %v/%v", member["status"], member["roster_status"])
		}
		f.must(t, "POST", "/api/tasks/T-3/priority", pred, `{"priority":"high"}`)
	})
}

func TestOutsourceTickNow(t *testing.T) {
	cases := []struct {
		name         string
		noOutsource  bool
		wantAssigned bool
	}{
		{name: "the disabled producer does not mint a worker", noOutsource: true, wantAssigned: false},
		{name: "the enabled producer runs the immediate assignment tick", noOutsource: false, wantAssigned: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, _, d, _ := newAPITestServer(t)
			manual := TaskManual{
				TypeKey:  "tm-event",
				Fields:   "[]",
				Assignee: `{"kind":"outsource","runtime":"claude","model":"sonnet"}`,
			}
			dalPutManual(t, d, manual)
			task := outsourceSchedTestQueuedTask(t, d, "T-event", manual.TypeKey)
			api.noOutsource = tc.noOutsource

			api.outsourceTickNow()

			gotTask, err := d.GetTask(task.ID)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			workers, err := d.ListOutsourceWorkers()
			if err != nil {
				t.Fatalf("ListOutsourceWorkers: %v", err)
			}
			assigned := gotTask != nil && strings.HasPrefix(gotTask.ExecutorID, "ow-")
			if assigned != tc.wantAssigned {
				t.Fatalf("assigned = %v from task %+v, want %v", assigned, gotTask, tc.wantAssigned)
			}
			wantWorkers := 0
			if tc.wantAssigned {
				wantWorkers = 1
			}
			if len(workers) != wantWorkers {
				t.Fatalf("worker count = %d, want %d", len(workers), wantWorkers)
			}
		})
	}
}
