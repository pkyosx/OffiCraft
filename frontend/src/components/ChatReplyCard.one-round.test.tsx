// One owner action on an INLINE chat card = ONE refetch of that card (T-a3e4
// node 8, third site).
//
// Same defect as the 等我回覆 panes, one card instead of a whole pane: answering
// an inline card refetched it TWICE — once from the action path, once from the
// `reply_card` delta the SAME write fans back. Nothing on screen showed it (both
// rounds fetch the identical card and the second just overwrites with the same
// value), which is why it survived the first half of this ticket.
//
// 🔴 COUNTS CALLS, NOT PIXELS — on purpose. "the card flips to 已回應" was
// already green on the broken code, so a screen-level assertion has ZERO
// discriminating power here. The only honest witness is how many times the
// adapter was asked for the card.
//
// 🔴 The two actions are asymmetric and BOTH directions are pinned below,
// because "fewer requests" is the wrong goal on its own — the reanswer path
// must NOT go to zero. The SSE effect deliberately drops the delta for terminal
// (answered/expired) cards, which is what stops 70+ historical cards each
// refetching on one unrelated answer (T-cdf4); so for 重新決定 the action-path
// refetch is the ONLY thing that updates the card, and dropping it would leave
// the owner looking at the OLD answer with edit mode already closed.
//
// The adapter under test is the REAL mock adapter, not a hand-written double,
// because the fix leans on a property of the adapters themselves: answering
// fans a `reply_card` topic from INSIDE the adapter (mock: `emitTopic` inside
// answerReplyCard; http: the server's `publishReplyCard`, after the row commits
// and before the response flushes). A hand-rolled stub would let that property
// silently rot and still pass.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatReplyCard } from "./ChatReplyCard";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import { api } from "../api";
import type { ReplyCard } from "../api/adapter";

function mkCard(over: Partial<ReplyCard> = {}): ReplyCard {
  return {
    id: "rc-inline",
    from: "mira",
    kind: "decision",
    summary: "要幫你寄出這封信嗎？",
    body: "細節",
    options: [{ text: "寄出", aiPick: true }, { text: "先不要", aiPick: false }],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    task: null,
    expiredTs: null,
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

function renderCard(id: string, initialStatus?: ReplyCard["status"]) {
  return render(
    <I18nProvider>
      <ChatReplyCard
        replyCardId={id}
        fallbackSummary="要幫你寄出這封信嗎？"
        initialStatus={initialStatus ?? null}
      />
    </I18nProvider>,
  );
}

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ChatReplyCard — one owner action costs one card refetch", () => {
  it("answering an inline card refetches THAT card exactly once", async () => {
    __injectMockReplyCard(mkCard());

    const { container } = renderCard("rc-inline");
    // EVERY card mounts collapsed now (owner 2026-09-04), waiting included —
    // expanding is what loads it. Wait for that load, so the count below is the
    // cost of the answer alone.
    fireEvent.click(container.querySelector(".reply-card__collapsed-row")!);
    await waitFor(() =>
      expect(container.querySelector(".reply-option")).toBeTruthy(),
    );

    const spy = vi.spyOn(api, "getReplyCard");
    fireEvent.click(container.querySelectorAll(".reply-option")[0]);

    // The card really does flip to answered — the reconcile still works, it
    // just happens once. Without this, "exactly 1" below would also be
    // satisfied by a refetch that never ran at all.
    await waitFor(() =>
      expect(container.querySelector(".reply-card__answer-text")).toBeTruthy(),
    );

    const mine = spy.mock.calls.filter((c) => c[0] === "rc-inline");
    expect(mine).toHaveLength(1);
  });

  it("re-answering STILL refetches — this one must not go to zero", async () => {
    // The reverse control. 重新決定 acts on an ANSWERED card, and the SSE effect
    // deliberately ignores reply_card deltas for terminal cards, so the action
    // path is the card's only route to the new answer. If someone "tidies" the
    // two handlers into the same shape, this goes to 0 and the owner is left
    // looking at the previous answer with edit mode closed.
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [1], text: "", attachments: [] },
      }),
    );

    const { container, findByText } = renderCard("rc-inline", "answered");
    // A terminal card mounts COLLAPSED and does not fetch (已回覆卡預設不載);
    // expanding is what loads it.
    fireEvent.click(container.querySelector(".reply-card__collapsed-row")!);
    await waitFor(() =>
      expect(container.querySelector(".reply-card__answer-text")).toBeTruthy(),
    );

    const spy = vi.spyOn(api, "getReplyCard");
    // 重新決定 sits behind 查看當初選項 (the answered card's review toggle).
    fireEvent.click(await findByText("查看當初選項"));
    fireEvent.click(await findByText("重新決定"));
    // Re-armed chips: pick the OTHER option.
    await waitFor(() =>
      expect(container.querySelectorAll(".reply-option").length).toBeGreaterThan(1),
    );
    fireEvent.click(container.querySelectorAll(".reply-option")[0]);

    await waitFor(() => {
      const mine = spy.mock.calls.filter((c) => c[0] === "rc-inline");
      expect(mine).toHaveLength(1);
    });
  });

  it("an unrelated card's delta does not wake a settled card (T-cdf4 kept)", async () => {
    // The guard the asymmetry above depends on. It is asserted here so that a
    // future change which "fixes" the reanswer asymmetry by widening the SSE
    // effect to terminal cards has to face this test too: widening it would
    // reintroduce the broadcast storm (one answer → every historical card
    // refetches).
    __injectMockReplyCard(
      mkCard({
        id: "rc-settled",
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      }),
    );
    __injectMockReplyCard(mkCard({ id: "rc-other" }));

    const { container } = renderCard("rc-settled", "answered");
    fireEvent.click(container.querySelector(".reply-card__collapsed-row")!);
    await waitFor(() =>
      expect(container.querySelector(".reply-card__answer-text")).toBeTruthy(),
    );

    const spy = vi.spyOn(api, "getReplyCard");
    // Somebody answers a DIFFERENT card — one reply_card delta, fanned to every
    // mounted card.
    await api.answerReplyCard("rc-other", { optionIdxs: [0] });
    await waitFor(() => expect(spy.mock.calls.length).toBeGreaterThanOrEqual(0));

    expect(spy.mock.calls.filter((c) => c[0] === "rc-settled")).toHaveLength(0);
  });
});
