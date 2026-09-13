// TaskCard — 可結案 / 結案 / 強制結案 (T-192).
//
// WHAT THIS TICKET IS ABOUT. A task whose every step is reported done does NOT
// close itself: it settles in `ready_for_done` and stays there until somebody
// acts. The server has no timer and chases nobody. Before this change the
// cockpit had ZERO product code for either close — `mark_task_done` and
// `force_task_done` existed on the wire and appeared in the frontend only as
// generated types — so a ticket parked waiting for a human read exactly like a
// ticket being worked on, and a ticket whose executor had already left could
// not be closed from this screen at all.
//
// 🔴 SCOPE, AND WHY THERE IS NO 結案 BUTTON IN THIS FILE. An earlier cut put
// one in the 可結案 banner, wired to `mark_task_done`. The AC that asked for it
// (「owner 可以直接按下結案」) was WITHDRAWN — the ticket now lists 強制結案 and
// nothing else — and the button was unpressable anyway: that route admits the
// task's OWN EXECUTOR and 403s everyone else, and this cockpit authenticates as
// exactly one principal, the owner, who is never the executor. The same "do not
// offer a button that could only ever fail" rule already governs 強制結案 via
// `canForceDone`. ① below now pins the banner WITHOUT a button, and pins that
// no close button is offered there — so re-adding one reddens this file.
//
// Locked here:
//   ① a ready_for_done card SAYS it is waiting, on the COLLAPSED card, and
//     offers NO close button. Any other status shows no line at all.
//   ② owner / admin assistant get 強制結案 in the 狀態 menu; a viewer the route
//     floor would refuse does not — the item is absent, not greyed.
//   ③ the 強制結案 confirm is a SECOND step (the menu item alone closes
//     nothing), and its reason is OPTIONAL: an empty textarea still submits.
//     That half is the owner's ruling rc-a92a6252c3bd, which this ticket also
//     relaxed server-side.
//   ④ what came back is READ BACK and shown: who forced it, and the reason —
//     or a visible "no reason given" when there was none.
//   ⑤ a refused close NAMES THE STATUS the task is actually in, rather than a
//     bare 操作失敗.
//   ⑥ 🔴 NO GENERIC SET-STATUS ENTRY (ticket DoD). Both closes go through their
//     own named action; a test below refuses any control that would let the
//     cockpit write an arbitrary status.
//
// ⚠️ THE GATE IN ② IS NOT A PERMISSION CHECK AND THIS FILE MUST NOT BE READ AS
// PROVING ONE. `Gated(principalAdminAgent, …)` in server/ocserverd/routes.go is
// what refuses a plain member, and it refuses them whatever the cockpit renders.
// What ② pins is only that the cockpit does not OFFER a button that could then
// only ever 403 — measured through the real prop the page threads down, with the
// other arm stated rather than assumed.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { TasksPage } from "./TasksPage";
import { TaskCard } from "./TaskCard";
import { __resetMock, __injectMockTask, mockApi } from "../api/mock";
import type { TaskView } from "../api/adapter";
import { toggleFilter } from "../test/tasksFilter";

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${2000 + seq}`,
    title: `任務 ${seq}`,
    typeKey: "",
    description: "",
    status: "ready_for_done",
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

function renderPage() {
  return render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>
  );
}

/** Reveal the 已完成 section so a card that just closed STAYS on screen. The
 * page hides terminals by default, so without this a forced close makes the
 * card vanish and there is nothing left to read the 強制結案 row off.
 *
 * ⚠️ TWO GESTURES, NOT ONE, and the second is easy to miss: ticking 已完成 in
 * the 狀態 filter only makes the page FETCH the closed rows — they land in a
 * 已結案 section that is itself COLLAPSED, so the card is in the DOM's mind but
 * not on screen. `openClosedSection` is the click that actually shows it, and it
 * can only be made once the section exists (i.e. after something closed). */
function showDone() {
  toggleFilter("filter-status", "done");
}

/** Expand the (single) card on screen.
 *
 * 🔴 REQUIRED BEFORE READING THE 強制結案 ROW, and the reason is a WIRE FACT
 * rather than a test convenience: `forced_done_by` / `forced_done_reason` are
 * declared on `TaskDTO` and NOT on `TaskListItemDTO` (spec/openapi.json), so the
 * light list row the closed section renders from cannot carry them. The card
 * learns them by hydrating the ONE task on expand — which is the same reason
 * the card must not print 「不是強制結案」 from a collapsed row either: it has
 * not been told. */
function expandCard(card: HTMLElement) {
  fireEvent.click(card.querySelector(".task-card__title")!);
}

async function openClosedSection() {
  const toggle = await waitFor(() => {
    const el = document.querySelector('[data-testid="closed-toggle"]');
    expect(el).toBeTruthy();
    return el as HTMLElement;
  });
  if (toggle.getAttribute("aria-expanded") !== "true") fireEvent.click(toggle);
}

beforeEach(() => {
  __resetMock();
  vi.restoreAllMocks();
  window.location.hash = "";
});

describe("① 可結案: the card says it is waiting — and offers no close button", () => {
  it("shows the waiting line on a COLLAPSED ready_for_done card — and offers NO close button there", async () => {
    __injectMockTask(mkTask({ title: "等人按結案" }));
    const { findByTestId } = renderPage();

    const card = await findByTestId("task-card");
    // Collapsed — nothing was expanded, and the line is still there. This is
    // the whole complaint: you must be able to see it in the LIST.
    expect(card.getAttribute("aria-expanded")).toBe("false");
    const banner = within(card).getByTestId("task-ready-done");
    expect(banner.textContent).toContain(zh.tasks.readyForDoneHint);

    // …and there is no close button on the banner. The 結案 one is out of
    // scope AND could only ever 403 from this cockpit.
    expect(card.querySelector('[data-testid="task-mark-done"]')).toBeNull();
    expect(banner.querySelector("button")).toBeNull();
  });

  it("shows no 可結案 line on a status that has not reached the precondition", async () => {
    for (const status of ["not_started", "in_progress", "waiting_owner", "waiting_external"]) {
      __injectMockTask(mkTask({ title: `不可結案-${status}`, status }));
    }
    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    expect(cards).toHaveLength(4);
    for (const card of cards) {
      expect(card.querySelector('[data-testid="task-ready-done"]')).toBeNull();
      expect(card.querySelector('[data-testid="task-mark-done"]')).toBeNull();
    }
  });

  it("the cockpit never calls mark_task_done — the banner has no door to it", async () => {
    // The negative form of the scope ruling, stated where it can actually
    // fail: re-adding the button (or any other caller) reddens this.
    __injectMockTask(mkTask({ title: "沒有一般結案" }));
    const spy = vi.spyOn(mockApi, "markTaskDone");
    const { findByTestId } = renderPage();

    const card = await findByTestId("task-card");
    const banner = within(card).getByTestId("task-ready-done");
    for (const b of banner.querySelectorAll("button")) fireEvent.click(b);
    fireEvent.click(banner);
    expect(spy).not.toHaveBeenCalled();
  });
});

describe("② 強制結案 is offered to the principals the route floor admits — and to nobody else", () => {
  it("owner/admin: the 狀態 menu carries 強制結案, alongside the items that were already there", async () => {
    __injectMockTask(mkTask({ title: "可以強制", status: "in_progress" }));
    const { findByTestId } = renderPage();

    fireEvent.click(await findByTestId("task-status"));
    const menu = await findByTestId("task-status-options");
    const force = within(menu).getByTestId("task-force-done");
    expect(force.textContent).toContain(zh.tasks.forceDone);
    expect(force.hasAttribute("disabled")).toBe(false);
    // The items this ticket did not touch are still there, in their ruled
    // order: adding a close must not quietly displace 標記重複/終止.
    expect(within(menu).getByTestId("task-mark-duplicate")).toBeTruthy();
    expect(within(menu).getByTestId("task-terminate")).toBeTruthy();
  });

  it("a viewer the server would refuse does not get the item AT ALL — absent, not greyed", async () => {
    // The negative arm, stated through the same prop TasksPage threads down.
    // Greying would be the wrong shape here and that is the assertion's point:
    // a disabled item says "yours, but not now", and for a principal that may
    // never force a close that sentence is false.
    const noop = async () => {};
    const { findByTestId } = render(
      <I18nProvider>
        <TaskCard
          task={mkTask({ title: "一般成員看不到", status: "in_progress" })}
          allTasks={[]}
          members={[]}
          workers={[]}
          nowTs={Date.now() / 1000}
          onTerminate={noop}
          onMarkDuplicate={noop}
          onSetPriority={noop}
          onReassign={noop}
          onSendMessage={noop}
          onHydrate={async (id) => mkTask({ id })}
          onForceDone={noop}
          canForceDone={false}
        />
      </I18nProvider>
    );

    fireEvent.click(await findByTestId("task-status"));
    const menu = await findByTestId("task-status-options");
    expect(menu.querySelector('[data-testid="task-force-done"]')).toBeNull();
    // …and not smuggled in under another name: the menu must not contain the
    // word at all.
    expect(menu.textContent).not.toContain(zh.tasks.forceDone);
    // The member's ORDINARY doors are untouched — this ticket narrows nobody.
    expect(within(menu).getByTestId("task-terminate")).toBeTruthy();
    expect(within(menu).getByTestId("task-mark-duplicate")).toBeTruthy();
  });

  it("a CLOSED card greys 強制結案 like its neighbours — the server 409s it, so it is shown-but-dead", async () => {
    __injectMockTask(mkTask({ title: "已經結案了", status: "done", closedTs: 1 }));
    const { findByTestId } = renderPage();
    showDone();
    await openClosedSection();

    fireEvent.click(await findByTestId("task-status"));
    const force = within(await findByTestId("task-status-options")).getByTestId(
      "task-force-done"
    );
    expect(force.hasAttribute("disabled")).toBe(true);
    expect(force.getAttribute("aria-disabled")).toBe("true");
  });
});

describe("③ the reason is asked for, not demanded (owner ruling rc-a92a6252c3bd)", () => {
  it("submits with the textarea left EMPTY, and sends no reason rather than a blank one", async () => {
    __injectMockTask(mkTask({ title: "不給理由", status: "in_progress" }));
    const spy = vi.spyOn(mockApi, "forceTaskDone");
    const { findByTestId } = renderPage();

    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    const dialog = await findByTestId("force-done-confirm");
    const reason = within(dialog).getByTestId("force-done-reason");
    expect((reason as HTMLTextAreaElement).value).toBe("");
    // The control is NOT required and the confirm is NOT disabled — a client
    // that re-imposed the requirement would put back exactly what the ruling
    // removed, one layer further from where anyone would look for it.
    expect(reason.hasAttribute("required")).toBe(false);
    const confirm = await findByTestId("force-done-confirm-btn");
    expect(confirm.hasAttribute("disabled")).toBe(false);

    fireEvent.click(confirm);
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    expect(spy.mock.calls[0][1]).toBe("");
  });

  it("the menu item alone closes nothing — the confirm is a real second step", async () => {
    __injectMockTask(mkTask({ title: "二次確認", status: "in_progress" }));
    const spy = vi.spyOn(mockApi, "forceTaskDone");
    const { findByTestId, queryByTestId } = renderPage();

    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    expect(await findByTestId("force-done-confirm")).toBeTruthy();
    expect(spy).not.toHaveBeenCalled();

    // And cancelling really cancels — the dialog goes, the task does not.
    fireEvent.click(within(await findByTestId("force-done-confirm")).getByText(
      zh.common.cancel
    ));
    await waitFor(() =>
      expect(queryByTestId("force-done-confirm")).toBeNull()
    );
    expect(spy).not.toHaveBeenCalled();
  });
});

describe("④ what the server recorded comes back onto the card", () => {
  it("a forced close with a reason shows WHO forced it and WHY", async () => {
    __injectMockTask(mkTask({ title: "有理由", status: "in_progress" }));
    const { findByTestId } = renderPage();
    showDone();

    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    fireEvent.change(await findByTestId("force-done-reason"), {
      target: { value: "負責人已退場,活早就交付了" },
    });
    fireEvent.click(await findByTestId("force-done-confirm-btn"));
    await openClosedSection();
    expandCard(await findByTestId("task-card"));

    const row = await findByTestId("task-forced-done");
    expect(row.textContent).toContain(zh.tasks.forcedDoneLabel);
    expect(row.textContent).toContain("負責人已退場,活早就交付了");
    // The card also actually moved — the row is not decoration on an open task.
    await waitFor(() =>
      expect(
        within(document.body).getByTestId("task-status").textContent?.trim()
      ).toBe(zh.tasks.status.done)
    );
  });

  it("a forced close with NO reason says so out loud, rather than showing an empty row", async () => {
    __injectMockTask(mkTask({ title: "沒有理由", status: "in_progress" }));
    const { findByTestId } = renderPage();
    showDone();

    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    fireEvent.click(await findByTestId("force-done-confirm-btn"));
    await openClosedSection();
    expandCard(await findByTestId("task-card"));

    const reason = await findByTestId("task-forced-done-reason");
    // Absent is rendered AS absent. A row that silently dropped the line would
    // read identically to a row that was never rendered, which is the one thing
    // an optional field must never be allowed to look like.
    expect(reason.textContent).toBe(zh.tasks.forcedDoneNoReason);
  });

  it("a task closed NORMALLY carries no 強制結案 row — the stamp is what tells the two closes apart", async () => {
    // Injected already-done rather than closed through the UI: the cockpit no
    // longer HAS an ordinary-close button (scope, see the header), and the
    // subject here was never the button — it is that `forcedDoneBy` is what
    // distinguishes the two closes on a card that has the full projection.
    __injectMockTask(
      mkTask({ title: "自己結案的", status: "done", closedTs: 1 })
    );
    const { findByTestId } = renderPage();
    showDone();
    await openClosedSection();
    // Expanded, so the card HAS the projection that could show a 強制結案 row —
    // this is the arm where the absence is evidence rather than ignorance.
    expandCard(await findByTestId("task-card"));
    await waitFor(() =>
      expect(document.querySelector(".task-card__workflow, .task-card__meta")).toBeTruthy()
    );

    await waitFor(() =>
      expect(
        within(document.body).getByTestId("task-status").textContent?.trim()
      ).toBe(zh.tasks.status.done)
    );
    expect(document.querySelector('[data-testid="task-forced-done"]')).toBeNull();
  });
});

describe("⑤ a refused close says WHERE the task actually is", () => {
  it("names the status instead of a bare 操作失敗", async () => {
    // The race this exists for: the menu was opened on an open card, and by
    // the time the confirm landed somebody else had already closed the task.
    // The mock answers the same 409 the server does. Driven through 強制結案
    // because that is the only close this surface still has — `reportCloseRefused`
    // is shared, so this is the same code path the removed button used.
    __injectMockTask(mkTask({ title: "被拒絕的結案" }));
    const { findByTestId } = renderPage();

    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    // Move it under the open dialog — now the close cannot succeed.
    const id = (await findByTestId("task-card")).getAttribute("data-task-id");
    vi.spyOn(mockApi, "forceTaskDone").mockRejectedValue(
      Object.assign(new Error("http 409"), { status: 409 })
    );
    vi.spyOn(mockApi, "getTask").mockResolvedValue(
      mkTask({ id: id ?? "task-1", status: "waiting_owner" })
    );
    fireEvent.click(await findByTestId("force-done-confirm-btn"));

    const err = await waitFor(() => {
      const el = document.querySelector(".task-card__error");
      expect(el).toBeTruthy();
      return el!;
    });
    expect(err.textContent).toContain(zh.tasks.closeStateError);
    // 🔴 THE STATUS IT NAMES IS THE FRESHLY READ ONE, not the one the card was
    // holding when the click happened — the card's copy is exactly what the
    // refusal proves wrong.
    expect(err.textContent).toContain(zh.tasks.status.waiting_owner);
    expect(err.textContent).not.toContain(zh.tasks.status.ready_for_done);
    // And the generic line is NOT what was rendered.
    expect(err.textContent).not.toBe(zh.tasks.actionError);
  });
});

describe("⑥ no generic set-status entry (ticket DoD)", () => {
  it("the 狀態 menu offers only NAMED actions — nothing that writes an arbitrary status", async () => {
    __injectMockTask(mkTask({ title: "只有具名動作", status: "in_progress" }));
    const { findByTestId } = renderPage();

    fireEvent.click(await findByTestId("task-status"));
    const menu = await findByTestId("task-status-options");
    // Every control in the menu is one of the known named actions. A future
    // 「設定狀態」 dropdown, radio group or free status picker would add a
    // <select> or an unlisted testid here and fail this.
    expect(menu.querySelector("select")).toBeNull();
    expect(menu.querySelectorAll("input")).toHaveLength(0);
    const known = new Set([
      "task-status-jump",
      "task-status-jump-external",
      "task-mark-duplicate",
      "task-terminate",
      "task-force-done",
    ]);
    for (const item of menu.querySelectorAll("[role='menuitem']")) {
      expect(known.has(item.getAttribute("data-testid") ?? "")).toBe(true);
    }
    // And the cockpit's api port carries no arbitrary-status writer to reach.
    expect(
      Object.keys(mockApi).filter((k) => /^setTaskStatus$|^updateTaskStatus$/.test(k))
    ).toEqual([]);
  });
});
