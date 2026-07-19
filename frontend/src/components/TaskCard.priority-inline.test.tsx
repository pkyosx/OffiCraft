// TaskCard — in-place priority editing (v2/v3, owner 2026-07-17). Locked here:
//   1. The 優先權 chip on the badge row is a button; clicking it drops a
//      select-style vertical menu with 高/中/低/凍結 right under the chip
//      (the ⋮ menu popover's visual vocabulary — no ⋮ dive, no card expand).
//   2. Picking an option saves through setTaskPriority and closes the menu;
//      the current priority is marked active.
//   3. A click outside the picker closes it without saving.
//   4. A closed (terminal) task's priority is frozen history — plain span,
//      no picker.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
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

describe("TaskCard in-place priority editing (v2)", () => {
  it("clicking the priority chip drops a select-style menu with the current option active", async () => {
    __injectMockTask(mkTask({ title: "就地編輯", priority: "mid" }));
    const { findByTestId, queryByTestId } = renderPage();
    const chip = await findByTestId("task-priority");
    expect(chip.tagName).toBe("BUTTON");
    expect(chip.getAttribute("aria-expanded")).toBe("false");
    expect(queryByTestId("task-priority-options")).toBeNull();

    fireEvent.click(chip);
    expect(chip.getAttribute("aria-expanded")).toBe("true");
    const options = await findByTestId("task-priority-options");
    // The dropdown reuses the ⋮ popover's visual vocabulary (v3) and lists
    // the four options vertically as menu items.
    expect(options.classList.contains("task-card__menu-pop")).toBe(true);
    expect(options.getAttribute("role")).toBe("menu");
    const labels = Array.from(options.querySelectorAll("button")).map(
      (b) => b.textContent
    );
    expect(labels).toEqual(["高", "中", "低", "凍結"]);
    expect(
      options
        .querySelector('[data-testid="priority-mid"]')!
        .classList.contains("task-card__menu-item--active")
    ).toBe(true);
  });

  it("picking an option saves the new priority and closes the row", async () => {
    __injectMockTask(mkTask({ title: "選高", priority: "mid" }));
    const { findByTestId, queryByTestId } = renderPage();
    const chip = await findByTestId("task-priority");
    fireEvent.click(chip);
    fireEvent.click(await findByTestId("priority-high"));

    expect(queryByTestId("task-priority-options")).toBeNull();
    await waitFor(() =>
      expect(
        document.querySelector('[data-testid="task-priority"]')?.textContent
      ).toContain("高")
    );
  });

  it("re-picking the current priority just closes the row", async () => {
    __injectMockTask(mkTask({ title: "原地選", priority: "low" }));
    const { findByTestId, queryByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-priority"));
    fireEvent.click(await findByTestId("priority-low"));
    expect(queryByTestId("task-priority-options")).toBeNull();
    expect(
      (await findByTestId("task-priority")).textContent
    ).toContain("低");
  });

  it("a click outside the picker closes it without saving", async () => {
    __injectMockTask(mkTask({ title: "點外面", priority: "mid" }));
    const { findByTestId, queryByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-priority"));
    expect(await findByTestId("task-priority-options")).toBeTruthy();

    fireEvent.mouseDown(document.body);
    await waitFor(() =>
      expect(queryByTestId("task-priority-options")).toBeNull()
    );
    expect((await findByTestId("task-priority")).textContent).toContain("中");
  });

  it("a closed task's priority is a plain badge — no picker", async () => {
    __injectMockTask(
      mkTask({
        title: "已終止",
        status: "terminated",
        closedTs: Date.now() / 1000 - 10,
      })
    );
    const { findAllByTestId, findByTestId } = renderPage();
    // Terminal tasks hide behind the 狀態 filter → tick 終止 to surface the
    // (collapsed) 已結束 section, then open it.
    const trigger = await findByTestId("filter-status");
    fireEvent.click(trigger);
    fireEvent.click(
      document.querySelector(
        '[data-testid="filter-status-opt-terminated"] input'
      )!
    );
    fireEvent.click(await findByTestId("closed-toggle"));

    const chips = await findAllByTestId("task-priority");
    expect(chips[0].tagName).toBe("SPAN");
    fireEvent.click(chips[0]);
    expect(
      document.querySelector('[data-testid="task-priority-options"]')
    ).toBeNull();
  });
});
