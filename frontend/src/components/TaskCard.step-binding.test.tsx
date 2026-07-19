// TaskCard — 卡綁 step 顯示面 (owner design 2026-07-14). Locked here:
//   1. A card-bearing step embeds its TaskReplyCard INSIDE the step row —
//      never floating above the workflow (Seth 07-13: 等我回覆卡要看得出屬於
//      哪個 step). Legacy discovery is unchanged (the step's replyCardId
//      pointer), so pre-binding data renders the same way — never a crash.
//   2. 審批持久標記: a step that is a declared gate OR ever carried a card
//      keeps a permanent 審批 marker after it is done.
//   3. A DONE announced gate drops the dashed 等我回覆 preview (nothing waits)
//      — the permanent marker carries the history instead.
//   4. 已回覆卡收合: an ANSWERED card renders as a one-line summary (已回覆
//      tag + summary + the standing answer), expandable to the full shared
//      interior and collapsible again.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import { api } from "../api";
import {
  __resetMock,
  __injectMockTask,
  __injectMockReplyCard,
} from "../api/mock";
import type { TaskView, TaskStepView, ReplyCard } from "../api/adapter";

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

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要現在同步到 Jira 嗎？",
    body: "",
    options: ["核可，直接同步上去", "先不要"],
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
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

  it("keeps the permanent 審批 marker on steps that ever carried an approval", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-done", status: "answered" }));
    __injectMockTask(
      mkTask({
        title: "審批留痕",
        steps: [
          // A finished declared gate: no dashed preview any more, marker stays.
          mkStep({
            name: "gate-done",
            status: "done",
            isGate: true,
            replyCardId: "",
            orderIdx: 0,
            startedTs: 100,
            finishedTs: 200,
          }),
          // A finished plain step an ask was auto-bound to: marker stays too.
          mkStep({
            name: "asked-done",
            status: "done",
            replyCardId: "rc-done",
            orderIdx: 1,
            startedTs: 100,
            finishedTs: 200,
          }),
          // An ordinary finished step: no marker.
          mkStep({ name: "plain-done", status: "done", orderIdx: 2 }),
        ],
      })
    );

    const { findByTestId, findAllByTestId, queryByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));
    const steps = await findAllByTestId("task-step");
    expect(
      steps[0].querySelector('[data-testid="gate-mark"]')?.textContent
    ).toBe("審批");
    expect(steps[1].querySelector('[data-testid="gate-mark"]')).not.toBeNull();
    expect(steps[2].querySelector('[data-testid="gate-mark"]')).toBeNull();
    // The DONE announced gate shows the done badge, not the dashed preview.
    expect(queryByTestId("gate-announced")).toBeNull();
    expect(
      steps[0].querySelector(".task-step-badge")?.textContent
    ).toBe("完成");
  });

  it("collapses an ANSWERED card to one line; the chevron expands and re-collapses it", async () => {
    __injectMockReplyCard(
      mkCard({
        id: "rc-ans",
        status: "answered",
        answeredTs: Date.now() / 1000 - 120,
        answer: { optionIdx: 1, text: "", attachments: [] },
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

  it("shows the waiting badge only while the bound card is WAITING; answered/expired render their own badges", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-b-wait" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-b-ans",
        status: "answered",
        answeredTs: Date.now() / 1000 - 120,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-b-exp",
        status: "expired",
        expiredTs: Date.now() / 1000 - 60,
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
        ],
      })
    );

    const { findByTestId, findAllByTestId } = renderPage();
    fireEvent.click(await findByTestId("task-card"));
    const steps = await findAllByTestId("task-step");

    // WAITING card → the solid 等我回覆 badge (truly waiting for the owner).
    expect(
      steps[0].querySelector(".task-step-badge--waiting_owner")?.textContent
    ).toBe("等我回覆");
    // ANSWERED card → never 等我回覆 any more; the answered badge instead
    // (no pickup promise in the copy — T-2b14 semantic-death call).
    expect(steps[1].querySelector(".task-step-badge--waiting_owner")).toBeNull();
    expect(
      steps[1].querySelector('[data-testid="step-card-answered"]')?.textContent
    ).toBe("已回覆");
    // EXPIRED card (terminal, T-1aa4) → never 等我回覆; the 已過期 badge.
    expect(steps[2].querySelector(".task-step-badge--waiting_owner")).toBeNull();
    expect(
      steps[2].querySelector('[data-testid="step-card-expired"]')?.textContent
    ).toBe("已過期");
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
