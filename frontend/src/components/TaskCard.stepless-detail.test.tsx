// T-71e8 — the 座艙 task card must never show the transitional empty state
// (「等待 ○○ 建立 Steps」) when the light list's progress already reports leaves
// (4/6) but the hydrated detail hasn't delivered steps yet.
//
// The card renders progress from the LIGHT list prop (task.progressTotal) but
// decides the transitional empty state vs the timeline from the HYDRATED detail
// (view.steps.length). The loading / error guards both require
// task.progressTotal > 0 before they fire; the fix extends the same gate to the
// empty branch — when progressTotal > 0 but view.steps is empty (the pre-hydrate
// frame, or a stepless getTask resolve) the card shows the LOADING placeholder,
// never the transitional copy that would contradict a 4/6 progress bar.
//
// These tests pinned the defect (originally red); they pass once the
// progressTotal gate is added to the empty branch.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskCard } from "./TaskCard";
import type { Member } from "../types";
import type { TaskView, TaskStepView, OutsourceWorkerView } from "../api/adapter";

let seq = 0;
function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`, name: `節點-${seq}`, dod: "", status: "pending",
    isGate: false, replyCardId: "", parallelGroup: "", orderIdx: seq,
    startedTs: 0, finishedTs: 0, ...over,
  };
}
function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "T-e987", taskNo: "T-e987", title: "高頻更新的任務", typeKey: "",
    description: "", status: "in_progress", priority: "high",
    executorKind: "member", executorId: "mira", creatorId: "", dedupeKey: "",
    deps: [], waitingReason: "", duplicateOf: "", createdTs: 1000, updatedTs: 2000,
    closedTs: null, progressDone: 4, progressTotal: 6, steps: [], ...over,
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const SIX_STEPS = [0, 1, 2, 3, 4, 5].map((i) =>
  mkStep({ name: `節點${i}`, status: i < 4 ? "done" : "pending", orderIdx: i })
);

const noop = async () => {};

function renderCard(
  task: TaskView,
  onHydrate: (id: string) => Promise<TaskView>,
  workers: OutsourceWorkerView[] = []
) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task} allTasks={[task]} members={[MIRA]} workers={workers} nowTs={3000}
        onTerminate={noop as never} onMarkDuplicate={noop as never} onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never} onHydrate={onHydrate}
      />
    </I18nProvider>
  );
}

function cardEl(task: TaskView, onHydrate: (id: string) => Promise<TaskView>) {
  return (
    <I18nProvider>
      <TaskCard
        task={task} allTasks={[task]} members={[MIRA]} workers={[]} nowTs={3000}
        onTerminate={noop as never} onMarkDuplicate={noop as never} onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never} onHydrate={onHydrate}
      />
    </I18nProvider>
  );
}

describe("TaskCard stepless-detail (T-71e8)", () => {
  it("shows loading (not the transitional empty state) when progress says 4/6 but the hydrate resolves stepless", async () => {
    // getTask resolves with a FULL task that carries no steps (matching id).
    const emptyDetail = mkTask({ steps: [] });
    const onHydrate = vi.fn(async () => emptyDetail);

    const { container, findByTestId, queryByTestId } = renderCard(
      mkTask({ progressDone: 4, progressTotal: 6 }),
      onHydrate
    );

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {}); // let the hydrate resolve

    // Progress bar still says 4/6 (light list) — so the workflow area must NOT
    // claim the transitional empty state; it shows the loading placeholder.
    expect(container.querySelector('[data-testid="task-progress"]')!.textContent)
      .toContain("4/6");
    expect(queryByTestId("task-transition")).toBeNull();
    expect(queryByTestId("task-steps-loading")).not.toBeNull();
  });

  it("never falls back to the transitional empty state when a re-hydrate (updatedTs bump) resolves stepless", async () => {
    // First hydrate → 6 steps (timeline). A later re-hydrate (driven by an
    // updatedTs bump, i.e. the high-frequency SSE churn on T-e987) resolves
    // stepless. Because progressTotal > 0 the card falls back to the loading
    // placeholder — never the transitional 「等待建立 Steps」 copy.
    const good = mkTask({ steps: SIX_STEPS });
    const empty = mkTask({ steps: [] });
    let call = 0;
    const onHydrate = vi.fn(async () => (++call === 1 ? good : empty));

    const { findByTestId, findAllByTestId, queryByTestId, rerender } =
      renderCard(mkTask({ updatedTs: 2000 }), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});
    expect((await findAllByTestId("task-step")).length).toBe(6); // timeline is up

    // One SSE-driven refetch bumps updatedTs → the card re-hydrates.
    rerender(cardEl(mkTask({ updatedTs: 2001 }), onHydrate));
    await act(async () => {});

    // A stepless re-hydrate must not surface the transitional empty state.
    expect(queryByTestId("task-transition")).toBeNull();
    expect(queryByTestId("task-steps-loading")).not.toBeNull();
  });
});

// (B) owner UX (2026-07): the genuinely-no-plan state (assigned, zero leaves,
// progressTotal === 0) reads 「等待 ○○ 建立 Steps」, ○○ = the executor's display
// name (member name / 外包 代號); unassigned keeps 「等待指派」. Mounted directly
// so members/workers are supplied synchronously — the copy is deterministic.
describe("TaskCard transitional copy (T-71e8 · B)", () => {
  it("assigned member + zero leaves → 「等待 <name> 建立 Steps」", async () => {
    const onHydrate = vi.fn(async () =>
      mkTask({ executorKind: "member", executorId: "mira", progressDone: 0, progressTotal: 0, steps: [] })
    );
    const { findByTestId } = renderCard(
      mkTask({ executorKind: "member", executorId: "mira", progressDone: 0, progressTotal: 0 }),
      onHydrate
    );
    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});
    expect((await findByTestId("task-transition")).textContent).toBe(
      "等待 Mira 建立 Steps"
    );
  });

  it("assigned outsource + zero leaves → 「等待 <codename> 建立 Steps」", async () => {
    const worker = {
      id: "ow-1", codename: "O-7", model: "opus", effort: "high", taskId: "T-e987",
    } as unknown as OutsourceWorkerView;
    const onHydrate = vi.fn(async () =>
      mkTask({ executorKind: "outsource", executorId: "ow-1", progressDone: 0, progressTotal: 0, steps: [] })
    );
    const { findByTestId } = renderCard(
      mkTask({ executorKind: "outsource", executorId: "ow-1", progressDone: 0, progressTotal: 0 }),
      onHydrate,
      [worker]
    );
    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});
    expect((await findByTestId("task-transition")).textContent).toBe(
      "等待 外包 · O-7 建立 Steps"
    );
  });

  it("unassigned outsource → 「等待指派」 (unchanged)", async () => {
    const onHydrate = vi.fn(async () =>
      mkTask({ executorKind: "outsource", executorId: "", progressDone: 0, progressTotal: 0, steps: [] })
    );
    const { findByTestId } = renderCard(
      mkTask({ executorKind: "outsource", executorId: "", progressDone: 0, progressTotal: 0 }),
      onHydrate
    );
    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});
    expect((await findByTestId("task-transition")).textContent).toBe("等待指派");
  });
});
