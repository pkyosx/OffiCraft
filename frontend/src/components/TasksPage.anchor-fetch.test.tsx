// TasksPage — 連結直跳單張任務 must not download the whole history
// (owner 2026-08-01).
//
// TasksPage.jump.test.tsx already pins what the owner SEES when arriving at
// #tasks/<id>. This file pins what goes ON THE WIRE to make that happen, because
// the two came apart: the page satisfied "the closed target is visible" by
// dropping its `?statuses=` constraint and pulling every task (432 KB / 706 rows
// measured on the live workshop) so the one it wanted was somewhere inside.
//
// So the assertions here spy on the api seam and read the CALLS. A test that
// only counted requests would be green on the old code (which issued exactly one
// list fetch — the enormous one); it is the ARGUMENTS that tell the two apart.
//
// 🔴 Out of scope on purpose: 清除篩選 (an empty status set) still asks for
// everything. The owner asked for the whole population there, so the whole
// population is the correct answer — TasksPage.jump.test.tsx's 清除篩選 case and
// useTasks.statuses.test.ts both keep that alive.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { api } from "../api";
import { __resetMock, __injectMockTask } from "../api/mock";
import type { TaskView } from "../api/adapter";

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${2000 + seq}`,
    title: `任務 ${seq}`,
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

function renderTasks() {
  return render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("#tasks/<id> 直跳:只補抓那一張", () => {
  it("a CLOSED anchor renders, and NO list fetch ever drops the status constraint", async () => {
    __injectMockTask(mkTask({ id: "t-open" }));
    __injectMockTask(
      mkTask({
        id: "t-closed",
        status: "done",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const listTasks = vi.spyOn(api, "listTasks");
    const getTask = vi.spyOn(api, "getTask");
    window.location.hash = "#tasks/t-closed";

    const { findByTestId } = renderTasks();

    // 驗收條件: the 已結案 target the owner followed the link to is on screen.
    const closedList = await findByTestId("closed-list");
    expect(
      closedList.querySelector('[data-task-id="t-closed"]')
    ).not.toBeNull();

    // 🔴 …and it got there WITHOUT the archive. Every list request carried a
    // status set; the old code sent one with none, which is the 432 KB request.
    // MUTANT (proven): restore `taskIdFilter !== undefined ? undefined : …` in
    // TasksPage's setStatuses effect → this fails on the very first call.
    expect(listTasks).toHaveBeenCalled();
    for (const [opts] of listTasks.mock.calls) {
      expect(opts?.statuses).toBeInstanceOf(Array);
      expect(opts?.statuses?.length).toBeGreaterThan(0);
      // The default 狀態 filter, unchanged by the jump: terminals excluded.
      expect(opts?.statuses).not.toContain("done");
    }
    // The one row it DID pay for: a single-task read of exactly the anchor.
    expect(getTask.mock.calls.map((c) => c[0])).toContain("t-closed");
  });

  it("the anchored task is fetched by ID even though the default filter excludes its status", async () => {
    // Non-vacuity control for the test above: the row genuinely is NOT in the
    // list response, so its presence on screen can only come from the by-id
    // read. Asserting on the mock adapter's own answer rather than trusting it.
    __injectMockTask(
      mkTask({ id: "t-done", status: "done", closedTs: Date.now() / 1000 - 60 })
    );
    const listTasks = vi.spyOn(api, "listTasks");
    window.location.hash = "#tasks/t-done";

    const { findByTestId } = renderTasks();
    await findByTestId("closed-list");

    const firstAsk = listTasks.mock.calls[0][0];
    const rows = await api.listTasks(firstAsk);
    expect(rows.map((x) => x.id)).not.toContain("t-done");
  });

  it("keeps the anchor while the by-id read is still in flight", async () => {
    // 🔴 The frames the old code never had. When the anchored row arrived inside
    // a widened LIST fetch, `loading` covered the whole wait. Now it arrives on
    // its own request, so between mount and that response the page holds an id
    // it cannot find in `tasks` — and the empty state reads exactly that shape
    // as 「不存在」. Without the anchorPending guard the page prints
    // 沒有符合篩選條件的任務 over a task that is simply still on its way.
    // A DEFERRED promise is what makes this visible: every other test in this
    // file resolves within a microtask, so the missing-guard version wins the
    // race and stays green. MUTANT: drop `!anchorPending` from `nothingMatches`
    // → the page answers 「不存在」 for a task still in flight.
    __injectMockTask(mkTask({ id: "t-live" }));
    __injectMockTask(
      mkTask({ id: "t-slow", status: "done", closedTs: Date.now() / 1000 - 60 })
    );
    let release!: (v: TaskView) => void;
    const real = api.getTask.bind(api);
    vi.spyOn(api, "getTask").mockImplementation(
      () => new Promise<TaskView>((r) => (release = r))
    );
    window.location.hash = "#tasks/t-slow";

    const { findByTestId } = renderTasks();
    // The list has landed (the live task would be rendered) but the anchor has
    // not — and the anchor must survive that window.
    await waitFor(() => expect(api.listTasks).toBeTruthy());
    await new Promise((r) => setTimeout(r, 20));
    expect(window.location.hash).toBe("#tasks/t-slow");

    await act(async () => {
      release(await real("t-slow"));
    });
    const closedList = await findByTestId("closed-list");
    expect(closedList.querySelector('[data-task-id="t-slow"]')).not.toBeNull();
  });

  it("a hydrate that failed for a NON-404 reason shows the load error, not an empty state", async () => {
    // 補抓失敗 that is NOT 「這張不存在」 (500 / offline; the 404 case is pinned
    // in TasksPage.jump.test.tsx and says something DIFFERENT on purpose).
    // The page could not ASK, so it must not answer: 沒有符合篩選條件的任務
    // would be a claim about a question nobody got to put, and the owner would
    // read a broken server as a deleted task. It shows its load error instead —
    // never blank, never a stuck spinner, never a lie.
    __injectMockTask(mkTask({ id: "t-live" }));
    __injectMockTask(
      mkTask({ id: "t-far", status: "done", closedTs: Date.now() / 1000 - 60 })
    );
    vi.spyOn(api, "getTask").mockRejectedValue(new Error("boom"));
    window.location.hash = "#tasks/t-far";

    const { findByTestId, queryByTestId } = renderTasks();

    await findByTestId("tasks-error");
    // Neither empty state may appear: both would be answers.
    expect(queryByTestId("tasks-empty")).toBeNull();
    expect(queryByTestId("tasks-empty-filtered")).toBeNull();
    // MUTANT: leave anchorPending true on the failure path → nothing settles and
    // the findByTestId above times out.
  });
});
