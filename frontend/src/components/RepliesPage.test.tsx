// 請示 page (M2 回覆卡 B2). Locked here — the SPEC §2 acceptance behaviors:
//   1. Empty 請示 list → the ✓ 目前沒有待處理的請示 state.
//   2. Waiting cards render newest ask first by default (createdTs desc); the LONGEST-
//      waiting one keeps the highlight, each card with initiator identity,
//      已等你 {t}, and the ai_pick option tagged AI 建議 (position means nothing).
//   3. Answering (option click OR typed reply) moves the card 請示 →
//      近期已回覆; the final answer is tagged 你選的 (+ AI 建議 when a circled
//      option IS the
//      AI pick). No close/skip control exists anywhere.
//   3b. 近期已回覆 is COLLAPSED by default — only the toggle row (title · N +
//      hint) renders; clicking expands the answered cards, clicking again
//      collapses. Not persisted (component state only).
//   3c. 待回覆's LEADING card opens itself — on arrival, and again whenever the
//      card the owner just dealt with leaves the pane — so a stack of asks is
//      worked through without a click between cards. It stops for the rest of
//      the visit once he shuts one himself (「先放著」); that half is NOT
//      pinned here — see the rule file, it is code-only.
//   4. 查看當初選項 expands the original options; 重新決定 re-arms them + shows
//      the composer; picking another option updates the answer in place
//      (stays answered); 取消 keeps the original answer.
//   5. 跳到原訊息 routes to that member's chat room WITH the ask message id
//      (#office/chat/<id>/msg/<msgId>) — ChatArea locates + highlights it (B3).
//   6. Answering never touches the chat unread red dot — the badge and the
//      red dot clear independently (red dot clears only by entering the
//      conversation).
//
// KNOWN BOUNDARY (T-ee17). The task-title assertions below cover BOTH surfaces
// at once: the chat inline card (ChatReplyCard) and this Ask page render the
// same ReplyCardTaskRef, so one set of assertions is all there is.
// 若日後有人在聊天面單獨加條件（例如聊天面不顯示標題），不會有任何東西變紅。

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider, useReplyCardWaitingCount } from "../hooks/useReplyCards";
import {
  __resetMock,
  __injectMockMember,
  __injectMockChat,
  __injectMockReplyCard,
  __injectMockOutsourceWorker,
  __injectMockTask,
} from "../api/mock";
import { api } from "../api";
import type { ReplyCard } from "../api/adapter";
import { ApiError } from "../api/errors";

// Unlisted-identity cache: fixed map (the hook has its own tests) — only
// "ow-rel" (released worker) and "m-left" (dismissed staff) resolve; other ids
// keep the raw-id fallback.
const UNLISTED_NAMES: Record<string, string> = { "ow-rel": "R-2", "m-left": "阿哲" };
// Every id the page hands to the per-id name read.
const lookedUp = new Set<string>();
vi.mock("../hooks/useWorkerCodenames", () => ({
  useWorkerCodenames: (ids: readonly string[]) => {
    ids.forEach((id) => lookedUp.add(id));
    return new Map(ids.filter((id) => id in UNLISTED_NAMES).map((id) => [id, UNLISTED_NAMES[id]]));
  },
  useWorkerAvatarUrls: (ids: readonly string[]) =>
    new Map(
      ids
        .filter((id) => id === "ow-rel")
        .map((id) => [id, "/api/chat/attachment/ava-worker"]),
    ),
  // T-196: the asker's current-task line resolves nothing here — the cards in
  // THIS file are about identity, status and the answer flow. The line's own
  // behaviour (and its empty state) is pinned in RepliesPage.worker-task.test.tsx.
  useWorkerCurrentTasks: () => new Map(),
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
    createdTs: Date.now() / 1000 - 25 * 60,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

function Badge() {
  return <span data-testid="nav-badge">{useReplyCardWaitingCount()}</span>;
}

function renderPage() {
  return render(
    <I18nProvider>
      <ReplyCardsProvider>
        <Badge />
        <RepliesPage />
      </ReplyCardsProvider>
    </I18nProvider>
  );
}

/** Ids of the cards in one pane, in screen order. */
function paneIds(testId: "waiting-card" | "snoozed-card" | "answered-card"): string[] {
  return [...document.querySelectorAll<HTMLElement>(`[data-testid="${testId}"]`)].map(
    (el) => el.dataset.replyCardId!
  );
}

// 2026-11-05（四） 10:00 Asia/Taipei (the suite's TZ).
const THU_10AM = 1793844000;

function failHandledListReads() {
  const listReplyCards = api.listReplyCards.bind(api);
  return vi.spyOn(api, "listReplyCards").mockImplementation((status) =>
    status === "answered" || status === "expired"
      ? Promise.reject(new Error("handled list unavailable"))
      : listReplyCards(status)
  );
}

/** Open every card the 請示 page is showing. Every card starts COLLAPSED (owner
 * 2026-09-11「預設全部折疊」) and a card's interior is READ when it is opened, so
 * a test that asserts on the interior has to open it first. The whole card is
 * the toggle, so the click lands on the article itself.
 *
 * 🔴 IT FAILS WHEN IT FINDS NOTHING. The version this replaces looped over a
 * `querySelectorAll` and did nothing at all when the selector stopped matching —
 * a silent pass in every caller. If the seam moves again, this line goes red
 * instead. */
async function openCards() {
  const cards = document.querySelectorAll<HTMLElement>(
    "[data-reply-card-id][aria-expanded]"
  );
  expect(
    cards.length,
    "openCards() found no reply card to open — the toggle seam moved"
  ).toBeGreaterThan(0);
  for (const el of cards) {
    if (el.getAttribute("aria-expanded") === "false") fireEvent.click(el);
  }
  await waitFor(() =>
    expect(document.querySelectorAll('[data-testid="card-loading"]')).toHaveLength(0)
  );
}

const originalRangeRect = Range.prototype.getBoundingClientRect;

/** Select the first `length` characters of `el`, the way a drag leaves it, and
 * press the 回覆這段 that floats over the selection. */
function quoteStartOf(el: Element, length: number) {
  // jsdom lays nothing out; the floating button only needs a box to sit under.
  Range.prototype.getBoundingClientRect = () =>
    ({ left: 10, top: 100, width: 40, height: 16, right: 50, bottom: 116 }) as DOMRect;
  const text = document.createTreeWalker(el, NodeFilter.SHOW_TEXT).nextNode()!;
  const range = document.createRange();
  range.setStart(text, 0);
  range.setEnd(text, length);
  const sel = window.getSelection()!;
  sel.removeAllRanges();
  sel.addRange(range);
  act(() => {
    document.dispatchEvent(new Event("selectionchange"));
  });
  const button = document.body.querySelector<HTMLButtonElement>(
    "[data-testid='chat-selection-reply']",
  );
  expect(button, "回覆這段 floats over the selection").not.toBeNull();
  expect(el.closest("article")?.contains(button!), "portaled out of the card").toBe(false);
  fireEvent.click(button!);
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
  Range.prototype.getBoundingClientRect = originalRangeRect;
});

describe("RepliesPage", () => {
  it("shows the ✓ empty state when nothing awaits a reply", async () => {
    const { findByTestId } = renderPage();
    const empty = await findByTestId("replies-empty");
    expect(empty.textContent).toBe("✓ 目前沒有待處理的請示");
  });

  it("renders waiting cards newest first, all styled alike (no highlight), with identity + waited time", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(
      mkCard({ id: "mid", summary: "中間的請示", createdTs: now - 3600 })
    );
    __injectMockReplyCard(
      mkCard({ id: "young", summary: "新的請示", createdTs: now - 5 * 60 })
    );
    __injectMockReplyCard(
      mkCard({ id: "old", summary: "等很久的請示", createdTs: now - 3 * 3600 })
    );

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");
    await openCards();
    expect(cards).toHaveLength(3);
    // Default order: newest ask first (開卡時間 新→舊), whatever order the cards arrived in.
    expect(cards[0].textContent).toContain("新的請示");
    expect(cards[1].textContent).toContain("中間的請示");
    expect(cards[2].textContent).toContain("等很久的請示");
    // No card wears a highlight — owner ruled the longest-waiting accent out
    // (T-9ea9): every card carries the identical base class.
    for (const card of cards) {
      // Identical to the first card's, whatever it is — the point is that no
      // ONE card is singled out (T-9ea9 ruled the longest-waiting accent out).
      expect(card.className).toBe(cards[0].className);
      expect(
        card.querySelector('[data-testid="waited"]')?.className
      ).toBe("reply-card__waited");
    }
    // Initiator identity resolves through the roster (mock Mira) + role label.
    expect(cards[2].textContent).toContain("Mira");
    expect(cards[2].textContent).toContain("特助");
    // 已等你 {t} computed from createdTs.
    expect(cards[2].querySelector('[data-testid="waited"]')?.textContent).toBe(
      "已等你 3h"
    );
    // …in the HEAD, to the RIGHT OF 標為過期 (owner 2026-09-11). The relative
    // wait is what he scans a collapsed row for, so it rides the same line as
    // the actions, after the last of them.
    const head = cards[2].querySelector(".reply-card__head")!;
    const waited = head.querySelector('[data-testid="waited"]')!;
    const expire = head.querySelector('[data-testid="expire-card"]')!;
    expect(
      expire.compareDocumentPosition(waited) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    // 🔴 And NO absolute 開卡 stamp anywhere on a waiting card — owner
    // 2026-09-11:「我不想知道絕對時間 相對時間已經夠了」.
    for (const card of cards) {
      expect(card.querySelector('[data-testid="opened-at"]')).toBeNull();
      expect(card.textContent).not.toContain("開卡");
    }
  });

  it("resolves an outsource asker to its 外包 代號, not the raw ow- id", async () => {
    // Default GET /api/members now contains outsource rows; that must not turn
    // the card into a salaried-member identity.
    __injectMockMember({ id: "ow-rel", kind: "outsource", name: "R-2" });
    __injectMockReplyCard(
      mkCard({ id: "rc-ow", from: "ow-rel", summary: "外包的請示" })
    );
    __injectMockReplyCard(
      mkCard({ id: "rc-gone", from: "ow-9", summary: "查無外包的請示" })
    );
    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");
    await openCards();
    const names = cards.map(
      (c) => c.querySelector(".reply-card__name")?.textContent
    );
    expect(names).toContain("外包 · R-2");
    expect(names).toContain("ow-9");
    expect(names).not.toContain("ow-rel");
  });

  it("under a dismissed staff asker the card and the 開卡人 option both read its plain name, the raw id only when unresolvable", async () => {
    __injectMockReplyCard(
      mkCard({ id: "rc-left", from: "m-left", summary: "離職正職的請示" })
    );
    __injectMockReplyCard(
      mkCard({ id: "rc-unknown", from: "m-unknown", summary: "查無正職的請示" })
    );
    const { findAllByTestId, findByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");
    const nameOf = (summary: string) =>
      cards
        .find((c) => c.textContent?.includes(summary))
        ?.querySelector(".reply-card__name")?.textContent;
    expect(nameOf("離職正職的請示")).toBe("阿哲");
    expect(nameOf("查無正職的請示")).toBe("m-unknown");

    fireEvent.click(await findByTestId("filter-opener"));
    expect(
      document.querySelector('[data-testid="filter-opener-opt-m-left"]')
        ?.textContent,
    ).toBe("阿哲1");
    expect(
      document.querySelector('[data-testid="filter-opener-count-m-left"]')
        ?.textContent,
    ).toBe("1");
  });

  it("while the roster is still loading no per-id read is issued for a live staff asker, and once it lands a departed one is still named", async () => {
    const roster = await api.listMembers({ light: true });
    let land: (rows: typeof roster) => void = () => {};
    const listMembers = vi
      .spyOn(api, "listMembers")
      .mockReturnValue(new Promise((resolve) => (land = resolve)));
    try {
      __injectMockReplyCard(mkCard({ id: "rc-live", from: "mira", summary: "在職的請示" }));
      __injectMockReplyCard(mkCard({ id: "rc-left", from: "m-left", summary: "離職正職的請示" }));
      lookedUp.clear();
      const { findAllByTestId } = renderPage();
      const cards = await findAllByTestId("waiting-card");
      expect(listMembers).toHaveBeenCalled();
      expect([...lookedUp]).toEqual([]);

      land(roster);
      const left = cards.find((c) => c.textContent?.includes("離職正職的請示"))!;
      await waitFor(() =>
        expect(left.querySelector(".reply-card__name")?.textContent).toBe("阿哲"),
      );
      expect([...lookedUp]).toEqual(["m-left"]);
    } finally {
      listMembers.mockRestore();
    }
  });

  it("when the roster read fails a departed staff asker is still named through the per-id read", async () => {
    const listMembers = vi
      .spyOn(api, "listMembers")
      .mockRejectedValue(new Error("roster unavailable"));
    try {
      __injectMockReplyCard(mkCard({ id: "rc-left", from: "m-left", summary: "離職正職的請示" }));
      const { findAllByTestId } = renderPage();
      const [card] = await findAllByTestId("waiting-card");
      await waitFor(() =>
        expect(card.querySelector(".reply-card__name")?.textContent).toBe("阿哲"),
      );
    } finally {
      listMembers.mockRestore();
    }
  });

  it("under a link to a handled card whose pane was never opened, the asker that has left still reads its name", async () => {
    // jsdom has no scrollIntoView; a deep link scrolls to its card.
    const scrollIntoView = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = vi.fn();
    try {
      const twoDaysAgo = Date.now() / 1000 - 2 * 24 * 3600;
      for (const [id, from, expected] of [
        ["rc-left-old", "m-left", "阿哲"],
        ["rc-rel-old", "ow-rel", "外包 · R-2"],
      ] as const) {
        __resetMock();
        vi.mocked(Element.prototype.scrollIntoView).mockClear();
        __injectMockReplyCard(
          mkCard({
            id,
            from,
            status: "expired",
            createdTs: twoDaysAgo,
            answeredTs: twoDaysAgo,
          })
        );
        const { findByTestId, unmount } = render(
          <I18nProvider>
            <ReplyCardsProvider>
              <RepliesPage replyCardId={id} />
            </ReplyCardsProvider>
          </I18nProvider>
        );
        const card = await findByTestId("expired-card");
        expect(card.querySelector(".reply-card__name")?.textContent).toBe(expected);
        expect(card.getAttribute("aria-expanded")).toBe("true");
        expect(Element.prototype.scrollIntoView).toHaveBeenCalled();
        unmount();
      }
    } finally {
      Element.prototype.scrollIntoView = scrollIntoView;
    }
  });

  it("tags the option that carries ai_pick and leaves every other chip untagged", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();
    // Each chip WHOLE: its 1/2 ordinal, its wording and exactly the tags it
    // earned. mkCard marks the SECOND option ai_pick, so the tag rides that one
    // and the first chip is bare — a reader that tagged by position fails
    // here.
    expect(
      [...card.querySelectorAll(".reply-option")].map((e) => e.textContent),
    ).toEqual(["1寄出", "2先不要AI 建議"]);
  });

  // 🔴 Written from the OWNER's side of the screen: he taps ONE option and the
  // ask is dealt with. Nothing here presses a send button, and nothing asserts
  // that some submit function ran — an implementation-side assertion like that
  // would stay green the day someone puts the two-step interaction back, which
  // is exactly how the last round shipped a card the owner could not answer
  // (「也沒人知道有這個改變，只以為是壞掉」).
  it("one tap on an option answers a single-select card outright — no second click anywhere", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId, findByTestId, queryAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    fireEvent.click(card.querySelectorAll(".reply-option")[1]);

    // The answered pane is collapsed by default — expand it to see the card.
    // (Expanding a pane is not answering anything: the ONLY click aimed at the
    // card itself was the one above.)
    fireEvent.click(await findByTestId("answered-toggle"));
    const answeredCard = await findByTestId("answered-card");
    await openCards();
    // The ask is GONE from 待回覆 and standing in 近期已回覆 — the owner's own
    // test of "did my tap take?".
    await waitFor(() => expect(queryAllByTestId("waiting-card")).toHaveLength(0));
    const final = answeredCard.querySelector('[data-testid="final-answer"]');
    // The circled option IS the ai_pick one → the AI 建議 tag rides alongside.
    expect(final?.textContent).toBe("你選的AI 建議先不要");
  });

  // T-4166: a 409 on answer is TERMINAL for that card — its task closed
  // underneath it (orphan), or it was already handled. Showing the network-blip
  //「回覆失敗，請稍後重試」and leaving the dead card on screen would send the owner
  // down a road that is 409 every time. Assert the DISTINCT message (a shared string would make this test
  // pass on the buggy code too) and the re-pull that clears the card.
  it("a 409 answer says the card is stale — never 請稍後重試 — and re-pulls the pane", async () => {
    __injectMockReplyCard(mkCard({}));
    const answerSpy = vi
      .spyOn(api, "answerReplyCard")
      .mockRejectedValue(
        new ApiError("http 409 for POST /api/reply-cards/rc-1/answer", 409,
          "conflict",
          "task 'T-69cf' is already closed (done) — this card is orphaned and can no longer be answered")
      );
    const listSpy = vi.spyOn(api, "listReplyCards");
    const { findAllByTestId, findByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();
    const before = listSpy.mock.calls.length;

    fireEvent.click(card.querySelectorAll(".reply-option")[0]);

    const notice = await findByTestId("replies-action-error");
    expect(notice.textContent).toBe(zh.replies.answerStale);
    expect(notice.textContent).not.toBe(zh.replies.answerError);
    // It must point at the exit that DOES work (標為過期), not at a retry.
    expect(notice.textContent).toContain(zh.replies.expire);
    expect(answerSpy).toHaveBeenCalledTimes(1);
    // …and the panes are re-pulled, so a card that is no longer waiting leaves.
    await waitFor(() =>
      expect(listSpy.mock.calls.length).toBeGreaterThan(before)
    );
  });

  // The SAME branch must be wired on 重新決定 (doReanswer), not only on the
  // first answer — a card whose task closed while the owner was revising is the
  // identical dead end (G12: without this the doReanswer catch could keep the
  // old wording and every test would still pass).
  it("a 409 on 重新決定 says the card is stale too", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    vi.spyOn(api, "reanswerReplyCard").mockRejectedValue(
      new ApiError("http 409 for PUT /api/reply-cards/rc-1/answer", 409,
        "conflict", "task is already closed — this card is orphaned")
    );
    const { findByTestId, getByText } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    const card = await findByTestId("answered-card");
    await openCards();
    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    fireEvent.click(card.querySelectorAll(".reply-option")[1]);

    const notice = await findByTestId("replies-action-error");
    expect(notice.textContent).toBe(zh.replies.answerStale);
  });

  // The regression side: a NON-409 failure keeps the retry wording — the 409
  // branch must not swallow honest transient errors.
  it("a 500 answer keeps the retryable 回覆失敗，請稍後重試 wording and the quoted passage", async () => {
    __injectMockReplyCard(mkCard({ body: "收件人寫在附件裡" }));
    vi.spyOn(api, "answerReplyCard").mockRejectedValue(
      new ApiError("http 500 for POST /api/reply-cards/rc-1/answer", 500,
        "internal_error", "boom")
    );
    const { findAllByTestId, findByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();
    quoteStartOf(card.querySelector(".reply-card__body")!, 3);

    fireEvent.click(card.querySelectorAll(".reply-option")[0]);

    const notice = await findByTestId("replies-action-error");
    expect(notice.textContent).toBe(zh.replies.answerError);
    expect(
      card.querySelector('[data-testid="reply-composer-quote"]')?.textContent,
    ).toBe("引用原文收件人");
  });

  it("answering via the typed composer closes the card with the free text as the answer, quoting the passage selected from the question", async () => {
    __injectMockReplyCard(mkCard({ body: "收件人寫在附件裡" }));
    const { findAllByTestId, findByTestId, getByPlaceholderText } =
      renderPage();
    const [waiting] = await findAllByTestId("waiting-card");
    await openCards();

    quoteStartOf(waiting.querySelector(".reply-card__body")!, 3);
    expect(waiting.getAttribute("aria-expanded")).toBe("true");
    expect(
      waiting.querySelector('[data-testid="reply-composer-quote"]')?.textContent,
    ).toBe("引用原文收件人");
    const input = getByPlaceholderText("輸入回覆…");
    expect(document.activeElement).toBe(input);

    fireEvent.change(input, { target: { value: "收件人是誰？" } });
    fireEvent.keyDown(input, { key: "Enter" });

    fireEvent.click(await findByTestId("answered-toggle"));
    const answeredCard = await findByTestId("answered-card");
    await openCards();
    const final = answeredCard.querySelector('[data-testid="final-answer"]');
    // A free-text answer circles nothing, so it is not the AI pick either.
    expect(final?.textContent).toBe("你選的收件人是誰？");
    expect(
      answeredCard.querySelector('[data-testid="final-answer-quote"]')?.textContent,
    ).toBe("引用原文收件人");
  });

  it("typed answer naming a colleague → sent as a mention link, and the answer shows the name opening their detail with 返回 to 請示", async () => {
    __injectMockReplyCard(mkCard({}));
    const answerSpy = vi.spyOn(api, "answerReplyCard");
    const { findAllByTestId, findByTestId, getByPlaceholderText } = renderPage();
    await findAllByTestId("waiting-card");
    await openCards();

    const input = getByPlaceholderText("輸入回覆…");
    await waitFor(() => {
      fireEvent.change(input, { target: { value: "@mi" } });
      expect(document.querySelector('[role="listbox"]')?.textContent).toBe("Mira特助");
    });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(answerSpy).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "交給 @Mira 處理" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(answerSpy).toHaveBeenCalledTimes(1));
    expect(answerSpy.mock.calls[0][1].text).toBe("交給 [@Mira](/#office/member/mira) 處理");

    fireEvent.click(await findByTestId("answered-toggle"));
    const answeredCard = await findByTestId("answered-card");
    await openCards();
    const final = answeredCard.querySelector('[data-testid="final-answer"]')!;
    expect(final.textContent).toBe("你選的交給 @Mira 處理");
    fireEvent.click(final.querySelector("button.md-mention")!);
    expect(window.location.hash).toBe("#office/member/mira/from/replies/rc-1");
  });

  it("arrival with every card collapsed reads only the light roster, never the outsource list", async () => {
    __injectMockReplyCard(mkCard({}));
    const membersSpy = vi.spyOn(api, "listMembers");
    const workersSpy = vi.spyOn(api, "listOutsourceWorkers");
    const workerSpy = vi.spyOn(api, "getOutsourceWorker");
    const { findAllByTestId } = renderPage();
    await findAllByTestId("waiting-card");
    await waitFor(() => expect(membersSpy).toHaveBeenCalled());
    await new Promise((r) => setTimeout(r, 50));

    expect(membersSpy.mock.calls).toEqual([[{ light: true }]]);
    expect(workersSpy).not.toHaveBeenCalled();
    expect(workerSpy).not.toHaveBeenCalled();
  });

  it("opening cards reads the outsource list once for all of them, and a worker mention in an answer opens that worker", async () => {
    __injectMockTask({
      id: "t-492",
      taskNo: "T-492",
      title: "Fix chat mention list",
      typeKey: "",
      description: "",
      status: "in_progress",
      priority: "mid",
      executorKind: "outsource",
      executorId: "ow-7",
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
    });
    __injectMockOutsourceWorker({
      id: "ow-7",
      codename: "O-7",
      model: "",
      effort: "",
      status: "active",
      taskId: "t-492",
      taskNo: "T-492",
      taskTitle: "Fix chat mention list",
    });
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-a", createdTs: now - 25 * 60 }));
    __injectMockReplyCard(mkCard({ id: "rc-b", createdTs: now - 26 * 60 }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-done",
        status: "answered",
        answeredTs: now - 60,
        answer: {
          optionIdxs: [],
          text: "交給 [@外包 · O-7](/#office/worker/ow-7)",
          attachments: [],
        },
      })
    );
    const workersSpy = vi.spyOn(api, "listOutsourceWorkers");
    const { findAllByTestId, findByTestId, getAllByPlaceholderText } = renderPage();
    await findAllByTestId("waiting-card");
    fireEvent.click(await findByTestId("answered-toggle"));
    const answeredCard = await findByTestId("answered-card");
    await openCards();

    await waitFor(() => expect(workersSpy).toHaveBeenCalledTimes(1));
    const input = getAllByPlaceholderText("輸入回覆…")[0];
    await waitFor(() => {
      fireEvent.change(input, { target: { value: "@492" } });
      expect(document.querySelector('[role="listbox"]')?.textContent).toContain("O-7");
    });
    const final = answeredCard.querySelector('[data-testid="final-answer"]')!;
    fireEvent.click(final.querySelector("button.md-mention")!);
    expect(window.location.hash).toBe("#office/worker/ow-7/from/replies/rc-done");
    expect(workersSpy).toHaveBeenCalledTimes(1);
  });

  it("近期已處理 is collapsed by default; the title row toggles it open and shut", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    const { findByTestId, queryAllByTestId } = renderPage();

    // Collapsed by default: the toggle row shows title · count, no cards.
    const toggle = await findByTestId("answered-toggle");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(toggle.textContent).toContain("近期已處理 · 1");
    expect(queryAllByTestId("answered-card")).toHaveLength(0);

    // Click → expanded; click again → collapsed.
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(await findByTestId("answered-card")).toBeTruthy();

    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(queryAllByTestId("answered-card")).toHaveLength(0);
  });

  it("查看當初選項 expands the original options with the standing pick marked", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    const { findByTestId, getByText } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    const card = await findByTestId("answered-card");
    await openCards();

    fireEvent.click(getByText("查看當初選項"));
    const options = card.querySelectorAll(".reply-option");
    // Review mode: options render but are NOT pickable yet, and the standing
    // answer (index 0) wears 目前 — every chip WHOLE so a 目前 that leaked onto
    // the wrong chip, or a lost AI tag, is a failure here. The two tags sit on
    // DIFFERENT chips, which is what makes a positional reader of either fail.
    expect([...options].map((e) => e.textContent)).toEqual([
      "1寄出目前",
      "2先不要AI 建議",
    ]);
    expect((options[0] as HTMLButtonElement).disabled).toBe(true);
    // 重新決定 sits at the expansion's bottom.
    expect(getByText("重新決定")).toBeTruthy();
  });

  it("重新決定 re-arms the options with the standing quote; picking another updates the answer in place and keeps the quote, and its x drops the quote from the next revision", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: {
          optionIdxs: [1],
          text: "",
          replyExcerpt: "這封信",
          attachments: [],
        },
      })
    );
    const { findByTestId, getByText, getByPlaceholderText } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    let card = await findByTestId("answered-card");
    await openCards();

    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    // Edit mode: options pickable + the SAME typed composer appears.
    const options = card.querySelectorAll(".reply-option");
    expect((options[0] as HTMLButtonElement).disabled).toBe(false);
    expect(getByPlaceholderText("或直接打字改寫回覆…")).toBeTruthy();
    expect(
      card.querySelector('[data-testid="reply-composer-quote"]')?.textContent,
    ).toBe("引用原文這封信");

    // Single-select 重新決定 also lands on the click.
    fireEvent.click(options[0]);

    await waitFor(async () => {
      card = await findByTestId("answered-card");
      const final = card.querySelector('[data-testid="final-answer"]');
      // No longer the AI pick → the AI 建議 tag is gone; 你選的 stays.
      expect(final?.textContent).toBe("你選的寄出");
      expect(
        card.querySelector('[data-testid="final-answer-quote"]')?.textContent,
      ).toBe("引用原文這封信");
    });

    const reanswer = vi.spyOn(api, "reanswerReplyCard");
    if (!card.querySelector(".reply-card__past")) fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    fireEvent.click(
      card.querySelector(`[aria-label="${zh.replies.quoteCancel}"]`)!,
    );
    expect(card.querySelector('[data-testid="reply-composer-quote"]')).toBeNull();
    fireEvent.click(card.querySelectorAll(".reply-option")[1]);

    await waitFor(() =>
      expect(reanswer).toHaveBeenLastCalledWith("rc-1", {
        optionIdxs: [1],
        text: "",
        attachments: [],
      }),
    );
    await waitFor(async () => {
      card = await findByTestId("answered-card");
      expect(card.querySelector('[data-testid="final-answer"]')?.textContent).toBe(
        "你選的AI 建議先不要",
      );
      expect(card.querySelector('[data-testid="final-answer-quote"]')).toBeNull();
    });
  });

  it("取消 leaves 重新決定 mode without touching the answer", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [1], text: "", attachments: [] },
      })
    );
    const { findByTestId, getByText, queryByPlaceholderText } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    const card = await findByTestId("answered-card");
    await openCards();

    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    fireEvent.click(getByText("取消"));

    expect(queryByPlaceholderText("或直接打字改寫回覆…")).toBeNull();
    const final = card.querySelector('[data-testid="final-answer"]');
    expect(final?.textContent).toBe("你選的AI 建議先不要");
  });

  // 跳到原訊息 NAVIGATES (owner 2026-08-29: 「1 跟 2 變回去原本那樣」). The
  // control writes #office/chat/<id>/msg/<msgId> and ChatArea locates +
  // highlights the ask (B3 聊天整合). The known cost the owner accepted with
  // this: when the ask is not inside the loaded window ChatArea's DOM search
  // misses and the room simply opens on the newest message, silently. That is
  // the OLD behaviour coming back on purpose — do not "fix" it here.
  it("跳到原訊息 routes to the member's chat with the ask message id (B3 locate target)", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId, getByText } = renderPage();
    await findAllByTestId("waiting-card");
    await openCards();

    fireEvent.click(getByText("跳到原訊息"));
    expect(window.location.hash).toBe("#office/chat/mira/msg/msg-1");
  });

  // T-a706 (owner 2026-07-21 screenshot): the header avatar was the one place
  // in the cockpit whose avatar did NOT open the member panel — every other
  // surface (roster row, etc.) already does. Mirrors MemberCard's avatar
  // click semantics + the SAME hash seam (frontend/src/lib/hashRoute.ts).
  it("clicking the avatar opens that member's detail panel (#office/member/<id>), tagged so 返回 lands back on 請示", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    fireEvent.click(card.querySelector(".reply-card__avatar")!);
    // T-a706 owner-acceptance finding: without the /from/replies tag,
    // OfficePage's own 返回 button reset to its default chat view instead of
    // back here — see OfficePage.member-detail-backto.test.tsx for the fix
    // proven from the OTHER side of this hash contract.
    expect(window.location.hash).toBe("#office/member/mira/from/replies/rc-1");
  });

  it("clicking an outsource asker's avatar opens the worker panel (#office/worker/<id>), not the member one, same 返回 tag", async () => {
    __injectMockMember({ id: "ow-rel", kind: "outsource", name: "R-2" });
    __injectMockReplyCard(mkCard({ id: "rc-ow", from: "ow-rel" }));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    expect(card.querySelector(".reply-card__avatar img")?.getAttribute("src")).toBe(
      "/api/chat/attachment/ava-worker",
    );
    fireEvent.click(card.querySelector(".reply-card__avatar")!);
    expect(window.location.hash).toBe("#office/worker/ow-rel/from/replies/rc-ow");
  });

  it("the avatar has an accessible name (aria-label) — Avatar's inner glyphs are aria-hidden", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    const avatarBtn = card.querySelector(".reply-card__avatar")!;
    expect(avatarBtn.tagName).toBe("BUTTON");
    expect(avatarBtn.getAttribute("aria-label")).toBe(zh.office.viewProfile);
  });

  it("標為過期 double-confirms, closes the card without an answer, and lands it 已過期 in 近期已處理", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findByTestId, findAllByTestId, queryAllByTestId, queryByTestId } =
      renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    // T-1aa4: 標為過期 wears the SAME outlined class as 跳到原訊息 — one
    // shared button style for the two header actions.
    expect(
      card
        .querySelector('[data-testid="expire-card"]')!
        .classList.contains("reply-card__jump")
    ).toBe(true);

    // The head's 標為過期 opens the confirm modal; cancel keeps the card.
    fireEvent.click(card.querySelector('[data-testid="expire-card"]')!);
    const modal = await findByTestId("expire-confirm");
    expect(modal.textContent).toContain("要幫你寄出這封信嗎？");
    fireEvent.click(modal.querySelector(".confirm-modal__btn")!); // cancel (first button)
    await waitFor(() =>
      expect(queryByTestId("expire-confirm")).toBeNull()
    );
    expect(queryAllByTestId("waiting-card")).toHaveLength(1);

    // Confirm actually expires: the card leaves 待回覆 and shows 已過期 (grey
    // terminal — no chips to pick, no 重新決定) in 近期已處理.
    fireEvent.click(card.querySelector('[data-testid="expire-card"]')!);
    fireEvent.click(await findByTestId("expire-confirm-btn"));
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(0)
    );
    fireEvent.click(await findByTestId("answered-toggle"));
    const expired = await findByTestId("expired-card");
    await openCards();
    expect(expired.textContent).toContain("已過期");
    expect(await findByTestId("expired-note")).toBeTruthy();
    expect(expired.querySelector(".reply-option:not([disabled])")).toBeNull();
    expect(expired.textContent).not.toContain("重新決定");
  });

  it("answering here never clears the chat unread red dot (independent signals)", async () => {
    // The ask rides a real chat message (unread for the owner) AND a card.
    __injectMockChat({
      id: "msg-1",
      from: "mira",
      to: "owner",
      body: "要幫你寄出這封信嗎？",
      ts: Date.now() / 1000 - 60,
      attachments: [],
      replyCardId: "rc-1",
    });
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId, findByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    fireEvent.click(card.querySelectorAll(".reply-option")[0]);
    fireEvent.click(await findByTestId("answered-toggle"));
    await findByTestId("answered-card");
    await openCards();

    // The card closed, but Mira's unread count still stands — only entering
    // the conversation (listChat) clears it.
    const members = await api.listMembers();
    expect(members.find((m) => m.id === "mira")?.unreadCount).toBe(1);
  });

  // T-ee17: the card has to say WHICH piece of work it is about. A contractor
  // card read only「外勤支援 · X-79」and the owner could not tell what he was
  // being asked about without leaving the page.
  it("names the task a contractor's ask belongs to, verbatim", async () => {
    __injectMockReplyCard(
      mkCard({
        from: "ow-rel",
        task: {
          id: "t-1",
          typeKey: "review-pr",
          title: "[ACE-7580] SOC2 年度風險評估：review Google Drive 上的 ISMS 文件",
        },
      }),
    );
    const { findByTestId } = renderPage();
    await findByTestId("waiting-card");
    await openCards();
    const ref = await findByTestId("reply-task-ref");
    // Verbatim — a shortened or reworded title is a different claim about
    // which task this is.
    expect(ref.textContent).toContain(
      "[ACE-7580] SOC2 年度風險評估：review Google Drive 上的 ISMS 文件",
    );
    // The initiator is still named: the row answers WHO and WHICH, not one
    // instead of the other.
    const card = await findByTestId("waiting-card");
    expect(card.textContent).toContain("R-2");
  });

  // Same field, different path (a staff member's card binds to its own step),
  // so it gets its own assertion rather than riding on the contractor one.
  it("names the task a staff member's ask belongs to", async () => {
    __injectMockMember({ id: "m-dev", name: "Kyle", kind: "staff" });
    __injectMockReplyCard(
      mkCard({
        from: "m-dev",
        task: { id: "t-2", typeKey: "", title: "把開機說明改成座艙可編輯" },
      }),
    );
    const { findByTestId } = renderPage();
    await findByTestId("waiting-card");
    await openCards();
    const ref = await findByTestId("reply-task-ref");
    expect(ref.textContent).toContain("把開機說明改成座艙可編輯");
  });

  // Owner 2026-08-14, T-ee17 acceptance:「這個不能夠放到最一開始嗎？」— at the
  // bottom of the card the row only answered WHICH work after the whole ask had
  // been read. Judged by DOM order, not by geometry: a rendered-later row can
  // still be painted high by CSS, and then the reading order screen readers and
  // keyboards follow would disagree with the picture.
  it("puts the task row at the top of the card, above the head and the summary, on waiting, snoozed and handled cards", async () => {
    __injectMockReplyCard(
      mkCard({ id: "rc-w", task: { id: "t-4", typeKey: "review-pr", title: "改設定頁" } }),
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-a",
        status: "answered",
        answer: { optionIdxs: [0], text: "好", attachments: [] },
        answeredTs: Date.now() / 1000 - 60,
        task: { id: "t-5", typeKey: "review-pr", title: "改開機說明" },
      }),
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-s",
        snoozedUntilTs: Date.now() / 1000 + 3600,
        task: { id: "t-6", typeKey: "review-pr", title: "改主題" },
      }),
    );
    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    fireEvent.click(await findByTestId("snoozed-toggle"));
    await findByTestId("answered-card");
    await findByTestId("snoozed-card");
    await openCards();

    for (const testId of ["waiting-card", "snoozed-card", "answered-card"]) {
      const card = await findByTestId(testId);
      const ref = card.querySelector('[data-testid="reply-task-ref"]')!;
      const head = card.querySelector(".reply-card__head")!;
      const summary = card.querySelector(".reply-card__summary")!;
      expect(card.firstElementChild).toBe(ref);
      expect(
        ref.compareDocumentPosition(head) & Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy();
      expect(
        head.compareDocumentPosition(summary) & Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy();
    }
  });

  // An empty title leaves the jump alone on the row — the cell is not drawn at
  // all, so there is no empty box and no placeholder standing in for a title
  // nobody wrote. The typeKey does NOT step in as a substitute.
  it("draws no title cell when the task's title is empty", async () => {
    __injectMockReplyCard(
      mkCard({ task: { id: "t-3", typeKey: "review-pr", title: "" } }),
    );
    const { findByTestId } = renderPage();
    await findByTestId("waiting-card");
    await openCards();
    const ref = await findByTestId("reply-task-ref");
    expect(ref.querySelector(".reply-card__task-title")).toBeNull();
    expect(ref.textContent).toBe("#t-3查看任務");
  });

  it("starts every card collapsed — head, task row and 已等你 only, no question and no options", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(
      mkCard({
        id: "rc-lead",
        summary: "第一個請示",
        body: "先看這一張",
        createdTs: now - 60,
        task: { id: "t-1", typeKey: "sync", title: "同步設定" },
      })
    );
    __injectMockReplyCard(
      mkCard({ id: "rc-next", summary: "第二個請示", body: "再看這一張", createdTs: now - 3600 })
    );

    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");

    // 🔴 NOTHING OPENS BY ITSELF (owner 2026-09-11「預設全部折疊」), the leading
    // card included. Waited on, not read once: an auto-open lands a commit after the first paint, so a bare read
    // here would pass against the very behaviour this forbids.
    await waitFor(() =>
      expect(document.querySelectorAll('[data-testid="card-loading"]')).toHaveLength(0)
    );
    for (const card of cards) {
      expect(card.getAttribute("aria-expanded")).toBe("false");
      expect(card.querySelectorAll(".reply-option")).toHaveLength(0);
      expect(card.querySelector(".reply-card__body")).toBeNull();
    }
    expect(cards[0].textContent).not.toContain("先看這一張");

    // What the collapsed row DOES show is the card's own head — the same strip
    // the open card wears (owner:「折疊起來那一列的內容不要另外設計」): who asked,
    // their role, both header actions, the task title, and the ticking counter.
    const head = cards[0].querySelector(".reply-card__head")!;
    expect(head.querySelector(".reply-card__name")?.textContent).toBe("Mira");
    expect(head.querySelector(".reply-card__role")?.textContent).toBe("特助");
    expect(head.textContent).toContain("跳到原訊息");
    expect(head.querySelector('[data-testid="expire-card"]')).toBeTruthy();
    expect(head.querySelector('[data-testid="waited"]')).toBeTruthy();
    expect(cards[0].querySelector(".reply-card__task-title")?.textContent).toBe(
      "同步設定"
    );
  });

  it("keeps an answer read from the server visible after 24h when its list cannot load", async () => {
    const staleTs = Date.now() / 1000 - 25 * 3600;
    const serverCard = mkCard({
      id: "rc-1",
      body: "寄出後無法撤回",
      createdTs: staleTs,
    });
    __injectMockReplyCard(serverCard);
    const listSpy = failHandledListReads();
    const { findAllByTestId, findByTestId, queryAllByTestId, container } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    expect(card.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(card);
    await waitFor(() =>
      expect(card.querySelector(".reply-card__body")?.textContent).toContain(
        "寄出後無法撤回"
      )
    );
    expect(card.querySelectorAll(".reply-option")).toHaveLength(2);
    expect(card.getAttribute("aria-expanded")).toBe("true");
    // The head did not move or double: it was there before the click and there
    // is exactly one of it after.
    expect(card.querySelectorAll(".reply-card__head")).toHaveLength(1);

    // 🔴 T-48: besides answering and 標為過期, clicking the card closes it.
    fireEvent.click(card);
    await waitFor(() =>
      expect(card.querySelector(".reply-card__body")).toBeNull()
    );
    expect(card.querySelectorAll(".reply-option")).toHaveLength(0);
    expect(card.getAttribute("aria-expanded")).toBe("false");
    // Still the same card, still naming who asked — closing is not removing.
    expect(card.textContent).toContain("Mira");

    serverCard.status = "answered";
    serverCard.answeredTs = staleTs;
    serverCard.answer = { optionIdxs: [1], text: "", attachments: [] };
    fireEvent.click(card);

    const answered = await findByTestId("answered-card");
    expect(answered.querySelector(".reply-card__body")?.textContent).toBe("寄出後無法撤回");
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(0)
    );
    expect(
      answered.querySelector('[data-testid="final-answer"]')?.textContent
    ).toContain("先不要");
    expect(
      answered.querySelector(".reply-card__answered-at")?.textContent
    ).toMatch(/^已回覆 (?:\d{4}\/)?\d{1,2}\/\d{1,2} \d{2}:\d{2}$/);
    expect(container.querySelector(".replies__section-title")?.textContent).toBe(
      "請示 · 0開卡時間（新到舊）"
    );
    const handledToggle = await findByTestId("answered-toggle");
    expect(handledToggle.textContent).toContain("近期已處理 · 1");
    expect(
      handledToggle
        .closest("section")
        ?.querySelector('[data-testid="answered-card"]')
    ).toBe(answered);
    expect(await findByTestId("handled-load-error")).toBeTruthy();
    expect(listSpy).toHaveBeenCalledWith("answered");
    expect(listSpy).toHaveBeenCalledWith("expired");
    expect(
      listSpy.mock.calls.filter(([status]) => status === "waiting")
    ).toHaveLength(2);
  });

  it("keeps an expired card read from the server visible after 24h when its list cannot load", async () => {
    const staleTs = Date.now() / 1000 - 25 * 3600;
    const serverCard = mkCard({ id: "rc-expired", body: "寄出後無法撤回", createdTs: staleTs });
    __injectMockReplyCard(serverCard);
    const listSpy = failHandledListReads();
    const { findAllByTestId, findByTestId, queryAllByTestId, container } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    serverCard.status = "expired";
    serverCard.expiredTs = staleTs;
    fireEvent.click(card);

    const expired = await findByTestId("expired-card");
    expect(expired.querySelector(".reply-card__body")?.textContent).toBe("寄出後無法撤回");
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(0)
    );
    expect(expired.textContent).toContain("已過期");
    expect(expired.querySelector('[data-testid="expired-note"]')).toBeTruthy();
    expect(
      expired.querySelector(".reply-card__answered-at")?.textContent
    ).toMatch(/^已過期 (?:\d{4}\/)?\d{1,2}\/\d{1,2} \d{2}:\d{2}$/);
    expect(container.querySelector(".replies__section-title")?.textContent).toBe(
      "請示 · 0開卡時間（新到舊）"
    );
    const handledToggle = await findByTestId("answered-toggle");
    expect(handledToggle.textContent).toContain("近期已處理 · 1");
    expect(
      handledToggle
        .closest("section")
        ?.querySelector('[data-testid="expired-card"]')
    ).toBe(expired);
    expect(await findByTestId("handled-load-error")).toBeTruthy();
    expect(listSpy).toHaveBeenCalledWith("answered");
    expect(listSpy).toHaveBeenCalledWith("expired");
    expect(
      listSpy.mock.calls.filter(([status]) => status === "waiting")
    ).toHaveLength(2);
  });

  it("a click on a header action does its own job instead of toggling the card", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const { findAllByTestId, findByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    fireEvent.click(card.querySelector('[data-testid="expire-card"]')!);
    // The confirm modal opened AND the card stayed shut — the interaction
    // filter in onCardToggleClick is what keeps those two facts together.
    expect(await findByTestId("expire-confirm")).toBeTruthy();
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  // The negative half: no task means the row says nothing about a task —
  // NOT an empty slot, which reads as "this ask has no title" rather than
  // "this ask has no task".
  it("shows no task row at all on a pure chat ask", async () => {
    __injectMockReplyCard(mkCard({ task: null }));
    const { findByTestId, queryByTestId } = renderPage();
    await findByTestId("waiting-card");
    await openCards();
    expect(queryByTestId("reply-task-ref")).toBeNull();
  });

  it("顯示方式 › 排序依據 → each order re-sorts 待回覆 only (已延後 stays by return time, 近期已處理 by handled time), the active one is named beside the title, and the choice survives a remount", async () => {
    const now = Date.now() / 1000;
    const task = (id: string, priority: string, dueTs: number | null = null) => ({ id, typeKey: "", title: id, priority, dueTs });
    __injectMockReplyCard(mkCard({ id: "rc-notask", createdTs: now - 10 * 60, task: null }));
    __injectMockReplyCard(mkCard({ id: "rc-high", createdTs: now - 3 * 3600, task: task("T-1", "high") }));
    __injectMockReplyCard(mkCard({ id: "rc-low", createdTs: now - 3600, task: task("T-2", "low", now + 3600) }));
    __injectMockReplyCard(mkCard({ id: "rc-mid", createdTs: now - 2 * 3600, task: task("T-3", "mid", now + 7200) }));
    __injectMockReplyCard(mkCard({ id: "rc-frozen", createdTs: now - 4 * 3600, task: task("T-4", "frozen", now - 60) }));
    __injectMockReplyCard(
      mkCard({ id: "rc-handled", status: "answered", answeredTs: now - 60, answer: { optionIdxs: [0], text: "", attachments: [] } })
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-handled-early",
        createdTs: now - 30,
        status: "answered",
        answeredTs: now - 3600,
        answer: { optionIdxs: [0], text: "", attachments: [] },
        task: task("T-5", "high", now - 60),
      })
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-handled-mid",
        createdTs: now - 5 * 3600,
        status: "answered",
        answeredTs: now - 1800,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    __injectMockReplyCard(mkCard({ id: "rc-snz-late", createdTs: now - 30, snoozedUntilTs: now + 7200, task: task("T-6", "high", now - 60) }));
    __injectMockReplyCard(mkCard({ id: "rc-snz-soon", createdTs: now - 5 * 3600, snoozedUntilTs: now + 3600, task: task("T-7", "low") }));
    localStorage.removeItem("oc_replies_sort");
    const first = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toHaveLength(5));
    expect(paneIds("waiting-card")).toEqual(["rc-notask", "rc-low", "rc-mid", "rc-high", "rc-frozen"]);
    fireEvent.click(await first.findByTestId("snoozed-toggle"));
    fireEvent.click(await first.findByTestId("answered-toggle"));
    await waitFor(() => expect(paneIds("answered-card")).toHaveLength(3));
    expect(first.getByTestId("replies-sort-active").textContent).toBe("開卡時間（新到舊）");

    fireEvent.click(first.getByTestId("display-options"));
    const select = first.getByTestId("replies-sort") as HTMLSelectElement;
    expect([...select.options].map((o) => o.textContent)).toEqual([
      "開卡時間（新到舊）",
      "開卡時間（舊到新）",
      "任務優先序（高到低）",
      "任務期限（早到晚）",
    ]);
    const expected: Record<string, string[]> = {
      createdAsc: ["rc-frozen", "rc-high", "rc-mid", "rc-low", "rc-notask"],
      priority: ["rc-high", "rc-mid", "rc-low", "rc-frozen", "rc-notask"],
      due: ["rc-frozen", "rc-low", "rc-mid", "rc-notask", "rc-high"],
      createdDesc: ["rc-notask", "rc-low", "rc-mid", "rc-high", "rc-frozen"],
    };
    for (const [value, order] of Object.entries(expected)) {
      fireEvent.change(select, { target: { value } });
      expect(paneIds("waiting-card")).toEqual(order);
      expect(paneIds("snoozed-card")).toEqual(["rc-snz-soon", "rc-snz-late"]);
      expect(paneIds("answered-card")).toEqual(["rc-handled", "rc-handled-mid", "rc-handled-early"]);
      expect(localStorage.getItem("oc_replies_sort")).toBe(value);
    }
    expect(first.getByTestId("replies-sort-active").textContent).toBe("開卡時間（新到舊）");

    fireEvent.change(select, { target: { value: "priority" } });
    expect(first.getByTestId("replies-sort-active").textContent).toBe("任務優先序（高到低）");
    first.unmount();

    const second = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(expected.priority));
    expect(second.getByTestId("replies-sort-active").textContent).toBe("任務優先序（高到低）");
    localStorage.removeItem("oc_replies_sort");
  });

  it("a card naming a task → one task row at the top, above the head: #number, priority, due date in its tone, title, 查看任務 → #tasks/<id>", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(
      mkCard({
        id: "rc-over",
        summary: "逾期那張",
        createdTs: now - 60,
        task: { id: "T-11", typeKey: "x", title: "修好登入", priority: "high", dueTs: 1793375999 - 30 * 86400 },
      })
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-own-lead",
        createdTs: now - 120,
        task: { id: "T-12", typeKey: "", title: "自己的提醒", priority: "mid", dueTs: now + 5 * 3600, dueSoonLeadMinutes: 600 },
      })
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-short-lead",
        createdTs: now - 180,
        task: { id: "T-13", typeKey: "", title: "提醒較短", priority: "low", dueTs: now + 5 * 3600, dueSoonLeadMinutes: 60 },
      })
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-workspace-lead",
        createdTs: now - 240,
        task: { id: "T-14", typeKey: "", title: "照全站", priority: "frozen", dueTs: now + 5 * 3600 },
      })
    );
    renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toHaveLength(4));
    const rowOf = (id: string) =>
      document.querySelector(`[data-reply-card-id="${id}"] [data-testid="reply-task-ref"]`)!;

    const over = rowOf("rc-over");
    expect(over.className).toBe("reply-card__task reply-card__task--row");
    expect(
      [...over.querySelectorAll(".reply-card__task-chips > *")].map((el) => [
        el.getAttribute("data-testid"),
        el.className,
      ])
    ).toEqual([
      ["reply-task-no", "task-card__id-badge"],
      ["reply-task-priority", "task-badge task-badge--prio-high"],
      ["reply-task-due", "task-card__chip task-card__due-date--overdue"],
    ]);
    expect(over.querySelector('[data-testid="reply-task-no"]')!.textContent).toBe("#T-11");
    expect(over.querySelector('[data-testid="reply-task-priority"]')!.textContent).toBe("高");
    expect(over.querySelector('[data-testid="reply-task-due"]')!.textContent).toMatch(
      /^2026\/9\/30（三）已逾期 \d+ 天$/
    );
    expect(over.querySelector(".reply-card__task-title")!.textContent).toBe("修好登入");
    expect(over.querySelector('[data-testid="reply-task-jump"]')!.textContent).toBe("查看任務");
    expect(over.textContent).not.toContain("x");

    const card = document.querySelector('[data-reply-card-id="rc-over"]')!;
    const head = card.querySelector(".reply-card__head")!;
    const summary = card.querySelector(".reply-card__summary")!;
    expect(card.firstElementChild).toBe(over);
    expect(over.compareDocumentPosition(head) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(head.compareDocumentPosition(summary) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    const tone = (id: string) => rowOf(id).querySelector('[data-testid="reply-task-due"]')!.className;
    expect(tone("rc-own-lead")).toBe("task-card__chip task-card__due-date--soon");
    expect(tone("rc-short-lead")).toBe("task-card__chip");
    expect(tone("rc-workspace-lead")).toBe("task-card__chip task-card__due-date--soon");

    fireEvent.click(over.querySelector('[data-testid="reply-task-jump"]')!);
    expect(window.location.hash).toBe("#tasks/T-11");
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  it("a task row with no priority or due date draws neither chip", async () => {
    __injectMockReplyCard(mkCard({ task: { id: "T-20", typeKey: "", title: "沒排期" } }));
    renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toHaveLength(1));
    const chips = document.querySelector(".reply-card__task-chips")!;
    expect([...chips.children].map((el) => el.getAttribute("data-testid"))).toEqual(["reply-task-no"]);
  });

  it("稍後再看 → four presets each with its return time, then a custom row; a preset moves the card into 已延後 with its time and out of the nav badge", async () => {
    vi.spyOn(Date, "now").mockReturnValue(THU_10AM * 1000);
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張", createdTs: THU_10AM - 600 }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張", createdTs: THU_10AM - 1200 }));
    const snoozeSpy = vi.spyOn(api, "snoozeReplyCard");
    const { getByTestId, findByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1", "rc-2"]));
    expect(getByTestId("nav-badge").textContent).toBe("2");
    expect(queryByTestId("snoozed-section")).toBeNull();

    const card = document.querySelector<HTMLElement>('[data-reply-card-id="rc-2"]')!;
    const heads = [...document.querySelectorAll(".reply-card__head")].map((h) =>
      [...h.querySelectorAll("button")].map((b) => b.textContent)
    );
    expect(heads.map((h) => h.slice(1))).toEqual([
      ["跳到原訊息", "稍後再看", "標為過期"],
      ["跳到原訊息", "稍後再看", "標為過期"],
    ]);
    fireEvent.click(card.querySelector('[data-testid="snooze-card"]')!);
    expect(card.getAttribute("aria-expanded")).toBe("false");
    const menu = await findByTestId("snooze-menu-rc-2");
    expect(
      [...menu.querySelectorAll(".reply-snooze__preset")].map((b) => [...b.children].map((c) => c.textContent))
    ).toEqual([
      ["1 小時後", "2026/11/5（四） 11:00"],
      ["3 小時後", "2026/11/5（四） 13:00"],
      ["1 天後", "2026/11/6（五） 10:00"],
      ["3 天後", "2026/11/8（日） 10:00"],
    ]);

    fireEvent.click(getByTestId("snooze-preset-3h"));
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));
    expect(snoozeSpy).toHaveBeenCalledWith("rc-2", THU_10AM + 3 * 3600);
    expect(queryByTestId("snooze-menu-rc-2")).toBeNull();
    expect(getByTestId("nav-badge").textContent).toBe("1");
    expect(getByTestId("snoozed-toggle").textContent).toBe("已延後 · 1時間到會自動回到請示");
    expect(getByTestId("snoozed-toggle").getAttribute("aria-expanded")).toBe("false");
    expect(paneIds("snoozed-card")).toEqual([]);

    fireEvent.click(getByTestId("snoozed-toggle"));
    expect(paneIds("snoozed-card")).toEqual(["rc-2"]);
    const snoozedCard = document.querySelector('[data-testid="snoozed-card"]')!;
    expect(snoozedCard.querySelector('[data-testid="snoozed-until"]')!.textContent).toBe("2026/11/5（四） 13:00 回來");
    expect(
      [...snoozedCard.querySelectorAll(".reply-card__head button")].slice(1).map((b) => b.textContent)
    ).toEqual(["跳到原訊息", "取消延後", "標為過期"]);
  });

  it("稍後再看 custom row → live preview of the return time, 延後 sends it; past 90 days or not a whole number the button is off and says why", async () => {
    vi.spyOn(Date, "now").mockReturnValue(THU_10AM * 1000);
    __injectMockReplyCard(mkCard({ id: "rc-1", createdTs: THU_10AM - 600 }));
    const snoozeSpy = vi.spyOn(api, "snoozeReplyCard");
    const { getByTestId, findByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));
    fireEvent.click(getByTestId("snooze-card"));
    await findByTestId("snooze-menu-rc-1");
    const amount = getByTestId("snooze-amount");
    const unit = getByTestId("snooze-unit") as HTMLSelectElement;
    const apply = getByTestId("snooze-custom-apply") as HTMLButtonElement;
    const preview = () => getByTestId("snooze-preview").textContent;

    expect([...unit.options].map((o) => o.textContent)).toEqual(["小時後", "天後"]);
    expect([preview(), apply.disabled]).toEqual(["\u00a0", true]);
    fireEvent.change(amount, { target: { value: "5" } });
    expect([preview(), apply.disabled]).toEqual(["2026/11/5（四） 15:00 回來", false]);
    fireEvent.change(unit, { target: { value: "day" } });
    expect([preview(), apply.disabled]).toEqual(["2026/11/10（二） 10:00 回來", false]);
    fireEvent.change(amount, { target: { value: "91" } });
    expect([preview(), apply.disabled]).toEqual(["最多只能延後 90 天", true]);
    fireEvent.change(amount, { target: { value: "0" } });
    expect([preview(), apply.disabled]).toEqual(["請輸入 1 以上的整數", true]);
    fireEvent.change(amount, { target: { value: "90" } });
    expect([preview(), apply.disabled]).toEqual(["2027/2/3（三） 10:00 回來", false]);
    expect(snoozeSpy).not.toHaveBeenCalled();

    fireEvent.click(apply);
    await waitFor(() => expect(paneIds("waiting-card")).toEqual([]));
    expect(snoozeSpy).toHaveBeenCalledWith("rc-1", THU_10AM + 90 * 86400);
  });

  it("取消延後 → the card is back in 待回覆 and the badge at once", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-1", snoozedUntilTs: now + 3600 }));
    const snoozeSpy = vi.spyOn(api, "snoozeReplyCard");
    const { getByTestId, findByTestId } = renderPage();
    fireEvent.click(await findByTestId("snoozed-toggle"));
    expect(paneIds("snoozed-card")).toEqual(["rc-1"]);
    expect(getByTestId("nav-badge").textContent).toBe("0");

    fireEvent.click(getByTestId("unsnooze-card"));
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));
    expect(snoozeSpy).toHaveBeenCalledWith("rc-1", null);
    expect(paneIds("snoozed-card")).toEqual([]);
    expect(document.querySelector('[data-testid="snoozed-section"]')).toBeNull();
    expect(getByTestId("nav-badge").textContent).toBe("1");
  });

  it("a snoozed card can still be opened and answered, or expired, from 已延後", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-a", summary: "延後要回答", snoozedUntilTs: now + 3600 }));
    __injectMockReplyCard(mkCard({ id: "rc-e", summary: "延後要過期", snoozedUntilTs: now + 7200 }));
    const { findByTestId, getByTestId } = renderPage();
    fireEvent.click(await findByTestId("snoozed-toggle"));
    expect(paneIds("snoozed-card")).toEqual(["rc-a", "rc-e"]);

    const answering = document.querySelector<HTMLElement>('[data-reply-card-id="rc-a"]')!;
    fireEvent.click(answering);
    await waitFor(() => expect(answering.querySelectorAll(".reply-option").length).toBe(2));
    fireEvent.click(answering.querySelectorAll(".reply-option")[0]);
    await waitFor(() => expect(paneIds("snoozed-card")).toEqual(["rc-e"]));
    expect((await mockApiCard("rc-a")).status).toBe("answered");

    const expiring = document.querySelector<HTMLElement>('[data-reply-card-id="rc-e"]')!;
    fireEvent.click(expiring.querySelector('[data-testid="expire-card"]')!);
    fireEvent.click(await findByTestId("expire-confirm-btn"));
    await waitFor(() => expect(document.querySelector('[data-testid="snoozed-section"]')).toBeNull());
    expect((await mockApiCard("rc-e")).status).toBe("expired");
    expect(getByTestId("answered-toggle").textContent).toContain("近期已處理 · 2");
  });

  it("a 409 on 稍後再看 → says the card was already handled, closes the menu and re-reads the panes; any other failure keeps the menu open with an error", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const snoozeSpy = vi
      .spyOn(api, "snoozeReplyCard")
      .mockRejectedValueOnce(new Error("offline"))
      .mockRejectedValueOnce(
        new ApiError("http 409 for PUT /api/reply-cards/rc-1/snooze", 409, "conflict", "already answered")
      );
    const listSpy = vi.spyOn(api, "listReplyCards");
    const { getByTestId, findByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));

    fireEvent.click(getByTestId("snooze-card"));
    fireEvent.click(getByTestId("snooze-preset-1h"));
    expect((await findByTestId("snooze-error")).textContent).toBe("延後失敗，請稍後重試");
    expect(queryByTestId("snooze-menu-rc-1")).not.toBeNull();
    expect(queryByTestId("replies-action-error")).toBeNull();
    expect(paneIds("waiting-card")).toEqual(["rc-1"]);

    const before = listSpy.mock.calls.length;
    fireEvent.click(getByTestId("snooze-preset-1h"));
    expect((await findByTestId("replies-action-error")).textContent).toBe(
      "這張卡已經回覆或標為過期，不能再延後"
    );
    expect(queryByTestId("snooze-menu-rc-1")).toBeNull();
    expect(snoozeSpy).toHaveBeenCalledTimes(2);
    await waitFor(() => expect(listSpy.mock.calls.length).toBeGreaterThan(before));
  });

  it("a failed 取消延後 → an error on that card, which stays in 已延後; a 409 instead says the card was already handled and re-reads the panes", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-1", snoozedUntilTs: now + 3600 }));
    vi.spyOn(api, "snoozeReplyCard")
      .mockRejectedValueOnce(new Error("offline"))
      .mockRejectedValueOnce(
        new ApiError("http 409 for PUT /api/reply-cards/rc-1/snooze", 409, "conflict", "already answered")
      );
    const listSpy = vi.spyOn(api, "listReplyCards");
    const { findByTestId, getByTestId, queryByTestId } = renderPage();
    fireEvent.click(await findByTestId("snoozed-toggle"));
    fireEvent.click(getByTestId("unsnooze-card"));
    const err = await findByTestId("unsnooze-error");
    expect(err.textContent).toBe("取消延後失敗，請稍後重試");
    expect(err.closest("[data-reply-card-id]")?.getAttribute("data-reply-card-id")).toBe("rc-1");
    expect(paneIds("snoozed-card")).toEqual(["rc-1"]);
    expect(queryByTestId("replies-action-error")).toBeNull();

    const before = listSpy.mock.calls.length;
    fireEvent.click(getByTestId("unsnooze-card"));
    expect((await findByTestId("replies-action-error")).textContent).toBe(
      "這張卡已經回覆或標為過期，不用再取消延後"
    );
    expect(queryByTestId("unsnooze-error")).toBeNull();
    await waitFor(() => expect(listSpy.mock.calls.length).toBeGreaterThan(before));
  });

  it("on a phone 稍後再看 opens as a bottom sheet; tapping the dimmed backdrop closes it without opening the card", async () => {
    const matchMedia = window.matchMedia;
    window.matchMedia = ((query: string) => ({
      matches: query.includes("max-width: 720px"),
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    })) as unknown as typeof window.matchMedia;
    try {
      __injectMockReplyCard(mkCard({ id: "rc-1" }));
      const { getByTestId, findByTestId, queryByTestId } = renderPage();
      await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));
      fireEvent.click(getByTestId("snooze-card"));
      const sheet = await findByTestId("snooze-menu-rc-1");
      expect(sheet.className).toBe("reply-snooze reply-snooze--sheet");
      expect(sheet.parentElement?.getAttribute("data-testid")).toBe("snooze-backdrop");

      fireEvent.click(getByTestId("snooze-backdrop"));
      expect(queryByTestId("snooze-menu-rc-1")).toBeNull();
      expect(document.querySelector('[data-reply-card-id="rc-1"]')!.getAttribute("aria-expanded")).toBe("false");
    } finally {
      window.matchMedia = matchMedia;
    }
  });
});

async function mockApiCard(id: string) {
  return api.getReplyCard(id);
}
