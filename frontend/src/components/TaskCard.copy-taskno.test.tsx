// TaskCard — 任務編號 chip 點擊複製 (owner 2026-07-19 圈截圖). Locked:
//   1. Clicking the #T-xxxx id chip writes the DISPLAY task number (task.taskNo,
//      never the internal id) to the clipboard.
//   2. On a successful write the chip flashes a transient 「已複製」feedback.
//   3. The chip is a keyboard-operable button with a copy aria-label.
//   4. Clicking the chip must NOT toggle the card open (the copy is its own job).

import { describe, it, expect, beforeEach, vi } from "vitest";
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

describe("TaskCard 任務編號 chip 點擊複製 (owner 2026-07-19)", () => {
  it("copies the display task number to the clipboard and flashes 已複製", async () => {
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });

    // internal id (task-N) deliberately differs from the display no — the copy
    // must carry the DISPLAY number, not the id.
    const task = mkTask({ title: "複製編號", taskNo: "T-0648" });
    __injectMockTask(task);

    const { findByTestId } = renderPage();
    const chip = await findByTestId("task-no");
    fireEvent.click(chip);

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("T-0648"));
    expect(writeText).not.toHaveBeenCalledWith(task.id);

    // transient success feedback appears after the write resolves.
    expect(await findByTestId("task-no-copied")).toBeTruthy();
  });

  it("exposes the chip as a button with a copy aria-label", async () => {
    __injectMockTask(mkTask({ title: "無障礙", taskNo: "T-0648" }));
    const { findByTestId } = renderPage();
    const chip = await findByTestId("task-no");
    expect(chip.tagName).toBe("BUTTON");
    expect(chip.getAttribute("aria-label")).toBe("複製任務編號 T-0648");
  });

  it("clicking the chip copies without toggling the card open", async () => {
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    __injectMockTask(mkTask({ title: "不展開", taskNo: "T-0648" }));

    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    expect(card.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(await findByTestId("task-no"));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("T-0648"));
    // the copy click never flipped the whole-card toggle.
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });
});
