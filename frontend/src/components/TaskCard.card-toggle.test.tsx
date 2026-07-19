// TaskCard — whole-card expand/collapse (owner mobile refactor 2026-07-17).
// Locked here:
//   1. The chevron button is GONE (no task-expand testid); clicking anywhere
//      on the card body toggles the workflow, with aria-expanded + role=button
//      semantics on the card itself.
//   2. INTERACTIVE elements never flip the card: the 狀態 / 優先權 dropdowns,
//      the meta chips (assignee chat / type settings / 識別鍵 link), the
//      message composer (paperclip / textarea / 送出), and in-card links —
//      their clicks do their own job only (closest() filter, not
//      stopPropagation sprinkled per child). (The ⋮ owner menu was one of
//      these until v5 deleted it; see TaskCard.status-menu.test.tsx.)
//   3. An active text selection (drag ending on the card) is not a toggle.
//   4. The ☑ #T-xxxx id badge LEADS the fixed badge row, label-free
//      (編號 · 優先權 · 狀態, v3), no longer on the title line or in the
//      meta stack.
//   5. The 等我回覆 jump (v5): the status badge drops a menu whose 查看等我回覆卡
//      item expands + scrolls to the embedded reply card.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockTaskType,
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
    progressTotal: 1,
    steps: [mkStep({ name: "build", status: "in_progress", orderIdx: 0 })],
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

describe("TaskCard whole-card toggle (mobile refactor)", () => {
  it("has no chevron button; the card itself carries the toggle semantics", async () => {
    __injectMockTask(mkTask({ title: "無下三角" }));
    const { findByTestId, queryByTestId } = renderPage();
    const card = await findByTestId("task-card");
    expect(queryByTestId("task-expand")).toBeNull();
    expect(card.getAttribute("role")).toBe("button");
    expect(card.getAttribute("tabindex")).toBe("0");
    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(card.getAttribute("aria-label")).toBe("展開工作流程");
  });

  it("clicking the card body expands the workflow; clicking again collapses", async () => {
    __injectMockTask(mkTask({ title: "整卡切換" }));
    const { findByTestId, queryByTestId } = renderPage();
    const card = await findByTestId("task-card");
    expect(queryByTestId("task-step")).toBeNull();

    fireEvent.click(card);
    expect(card.getAttribute("aria-expanded")).toBe("true");
    expect(card.getAttribute("aria-label")).toBe("收合工作流程");
    expect(await findByTestId("task-step")).not.toBeNull();

    fireEvent.click(card);
    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(queryByTestId("task-step")).toBeNull();
  });

  it("clicking a NON-interactive inner region (the title) still toggles", async () => {
    __injectMockTask(mkTask({ title: "點標題也展開" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(card.querySelector(".task-card__title")!);
    expect(card.getAttribute("aria-expanded")).toBe("true");
  });

  it("keyboard: Enter / Space on the card itself toggles; keys inside the composer do not", async () => {
    __injectMockTask(mkTask({ title: "鍵盤切換" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.keyDown(card, { key: "Enter" });
    expect(card.getAttribute("aria-expanded")).toBe("true");
    fireEvent.keyDown(card, { key: " " });
    expect(card.getAttribute("aria-expanded")).toBe("false");
    // An Enter bubbling out of the message textarea (send) never flips.
    fireEvent.keyDown(await findByTestId("task-msg-input"), { key: "Enter" });
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  it("the 狀態 badge and its dropdown items never toggle the card", async () => {
    __injectMockTask(mkTask({ title: "狀態選單不切換" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(await findByTestId("task-status"));
    expect(card.getAttribute("aria-expanded")).toBe("false");
    // A click inside the open dropdown stays a menu action only (the dup-picker
    // modal opens; the card must not flip).
    fireEvent.click(await findByTestId("task-mark-duplicate"));
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  it("the priority chip and its in-place options never toggle the card", async () => {
    __injectMockTask(mkTask({ title: "優先權不切換" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(await findByTestId("task-priority"));
    expect(card.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(await findByTestId("priority-high"));
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  it("the message composer (paperclip / textarea / 送出) never toggles the card", async () => {
    __injectMockTask(mkTask({ title: "留言不切換" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(await findByTestId("task-msg-attach"));
    expect(card.getAttribute("aria-expanded")).toBe("false");
    const input = await findByTestId("task-msg-input");
    fireEvent.click(input);
    expect(card.getAttribute("aria-expanded")).toBe("false");
    fireEvent.change(input, { target: { value: "hi" } });
    fireEvent.click(await findByTestId("task-msg-send"));
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  it("meta chips (assignee chat / type settings) navigate without toggling", async () => {
    __injectMockTaskType({ typeKey: "review-pr", displayName: "", purpose: "" });
    __injectMockTask(
      mkTask({ title: "連結不切換", typeKey: "review-pr", executorId: "mira" })
    );
    const { findByTestId, findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const card = cards[0];

    fireEvent.click(await findByTestId("task-assignee-link"));
    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(window.location.hash).toContain("#office/chat/mira");

    window.location.hash = "";
    fireEvent.click(await findByTestId("task-type-link"));
    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(window.location.hash).toBe("#settings/manuals/review-pr");
  });

  it("clicking a question-attachment image thumbnail never collapses the card, and the Lightbox opens (review fix on c891881)", async () => {
    // The thumbnail is an <img role="button"> (AttachmentStrip) — no tag
    // selector matches it, so the closest() filter must carry [role='button']
    // (excluding the card itself, which also carries role=button). Regression:
    // the unfixed filter let the click bubble → the card collapsed → the
    // workflow (and the Lightbox inside TaskReplyCard) unmounted, so the
    // T-5e8a 點圖預覽 could never open on the task card.
    __injectMockReplyCard(
      mkCard({
        id: "rc-att",
        attachments: [
          {
            id: "att-1",
            // Mock cards carry data-URI urls — AttachmentStrip serves them
            // verbatim (authedAttachmentUrl only decorates server paths).
            url: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
            filename: "shot.png",
            mime: "image/png",
            isImage: true,
          },
        ],
      })
    );
    __injectMockTask(
      mkTask({
        title: "附件縮圖不收卡",
        steps: [
          mkStep({
            name: "approve",
            status: "waiting_owner",
            isGate: true,
            replyCardId: "rc-att",
            orderIdx: 0,
          }),
        ],
      })
    );
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(card); // expand
    expect(card.getAttribute("aria-expanded")).toBe("true");

    // The waiting card hydrates inside its step row; wait for the thumbnail.
    const embedded = await findByTestId("task-reply-card");
    const thumb = await waitFor(() => {
      const el = embedded.querySelector('img[role="button"]');
      expect(el).toBeTruthy();
      return el!;
    });

    fireEvent.click(thumb);
    // The card STAYS expanded, and the Lightbox overlay is up.
    expect(card.getAttribute("aria-expanded")).toBe("true");
    const lightbox = card.querySelector(".chat__lightbox");
    expect(lightbox).toBeTruthy();
    expect(lightbox!.getAttribute("role")).toBe("dialog");
  });

  it("an active text selection suppresses the toggle", async () => {
    __injectMockTask(mkTask({ title: "選字不切換" }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");

    // Simulate a real drag-selection over the card title (jsdom carries a
    // live Selection API): the click that ends the drag must not toggle.
    const title = card.querySelector(".task-card__title")!;
    const range = document.createRange();
    range.selectNodeContents(title);
    const sel = window.getSelection()!;
    sel.removeAllRanges();
    sel.addRange(range);
    fireEvent.click(card);
    expect(card.getAttribute("aria-expanded")).toBe("false");

    // Selection cleared → the same click toggles again.
    sel.removeAllRanges();
    fireEvent.click(card);
    expect(card.getAttribute("aria-expanded")).toBe("true");
  });

  it("the ☑ #T-xxxx id badge leads the badge row (編號 · 優先權 · 狀態), label-free, not on the title line", async () => {
    const task = mkTask({ title: "編號居首" });
    __injectMockTask(task);
    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    const badge = card.querySelector('[data-testid="task-no"]')!;
    expect(badge.closest(".task-card__badge-row")).toBeTruthy();
    expect(badge.closest(".task-card__title-line")).toBeNull();
    expect(badge.closest(".task-card__meta")).toBeNull();
    expect(badge.textContent).toContain(`#${task.taskNo}`);
    // v3 order on the row: the id badge leads, then the 優先權 chip, then
    // 狀態 — and no 任務編號 field label anywhere.
    const row = badge.closest(".task-card__badge-row")!;
    const prio = row.querySelector('[data-testid="task-priority"]')!;
    const status = row.querySelector('[data-testid="task-status"]')!;
    const follows = (a: Element, b: Element) =>
      a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING;
    expect(follows(badge, prio)).toBeTruthy();
    expect(follows(prio, status)).toBeTruthy();
    expect(card.textContent).not.toContain("任務編號");
  });

  it("the 等我回覆 jump lives in the status dropdown: badge → 查看等我回覆卡 expands + scrolls to the embedded reply card", async () => {
    const scrollCalls: Element[] = [];
    Element.prototype.scrollIntoView = function (this: Element) {
      scrollCalls.push(this);
    } as typeof Element.prototype.scrollIntoView;

    __injectMockReplyCard(mkCard({ id: "rc-jump" }));
    __injectMockTask(
      mkTask({
        title: "等我回覆跳卡",
        status: "waiting_owner",
        steps: [
          mkStep({ name: "build", status: "done", orderIdx: 0 }),
          mkStep({
            name: "approve",
            status: "waiting_owner",
            isGate: true,
            replyCardId: "rc-jump",
            orderIdx: 1,
          }),
        ],
      })
    );
    __injectMockTask(mkTask({ title: "進行中不可點", status: "in_progress" }));

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("task-card");
    const waiting = cards.find((c) =>
      c.querySelector(".task-card__title")?.textContent?.includes("跳卡")
    )!;
    const plain = cards.find((c) =>
      c.querySelector(".task-card__title")?.textContent?.includes("不可點")
    )!;

    // v5: EVERY live status badge opens the dropdown — but the jump item is
    // exclusive to 等我回覆. An 進行中 card's dropdown offers the two owner
    // actions and NO jump.
    fireEvent.click(plain.querySelector('[data-testid="task-status"]')!);
    const plainMenu = plain.querySelector(
      '[data-testid="task-status-options"]'
    )!;
    expect(plainMenu).toBeTruthy();
    expect(
      plainMenu.querySelector('[data-testid="task-status-jump"]')
    ).toBeNull();
    expect(plainMenu.textContent).toContain("標記重複");
    expect(plainMenu.textContent).toContain("終止");

    // 等我回覆: the badge drops the menu (it does NOT jump on its own — owner's
    // informed two-step ruling), and the extra 查看等我回覆卡 item is present.
    const badge = waiting.querySelector('[data-testid="task-status"]')!;
    fireEvent.click(badge);
    expect(waiting.getAttribute("aria-expanded")).toBe("false");
    const jump = waiting.querySelector('[data-testid="task-status-jump"]')!;
    expect(jump).toBeTruthy();
    expect(jump.textContent).toContain("查看等我回覆卡");

    // Picking it expands (no toggle-back — the closest() filter treats it as an
    // interactive element) and scrolls to the embedded reply card once the
    // hydrated workflow renders it.
    fireEvent.click(jump);
    expect(waiting.getAttribute("aria-expanded")).toBe("true");
    await waitFor(() => {
      const target = scrollCalls.find(
        (el) => el.getAttribute("data-testid") === "task-reply-card"
      );
      expect(target).toBeTruthy();
      expect(waiting.contains(target!)).toBe(true);
    });

    // Re-running the jump while already expanded never collapses the card.
    fireEvent.click(badge);
    fireEvent.click(waiting.querySelector('[data-testid="task-status-jump"]')!);
    expect(waiting.getAttribute("aria-expanded")).toBe("true");
  });

  it("with earlier answered cards in the timeline, the 等我回覆 jump scrolls to the WAITING card, not the DOM-first one", async () => {
    const scrollCalls: Element[] = [];
    Element.prototype.scrollIntoView = function (this: Element) {
      scrollCalls.push(this);
    } as typeof Element.prototype.scrollIntoView;

    __injectMockReplyCard(
      mkCard({
        id: "rc-old",
        summary: "先前已答的",
        status: "answered",
        answeredTs: Date.now() / 1000 - 300,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    __injectMockReplyCard(mkCard({ id: "rc-wait", summary: "還等著的" }));
    __injectMockTask(
      mkTask({
        title: "多卡跳等待那張",
        status: "waiting_owner",
        steps: [
          mkStep({
            name: "early gate",
            status: "done",
            isGate: true,
            replyCardId: "rc-old",
            replyCardStatus: "answered",
            orderIdx: 0,
          }),
          mkStep({
            name: "approve",
            status: "waiting_owner",
            isGate: true,
            replyCardId: "rc-wait",
            replyCardStatus: "waiting",
            orderIdx: 1,
          }),
        ],
      })
    );

    const { findByTestId } = renderPage();
    const card = await findByTestId("task-card");
    fireEvent.click(card.querySelector('[data-testid="task-status"]')!);
    fireEvent.click(card.querySelector('[data-testid="task-status-jump"]')!);
    await waitFor(() => {
      const target = scrollCalls.find(
        (el) => el.getAttribute("data-testid") === "task-reply-card"
      );
      expect(target).toBeTruthy();
      expect(target!.getAttribute("data-reply-card-id")).toBe("rc-wait");
    });
    // Only the waiting card was targeted — the answered collapsed one never
    // received a scroll.
    expect(
      scrollCalls.some(
        (el) => el.getAttribute("data-reply-card-id") === "rc-old"
      )
    ).toBe(false);
  });
});
