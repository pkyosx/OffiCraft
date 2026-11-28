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

import { afterEach, describe, it, expect, vi } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
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
    executorKind: "staff",
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
    const descBlock = container.querySelector('[data-testid="task-desc-section"]')!;
    const composer = container.querySelector(".task-card__composer")!;
    // The card's own top-level child is the framed 任務描述 section, and the
    // rendered description lives inside THAT section — so it cannot have been
    // buried inside the composer or anywhere else.
    expect(desc.closest('[data-testid="task-desc-section"]')).toBe(descBlock);
    const descIdx = kids.indexOf(descBlock);
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

    const meta = container.querySelector(".card-meta")!;
    const composer = container.querySelector(".task-card__composer")!;
    const desc = container.querySelector(".task-card__desc")!;
    // 任務類型 / 負責人 / 建立者 did NOT ride along — they stay in the head,
    // above the composer, while the desc went below it.
    expect(meta.textContent).toContain("負責人");
    expect(precedes(meta, composer)).toBe(true);
    expect(precedes(meta, desc)).toBe(true);
    expect(meta.closest(".task-card__head")).toBeTruthy();
  });

  describe("the framed 任務描述 section", () => {
    afterEach(() => vi.restoreAllMocks());

    // jsdom lays nothing out: the clamped body reports one height unit per
    // character against a box of 40, at a line height of 10.
    function fakeLayout() {
      vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockImplementation(function (this: HTMLElement) {
        return this.dataset.testid === "task-desc-body" ? (this.textContent ?? "").length : 0;
      });
      vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(function (this: HTMLElement) {
        return this.dataset.testid === "task-desc-body" ? 40 : 0;
      });
      const real = window.getComputedStyle.bind(window);
      vi.spyOn(window, "getComputedStyle").mockImplementation((el, pseudo) => {
        const style = real(el, pseudo);
        if ((el as HTMLElement).dataset.testid !== "task-desc-body") return style;
        return Object.assign(Object.create(style), {
          getPropertyValue: (name: string) => (name === "--clamped-text-line-height" ? "10px" : style.getPropertyValue(name)),
        });
      });
    }

    async function expand(description: string) {
      const detail = mkTask({ description });
      const view = renderCard(mkTask({}), vi.fn(async () => detail));
      fireEvent.click(await view.findByTestId("task-card"));
      await act(async () => {});
      return view;
    }

    it("a long description is clamped with a fade and 展開全文 that opens and closes it", async () => {
      fakeLayout();
      const { getByTestId } = await expand("字".repeat(200));

      const body = getByTestId("task-desc-body");
      const toggle = getByTestId("task-desc-toggle");
      expect(body.classList.contains("clamped-text__body--clamped")).toBe(true);
      expect(body.classList.contains("clamped-text__body--faded")).toBe(true);
      expect(toggle.textContent).toBe(zh.common.textShowAll);

      fireEvent.click(toggle);
      expect(toggle.textContent).toBe(zh.common.textShowLess);
      expect(body.classList.contains("clamped-text__body--clamped")).toBe(false);
      expect(getByTestId("task-card").getAttribute("aria-expanded")).toBe("true");

      fireEvent.click(toggle);
      expect(body.classList.contains("clamped-text__body--faded")).toBe(true);
    });

    it("a description that fits shows neither the fade nor 展開全文", async () => {
      fakeLayout();
      const { getByTestId, queryByTestId } = await expand("短");
      expect(queryByTestId("task-desc-toggle")).toBeNull();
      expect(getByTestId("task-desc-body").classList.contains("clamped-text__body--faded")).toBe(false);
    });

    it("its title bar is the icon, 任務描述 and the chevron only, and hides and restores the whole body without collapsing the card", async () => {
      fakeLayout();
      const { container, getByTestId } = await expand(DESC);
      const head = getByTestId("task-desc-section-head");
      expect(head.textContent).toBe("任務描述");
      expect(Array.from(head.children).map((el) => el.className)).toEqual([
        "task-section__icon",
        "task-section__title",
        "task-section__chevron",
      ]);
      expect(container.querySelector('[data-testid="task-desc-section-summary"]')).toBeNull();
      expect(head.getAttribute("aria-expanded")).toBe("true");

      fireEvent.click(head);
      expect(head.getAttribute("aria-expanded")).toBe("false");
      expect(container.querySelector(".task-card__desc")).toBeNull();
      expect(getByTestId("task-card").getAttribute("aria-expanded")).toBe("true");
      expect(container.querySelector(".task-card__workflow")).toBeTruthy();

      fireEvent.click(head);
      expect(container.querySelector(".task-card__desc")).toBeTruthy();
    });
  });
});
