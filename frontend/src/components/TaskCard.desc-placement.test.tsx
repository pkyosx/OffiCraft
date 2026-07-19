// TaskCard — spec ③ 詳細敘述搬到訊息輸入框下面 (owner 2026-07-17:「task details
// 應該在回覆訊息的下面」). Locked here:
//   1. The 詳細敘述 (.task-card__desc) renders AFTER the 傳訊息給 ○○… composer
//      in real DOM order — asserted with compareDocumentPosition / child index
//      against the card's own top-level blocks, never a mere "both exist".
//   2. It is still BEFORE the workflow timeline (the timeline keeps the tail).
//   3. Its expanded-only gate survived the move — a collapsed card shows no
//      description at all.
//   4. Owner's ruling scoped the move to THIS block: the head's meta column
//      (任務類型 / 負責人 / 建立者) stays ABOVE the composer, untouched.

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
    id: `step-${seq}`,
    name: `節點-${seq}`,
    dod: "",
    status: "pending",
    isGate: false,
    replyCardId: "",
    parallelGroup: "",
    orderIdx: seq,
    startedTs: 0,
    finishedTs: 0,
    ...over,
  };
}
function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "T-9fd5",
    taskNo: "T-9fd5",
    title: "敘述位置任務",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "high",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1000,
    updatedTs: 2000,
    closedTs: null,
    progressDone: 0,
    progressTotal: 1,
    steps: [],
    ...over,
  };
}

const MIRA = { id: "mira", name: "Mira", kind: "agent" } as unknown as Member;
const noop = async () => {};
const workers: OutsourceWorkerView[] = [];

function renderCard(
  task: TaskView,
  onHydrate: (id: string) => Promise<TaskView>
) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={[task]}
        members={[MIRA]}
        workers={workers}
        nowTs={3000}
        onTerminate={noop as never}
        onMarkDuplicate={noop as never}
        onSetPriority={noop as never}
        onReassign={noop as never}
        onSendMessage={noop as never}
        onHydrate={onHydrate}
      />
    </I18nProvider>
  );
}

/** a precedes b in document order (real DOM order, not mere existence). */
function precedes(a: Element, b: Element): boolean {
  return Boolean(
    a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING
  );
}

const DESC = "## 詳細內容\n這是任務的詳細敘述。";

describe("spec ③ 詳細敘述排在訊息輸入框之後", () => {
  it("renders the description AFTER the composer and BEFORE the workflow", async () => {
    const detail = mkTask({
      description: DESC,
      steps: [mkStep({ name: "第一個節點", status: "in_progress" })],
    });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const desc = container.querySelector(".task-card__desc")!;
    const composer = container.querySelector(".task-card__composer")!;
    const workflow = container.querySelector(".task-card__workflow")!;
    expect(desc).toBeTruthy();
    expect(composer).toBeTruthy();
    expect(workflow).toBeTruthy();

    // The move itself: composer → desc (this is the assertion the old layout
    // fails — it used to be desc → composer).
    expect(precedes(composer, desc)).toBe(true);
    expect(precedes(desc, composer)).toBe(false);
    // …and the workflow still owns the tail.
    expect(precedes(desc, workflow)).toBe(true);
  });

  it("sits below the composer as the card's own top-level block (child index)", async () => {
    const detail = mkTask({ description: DESC });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const card = await findByTestId("task-card");
    const kids = [...card.children];
    const desc = container.querySelector(".task-card__desc")!;
    const composer = container.querySelector(".task-card__composer")!;
    // Both are direct children of the card article — the desc did not get
    // buried inside the composer (or anywhere else) by the move.
    const descIdx = kids.indexOf(desc);
    const composerIdx = kids.indexOf(composer);
    expect(descIdx).toBeGreaterThan(-1);
    expect(composerIdx).toBeGreaterThan(-1);
    expect(descIdx).toBeGreaterThan(composerIdx);
    // The progress bar (never moved) still precedes the composer — the move
    // did not reshuffle the blocks around it.
    const progressIdx = kids.indexOf(
      container.querySelector(".task-card__progress")!
    );
    expect(progressIdx).toBeLessThan(composerIdx);
  });

  it("keeps its expanded-only gate: a collapsed card renders no description", async () => {
    const detail = mkTask({ description: DESC });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    // Collapsed on first paint — nothing hydrated, nothing shown.
    const card = await findByTestId("task-card");
    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(container.querySelector(".task-card__desc")).toBeNull();
    expect(card.textContent).not.toContain("這是任務的詳細敘述");

    // Expand → it appears (below the composer).
    fireEvent.click(card);
    await act(async () => {});
    expect(container.querySelector(".task-card__desc")).toBeTruthy();

    // Collapse again → it goes away, even though the detail is now hydrated.
    fireEvent.click(card);
    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(container.querySelector(".task-card__desc")).toBeNull();
  });

  it("owner scoped the move to the description ONLY: the head meta column stays above the composer", async () => {
    const detail = mkTask({ description: DESC });
    const onHydrate = vi.fn(async () => detail);
    const { container, findByTestId } = renderCard(mkTask({}), onHydrate);

    fireEvent.click(await findByTestId("task-card"));
    await act(async () => {});

    const meta = container.querySelector(".task-card__meta")!;
    const composer = container.querySelector(".task-card__composer")!;
    const desc = container.querySelector(".task-card__desc")!;
    // 任務類型 / 負責人 / 建立者 did NOT ride along — they stay in the head,
    // above the composer, while the desc went below it.
    expect(meta.textContent).toContain("負責人");
    expect(precedes(meta, composer)).toBe(true);
    expect(precedes(meta, desc)).toBe(true);
    expect(meta.closest(".task-card__head")).toBeTruthy();
  });
});
