// TasksPage — 連結直跳單張任務 must not download the whole history
// (owner 2026-08-01). TasksPage.jump.test.tsx pins what the owner SEES at
// #tasks/<id>; this file pins what goes ON THE WIRE: the jump asks the list
// for exactly that id (`?ids=`), never a list without a constraint.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act } from "@testing-library/react";
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
    executorKind: "staff",
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

describe("#tasks/<id> 直跳:只問那一張", () => {
  it("a CLOSED anchor renders, and every list ask names only that id", async () => {
    __injectMockTask(mkTask({ id: "t-open" }));
    __injectMockTask(
      mkTask({
        id: "t-closed",
        status: "done",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const listTasks = vi.spyOn(api, "listTasks");
    window.location.hash = "#tasks/t-closed";

    const { findByTestId } = renderTasks();

    const list = await findByTestId("task-list");
    expect(list.querySelector('[data-task-id="t-closed"]')).not.toBeNull();
    expect(list.querySelectorAll('[data-testid="task-card"]')).toHaveLength(1);
    expect(listTasks.mock.calls.map(([opts]) => opts)).toEqual([
      {
        statuses: [],
        executors: [],
        types: [],
        due: [],
        ids: ["t-closed"],
        groupBy: "none",
        limit: 50,
        offset: 0,
      },
    ]);
  });

  it("keeps the anchor and says nothing while the by-id read is still in flight", async () => {
    __injectMockTask(
      mkTask({ id: "t-slow", status: "done", closedTs: Date.now() / 1000 - 60 })
    );
    const real = api.listTasks.bind(api);
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    vi.spyOn(api, "listTasks").mockImplementation(async (opts) => {
      await gate;
      return real(opts);
    });
    window.location.hash = "#tasks/t-slow";

    const { findByTestId, queryByTestId } = renderTasks();
    await new Promise((r) => setTimeout(r, 20));
    expect(window.location.hash).toBe("#tasks/t-slow");
    expect(queryByTestId("tasks-empty")).toBeNull();
    expect(queryByTestId("tasks-empty-filtered")).toBeNull();

    await act(async () => release());
    const list = await findByTestId("task-list");
    expect(list.querySelector('[data-task-id="t-slow"]')).not.toBeNull();
  });

  it("a read that failed shows the error naming the id, not an empty state", async () => {
    __injectMockTask(
      mkTask({ id: "t-far", status: "done", closedTs: Date.now() / 1000 - 60 })
    );
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(api, "listTasks").mockRejectedValue(new Error("boom"));
    window.location.hash = "#tasks/t-far";

    const { findByTestId, queryByTestId } = renderTasks();

    const err = await findByTestId("tasks-error");
    expect(err.textContent).toContain("t-far");
    expect(queryByTestId("tasks-empty")).toBeNull();
    expect(queryByTestId("tasks-empty-filtered")).toBeNull();
  });
});
