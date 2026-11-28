// TaskCard — 卡綁 step 顯示面 (owner design 2026-07-14). Locked here:
//   1. A card-bearing step embeds its TaskReplyCard INSIDE the step row —
//      never floating above the workflow (Seth 07-13: 等我回覆卡要看得出屬於
//      哪個 step). Legacy discovery is unchanged (the step's replyCardId
//      pointer), so pre-binding data renders the same way — never a crash.
//   2. 需要你核可: only a gate step (is_gate — the server marks every
//      card-bound step as one) carries the shield badge, also after it is done.
//   3. The timeline icon names only the step's own stage: a gate with no card
//      is plain 待辦, and a waiting_owner step whose card is answered/expired
//      reads 進行中. The step row itself carries no status words, only the
//      title and its time pill.
//   4. 已回覆卡收合: an ANSWERED card renders as a one-line summary (已回覆
//      tag + summary + the standing answer), expandable to the full shared
//      interior and collapsible again.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { api } from "../api";
import {
  __resetMock,
  __injectMockTask,
  __injectMockReplyCard,
} from "../api/mock";
import type { TaskView, TaskStepView, ReplyCard } from "../api/adapter";
import { CircleDashedIcon, CircleProgressIcon, UserClockIcon } from "./icons";

let seq = 0;

function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`,
    name: `step-${seq}`,
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
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${1000 + seq}`,
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

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要現在同步到 Jira 嗎？",
    body: "",
    options: [{ text: "核可，直接同步上去", aiPick: true }, { text: "先不要", aiPick: false }],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

function iconMarkup(Icon: (p: { size: number }) => React.JSX.Element): string {
  return render(<Icon size={16} />).container.innerHTML;
}

/** The timeline icon drawn for a step. */
function markerOf(step: HTMLElement): HTMLElement {
  return step.closest(".task-parallel__item, .task-timeline__row")!.querySelector('[data-testid="step-marker"]')!;
}

function renderPage() {
  return render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("TaskCard 卡綁 step", () => {
  it("embeds the reply card INSIDE its step row, not above the workflow", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-in-step" }));
    __injectMockTask(
      mkTask({
        title: "卡在節點裡",
        status: "waiting_owner",
        steps: [
          mkStep({ name: "prep", status: "done", orderIdx: 0 }),
          mkStep({
            name: "ask-owner",
            status: "waiting_owner",
            replyCardId: "rc-in-step",
            orderIdx: 1,
            startedTs: Date.now() / 1000 - 60,
          }),
        ],
      })
    );

    const { findByTestId, findAllByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));
    const embedded = await findByTestId("task-reply-card");

    // The card sits inside the CARD-BEARING step element…
    const steps = await findAllByTestId("task-step");
    expect(steps[1].contains(embedded)).toBe(true);
    expect(steps[0].querySelector('[data-testid="task-reply-card"]')).toBeNull();
    // …and nowhere outside the workflow timeline.
    const card = await findByTestId("task-card");
    const workflow = card.querySelector(".task-card__workflow")!;
    expect(workflow.contains(embedded)).toBe(true);
  });

  it("shows the 需要你核可 shield on gate steps only; a card-less gate's icon names its plain status", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-done", status: "answered" }));
    __injectMockTask(
      mkTask({
        title: "審批留痕",
        steps: [
          mkStep({
            name: "gate-done",
            status: "done",
            isGate: true,
            replyCardId: "",
            orderIdx: 0,
            startedTs: 100,
            finishedTs: 100 + 4 * 3600 + 59 * 60,
          }),
          // isGate is the only source; a replyCardId alone never marks it.
          mkStep({
            name: "asked-done",
            status: "done",
            replyCardId: "rc-done",
            orderIdx: 1,
            startedTs: 100,
            finishedTs: 200,
          }),
          mkStep({ name: "plain-done", status: "done", orderIdx: 2 }),
          mkStep({
            name: "gate-pending",
            status: "pending",
            isGate: true,
            replyCardId: "",
            orderIdx: 3,
          }),
        ],
      })
    );

    const { findByTestId, findAllByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));
    const steps = await findAllByTestId("task-step");
    const shield = (step: HTMLElement) =>
      within(step).queryByRole("img", { name: "需要你核可" });
    expect(shield(steps[0])?.getAttribute("title")).toBe("需要你核可");
    expect(shield(steps[1])).toBeNull();
    expect(shield(steps[2])).toBeNull();
    expect(shield(steps[3])).not.toBeNull();
    expect(steps.map((s) => markerOf(s).getAttribute("aria-label"))).toEqual([
      "完成 · 需要你核可",
      "完成",
      "完成",
      "待辦 · 需要你核可",
    ]);
    expect(markerOf(steps[0]).getAttribute("title")).toBe("完成 · 需要你核可");

    const pill = within(steps[0]).getByTestId("step-duration");
    expect(pill.textContent).toBe("4h");
    expect(pill.getAttribute("title")).toBe("已歷時 4h 59m");
    expect(within(steps[0]).getByRole("img", { name: "已歷時 4h 59m" })).toBe(pill);
    expect(within(steps[2]).queryByTestId("step-duration")).toBeNull();
    expect(steps.map((s) => s.querySelector(".task-step__row")!.textContent)).toEqual([
      "gate-done4h",
      "asked-done1m",
      "plain-done",
      "gate-pending",
    ]);
  });

  it("collapses an ANSWERED card to one line; the chevron expands and re-collapses it", async () => {
    __injectMockReplyCard(
      mkCard({
        id: "rc-ans",
        status: "answered",
        answeredTs: Date.now() / 1000 - 120,
        answer: { optionIdxs: [1], text: "", attachments: [] },
      })
    );
    __injectMockTask(
      mkTask({
        title: "已答收合",
        steps: [
          mkStep({
            name: "approve",
            status: "done",
            isGate: true,
            replyCardId: "rc-ans",
            orderIdx: 0,
            startedTs: 100,
            finishedTs: 200,
          }),
        ],
      })
    );

    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));

    // Collapsed by default AND NOT fetched (owner 已回覆卡預設不載): the stub
    // shows the 已回覆 tag + the step-name fallback (no card fetched, so no
    // summary/answer yet), and getReplyCard has NOT been called for it.
    const row = await findByTestId("task-reply-card-expand");
    expect(row.textContent).toContain("已回覆");
    expect(row.textContent).toContain("approve");
    let embedded = await findByTestId("task-reply-card");
    expect(embedded.querySelector('[data-testid="final-answer"]')).toBeNull();
    expect(getSpy).not.toHaveBeenCalled();

    // Expand → NOW it fetches, and the full shared interior appears (final
    // answer + 重新決定 machinery), carrying the real card summary + answer.
    fireEvent.click(row);
    await waitFor(async () => {
      embedded = await findByTestId("task-reply-card");
      expect(
        embedded.querySelector('[data-testid="final-answer"]')?.textContent
      ).toContain("你選的");
    });
    expect(getSpy).toHaveBeenCalledTimes(1);
    embedded = await findByTestId("task-reply-card");
    expect(embedded.textContent).toContain("要現在同步到 Jira 嗎？");
    expect(embedded.textContent).toContain("先不要");

    // Collapse again from the header control.
    fireEvent.click(await findByTestId("task-reply-card-collapse"));
    await waitFor(async () => {
      expect(await findByTestId("task-reply-card-expand")).not.toBeNull();
    });
  });

  it("typed answer with mentions → a known id is its roster name opening the detail with 返回 to tasks, an unknown id plain text, and the folded row reads @name", async () => {
    __injectMockReplyCard(
      mkCard({
        id: "rc-mention",
        status: "answered",
        answeredTs: Date.now() / 1000 - 120,
        answer: {
          optionIdxs: [],
          text: "請 [@Mira 舊名](/#office/member/mira) 和 [@外包 · O-9](/#office/worker/ow-9) 處理",
          attachments: [],
        },
      })
    );
    __injectMockTask(
      mkTask({
        id: "task-mention",
        steps: [
          mkStep({ status: "done", replyCardId: "rc-mention", orderIdx: 0 }),
        ],
      })
    );
    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));
    fireEvent.click(await findByTestId("task-reply-card-expand"));
    const final = await findByTestId("final-answer");
    expect(final.textContent).toBe("你選的請 @Mira 和 @外包 · O-9 處理");
    const buttons = final.querySelectorAll("button.md-mention");
    expect(Array.from(buttons, (b) => b.textContent)).toEqual(["@Mira"]);

    fireEvent.click(await findByTestId("task-reply-card-collapse"));
    expect((await findByTestId("task-reply-card-expand")).textContent).toBe(
      "已回覆要現在同步到 Jira 嗎？請 @Mira 舊名 和 @外包 · O-9 處理"
    );

    fireEvent.click(await findByTestId("task-reply-card-expand"));
    fireEvent.click((await findByTestId("final-answer")).querySelector("button.md-mention")!);
    expect(window.location.hash).toBe(
      "#office/member/mira/from/tasks/task-mention"
    );
  });

  it("shows 等我回覆 only while the bound card is WAITING; an answered or expired card puts the step back to 進行中", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-b-wait" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-b-ans",
        status: "answered",
        answeredTs: Date.now() / 1000 - 120,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-b-exp",
        status: "expired",
        expiredTs: Date.now() / 1000 - 60,
      })
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-b-par",
        status: "answered",
        answeredTs: Date.now() / 1000 - 120,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    __injectMockTask(
      mkTask({
        title: "卡態決定徽章",
        status: "waiting_owner",
        steps: [
          mkStep({
            name: "ask-waiting",
            status: "waiting_owner",
            replyCardId: "rc-b-wait",
            orderIdx: 0,
            startedTs: Date.now() / 1000 - 60,
          }),
          mkStep({
            name: "ask-answered",
            status: "waiting_owner",
            replyCardId: "rc-b-ans",
            orderIdx: 1,
            startedTs: Date.now() / 1000 - 60,
          }),
          mkStep({
            name: "ask-expired",
            status: "waiting_owner",
            replyCardId: "rc-b-exp",
            orderIdx: 2,
            startedTs: Date.now() / 1000 - 60,
          }),
          mkStep({
            name: "par-answered",
            status: "waiting_owner",
            replyCardId: "rc-b-par",
            parallelGroup: "pg",
            orderIdx: 3,
            startedTs: Date.now() / 1000 - 60,
          }),
          mkStep({ name: "par-pending", parallelGroup: "pg", orderIdx: 4 }),
        ],
      })
    );

    const { findByTestId, findAllByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));
    const steps = await findAllByTestId("task-step");
    const progressIcon = iconMarkup(CircleProgressIcon);
    const waitingIcon = iconMarkup(UserClockIcon);

    expect(steps.map((s) => markerOf(s).getAttribute("aria-label"))).toEqual([
      "等我回覆",
      "進行中",
      "進行中",
      "進行中",
      "待辦",
    ]);
    expect(
      steps.map((s) =>
        Array.from(markerOf(s).classList).find((c) => c.startsWith("task-timeline__marker--"))
      )
    ).toEqual([
      "task-timeline__marker--waiting_owner",
      "task-timeline__marker--in_progress",
      "task-timeline__marker--in_progress",
      "task-timeline__marker--in_progress",
      "task-timeline__marker--pending",
    ]);
    expect(steps.map((s) => markerOf(s).innerHTML)).toEqual([
      waitingIcon,
      progressIcon,
      progressIcon,
      progressIcon,
      iconMarkup(CircleDashedIcon),
    ]);
    expect(steps.map((s) => s.classList.contains("task-step--in_progress"))).toEqual([
      false,
      true,
      true,
      true,
      false,
    ]);
  });

  it("keeps a WAITING card fully open (the collapse applies to answered cards only)", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-wait" }));
    __injectMockTask(
      mkTask({
        title: "等回覆全開",
        status: "waiting_owner",
        steps: [
          mkStep({
            name: "gate",
            status: "waiting_owner",
            isGate: true,
            replyCardId: "rc-wait",
            orderIdx: 0,
            startedTs: Date.now() / 1000 - 60,
          }),
        ],
      })
    );

    const { findByTestId, queryByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));
    const embedded = await findByTestId("task-reply-card");
    await waitFor(() => {
      expect(embedded.querySelectorAll(".reply-option")).toHaveLength(2);
    });
    expect(queryByTestId("task-reply-card-expand")).toBeNull();
  });
});
