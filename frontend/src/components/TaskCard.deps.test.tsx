// T-1d82 — the dep row stops being a dead id print. Locked here, one describe
// per shape owner named:
//
//   ① 非終態 dep — 編號 + 標題 on the row, the whole row clicks through to that
//     task's card (#tasks/<id>), and the full title is reachable on hover.
//   ② 終態 dep — resolves AT ALL (this is the root-cause guard, see below) and
//     recedes: ✓ instead of ⏱, the --dep-closed modifier, still in the list,
//     still clickable.
//   ③ 壞 id — says so in words, and is NOT a button: there is no card to land
//     on, so a click-through would dump the user on an empty filter.
//
// ── WHY ② IS THE LOAD-BEARING TEST ────────────────────────────────────────
// The lookup (allTasks.find) always existed. What owner screenshotted —
// 「等 t-35e06c8e63c8」 — was that lookup MISSING, because TasksPage loads
// 未結束-only by default (T-2b9d ?open=true) and a dep that has closed is
// simply not in that population. The mock honours `open` byte-for-byte
// (mock.ts listTasks), so ② renders through the same fast path the real app
// does: revert TasksPage's needClosed dep clause and ② goes red with the raw
// id, which is exactly the bug. It is not a styling test wearing a data hat.
//
// ── WHAT THIS FILE CANNOT SEE ─────────────────────────────────────────────
// jsdom parses no stylesheet: the DIMMING of a terminal dep and the ellipsis
// truncation of a long title are CSS (tasks.css .task-card__waiting--dep-closed
// / .task-card__dep-title) and are invisible here. So nothing below asserts
// "it looks faded" — only that the class carrying the fade is applied and that
// the title attribute (the affordance truncation depends on) is present with
// the FULL text. Asserting the look in jsdom would launder an unverified claim
// as a test; the look is owner's 控制台驗收.

import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { mockApi } from "../api/mock";
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

/** Toggle one option in a multi-select filter dropdown — same helper as
 * TasksPage.test.tsx. */
function toggleFilter(testId: string, value: string) {
  const trigger = document.querySelector(`[data-testid="${testId}"]`)!;
  if (trigger.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(trigger);
  }
  const checkbox = document.querySelector(
    `[data-testid="${testId}-opt-${value}"] input`
  )!;
  fireEvent.click(checkbox);
}

function byTitle(cards: HTMLElement[], title: string): HTMLElement {
  const card = cards.find((c) =>
    c.querySelector(".task-card__title")?.textContent?.includes(title)
  );
  if (!card) throw new Error(`no card titled ${title}`);
  return card;
}

/** The blocked card's dep rows, in render order, AFTER the page has the data
 * to resolve them.
 *
 * The wait is load-bearing, not defensive politeness. A terminal dep needs the
 * closed-inclusive fetch, which lands a tick or two after mount — until then
 * every row honestly reports `unresolved`. Reading the DOM immediately makes
 * these tests race the fetch: they pass or fail on machine speed, which is
 * precisely how a suite starts lying. (The full CI run caught this where the
 * isolated file run did not.) */
async function depsOf(title: string): Promise<HTMLElement[]> {
  const { findAllByTestId } = renderPage();
  const card = byTitle(await findAllByTestId("task-card"), title);
  const rows = () => [
    ...card.querySelectorAll<HTMLElement>('[data-testid="task-dep"]'),
  ];
  await waitFor(() => {
    const pending = rows().filter(
      (d) => d.getAttribute("data-dep-state") === "unresolved"
    );
    expect(pending).toHaveLength(0);
  });
  return rows();
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("T-a3e4 資料供給: 勾什麼就問什麼 — dep 不再讓它拉整部歷史", () => {
  it("a LIVE task carrying a dep does NOT widen the fetch", async () => {
    // 🔴 This is the exact inverse of what T-1d82 pinned here, and the inversion
    // is the ticket: T-1d82 widened the fetch whenever any live task had a dep,
    // because the dep row resolved its title out of the loaded list. With deps
    // on live tasks being ordinary, the widened fetch became the NORMAL one —
    // 408,482 B on every task SSE delta versus 17,295 B for the rows on screen.
    // The dep facts ride the response now (dep_tasks), so the widening is gone.
    const spy = vi.spyOn(mockApi, "listTasks");
    const blocker = mkTask({ taskNo: "T-dddd", title: "前置", status: "done" });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "有 dep 也不放棄篩選", deps: [blocker.id] }));

    const { findAllByTestId } = renderPage();
    await findAllByTestId("task-card");
    // Every call carries a status set — none of them is the unconstrained fetch.
    await waitFor(() => expect(spy.mock.calls.length).toBeGreaterThan(0));
    expect(
      spy.mock.calls.every(([opts]) => (opts?.statuses ?? []).length > 0)
    ).toBe(true);
    // And the set is the DEFAULT view's, stated as values: five non-terminal
    // states, terminals excluded. "It sent something" is not the contract.
    expect([...(spy.mock.calls[0][0]?.statuses ?? [])].sort()).toEqual([
      "in_progress",
      "not_started",
      "reassigning",
      "waiting_external",
      "waiting_owner",
    ]);
  });

  it("…and the dep is STILL named, even though its status was never asked for", async () => {
    // Both halves live in THIS test, not one each: naming a closed dep was
    // already true on e7120c5 (it got there by downloading the archive), so the
    // naming assertion ALONE has no discriminating power. What is new is naming
    // it while the fetch stays narrow — so the narrowness is asserted here too.
    const spy = vi.spyOn(mockApi, "listTasks");
    const blocker = mkTask({
      taskNo: "T-dcdc",
      title: "早就做完的前置",
      status: "done",
      closedTs: Date.now() / 1000 - 500,
    });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "被結案的擋", deps: [blocker.id] }));

    const [dep] = await depsOf("被結案的擋");
    expect(dep.getAttribute("data-dep-state")).toBe("closed");
    expect(dep.textContent).toContain("T-dcdc");
    expect(dep.textContent).toContain("早就做完的前置");
    // …and nothing widened to get that name: every fetch carried a status set,
    // and none of them asked for the terminal state the blocker is in.
    expect(spy.mock.calls.length).toBeGreaterThan(0);
    expect(
      spy.mock.calls.every(
        ([opts]) =>
          (opts?.statuses ?? []).length > 0 && !opts?.statuses?.includes("done")
      )
    ).toBe(true);
  });

  it("ticking a terminal status asks for THAT status — and unticking gives it back", async () => {
    // The T-1d82 latch guard, re-aimed: the failure it caught was a view that
    // never returned to the cheap ask. Here the ask is the set itself, so the
    // observable is the set's value before / during / after.
    __injectMockTask(mkTask({ title: "活著", status: "in_progress" }));
    __injectMockTask(
      mkTask({
        title: "已結束",
        status: "done",
        closedTs: Date.now() / 1000 - 100,
      })
    );
    const { findAllByTestId } = renderPage();
    await findAllByTestId("task-card");
    const spy = vi.spyOn(mockApi, "listTasks");

    toggleFilter("filter-status", "done");
    await waitFor(() =>
      expect(
        spy.mock.calls.some(([opts]) => opts?.statuses?.includes("done"))
      ).toBe(true)
    );

    spy.mockClear();
    toggleFilter("filter-status", "done");
    await waitFor(() => expect(spy.mock.calls.length).toBeGreaterThan(0));
    // Back to the default ask: done is gone again, and it never degenerates
    // into "no constraint" (which would be the whole archive).
    expect(
      spy.mock.calls.every(
        ([opts]) =>
          (opts?.statuses ?? []).length > 0 && !opts?.statuses?.includes("done")
      )
    ).toBe(true);
  });
});

describe("T-1d82 ① 非終態 dep: 編號 + 標題, 整列可點跳轉", () => {
  it("shows the dep's 編號 AND title — the id alone never said what we were waiting for", async () => {
    const blocker = mkTask({
      taskNo: "T-35e0",
      title: "先把 SSE 重連補起來",
      status: "in_progress",
    });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "被擋的", deps: [blocker.id] }));

    const [dep] = await depsOf("被擋的");
    expect(dep.querySelector(".task-card__dep-no")?.textContent).toBe(
      "等 T-35e0"
    );
    expect(dep.querySelector(".task-card__dep-title")?.textContent).toBe(
      "先把 SSE 重連補起來"
    );
    // Negative control: the raw id is what the old row printed. It must be
    // gone from the visible text now that the row can name the task.
    expect(dep.textContent).not.toContain(blocker.id);
    // Non-terminal → the live shape: no 收斂 modifier, and it stays marked open.
    expect(dep.classList.contains("task-card__waiting--dep-closed")).toBe(
      false
    );
    expect(dep.getAttribute("data-dep-state")).toBe("open");
  });

  it("the whole row is a button that routes to that task's card", async () => {
    const blocker = mkTask({ taskNo: "T-35e0", title: "擋路的" });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "被擋的2", deps: [blocker.id] }));

    const [dep] = await depsOf("被擋的2");
    // A <div> with an onClick would look identical in a screenshot and be
    // unreachable by keyboard — the element TYPE is the accessibility claim.
    expect(dep.tagName).toBe("BUTTON");
    // 🔴 The accessible NAME must stay the row's own text (編號 + 標題). An
    // earlier cut labelled the button 「跳到 T-35e0」 via aria-label, which
    // WINS over descendant text in accname computation and would have deleted
    // the dep title from the a11y tree — this ticket's whole payload, removed
    // for the users who most need it. `title` describes the ACTION without
    // displacing the name (the duplicated-link's convention).
    expect(dep.getAttribute("aria-label")).toBeNull();
    expect(dep.getAttribute("title")).toBe("跳到 T-35e0");
    expect(dep.textContent).toContain("T-35e0");
    expect(dep.textContent).toContain("擋路的");

    fireEvent.click(dep);
    // Same anchor the duplicated-link and 請示卡 jumps already use — arriving
    // there filters the list to that one task (TasksPage.jump.test.tsx).
    expect(window.location.hash).toBe(`#tasks/${blocker.id}`);
  });

  it("a long title keeps its FULL text in the title attribute (what hover reads)", async () => {
    const long =
      "控制台任務卡 deps「等 t-xxx」列可點跳轉+顯示 task 編號與標題;終態 dep 收斂顯示";
    const blocker = mkTask({ taskNo: "T-ea82", title: long });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "被長標題擋的", deps: [blocker.id] }));

    const [dep] = await depsOf("被長標題擋的");
    const titleSpan = dep.querySelector(".task-card__dep-title")!;
    // The row truncates to one line in CSS; the attribute is the only place the
    // rest of the title survives, so it must carry the WHOLE string.
    expect(titleSpan.getAttribute("title")).toBe(long);
  });
});

describe("T-1d82 ② 終態 dep: 解析得到 + 淡化收斂, 仍留在列", () => {
  // done / terminated / duplicated — all three are terminal, and all three are
  // dropped by the ?open=true fast path, so each one independently proves the
  // needClosed fix.
  for (const status of ["done", "terminated", "duplicated"] as const) {
    it(`resolves a ${status} dep to 編號 + 標題 despite the 未結束-only fast path`, async () => {
      const blocker = mkTask({
        taskNo: "T-35e0",
        title: "已經做完的前置",
        status,
        closedTs: Date.now() / 1000 - 100,
      });
      __injectMockTask(blocker);
      __injectMockTask(mkTask({ title: `被${status}擋的`, deps: [blocker.id] }));

      const [dep] = await depsOf(`被${status}擋的`);
      // 🔴 The regression owner reported: without the closed population loaded,
      // this row falls back to the raw id and neither line below can pass.
      expect(dep.querySelector(".task-card__dep-no")?.textContent).toBe(
        "等 T-35e0"
      );
      expect(dep.querySelector(".task-card__dep-title")?.textContent).toBe(
        "已經做完的前置"
      );
      expect(dep.textContent).not.toContain("查無此任務");
    });
  }

  it("recedes rather than disappears: ✓ + the 收斂 modifier, still clickable", async () => {
    const blocker = mkTask({
      taskNo: "T-35e0",
      title: "已完成前置",
      status: "done",
    });
    __injectMockTask(blocker);
    __injectMockTask(mkTask({ title: "被完成的擋", deps: [blocker.id] }));

    const [dep] = await depsOf("被完成的擋");
    // Kyle's default, owner may retune at 驗收: it KEEPS its row (the blocking
    // history is context) and only gives up attention.
    expect(dep.getAttribute("data-dep-state")).toBe("closed");
    expect(dep.classList.contains("task-card__waiting--dep-closed")).toBe(true);
    // Still the dep block's own visual language — the modifier dims it, it does
    // not defect to some other component's styling.
    expect(dep.classList.contains("task-card__waiting--dep")).toBe(true);
    // Still reachable: 收斂 is about attention, not about removing the link.
    expect(dep.tagName).toBe("BUTTON");
    fireEvent.click(dep);
    expect(window.location.hash).toBe(`#tasks/${blocker.id}`);
  });

  it("a card blocked by one live and one closed dep shows both, distinguished", async () => {
    const live = mkTask({ taskNo: "T-aaaa", title: "還在跑" });
    const closed = mkTask({ taskNo: "T-bbbb", title: "收工了", status: "done" });
    __injectMockTask(live);
    __injectMockTask(closed);
    __injectMockTask(mkTask({ title: "兩個 dep", deps: [live.id, closed.id] }));

    const deps = await depsOf("兩個 dep");
    expect(deps).toHaveLength(2);
    // Order follows task.deps, and the two states are told apart — which is the
    // entire point of 收斂: the live one is what still deserves attention.
    expect(deps.map((d) => d.getAttribute("data-dep-state"))).toEqual([
      "open",
      "closed",
    ]);
  });
});

describe("T-1d82 ③ 壞 id: 明說查無, 不可點, 不炸版", () => {
  it("names the unresolvable id in words instead of pretending it is a link", async () => {
    __injectMockTask(
      mkTask({ title: "被鬼擋的", deps: ["t-deadbeefdead"] })
    );

    const [dep] = await depsOf("被鬼擋的");
    expect(dep.getAttribute("data-dep-state")).toBe("missing");
    // A handle on whatever this was STAYS — it just no longer masquerades as a
    // resolved dep. T-c21e narrowed WHICH handle: owner 2026-07-20 asked for
    // the id here to match the card's, so it is now the short number rather
    // than the raw id this line used to pin. The tradeoff is deliberate and
    // worth naming: the short form is display-only and can collide, so it is a
    // weaker handle than the full id for anyone debugging from a screenshot.
    // Consistency with every other surface won. The full id survives in the
    // row's `title` — but only where a hover exists, i.e. NOT on the phone,
    // which is where the cockpit is mostly read. State the limit rather than
    // let the `title` read as if nothing were lost.
    expect(dep.textContent).toContain("T-dead");
    expect(dep.textContent).toContain("查無此任務");
  });

  it("is NOT a button — a click-through would land on an empty filter", async () => {
    __injectMockTask(mkTask({ title: "被鬼擋的2", deps: ["t-nope"] }));

    const [dep] = await depsOf("被鬼擋的2");
    expect(dep.tagName).not.toBe("BUTTON");
    expect(dep.classList.contains("task-card__waiting--dep-missing")).toBe(
      true
    );
    fireEvent.click(dep);
    // Nothing happened: no navigation was wired in the first place.
    expect(window.location.hash).toBe("");
  });

  it("does NOT claim 查無此任務 before the full population is loaded", async () => {
    // The honesty case (review finding): under the open-only fast path an
    // unresolved id may be a perfectly healthy task that merely CLOSED. Saying
    // 查無此任務 then is a confident lie the owner would act on — worse than
    // the raw id it replaced. T-a3e4 kept that split but moved it to where the
    // fact lives: a task with NO dep_tasks at all (a pre-join server, or any
    // caller that could not resolve deps) is rendered directly here.
    const { TaskCard } = await import("./TaskCard");
    const noop = async () => {};
    const blocked = mkTask({ title: "還沒載到", deps: ["t-not-loaded-yet"] });
    const { container } = render(
      <I18nProvider>
        <TaskCard
          // No depTasks: the server did not resolve deps. NOT the same as an
          // empty-status entry, which WOULD mean 查無此任務.
          task={{ ...blocked, depTasks: undefined }}
          allTasks={[blocked]}
          members={[]}
          workers={[]}
          nowTs={Date.now() / 1000}
          onTerminate={noop as never}
          onMarkDuplicate={noop as never}
          onSetPriority={noop as never}
          onReassign={noop as never}
          onSendMessage={noop as never}
          onHydrate={noop as never}
          onRemoveArtifact={noop as never}
        />
      </I18nProvider>
    );
    const dep = container.querySelector('[data-testid="task-dep"]')!;
    expect(dep.getAttribute("data-dep-state")).toBe("unresolved");
    // It says only what it knows: waiting on this dep, cannot name it yet.
    // T-c21e: the number shown is the short form (owner: match the card), and
    // the exact id moved to `title` so an unresolvable dep stays debuggable.
    expect(dep.textContent).toContain("T-not-");
    expect(dep.textContent).not.toContain("t-not-loaded-yet");
    expect(dep.getAttribute("title")).toBe("t-not-loaded-yet");
    expect(dep.textContent).not.toContain("查無此任務");
    // And it does not wear the 壞 id styling either — that class IS the claim.
    expect(dep.classList.contains("task-card__waiting--dep-missing")).toBe(
      false
    );
  });

  it("does not take the rest of the card down with it (不炸版)", async () => {
    const good = mkTask({ taskNo: "T-cccc", title: "好的前置" });
    __injectMockTask(good);
    __injectMockTask(
      mkTask({
        title: "一好一壞",
        status: "in_progress",
        deps: ["t-missing-1", good.id],
      })
    );

    const deps = await depsOf("一好一壞");
    const card = deps[0].closest(".task-card")!;
    // The bad dep degrades ALONE: its neighbour still resolves…
    expect(deps.map((d) => d.getAttribute("data-dep-state"))).toEqual([
      "missing",
      "open",
    ]);
    expect(deps[1].querySelector(".task-card__dep-title")?.textContent).toBe(
      "好的前置"
    );
    // …and the card around it is intact (status badge + progress still render).
    expect(card.querySelector('[data-testid="task-status"]')?.textContent).toBe(
      "進行中"
    );
    expect(card.querySelector('[data-testid="task-progress"]')).toBeTruthy();
  });
});
