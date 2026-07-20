// TaskCard — spec ② 狀態 badge 下拉 + spec ④ 右上三角指示器 (owner 2026-07-17).
// Locked here:
//   ② Clicking the 狀態 badge ALWAYS opens a dropdown — whatever the status is,
//     LIVE or CLOSED (owner's informed ruling, accepting that the old one-click
//     等我回覆 jump becomes two steps). It carries 標記重複 + 終止, plus the
//     「查看等我回覆卡」 jump as an EXTRA item only while status = 等我回覆.
//   ② On a CLOSED card (已完成 / 已終止 / 已標為重複) the menu still opens and
//     those two items render GREYED + disabled — owner ruling 2026-07-17
//     (rc-12d552eed7ce), taken knowing the server 409s both on a closed task.
//   ② 標記重複 / 終止 MOVED onto that dropdown — and the ⋮ owner menu that used
//     to hold them is DELETED outright (owner ruling 2026-07-17, after this
//     move left it empty). A test below pins its absence so nobody re-adds it.
//   ④ The card's top-right carries a chevron pointing RIGHT (collapsed) / DOWN
//     (expanded). v6/T-17be swapped the ▸/▾ TEXT GLYPHS for the ChevronRight/
//     ChevronDown ICONS at size 18 (the settings page's drill-in size) — so
//     these assertions now read the rendered svg, not textContent. It
//     is a pure STATE INDICATOR, not a control: it must not veto the whole-card
//     toggle (T-70fb behaviour 3), so a click on it still expands the card.
//   ④ The triangle now OWNS the top-right corner the ⋮ used to hold. T-70fb
//     behaviour 1 asked for a STABLE top-right anchor, not for that particular
//     button — so the anchor test below moved onto the triangle rather than
//     being dropped: it must sit last in the head row, in every state.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { __resetMock, __injectMockTask, mockApi } from "../api/mock";
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

/** Terminals are hidden by default — tick one in the 狀態 filter to reveal the
 * 已結束 section (same helper shape as TasksPage.test.tsx). */
function toggleFilter(testId: string, value: string) {
  const trigger = document.querySelector(`[data-testid="${testId}"]`)!;
  if (trigger.getAttribute("aria-expanded") !== "true") {
    fireEvent.click(trigger);
  }
  fireEvent.click(
    document.querySelector(`[data-testid="${testId}-opt-${value}"] input`)!
  );
}

beforeEach(() => {
  __resetMock();
  vi.restoreAllMocks();
  window.location.hash = "";
});

describe("spec ② 狀態 badge → 下拉選單", () => {
  it("opens the dropdown for EVERY live status, not just 等我回覆", async () => {
    // One card per live status — each badge must drop the menu.
    const statuses = ["in_progress", "waiting_owner", "waiting_external"];
    for (const status of statuses) {
      __injectMockTask(mkTask({ title: `狀態-${status}`, status }));
    }
    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    expect(cards).toHaveLength(statuses.length);

    for (const card of cards) {
      // Closed before the click…
      expect(
        card.querySelector('[data-testid="task-status-options"]')
      ).toBeNull();
      const badge = within(card).getByTestId("task-status");
      expect(badge.getAttribute("aria-haspopup")).toBe("menu");
      expect(badge.getAttribute("aria-expanded")).toBe("false");
      fireEvent.click(badge);
      // …open after it, whatever this card's status is.
      const menu = card.querySelector('[data-testid="task-status-options"]')!;
      expect(menu).toBeTruthy();
      expect(menu.getAttribute("role")).toBe("menu");
      expect(badge.getAttribute("aria-expanded")).toBe("true");
      // A second click closes it again.
      fireEvent.click(badge);
      expect(
        card.querySelector('[data-testid="task-status-options"]')
      ).toBeNull();
    }
  });

  it("carries 標記重複 + 終止, and 標記重複 opens the dup picker", async () => {
    __injectMockTask(mkTask({ title: "下拉有兩項" }));
    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-status"));
    const menu = await findByTestId("task-status-options");
    const dup = within(menu).getByTestId("task-mark-duplicate");
    const term = within(menu).getByTestId("task-terminate");
    expect(dup.textContent).toContain("標記重複");
    expect(term.textContent).toContain("終止");
    // The item really wires the action through (not a dead label).
    fireEvent.click(dup);
    expect(await findByTestId("mark-duplicate")).toBeTruthy();
  });

  it("終止 in the dropdown opens the double-confirm modal", async () => {
    __injectMockTask(mkTask({ title: "下拉終止" }));
    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-terminate"));
    const modal = await findByTestId("terminate-confirm");
    expect(modal.textContent).toContain("下拉終止");
  });

  it("the 等我回覆 jump item appears ONLY while status = 等我回覆", async () => {
    __injectMockTask(mkTask({ title: "等我回覆的", status: "waiting_owner" }));
    __injectMockTask(mkTask({ title: "進行中的", status: "in_progress" }));
    __injectMockTask(
      mkTask({ title: "等外部的", status: "waiting_external" })
    );
    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const byTitle = (s: string) =>
      cards.find((c) =>
        c.querySelector(".task-card__title")?.textContent?.includes(s)
      )!;

    // 等我回覆 → the extra item is there.
    const waiting = byTitle("等我回覆的");
    fireEvent.click(within(waiting).getByTestId("task-status"));
    expect(
      waiting.querySelector('[data-testid="task-status-jump"]')
    ).toBeTruthy();

    // Every other live status → the menu opens but carries NO jump item.
    for (const title of ["進行中的", "等外部的"]) {
      const card = byTitle(title);
      fireEvent.click(within(card).getByTestId("task-status"));
      expect(
        card.querySelector('[data-testid="task-status-options"]')
      ).toBeTruthy();
      expect(
        card.querySelector('[data-testid="task-status-jump"]')
      ).toBeNull();
    }
  });

  // ── T-c514 (owner 2026-07-20): jumps go FIRST ──────────────────────────
  // 「跳到等我回覆卡或是跳到這個等待外部的地方,都應該在第一個選項」.
  // These assert POSITION, not presence — the item already existed, it was just
  // last. A presence-only assertion would have passed before this change and
  // after it, i.e. it would pin nothing. So every assertion below reads the
  // menu's actual child order via [role="menuitem"] and indexes into it.
  function menuItemIds(card: Element): (string | null)[] {
    return Array.from(
      card.querySelectorAll('[data-testid="task-status-options"] [role="menuitem"]')
    ).map((el) => el.getAttribute("data-testid"));
  }

  it("等我回覆 jump is the FIRST menu item, ahead of 標記重複/終止", async () => {
    __injectMockTask(mkTask({ title: "等我回覆的", status: "waiting_owner" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(within(card).getByTestId("task-status"));

    const ids = menuItemIds(card);
    // positive control: the menu really rendered its full set, so the index
    // assertion below cannot pass by measuring an empty/partial menu.
    expect(ids).toContain("task-mark-duplicate");
    expect(ids).toContain("task-terminate");
    // the assertion that matters — FIRST, not merely present.
    expect(ids[0]).toBe("task-status-jump");
    // and strictly ahead of both destructive items (survives future inserts
    // between them that would leave index 0 intact but reorder the rest).
    expect(ids.indexOf("task-status-jump")).toBeLessThan(
      ids.indexOf("task-mark-duplicate")
    );
    expect(ids.indexOf("task-status-jump")).toBeLessThan(
      ids.indexOf("task-terminate")
    );
  });

  // The two halves of the union gate get a test each, because they cover
  // genuinely different moments and the first one is the one that broke:
  // a COLLAPSED card has no steps yet (they arrive with the detail hydrate), so
  // a steps-only gate rendered nothing exactly where owner wants the item.
  it("等待外部 jump: on a COLLAPSED waiting_external card, first in the menu, and expands the card", async () => {
    __injectMockTask(
      mkTask({
        title: "等外部的",
        status: "waiting_external",
        waitingReason: "等第三方開通",
        progressDone: 0,
        progressTotal: 2,
        steps: [
          {
            id: "s-1", name: "節點一", dod: "d", status: "waiting_external",
            isGate: false, replyCardId: "", parallelGroup: "", orderIdx: 0,
            startedTs: 1, finishedTs: 0, waitingReason: "等第三方開通",
          },
        ] as never,
      })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    // NOT expanded — this is the state the owner actually clicks from.
    fireEvent.click(within(card).getByTestId("task-status"));

    const ids = menuItemIds(card);
    expect(ids).toContain("task-mark-duplicate"); // positive control
    expect(ids[0]).toBe("task-status-jump-external");
    expect(ids.indexOf("task-status-jump-external")).toBeLessThan(
      ids.indexOf("task-mark-duplicate")
    );

    // Clicking it must do BOTH halves: expand, and actually scroll the step
    // into view. Asserting only the expand half is what let the first version
    // of this ship broken — the flag was cleared before the hydrate delivered
    // the steps, so the card opened and nothing ever scrolled. jsdom computes
    // no layout, but scrollIntoView is still a callable it records, so the
    // "did we scroll, and to WHICH element" question is answerable here.
    // jsdom implements no layout and therefore does NOT define
    // Element.prototype.scrollIntoView — spyOn would throw "does not exist".
    // Install it, then spy. (Also why this assertion can only ever check THAT
    // we scrolled and to which node, never where the pixels ended up.)
    const scrolled: Element[] = [];
    const proto = Element.prototype as unknown as Record<string, unknown>;
    const had = "scrollIntoView" in proto;
    if (!had) proto.scrollIntoView = function () {};
    const spy = vi
      .spyOn(Element.prototype, "scrollIntoView")
      .mockImplementation(function (this: Element) {
        scrolled.push(this);
      });
    try {
      fireEvent.click(within(card).getByTestId("task-status-jump-external"));
      await waitFor(() => {
        expect(card.querySelector('[data-testid="task-step"]')).toBeTruthy();
      });
      // the scroll lands AFTER the hydrate — wait for it rather than sampling
      // once, otherwise this races the very thing it is meant to pin.
      await waitFor(() => {
        expect(scrolled.length).toBeGreaterThan(0);
      });
      // …and on the RIGHT step: the one that is waiting_external (s-1), not
      // merely the first step in the timeline.
      expect(
        scrolled.some((el) => el.getAttribute("data-step-id") === "s-1")
      ).toBe(true);
    } finally {
      spy.mockRestore();
      if (!had) delete proto.scrollIntoView;
    }
  });

  it("等待外部 jump: shown when 等我回覆 wins the status precedence but a step still waits on a vendor", async () => {
    // The real reachable case for the step half of the OR gate. The server's
    // RecomputeTaskStatus ranks waiting_owner ABOVE waiting_external, so this
    // task reads 等我回覆 at the task level while a step is genuinely blocked
    // on a third party — task.status alone would never reveal it.
    __injectMockTask(
      mkTask({
        title: "等我回覆但也有等外部節點",
        status: "waiting_owner",
        progressDone: 0,
        progressTotal: 2,
        steps: [
          {
            id: "s-x", name: "節點甲", dod: "d", status: "waiting_external",
            isGate: false, replyCardId: "", parallelGroup: "", orderIdx: 0,
            startedTs: 1, finishedTs: 0, waitingReason: "等海關放行",
          },
          {
            id: "s-y", name: "節點乙", dod: "d", status: "waiting_owner",
            isGate: true, replyCardId: "", parallelGroup: "", orderIdx: 1,
            startedTs: 1, finishedTs: 0,
          },
        ] as never,
      })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(card.querySelector(".task-card__head")!);
    await waitFor(() => {
      expect(card.querySelector('[data-testid="task-step"]')).toBeTruthy();
    });

    fireEvent.click(within(card).getByTestId("task-status"));
    const ids = menuItemIds(card);
    // BOTH jumps are live here, and both sit ahead of the destructive items.
    expect(ids).toContain("task-mark-duplicate"); // positive control
    expect(ids.indexOf("task-status-jump")).toBeLessThan(
      ids.indexOf("task-mark-duplicate")
    );
    expect(ids.indexOf("task-status-jump-external")).toBeLessThan(
      ids.indexOf("task-mark-duplicate")
    );
  });

  it("等待外部 jump is ABSENT on a CLOSED task, even though its frozen step still says waiting_external", async () => {
    // closeTask does not roll steps back and RecomputeTaskStatus returns early
    // on a terminal task, so a task terminated mid-wait keeps a waiting_external
    // step forever. Nobody is waiting on it any more — offering the jump would
    // be a lie, and it used to appear only after expanding (absent collapsed),
    // which read as a glitch.
    __injectMockTask(
      mkTask({
        title: "終止但殘留等外部節點",
        status: "terminated",
        closedTs: Date.now() / 1000 - 10,
        progressDone: 0,
        progressTotal: 1,
        steps: [
          {
            id: "s-z", name: "節點", dod: "d", status: "waiting_external",
            isGate: false, replyCardId: "", parallelGroup: "", orderIdx: 0,
            startedTs: 1, finishedTs: 0, waitingReason: "等第三方",
          },
        ] as never,
      })
    );
    const { findByTestId } = renderPage();
    // Terminals are hidden by default: tick the 狀態 filter AND open the
    // 已結束 section (same two-step the existing closed-card test uses).
    toggleFilter("filter-status", "terminated");
    fireEvent.click(await findByTestId("closed-toggle"));
    const card = await findByTestId("task-card");
    fireEvent.click(card.querySelector(".task-card__head")!);
    await waitFor(() => {
      expect(card.querySelector('[data-testid="task-step"]')).toBeTruthy();
    });

    fireEvent.click(within(card).getByTestId("task-status"));
    const ids = menuItemIds(card);
    // positive control: the menu really opened and carries the greyed pair, so
    // the absence below is real and not an unopened menu.
    expect(ids).toContain("task-mark-duplicate");
    expect(ids).toContain("task-terminate");
    expect(ids).not.toContain("task-status-jump-external");
  });

  it("等待外部 jump: also on an in_progress task whose STEP waits — the derived status hides it, the step does not", async () => {
    __injectMockTask(
      mkTask({
        title: "進行中但有等外部節點",
        // The derived task status reads in_progress (other steps are running),
        // yet one step really is blocked on a third party. Gating on
        // task.status alone would hide the jump here.
        status: "in_progress",
        progressDone: 0,
        progressTotal: 2,
        steps: [
          {
            id: "s-a", name: "節點甲", dod: "d", status: "waiting_external",
            isGate: false, replyCardId: "", parallelGroup: "", orderIdx: 0,
            startedTs: 1, finishedTs: 0, waitingReason: "等海關放行",
          },
          {
            id: "s-b", name: "節點乙", dod: "d", status: "in_progress",
            isGate: false, replyCardId: "", parallelGroup: "", orderIdx: 1,
            startedTs: 1, finishedTs: 0,
          },
        ] as never,
      })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    // Expand first: this half of the gate reads view.steps, which only exist
    // after the detail hydrate. Asserting it on a collapsed card would be
    // asserting something the client cannot know yet.
    fireEvent.click(card.querySelector(".task-card__head")!);
    await waitFor(() => {
      expect(card.querySelector('[data-testid="task-step"]')).toBeTruthy();
    });

    fireEvent.click(within(card).getByTestId("task-status"));
    const ids = menuItemIds(card);
    expect(ids).toContain("task-mark-duplicate"); // positive control
    expect(ids[0]).toBe("task-status-jump-external");
  });

  it("等待外部 jump is ABSENT when no step waits on a third party", async () => {
    __injectMockTask(
      mkTask({
        title: "沒有等外部節點的",
        status: "in_progress",
        progressDone: 0,
        progressTotal: 1,
        steps: [
          {
            id: "s-9", name: "節點", dod: "d", status: "in_progress",
            isGate: false, replyCardId: "", parallelGroup: "", orderIdx: 0,
            startedTs: 1, finishedTs: 0,
          },
        ] as never,
      })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(within(card).getByTestId("task-status"));

    const ids = menuItemIds(card);
    // positive control FIRST — the menu is genuinely open and populated, so the
    // absence below is a real absence and not an unopened menu (the classic way
    // a negative assertion goes vacuous).
    expect(ids.length).toBeGreaterThan(0);
    expect(ids).toContain("task-mark-duplicate");
    expect(ids).not.toContain("task-status-jump-external");
  });

  it("MOVED, not copied: the card carries exactly ONE 標記重複 / 終止, on the status menu", async () => {
    __injectMockTask(mkTask({ title: "只有一份" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(within(card).getByTestId("task-status"));
    // Exactly one of each on the whole card — a copy left behind anywhere else
    // (e.g. a re-added ⋮ menu) makes these counts 2.
    expect(
      card.querySelectorAll('[data-testid="task-mark-duplicate"]')
    ).toHaveLength(1);
    expect(card.querySelectorAll('[data-testid="task-terminate"]')).toHaveLength(
      1
    );
    // …and the one that exists hangs off the status dropdown, nowhere else.
    for (const id of ["task-mark-duplicate", "task-terminate"]) {
      expect(
        card
          .querySelector(`[data-testid="${id}"]`)!
          .closest('[data-testid="task-status-options"]')
      ).toBeTruthy();
    }
  });

  it("the ⋮ owner menu is GONE — no button, no popover, no chrome, on live or closed cards", async () => {
    __injectMockTask(mkTask({ title: "活卡無選單" }));
    __injectMockTask(
      mkTask({
        title: "結束卡無選單",
        status: "terminated",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const { findAllByTestId, findByTestId } = renderPage();
    toggleFilter("filter-status", "terminated");
    fireEvent.click(await findByTestId("closed-toggle"));
    const cards = await findAllByTestId("task-card");
    expect(cards).toHaveLength(2);

    for (const card of cards) {
      // The button + its popover, by testid and by class chrome.
      expect(card.querySelector('[data-testid="task-menu-btn"]')).toBeNull();
      expect(
        card.querySelector('[data-testid="task-menu-options"]')
      ).toBeNull();
      expect(card.querySelector(".task-card__menu")).toBeNull();
      expect(card.querySelector(".task-card__menu-btn")).toBeNull();
      // And by the glyph itself — a re-add under a different testid/class still
      // has to render a ⋮ somewhere to be a ⋮ menu. Scoped to the head row, not
      // the whole card: a task TITLE is owner-authored free text and may
      // legitimately contain a ⋮ (this assertion caught exactly that on a
      // fixture titled 「沒有⋮的活卡」).
      const headTop = card.querySelector(".task-card__head-top")!;
      expect(headTop.textContent).not.toContain("⋮");
      // The head row's right corner holds the triangle and nothing else: its
      // only element children are the badge row and the indicator.
      const kids = [...headTop.children];
      expect(kids).toHaveLength(2);
      expect(kids[0].classList.contains("task-card__badge-row")).toBe(true);
      expect(kids[1].getAttribute("data-testid")).toBe("task-expand-mark");
    }
  });

  // ── owner ruling 2026-07-17 (rc-12d552eed7ce), spec ② follow-up ──────────
  // "照字面永遠出，已結束時兩項變灰不可點". The badge of a CLOSED card
  // (已完成 / 已終止 / 已標為重複 — all three shapes, not just 已終止) still
  // drops the menu; 標記重複 + 終止 render greyed and MUST NOT be able to fire.
  // The two assertions that actually bite are the disabled-attr check and the
  // modal-negative ("the click opens NO modal"); they are independent — either
  // one alone kills a mutant that greys the items with `aria-disabled` but
  // leaves the real gate off. The seam-level spy negatives further down are
  // NOT the guard: they are structurally vacuous on a closed card, because
  // clicking 終止/標記重複 only ever OPENS a modal/picker — `terminateTask` /
  // `markTaskDuplicate` are reached only by a SECOND click on the confirm
  // button. So an unlocked item would still leave both spies uncalled. They
  // are kept as a cheap backstop; do NOT read them as coverage, and do NOT
  // drop the disabled-attr or modal-negative assertions on their strength.
  // The spies do earn their keep in the LIVE positive control at the end,
  // which proves the click path can reach the seam at all.
  it("a CLOSED card: the dropdown still opens, 標記重複/終止 greyed and firing nothing", async () => {
    const termSpy = vi.spyOn(mockApi, "terminateTask");
    const dupSpy = vi.spyOn(mockApi, "markTaskDuplicate");

    // All THREE closed shapes — done / terminated / duplicated.
    const closedStatuses = ["done", "terminated", "duplicated"];
    for (const status of closedStatuses) {
      __injectMockTask(
        mkTask({
          title: `已結束-${status}`,
          status,
          closedTs: Date.now() / 1000 - 60,
        })
      );
    }
    // …plus one LIVE card, the positive control for the whole click path.
    __injectMockTask(mkTask({ title: "活的對照組", status: "in_progress" }));

    const { findAllByTestId, findByTestId } = renderPage();
    for (const s of closedStatuses) toggleFilter("filter-status", s);
    fireEvent.click(await findByTestId("closed-toggle"));
    const cards = await findAllByTestId("task-card");
    expect(cards).toHaveLength(closedStatuses.length + 1);
    const byTitle = (s: string) =>
      cards.find((c) =>
        c.querySelector(".task-card__title")?.textContent?.includes(s)
      )!;

    for (const status of closedStatuses) {
      const card = byTitle(`已結束-${status}`);
      const badge = within(card).getByTestId("task-status");
      // It IS a menu trigger now (it used to be a plain span).
      expect(badge.getAttribute("aria-haspopup")).toBe("menu");
      fireEvent.click(badge);

      const menu = card.querySelector('[data-testid="task-status-options"]')!;
      // Positive control INSIDE this scope: the menu really rendered here, and
      // really carries both items — so the negatives below are about their
      // disabled-ness, not about a scope that is silently empty.
      expect(menu).toBeTruthy();
      expect(badge.getAttribute("aria-expanded")).toBe("true");
      const dup = within(menu as HTMLElement).getByTestId("task-mark-duplicate");
      const term = within(menu as HTMLElement).getByTestId("task-terminate");
      expect(dup.textContent).toContain("標記重複");
      expect(term.textContent).toContain("終止");
      // A closed card is never 等我回覆 → the menu is these two and no more.
      expect(card.querySelector('[data-testid="task-status-jump"]')).toBeNull();

      // Greyed + genuinely disabled.
      for (const el of [dup, term]) {
        expect((el as HTMLButtonElement).disabled).toBe(true);
        expect(el.getAttribute("aria-disabled")).toBe("true");
        expect(
          el.classList.contains("task-card__menu-item--disabled")
        ).toBe(true);
      }

      // …and the click is DEAD: no modal opens, so nothing can reach the seam.
      fireEvent.click(dup);
      fireEvent.click(term);
      expect(document.querySelector('[data-testid="mark-duplicate"]')).toBeNull();
      expect(
        document.querySelector('[data-testid="terminate-confirm"]')
      ).toBeNull();
      // The menu did not even close on the dead clicks.
      expect(
        card.querySelector('[data-testid="task-status-options"]')
      ).toBeTruthy();
    }

    // No 409-bound request left the UI for any of the three. NOTE: vacuous on
    // its own — see the header comment. Nothing above can reach these seams
    // even with the gate removed, since that needs a second confirm click.
    // The real guards are the disabled-attr and modal-negative checks above.
    expect(termSpy).not.toHaveBeenCalled();
    expect(dupSpy).not.toHaveBeenCalled();

    // POSITIVE CONTROL for the spy + the path: the same two clicks on the LIVE
    // card DO open the modals, and confirming DOES reach the seam. If this
    // half fails, the negatives above prove nothing.
    const live = byTitle("活的對照組");
    fireEvent.click(within(live).getByTestId("task-status"));
    const liveTerm = within(live).getByTestId("task-terminate");
    expect((liveTerm as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(liveTerm);
    expect(await findByTestId("terminate-confirm")).toBeTruthy();
    fireEvent.click(await findByTestId("terminate-confirm-btn"));
    await waitFor(() => expect(termSpy).toHaveBeenCalledTimes(1));
  });
});

/** Which way the expand indicator's chevron ICON points, read off the svg path
 * the icon component renders ("down" = ChevronDownIcon, "right" =
 * ChevronRightIcon). Returns the raw `d` on an unrecognised path and null when
 * there is no icon at all, so a failure names what it actually found instead of
 * collapsing every wrong state into one anonymous falsy. */
function chevronDir(mark: Element): string | null {
  const d = mark.querySelector("svg path")?.getAttribute("d");
  if (!d) return null;
  if (d === "m6 9 6 6 6-6") return "down";
  if (d === "m9 18 6-6-6-6") return "right";
  return d;
}

describe("spec ④ 右上三角展開指示器", () => {
  it("points right while collapsed and down while expanded, tracking the card", async () => {
    __injectMockTask(mkTask({ title: "三角跟著展開" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    const mark = await findByTestId("task-expand-mark");

    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(chevronDir(mark)).toBe("right");

    // Expand via the card body (the toggle surface) — the chevron follows.
    fireEvent.click(card.querySelector(".task-card__title")!);
    expect(card.getAttribute("aria-expanded")).toBe("true");
    expect(chevronDir(mark)).toBe("down");

    // …and back.
    fireEvent.click(card.querySelector(".task-card__title")!);
    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(chevronDir(mark)).toBe("right");
  });

  it("is an indicator, not a button: clicking it still expands through the whole-card toggle", async () => {
    __injectMockTask(mkTask({ title: "三角不是按鈕" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    const mark = await findByTestId("task-expand-mark");
    // No control semantics — nothing for the closest() interaction filter to
    // catch, so the click falls through to the card toggle (T-70fb behaviour
    // 3: the whole card expands, and the expand BUTTON stays gone).
    expect(mark.closest("button, [role='button']")).toBe(card);
    expect(card.querySelector('[data-testid="task-expand"]')).toBeNull();
    fireEvent.click(mark);
    expect(card.getAttribute("aria-expanded")).toBe("true");
    expect(chevronDir(mark)).toBe("down");
    fireEvent.click(mark);
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  // T-70fb behaviour 1 rewritten (owner ruling): the locked property was a
  // STABLE top-right anchor, not the ⋮ button that used to serve it. The
  // triangle inherited the slot, so the protection moves here instead of being
  // dropped — the corner must never go empty or wander.
  it("owns the card's top-right anchor: last in the head row, after the badge row", async () => {
    __injectMockTask(mkTask({ title: "三角釘右上" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    const mark = await findByTestId("task-expand-mark");
    const headTop = card.querySelector(".task-card__head-top")!;
    const badgeRow = card.querySelector(".task-card__badge-row")!;

    // Anchored in the head-top row (the card's top-right corner)…
    expect(headTop.contains(mark)).toBe(true);
    // …as its LAST element child — in a flex row that IS the right edge.
    expect(headTop.lastElementChild).toBe(mark);
    // …with the badge row leading and taking the slack (flex:1) that pushes the
    // indicator to the edge — the v4 swap survives.
    expect(headTop.firstElementChild).toBe(badgeRow);
    expect(
      badgeRow.compareDocumentPosition(mark) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    // It is not buried inside the badge row / any dropdown.
    expect(mark.parentElement).toBe(headTop);
  });

  it("the anchor is STABLE: the indicator stays put across expand, collapse, and closed cards", async () => {
    __injectMockTask(mkTask({ title: "活的卡" }));
    __injectMockTask(
      mkTask({
        title: "結束的卡",
        status: "terminated",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const { findAllByTestId, findByTestId } = renderPage();
    toggleFilter("filter-status", "terminated");
    fireEvent.click(await findByTestId("closed-toggle"));
    const cards = await findAllByTestId("task-card");
    expect(cards).toHaveLength(2);

    // Live AND closed, collapsed AND expanded: the corner always holds the
    // indicator as the head row's last child. (The ⋮ it replaced vanished on
    // closed cards — the corner must not do that.)
    for (const card of cards) {
      const headTop = card.querySelector(".task-card__head-top")!;
      for (const _pass of [0, 1]) {
        const mark = card.querySelector('[data-testid="task-expand-mark"]')!;
        expect(mark).toBeTruthy();
        expect(headTop.lastElementChild).toBe(mark);
        expect([...headTop.children]).toHaveLength(2);
        fireEvent.click(card.querySelector(".task-card__title")!);
      }
    }
  });

  it("a closed task still shows the indicator and still expands", async () => {
    __injectMockTask(
      mkTask({
        title: "已結束仍有三角",
        status: "terminated",
        closedTs: Date.now() / 1000 - 60,
      })
    );
    const { findByTestId } = renderPage();
    toggleFilter("filter-status", "terminated");
    fireEvent.click(await findByTestId("closed-toggle"));
    const card = (await findByTestId("closed-list")).querySelector(
      '[data-testid="task-card"]'
    )!;
    const mark = card.querySelector('[data-testid="task-expand-mark"]')!;
    expect(chevronDir(mark)).toBe("right");
    fireEvent.click(card.querySelector(".task-card__title")!);
    expect(card.getAttribute("aria-expanded")).toBe("true");
    expect(chevronDir(mark)).toBe("down");
  });
});
