// Skeleton generated from server/ocserverd/api_tasks.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskLog(t *testing.T) {
	t.Skip("TODO: taskLog emits one task-lifecycle observability line to stderr.")
}

func TestPublishTask(t *testing.T) {
	t.Skip("TODO: ── SSE fan helpers (spec/sse.md §2.2 — hint payloads, never full bodies) ────")
}

func TestInheritDispatchSpec(t *testing.T) {
	t.Skip("TODO: inheritDispatchSpec fills the fields a 發包 left unset.")
}

func TestFillDispatchSpecFrom(t *testing.T) {
	t.Skip("TODO: fillDispatchSpecFrom copies ONE source's fields into the slots spec leaves empty — never over an already-decided field, and applying NO defaults of its own (defaults belong to defaultedDispatchSpec, once, after every source has had its turn; baked in here they would pre-empt a later source's runtime and then drop its model as \"another runtime's\").")
}

func TestDefaultedDispatchSpec(t *testing.T) {
	t.Skip("TODO: defaultedDispatchSpec applies the only two defaults there are: a runtime, and an effort.")
}

func TestPublishOutsourceWorker(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestPublishTaskManual(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestResolveTask(t *testing.T) {
	t.Skip("TODO: ── shared plumbing ────────────────────────────────────────────────────────── resolveTask returns the task for taskID (errNotFound when absent).")
}

func TestTaskDTOOf(t *testing.T) {
	t.Skip("TODO: taskDTOOf assembles the full served view of one task (steps + deps).")
}

func TestBlockingTasksOf(t *testing.T) {
	t.Skip("TODO: blockingTasksOf resolves the REVERSE dependency edge of one task (T-91): the non-terminal tasks that name it in their own blocked_by, as the same display refs the forward direction serves.")
}

func TestTaskArtifactDTOs(t *testing.T) {
	t.Skip("TODO: taskArtifactDTOs lists one task's artifacts and projects them onto the wire, resolving the referenced chat_attachment for EVERY kind since T-92 — a link's target now lives in a text/uri-list blob, so a link row needs its blob too, and it needs the BYTES rather than only the metadata.")
}

func TestReplyCardStatusesForSteps(t *testing.T) {
	t.Skip("TODO: replyCardStatusesForSteps maps each step's bound reply_card_id → the card's live status (\"waiting\"/\"answered\") for the read-time reply_card_status the task-embedded TaskReplyCard reads to lazy-load answered cards (and the board reads to derive the H4 badge without the child round-trip).")
}

func TestStepCardSettled(t *testing.T) {
	t.Skip("TODO: stepCardSettled reports whether the step's LATEST bound reply card (the reply_card_id pointer — historical cards deliberately out of scope) exists and has left waiting through a settling action (answered / expired): the submit_plan preservation test of T-1aea.")
}

func TestWriteTask(t *testing.T) {
	t.Skip("TODO: writeTask is the common single-task response tail.")
}

func TestWriteTaskWriteReceipt(t *testing.T) {
	t.Skip("TODO: writeTaskWriteReceipt is the common tail of the EIGHT task-driving writes that used to answer with the whole taskDTO (T-91): update_task and its title and description twins, claim, reassign, terminate, mark_duplicate and set_task_deps.")
}

func TestWriteTaskArtifactReceipt(t *testing.T) {
	t.Skip("TODO: writeTaskArtifactReceipt is the common tail of the two artifact writes: the artifact just touched plus the resulting set size (T-a98d — these used to answer with the whole task, ~80k characters for a one-line pin).")
}

func TestWriteTaskCloseoutReceipt(t *testing.T) {
	t.Skip("TODO: writeTaskCloseoutReceipt is the common tail of BOTH close-out exits — the first (stamping) report and the idempotent no-op repeat (T-bb70).")
}

func TestWriteTaskStepStatusReceipt(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestCallerMayDriveTask(t *testing.T) {
	t.Skip("TODO: callerMayDriveTask enforces the executor guard on the agent report routes (plan / status / step status / gate / deps): the caller must BE the task's executor — the caller-identity convention (root CLAUDE.md §14: a non-admin agent only ever operates itself; admin capability — owner or admin agent — may act on any task).")
}

func TestCallerMayEditTaskText(t *testing.T) {
	t.Skip("TODO: callerMayEditTaskText is callerMayDriveTask widened by exactly one structural fact: while a task has NO executor at all (executor_id == \"\"), its CREATOR counts as the executor — but only at the text-only doors (T-52).")
}

func TestCallerMayWriteHandover(t *testing.T) {
	t.Skip("TODO: callerMayWriteHandover is callerMayDriveTask PLUS one narrow, time-boxed exception (T-91): while a task sits under the `reassigning` lock, the PREDECESSOR stamped on it may still write the handover record.")
}

func TestTaskCallerOf(t *testing.T) {
	t.Skip("TODO: taskCallerOf resolves the caller's facets from the verified claims (the twin of resolvePrincipal that also hands back the member row).")
}

func TestAuthorizeTaskCreate(t *testing.T) {
	t.Skip("TODO: authorizeTaskCreate is the caller-side create gate of the 正職授權矩陣 (T-23cf phase 2).")
}

func TestCloseTask(t *testing.T) {
	t.Skip("TODO: closeTask applies the terminal-status side effects (done AND terminated): stamp closed_ts, retire every waiting reply card still bound to the task, release every bound outsource worker (the panel row disappears; the row itself is the audit trail) and fan their deltas.")
}

func TestNameWithIDSlot(t *testing.T) {
	t.Skip("TODO: nameWithIDSlot composes the ONE slot that has to carry TWO facts: 「銀月（mira）」.")
}

func TestDeriveAndPersistTask(t *testing.T) {
	t.Skip("TODO: deriveAndPersistTask is the DERIVATION SEAM (T-9ca5 \"任務狀態全推導\"): the single call every step-mutation path funnels through to re-project the task's status (and display waiting_reason) from its steps, persist it, and fan the delta.")
}

func TestReconcileTaskStatusesOnBoot(t *testing.T) {
	t.Skip("TODO: reconcileTaskStatusesOnBoot aligns every non-terminal task's stored status with what its steps derive to (owner T-9ca5 ⑤: 上線時既有不一致一次對齊) — a one-shot at startup after task status became fully derived.")
}

func TestManualAssignee(t *testing.T) {
	t.Skip("TODO: manualAssignee decodes a manual's assignee JSON ({} = unset → nil map).")
}

func TestResumeTasksFor(t *testing.T) {
	t.Skip("TODO: resumeTasksFor assembles the bounded task block of the wake snapshot (SPEC §6.2 — a handover resumes in-flight tasks, not just chat) as LIGHT rows (T-3f31 owner ruling: 任務不該包含細節 — no steps/DoD text ride the snapshot; each row names the task, its status/priority and the current node id + NAME, current = the first non-done step).")
}

func TestHandleListTasksApiTasksGet(t *testing.T) {
	t.Run("every task on the roster is served as a full light list row", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"desc"}`)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/tasks", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var got any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", got, []any{map[string]any{
			"id":                   "T-1",
			"task_no":              "T-1",
			"type_key":             "",
			"title":                "Ship it",
			"dedupe_key":           "",
			"duplicate_of":         "",
			"status":               "not_started",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "staff",
			"executor_id":          "kip",
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{},
			"dep_tasks":            []any{},
			"progress_done":        0,
			"progress_total":       0,
			"current_step_id":      "",
			"current_step_name":    "",
			"artifact_count":       0,
		}})
		dashboard.wantFrames()
	})

	t.Run("?status= narrows the answer to the tasks in that state", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Open one","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Closed one","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/terminate", owner, "")

		rec := apiRequest(t, h, "GET", "/api/tasks?status=terminated", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var terminated []any
		if err := json.Unmarshal(rec.Body.Bytes(), &terminated); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(terminated) != 1 {
			t.Fatalf("want exactly the terminated task, got %v", terminated)
		}
		apiWantValue(t, "body[0].id", terminated[0].(map[string]any)["id"], "T-2")
		apiWantValue(t, "body[0].title", terminated[0].(map[string]any)["title"], "Closed one")

		rec = apiRequest(t, h, "GET", "/api/tasks?status=not_started", owner, "")
		var open []any
		if err := json.Unmarshal(rec.Body.Bytes(), &open); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(open) != 1 {
			t.Fatalf("want exactly the open task, got %v", open)
		}
		apiWantValue(t, "body[0].id", open[0].(map[string]any)["id"], "T-1")
	})

	t.Run("?open=true drops the closed tasks and keeps the live ones", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Open one","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Closed one","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/terminate", owner, "")

		rec := apiRequest(t, h, "GET", "/api/tasks?open=true", owner, "")
		var live []any
		if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(live) != 1 {
			t.Fatalf("want exactly the open task, got %v", live)
		}
		apiWantValue(t, "body[0].id", live[0].(map[string]any)["id"], "T-1")
	})

	t.Run("?statuses= folds a repeated param into the union of those states", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"One","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Two","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/terminate", owner, "")

		rec := apiRequest(t, h, "GET", "/api/tasks?statuses=not_started&statuses=terminated", owner, "")
		var both []any
		if err := json.Unmarshal(rec.Body.Bytes(), &both); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(both) != 2 {
			t.Fatalf("want both tasks, got %v", both)
		}
		rec = apiRequest(t, h, "GET", "/api/tasks?statuses=done", owner, "")
		if body := rec.Body.String(); body != "[]\n" && body != "[]" {
			t.Fatalf("want an empty list for a state nothing is in, got %q", body)
		}
	})

	t.Run("?executor= keeps only the tasks that member executes", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Kip's","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Mira's","executor_member_id":"mira"}`)

		rec := apiRequest(t, h, "GET", "/api/tasks?executor=mira", owner, "")
		var mine []any
		if err := json.Unmarshal(rec.Body.Bytes(), &mine); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(mine) != 1 {
			t.Fatalf("want exactly Mira's task, got %v", mine)
		}
		apiWantValue(t, "body[0].id", mine[0].(map[string]any)["id"], "T-2")
	})

	t.Run("?type= keeps only the tasks of that type and an ad-hoc task has none", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ad-hoc","executor_member_id":"kip"}`)

		rec := apiRequest(t, h, "GET", "/api/tasks?type=daily_report", owner, "")
		if body := rec.Body.String(); body != "[]\n" && body != "[]" {
			t.Fatalf("want an empty list, got %q", body)
		}
		rec = apiRequest(t, h, "GET", "/api/tasks", owner, "")
		var all []any
		if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(all) != 1 {
			t.Fatalf("the unfiltered list must still carry the task, got %v", all)
		}
	})

	t.Run("?executor=outsource keeps only the tasks tracked on the outsource lane", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Kip's","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Contracted out","target":{"kind":"outsource"}}`)

		rec := apiRequest(t, h, "GET", "/api/tasks?executor=outsource", owner, "")
		var contracted any
		if err := json.Unmarshal(rec.Body.Bytes(), &contracted); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", contracted, []any{map[string]any{
			"id":                   "T-2",
			"task_no":              "T-2",
			"type_key":             "",
			"title":                "Contracted out",
			"dedupe_key":           "",
			"duplicate_of":         "",
			"status":               "not_started",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "outsource",
			"executor_id":          apiAnyString,
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{},
			"dep_tasks":            []any{},
			"progress_done":        0,
			"progress_total":       0,
			"current_step_id":      "",
			"current_step_name":    "",
			"artifact_count":       0,
		}})
	})

	t.Run("?executor=unassigned keeps only the outsource tasks no worker has taken yet", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		api.noOutsource = true
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Kip's","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Contracted out","target":{"kind":"outsource"}}`)

		rec := apiRequest(t, h, "GET", "/api/tasks?executor=unassigned", owner, "")
		var waiting any
		if err := json.Unmarshal(rec.Body.Bytes(), &waiting); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", waiting, []any{map[string]any{
			"id":                   "T-2",
			"task_no":              "T-2",
			"type_key":             "",
			"title":                "Contracted out",
			"dedupe_key":           "",
			"duplicate_of":         "",
			"status":               "not_started",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "outsource",
			"executor_id":          "",
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{},
			"dep_tasks":            []any{},
			"progress_done":        0,
			"progress_total":       0,
			"current_step_id":      "",
			"current_step_name":    "",
			"artifact_count":       0,
		}})
	})

	t.Run("a ?status= outside the vocabulary answers 400 listing the whole vocabulary", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks?status=nope", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"status must be one of not_started, in_progress, waiting_owner, waiting_external, reassigning, done, terminated, duplicated")
	})

	t.Run("a ?statuses= entry outside the vocabulary answers 400 naming the offending value", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks?statuses=not_started&statuses=nope", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"statuses must each be one of not_started, in_progress, waiting_owner, "+
				"waiting_external, reassigning, done, terminated, duplicated — got 'nope'")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestParseTaskStatusSet(t *testing.T) {
	t.Skip("TODO: parseTaskStatusSet folds the repeatable ?statuses= param into a lookup set.")
}

func TestTaskStatusSetMatch(t *testing.T) {
	t.Skip("TODO: taskStatusSetMatch reports whether one task belongs to a ?statuses= set.")
}

func TestHandleTaskCountApiTasksCountGet(t *testing.T) {
	t.Run("the badge counts the non-terminal tasks and the whole population", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"One","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Two","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/terminate", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"open": 1, "total": 2})
		dashboard.wantFrames()
	})

	t.Run("an empty roster counts zero open and zero total", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"open": 0, "total": 0})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/count", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetTaskApiTasksTaskIdGet(t *testing.T) {
	t.Run("the id in the path selects that task and it comes back in full", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Second","executor_member_id":"kip","description":"desc"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-2", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":                   "T-2",
			"task_no":              "T-2",
			"type_key":             "",
			"title":                "Second",
			"dedupe_key":           "",
			"inputs":               map[string]any{},
			"description":          "desc",
			"duplicate_of":         "",
			"status":               "not_started",
			"lock":                 "",
			"priority":             "mid",
			"executor_kind":        "staff",
			"executor_id":          "kip",
			"creator_id":           "owner",
			"reassigned_from":      "",
			"reassigned_from_kind": "",
			"handover_note":        "",
			"handover_note_ts":     0,
			"handover_note_by":     "",
			"waiting_reason":       "",
			"created_ts":           apiAnyNumber,
			"updated_ts":           apiAnyNumber,
			"closed_ts":            nil,
			"deps":                 []any{},
			"steps":                []any{},
			"detail_level":         "summary",
			"notes_included":       false,
			"progress_done":        0,
			"progress_total":       0,
			"closeout_reported":    false,
			"artifact_count":       0,
			"handoff":              "",
			"handoff_note":         "",
			"handoff_task_id":      "",
			"blocking":             []any{},
			"frozen_by":            "",
		})
		dashboard.wantFrames()
	})

	t.Run("the submitted plan comes back as the task's step rows", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Planned","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.steps", data["steps"], []any{map[string]any{
			"id":                apiAnyString,
			"task_id":           "T-1",
			"order_idx":         0,
			"name":              "Draft",
			"dod":               "a draft exists",
			"status":            "pending",
			"parallel_group":    "",
			"is_gate":           false,
			"reply_card_id":     "",
			"reply_card_status": "",
			"waiting_reason":    "",
			"note_size_chars":   0,
			"note_cap_chars":    10000,
			"started_ts":        0,
			"finished_ts":       0,
		}})
		apiWantValue(t, "body.progress_total", data["progress_total"], 1)
	})

	t.Run("a task another task blocks on names its waiter under blocking", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.blocking", data["blocking"], []any{map[string]any{
			"id":      "T-2",
			"task_no": "T-2",
			"title":   "Waiter",
			"status":  "not_started",
		}})
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-999", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"T","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestCallerMayTerminateTask(t *testing.T) {
	t.Skip("TODO: ── C.2 owner actions ──────────────────────────────────────────────────────── callerMayTerminateTask is the terminate gate.")
}

func TestHandleTerminateTaskApiTasksTaskIdTerminatePost(t *testing.T) {
	t.Run("terminating an open task answers the write receipt, fans the task delta and the executor's close notice", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "terminated",
			"executor_id":            "kip",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              apiAnyNumber,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "terminated"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		noticeFrame := map[string]any{
			"seq":   3,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame, noticeFrame)
		executor.wantFrames(taskFrame, noticeFrame)
		bystander.wantFrames()
	})

	t.Run("terminating an already-closed task answers 409 naming the status it is in", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", other, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/terminate", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleSetTaskPriorityApiTasksTaskIdPriorityPost(t *testing.T) {
	t.Run("setting a priority answers the receipt and fans the task delta carrying it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"high"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "priority": "high", "frozen_by": "",
		})

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "high", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("freezing records the actor that froze it and unfreezing clears that record", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", agent, `{"priority":"frozen"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "priority": "frozen", "frozen_by": "kip",
		})

		status, data = apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"low"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "priority": "low", "frozen_by": "",
		})
	})

	t.Run("a priority outside the closed set answers 400 listing the whole set", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"urgent"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "priority must be one of high, mid, low, frozen")
		dashboard.wantFrames()
	})

	t.Run("a body with no priority key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: priority")
	})

	t.Run("a body carrying a key the route does not declare answers 422 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner,
			`{"priority":"high","urgency":"very"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", `invalid request body: json: unknown field "urgency"`)
	})

	t.Run("a body that is not JSON answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", other, `{"priority":"high"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"high"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", machine, `{"priority":"high"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/priority", owner, `{"priority":"high"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", "", `{"priority":"high"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandlePostTaskMessageApiTasksTaskIdMessagePost(t *testing.T) {
	t.Run("a message on the task card answers the post receipt naming the executor and fans the chat delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner, `{"body":"any update?"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          apiAnyString,
			"to":          "kip",
			"ts":          apiAnyNumber,
			"attachments": []any{},
		})
		messageID, _ := data["id"].(string)

		chatFrame := map[string]any{
			"seq":   2,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     "owner::" + messageID,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": messageID, "from": "owner", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(chatFrame)
		executor.wantFrames(chatFrame)
		bystander.wantFrames()
	})

	t.Run("a message carrying an uploaded attachment answers the receipt naming that blob", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"report.txt","data_b64":"aGVsbG8="}`)
		attachmentID, _ := uploaded["id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner,
			`{"body":"see this","attachments":[{"id":"`+attachmentID+`"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": apiAnyString,
			"to": "kip",
			"ts": apiAnyNumber,
			"attachments": []any{map[string]any{
				"id":       attachmentID,
				"url":      "/api/chat/attachment/" + attachmentID,
				"filename": "",
				"mime":     "application/octet-stream",
				"is_image": false,
			}},
		})
	})

	t.Run("a body that is not JSON answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner, `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
	})

	t.Run("a message carrying neither text nor an attachment answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner, `{"body":"  "}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "message must carry text or an attachment")
		dashboard.wantFrames()
	})

	t.Run("more than ten attachments answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		items := strings.TrimSuffix(strings.Repeat(`{"id":"att-nope"},`, 11), ",")
		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner,
			`{"body":"hi","attachments":[`+items+`]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "a message may carry at most 10 attachments")
	})

	t.Run("an attachment id the store does not carry is refused", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner,
			`{"body":"hi","attachments":[{"id":"att-nope"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment 'att-nope' not found")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", agent, `{"body":"hi"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/message", owner, `{"body":"hi"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", "", `{"body":"hi"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReassignTaskApiTasksTaskIdReassignPost(t *testing.T) {
	t.Run("handing a task to another member answers the receipt under the reassigning lock and pairs both sides in chat", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		predecessor := apiTestListen(t, api, "kip")
		successor := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"},"note":"context is on the ticket"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "not_started",
			"executor_id":            "mira",
			"executor_kind":          "staff",
			"lock":                   "reassigning",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		predecessorNotice := map[string]any{
			"seq":   2,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		successorNotice := map[string]any{
			"seq":   3,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "mira"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		newAudienceDelta := map[string]any{
			"seq":   4,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   4,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		oldAudienceDelta := map[string]any{
			"seq":   5,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   5,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(predecessorNotice, successorNotice, newAudienceDelta, oldAudienceDelta)
		predecessor.wantFrames(predecessorNotice, oldAudienceDelta)
		successor.wantFrames(successorNotice, newAudienceDelta)
	})

	t.Run("handing a planned task to the outsource lane resets its live steps and leaves it unassigned under the lock", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"outsource","runtime":"claude","effort":"high"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "not_started",
			"executor_id":            "",
			"executor_kind":          "outsource",
			"lock":                   "reassigning",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         1,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		_, handedOver := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "body.steps", handedOver["steps"], []any{map[string]any{
			"id":                stepID,
			"task_id":           "T-1",
			"order_idx":         0,
			"name":              "Draft",
			"dod":               "a draft exists",
			"status":            "pending",
			"parallel_group":    "",
			"is_gate":           false,
			"reply_card_id":     "",
			"reply_card_status": "",
			"waiting_reason":    "",
			"note_size_chars":   0,
			"note_cap_chars":    10000,
			"started_ts":        0,
			"finished_ts":       0,
		}})
		apiWantValue(t, "body.reassigned_from", handedOver["reassigned_from"], "kip")
		apiWantValue(t, "body.reassigned_from_kind", handedOver["reassigned_from_kind"], "staff")
	})

	t.Run("an outsource target naming a machine nothing carries answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"outsource","machine":"m-nosuchhost"}}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'm-nosuchhost' not found")
	})

	t.Run("a target naming the task's current executor answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"kip"}}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "member 'kip' is already the task's executor")
		dashboard.wantFrames()
	})

	t.Run("a staff target with no member_id answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.member_id is required for kind 'staff'")
	})

	t.Run("a target member nobody carries answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"nobody"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target member 'nobody' is not an active roster member")
	})

	t.Run("a target that is a machine answers 400 saying machines never execute tasks", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"m-server-self"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"target member 'm-server-self' is a machine (warden) — machines never execute tasks")
	})

	t.Run("the pre-rename executor kind answers 400 saying it was renamed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"member","member_id":"mira"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`target.kind: task executor kind "member" was renamed to "staff" (T-101); `+
				`the closed set is {"staff", "outsource"}`)
	})

	t.Run("an outsource target with an effort outside the closed set answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"outsource","effort":"turbo"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.effort must be one of low, medium, high, max")
	})

	t.Run("an outsource target with a runtime outside the closed set answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"outsource","runtime":"gemini"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.runtime must be 'claude' or 'codex'")
	})

	t.Run("a plain agent handing its own task to another member answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", agent,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"only the owner or an admin agent may reassign a task to another member; 發包 to an outsource worker instead")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", other,
			`{"target":{"kind":"staff","member_id":"kip"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("a body with no target key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: target")
	})

	t.Run("a handover note over the character cap answers 400 naming both numbers", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"},"note":"`+strings.Repeat("x", 4001)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "handover note is 4001 chars, over the 4000-char limit")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", machine,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", "",
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleClaimTaskApiTasksTaskIdClaimPost(t *testing.T) {
	t.Run("the successor claiming a handed-over task clears the lock and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		successorToken := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")
		successor := apiTestListen(t, api, "mira")
		predecessor := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", successorToken, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Ship it",
			"status":                 "not_started",
			"executor_id":            "mira",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		taskFrame := map[string]any{
			"seq":   6,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   6,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "mira",
		}
		dashboard.wantFrames(taskFrame)
		successor.wantFrames(taskFrame)
		predecessor.wantFrames()
	})

	t.Run("claiming a task handed over from an outsource worker clears the lock and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource"}}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`)
		successorToken := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", successorToken, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-1",
			"title":                  "Contracted out",
			"status":                 "not_started",
			"executor_id":            "mira",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})
	})

	t.Run("a task under no handover lock answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is not awaiting takeover (no reassigning lock)")
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", other, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/claim", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/claim", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestExecutorLabel(t *testing.T) {
	t.Skip("TODO: executorLabel resolves a human-facing label for a task executor given its kind + id (T-ba04 handover pairing): a member's display name (falling back to its id), or \"外包 <codename>\" for an outsource worker (falling back to its id).")
}

func TestPostTaskChat(t *testing.T) {
	t.Skip("TODO: postTaskChat posts one server-authored task-context chat message (the reassign handover notices — the task-message route's meta shape: task_id / task_title / task_type ride along for the client linkage).")
}

func TestHandleCreateTaskApiTasksPost(t *testing.T) {
	t.Run("an ad-hoc task naming its executor answers the create receipt and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","description":"the whole story"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "staff",
			"executor_id":   "kip",
			"deduped":       false,
		})

		taskFrame := map[string]any{
			"seq":   1,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an explicit priority rides onto the created task's delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","priority":"high"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "staff",
			"executor_id":   "kip",
			"deduped":       false,
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "high", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("the created task's ticket numbers run in the order they were opened", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, first := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"First","executor_member_id":"kip"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, first)
		}
		apiWantBody(t, first, map[string]any{
			"task_id": "T-1", "executor_kind": "staff", "executor_id": "kip", "deduped": false,
		})
		status, second := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Second","executor_member_id":"kip"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, second)
		}
		apiWantBody(t, second, map[string]any{
			"task_id": "T-2", "executor_kind": "staff", "executor_id": "kip", "deduped": false,
		})
	})

	t.Run("a typed task the manual assigns takes its executor and dedupe identity from that manual", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/task-manuals", owner,
			`{"type_key":"daily_report","display_name":"Daily report","assignee":{"kind":"staff","member_id":"kip"}}`)
		apiJSON(t, h, "POST", "/api/task-manuals/daily_report", owner,
			`{"fields":[{"name":"day","required":true,"is_key":true},{"name":"note"}]}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", agent,
			`{"title":"Monday report","type_key":"daily_report","inputs":{"day":"mon","stray":"x"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "staff",
			"executor_id":   "kip",
			"deduped":       false,
			"warnings": []any{
				"unknown input field 'stray' (not defined in manual 'daily_report')",
			},
		})

		status, again := apiJSON(t, h, "POST", "/api/tasks", agent,
			`{"title":"Monday report again","type_key":"daily_report","inputs":{"day":"mon"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, again)
		}
		apiWantBody(t, again, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "staff",
			"executor_id":   "kip",
			"deduped":       true,
			"title":         "Monday report",
			"status":        "not_started",
		})
	})

	t.Run("a typed task missing a required input answers 400 naming the field", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/task-manuals", owner,
			`{"type_key":"daily_report","display_name":"Daily report","assignee":{"kind":"staff","member_id":"kip"}}`)
		apiJSON(t, h, "POST", "/api/task-manuals/daily_report", owner,
			`{"fields":[{"name":"day","required":true,"is_key":true}]}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", agent,
			`{"title":"Monday report","type_key":"daily_report","inputs":{}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "required input 'day' is missing")
	})

	t.Run("a typed task the manual assigns to somebody else answers 403 naming that member", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/task-manuals", owner,
			`{"type_key":"daily_report","display_name":"Daily report","assignee":{"kind":"staff","member_id":"kip"}}`)
		apiJSON(t, h, "POST", "/api/task-manuals/daily_report", owner,
			`{"fields":[{"name":"day","required":true,"is_key":true}]}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Monday report","type_key":"daily_report","inputs":{"day":"mon"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"a typed task assigned to member 'kip' may only be created by that member")
	})

	t.Run("an explicit outsource dispatch lands the task unassigned on the outsource lane", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource","runtime":"claude","effort":"high"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":       "T-1",
			"executor_kind": "outsource",
			"executor_id":   "",
			"deduped":       false,
		})
		dashboard.wantFrames(
			map[string]any{
				"seq":   1,
				"topic": "task",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "task",
					"key":     "owner::T-1",
					"epoch":   1,
					"deleted": false,
					"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
				},
				"ts":      apiAnyNumber,
				"trigger": "owner",
			},
			map[string]any{
				"seq":   2,
				"topic": "outsource_worker",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "outsource_worker",
					"key":     apiAnyString,
					"epoch":   2,
					"deleted": false,
					"payload": map[string]any{"id": apiAnyString, "codename": "X-1", "status": "assigned"},
				},
				"ts":      apiAnyNumber,
				"trigger": "server",
			},
			map[string]any{
				"seq":   3,
				"topic": "task",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "task",
					"key":     "owner::T-1",
					"epoch":   3,
					"deleted": false,
					"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
				},
				"ts":      apiAnyNumber,
				"trigger": "server",
			},
			map[string]any{
				"seq":   4,
				"topic": "outsource_worker",
				"op":    "patch",
				"data": map[string]any{
					"entity":  "outsource_worker",
					"key":     apiAnyString,
					"epoch":   4,
					"deleted": false,
					"payload": map[string]any{"id": apiAnyString, "codename": "X-1", "status": "assigned"},
				},
				"ts":      apiAnyNumber,
				"trigger": "server",
			},
		)
	})

	t.Run("an outsource dispatch with a runtime outside the closed set answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource","runtime":"gemini"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.runtime must be 'claude' or 'codex'")
	})

	t.Run("an outsource dispatch with an effort outside the closed set answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource","effort":"turbo"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "target.effort must be one of low, medium, high, max")
	})

	t.Run("an outsource dispatch naming a machine nothing carries answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Contracted out","target":{"kind":"outsource","machine":"m-nosuchhost"}}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "machine 'm-nosuchhost' not found")
	})

	t.Run("a body with no title key answers 422 naming the missing field", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: title")
		dashboard.wantFrames()
	})

	t.Run("a blank title answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"   "}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "title must not be blank")
	})

	t.Run("an ad-hoc task with no executor answers 400 saying the field is required", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "executor_member_id is required for an ad-hoc task")
	})

	t.Run("a priority outside the closed set answers 400 listing the whole set", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","priority":"urgent"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "priority must be one of high, mid, low, frozen")
	})

	t.Run("the pre-rename executor kind answers 400 saying it was renamed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","executor_member_id":"kip","target":{"kind":"member"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			`target.kind: task executor kind "member" was renamed to "staff" (T-101); `+
				`the closed set is {"staff", "outsource"}`)
	})

	t.Run("a type_key no manual carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
			`{"title":"Ship it","type_key":"daily_report","executor_member_id":"kip"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task manual 'daily_report' not found")
	})

	t.Run("a plain agent naming somebody else as executor answers 403", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", agent,
			`{"title":"Ship it","executor_member_id":"mira"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"an ad-hoc task may only name yourself as executor (or be dispatched to an outsource worker)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks", machine,
			`{"title":"Ship it","executor_member_id":"kip"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks", "",
			`{"title":"Ship it","executor_member_id":"kip"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleSubmitTaskPlanApiTasksTaskIdPlanPost(t *testing.T) {
	t.Run("a fresh plan answers the plan receipt and fans the re-derived task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "steps_total": 2, "progress_done": 0, "progress_total": 2,
		})

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("a replan of a task whose last step already closed it answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done","handoff":"none","handoff_note":"nothing follows"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (done)")
	})

	t.Run("a replan over an unfinished plan replaces the pending steps wholesale", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Rewrite","dod":"the rewrite lands"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "steps_total": 1, "progress_done": 0, "progress_total": 1,
		})

		_, replanned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		apiWantValue(t, "body.steps", replanned["steps"], []any{map[string]any{
			"id":                apiAnyString,
			"task_id":           "T-1",
			"order_idx":         0,
			"name":              "Rewrite",
			"dod":               "the rewrite lands",
			"status":            "pending",
			"parallel_group":    "",
			"is_gate":           false,
			"reply_card_id":     "",
			"reply_card_status": "",
			"waiting_reason":    "",
			"note_size_chars":   0,
			"note_cap_chars":    10000,
			"started_ts":        0,
			"finished_ts":       0,
		}})
	})

	t.Run("a replan keeps a finished step in place and appends only the steps it did not already carry", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks", agent, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", agent, "")
		firstStepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+firstStepID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+firstStepID+"/status", agent, `{"status":"done"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Rewrite","dod":"the rewrite lands"}]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id": "T-1", "steps_total": 2, "progress_done": 1, "progress_total": 2,
		})

		_, replanned := apiJSON(t, h, "GET", "/api/tasks/T-1", agent, "")
		apiWantValue(t, "body.steps", replanned["steps"], []any{
			map[string]any{
				"id":                firstStepID,
				"task_id":           "T-1",
				"order_idx":         0,
				"name":              "Draft",
				"dod":               "a draft exists",
				"status":            "done",
				"parallel_group":    "",
				"is_gate":           false,
				"reply_card_id":     "",
				"reply_card_status": "",
				"waiting_reason":    "",
				"note_size_chars":   0,
				"note_cap_chars":    10000,
				"started_ts":        apiAnyNumber,
				"finished_ts":       apiAnyNumber,
			},
			map[string]any{
				"id":                apiAnyString,
				"task_id":           "T-1",
				"order_idx":         1,
				"name":              "Rewrite",
				"dod":               "the rewrite lands",
				"status":            "pending",
				"parallel_group":    "",
				"is_gate":           false,
				"reply_card_id":     "",
				"reply_card_status": "",
				"waiting_reason":    "",
				"note_size_chars":   0,
				"note_cap_chars":    10000,
				"started_ts":        0,
				"finished_ts":       0,
			},
		})
	})

	t.Run("a parallel group holding a single lane answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists","parallel_group":"g1"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"parallel_group 'g1' holds only one step — running in parallel takes at least two; "+
				"drop the parallel_group to keep the step sequential")
	})

	t.Run("a replan that would close a task somebody else opened is refused until the ball is declared", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		firstStepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+firstStepID+"/status", agent, `{"status":"in_progress"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+firstStepID+"/status", agent, `{"status":"done"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"task 'T-1' was created by 'owner' but executed by 'kip': this plan leaves EVERY step "+
				"done, which CLOSES the task, and a closed task can never be replanned. A plan "+
				"carries no handoff declaration, so hand the ball over first, one of two ways: "+
				"(1) keep ONE unfinished step in this plan, then declare the handover on the "+
				"update_step_status report that closes it (handoff='return_to_creator' | "+
				"'follow_up' + handoff_task_id | 'none' + handoff_note) — this route always works, "+
				"and the server adds the dependency edge itself; or (2) create the successor task "+
				"(create_task) and point its blocked_by at this task (set_task_deps) — this gate "+
				"then stands aside by itself, and closing this task releases the successor. NOTE: "+
				"set_task_deps requires you to be the successor's executor (or an owner), so this "+
				"route only works when the successor is assigned to you; otherwise it answers 403 "+
				"and you want route (1).")
		dashboard.wantFrames()
	})

	t.Run("a plan with no steps at all answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner, `{"steps":[]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "a plan must have at least one step")
		dashboard.wantFrames()
	})

	t.Run("a step with a blank name answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner,
			`{"steps":[{"name":"  ","dod":"a draft exists"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "step name must not be blank")
	})

	t.Run("a step with a blank definition of done answers 400 naming the step", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner,
			`{"steps":[{"name":"Draft","dod":"   "}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "step 'Draft' must have a non-empty definition of done")
	})

	t.Run("a body with no steps key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: steps")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", other,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", owner,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", machine,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/plan", owner,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", "",
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleMarkTaskDuplicateApiTasksTaskIdDuplicatePost(t *testing.T) {
	t.Run("marking a task duplicated closes it pointing at the original, fans the delta and the executor's close notice", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/duplicate", owner, `{"duplicate_of":"T-1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-2",
			"title":                  "The twin",
			"status":                 "duplicated",
			"executor_id":            "kip",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              apiAnyNumber,
			"duplicate_of":           "T-1",
			"deps":                   []any{},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-2",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-2", "priority": "mid", "status": "duplicated"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		noticeFrame := map[string]any{
			"seq":   4,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   4,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame, noticeFrame)
		executor.wantFrames(taskFrame, noticeFrame)
		bystander.wantFrames()
	})

	t.Run("pointing a task at itself answers 409", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/duplicate", owner, `{"duplicate_of":"T-1"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "a task cannot be marked a duplicate of itself")
		dashboard.wantFrames()
	})

	t.Run("pointing at a task that is itself a duplicate answers 409 naming the final original", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The third","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/duplicate", owner, `{"duplicate_of":"T-1"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-3/duplicate", owner, `{"duplicate_of":"T-2"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"duplicate_of task 'T-2' is itself a duplicate; point at the final original it duplicates (T-1)")
	})

	t.Run("a task already cited as an original cannot itself be marked duplicated", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The third","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/duplicate", owner, `{"duplicate_of":"T-1"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/duplicate", owner, `{"duplicate_of":"T-3"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is already the original of another duplicate; it cannot itself be marked duplicated")
	})

	t.Run("pointing at an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/duplicate", owner, `{"duplicate_of":"T-999"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "duplicate_of task 'T-999' not found")
	})

	t.Run("a blank duplicate_of answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/duplicate", owner, `{"duplicate_of":"  "}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "duplicate_of must not be blank")
	})

	t.Run("a body with no duplicate_of key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/duplicate", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: duplicate_of")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/duplicate", other, `{"duplicate_of":"T-1"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/terminate", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/duplicate", owner, `{"duplicate_of":"T-1"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-2' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/duplicate", machine, `{"duplicate_of":"T-1"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/duplicate", owner, `{"duplicate_of":"T-1"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The original","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"The twin","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/duplicate", "", `{"duplicate_of":"T-1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(t *testing.T) {
	t.Run("reporting a step in progress answers the step receipt and fans the re-derived task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"in_progress"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"step_id":        stepID,
			"step_status":    "in_progress",
			"waiting_reason": "",
			"task_status":    "in_progress",
			"closed_ts":      nil,
			"progress_done":  0,
			"progress_total": 2,
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "in_progress"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("entering waiting_external records the reason on both the step and the task", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"waiting_external","waiting_reason":"the vendor has not answered"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"step_id":        stepID,
			"step_status":    "waiting_external",
			"waiting_reason": "the vendor has not answered",
			"task_status":    "waiting_external",
			"closed_ts":      nil,
			"progress_done":  0,
			"progress_total": 1,
		})
	})

	t.Run("entering waiting_external with no reason answers 422", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"waiting_external"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"waiting_reason is required when entering waiting_external")
		dashboard.wantFrames()
	})

	t.Run("the report that finishes the last step closes the task once the ball is declared", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done","handoff":"none","handoff_note":"nothing follows this"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"step_id":        stepID,
			"step_status":    "done",
			"waiting_reason": "",
			"task_status":    "done",
			"closed_ts":      apiAnyNumber,
			"progress_done":  1,
			"progress_total": 1,
		})

		taskFrame := map[string]any{
			"seq":   4,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   4,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "done"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		noticeFrame := map[string]any{
			"seq":   5,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   5,
				"deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "system", "to": "kip"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame, noticeFrame)
		executor.wantFrames(taskFrame, noticeFrame)
		bystander.wantFrames()
	})

	t.Run("the report that would close a task somebody else opened is refused until the ball is declared", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent, `{"status":"in_progress"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"task 'T-1' was created by 'owner' but executed by 'kip': this report would CLOSE it, "+
				"and a closed task can never be replanned (submit_plan turns into a permanent 409). "+
				"Say where the ball goes, in THIS same update_step_status call, with one of: "+
				"handoff='return_to_creator' (recorded on this task and nothing else — no task is "+
				"opened and nobody is notified); handoff='follow_up' + handoff_task_id='<the "+
				"successor task you already created>' (the server attaches this task to it as a "+
				"dependency, and closing this one releases it); handoff='none' + "+
				"handoff_note='<why nothing follows>' (an explicit end of the line, recorded).")
		dashboard.wantFrames()
	})

	t.Run("a transition the step machine does not allow answers 409 naming both ends", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"done"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "illegal step transition 'pending' -> 'done'")
	})

	t.Run("reporting waiting_owner answers 400 because only a reply card opens that hold", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"waiting_owner"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"waiting_owner is not an agent-reportable status; a step enters it only "+
				"by opening a reply card (create_reply_card with linked_task)")
	})

	t.Run("reporting superseded answers 400 because only a replan freezes a step", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		_, planned := apiJSON(t, h, "GET", "/api/tasks/T-1", owner, "")
		stepID, _ := planned["steps"].([]any)[0].(map[string]any)["id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+stepID+"/status", agent,
			`{"status":"superseded"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"superseded is not agent-reportable; the server freezes a replaced "+
				"step itself when a new plan is submitted (submit_plan)")
	})

	t.Run("a status outside the closed set answers 400 listing the whole set", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", owner,
			`{"status":"halfway"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"status must be one of pending, in_progress, waiting_owner, waiting_external, done, superseded")
	})

	t.Run("a step id this task does not carry answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", owner,
			`{"status":"in_progress"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "step 'ts-whatever' not found")
	})

	t.Run("a body with no status key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: status")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", other,
			`{"status":"in_progress"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", owner,
			`{"status":"in_progress"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", machine,
			`{"status":"in_progress"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/steps/ts-whatever/status", owner,
			`{"status":"in_progress"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/ts-whatever/status", "",
			`{"status":"in_progress"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestArmStepWithCard(t *testing.T) {
	t.Skip("TODO: armStepWithCard applies the card→step waiting state machine behind the ONE card-open path — create_reply_card carrying an explicit linked_task {task_id, step_id} (T-18 collapsed the two entrances into it): the step enters waiting_owner carrying the CURRENT card (reply_card_id points at the latest ask; the card's own task/step birth marks keep the full history), started_ts stamps on first touch, and the task follows into waiting_owner — UNLESS the step sits inside a parallel group, where flipping the WHOLE task would lie while sibling lanes still run (the ValidatePlanParallelShape rationale).")
}

func TestHandleSetTaskDepsApiTasksTaskIdDepsPost(t *testing.T) {
	t.Run("replacing the blocking list answers the write receipt carrying it and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":                "T-2",
			"title":                  "Waiter",
			"status":                 "not_started",
			"executor_id":            "kip",
			"executor_kind":          "staff",
			"lock":                   "",
			"closed_ts":              nil,
			"duplicate_of":           "",
			"deps":                   []any{"T-1"},
			"progress_done":          0,
			"progress_total":         0,
			"artifact_count":         0,
			"description_size_chars": 0,
			"description_sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-2",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-2", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an empty list clears the blocking edges a task already carried", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":["T-1"]}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner, `{"blocked_by":[]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.deps", data["deps"], []any{})
	})

	t.Run("a repeated blocker is stored once", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Blocker","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/deps", owner,
			`{"blocked_by":["T-1","T-1"]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "body.deps", data["deps"], []any{"T-1"})
	})

	t.Run("a task naming itself as a blocker answers 422", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-1"]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "a task cannot block on itself")
		dashboard.wantFrames()
	})

	t.Run("a blocker no task carries answers 422 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":["T-999"]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "unknown blocking task 'T-999'")
	})

	t.Run("a body with no blocked_by key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: blocked_by")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", other, `{"blocked_by":[]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 naming the status it is in", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", owner, `{"blocked_by":[]}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict", "task 'T-1' is already closed (terminated)")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", machine, `{"blocked_by":[]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/deps", owner, `{"blocked_by":[]}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Waiter","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/deps", "", `{"blocked_by":[]}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReportTaskCloseoutApiTasksTaskIdCloseoutPost(t *testing.T) {
	t.Run("the first close-out report stamps the moment, answers the receipt and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":           "T-1",
			"task_status":       "terminated",
			"closeout_reported": true,
			"closeout_ts":       apiAnyNumber,
		})

		taskFrame := map[string]any{
			"seq":   4,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   4,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "terminated"},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("a repeat report answers the original stamp again and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")
		_, first := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", owner, "")
		firstStamp, _ := first["closeout_ts"].(float64)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":           "T-1",
			"task_status":       "terminated",
			"closeout_reported": true,
			"closeout_ts":       firstStamp,
		})
		dashboard.wantFrames()
	})

	t.Run("a task that is still open answers 409 naming the status it is in", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is still open (not_started) — close-out is reported after the task ends")
		dashboard.wantFrames()
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", other, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/closeout", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/closeout", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestArtifactTextOrError(t *testing.T) {
	t.Skip("TODO: artifactTextOrError validates the pair and writes the 400 itself, returning ok=false when it did.")
}

func TestMintLinkTargetBlob(t *testing.T) {
	t.Skip("TODO: mintLinkTargetBlob turns a link target into the blob that will hold it, and returns the id to point the artifact at.")
}

func TestHandleAddTaskArtifactApiTasksTaskIdArtifactPost(t *testing.T) {
	t.Run("pinning a link answers the artifact receipt with the new set size and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    apiAnyString,
			"artifact_count": 1,
		})

		taskFrame := map[string]any{
			"seq":   2,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("pinning a second deliverable answers the grown set size", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #124","url":"https://example.com/pr/124"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    apiAnyString,
			"artifact_count": 2,
		})
	})

	t.Run("pinning an uploaded file answers the artifact receipt", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"report.txt","data_b64":"aGVsbG8="}`)
		attachmentID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+attachmentID+`"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    apiAnyString,
			"artifact_count": 1,
		})
	})

	t.Run("an attachment_id reserved for a member avatar is refused", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"image","name":"the face","attachment_id":"ava-kip"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"attachment 'ava-kip' is reserved for a member avatar")
	})

	t.Run("a kind outside the closed set answers 400 listing the whole set", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"video","name":"PR #123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "kind must be one of file, image, link")
		dashboard.wantFrames()
	})

	t.Run("a blank name answers 400 asking for a display name", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"  ","url":"https://example.com/pr/123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"name is required: give this deliverable a short display name")
	})

	t.Run("a name over the character cap answers 400 naming both numbers", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"`+strings.Repeat("x", 49)+`","url":"https://example.com/pr/123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "artifact name is 49 chars, over the 48-char limit")
	})

	t.Run("a description over the character cap answers 400 naming both numbers", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123","description":"`+strings.Repeat("x", 257)+
				`","url":"https://example.com/pr/123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact description is 257 chars, over the 256-char limit")
	})

	t.Run("a link with no url answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url is required for a link artifact")
	})

	t.Run("a link url outside the scheme whitelist answers 400", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123","url":"javascript:alert(1)"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url must start with https:// or http://")
	})

	t.Run("a link url over the character cap answers 400 naming both numbers", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123","url":"https://example.com/`+strings.Repeat("x", 2029)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url is 2049 chars, over the 2048-char limit")
	})

	t.Run("a file with no attachment_id answers 400 naming the kind", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"file","name":"the report"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment_id is required for a file artifact")
	})

	t.Run("an attachment_id the store does not carry is refused with the upload instruction", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"file","name":"the report","attachment_id":"att-nosuchblob"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"attachment 'att-nosuchblob' not found (upload it first via POST /api/chat/attachments)")
	})

	t.Run("a body with no kind key answers 422 naming the missing field", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: kind")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", other,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 saying its deliverables are frozen", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", owner,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is closed (terminated) — its deliverables are frozen")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", machine,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/artifact", owner,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", "",
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleRemoveTaskArtifactApiTasksTaskIdArtifactArtifactIdDelete(t *testing.T) {
	t.Run("un-pinning a deliverable answers the shrunk set size and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/"+artifactID, agent, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 0,
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an artifact id nothing carries answers 404 naming it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-nosucharttifact", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "artifact 'ta-nosucharttifact' not found")
		dashboard.wantFrames()
	})

	t.Run("an artifact pinned on another task answers 400 naming both", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Second","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-2/artifact/"+artifactID, agent, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact '"+artifactID+"' does not belong to task 'T-2'")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-whatever", other, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 saying its deliverables are frozen", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/"+artifactID, owner, "")
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is closed (terminated) — its deliverables are frozen")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-whatever", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-999/artifact/ta-whatever", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "DELETE", "/api/tasks/T-1/artifact/ta-whatever", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestTaskFrozenDeliverablesRefusal(t *testing.T) {
	t.Skip("TODO: taskFrozenDeliverablesRefusal is the ONE sentence all three artifact verbs refuse a closed task with.")
}

func TestArtifactOnTask(t *testing.T) {
	t.Skip("TODO: artifactOnTask resolves the (task, artifact) pair the per-artifact routes address and answers every guard they share, in the ONE order the wire documents for the WRITE verbs: 404 task → 403 not the executor (admin excepted, §14) → 409 the task is closed → 404 artifact → 400 the artifact belongs to a different task.")
}

func TestHandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost(t *testing.T) {
	t.Run("swapping a link's target answers the replace receipt with the grown version count and fans the task delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 1,
			"version_count":  2,
		})

		taskFrame := map[string]any{
			"seq":   3,
			"topic": "task",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "task",
				"key":     "owner::T-1",
				"epoch":   3,
				"deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "mid", "status": "not_started"},
			},
			"ts":      apiAnyNumber,
			"trigger": "kip",
		}
		dashboard.wantFrames(taskFrame)
		executor.wantFrames(taskFrame)
		bystander.wantFrames()
	})

	t.Run("an omitted name carries the pinned one forward and an explicit one replaces it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/125","name":"PR #125","description":"the follow-up"}`)

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{
			map[string]any{
				"id":            apiAnyNumber,
				"kind":          "link",
				"url":           "https://example.com/pr/124",
				"name":          "PR #123",
				"description":   "the change itself",
				"filename":      "",
				"mime":          "",
				"is_image":      false,
				"attachment_id": apiAnyString,
				"created_ts":    apiAnyNumber,
				"created_by":    "kip",
			},
			map[string]any{
				"id":            apiAnyNumber,
				"kind":          "link",
				"url":           "https://example.com/pr/123",
				"name":          "PR #123",
				"description":   "the change itself",
				"filename":      "",
				"mime":          "",
				"is_image":      false,
				"attachment_id": apiAnyString,
				"created_ts":    apiAnyNumber,
				"created_by":    "kip",
			},
		})
	})

	t.Run("swapping a file's blob answers the replace receipt and the previous blob is retained as a version", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, firstBlob := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		firstBlobID, _ := firstBlob["id"].(string)
		_, secondBlob := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v2.txt","data_b64":"d29ybGQ="}`)
		secondBlobID, _ := secondBlob["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+firstBlobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"attachment_id":"`+secondBlobID+`","description":"the corrected numbers"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"task_id":        "T-1",
			"artifact_id":    artifactID,
			"artifact_count": 1,
			"version_count":  2,
		})

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{map[string]any{
			"id":            apiAnyNumber,
			"kind":          "file",
			"url":           "/api/chat/attachment/" + firstBlobID,
			"name":          "the report",
			"description":   "",
			"filename":      "",
			"mime":          "application/octet-stream",
			"is_image":      false,
			"attachment_id": firstBlobID,
			"created_ts":    apiAnyNumber,
			"created_by":    "kip",
		}})
	})

	t.Run("a url on a file artifact answers 400 saying the kind cannot change", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		blobID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+blobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact kind cannot change across versions: this artifact is a file and the "+
				"replacement asks for a link — un-pin it and register a new artifact instead")
	})

	t.Run("a file replacement with no attachment_id answers 400 naming the kind", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		blobID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+blobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent, `{}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "attachment_id is required for a file artifact")
	})

	t.Run("a file replacement naming an attachment reserved for a member avatar is refused", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		blobID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+blobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"attachment_id":"ava-kip"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"attachment 'ava-kip' is reserved for a member avatar")
	})

	t.Run("a file replacement naming an attachment the store does not carry is refused", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		_, uploaded := apiJSON(t, h, "POST", "/api/chat/attachments", owner,
			`{"filename":"v1.txt","data_b64":"aGVsbG8="}`)
		blobID, _ := uploaded["id"].(string)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"file","name":"the report","attachment_id":"`+blobID+`"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"attachment_id":"att-nosuchblob"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"attachment 'att-nosuchblob' not found (upload it first via POST /api/chat/attachments)")
	})

	t.Run("a body that is not JSON answers 422", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-whatever/replace", owner, `{`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: unexpected end of JSON input")
	})

	t.Run("a name over the character cap answers 400 naming both numbers", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124","name":"`+strings.Repeat("x", 49)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "artifact name is 49 chars, over the 48-char limit")
	})

	t.Run("a description over the character cap answers 400 naming both numbers", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124","description":"`+strings.Repeat("x", 257)+`"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact description is 257 chars, over the 256-char limit")
	})

	t.Run("an explicit kind that differs from the pinned one answers 400 saying the kind cannot change", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"kind":"file","attachment_id":"att-whatever"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact kind cannot change across versions: this artifact is a link and the "+
				"replacement asks for a file — un-pin it and register a new artifact instead")
		dashboard.wantFrames()
	})

	t.Run("an attachment_id on a link answers 400 saying the kind cannot change", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"attachment_id":"att-whatever"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact kind cannot change across versions: this artifact is a link and the "+
				"replacement asks for a file — un-pin it and register a new artifact instead")
	})

	t.Run("a blank name answers 400 telling the caller to omit it instead", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124","name":"  "}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"name cannot be blank: omit it to keep the name this deliverable already has")
	})

	t.Run("a link replacement with no url answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent, `{}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url is required for a link artifact")
	})

	t.Run("a link replacement url outside the scheme whitelist answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"ftp://example.com/pr/124"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "url must start with https:// or http://")
	})

	t.Run("an artifact id nothing carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-nosucharttifact/replace",
			owner, `{"url":"https://example.com/pr/124"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "artifact 'ta-nosucharttifact' not found")
	})

	t.Run("an artifact pinned on another task answers 400 naming both", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Second","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-2/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact '"+artifactID+"' does not belong to task 'T-2'")
	})

	t.Run("an agent that is not the task's executor answers 403", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"mira"}`)
		other := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-whatever/replace", other,
			`{"url":"https://example.com/pr/124"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "caller is not the task's executor")
	})

	t.Run("a closed task answers 409 saying its deliverables are frozen", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", owner,
			`{"url":"https://example.com/pr/124"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is closed (terminated) — its deliverables are frozen")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-whatever/replace", machine,
			`{"url":"https://example.com/pr/124"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-999/artifact/ta-whatever/replace", owner,
			`{"url":"https://example.com/pr/124"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/ta-whatever/replace", "",
			`{"url":"https://example.com/pr/124"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestWriteTaskArtifactReplaceReceipt(t *testing.T) {
	t.Skip("TODO: writeTaskArtifactReplaceReceipt is the bounded answer BOTH replace doors give — the JSON one and the raw-body one.")
}

func TestArtifactLinkURLRefusal(t *testing.T) {
	t.Skip("TODO: artifactLinkURLRefusal validates a link artifact's url and answers the refusal sentence, or \"\" when the url passes.")
}

func TestArtifactKindRefusal(t *testing.T) {
	t.Skip("TODO: artifactKindRefusal is the one sentence every cross-kind replacement is refused with — written once so the three ways to ask for one (an explicit kind, a url on a file, an attachment_id on a link) cannot answer differently about the same rule.")
}

func TestHandleListTaskArtifactHistoryApiTasksTaskIdArtifactArtifactIdHistoryGet(t *testing.T) {
	t.Run("the retained versions come back newest first with the target each one pointed at", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","description":"the change itself","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		dashboard := apiTestListen(t, api, "")

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{map[string]any{
			"id":            apiAnyNumber,
			"kind":          "link",
			"url":           "https://example.com/pr/123",
			"name":          "PR #123",
			"description":   "the change itself",
			"filename":      "",
			"mime":          "",
			"is_image":      false,
			"attachment_id": apiAnyString,
			"created_ts":    apiAnyNumber,
			"created_by":    "kip",
		}})
		dashboard.wantFrames()
	})

	t.Run("an artifact nobody has replaced yet has an empty history", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{})
	})

	t.Run("a closed task still serves its deliverable's history", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		apiJSON(t, h, "POST", "/api/tasks/T-1/artifact/"+artifactID+"/replace", agent,
			`{"url":"https://example.com/pr/124"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/terminate", owner, "")

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions []any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		if len(versions) != 1 {
			t.Fatalf("want the one retained version, got %v", versions)
		}
		apiWantValue(t, "body[0].url", versions[0].(map[string]any)["url"], "https://example.com/pr/123")
	})

	t.Run("an agent that is not the task's executor still reads the history", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)
		other := apiTestAgentToken(t, api, "mira", "")

		rec := apiRequest(t, h, "GET", "/api/tasks/T-1/artifact/"+artifactID+"/history", other, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var versions any
		if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "body", versions, []any{})
	})

	t.Run("an artifact id nothing carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifact/ta-nosucharttifact/history", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "artifact 'ta-nosucharttifact' not found")
	})

	t.Run("an artifact pinned on another task answers 400 naming both", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"First","executor_member_id":"kip"}`)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Second","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		_, pinned := apiJSON(t, h, "POST", "/api/tasks/T-1/artifact", agent,
			`{"kind":"link","name":"PR #123","url":"https://example.com/pr/123"}`)
		artifactID, _ := pinned["artifact_id"].(string)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-2/artifact/"+artifactID+"/history", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"artifact '"+artifactID+"' does not belong to task 'T-2'")
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		machine := apiTestAgentToken(t, api, "m-server-self", "")

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifact/ta-whatever/history", machine, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("an id no task carries answers 404 naming it", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-999/artifact/ta-whatever/history", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-999' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)

		status, data := apiJSON(t, h, "GET", "/api/tasks/T-1/artifact/ta-whatever/history", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}
