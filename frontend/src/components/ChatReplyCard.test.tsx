// Inline chat reply card (SPEC §3, B3 聊天整合). Locked here:
//   1. A thread message carrying replyCardId renders as a CARD in the stream
//      (no extra banner): quick-reply chips (the ai_pick one tagged AI 建議,
//      wherever it sits) + the
//      typed composer — and NO close/skip control anywhere.
//   2. Answering in chat (chip OR typed) flips the card to 已回應 in place:
//      final answer tagged 你選的 (+ AI 建議 when a circled option IS the AI
//      pick), and the
//      waiting count drops (the replies page / nav badge side of the sync).
//   3. 查看當初選項 → 重新決定 re-arms the chips + shows the same composer;
//      picking another option PUTs the revision (stays answered); 取消 keeps
//      the original answer.
//   4. Two-way sync: an answer landing through the OTHER entry point (the
//      等我回覆 page / another window → a reply_card delta) flips the inline
//      card to answered without any local action.
//   5. EVERY card in the thread — waiting ones included — mounts COLLAPSED and
//      fetches NOTHING until it is expanded (owner 2026-09-04). 1–3 above are
//      claims about the card once it is OPEN, so those tests expand first.

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
    // ai_pick sits on the SECOND option ON PURPOSE: with it on the first,
    // reading `idx === 0` instead of `opt.aiPick` passes every assertion below.
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

function mkMember(): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "staff",
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

/** Renders a card and OPENS it — every card now mounts collapsed, and the
 * chips/composer/answer claims below are all about the open card. The expand is
 * the interaction, not a workaround: the assertions after it are unchanged. */
async function renderOpenCard(id = "rc-1") {
  const r = renderCard(id);
  fireEvent.click(await r.findByTestId("chat-reply-card-expand"));
  return r;
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
  it("renders inline in the chat thread as a card: chips (the ai_pick one tagged) + composer, no close/skip", async () => {
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

    // …collapsed, as every card in the thread now is. Open it: the chips and
    // the composer are what the OPEN card owes the reader.
    fireEvent.click(
      row.querySelector('[data-testid="chat-reply-card-expand"]')!,
    );
    await findAllByText("寄出");
    // Each chip WHOLE: its 1/2 ordinal, its wording and exactly the tags it
    // earned. mkCard puts ai_pick on the SECOND option, so the AI tag rides that
    // one and nothing else — a reader that tags by position fails here.
    expect(
      [...row.querySelectorAll(".reply-option")].map((e) => e.textContent),
    ).toEqual(["1寄出", "2先不要AI 建議"]);
    // The typed composer rides the card; no close/skip control exists.
    expect(row.querySelector(".reply-composer")).not.toBeNull();
    expect(row.textContent).not.toContain("關閉");
    expect(row.textContent).not.toContain("略過");
  });

  // The same owner-side claim on the CHAT surface: one tap on a chip and the
  // inline card is answered in place. Deliberately not "the answer POST fired"
  // — that phrasing survives a return to the two-step interaction.
  it("one tap on a chip flips the inline card to answered (你選的 + AI 建議) and drops the waiting count", async () => {
    __injectMockReplyCard(mkCard({}));
    const { container, findAllByText, findByTestId } = await renderOpenCard();
    await findAllByText("寄出");

    // A SINGLE card (mkCard's default) answers on the CLICK — no send button
    // in the loop (owner, rc-06bc715358c2).
    fireEvent.click(container.querySelectorAll(".reply-option")[1]);

    const final = await findByTestId("final-answer");
    expect(final.textContent).toBe(
      "你選的AI 建議先不要",
    );
    // The chips + composer are gone — a card is answered exactly once.
    expect(container.querySelector(".reply-composer")).toBeNull();
    // The replies-page side of the sync: the waiting count dropped.
    expect((await api.getReplyCardCount()).waiting).toBe(0);
  });

  it("answering via the typed composer closes the card with the free text", async () => {
    __injectMockReplyCard(mkCard({}));
    const { getByPlaceholderText, findAllByText, findByTestId } =
      await renderOpenCard();
    await findAllByText("寄出");

    const input = getByPlaceholderText("輸入回覆…");
    fireEvent.change(input, { target: { value: "收件人是誰？" } });
    fireEvent.keyDown(input, { key: "Enter" });

    const final = await findByTestId("final-answer");
    expect(final.textContent).toBe("你選的收件人是誰？");
  });

  it("重新決定 re-arms the chips; picking another updates the answer in place (stays answered)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [1], text: "", attachments: [] },
      })
    );
    const { container, getByText, getByPlaceholderText, findByTestId } =
      await renderOpenCard();
    await findByTestId("final-answer");

    fireEvent.click(getByText("查看當初選項"));
    // Review mode first: chips visible but static.
    expect(
      (container.querySelectorAll(".reply-option")[0] as HTMLButtonElement)
        .disabled
    ).toBe(true);

    fireEvent.click(getByText("重新決定"));
    const options = container.querySelectorAll(".reply-option");
    expect((options[0] as HTMLButtonElement).disabled).toBe(false);
    expect(getByPlaceholderText("或直接打字改寫回覆…")).toBeTruthy();

    fireEvent.click(options[0]);

    await waitFor(() => {
      // Moved OFF the ai_pick option → the AI 建議 tag goes with it.
      const final = container.querySelector('[data-testid="final-answer"]');
      expect(final?.textContent).toBe("你選的寄出");
    });
    // A revision never reopens the card (waiting count stays 0).
    expect((await api.getReplyCardCount()).waiting).toBe(0);
  });

  it("取消 leaves 重新決定 mode without touching the answer", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [1], text: "", attachments: [] },
      })
    );
    const { container, getByText, queryByPlaceholderText, findByTestId } =
      await renderOpenCard();
    await findByTestId("final-answer");

    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    fireEvent.click(getByText("取消"));

    expect(queryByPlaceholderText("或直接打字改寫回覆…")).toBeNull();
    const final = container.querySelector('[data-testid="final-answer"]');
    expect(final?.textContent).toBe(
      "你選的AI 建議先不要",
    );
  });

  it("an answer landing through the OTHER entry point flips the inline card (reply_card delta sync)", async () => {
    __injectMockReplyCard(mkCard({}));
    const { container, findAllByText, findByTestId } = await renderOpenCard();
    await findAllByText("寄出");
    expect(container.querySelector(".reply-composer")).not.toBeNull();

    // The 等我回覆 page (or another window) answers the card — not this
    // component. The reply_card fan-out must refetch and flip it in place.
    await api.answerReplyCard("rc-1", { optionIdxs: [0] });

    const final = await findByTestId("final-answer");
    expect(final.textContent).toBe("你選的寄出");
    expect(container.querySelector(".reply-composer")).toBeNull();
  });

  // ── SSE broadcast-storm guard (T-cdf4) ──────────────────────────────────────
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
    const { findByTestId } = await renderOpenCard();
    // The expand does its one read (initial card shape).
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
    const { findAllByText } = await renderOpenCard();
    // The expand's one read.
    await findAllByText("寄出");
    expect(getSpy).toHaveBeenCalledTimes(1);

    // A waiting card must still react to the delta (it may have just been
    // answered on another surface and needs to flip in place).
    await act(async () => {
      fireDelta();
    });
    await waitFor(() => expect(getSpy).toHaveBeenCalledTimes(2));
  });

  // ── lazy-load: EVERY card defaults NOT loaded (owner 2026-09-04) ────────────
  function renderHinted(initialStatus: "waiting" | "answered" | "expired") {
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
        answer: { optionIdxs: [0], text: "", attachments: [] },
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
        answer: { optionIdxs: [1], text: "", attachments: [] },
      })
    );
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("answered");

    fireEvent.click(await findByTestId("chat-reply-card-expand"));

    const final = await findByTestId("final-answer");
    expect(final.textContent).toBe(
      "你選的AI 建議先不要",
    );
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it("a WAITING-hinted card is COLLAPSED too, tagged 待回覆, and fetches nothing", async () => {
    // 🔴 THE OWNER RULING ITSELF (2026-09-04): 待回覆的卡也跟已回覆一樣先收合.
    // Both halves are load-bearing — the row is one line high on its first
    // frame (so nothing below it moves), and it costs no read, which is what
    // turns a history of dozens of cards into zero `getReplyCard`s.
    __injectMockReplyCard(mkCard({}));
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId, queryByTestId, queryByPlaceholderText, container } =
      renderHinted("waiting");

    const stub = await findByTestId("chat-reply-card-expand");
    expect(stub.textContent).toContain("待回覆");
    expect(stub.textContent).toContain("要幫你寄出這封信嗎？");
    expect(
      getSpy,
      "a waiting card read itself on mount — the collapsed row is paying for a card nobody opened",
    ).not.toHaveBeenCalled();
    // Nothing actionable is on screen: no chips, no composer.
    expect(container.querySelectorAll(".reply-option")).toHaveLength(0);
    expect(queryByPlaceholderText("輸入回覆…")).toBeNull();
    expect(queryByTestId("final-answer")).toBeNull();
  });

  it("expanding a WAITING-hinted card fetches it once and shows the chips", async () => {
    __injectMockReplyCard(mkCard({}));
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId, findAllByText, container } = renderHinted("waiting");

    fireEvent.click(await findByTestId("chat-reply-card-expand"));

    await findAllByText("寄出");
    expect(
      [...container.querySelectorAll(".reply-option")].map((e) => e.textContent),
    ).toEqual(["1寄出", "2先不要AI 建議"]);
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it("a COLLAPSED waiting card answered elsewhere flips its tag 待回覆 → 已回覆 in place", async () => {
    // 🔴 DELIBERATELY KEPT WHEN THE TERMINAL CARDS' SSE IMMUNITY WAS NOT. A
    // collapsed waiting card still refetches on a reply_card delta, because the
    // 等我回覆 page (or another window) answering it must be visible here
    // WITHOUT the owner opening the row — the stub is the only thing on screen
    // and a stale 待回覆 on it is a lie.
    __injectMockReplyCard(mkCard({}));
    const { findByTestId, container } = renderHinted("waiting");
    const stub = await findByTestId("chat-reply-card-expand");
    expect(stub.textContent).toContain("待回覆");

    await act(async () => {
      await api.answerReplyCard("rc-1", { optionIdxs: [0] });
    });

    await waitFor(() =>
      expect(
        container.querySelector('[data-testid="chat-reply-card-expand"]')
          ?.textContent,
      ).toContain("已回覆"),
    );
    // Still collapsed — the flip is a relabel, not an auto-expand.
    expect(
      container.querySelector("[data-reply-card-status]")
        ?.getAttribute("data-reply-card-status"),
    ).toBe("answered");
    expect(container.querySelector(".reply-composer")).toBeNull();
  });

  it("an EXPIRED-hinted card is collapsed, tagged 已過期, and fetches nothing", async () => {
    __injectMockReplyCard(mkCard({ status: "expired" }));
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("expired");

    const stub = await findByTestId("chat-reply-card-expand");
    expect(stub.textContent).toContain("已過期");
    expect(getSpy).not.toHaveBeenCalled();
  });

  it("an ANSWERED-hinted card paints nothing actionable while its expand read is in flight", async () => {
    // 🔴 THE FRAMES BETWEEN THE EXPAND AND THE RESPONSE, HELD ON PURPOSE. This
    // used to be the seed test: a prefill cache could hand this card its
    // PRE-ANSWER copy and the row painted option chips and a composer over an
    // already-answered card, one click from POSTing to it. The cache is gone
    // (owner 2026-09-04) and there is nothing left to paint from — which is
    // what this pins, in the window where it is measurable rather than after
    // the read has landed (every implementation passes that).
    const mockAnswered = mkCard({
      status: "answered",
      answeredTs: Date.now() / 1000 - 60,
      answer: { optionIdxs: [1], text: "", attachments: [] },
    });
    __injectMockReplyCard(mockAnswered);
    const getSpy = vi.spyOn(api, "getReplyCard");
    let release: () => void = () => {};
    getSpy.mockImplementation(
      () =>
        new Promise((resolve) => {
          release = () => resolve(mockAnswered);
        }),
    );
    const { container, findByTestId, queryByPlaceholderText } =
      renderHinted("answered");

    fireEvent.click(await findByTestId("chat-reply-card-expand"));

    expect(
      container.querySelectorAll(".reply-option"),
      "chips were painted over an already-answered card — one click from POSTing to it",
    ).toHaveLength(0);
    expect(queryByPlaceholderText("輸入回覆…")).toBeNull();

    await act(async () => {
      release();
      await new Promise((r) => setTimeout(r, 0));
    });

    const final = await findByTestId("final-answer");
    expect(final.textContent).toBe("你選的AI 建議先不要");
    expect(getSpy).toHaveBeenCalledTimes(1);
    expect(container.querySelectorAll(".reply-option")).toHaveLength(0);
    expect(queryByPlaceholderText("輸入回覆…")).toBeNull();
  });

  it("a collapsed ANSWERED-hinted card ignores an unrelated reply_card SSE delta WITHOUT fetching (seeded statusRef — T-cdf4 extended)", async () => {
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
