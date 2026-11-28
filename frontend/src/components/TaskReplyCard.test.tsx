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
  // owner 2026-08-29: 「1 跟 2 變回去原本那樣」. The header 在聊天室回覆 control
  // NAVIGATES — it writes #office/chat/<id>/msg/<msgId> and lets ChatArea
  // locate + highlight the ask (same hashRoute contract as RepliesPage's
  // 跳到原訊息). The accepted cost that comes back with it: an ask outside the
  // loaded window is not found and the room opens on the newest message,
  // silently. Deliberate — not a bug to patch here.
  it("the header 在聊天室回覆 routes to the member's chat with the ask message id", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findByTestId, getByText } = renderCard();
    await findByTestId("task-reply-card");

    fireEvent.click(getByText("在聊天室回覆"));

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
  });

  it("a waiting card's head reads only 請示 · 在聊天室回覆, snoozed or not", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1", snoozedUntilTs: Date.now() / 1000 + 3600 }));
    __injectMockReplyCard(mkCard({ id: "rc-2" }));
    render(
      <I18nProvider>
        <ReplyCardsProvider>
          <TaskReplyCard replyCardId="rc-1" />
          <TaskReplyCard replyCardId="rc-2" />
        </ReplyCardsProvider>
      </I18nProvider>
    );
    const head = (id: string) =>
      document.querySelector(`[data-reply-card-id="${id}"] .task-reply-card__head`);
    await waitFor(() => expect(head("rc-1")?.textContent).toContain("在聊天室回覆"));
    await waitFor(() => expect(head("rc-2")?.textContent).toContain("在聊天室回覆"));
    for (const id of ["rc-1", "rc-2"]) {
      expect(head(id)!.textContent).toBe("請示在聊天室回覆");
    }
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
