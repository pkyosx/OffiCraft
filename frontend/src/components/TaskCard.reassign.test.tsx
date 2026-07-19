// TaskCard — 轉派 (T-160e). Locked here:
//   1. the 負責人 row carries a 轉派 icon button (owner 2026-07-18: NOT the
//      deleted ⋮ menu, NOT the 狀態 dropdown), and it opens the
//      TaskReassignDialog;
//   2. the dialog is a member/外包 segmented pair — the member face lists the
//      roster's assistants, the 外包 face shows the SAME 模型/投入程度/機器
//      knobs the task type's 負責成員 carries (ModelEffortEditor's vocabulary);
//   3. confirming a member target re-points the executor and lands the task in
//      轉派中 (the FE never flips it back — that is the new executor's report);
//   4. confirming an 外包 target mints a worker and the card shows its codename;
//   5. the CURRENT executor is never offered (the server 409s a no-op);
//   6. 轉派中 is an orthogonal LOCK (T-9ca5), not a status: the task keeps its
//      derived status (in_progress), stays in 未結束, and renders the lock badge.

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
    // Outsource-executed by default so Mira (the mock roster's only assistant)
    // is a legal reassign target — a task she already owns could not be
    // reassigned to her.
    executorKind: "outsource",
    executorId: "",
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

/** Click the 負責人 row's 轉派 icon and return the dialog. */
async function openDialog(findByTestId: (id: string) => Promise<HTMLElement>) {
  fireEvent.click(await findByTestId("task-reassign"));
  return findByTestId("reassign");
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  seq = 0;
});

describe("TaskCard 轉派 entry + dialog", () => {
  it("opens the reassign dialog from the 負責人 row icon with both target faces", async () => {
    __injectMockTask(mkTask({ title: "要轉派的任務" }));
    const { findByTestId } = renderPage();

    // The 轉派 icon sits next to 負責人 and is present without any menu (owner
    // 2026-07-18): a plain icon button, labelled 轉派, one click opens the
    // dialog.
    const item = await findByTestId("task-reassign");
    expect(item.tagName).toBe("BUTTON");
    expect(item.getAttribute("aria-label")).toContain("轉派");

    fireEvent.click(item);
    const dialog = await findByTestId("reassign");
    expect(dialog.getAttribute("role")).toBe("dialog");
    // 成員 face is the default; the 外包 knobs are not mounted yet.
    expect(within(dialog).getByTestId("reassign-member-mira")).toBeTruthy();
    expect(within(dialog).queryByTestId("reassign-model")).toBeNull();

    // Switching to 外包 swaps in the model / effort / machine knobs — the same
    // vocabulary ModelEffortEditor publishes. No 自動分配 row: a reassign must
    // name a machine (owner 2026-07-19), so the machine section mounts without it.
    fireEvent.click(within(dialog).getByTestId("reassign-kind-outsource"));
    expect(within(dialog).getByTestId("reassign-model-opus")).toBeTruthy();
    expect(within(dialog).getByTestId("reassign-effort-high")).toBeTruthy();
    expect(within(dialog).getByText("機器")).toBeTruthy();
    expect(within(dialog).queryByTestId("reassign-machine-auto")).toBeNull();
    // The member list is gone with its face.
    expect(within(dialog).queryByTestId("reassign-member-mira")).toBeNull();
  });

  it("puts the 轉派 icon in the 負責人 cell — never the 狀態 dropdown (owner 2026-07-18)", async () => {
    __injectMockTask(mkTask({ title: "入口鎖" }));
    const { findByTestId } = renderPage();

    // The icon is a sibling of the 負責人 chip inside the assignee cell — the
    // owner's ruling that 轉派 lives beside WHO owns the task.
    const reassign = await findByTestId("task-reassign");
    const cell = reassign.closest(".task-card__assignee-cell");
    expect(cell).not.toBeNull();
    expect(cell!.querySelector('[data-testid="task-executor"]')).not.toBeNull();

    // And it is NOT smuggled back into the 狀態 badge dropdown: opening that
    // menu shows only 標記重複 / 終止, never 轉派.
    fireEvent.click(await findByTestId("task-status"));
    const menu = await findByTestId("task-status-options");
    expect(within(menu).queryByTestId("task-reassign")).toBeNull();
    expect(within(menu).getByTestId("task-mark-duplicate")).toBeTruthy();
  });

  it("hides the 轉派 icon on a terminal task (the server 409s a reassign)", async () => {
    __injectMockTask(
      mkTask({
        title: "已終止",
        status: "terminated",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const { findByTestId } = renderPage();
    // Terminals are filtered out of the default view — reveal via the status
    // filter + the closed toggle (same path as the status-menu suite).
    const trigger = document.querySelector('[data-testid="filter-status"]')!;
    if (trigger.getAttribute("aria-expanded") !== "true") fireEvent.click(trigger);
    fireEvent.click(
      document.querySelector('[data-testid="filter-status-opt-terminated"] input')!
    );
    fireEvent.click(await findByTestId("closed-toggle"));

    const card = await findByTestId("task-card");
    // The card renders (its status badge is there) but the 轉派 icon is gone.
    expect(within(card).getByTestId("task-status")).toBeTruthy();
    expect(within(card).queryByTestId("task-reassign")).toBeNull();
  });

  it("never offers the task's CURRENT executor as a target", async () => {
    __injectMockTask(
      mkTask({ title: "Mira 的任務", executorKind: "member", executorId: "mira" })
    );
    const { findByTestId, queryByTestId } = renderPage();
    const dialog = await openDialog(findByTestId);

    // Mira is the executor → not a candidate; she is the only assistant on the
    // mock roster, so the picker is honestly empty rather than a 409 trap.
    expect(queryByTestId("reassign-member-mira")).toBeNull();
    expect(within(dialog).getByText("沒有可轉派的成員")).toBeTruthy();
  });
});

describe("TaskCard 轉派 commit", () => {
  it("reassigns to a member: the task re-points and enters 轉派中", async () => {
    const task = mkTask({ title: "轉給 Mira" });
    __injectMockTask(task);
    const { findByTestId } = renderPage();
    const dialog = await openDialog(findByTestId);

    fireEvent.click(within(dialog).getByTestId("reassign-member-mira"));
    fireEvent.change(within(dialog).getByTestId("reassign-note"), {
      target: { value: "交接備註" },
    });
    fireEvent.click(within(dialog).getByTestId("reassign-confirm"));

    // The dialog closes on success and the card reconciles by refetch.
    await waitFor(() => {
      expect(document.querySelector('[data-testid="reassign"]')).toBeNull();
    });
    const card = await findByTestId("task-card");
    // T-9ca5: 轉派中 is the orthogonal LOCK overlay badge now, beside the honest
    // derived status (in_progress), not the status badge itself.
    expect(within(card).getByTestId("task-lock").textContent).toContain(
      "轉派中"
    );
    expect(within(card).getByTestId("task-status").textContent).toContain(
      "進行中"
    );
    // 轉派中 is NOT terminal — the card stays in the live list.
    expect(within(await findByTestId("open-list")).getByText("轉給 Mira")).toBeTruthy();
    // The new executor is on the card.
    expect(card.textContent).toContain("Mira");
  });

  it("reassigns to 外包: a fresh worker is minted and shown on the card", async () => {
    __injectMockTask(mkTask({ title: "轉外包" }));
    const { findByTestId } = renderPage();
    const dialog = await openDialog(findByTestId);

    fireEvent.click(within(dialog).getByTestId("reassign-kind-outsource"));
    fireEvent.click(within(dialog).getByTestId("reassign-model-sonnet"));
    fireEvent.click(within(dialog).getByTestId("reassign-effort-low"));
    // A reassign must name a machine now (no 自動分配 default) — pick the first
    // machine row the registry offers so commit() isn't blocked by the guard.
    const machineRow = await waitFor(() => {
      const row = dialog.querySelector<HTMLElement>(
        '[data-testid^="reassign-machine-"]'
      );
      if (!row) throw new Error("no machine row yet");
      return row;
    });
    fireEvent.click(machineRow);
    fireEvent.click(within(dialog).getByTestId("reassign-confirm"));

    await waitFor(() => {
      expect(document.querySelector('[data-testid="reassign"]')).toBeNull();
    });
    const card = await findByTestId("task-card");
    // T-9ca5: the 轉派中 LOCK overlay badge rides beside the derived status.
    expect(within(card).getByTestId("task-lock").textContent).toContain(
      "轉派中"
    );
    // 外包 代號 · 模型 · 投入度 — the minted worker, resolved through the live
    // roster (the S family starts at S-1).
    await waitFor(() => {
      expect(
        document.querySelector('[data-testid="task-card"]')?.textContent
      ).toContain("外包 S-1 · sonnet · 低投入");
    });
  });

  it("keeps the dialog open and surfaces the failure when the server refuses", async () => {
    // A frozen task is a 400 (unfreeze first) — the honest inline failure path.
    __injectMockTask(mkTask({ title: "凍結中", priority: "frozen" }));
    const { findByTestId } = renderPage();
    const dialog = await openDialog(findByTestId);

    fireEvent.click(within(dialog).getByTestId("reassign-member-mira"));
    fireEvent.click(within(dialog).getByTestId("reassign-confirm"));

    expect(await findByTestId("reassign")).toBeTruthy();
    await waitFor(() => {
      expect(
        document.querySelector('[data-testid="reassign"]')?.textContent
      ).toContain("轉派失敗");
    });
  });
});
