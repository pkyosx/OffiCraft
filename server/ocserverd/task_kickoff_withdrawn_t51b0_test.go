// T-51b0 — the kickoff seam is withdrawn (owner 2026-08-15, card
// rc-a4f6a7f8cd71). T-e77f had the server post "you can start now" notices to an
// outsource executor on three transitions; this file pins what happens instead.
//
// 🔴 WHY IT NEEDS A POSITIVE CONTROL. Every assertion here is "no message was
// posted", and that is also what a fixture too broken to post ANY message
// produces. So each absence is measured against a live delivery to the SAME
// recipient in the SAME server: the dependency-release notice, which stayed.
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func systemChatsTo(t *testing.T, api *apiServer, recipient string) []ChatMessage {
	t.Helper()
	msgs, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	var out []ChatMessage
	for _, m := range msgs {
		if m.Sender == wireSystemSender && m.Recipient == recipient {
			out = append(out, m)
		}
	}
	return out
}

func boundOutsourceTask(t *testing.T, api *apiServer, title string) (Task, string) {
	t.Helper()
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 5)
	created := createOutsourceTask(t, api, "review-pr", title)
	api.runOutsourceTick(1000.0)
	bound, err := api.dal.GetTask(created.ID)
	if err != nil || bound == nil || bound.ExecutorID == "" {
		t.Fatalf("fixture must bind a worker: %+v %v", bound, err)
	}
	return *bound, bound.ExecutorID
}

func setDeps(t *testing.T, api *apiServer, taskID string, blockedBy ...string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleSetTaskDepsApiTasksTaskIdDepsPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"blocked_by": blockedBy},
			wireOwnerID, "owner"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("set deps: %d %s", rec.Code, rec.Body.String())
	}
}

// Binding a task to a worker says nothing to it. What used to justify the notice
// — "a worker that boots and then idles has no other durable prompt" — is now
// covered where the gap actually is: a fresh worker carries the task in its own
// boot context, and a worker that is already up is woken by its sidecar when SSE
// comes up (cli/ocwarden: codexPostBootWake).
func TestAssignmentPostsNoKickoffToTheBoundWorker(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, worker := boundOutsourceTask(t, api, "指派不再發訊息")

	if got := systemChatsTo(t, api, worker); len(got) != 0 {
		t.Fatalf("the bind posted %d system message(s) to the worker; the kickoff "+
			"seam is withdrawn:\n%s", len(got), got[0].Body)
	}

	// POSITIVE CONTROL — the same recipient in the same server CAN be reached,
	// so the zero above is a decision and not a dead fixture.
	blocker := createAdHocTask(t, api, "m-front")
	setDeps(t, api, task.ID, blocker.ID)
	terminateTask(t, api, blocker.ID)
	if got := systemChatsTo(t, api, worker); len(got) != 1 {
		t.Fatalf("control: the release notice must reach this worker, got %d", len(got))
	}
}

// The dependency release is the one notice that stayed, and it is now the SAME
// one a member gets — T-e77f had diverted an outsource dependent to the kickoff
// seam, so withdrawing the seam restores the pre-T-e77f behaviour rather than
// leaving a contractor with nothing. Told-nobody is the failure this guards:
// a worker that correctly refused to advance a blocked task, and was never told
// the blocker went away, stalls forever and looks idle while doing it.
func TestDependencyReleaseNotifiesAnOutsourceExecutorLikeAMember(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, worker := boundOutsourceTask(t, api, "依賴解除仍要通知")

	blocker := createAdHocTask(t, api, "m-front")
	setDeps(t, api, task.ID, blocker.ID)
	before := len(systemChatsTo(t, api, worker))

	terminateTask(t, api, blocker.ID)

	got := systemChatsTo(t, api, worker)
	if len(got) != before+1 {
		t.Fatalf("a blocker closing must post exactly one notice: had %d, now %d",
			before, len(got))
	}
	body := got[len(got)-1].Body
	for _, want := range []string{TaskNo(task.ID), TaskNo(blocker.ID), "不再擋著你"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the release notice must name both tasks and what changed, "+
				"missing %q in:\n%s", want, body)
		}
	}
	// And it must be the shared notice, not the withdrawn kickoff wording coming
	// back under a different call site.
	if strings.Contains(body, "現在可以開始推進了") {
		t.Fatalf("the withdrawn kickoff wording is back:\n%s", body)
	}
}

// The unfreeze was the transition T-e77f was built for, and it is the one that
// goes quiet again. Recorded rather than assumed: the owner withdrew the seam
// knowing this, and the replacement is the sidecar wake, not another notice.
func TestUnfreezePostsNoKickoff(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, worker := boundOutsourceTask(t, api, "解凍不再發訊息")

	if rec := setPriority(t, api, task.ID, wireOwnerID, "owner", TaskPriorityFrozen); rec.Code != http.StatusOK {
		t.Fatalf("freeze: %d %s", rec.Code, rec.Body.String())
	}
	before := len(systemChatsTo(t, api, worker))
	if rec := setPriority(t, api, task.ID, wireOwnerID, "owner", TaskPriorityHigh); rec.Code != http.StatusOK {
		t.Fatalf("unfreeze: %d %s", rec.Code, rec.Body.String())
	}
	if got := systemChatsTo(t, api, worker); len(got) != before {
		t.Fatalf("an unfreeze must post nothing now: had %d, now %d\n%s",
			before, len(got), got[len(got)-1].Body)
	}
}
