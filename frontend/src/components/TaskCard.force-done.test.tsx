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
import { en } from "../i18n/locales/en";
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
  // The en-locale arms below park `oc.language` in localStorage, which the
  // I18nProvider reads at mount. Clearing here keeps every zh test independent
  // of run order (and of an en test that failed before its own cleanup).
  localStorage.clear();
});

// ── polarity helpers (see ①b / ③c) ──────────────────────────────────────────
// 🔴 WHY A CLAUSE, NOT THE WHOLE STRING. Every literal assertion in this file
// before ①b/③c was of the form `toContain("按鈕")` / `toContain("費用")` — the
// WORD is present. A word-presence assertion cannot tell 「沒有那顆按鈕」 from
// 「就有那顆按鈕」, nor 「會產生費用」 from 「不會產生費用」: both sides of the
// flip contain the word. Two independently-run mutants exploited exactly that
// and the whole suite stayed green.
//
// So these cut the sentence into clauses, pick the ONE clause that talks about
// the thing, and assert its POLARITY. A flip moves the negation into (or out
// of) that clause and reddens. `clauseCount` is the positive control: if the
// wording is rewritten so the subject appears in zero or several clauses, the
// test fails LOUDLY instead of silently asserting about the wrong clause.
const ZH_BREAK = /[,;、。?!，；：]|——/;
/** English clauses: sentence/segment punctuation, plus a conjoined ", and …". */
const EN_BREAK = /[.;:—]|,\s+and\s+/;

function clausesWith(text: string, sep: RegExp, needle: RegExp): string[] {
  return text
    .split(sep)
    .map((s) => s.trim())
    .filter((s) => needle.test(s));
}

/** The single clause of `text` that talks about `needle` — fails if there is
 * not exactly one, so a rewording can never make this guard vacuous. */
function theClauseAbout(text: string, sep: RegExp, needle: RegExp): string {
  const hits = clausesWith(text, sep, needle);
  expect(
    hits,
    `expected exactly ONE clause about ${needle} in: ${text}`
  ).toHaveLength(1);
  return hits[0];
}

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

    // 🔴 THE SENTENCE HAS TO BE TRUE IN THE CASE THIS TICKET EXISTS FOR, and
    // this literal is the anchor that says so. Asserting only
    // `toContain(zh.tasks.readyForDoneHint)` above reads the SAME constant the
    // component reads, so it would pass just as happily on a sentence that said
    // the opposite — which is how two false drafts in a row survived.
    //
    // The line now states the STATE only (owner ruling 2026-09-14): every step
    // is done, and the assignee is who it is waiting on. The clauses that used
    // to say who may call the ordinary close and that no button for it is on
    // this screen were removed as developer-facing justification, so the
    // anchors that read them (「你」/「強制結案」, and the polarity arms in ①b)
    // went with them — see ①b for what took over.
    //
    // What survives here: the waited-for party is named with the card's OWN
    // noun.
    expect(banner.textContent).toContain("負責人");
    // 🔴 AND NOT with a second noun for the same human. 「執行者」 appeared in
    // exactly one user-visible zh string — this one — while every other surface
    // called that person 負責人; a card that uses both words for one person
    // cannot be read at all. (The naming itself is out of this package's scope,
    // which is why this line conforms to it rather than changing it.)
    expect(banner.textContent).not.toContain("執行者");

    // …and there is no close button on the banner. The 結案 one is out of
    // scope AND could only ever 403 from this cockpit.
    expect(card.querySelector('[data-testid="task-mark-done"]')).toBeNull();
    expect(banner.querySelector("button")).toBeNull();
  });

  it("keeps the waiting line after the card is EXPANDED — it is not a collapsed-only affordance", async () => {
    // 🔴 THE REVIEWER'S SURVIVING MUTANT M4. Narrowing the banner's condition
    // to `!expanded` made the line vanish the moment anyone opened the card,
    // and the whole suite stayed green: every assertion on it was made on a
    // COLLAPSED card. Opening a ticket to look at its steps is the most likely
    // way to arrive at the decision to force it closed, so that is precisely
    // when the sentence has to still be on screen.
    __injectMockTask(mkTask({ title: "展開也要看得到" }));
    const { findByTestId } = renderPage();

    const card = await findByTestId("task-card");
    expect(within(card).getByTestId("task-ready-done")).toBeTruthy();

    expandCard(card);
    await waitFor(() =>
      expect(card.getAttribute("aria-expanded")).toBe("true")
    );
    const banner = within(card).getByTestId("task-ready-done");
    expect(banner.textContent).toContain(zh.tasks.readyForDoneHint);
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

describe("③b the confirm discloses what the press actually destroys", () => {
  it("names the outsource dismissal AND the record freeze, not just 'cannot be resumed'", async () => {
    // 🔴 WHY THIS IS ASSERTED IN LITERALS. Both facts are verified server-side
    // and neither was on the dialog before:
    //   * closeTask() -> dismissOutsourceWorkersForTask() fires
    //     ReleaseWorkersForTask (the roster row) AND reclaimWorkerSession (the
    //     live session), on this door with no opt-out;
    //   * `done` satisfies TaskRecordFrozen(), so the artifact verbs and the
    //     step-note write answer 409 from then on.
    // The moment a person reaches for 強制結案 is "this ticket looks stuck",
    // and a worker that is mid-run but has not reported looks IDENTICAL to a
    // stuck one on this screen — so the cost has to be on the dialog, not in
    // the route description nobody opens.
    //
    // Literals rather than `toContain(zh.tasks.forceDoneConfirmBody)`: that
    // form reads the same constant the component reads and would survive the
    // whole sentence being deleted from the locale.
    __injectMockTask(mkTask({ title: "要講清楚後果", status: "in_progress" }));
    const { findByTestId } = renderPage();

    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    const body = (await findByTestId("force-done-confirm")).textContent ?? "";

    expect(body).toContain("外包");
    expect(body).toContain("遣散");
    expect(body).toContain("工作階段");
    expect(body).toContain("凍結");
    // The consequence that WAS already disclosed stays disclosed.
    expect(body).toContain("無法恢復");
  });

  it("names the reply cards this close retires — the owner's own questions die here", async () => {
    // 🔴 VERIFIED SERVER-SIDE, AND IT WAS NOT DECLARED. closeTask ->
    // expireWaitingCardsForTask (api_tasks.go:684 -> api_replycards.go:931)
    // retires EVERY card this task still has waiting. Those cards are the
    // owner's OWN 等我回覆 pane: a question this ticket asked disappears from
    // it at that moment and can never be answered again. The person pressing
    // 強制結案 is very often not the person who knows what the ticket asked,
    // which is exactly why the dialog has to say it rather than the route doc.
    __injectMockTask(mkTask({ title: "請示卡會過期", status: "in_progress" }));
    const { findByTestId } = renderPage();

    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    const body = (await findByTestId("force-done-confirm")).textContent ?? "";

    expect(body).toContain("請示卡");
    expect(body).toContain("過期");
    // The pane it names is the one the owner actually looks at — the same
    // literal the status dictionary uses for waiting_owner.
    expect(body).toContain(zh.tasks.status.waiting_owner);
  });

  it("🔴 names the worker this close can MINT — the one consequence that bills", async () => {
    // The dialog already declared that the press DISMISSES an outsource worker.
    // closeTask -> releaseDependentsOnClose (api_tasks.go:718 ->
    // api_tasks_handoff.go:363) releases the tasks this one was blocking and,
    // when a dependent is outsource-with-no-executor, calls tickOutsource —
    // whose own comment says that tick is "what actually turns \"design done\"
    // into \"dev worker spawned\"".
    //
    // ⇒ Same button, opposite direction, and THIS half spends money. A dialog
    // that discloses the free consequence and hides the billed one is worse
    // than one that discloses neither: it reads as complete.
    __injectMockTask(mkTask({ title: "可能會再生一個", status: "in_progress" }));
    const { findByTestId } = renderPage();

    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    const body = (await findByTestId("force-done-confirm")).textContent ?? "";

    expect(body).toContain("下游");
    expect(body).toContain("解除阻擋");
    // The spawn and its price, both stated. "會產生費用" is the half a reader
    // cannot infer from "起一位新的 worker" if they do not know how billing
    // works here.
    expect(body).toContain("新的 worker");
    expect(body).toContain("費用");
  });
});

describe("④ what the server recorded comes back onto the card", () => {
  it("🔴 RE-READS the one task after a successful force close — the list row cannot carry the stamp", async () => {
    // THE REVIEWER'S SURVIVING MUTANT M1: deleting the whole read-back block in
    // `doForceDone()` left all 3331 tests green. Every other test in ④ expands
    // the card to read the 強制結案 row, and EXPANDING HYDRATES — so the
    // read-back was covered by a gesture that would have fetched the data
    // anyway. Nothing pinned the case the block exists for: the owner forces a
    // close and looks at the card WITHOUT touching it again.
    //
    // The block is not a nicety. `forced_done_by` / `forced_done_reason` are
    // declared on `TaskDTO` and NOT on `TaskListItemDTO` (spec/openapi.json),
    // so the refetch `useTasks` does after the write brings back a row that
    // carries NEITHER. Without the hydrate the dialog closes and the card says
    // nothing about who forced it — which is precisely what the owner asked to
    // see.
    //
    // Driven on a bare TaskCard so the hydrate seam is the assertion's subject
    // rather than a side effect of the page: the fake answers with the fields
    // only the full projection has, and NOTHING in this test expands the card.
    const hydrate = vi.fn(async (id: string) =>
      mkTask({
        id,
        status: "done",
        closedTs: 1,
        forcedDoneBy: "owner",
        forcedDoneReason: "讀回來的理由",
      })
    );
    const forceDone = vi.fn(async () => {});
    const { findByTestId, getByTestId } = render(
      <I18nProvider>
        <TaskCard
          task={mkTask({ id: "task-readback", title: "要讀回來" })}
          allTasks={[]}
          members={[]}
          workers={[]}
          nowTs={Date.now() / 1000}
          onTerminate={async () => {}}
          onMarkDuplicate={async () => {}}
          onSetPriority={async () => {}}
          onReassign={async () => {}}
          onSendMessage={async () => {}}
          onHydrate={hydrate}
          onForceDone={forceDone}
          canForceDone
        />
      </I18nProvider>
    );

    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    fireEvent.click(await findByTestId("force-done-confirm-btn"));

    await waitFor(() => expect(forceDone).toHaveBeenCalledTimes(1));
    // The read-back happened, and it read THE TASK THAT WAS JUST CLOSED.
    await waitFor(() => expect(hydrate).toHaveBeenCalledWith("task-readback"));
    // …and what it answered is on screen, with the card still COLLAPSED — no
    // expand gesture anywhere in this test to do the fetching for it.
    const row = await findByTestId("task-forced-done");
    expect(row.textContent).toContain("讀回來的理由");
    expect(getByTestId("task-card").getAttribute("aria-expanded")).toBe("false");
  });

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
    // 🔴 AND THE SAME FACT AGAIN AS A LITERAL — the line above is NOT enough on
    // its own, because it reads the SAME constant the component renders. Invert
    // that constant's meaning (「未填理由」→「已填理由」) and the assertion
    // follows it happily; the reviewer's mutant did exactly that and this whole
    // file stayed green. A literal is the one anchor that does not move when the
    // constant does. The constant assertion is KEPT (it still pins that the
    // component reads the locale rather than hardcoding a string) — this is an
    // extra anchor, not a replacement.
    expect(reason.textContent).toBe("未填理由");
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

describe("⑤ a refused close ANSWERS THE REFUSAL", () => {
  // 🔴 WHY THIS BLOCK WAS REWRITTEN. Its previous single test was called "a
  // refused close says WHERE the task actually is" and seeded
  // `status: "waiting_owner"` as the state the post-refusal read came back
  // with. force_task_done CANNOT ANSWER 409 FOR THAT STATE — the handler's
  // only 409 is `if TaskIsTerminal(t.Status)`, and `waiting_owner` is not
  // terminal; that call would have SUCCEEDED. So the test pinned a sentence
  // against a situation nobody can construct, while the sentence the owner
  // would really see on the only reachable 409 read:
  //   「這張票沒有被結案。它現在的狀態是:已完成」
  // — which says it was not closed and then says it is Done.
  //
  // The three refusals are enumerable (422 decode / 404 / 409 terminal), so
  // each gets its own arm here, each seeded with a state that ACTUALLY
  // produces it.
  /** Open the 強制結案 dialog on the one card on screen, and answer the close
   * with `err`. Returns the error line the card ends up rendering. */
  async function refuseWith(
    err: unknown,
    fresh?: Partial<TaskView>
  ): Promise<HTMLElement> {
    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    const id = (await findByTestId("task-card")).getAttribute("data-task-id");
    vi.spyOn(mockApi, "forceTaskDone").mockRejectedValue(err);
    if (fresh) {
      vi.spyOn(mockApi, "getTask").mockResolvedValue(
        mkTask({ id: id ?? "task-1", ...fresh })
      );
    }
    fireEvent.click(await findByTestId("force-done-confirm-btn"));
    return await waitFor(() => {
      const el = document.querySelector(".task-card__error");
      expect(el).toBeTruthy();
      return el as HTMLElement;
    });
  }

  it("409 — says the task has ALREADY ENDED, and does not also claim it was not closed", async () => {
    // THE ONLY 409 THAT EXISTS HERE, and the only refusal this screen can
    // realistically produce: the menu was opened on an open card and by the
    // time the confirm landed somebody else had closed the task. The freshly
    // read status is therefore TERMINAL — that is what makes it a 409 at all.
    __injectMockTask(mkTask({ title: "已經被別人結掉了" }));
    const err = await refuseWith(
      Object.assign(new Error("http 409"), { status: 409 }),
      { status: "done", closedTs: 1 }
    );

    expect(err.textContent).toContain(zh.tasks.closeAlreadyClosedLead);
    expect(err.textContent).toContain(zh.tasks.closeAlreadyClosedTail);
    // 🔴 THE STATUS IT NAMES IS THE FRESHLY READ ONE, not the one the card was
    // holding when the click happened — the card's copy is exactly what the
    // refusal proves wrong. And WHICH terminal state matters: done /
    // terminated / duplicated imply different next moves.
    expect(err.textContent).toContain(zh.tasks.status.done);
    expect(err.textContent).not.toContain(zh.tasks.status.ready_for_done);
    // 🔴 THE SELF-CONTRADICTION IS GONE, asserted as a LITERAL rather than
    // through the constant the component reads. This is the defect: the old
    // line opened with 「這張票沒有被結案」 and closed with 「狀態是:已完成」
    // on the same breath, on the one path that can actually be reached.
    expect(err.textContent).not.toContain("沒有被結案");
    expect(err.textContent).not.toContain(zh.tasks.closeStateError);
    expect(err.textContent).toContain("已經結束了");
    expect(err.textContent).not.toBe(zh.tasks.actionError);
  });

  it("409 on a TERMINATED task names that state, not a generic 'closed'", async () => {
    // The second reachable shape of the same 409. Lumping the three terminal
    // states into one word would put the reader back where 「操作失敗」 left
    // them: something ended, and no way to tell what.
    __injectMockTask(mkTask({ title: "其實是被終止的" }));
    const err = await refuseWith(
      Object.assign(new Error("http 409"), { status: 409 }),
      { status: "terminated", closedTs: 1 }
    );
    expect(err.textContent).toContain(zh.tasks.status.terminated);
    expect(err.textContent).not.toContain(zh.tasks.status.done);
  });

  it("404 — says the task is gone, and names NO status", async () => {
    // resolveTask's refusal. There is no status to report, and the old shared
    // sentence would have ended 「它現在的狀態是:」 with whatever stale value
    // the card was holding — asserting a fact about a task the server just
    // said does not exist.
    __injectMockTask(mkTask({ title: "票不見了" }));
    const err = await refuseWith(
      Object.assign(new Error("http 404"), { status: 404 })
    );
    expect(err.textContent).toBe(zh.tasks.closeGoneError);
    expect(err.textContent).not.toContain(zh.tasks.closeStateError);
    expect(err.textContent).not.toContain(zh.tasks.status.ready_for_done);
  });

  it("422 — says the request could not be understood, and does not invite a retry", async () => {
    // decodeJSONBody's refusal: malformed JSON or an unknown key. The task was
    // never even looked at, so naming its status answers a question nobody
    // asked; and because the same payload will be refused again, the line must
    // not tell the reader to try again (which is exactly what the shared
    // 操作失敗 line does).
    __injectMockTask(mkTask({ title: "送出的內容伺服器看不懂" }));
    const err = await refuseWith(
      Object.assign(new Error("http 422"), { status: 422 })
    );
    expect(err.textContent).toBe(zh.tasks.closeBadRequestError);
    expect(err.textContent).not.toContain(zh.tasks.closeStateError);
    expect(err.textContent).not.toContain("重試");
  });

  it("an UNKNOWN failure keeps the status line — it is the honest answer when the reason is not known", async () => {
    // A 500 / offline / a throw from outside the adapters. Nothing can be said
    // about WHY, so the card says what did not happen and where the task
    // stands — which is the sentence that was wrong only for the three
    // enumerated codes, not wrong in general.
    __injectMockTask(mkTask({ title: "伺服器掛了" }));
    const err = await refuseWith(new Error("boom"), { status: "waiting_owner" });
    expect(err.textContent).toContain(zh.tasks.closeStateError);
    expect(err.textContent).toContain(zh.tasks.status.waiting_owner);
  });

  // ── the branch key: HTTP CODE, not the card's terminality ────────────────
  // 🔴 THE SURVIVING MUTANT M2. `if (isHttpStatus(e, 409))` was replaced with
  // `if (TERMINAL.has(status))` — the "equivalent" the surrounding comment
  // invites, since the handler's only 409 IS `TaskIsTerminal(t.Status)`. The
  // whole suite stayed green, because every arm above varies the two together:
  // 409 always arrived with a terminal re-read, and the non-409 arm always
  // arrived with a non-terminal one. Nothing separated the wire's answer from
  // the ticket's state, so both predicates gave the same answer everywhere.
  //
  // They are NOT the same predicate. The re-read is a SECOND, LATER request
  // with its own outcome — it can fail (the catch swallows it and keeps the
  // card's stale copy), it can race, and the error being reported may not have
  // come from this endpoint at all. The two arms below are the two ways that
  // costs the reader:
  //   * a 500 / dropped connection on a ticket that happens to read terminal
  //     would be reported as 「已經結束了,不需要再結一次」 — an unknown failure
  //     dressed up as a benign one, and the reader stops looking.
  //   * a real 409 whose re-read has not caught up would fall through to the
  //     generic line, which opens 「這張票沒有被結案」 about a task the server
  //     just refused BECAUSE it is closed — the exact self-contradiction ⑤
  //     exists to remove.
  it("🔴 an UNKNOWN failure on a task that reads TERMINAL is still reported as unknown", async () => {
    __injectMockTask(mkTask({ title: "500 但票剛好已經結束" }));
    const err = await refuseWith(new Error("boom"), {
      status: "done",
      closedTs: 1,
    });
    // Terminal re-read, NOT a 409 ⇒ the honest generic line, not the
    // already-closed reassurance.
    expect(err.textContent).toContain(zh.tasks.closeStateError);
    expect(err.textContent).toContain(zh.tasks.status.done);
    expect(err.textContent).not.toContain(zh.tasks.closeAlreadyClosedLead);
    expect(err.textContent).not.toContain("已經結束了");
  });

  it("🔴 a real 409 is answered as already-closed even when the re-read is NOT terminal", async () => {
    // The mirror arm. The re-read is a second request and can come back stale
    // or from a replica that has not seen the close; the REFUSAL is the fact
    // that is certain here, so it is what picks the sentence. (This is also
    // what the card does when the re-read throws outright — it falls back to
    // its own stale copy, which is non-terminal on an open card.)
    __injectMockTask(mkTask({ title: "409 但讀回來還沒追上" }));
    const err = await refuseWith(
      Object.assign(new Error("http 409"), { status: 409 }),
      { status: "waiting_owner" }
    );
    expect(err.textContent).toContain(zh.tasks.closeAlreadyClosedLead);
    expect(err.textContent).toContain(zh.tasks.closeAlreadyClosedTail);
    expect(err.textContent).not.toContain(zh.tasks.closeStateError);
    expect(err.textContent).not.toContain("沒有被結案");
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

// ═══════════════════════════════════════════════════════════════════════════
// The four cells below were each opened by an independently-run mutation round
// that killed nothing. Every one of them is the same shape of hole: the file
// asserted that a WORD was present, or that a DOM node was absent, and neither
// of those can see a sentence that has been turned around, or a value that was
// read from the wrong source, or a default that was flipped.
// ═══════════════════════════════════════════════════════════════════════════

/** Render the page with the UI in `locale`. The provider reads `oc.language`
 * from localStorage at mount; the file's beforeEach clears it again. */
function renderPageIn(locale: "zh" | "en") {
  localStorage.setItem("oc.language", locale);
  return renderPage();
}

describe("①b the banner's SENTENCE and the screen agree — in BOTH locales", () => {
  // 🔴 THE SURVIVING MUTANT M8 IS WHY THIS BLOCK EXISTS.
  // `readyForDoneHint` was flipped from 「一般結案只有負責人本人做得到,這個畫面
  // 上沒有那顆按鈕」 to 「一般結案你自己也做得到,這個畫面上就有那顆按鈕」 — a
  // sentence that sends the reader hunting for a control that is not there, and
  // tells them they hold a permission the route floor 403s them for. The whole
  // suite stayed green.
  //
  // It survived because ① asserted (a) the WORDS 負責人 / 你 / 強制結案 are
  // present — all three are still present after the flip — and (b) the DOM has
  // no <button> in the banner — which is about the SCREEN, not about what the
  // sentence CLAIMS about the screen.
  //
  // ⚠️ THE SENTENCE THOSE TWO ARMS GUARDED IS GONE (owner ruling 2026-09-14:
  // the permission and button clauses were developer-facing justification, not
  // status). The line now claims one thing only, so the arms that read the
  // 一般結案 clause and the 按鈕 clause lost their subject and were REMOVED
  // rather than loosened — a loosened polarity check on a clause that no longer
  // exists is vacuous, which is the exact failure mode this block was opened
  // for. What is asserted instead, in each locale:
  //   (i) THE SENTENCE: the clause naming the waited-for party names the
  //       ASSIGNEE, and does NOT hand the wrap-up to the reader. This still
  //       kills the M8 family: 「你自己就可以收尾」 either drops 負責人 out of
  //       the clause (theClauseAbout fails loudly at 0 hits) or puts 你 into it.
  //  (ii) THE SCREEN: there is still no ordinary-close control in the banner or
  //       on the card. The sentence no longer restates this, so the DOM
  //       assertion is now the ONLY thing holding it — which is why it stays.
  it("zh: the banner says it is waiting on the ASSIGNEE, not on the reader", async () => {
    __injectMockTask(mkTask({ title: "文案只講狀態" }));
    const { findByTestId } = renderPageIn("zh");
    const card = await findByTestId("task-card");
    const text = within(card).getByTestId("task-ready-done").textContent ?? "";

    // positive control: this IS the zh string, so a locale mix-up fails here
    // rather than vacuously passing the checks below.
    expect(text).toContain(zh.tasks.readyForDoneHint);

    // (i) WHO is being waited for. `callerMayMarkTaskDone` is
    //     `t.ExecutorID != "" && currentActor(r) == t.ExecutorID` — no admin
    //     exemption, the owner included — so the wrap-up is the assignee's and
    //     a line that offers it to 「你」 is false.
    const whoClause = theClauseAbout(text, ZH_BREAK, /負責人/);
    expect(whoClause).toMatch(/等.*負責人/);
    expect(whoClause).not.toMatch(/你/);

    // …and the screen still carries no ordinary-close control. The sentence
    // used to say so too; now this is the only assertion that does.
    expect(card.querySelector('[data-testid="task-mark-done"]')).toBeNull();
    expect(
      within(card).getByTestId("task-ready-done").querySelector("button")
    ).toBeNull();
  });

  it("en: same claim, same subject — the en string had no test anchor at all", async () => {
    // 🔴 THE EN PATH WAS NEVER MEASURED. Every anchor in this file was a zh
    // literal, so `en.tasks.readyForDoneHint` could have said anything — the
    // suite would not have noticed. An en reader is exactly as capable of
    // being told they can wrap up a task the server 403s them for.
    __injectMockTask(mkTask({ title: "en banner subject" }));
    const { findByTestId } = renderPageIn("en");
    const card = await findByTestId("task-card");
    const text = within(card).getByTestId("task-ready-done").textContent ?? "";

    expect(text).toContain(en.tasks.readyForDoneHint);
    expect(text).not.toContain(zh.tasks.readyForDoneHint);

    // (i) the wrap-up is the assignee's, not the reader's.
    const whoClause = theClauseAbout(text, EN_BREAK, /assignee/i);
    expect(whoClause).toMatch(/waiting for its assignee/i);
    expect(whoClause).not.toMatch(/\byou(r|rself)?\b/i);

    expect(card.querySelector('[data-testid="task-mark-done"]')).toBeNull();
    expect(
      within(card).getByTestId("task-ready-done").querySelector("button")
    ).toBeNull();
  });
});

describe("①c the banner reads the HYDRATED status, not the list row's", () => {
  // 🔴 THE SURVIVING MUTANT M4. The banner's condition source was changed from
  // `view.status` to `task.status` and the suite stayed green. The file comment
  // beside the banner CLAIMS the `view` reading is deliberate ("an expanded
  // card whose hydrate has moved the status shows the hydrated truth") — an
  // unpinned claim. ① already opens a card, but its hydrate answers with the
  // SAME status the list row carried, so both sources agreed and neither test
  // could tell them apart.
  //
  // It matters because the list row is the STALE one: the row was fetched with
  // the page, the hydrate happens on expand, and the most likely way to arrive
  // at 強制結案 is opening a ticket to look at its steps. A card whose steps
  // just came back finished must start saying it is waiting; a card whose work
  // restarted must stop.
  function renderWithHydrate(taskStatus: string, hydratedStatus: string) {
    const noop = async () => {};
    return render(
      <I18nProvider>
        <TaskCard
          task={mkTask({ id: "t-hydrate", title: "狀態來源", status: taskStatus })}
          allTasks={[]}
          members={[]}
          workers={[]}
          nowTs={Date.now() / 1000}
          onTerminate={noop}
          onMarkDuplicate={noop}
          onSetPriority={noop}
          onReassign={noop}
          onSendMessage={noop}
          onHydrate={async (id) => mkTask({ id, status: hydratedStatus })}
          onForceDone={noop}
          canForceDone
        />
      </I18nProvider>
    );
  }

  it("the banner APPEARS when the hydrate reports ready_for_done on a row that said in_progress", async () => {
    const { findByTestId } = renderWithHydrate("in_progress", "ready_for_done");
    const card = await findByTestId("task-card");
    // The list row's status ⇒ no banner yet. (Positive control for the arm: if
    // the banner were unconditional this would already fail.)
    expect(card.querySelector('[data-testid="task-ready-done"]')).toBeNull();

    expandCard(card);
    await waitFor(() =>
      expect(card.getAttribute("aria-expanded")).toBe("true")
    );
    // Reading `task.status` here leaves the banner absent forever.
    await waitFor(() =>
      expect(card.querySelector('[data-testid="task-ready-done"]')).toBeTruthy()
    );
  });

  it("the banner DISAPPEARS when the hydrate reports the task moved off ready_for_done", async () => {
    const { findByTestId } = renderWithHydrate("ready_for_done", "in_progress");
    const card = await findByTestId("task-card");
    expect(card.querySelector('[data-testid="task-ready-done"]')).toBeTruthy();

    expandCard(card);
    await waitFor(() =>
      expect(card.getAttribute("aria-expanded")).toBe("true")
    );
    // Reading `task.status` here keeps saying "waiting for a close" about a
    // ticket that is being worked on again.
    await waitFor(() =>
      expect(card.querySelector('[data-testid="task-ready-done"]')).toBeNull()
    );
  });
});

describe("②b canForceDone DEFAULTS to the safe shape — a caller that says nothing offers nothing", () => {
  // 🔴 THE SURVIVING MUTANT M5. `canForceDone = false` was changed to
  // `= true` and the suite stayed green: ② states BOTH arms, but always by
  // PASSING the prop, so the default was never the value under test. The prop's
  // own doc comment calls the false default "deliberate… a hand-built fixture
  // or a future caller that forgets to pass it gets the SAFE shape" — that
  // sentence was, until now, unguarded.
  //
  // The failure it prevents is silent in exactly the way that matters: a new
  // call site that omits the prop renders a destructive, irreversible,
  // money-spending action for a principal nobody decided to offer it to, and
  // the only thing that says no afterwards is a 403 from the server.
  it("a TaskCard rendered WITHOUT the prop shows no 強制結案 — absent, not greyed", async () => {
    const noop = async () => {};
    const { findByTestId } = render(
      <I18nProvider>
        <TaskCard
          task={mkTask({ title: "忘記傳 canForceDone", status: "in_progress" })}
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
          /* canForceDone deliberately NOT passed — the default is the subject */
        />
      </I18nProvider>
    );

    fireEvent.click(await findByTestId("task-status"));
    const menu = await findByTestId("task-status-options");
    expect(menu.querySelector('[data-testid="task-force-done"]')).toBeNull();
    expect(menu.textContent).not.toContain(zh.tasks.forceDone);
    // positive control: the menu really rendered, so the absence above is a
    // decision and not an empty DOM.
    expect(within(menu).getByTestId("task-terminate")).toBeTruthy();
  });
});

describe("③c the dialog's consequences are AFFIRMATIVE — polarity, in BOTH locales", () => {
  // 🔴 THE SURVIVING MUTANT M3. `forceDoneConfirmBody` was flipped from
  // 「…會在這一刻起一位新的 worker,那會產生費用」 to 「…不會…所以不會產生
  // 費用」 and the suite stayed green: ③b asserts that the WORDS 下游 /
  // 解除阻擋 / 新的 worker / 費用 are present, and every one of them is still
  // present after the flip. A guard built out of word-presence can stop the
  // disclosure being DELETED; it cannot stop it being REVERSED — and a dialog
  // that actively promises there is no charge is worse than one that says
  // nothing, because it answers the reader's question with the wrong answer.
  async function openConfirmBody(locale: "zh" | "en"): Promise<string> {
    const { findByTestId } = renderPageIn(locale);
    fireEvent.click(await findByTestId("task-status"));
    fireEvent.click(await findByTestId("task-force-done"));
    return (await findByTestId("force-done-confirm")).textContent ?? "";
  }

  it("zh: the spawn and its price are both stated as things that WILL happen", async () => {
    __injectMockTask(mkTask({ title: "費用要講成會發生", status: "in_progress" }));
    const body = await openConfirmBody("zh");
    expect(body).toContain(zh.tasks.forceDoneConfirmBody);

    // The clause that mints the worker: affirmative.
    const spawnClause = theClauseAbout(body, ZH_BREAK, /新的 worker/);
    // 「不會在…起」 still contains 「會在」 as a substring, so the positive
    // form needs the lookbehind too. (「還沒有負責人」 legitimately carries a
    // 沒有 in this clause — it is about the DEPENDENT having no assignee, not
    // about the spawn — so 沒有 is not in the negative set here.)
    expect(spawnClause).toMatch(/(?<![不未])會(在|起)/);
    expect(spawnClause).not.toMatch(/不會|不再|未必/);

    // The clause that bills: affirmative. `toContain("會產生費用")` would NOT
    // do — 「不會產生費用」 contains it as a substring, which is precisely how
    // the flip could have survived a naive tightening of ③b.
    const costClause = theClauseAbout(body, ZH_BREAK, /費用/);
    expect(costClause).toMatch(/會產生費用/);
    expect(costClause).not.toMatch(/不會|免費|不收/);
    expect(body).not.toContain("不會產生費用");
  });

  it("en: same two clauses, same polarity — the en dialog had no test anchor at all", async () => {
    __injectMockTask(mkTask({ title: "en dialog polarity", status: "in_progress" }));
    const body = await openConfirmBody("en");
    expect(body).toContain(en.tasks.forceDoneConfirmBody);
    expect(body).not.toContain(zh.tasks.forceDoneConfirmBody);

    // The spawn-and-bill clause, affirmative in both halves.
    const costClause = theClauseAbout(body, EN_BREAK, /costs?\b/i);
    expect(costClause).toMatch(/spawns a new worker/i);
    expect(costClause).toMatch(/costs money/i);
    expect(costClause).not.toMatch(
      /\bdoes not\b|\bwill not\b|\bno new worker\b|\bnever\b|\bfree\b|\bcosts nothing\b|\bno cost\b/i
    );

    // …and the consequences ③b pins in zh are disclosed in en too. These were
    // never asserted on the en string before, so it could have been missing any
    // of them.
    expect(body).toMatch(/outsource worker/i);
    expect(body).toMatch(/dismissed/i);
    expect(body).toMatch(/frozen/i);
    expect(body).toMatch(/reply card/i);
    expect(body).toMatch(/expired/i);
    expect(body).toMatch(/cannot be resumed/i);
    expect(body).toMatch(/downstream/i);
    expect(body).toMatch(/released/i);
  });
});
