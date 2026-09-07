// One owner action = ONE list refetch round (T-a3e4 step 8).
//
// The defect this pins: answering a card used to refetch the 等我回覆 panes
// TWICE. The action path refetched, and the `reply_card` delta that the SAME
// write fans back refetched again. T-e862's generation guard hides that from
// the screen (the loser's snapshot is dropped) — which is exactly why it was
// invisible for so long. It is NOT invisible on the wire: measured against a
// real ocserverd over a 25-card pane, one answered card pulled 48 per-card
// hydrates (24 cards × 2) and 100,952 B, half of it downloaded only to be
// thrown away.
//
// 🔴 THIS FILE COUNTS CALLS, NOT PIXELS, ON PURPOSE. Every screen-level
// assertion about answering a card was already green on the broken code — the
// duplicate round is by construction invisible to the rendered output. A test
// that renders and asserts what the pane shows has ZERO discriminating power
// here. The only honest witness is how many times the adapter was asked.
//
// The adapter under test is the REAL mock adapter, not a hand-written double,
// because the fix leans on a property of the adapters themselves: answering
// fans a `reply_card` topic from INSIDE the adapter (mock: `emitTopic` in
// answerReplyCard; http: the server's `publishReplyCard`). A hand-rolled stub
// would let that property silently rot and still pass.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "../components/RepliesPage";
import { ReplyCardsProvider } from "./useReplyCards";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import { api } from "../api";
import type { ReplyCard } from "../api/adapter";

function mkCard(over: Partial<ReplyCard> = {}): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要不要切到新的排程器?",
    body: "細節",
    options: [{ text: "切過去", aiPick: true }, { text: "先不要", aiPick: false }],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    task: null,
    expiredTs: null,
    createdTs: Date.now() / 1000 - 25 * 60,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

function renderPage() {
  return render(
    <I18nProvider>
      <ReplyCardsProvider>
        <RepliesPage />
      </ReplyCardsProvider>
    </I18nProvider>
  );
}

/** Open every card on screen: the panes render collapsed rows and read a card
 * only when it is opened (owner 2026-09-07), so the option chips and 標為過期
 * these tests click do not exist until then. Opening reads ONE card each and
 * never touches `listReplyCards`, which is what this file counts. */
async function openCards() {
  for (const btn of document.querySelectorAll<HTMLElement>(
    '[data-testid="reply-card-toggle"]'
  )) {
    if (btn.getAttribute("aria-expanded") === "false") fireEvent.click(btn);
  }
  await waitFor(() =>
    expect(document.querySelectorAll('[data-testid="card-loading"]')).toHaveLength(0)
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useReplyCards — one owner action costs one refetch round", () => {
  it("answering a card refetches the waiting pane EXACTLY once", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));
    __injectMockReplyCard(mkCard({ id: "rc-3", summary: "第三張" }));

    const { findAllByTestId, queryAllByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");
    expect(cards).toHaveLength(3);
    await openCards();

    // Spy AFTER the mount fetch has settled, so the count below is the cost of
    // the answer alone.
    const listSpy = vi.spyOn(api, "listReplyCards");
    const countSpy = vi.spyOn(api, "getReplyCardCount");

    fireEvent.click(cards[0].querySelectorAll(".reply-option")[0]);

    // The answered card really does leave the pane — the reconcile still works,
    // it just happens once. Without this the "exactly 1" below could also be
    // satisfied by a refetch that never ran at all.
    await waitFor(() => expect(queryAllByTestId("waiting-card")).toHaveLength(2));

    const waitingReads = listSpy.mock.calls.filter((c) => c[0] === "waiting");
    expect(waitingReads).toHaveLength(1);
    expect(countSpy).toHaveBeenCalledTimes(1);
  });

  it("expiring a card refetches the waiting pane EXACTLY once", async () => {
    // 標為過期 is the other owner action that closes a waiting card, and it
    // rode the same duplicated path. Same write, same echo, same double round.
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));

    const { findAllByTestId, findByTestId, queryAllByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");
    expect(cards).toHaveLength(2);
    await openCards();

    const listSpy = vi.spyOn(api, "listReplyCards");

    fireEvent.click(cards[0].querySelector('[data-testid="expire-card"]')!);
    // The action is behind a ConfirmModal (terminal, not an answer).
    const confirm = await findByTestId("expire-confirm-btn");
    fireEvent.click(confirm);

    await waitFor(() => expect(queryAllByTestId("waiting-card")).toHaveLength(1));

    const waitingReads = listSpy.mock.calls.filter((c) => c[0] === "waiting");
    expect(waitingReads).toHaveLength(1);
  });

  it("a delta the cockpit did NOT cause still refetches — the trigger that was kept", async () => {
    // The fix removes the action-path refetch and keeps the delta as the single
    // reconcile trigger. If someone "fixes" the duplicate by deleting the SSE
    // path instead, the pane would stop reacting to everyone else's writes and
    // the two tests above would STILL be green. This is the other half.
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));

    const { findAllByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(1);

    // A card opened by an agent — a write this cockpit had no hand in.
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "別人開的" }));

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(2)
    );
  });
});
