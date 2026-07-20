// T-c21e ② — owner 2026-07-20, on the T-1d82 驗收 card:
//   「另外那些 ID 應該要跟任務卡上面顯示的一樣,任務卡上的 ID 似乎沒這麼長」
//
// The complaint is about a MISMATCH between two surfaces, so every assertion
// below compares the dep row against the CARD, rather than against a literal
// typed out by hand. A test that only pinned "the row says T-1d82" would stay
// green if the card's own number changed shape — and then the two surfaces
// would disagree again with the suite still green, which is exactly the bug
// owner reported.
//
// The regression had TWO mouths, not one. When a dep could not be resolved
// against the frontend's task list, both fallback branches printed the raw id
// (`t-1d8292a2f8db`) because there was no server-supplied task_no to print.
// The predecessor ticket named only the `missing` one; `unresolved` is on the
// screen far more often (it is every frame before the closed-inclusive fetch
// lands). Both are pinned here.
//
// ── WHAT THIS FILE DELIBERATELY DOES NOT DO ───────────────────────────────
// It does not re-test deriveTaskNo's projection rules — the id→number edges
// (prefix trimming, short ids) live in lib/taskNo.test.ts, pinned against the
// SAME cases as the server's own Go test. Duplicating them here would create
// a second place to update and a chance for the two to disagree. What this
// file owns is the WIRING: that the dep row reaches for the derived number at
// all, and that a resolved row still prefers the server's value.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { TaskCard } from "./TaskCard";
import { __resetMock, __injectMockTask } from "../api/mock";
import type { TaskView } from "../api/adapter";

/** A real-shaped task id — 12 hex after the prefix, as the server mints them.
 * The fallback rows are fed THIS, and the point of the ticket is that they
 * must not print it whole. */
const LONG_ID = "t-1d8292a2f8db";

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

function byTitle(cards: HTMLElement[], title: string): HTMLElement {
  const card = cards.find((c) =>
    c.querySelector(".task-card__title")?.textContent?.includes(title)
  );
  if (!card) throw new Error(`no card titled ${title}`);
  return card;
}

const NOOP = async () => {};

/** TaskCard rendered directly, so `depsResolvable` can be pinned — the flag is
 * what splits the two fallback shapes. */
function renderCard(task: TaskView, allTasks: TaskView[], resolvable: boolean) {
  return render(
    <I18nProvider>
      <TaskCard
        task={task}
        allTasks={allTasks}
        depsResolvable={resolvable}
        members={[]}
        workers={[]}
        nowTs={Date.now() / 1000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onReassign={NOOP as never}
        onSendMessage={NOOP as never}
        onHydrate={NOOP as never}
        onRemoveArtifact={NOOP as never}
      />
    </I18nProvider>
  );
}

function depRows(root: HTMLElement | Document): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>('[data-testid="task-dep"]')];
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});
afterEach(() => vi.restoreAllMocks());

describe("T-c21e ② 兩條 fallback 都不再吐原始長 id", () => {
  // `unresolved` — the population is still the open-only fast path, so the row
  // cannot name the dep yet. It can still NUMBER it: the short form is a pure
  // projection of the id, available without resolving anything.
  it("unresolved 列顯示短編號,且畫面上找不到原始長 id", () => {
    const blocked = mkTask({ title: "被擋住的", deps: [LONG_ID] });
    const { container } = renderCard(blocked, [blocked], false);

    const [row] = depRows(container);
    expect(row.getAttribute("data-dep-state")).toBe("unresolved");
    expect(row.textContent).toContain("T-1d82");
    // The regression itself, stated as the thing that must be absent.
    expect(row.textContent).not.toContain(LONG_ID);
  });

  // `missing` — the full population IS in hand and the dep is genuinely not in
  // it. Different sentence, same numbering rule; the predecessor fixed neither.
  it("missing 列顯示短編號,且畫面上找不到原始長 id", () => {
    const blocked = mkTask({ title: "被擋住的", deps: [LONG_ID] });
    const { container } = renderCard(blocked, [blocked], true);

    const [row] = depRows(container);
    expect(row.getAttribute("data-dep-state")).toBe("missing");
    expect(row.textContent).toContain("T-1d82");
    expect(row.textContent).not.toContain(LONG_ID);
  });

  // Both shapes at once, phrased the way owner would check it: scan the rows,
  // see nothing long. Guards against another fallback branch being added with
  // the raw id — the failure mode this ticket exists to close.
  // Not a hypothetical: review found a THIRD mouth already in this file (the
  // duplicated-original row, ~60 lines below the dep block) wearing the exact
  // same `?? rawId` shape. It is fixed and pinned in
  // TaskCard.duplicate-link.test.tsx. Stating it here because an earlier draft
  // of this comment called the third branch "future" — it was already present
  // while the sentence claimed otherwise.
  it("兩種 fallback 形態的 dep 列,沒有一條吐出原始長 id", () => {
    const blocked = mkTask({ title: "被擋住的", deps: [LONG_ID] });

    for (const resolvable of [false, true]) {
      const { container, unmount } = renderCard(blocked, [blocked], resolvable);
      for (const row of depRows(container)) {
        expect(row.textContent).not.toContain(LONG_ID);
      }
      unmount();
    }
  });
});

describe("T-c21e ② 解析得到的 dep 仍以 server 的 task_no 為準", () => {
  // Deriving is the FALLBACK, not the default. A resolved row must keep
  // printing the value the server sent, so that if the projection ever changes
  // server-side the surface that matters most agrees with it for free. The
  // mutant this catches is "now that we have a helper, use it everywhere".
  it("非終態 dep 列印的是 dep.taskNo,不是從 id 派生的值", async () => {
    // taskNo and id are deliberately INCONSISTENT: derivation would yield
    // T-9999, the server's value is T-abcd. Only one of them can be on screen,
    // so this distinguishes the two sources — which identical values could not.
    const dep = mkTask({
      id: "t-9999ffffffff",
      taskNo: "T-abcd",
      title: "擋路的",
      status: "in_progress",
    });
    const blocked = mkTask({ title: "被擋住的", deps: [dep.id] });
    const { container } = renderCard(blocked, [blocked, dep], true);

    const [row] = depRows(container);
    expect(row.getAttribute("data-dep-state")).toBe("open");
    expect(row.textContent).toContain("T-abcd");
    expect(row.textContent).not.toContain("T-9999");
  });
});

describe("T-c21e ② dep 列的編號與那張 dep 自己卡片上的編號一致", () => {
  // The complaint was a mismatch BETWEEN SURFACES, so this reads both off the
  // rendered page and compares them — no hand-typed expected value. If the
  // card's own number ever changes shape, this fails instead of silently
  // letting the two drift apart again.
  it("dep 列上的編號,逐字元等於該 dep 卡片狀態列旁顯示的編號", async () => {
    const dep = mkTask({ title: "擋路的", status: "in_progress" });
    const blocked = mkTask({ title: "被擋住的", deps: [dep.id] });
    __injectMockTask(dep);
    __injectMockTask(blocked);

    const { findAllByTestId } = render(
      <I18nProvider>
        <TasksPage />
      </I18nProvider>
    );
    const cards = await findAllByTestId("task-card");
    const blockedCard = byTitle(cards, "被擋住的");

    await waitFor(() => {
      expect(
        depRows(blockedCard).filter(
          (d) => d.getAttribute("data-dep-state") === "unresolved"
        )
      ).toHaveLength(0);
    });

    // Read the number off the DEP's own card, the way owner does.
    const depCard = byTitle(await findAllByTestId("task-card"), "擋路的");
    // The chip renders as 「#T-xxxx」 behind an icon; take what follows the #.
    const onCard = depCard
      .querySelector('[data-testid="task-no"]')
      ?.textContent?.split("#")
      .pop()
      ?.trim();
    expect(onCard).toBeTruthy();
    expect(onCard).toMatch(/^T-/);

    const [row] = depRows(blockedCard);
    expect(row.textContent).toContain(onCard as string);
  });
});
