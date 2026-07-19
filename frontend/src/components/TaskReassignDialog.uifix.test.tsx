// TaskReassignDialog / TaskCard — owner UI-review fixes (T-160e, uifix pass).
// Locks the DOM-level wiring behind the two owner tweaks:
//
//   1. 轉派中 is now an ORTHOGONAL LOCK overlay badge (T-9ca5), not a status:
//      it rides ALONGSIDE the honest derived status badge and keys off
//      task.lock === "reassigning". The COOL hue VALUE (#38c6d9) lives in
//      tasks.css and cannot be read back through jsdom (no stylesheet layout),
//      so this file only locks the class HOOK the rule targets
//      (task-badge--lock-reassigning) + the task-lock testid. The hue/overlay
//      layout is verified by the CT visual guard (lock-badge-*).
//   2. 模型 picker lays its 4 chips out as a fixed 2x2 (grid2 modifier) while
//      投入程度 / 轉給-轉外包 keep the flex row, AND a selected 模型 chip carries
//      the SAME active class as a selected 投入程度 chip. The 2x2 GEOMETRY is a
//      CSS grid (untestable in jsdom); what is locked here is the modifier
//      class that switches it on and the shared active-class consistency.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, within, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { TaskReassignDialog } from "./TaskReassignDialog";
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

async function openOutsourceFace(
  findByTestId: (id: string) => Promise<HTMLElement>
) {
  // 轉派 opens from the 負責人 row's icon button (owner 2026-07-18), not a menu.
  fireEvent.click(await findByTestId("task-reassign"));
  const dialog = await findByTestId("reassign");
  fireEvent.click(within(dialog).getByTestId("reassign-kind-outsource"));
  return dialog;
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  seq = 0;
});

describe("轉派中 lock badge — orthogonal overlay class hook", () => {
  it("renders the reassigning lock badge beside the honest derived status", async () => {
    __injectMockTask(
      mkTask({ lock: "reassigning", status: "in_progress", priority: "high" })
    );
    const { findByTestId } = renderPage();

    // The LOCK overlay — its own badge, keyed off task.lock, cool-hue class.
    const lock = await findByTestId("task-lock");
    expect(lock.textContent).toContain("轉派中");
    expect(lock.classList.contains("task-badge--lock-reassigning")).toBe(true);

    // The status badge stays the derived status (in_progress) — reassigning is
    // NOT a status any more, so it never borrows a status colour class.
    const status = await findByTestId("task-status");
    expect(status.textContent).toContain("進行中");
    expect(status.classList.contains("task-badge--lock-reassigning")).toBe(
      false
    );
  });

  it("shows no lock badge on a task with no reassigning lock", async () => {
    __injectMockTask(mkTask({ status: "in_progress" }));
    const { findByTestId, queryByTestId } = renderPage();

    await findByTestId("task-status");
    expect(queryByTestId("task-lock")).toBeNull();
  });
});

describe("模型 picker — 2x2 grid modifier + selection parity with 投入程度", () => {
  it("puts the grid2 modifier on the 模型 group only, not 投入程度", async () => {
    __injectMockTask(mkTask({}));
    const { findByTestId } = renderPage();
    const dialog = await openOutsourceFace(findByTestId);

    // Each Segmented group is the parent .task-reassign__seg of its chips.
    const modelGroup = within(dialog)
      .getByTestId("reassign-model-fable")
      .closest(".task-reassign__seg");
    const effortGroup = within(dialog)
      .getByTestId("reassign-effort-medium")
      .closest(".task-reassign__seg");

    expect(modelGroup).not.toBeNull();
    expect(effortGroup).not.toBeNull();
    // 模型: 4 chips → 2x2 grid, so haiku never wraps alone (owner review).
    expect(
      modelGroup!.classList.contains("task-reassign__seg--grid2")
    ).toBe(true);
    // 投入程度: 3 chips stay a flex row — must NOT inherit the grid.
    expect(
      effortGroup!.classList.contains("task-reassign__seg--grid2")
    ).toBe(false);
  });

  it("gives a selected 模型 chip the SAME active class as a selected 投入程度 chip", async () => {
    __injectMockTask(mkTask({}));
    const { findByTestId } = renderPage();
    const dialog = await openOutsourceFace(findByTestId);

    const ACTIVE = "task-reassign__seg-cell--active";

    // 投入程度 opens with 中 already selected (medium default) → active box.
    const effortMid = within(dialog).getByTestId("reassign-effort-medium");
    expect(effortMid.classList.contains(ACTIVE)).toBe(true);

    // 模型 opens with nothing selected (blank ⇒ runtime default); picking one
    // must give it the identical active class the effort chip carries.
    const modelSonnet = within(dialog).getByTestId("reassign-model-sonnet");
    expect(modelSonnet.classList.contains(ACTIVE)).toBe(false);
    fireEvent.click(modelSonnet);
    expect(modelSonnet.classList.contains(ACTIVE)).toBe(true);

    // Same visual token on both selectors — the parity the owner asked for.
    expect(effortMid.classList.contains(ACTIVE)).toBe(
      modelSonnet.classList.contains(ACTIVE)
    );
  });
});

describe("機器選擇 — 移除自動分配、明選必填 (owner 2026-07-19)", () => {
  function renderDialog(onReassign = vi.fn().mockResolvedValue(undefined)) {
    const onClose = vi.fn();
    const utils = render(
      <I18nProvider>
        <TaskReassignDialog
          task={mkTask({ executorKind: "outsource", executorId: "" })}
          members={[]}
          onReassign={onReassign}
          onClose={onClose}
        />
      </I18nProvider>
    );
    return { ...utils, onReassign, onClose };
  }

  async function pickOutsource(findByTestId: (id: string) => Promise<HTMLElement>) {
    fireEvent.click(await findByTestId("reassign-kind-outsource"));
  }

  it("轉外包 tab 機器清單不再有自動分配列", async () => {
    const { findByTestId, queryByTestId } = renderDialog();
    await pickOutsource(findByTestId);
    expect(queryByTestId("reassign-machine-auto")).toBeNull();
  });

  it("未選機器時擋住送出並提示,不呼叫 onReassign", async () => {
    const { findByTestId, findByText, onReassign } = renderDialog();
    await pickOutsource(findByTestId);
    fireEvent.click(await findByTestId("reassign-confirm"));
    expect(await findByText("請選擇要運行的機器")).toBeTruthy();
    expect(onReassign).not.toHaveBeenCalled();
  });

  it("選定機器後送出真機器 id(非 auto、非空)", async () => {
    const { findByTestId, container, onReassign } = renderDialog();
    await pickOutsource(findByTestId);

    // Machine rows are derived from the mock warden registry; wait for one.
    await waitFor(() =>
      expect(
        container.querySelector('[data-testid^="reassign-machine-"]')
      ).not.toBeNull()
    );
    const row = container.querySelector<HTMLElement>(
      '[data-testid^="reassign-machine-"]'
    )!;
    const machineId = row.dataset.testid!.replace("reassign-machine-", "");
    expect(machineId).not.toBe("");
    expect(machineId).not.toBe("auto");

    fireEvent.click(row);
    fireEvent.click(await findByTestId("reassign-confirm"));

    await waitFor(() => expect(onReassign).toHaveBeenCalledTimes(1));
    const input = onReassign.mock.calls[0][1];
    expect(input.target).toMatchObject({ kind: "outsource", machine: machineId });
  });
});
