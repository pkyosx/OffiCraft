// Inline chat reply card (SPEC §3, B3 聊天整合). Locked here:
//   1. A thread message carrying replyCardId renders as a CARD in the stream
//      (no extra banner): quick-reply chips (options[0] tagged AI 建議) + the
//      typed composer — and NO close/skip control anywhere.
//   2. Answering in chat (chip OR typed) flips the card to 已回應 in place:
//      final answer tagged 你選的 (+ AI 建議 when it IS the AI pick), and the
//      waiting count drops (the replies page / nav badge side of the sync).
//   3. 查看當初選項 → 重新決定 re-arms the chips + shows the same composer;
//      picking another option PUTs the revision (stays answered); 取消 keeps
//      the original answer.
//   4. Two-way sync: an answer landing through the OTHER entry point (the
//      等我回覆 page / another window → a reply_card delta) flips the inline
//      card to answered without any local action.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatReplyCard } from "./ChatReplyCard";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage, ReplyCard } from "../api/adapter";
import { api } from "../api";
import { __resetMock, __injectMockReplyCard } from "../api/mock";

// The ChatArea integration test drives the thread through a mocked useChat
// (the same harness as the other ChatArea test files); the direct
// ChatReplyCard tests never touch it.
let messages: ChatMessage[] = [];
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    messagesPeer: "mira",
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要幫你寄出這封信嗎？",
    body: "",
    options: ["寄出", "先不要"],
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

function mkMember(): Member {
  return {
    id: "mira",
    memberId: "mira",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function renderCard(id = "rc-1") {
  return render(
    <I18nProvider>
      <ChatReplyCard replyCardId={id} fallbackSummary="(summary)" />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  localStorage.clear();
  Element.prototype.scrollIntoView = vi.fn();
  messages = [];
});

afterEach(() => {
  vi.restoreAllMocks();
});

// A reply_card SSE delta is NOT scoped to one card — any card being
// opened/answered fans it to EVERY mounted inline card. This captures the
// component's own subscribeEvents callback so a test can fire that unrelated
// delta directly (i.e. WITHOUT mutating the card under test) and assert
// whether the component refetches.
function captureSseCallback(): () => void {
  let cb: ((topic: string) => void) | undefined;
  vi.spyOn(api, "subscribeEvents").mockImplementation((onTopic) => {
    cb = onTopic;
    return () => {};
  });
  return () => cb?.("reply_card");
}

describe("ChatReplyCard", () => {
  it("renders inline in the chat thread as a card: chips (AI 建議 first) + composer, no close/skip", async () => {
    __injectMockReplyCard(mkCard({}));
    messages = [
      {
        id: "msg-1",
        from: "mira",
        to: "owner",
        body: "要幫你寄出這封信嗎？",
        ts: 1000,
        attachments: [],
        replyCardId: "rc-1",
      },
    ];
    const { container, findAllByText } = render(
      <I18nProvider>
        <ChatArea member={mkMember()} members={[mkMember()]} />
      </I18nProvider>
    );

    // The message row carries the CARD, not a plain bubble.
    const row = container.querySelector('[data-msg-id="msg-1"]')!;
    expect(row.querySelector('[data-testid="chat-reply-card"]')).not.toBeNull();
    expect(row.querySelector(".chat__msg-bubble")).toBeNull();

    await findAllByText("寄出");
    const options = row.querySelectorAll(".reply-option");
    expect(options).toHaveLength(2);
    expect(options[0].textContent).toContain("AI 建議");
    expect(options[1].textContent).not.toContain("AI 建議");
    // The typed composer rides the card; no close/skip control exists.
    expect(row.querySelector(".reply-composer")).not.toBeNull();
    expect(row.textContent).not.toContain("關閉");
    expect(row.textContent).not.toContain("略過");
  });

  it("answering via a chip flips the card to answered (你選的 + AI 建議) and drops the waiting count", async () => {
    __injectMockReplyCard(mkCard({}));
    const { container, findAllByText, findByTestId } = renderCard();
    await findAllByText("寄出");

    fireEvent.click(container.querySelectorAll(".reply-option")[0]);

    const final = await findByTestId("final-answer");
    expect(final.textContent).toContain("寄出");
    expect(final.textContent).toContain("你選的");
    expect(final.textContent).toContain("AI 建議");
    // The chips + composer are gone — a card is answered exactly once.
    expect(container.querySelector(".reply-composer")).toBeNull();
    // The replies-page side of the sync: the waiting count dropped.
    expect((await api.getReplyCardCount()).waiting).toBe(0);
  });

  it("answering via the typed composer closes the card with the free text", async () => {
    __injectMockReplyCard(mkCard({}));
    const { getByPlaceholderText, findAllByText, findByTestId } = renderCard();
    await findAllByText("寄出");

    const input = getByPlaceholderText("輸入回覆…");
    fireEvent.change(input, { target: { value: "收件人是誰？" } });
    fireEvent.keyDown(input, { key: "Enter" });

    const final = await findByTestId("final-answer");
    expect(final.textContent).toContain("收件人是誰？");
    expect(final.textContent).toContain("你選的");
    expect(final.textContent).not.toContain("AI 建議");
  });

  it("重新決定 re-arms the chips; picking another updates the answer in place (stays answered)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const { container, getByText, getByPlaceholderText, findByTestId } =
      renderCard();
    await findByTestId("final-answer");

    fireEvent.click(getByText("查看當初選項"));
    // Review mode first: chips visible but static.
    expect(
      (container.querySelectorAll(".reply-option")[0] as HTMLButtonElement)
        .disabled
    ).toBe(true);

    fireEvent.click(getByText("重新決定"));
    const options = container.querySelectorAll(".reply-option");
    expect((options[1] as HTMLButtonElement).disabled).toBe(false);
    expect(getByPlaceholderText("或直接打字改寫回覆…")).toBeTruthy();

    fireEvent.click(options[1]);

    await waitFor(() => {
      const final = container.querySelector('[data-testid="final-answer"]');
      expect(final?.textContent).toContain("先不要");
      expect(final?.textContent).not.toContain("AI 建議");
    });
    // A revision never reopens the card (waiting count stays 0).
    expect((await api.getReplyCardCount()).waiting).toBe(0);
  });

  it("取消 leaves 重新決定 mode without touching the answer", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const { container, getByText, queryByPlaceholderText, findByTestId } =
      renderCard();
    await findByTestId("final-answer");

    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    fireEvent.click(getByText("取消"));

    expect(queryByPlaceholderText("或直接打字改寫回覆…")).toBeNull();
    const final = container.querySelector('[data-testid="final-answer"]');
    expect(final?.textContent).toContain("寄出");
    expect(final?.textContent).toContain("AI 建議");
  });

  it("an answer landing through the OTHER entry point flips the inline card (reply_card delta sync)", async () => {
    __injectMockReplyCard(mkCard({}));
    const { container, findAllByText, findByTestId } = renderCard();
    await findAllByText("寄出");
    expect(container.querySelector(".reply-composer")).not.toBeNull();

    // The 等我回覆 page (or another window) answers the card — not this
    // component. The reply_card fan-out must refetch and flip it in place.
    await api.answerReplyCard("rc-1", { optionIdx: 1 });

    const final = await findByTestId("final-answer");
    expect(final.textContent).toContain("先不要");
    expect(final.textContent).toContain("你選的");
    expect(container.querySelector(".reply-composer")).toBeNull();
  });

  // ── SSE broadcast-storm guard (T-cdf4) ──────────────────────────────────────
  it("an ALREADY-answered card ignores an unrelated reply_card SSE delta (no refetch storm)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const fireDelta = captureSseCallback();
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderCard();
    // Mount does its one unconditional refetch (initial card shape).
    await findByTestId("final-answer");
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

  // ── lazy-load: answered cards default NOT loaded (owner 已回覆卡預設不載) ──────
  function renderHinted(initialStatus: "waiting" | "answered") {
    return render(
      <I18nProvider>
        <ChatReplyCard
          replyCardId="rc-1"
          fallbackSummary="要幫你寄出這封信嗎？"
          initialStatus={initialStatus}
        />
      </I18nProvider>
    );
  }

  it("an ANSWERED-hinted card does NOT fetch on mount — collapsed stub only", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId, queryByTestId } = renderHinted("answered");

    const stub = await findByTestId("chat-reply-card-expand");
    // The stub shows the 已回覆 tag + the ask summary (the message body — no
    // card fetched), and NOTHING was fetched.
    expect(stub.textContent).toContain("已回覆");
    expect(stub.textContent).toContain("要幫你寄出這封信嗎？");
    expect(getSpy).not.toHaveBeenCalled();
    expect(queryByTestId("final-answer")).toBeNull();
  });

  it("expanding an ANSWERED-hinted card fetches it once and shows the full answer", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("answered");

    fireEvent.click(await findByTestId("chat-reply-card-expand"));

    const final = await findByTestId("final-answer");
    expect(final.textContent).toContain("寄出");
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it("a WAITING-hinted card still loads eagerly on mount (waiting 卡照常)", async () => {
    __injectMockReplyCard(mkCard({}));
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findAllByText } = renderHinted("waiting");

    await findAllByText("寄出");
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it("a collapsed ANSWERED-hinted card ignores an unrelated reply_card SSE delta WITHOUT fetching (seeded statusRef — T-cdf4 extended)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const fireDelta = captureSseCallback();
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("answered");
    await findByTestId("chat-reply-card-expand");
    expect(getSpy).not.toHaveBeenCalled();

    // The non-scoped reply_card fan-out reaches this collapsed, never-fetched
    // card. It must stay lazy — no fetch (or lazy-load is defeated on the first
    // unrelated delta).
    fireDelta();
    await Promise.resolve();
    expect(getSpy).not.toHaveBeenCalled();
  });
});
