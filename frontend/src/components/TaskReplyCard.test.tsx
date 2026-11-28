// Inline task reply card (SPEC §3.2 內嵌等我回覆卡, M3). Locked here for
// T-cdf4: the reply_card SSE topic is NOT per-card — any card being
// opened/answered fans the same topic to EVERY mounted inline card. A card
// that is already ANSWERED is terminal (only a local 重新決定 changes it, and
// that refetches itself), so it must IGNORE the delta; a still-WAITING card
// must still refetch so it can flip in place. This is the fix for the
// broadcast-storm where 70+ historical cards each refetched on one unrelated
// answer.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskReplyCard } from "./TaskReplyCard";
import type { ReplyCard } from "../api/adapter";
import type { Member } from "../types";
import { api } from "../api";
import {
  __resetMock,
  __injectMockReplyCard,
  __setMockSuggestedRepliesReplyCard,
  mockApi,
} from "../api/mock";
import { WithSuggestedReplies } from "../test/suggestedReplies";
import { ReplyCardsProvider } from "../hooks/useReplyCards";

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要幫你寄出這封信嗎？",
    body: "",
    options: [{ text: "寄出", aiPick: false }, { text: "先不要", aiPick: true }],
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

const replyChips = (root: ParentNode) =>
  [...root.querySelectorAll('[data-testid="reply-suggested-replies"] button')].map((b) => b.textContent);

function renderCard(id = "rc-1") {
  return render(
    <I18nProvider>
      <TaskReplyCard replyCardId={id} />
    </I18nProvider>
  );
}

// Capture the component's own subscribeEvents callback so a test can fire an
// unrelated reply_card delta directly — i.e. WITHOUT mutating the card under
// test — and assert whether the component refetches.
function captureSseCallback(): () => void {
  let cb: ((topic: string) => void) | undefined;
  vi.spyOn(api, "subscribeEvents").mockImplementation((onTopic) => {
    cb = onTopic;
    return () => {};
  });
  return () => cb?.("reply_card");
}

beforeEach(() => {
  __resetMock();
  localStorage.clear();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("TaskReplyCard", () => {
  // owner 2026-08-29: 「1 跟 2 變回去原本那樣」. The header 跳到原訊息 control
  // NAVIGATES — it writes #office/chat/<id>/msg/<msgId> and lets ChatArea
  // locate + highlight the ask (same hashRoute contract as RepliesPage's
  // 跳到原訊息). The accepted cost that comes back with it: an ask outside the
  // loaded window is not found and the room opens on the newest message,
  // silently. Deliberate — not a bug to patch here.
  it("the header's icon-only 跳到原訊息 routes to the member's chat with the ask message id", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findByTestId, getByRole } = renderCard();
    await findByTestId("task-reply-card");

    const jump = getByRole("button", { name: "跳到原訊息" });
    expect(jump.getAttribute("title")).toBe("跳到原訊息");
    expect(jump.textContent).toBe("");
    fireEvent.click(jump);

    expect(window.location.hash).toBe("#office/chat/mira/msg/msg-1");
  });

  it("a waiting card's answer box offers its task type's sentences, then its author's, then the reply-card ones, each once", async () => {
    await mockApi.putTaskManualSuggestedReplies("builtin-role-design", ["照角色手冊", "收到，照這樣做"]);
    await mockApi.putChatSuggestedReplies("mira", ["Mira 專屬", "收到，照這樣做"]);
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做", "先擱著，這週不碰"]);
    __injectMockReplyCard(mkCard({ task: { id: "t-1", typeKey: "builtin-role-design", title: "設計角色", priority: "", dueTs: null, dueSoonLeadMinutes: null } }));
    const { findByTestId } = render(
      <I18nProvider>
        <WithSuggestedReplies>
          <TaskReplyCard replyCardId="rc-1" />
        </WithSuggestedReplies>
      </I18nProvider>
    );
    const card = await findByTestId("task-reply-card");
    await waitFor(() =>
      expect(replyChips(card)).toEqual(["照角色手冊", "收到，照這樣做", "Mira 專屬", "先擱著，這週不碰"])
    );
  });

  it("an ALREADY-answered card ignores an unrelated reply_card SSE delta (no refetch storm)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    const fireDelta = captureSseCallback();
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderCard();
    // Mount does its one unconditional refetch (initial card shape) — an
    // answered card renders collapsed.
    await findByTestId("task-reply-card-expand");
    expect(getSpy).toHaveBeenCalledTimes(1);

    // Some OTHER card is opened/answered elsewhere → the non-scoped reply_card
    // topic fans to this already-answered card. It is terminal — it must NOT
    // refetch (pre-fix this fired a getReplyCard; that was the storm).
    fireDelta();
    await Promise.resolve();
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it("a still-WAITING card DOES refetch on a reply_card SSE delta (flip path preserved)", async () => {
    __injectMockReplyCard(mkCard({}));
    const fireDelta = captureSseCallback();
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findAllByText } = renderCard();
    // Mount's unconditional refetch.
    await findAllByText("寄出");
    expect(getSpy).toHaveBeenCalledTimes(1);

    // A waiting card must still react to the delta (it may have just been
    // answered on another surface and needs to flip in place).
    await act(async () => {
      fireDelta();
    });
    await waitFor(() => expect(getSpy).toHaveBeenCalledTimes(2));
  });

  // ── lazy-load: answered-hinted cards default NOT loaded (owner 已回覆卡預設不載) ─
  function renderHinted(initialStatus: "waiting" | "answered") {
    return render(
      <I18nProvider>
        <TaskReplyCard
          replyCardId="rc-1"
          initialStatus={initialStatus}
          fallbackSummary="核准步驟"
        />
      </I18nProvider>
    );
  }

  it("an ANSWERED-hinted card does NOT fetch on mount — collapsed stub (step-name fallback)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        // The circled option is the one carrying ai_pick — and it is NOT the
        // first one, so a chip that reads its own position instead of its own
        // ai_pick flag puts the tag on the wrong chip and this assertion reddens.
        answer: { optionIdxs: [1], text: "", attachments: [] },
      })
    );
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("answered");

    const stub = await findByTestId("task-reply-card-expand");
    // The stub WHOLE: the 已回覆 tag plus the step-name fallback — no card was
    // fetched, so none of the card's own wording can be in this row.
    expect(stub.textContent).toBe("已回覆核准步驟");
    expect(getSpy).not.toHaveBeenCalled();
  });

  it("prints EVERY circled option on the collapsed one-line row, joined by the locale's list separator", async () => {
    // The collapsed row is one of the five faces that draw 「你選的」, and it is
    // the only one that has to fit a multi-select decision onto a single line.
    // Printing only the first circled option reads as a narrower decision than
    // the owner made — and nothing else in the tree looks at this row.
    __injectMockReplyCard(
      mkCard({
        selectMode: "multi",
        options: [
          { text: "走海運", aiPick: false },
          { text: "走空運", aiPick: true },
          { text: "先擱著", aiPick: false },
        ],
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0, 2], text: "", attachments: [] },
      })
    );
    const { findByTestId } = renderCard();
    const stub = await findByTestId("task-reply-card-expand");
    expect(stub.textContent).toBe("已回覆要幫你寄出這封信嗎？走海運、先擱著");
  });

  it("expanding an ANSWERED-hinted card fetches it once and shows the answer", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        // The circled option is the one carrying ai_pick — and it is NOT the
        // first one, so a chip that reads its own position instead of its own
        // ai_pick flag puts the tag on the wrong chip and this assertion reddens.
        answer: { optionIdxs: [1], text: "", attachments: [] },
      })
    );
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("answered");

    fireEvent.click(await findByTestId("task-reply-card-expand"));
    const final = await findByTestId("final-answer");
    expect(final.textContent).toBe("你選的AI 建議先不要");
    expect(getSpy).toHaveBeenCalledTimes(1);
    const card = await findByTestId("task-reply-card");
    expect(card.querySelector('[data-testid="reply-status-icon"]')!.getAttribute("data-state")).toBe("answered");
    expect(card.querySelector('[data-testid="reply-card-handled-at"]')!.textContent).toMatch(/^處理時間 \d{1,2}\/\d{1,2} \d{2}:\d{2}$/);
    const fold = card.querySelector('.reply-card__head [data-testid="task-reply-card-collapse"]')!;
    expect(fold.getAttribute("aria-label")).toBe("收合回覆卡");
    fireEvent.click(fold);
    expect(await findByTestId("task-reply-card-expand")).toBeTruthy();
  });

  it("an expanded card reads its status, own id and 跳到原訊息, then its title, 開卡人, the divider and the question", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1a2b3c4d5e6f", body: "信已經寫好了", snoozedUntilTs: Date.now() / 1000 + 3600 }));
    __injectMockReplyCard(mkCard({ id: "rc-2" }));
    const mira = { id: "mira", name: "Mira", kind: "staff" } as unknown as Member;
    render(
      <I18nProvider>
        <ReplyCardsProvider>
          <TaskReplyCard replyCardId="rc-1a2b3c4d5e6f" members={[mira]} />
          <TaskReplyCard replyCardId="rc-2" members={[mira]} />
        </ReplyCardsProvider>
      </I18nProvider>
    );
    const card = (id: string) => document.querySelector<HTMLElement>(`[data-reply-card-id="${id}"]`)!;
    await waitFor(() => expect(card("rc-2").querySelector('[aria-label="跳到原訊息"]')).not.toBeNull());
    await waitFor(() =>
      expect(card("rc-1a2b3c4d5e6f").querySelector('[data-testid="reply-status-icon"]')?.getAttribute("data-state")).toBe("snoozed")
    );
    expect(card("rc-2").querySelector('[data-testid="reply-status-icon"]')?.getAttribute("data-state")).toBe("waiting");

    const first = card("rc-1a2b3c4d5e6f");
    const head = first.querySelector(".reply-card__head")!;
    expect([...head.querySelectorAll("button")].map((b) => b.getAttribute("aria-label") ?? b.textContent)).toEqual([
      "複製完整請示卡 ID rc-1a2b3c4d5e6f",
      "跳到原訊息",
    ]);
    expect(head.querySelector('[data-testid="reply-card-id"]')!.textContent).toBe("#rc-1a2b3c");
    expect([...first.children].map((c) => c.className.split(" ")[0])).toEqual([
      "reply-card__head",
      "reply-card__summary",
      "card-meta",
      "reply-card__divider",
      "reply-card__body",
      "reply-card__options",
      "reply-composer",
    ]);
    const meta = first.querySelector(".card-meta")!;
    expect(meta.textContent).toBe("開卡人Mira");
    expect(first.querySelector('[data-testid="reply-card-handled-at"]')).toBeNull();
    expect(first.querySelector(".reply-card__task")).toBeNull();

    fireEvent.click(meta.querySelector('[data-testid="reply-card-opener"]')!);
    expect(window.location.hash).toBe("#office/chat/mira");
  });

  it("a WAITING-hinted card loads eagerly on mount (waiting 卡照常)", async () => {
    __injectMockReplyCard(mkCard({}));
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findAllByText } = renderHinted("waiting");

    await findAllByText("寄出");
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it("a collapsed ANSWERED-hinted card ignores an unrelated reply_card SSE delta WITHOUT fetching (seeded statusRef)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    const fireDelta = captureSseCallback();
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("answered");
    await findByTestId("task-reply-card-expand");
    expect(getSpy).not.toHaveBeenCalled();

    fireDelta();
    await Promise.resolve();
    expect(getSpy).not.toHaveBeenCalled();
  });
});
