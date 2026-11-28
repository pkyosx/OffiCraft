// TaskCard — the duplicated terminal status + 原票指向 (T-02c9). Locked:
//   1. a duplicated task is terminal and renders the 重複
//      status badge + a 重複於 <task id> link that jumps to the original;
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
    // A duplicated task is terminal → jump straight to it so it is listed.
    window.location.hash = `#tasks/${dup.id}`;
    const { findByTestId } = renderPage();

    const list = await findByTestId("task-list");
    const card = within(list).getByTestId("task-card");
    // the 重複 status badge …
    expect(within(card).getByTestId("task-status").getAttribute("aria-label")).toContain("重複");
    // … and a link naming the original by its id, which is its number.
    const link = within(card).getByTestId("task-duplicate-link");
    expect(link.textContent).toBe(`重複於 #${original.id}`);

    fireEvent.click(link);
    expect(window.location.hash).toBe(`#tasks/${original.id}`);
  });

  it("marks a task duplicated by searching every task for the original; it leaves the live list", async () => {
    const original = mkTask({
      title: "原始工單",
      status: "done",
      closedTs: Date.now() / 1000 - 600,
    });
    const other = mkTask({ title: "另一張工單" });
    const shell = mkTask({ title: "空殼重複" });
    const gone = mkTask({
      title: "已重複的工單",
      status: "duplicated",
      closedTs: Date.now() / 1000 - 60,
    });
    __injectMockTask(original);
    __injectMockTask(other);
    __injectMockTask(shell);
    __injectMockTask(gone);
    const { findByTestId } = renderPage();

    const list = await findByTestId("task-list");
    const shellCard = within(list)
      .getAllByTestId("task-card")
      .find((c) =>
        within(c).getByTestId("task-no").textContent?.includes(shell.taskNo)
      )!;
    fireEvent.click(within(shellCard).getByTestId("task-status"));
    fireEvent.click(within(shellCard).getByTestId("task-mark-duplicate"));

    const select = (await findByTestId(
      "mark-duplicate-select"
    )) as HTMLSelectElement;
    const offered = () => [...select.options].slice(1).map((o) => o.textContent);
    // Empty box: the first page of every task but itself and the duplicates —
    // the closed original included, though the page never loaded it.
    await waitFor(() =>
      expect(offered()).toEqual([`${other.taskNo} · 另一張工單`, `${original.taskNo} · 原始工單`])
    );

    fireEvent.change(await findByTestId("mark-duplicate-search"), {
      target: { value: "原始" },
    });
    await waitFor(() => expect(offered()).toEqual([`${original.taskNo} · 原始工單`]));

    fireEvent.change(await findByTestId("mark-duplicate-search"), {
      target: { value: "沒有這張" },
    });
    await findByTestId("mark-duplicate-none");
    expect(offered()).toEqual([]);

    fireEvent.change(await findByTestId("mark-duplicate-search"), {
      target: { value: original.id },
    });
    await waitFor(() => expect(offered()).toEqual([`${original.taskNo} · 原始工單`]));
    fireEvent.change(select, { target: { value: original.id } });
    fireEvent.click(await findByTestId("mark-duplicate-btn"));

    await waitFor(() => {
      const live = within(findByTestIdSync("task-list")).queryByText(shell.title);
      expect(live).toBeNull();
    });
    expect(
      within(findByTestIdSync("task-list")).getByText(other.title)
    ).toBeTruthy();
  });
});

/** getByTestId against the live document (post-refetch re-render). */
function findByTestIdSync(id: string): HTMLElement {
  const el = document.querySelector(`[data-testid="${id}"]`);
  if (!el) throw new Error(`no element ${id}`);
  return el as HTMLElement;
}
