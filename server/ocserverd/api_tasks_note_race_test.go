package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── T-e271 節點 6:步驟備註的並行覆蓋 ─────────────────────────────────────────
//
// THE HAZARD, in the same shape node 3 measured one table over: PutTaskStep is
// a whole-row upsert with no optimistic lock, and every OTHER step writer is a
// load-mutate-save — dal.GetTaskStep → mutate one field → dal.PutTaskStep.
// Nothing links that read to that write, so a second writer landing in the
// window has its change replayed away: the upsert asserts EVERY column as the
// first handler read them.
//
// The WRITING side was already safe: api_tasks_note.go writes through
// dal.SetTaskStepNote, a single-column UPDATE. The danger came from the other
// side — update_step_status (api_tasks.go), armStepWithCard (open_gate /
// create_reply_card auto-bind), and the reassign step-reset loop all replay the
// note they happened to read a moment earlier, destroying a handover note the
// note endpoint had already answered 200 to. Nothing anywhere reports it.
//
// api_tasks_note_test.go's TestSetTaskStepNoteWritesOnlyTheNoteColumn said in
// its own header that it does NOT cover this and that the protection was
// "STRUCTURAL, not tested". These two tests construct it instead of arguing it,
// and TestTaskStepNoteRaceGuardHasTeeth names the single line that decides it.

// TestStepNoteSurvivesAWholeRowStepWriterInterleavedExactly is the
// DETERMINISTIC construction. It does not hope to hit the window — it opens the
// window by hand and drives the note write through it.
//
// The interleave replays HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost's
// own sequence, statement for statement (dal.GetTaskStep → mutate
// WaitingReason/Status/StartedTS → dal.PutTaskStep), with the note write placed
// between the read and the write. That is faithful, not a caricature: the
// handler holds the snapshot from that GetTaskStep until its PutTaskStep, and
// the fields mutated here are exactly the ones it mutates on a pending →
// in_progress report.
//
// Both writes must stand. If the note reverts to "before", a handover the agent
// was told had landed (200 + the note echoed back in the receipt) has been
// silently destroyed by an unrelated status report — the worst shape of this
// bug, because the successor session reads the stale note and never learns the
// newer one existed.
func TestStepNoteSurvivesAWholeRowStepWriterInterleavedExactly(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
	})
	stepID := view.Steps[0].ID
	if rec := writeStepNote(t, api, task.ID, stepID, "m-exec", "before"); rec.Code != http.StatusOK {
		t.Fatalf("seed note: %d %s", rec.Code, rec.Body.String())
	}

	// ① the status handler's READ. Everything it holds from here is a snapshot.
	stale, err := api.dal.GetTaskStep(stepID)
	if err != nil || stale == nil {
		t.Fatalf("stale read: %v %v", stale, err)
	}
	if stale.Note != "before" {
		t.Fatalf("precondition: snapshot note = %q, want before", stale.Note)
	}

	// ② the handover note lands INSIDE the window, through the real endpoint.
	if rec := writeStepNote(t, api, task.ID, stepID, "m-exec",
		"跑完 conformance;下一步接前端 i18n"); rec.Code != http.StatusOK {
		t.Fatalf("interleaved note write: %d %s", rec.Code, rec.Body.String())
	}

	// ③ the status handler's WRITE, carrying the note it read in ①.
	stale.WaitingReason = ""
	stale.Status = StepStatusInProgress
	stale.StartedTS = nowSecs()
	if err := api.dal.PutTaskStep(*stale); err != nil {
		t.Fatalf("stale whole-row write: %v", err)
	}

	if got := readStepNote(t, api, task.ID, stepID); got != "跑完 conformance;下一步接前端 i18n" {
		t.Fatalf("LOST UPDATE: note = %q, want the interleaved handover note — a "+
			"whole-row step writer replayed the note it had read before the write", got)
	}
	// The other direction must also hold: the note write must not have eaten the
	// status change. A "fix" that merely swapped which writer loses would pass
	// the assertion above and fail this one.
	after, err := api.dal.GetTaskStep(stepID)
	if err != nil || after == nil {
		t.Fatalf("reload step: %v %v", after, err)
	}
	if after.Status != StepStatusInProgress {
		t.Fatalf("step status = %q, want in_progress — the note write must not "+
			"clobber a concurrent whole-row writer either", after.Status)
	}
}

// TestStepNoteSurvivesConcurrentStatusReports is the same hazard without a
// hand-placed window: two goroutines drive the two REAL endpoints against one
// step, repeatedly. It is the honest complement to the deterministic case —
// deterministic proof that the window can be exploited, plus evidence that the
// scheduler actually lands in it under ordinary contention.
//
// The status writer cycles in_progress ⇄ waiting_external, the only pair of
// agent-reportable step transitions that can be driven repeatedly
// (agentStepTransitions; done is terminal and would close the task).
//
// The assertion is the invariant, not a count: after every round the stored
// note must be the one the note endpoint actually wrote in that round.
// Reverting to an OLDER value is the lost update; there is no legal path to it.
func TestStepNoteSurvivesConcurrentStatusReports(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
	})
	stepID := view.Steps[0].ID
	if rec := reportStepStatus(t, api, task.ID, stepID, "m-exec",
		StepStatusInProgress, ""); rec.Code != http.StatusOK {
		t.Fatalf("start step: %d %s", rec.Code, rec.Body.String())
	}

	const rounds = 60
	for round := 0; round < rounds; round++ {
		want := fmt.Sprintf("handover-%d", round)
		// even rounds park the step, odd rounds resume it — both legal, both
		// whole-row writes through PutTaskStep.
		status, reason := StepStatusWaitingExternal, "waiting on the vendor"
		if round%2 == 1 {
			status, reason = StepStatusInProgress, ""
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if rec := writeStepNote(t, api, task.ID, stepID, "m-exec", want); rec.Code != http.StatusOK {
				t.Errorf("round %d note write: %d %s", round, rec.Code, rec.Body.String())
			}
		}()
		go func() {
			defer wg.Done()
			if rec := reportStepStatus(t, api, task.ID, stepID, "m-exec",
				status, reason); rec.Code != http.StatusOK {
				t.Errorf("round %d status report: %d %s", round, rec.Code, rec.Body.String())
			}
		}()
		wg.Wait()

		if got := readStepNote(t, api, task.ID, stepID); got != want {
			t.Fatalf("round %d LOST UPDATE: note = %q, want %q — the concurrent "+
				"status report replayed a stale note", round, got, want)
		}
	}
}

// TestTaskStepNoteRaceGuardHasTeeth names the guard so the next reader can find
// it, and states the counterfactual that was actually run rather than merely
// asserted.
//
// THE GUARD: dal_tasks.go PutTaskStep's ON CONFLICT DO UPDATE list does NOT
// contain `note = excluded.note`. The column is therefore written by exactly
// one statement that can ever target an EXISTING row — SetTaskStepNote, a
// single-column UPDATE — so no whole-row upsert can replay a stale copy of it.
// That is the whole protection: not a lock, not a retry, an ownership boundary.
//
// Do not read the surviving INSERT half of PutTaskStep as a second writer of an
// existing row: no production caller reaches it deliberately (all four load a
// row first), and submit_plan mints its rows through ReplaceTaskPlan's own bare
// INSERT — a different statement, with no conflict clause at all. See
// PutTaskStep's godoc for the full argument.
//
// COUNTERFACTUAL (run by hand, then re-measured by an independent reviewer):
// adding `note = excluded.note` back to that ON CONFLICT list turns all three
// tests red. ⚠️ They are NOT equally strong, and the difference matters to
// anyone diagnosing a future failure:
//
//   - the deterministic one is RELIABLE — 5 of 5 runs red, always on its first
//     assertion. It is what actually carries the discriminating power here.
//   - the concurrent one is PROBABILISTIC — 12 of 15 runs red (~80%), and the
//     round it lands on ranges from 0 to 54 out of 60. Three runs went the full
//     60 rounds without ever hitting the window. Do NOT expect it within the
//     first few rounds, and do NOT read a single green run of it as evidence
//     that the hazard is gone.
//
// Without this counterfactual the two tests above would be indistinguishable
// from tests that pass because nothing was ever concurrent.
//
// This test itself asserts the boundary structurally, so the guard cannot be
// removed quietly even by someone who never runs the counterfactual:
// PutTaskStep must not name the note column in its conflict clause.
func TestTaskStepNoteRaceGuardHasTeeth(t *testing.T) {
	raw, err := os.ReadFile("dal_tasks.go")
	if err != nil {
		t.Fatalf("read dal_tasks.go: %v", err)
	}
	// Anchored on the SYMBOL, never a line number: the enclosing text of
	// PutTaskStep, cut at the next top-level func.
	const anchor = "func (d *DAL) PutTaskStep(st TaskStep) error {"
	start := strings.Index(string(raw), anchor)
	if start < 0 {
		t.Fatal("PutTaskStep not found in dal_tasks.go — this guard is anchored " +
			"on the symbol, not a line number; re-point it if the function moved")
	}
	rest := string(raw)[start+len(anchor):]
	if end := strings.Index(rest, "\nfunc "); end >= 0 {
		rest = rest[:end]
	}
	body := rest
	if strings.Contains(body, "note = excluded.note") {
		t.Fatal("PutTaskStep's ON CONFLICT list writes the note column again. " +
			"That makes note a shared-write column, and every load-mutate-save " +
			"step writer will replay a stale copy of it over a concurrent " +
			"handover note (T-e271 node 6). All four of them do it: " +
			"update_step_status, armStepWithCard, the reply-card release path, " +
			"and the reassign step reset. The only statement that may write " +
			"note to an EXISTING row is SetTaskStepNote.")
	}
	// A live positive control on the reader itself: a column that IS in the
	// conflict list must be found. Without this, a broken anchor/slice would
	// make the assertion above vacuously green.
	if !strings.Contains(body, "status = excluded.status") {
		t.Fatal("source-reading control failed: PutTaskStep's conflict list " +
			"should still carry status — the assertion above cannot be trusted")
	}
}

// ── T-1667 no-op patch:被並發刪掉的 step 仍須 404 ────────────────────────────

// TestNoOpPatchStepNoteStillCatchesAConcurrentStepDeletion pins the other half
// of the no-op patch gate: a batch whose edits cancel out publishes nothing, but
// it must still not answer 200 with a note and a sha256 for a step that a
// concurrent submit_plan deleted while the request was in flight. That deletion
// is noticed in exactly one place — SetTaskStepNote affecting zero rows — so the
// gate has to hold back the ANNOUNCEMENT and not the write; a receipt describing
// the current contents of a row that no longer exists is a false statement, and
// the caller has no reason to look again.
//
// The interleave is exact, not hoped for. The write pool is one connection with
// _txlock=immediate: holding it with the deletion UNCOMMITTED means the read
// pool still serves the pre-delete snapshot (so the handler's guard chain
// resolves task and step whenever it happens to be scheduled — the 404 measured
// below cannot be the unknown-step guard's) while the handler's write cannot run
// until this transaction commits. That split needs the REAL two-pool wiring;
// NewDAL's single handle would block the reads too.
func TestNoOpPatchStepNoteStillCatchesAConcurrentStepDeletion(t *testing.T) {
	api := newAPIServer(newSplitPoolDAL(t), NewHub(), []byte("tasks-test-secret"),
		3600, assetRoot(t.TempDir()))
	const seeded = "做到哪：改到一半\n下一步：待補"
	taskID, stepID := seedStepWithNote(t, api, seeded)

	tx, err := api.dal.wdb.Begin()
	if err != nil {
		t.Fatalf("hold the write connection: %v", err)
	}
	// The statement submit_plan's ReplaceTaskPlan issues for a non-terminal step
	// the fresh plan did not re-list.
	if _, err := tx.Exec(`DELETE FROM task_step WHERE id = ?`, stepID); err != nil {
		t.Fatalf("delete the step: %v", err)
	}

	type answer struct {
		status int
		data   map[string]any
	}
	waits := api.dal.wdb.Stats().WaitCount
	done := make(chan answer, 1)
	go func() {
		status, data := patchStepNote(t, api, taskID, stepID, "m-exec",
			cancellingBatch("下一步：待補", "下一步：接 auth matrix"))
		done <- answer{status, data}
	}()

	// The handler is past its guard chain and inside the window exactly when it is
	// queued for the write connection this test is holding.
	blocked := false
	for i := 0; i < 400 && !blocked; i++ {
		if blocked = api.dal.wdb.Stats().WaitCount > waits; !blocked {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit the deletion: %v", err)
	}
	got := <-done

	if !blocked {
		t.Errorf("the no-op patch never asked for the write connection, so the " +
			"deletion was noticed by NOTHING on this path")
	}
	if got.status != http.StatusNotFound {
		t.Fatalf("a step deleted mid-request must be 404, got %d: %v", got.status, got.data)
	}
	// Same 404 as the unknown-step guard and the wholesale face, not a second
	// shape for the caller to learn.
	body, _ := got.data["error"].(map[string]any)
	if msg, _ := body["message"].(string); !strings.Contains(msg, stepID) {
		t.Errorf("the 404 must name the step, got %v", got.data)
	}
}
