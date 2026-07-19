// TaskCard — the duplicated terminal status + 原票指向 (T-02c9). Locked:
//   1. a duplicated task is terminal (sits in 已結束) and renders the 重複
//      status badge + a 重複於 T-xxxx link that jumps to the original;
//   2. the ⋮ menu's 標記重複 opens a picker; confirming a chosen original marks
//      the task duplicated through the seam — it then leaves the live list
//      (whoever spots the duplicate closes it, no owner terminate needed).

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { __resetMock, __injectMockTask } from "../api/mock";
import type { TaskView } from "../api/adapter";

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${1000 + seq}`,
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

function renderPage() {
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

describe("TaskCard duplicated 終態 + 原票指向 (T-02c9)", () => {
  it("renders the 重複 badge and a 重複於 link that jumps to the original", async () => {
    const original = mkTask({ title: "原票" });
    const dup = mkTask({
      title: "重複票",
      status: "duplicated",
      duplicateOf: original.id,
      closedTs: Date.now() / 1000,
    });
    __injectMockTask(original);
    __injectMockTask(dup);
    // A duplicated task is terminal → jump straight to it so 已結束 auto-expands.
    window.location.hash = `#tasks/${dup.id}`;
    const { findByTestId } = renderPage();

    const closedList = await findByTestId("closed-list");
    const card = within(closedList).getByTestId("task-card");
    // the 重複 status badge …
    expect(within(card).getByTestId("task-status").textContent).toContain("重複");
    // … and a link naming the original's task_no.
    const link = within(card).getByTestId("task-duplicate-link");
    expect(link.textContent).toContain(original.taskNo);

    fireEvent.click(link);
    expect(window.location.hash).toBe(`#tasks/${original.id}`);
  });

  it("marks a task duplicated through the ⋮ menu picker; it leaves the live list", async () => {
    const original = mkTask({ title: "原始工單" });
    const shell = mkTask({ title: "空殼重複" });
    __injectMockTask(original);
    __injectMockTask(shell);
    const { findByTestId } = renderPage();

    const openList = await findByTestId("open-list");
    // the shell's live card carries 標記重複 on its 狀態 badge dropdown (v5 —
    // the action moved off the ⋮ menu).
    const shellCard = within(openList)
      .getAllByTestId("task-card")
      .find((c) =>
        within(c).getByTestId("task-no").textContent?.includes(shell.taskNo)
      )!;
    fireEvent.click(within(shellCard).getByTestId("task-status"));
    fireEvent.click(within(shellCard).getByTestId("task-mark-duplicate"));

    // pick the original in the modal, then confirm.
    const select = (await findByTestId(
      "mark-duplicate-select"
    )) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: original.id } });
    fireEvent.click(await findByTestId("mark-duplicate-btn"));

    // the shell is now terminal (duplicated) and drops out of the live list.
    await waitFor(() => {
      const live = within(findByTestIdSync("open-list")).queryByText(shell.title);
      expect(live).toBeNull();
    });
    // the original stays live.
    expect(
      within(findByTestIdSync("open-list")).getByText(original.title)
    ).toBeTruthy();
  });
});

/** getByTestId against the live document (post-refetch re-render). */
function findByTestIdSync(id: string): HTMLElement {
  const el = document.querySelector(`[data-testid="${id}"]`);
  if (!el) throw new Error(`no element ${id}`);
  return el as HTMLElement;
}
