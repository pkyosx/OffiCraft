// T-c21e ② — owner 2026-07-20, on the T-1d82 驗收 card:
//   「另外那些 ID 應該要跟任務卡上面顯示的一樣,任務卡上的 ID 似乎沒這麼長」
//
// 🔴 THAT PREMISE IS DEAD (T-5291, owner 2026-08-25). The number IS the id
// now, so «the row must not print the raw long id» has become «the row must
// print exactly the raw long id» — the two surfaces the complaint compared can
// no longer differ in LENGTH at all. What survives, and all this file still
// claims, is the part that still binds: the dep row and the dep's own card
// must show the SAME string. The old assertions are kept in that form rather
// than deleted, because the wiring they cover (the fallback rows reach for a
// number at all, and a resolved row prefers the SERVER's value) is still real.
//
// The complaint is about a MISMATCH between two surfaces, so every assertion
// below compares the dep row against the CARD, rather than against a literal
// typed out by hand. A test that only pinned "the row says T-1d82" would stay
// green if the card's own number changed shape — and then the two surfaces
// would disagree again with the suite still green, which is exactly the bug
// owner reported.
//
// The original regression had TWO mouths, not one: when a dep could not be
// resolved, both fallback branches printed the raw id because there was no
// server-supplied task_no. The predecessor ticket named only the `missing`
// one; `unresolved` is on screen far more often (every frame before the
// closed-inclusive fetch lands). Both are still exercised here — they are the
// only LIVE coverage of the `?? deriveTaskNo(id)` fallback in the suite (the
// sibling dep-status file never reaches it) — but what they now pin is that
// each row NAMES the dep, not that it names it shortly.
//
// ── WHAT THIS FILE DELIBERATELY DOES NOT DO ───────────────────────────────
// It does not re-test deriveTaskNo itself — that lives in lib/taskNo.test.ts,
// pinned against the SAME cases as the server's Go test. Duplicating them here
// would create a second place to update and a chance for the two to disagree.
// What this file owns is the WIRING.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { TaskCard } from "./TaskCard";
import { __resetMock, __injectMockTask } from "../api/mock";
import { deriveTaskNo } from "../lib/taskNo";
import type { TaskView } from "../api/adapter";

/** A real-shaped task id — 12 hex after the prefix, as the server mints them.
 * The fallback rows are fed THIS, and the point of the ticket is that the
 * number they print IS this id, unchanged — the fallback still sources it
 * from the server's task_no where there is one (see T-c21e ② below).
 *
 * 🔴 The line this replaces said the rows "print the DISPLAY number for it,
 * never the raw id". That was FALSE against the code — the fallback is
 * `dep?.taskNo ?? deriveTaskNo(id)` and `deriveTaskNo` is `return taskId`, so
 * the raw id is exactly what it prints — and it re-created the very split
 * (display number vs raw id) that T-5291 abolished, two lines above a comment
 * saying they are equal. The provenance point it was reaching for is real, so
 * it is kept, but stated as provenance ("where it is sourced from") instead of
 * as a value claim ("what it prints"), because on VALUE the two are now the
 * same thing. */
const LONG_ID = "t-1d8292a2f8db";

/** What the number for LONG_ID must be, written out. It is deliberately NOT
 * `deriveTaskNo(LONG_ID)`: the fixture below calls that function, so pinning
 * the expectation to it too would move both sides together and the test could
 * never fail. It is equal to LONG_ID by the T-5291 ruling — written as its own
 * constant so that a future projection would have to change this line, loudly,
 * instead of quietly following the implementation. */
const LONG_NO = "t-1d8292a2f8db";

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

function byTitle(cards: HTMLElement[], title: string): HTMLElement {
  const card = cards.find((c) =>
    c.querySelector(".task-card__title")?.textContent?.includes(title)
  );
  if (!card) throw new Error(`no card titled ${title}`);
  return card;
}

const NOOP = async () => {};


/** withDepJoin translates the old `depsResolvable` axis onto the mechanism that
 * replaced it (T-a3e4): the dep facts now ride the task itself as the SERVER's
 * `dep_tasks` join, not a lookup in the loaded list.
 *   resolved=true  → the server answered: every dep resolved against allTasks,
 *                    and a dep it does not know keeps an EMPTY status, which is
 *                    the 查無此任務 shape.
 *   resolved=false → no dep_tasks at all — the different, older silence
 *                    (「還不知道」): the card must not claim non-existence. */
function withDepJoin(
  task: TaskView,
  allTasks: TaskView[],
  resolved: boolean
): TaskView {
  if (!resolved) return { ...task, depTasks: undefined };
  return {
    ...task,
    depTasks: task.deps.map((id) => {
      const dep = allTasks.find((x) => x.id === id);
      return {
        id,
        // The server's own projection, called — NOT a third hand-rolled copy
        // of it. A local `T-${id.slice(2, 6)}` used to live here and drifted
        // the moment TaskNo stopped truncating, while still reading green.
        taskNo: dep?.taskNo ?? deriveTaskNo(id),
        title: dep?.title ?? "",
        status: dep?.status ?? "",
      };
    }),
  };
}

/** TaskCard rendered directly, so the dep-join shape can be pinned per case. */
function renderCard(task: TaskView, allTasks: TaskView[], resolvable: boolean) {
  return render(
    <I18nProvider>
      <TaskCard
        task={withDepJoin(task, allTasks, resolvable)}
        allTasks={allTasks}
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

describe("T-c21e ② 兩條 fallback 都叫得出 dep 的識別值", () => {
  // `unresolved` — the population is still the open-only fast path, so the row
  // cannot TITLE the dep yet. It can still NUMBER it: the number IS the id
  // (T-5291), which the row already holds, so no resolution is needed.
  it("unresolved 列顯示完整識別值", () => {
    const blocked = mkTask({ title: "被擋住的", deps: [LONG_ID] });
    const { container } = renderCard(blocked, [blocked], false);

    const [row] = depRows(container);
    expect(row.getAttribute("data-dep-state")).toBe("unresolved");
    expect(row.textContent).toContain(LONG_NO);
    // ⚠️ The old companion assertion `not.toContain(LONG_ID)` is GONE, not
    // relaxed: LONG_NO === LONG_ID now, so it directly contradicts the line
    // above. Keeping a guard that can only pass when the row is empty would be
    // theatre. What the row must not do — print NOTHING for a dep it cannot
    // resolve — is what the assertion above already covers.
  });

  // `missing` — the full population IS in hand and the dep is genuinely not in
  // it. Different sentence, same numbering rule; the predecessor fixed neither.
  it("missing 列顯示完整識別值", () => {
    const blocked = mkTask({ title: "被擋住的", deps: [LONG_ID] });
    const { container } = renderCard(blocked, [blocked], true);

    const [row] = depRows(container);
    expect(row.getAttribute("data-dep-state")).toBe("missing");
    expect(row.textContent).toContain(LONG_NO);
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
  it("兩種 fallback 形態的 dep 列,每一條都叫得出那張 dep 的識別值", () => {
    // Was 「沒有一條吐出原始長 id」. Inverted by T-5291: the identifier is the
    // number, so the sweep now asserts every row NAMES the dep rather than
    // that none of them does. A fallback branch added later that prints
    // nothing — the failure this sweep exists to catch — still fails here.
    const blocked = mkTask({ title: "被擋住的", deps: [LONG_ID] });

    for (const resolvable of [false, true]) {
      const { container, unmount } = renderCard(blocked, [blocked], resolvable);
      const rows = depRows(container);
      expect(rows).toHaveLength(1);
      for (const row of rows) {
        expect(row.textContent).toContain(LONG_NO);
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
    // t-9999ffffffff, the server's value is T-abcd. Only one can be on screen,
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
    expect(row.textContent).not.toContain("t-9999ffffffff");
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
    // The chip renders as 「#<task id>」 behind an icon; take what follows the #.
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
