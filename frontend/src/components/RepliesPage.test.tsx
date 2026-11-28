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
import { render, fireEvent, waitFor, act, within } from "@testing-library/react";
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
  __setMockSuggestedRepliesReplyCard,
  mockApi,
} from "../api/mock";
import { WithSuggestedReplies } from "../test/suggestedReplies";
import {
  toggleFilter,
  openFiltersWhenReady,
  selectRepliesTab,
} from "../test/tasksFilter";
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

async function showHandled() {
  await selectRepliesTab("answered");
}

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

const replyChips = (root: ParentNode) =>
  [...root.querySelectorAll('[data-testid="reply-suggested-replies"] button')].map((b) => b.textContent);

function Badge() {
  return <span data-testid="nav-badge">{useReplyCardWaitingCount()}</span>;
}

function renderPage() {
  return render(
    <I18nProvider>
      <WithSuggestedReplies>
        <ReplyCardsProvider>
          <Badge />
          <RepliesPage />
        </ReplyCardsProvider>
      </WithSuggestedReplies>
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
const originalRangeRects = Range.prototype.getClientRects;

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

/** A press by a finger (or a mouse) held for `ms`. jsdom has no
 * PointerEvent, so it is a MouseEvent carrying the pointerType. */
async function hold(el: Element, ms: number, pointerType = "touch") {
  const press = (type: string) => {
    const e = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: 10, clientY: 10 });
    Object.defineProperty(e, "pointerType", { value: pointerType });
    act(() => {
      el.dispatchEvent(e);
    });
  };
  press("pointerdown");
  await act(() => new Promise((r) => setTimeout(r, ms)));
  press("pointerup");
}

function insideInert(el: Element): boolean {
  for (let at: Element | null = el; at; at = at.parentElement) {
    if ((at as HTMLElement).inert) return true;
  }
  return false;
}

const menuLabels = () =>
  [...document.querySelectorAll("[role='menuitem']")].map((b) => b.textContent);

/** An item of the long-press sheet, chosen by a finger. */
function choose(label: string) {
  const item = [...document.querySelectorAll("[role='menuitem']")].find((b) => b.textContent === label)!;
  const down = new MouseEvent("pointerdown", { bubbles: true });
  act(() => {
    item.dispatchEvent(down);
  });
  fireEvent.click(item, { detail: 1 });
}

let clipboard: string[] = [];

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  clipboard = [];
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: {
      writeText: (text: string) => {
        clipboard.push(text);
        return Promise.resolve();
      },
    },
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  Range.prototype.getBoundingClientRect = originalRangeRect;
  Range.prototype.getClientRects = originalRangeRects;
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
    // Initiator identity resolves through the roster (mock Mira), name only.
    expect(cards[2].querySelector('[data-testid="reply-card-opener"]')?.textContent).toBe("Mira");
    // 已等你 {t} computed from createdTs.
    expect(cards[2].querySelector('[data-testid="waited-pill"]')?.getAttribute("title")).toBe(
      "已等你 3 小時"
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
      expect(card.textContent).not.toMatch(/\d{1,2}\/\d{1,2}/);
    }
  });

  it("resolves an outsource asker to its bare 代號, not the raw ow- id", async () => {
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
      (c) => c.querySelector(".card-meta__chip-name")?.textContent
    );
    expect(names).toContain("R-2");
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
        ?.querySelector(".card-meta__chip-name")?.textContent;
    expect(nameOf("離職正職的請示")).toBe("阿哲");
    expect(nameOf("查無正職的請示")).toBe("m-unknown");

    await openFiltersWhenReady();
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
        expect(left.querySelector(".card-meta__chip-name")?.textContent).toBe("阿哲"),
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
        expect(card.querySelector(".card-meta__chip-name")?.textContent).toBe("阿哲"),
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
        ["rc-rel-old", "ow-rel", "R-2"],
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
        expect(card.querySelector(".card-meta__chip-name")?.textContent).toBe(expected);
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

    // Switching to 近期已回覆 is not answering anything: the ONLY click aimed
    // at the card itself was the one above.
    await showHandled();
    const answeredCard = await findByTestId("answered-card");
    await openCards();
    // The ask is GONE from 待回覆 and standing in 近期已回覆 — the owner's own
    // test of "did my tap take?".
    await waitFor(() => expect(queryAllByTestId("waiting-card")).toHaveLength(0));
    const final = answeredCard.querySelector('[data-testid="final-answer"]');
    // The circled option IS the ai_pick one → the AI 建議 tag rides alongside.
    expect(final?.textContent).toBe("你選的AI 建議先不要");
  });

  it("a waiting card's answer box offers its task type's sentences, then its author's, then the reply-card ones, each once", async () => {
    await mockApi.putTaskManualSuggestedReplies("builtin-role-design", ["照角色手冊", "收到，照這樣做"]);
    await mockApi.putChatSuggestedReplies("mira", ["Mira 專屬", "收到，照這樣做"]);
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做", "先擱著，這週不碰"]);
    __injectMockReplyCard(mkCard({ task: { id: "t-1", typeKey: "builtin-role-design", title: "設計角色", priority: "", dueTs: null, dueSoonLeadMinutes: null } }));
    const { findByTestId } = renderPage();
    const card = await findByTestId("waiting-card");
    await openCards();
    await waitFor(() =>
      expect(replyChips(card)).toEqual(["照角色手冊", "收到，照這樣做", "Mira 專屬", "先擱著，這週不碰"])
    );
  });

  it("重新決定's answer box offers the task type's sentences, then the card author's, then the reply-card ones", async () => {
    await mockApi.putTaskManualSuggestedReplies("builtin-role-design", ["照角色手冊"]);
    await mockApi.putChatSuggestedReplies("mira", ["Mira 專屬"]);
    __setMockSuggestedRepliesReplyCard(["先擱著，這週不碰"]);
    __injectMockReplyCard(
      mkCard({
        task: { id: "t-1", typeKey: "builtin-role-design", title: "設計角色", priority: "", dueTs: null, dueSoonLeadMinutes: null },
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    const { findByTestId, getByText } = renderPage();
    await showHandled();
    const card = await findByTestId("answered-card");
    await openCards();
    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    await waitFor(() => expect(replyChips(card)).toEqual(["照角色手冊", "Mira 專屬", "先擱著，這週不碰"]));
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
    await showHandled();
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

    await showHandled();
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

    await showHandled();
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
    await openCards();

    await waitFor(() => expect(workersSpy).toHaveBeenCalledTimes(1));
    const input = getAllByPlaceholderText("輸入回覆…")[0];
    await waitFor(() => {
      fireEvent.change(input, { target: { value: "@492" } });
      expect(document.querySelector('[role="listbox"]')?.textContent).toContain("O-7");
    });
    await showHandled();
    const answeredCard = await findByTestId("answered-card");
    await openCards();
    const final = answeredCard.querySelector('[data-testid="final-answer"]')!;
    fireEvent.click(final.querySelector("button.md-mention")!);
    expect(window.location.hash).toBe("#office/worker/ow-7/from/replies/rc-done");
    expect(workersSpy).toHaveBeenCalledTimes(1);
  });

  it("opens on 等待回覆 with the selected label's count on the line under the toolbar and no number on any label, lists only that label's cards (handled ones with their status icon and 處理時間 under 開卡人), and fetches the handled lists only once 近期已回覆 is selected", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-wait", createdTs: now - 60 }));
    __injectMockReplyCard(mkCard({ id: "rc-later", createdTs: now - 30, snoozedUntilTs: now + 3600 }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-done",
        status: "answered",
        answeredTs: now - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    __injectMockReplyCard(mkCard({ id: "rc-gone", status: "expired", expiredTs: now - 30 }));
    const listSpy = vi.spyOn(api, "listReplyCards");
    vi.spyOn(api, "getReplyCardCount").mockResolvedValue({ waiting: 1, answered: 4, expired: 3 });
    const { findAllByTestId, getByTestId, queryByTestId } = renderPage();
    await findAllByTestId("waiting-card");

    const shown = () =>
      [...document.querySelectorAll<HTMLElement>("[data-reply-card-id][aria-expanded]")].map(
        (el) => [
          el.dataset.replyCardId,
          el.querySelector('[data-testid="reply-status-icon"]')?.getAttribute("data-state"),
          el.querySelector(".reply-card__handled-at .card-meta__label")?.textContent ?? null,
        ]
      );
    const tab = (v: string) => getByTestId(`replies-tab-${v}`);
    const label = (v: string) => tab(v).querySelector<HTMLElement>(".replies__tab-label")!.title;
    const line = () => getByTestId("replies-count-line").textContent;
    expect(tab("waiting").getAttribute("aria-selected")).toBe("true");
    expect([label("waiting"), label("snoozed"), label("answered")]).toEqual(["等待回覆", "稍後再看", "近期已回覆"]);
    for (const v of ["waiting", "snoozed", "answered"]) {
      expect(tab(v).textContent, "the labels carry no count").not.toMatch(/\d/);
    }
    expect(line()).toBe("1 張請示卡");
    expect(shown()).toEqual([["rc-wait", "waiting", null]]);
    expect(queryByTestId("replies-answered-window")).toBeNull();
    expect(listSpy.mock.calls.map(([status]) => status)).not.toContain("answered");
    expect(listSpy.mock.calls.map(([status]) => status)).not.toContain("expired");

    await selectRepliesTab("snoozed");
    expect(tab("snoozed").getAttribute("aria-selected")).toBe("true");
    expect(tab("waiting").getAttribute("aria-selected")).toBe("false");
    expect(shown()).toEqual([["rc-later", "snoozed", null]]);
    expect(line()).toBe("1 張請示卡");
    expect(listSpy.mock.calls.map(([status]) => status)).not.toContain("answered");

    await selectRepliesTab("answered");
    expect(line(), "the count read's answered + expired, before the list is loaded").toBe("7 張請示卡 · 近 24 小時");
    await waitFor(() =>
      expect(shown()).toEqual([
        ["rc-gone", "expired", "處理時間"],
        ["rc-done", "answered", "處理時間"],
      ])
    );
    expect(line(), "the loaded list's length from then on").toBe("2 張請示卡 · 近 24 小時");
    expect(getByTestId("replies-answered-window").textContent).toBe("近 24 小時");
  });

  it("a count not known yet is left off the line under the toolbar, which stays in place: while the first read is out, and 近期已回覆's under a filter until its list is loaded", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-wait", task: { id: "T-1", typeKey: "", title: "t", priority: "high" } }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-done",
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    let releaseCount!: () => void;
    const realCount = api.getReplyCardCount.bind(api);
    vi.spyOn(api, "getReplyCardCount").mockImplementationOnce(
      () => new Promise((resolve) => (releaseCount = () => resolve(realCount())))
    );
    const { findAllByTestId, getByTestId, queryByTestId } = renderPage();
    const line = () => getByTestId("replies-count-line").textContent;
    await waitFor(() => expect(getByTestId("replies-count-line")).toBeTruthy());
    expect(line()).toBe("");
    expect(queryByTestId("replies-count")).toBeNull();

    await act(async () => releaseCount());
    await findAllByTestId("waiting-card");
    expect(line()).toBe("1 張請示卡");
    await selectRepliesTab("snoozed");
    expect(line()).toBe("0 張請示卡");
    await selectRepliesTab("waiting");

    toggleFilter("filter-priority", "high");
    expect(line()).toBe("1 張請示卡");

    await selectRepliesTab("answered");
    expect(line(), "the count read knows nothing of the filter").toBe("近 24 小時");
    await waitFor(() => expect(line()).toBe("0 張請示卡 · 近 24 小時"));
  });

  it("the labels are one tab stop; arrow keys, Home and End move the selection and the focus", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-wait" }));
    const { findAllByTestId, getByTestId, getByRole } = renderPage();
    await findAllByTestId("waiting-card");
    const tablist = getByRole("tablist", { name: "請示卡" });
    const tab = (v: string) => getByTestId(`replies-tab-${v}`);
    expect([...tablist.querySelectorAll('[role="tab"]')].map((el) => el.getAttribute("tabindex"))).toEqual([
      "0",
      "-1",
      "-1",
    ]);
    const panel = getByRole("tabpanel");
    expect(panel.getAttribute("aria-labelledby")).toBe(tab("waiting").id);

    tab("waiting").focus();
    fireEvent.keyDown(tab("waiting"), { key: "ArrowRight" });
    expect(tab("snoozed").getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(tab("snoozed"));
    expect(tab("snoozed").getAttribute("tabindex")).toBe("0");
    expect(panel.getAttribute("aria-labelledby")).toBe(tab("snoozed").id);

    fireEvent.keyDown(tab("snoozed"), { key: "End" });
    expect(document.activeElement).toBe(tab("answered"));
    fireEvent.keyDown(tab("answered"), { key: "ArrowRight" });
    expect(document.activeElement, "wraps to the first").toBe(tab("waiting"));
    fireEvent.keyDown(tab("waiting"), { key: "ArrowLeft" });
    expect(tab("answered").getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(tab("answered"), { key: "Home" });
    expect(tab("waiting").getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(tab("waiting"));
  });

  it("each label has its own empty sentence; a filter that empties any of them says so instead, and 清除篩選 keeps the selected label", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-wait" }));
    const { findAllByTestId, getByTestId, queryByTestId } = renderPage();
    await findAllByTestId("waiting-card");

    await selectRepliesTab("snoozed");
    expect(getByTestId("replies-empty").textContent).toBe("沒有稍後再看的請示卡");
    await selectRepliesTab("answered");
    await waitFor(() =>
      expect(getByTestId("replies-empty").textContent).toBe("最近 24 小時沒有已回覆的請示卡")
    );

    toggleFilter("filter-priority", "high");
    expect(getByTestId("replies-filter-count").textContent).toBe("1");
    for (const v of ["waiting", "snoozed", "answered"] as const) {
      await selectRepliesTab(v);
      expect(getByTestId("replies-empty").textContent).toBe("沒有符合篩選條件的請示");
      expect(getByTestId("replies-count").textContent).toBe("0 張請示卡");
    }

    fireEvent.click(getByTestId("replies-filter-clear"));
    expect(queryByTestId("replies-filter-count")).toBeNull();
    expect(getByTestId("replies-tab-answered").getAttribute("aria-selected")).toBe("true");
    await selectRepliesTab("waiting");
    expect(getByTestId("replies-count").textContent).toBe("1 張請示卡");
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
    await showHandled();
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
    await showHandled();
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
    await showHandled();
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
    const { findAllByTestId, getByRole } = renderPage();
    await findAllByTestId("waiting-card");
    await openCards();

    fireEvent.click(getByRole("button", { name: "跳到原訊息" }));
    expect(window.location.hash).toBe("#office/chat/mira/msg/msg-1");
  });

  it("clicking the 開卡人 chip opens that member's chat (#office/chat/<id>)", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    fireEvent.click(card.querySelector('[data-testid="reply-card-opener"]')!);
    expect(window.location.hash).toBe("#office/chat/mira");
  });

  it("clicking an outsource asker's 開卡人 chip opens the worker's chat, released or not", async () => {
    __injectMockMember({ id: "ow-rel", kind: "outsource", name: "R-2" });
    __injectMockReplyCard(mkCard({ id: "rc-ow", from: "ow-rel" }));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    expect(card.querySelector('[data-testid="reply-card-opener"] img')?.getAttribute("src")).toBe(
      "/api/chat/attachment/ava-worker",
    );
    fireEvent.click(card.querySelector('[data-testid="reply-card-opener"]')!);
    expect(window.location.hash).toBe("#office/chat/ow-rel");
  });

  it("the 開卡人 chip is the task card's chat chip: a button named 傳訊息給開卡人 with the asker's name and the chat bubble after it", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    const opener = card.querySelector('[data-testid="reply-card-opener"]')!;
    expect(opener.tagName).toBe("BUTTON");
    expect([opener.className, opener.getAttribute("aria-label"), opener.textContent]).toEqual([
      "card-meta__chip card-meta__chip--link",
      "傳訊息給開卡人",
      "Mira",
    ]);
    expect(opener.lastElementChild!.tagName).toBe("svg");
  });

  it("標為過期 double-confirms, closes the card without an answer, and lists it as 已過期 under 近期已回覆", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findByTestId, findAllByTestId, queryAllByTestId, queryByTestId } =
      renderPage();
    const [card] = await findAllByTestId("waiting-card");
    await openCards();

    // The head's 標為過期 opens the confirm modal; cancel keeps the card.
    fireEvent.click(within(card).getByRole("button", { name: "標為過期" }));
    const modal = await findByTestId("expire-confirm");
    expect(modal.textContent).toContain("要幫你寄出這封信嗎？");
    fireEvent.click(modal.querySelector(".confirm-modal__btn")!); // cancel (first button)
    await waitFor(() =>
      expect(queryByTestId("expire-confirm")).toBeNull()
    );
    expect(queryAllByTestId("waiting-card")).toHaveLength(1);

    // Confirm actually expires: the card leaves 待回覆 and shows 已過期 (grey
    // terminal — no chips to pick, no 重新決定) under 近期已回覆.
    fireEvent.click(card.querySelector('[data-testid="expire-card"]')!);
    fireEvent.click(await findByTestId("expire-confirm-btn"));
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(0)
    );
    await showHandled();
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
    await showHandled();
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

  // Judged by DOM order, not by geometry: a rendered-later row can still be
  // painted high by CSS, and then the reading order screen readers and
  // keyboards follow would disagree with the picture.
  it("leads every card with its first line — the state icon, its own id, the chevron last — then the task box, the title and 開卡人, on waiting, snoozed and handled cards", async () => {
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
    for (const [view, testId, state, id] of [
      ["waiting", "waiting-card", "waiting", "rc-w"],
      ["snoozed", "snoozed-card", "snoozed", "rc-s"],
      ["answered", "answered-card", "answered", "rc-a"],
    ] as const) {
      await selectRepliesTab(view);
      const card = await findByTestId(testId);
      const ref = card.querySelector('[data-testid="reply-task-ref"]')!;
      const head = card.querySelector(".reply-card__head")!;
      const summary = card.querySelector(".reply-card__summary")!;
      const meta = card.querySelector(".card-meta")!;
      expect(card.firstElementChild).toBe(head);
      expect(head.nextElementSibling).toBe(ref);
      expect(
        ref.compareDocumentPosition(summary) & Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy();
      expect(summary.nextElementSibling).toBe(meta);
      const status = head.firstElementChild as HTMLElement;
      expect([status.dataset.testid, status.dataset.state]).toEqual(["reply-status-icon", state]);
      expect(status.nextElementSibling?.getAttribute("data-testid")).toBe("reply-card-id");
      expect(status.nextElementSibling?.textContent).toBe(`#${id}`);
      expect(head.querySelector(".reply-card__head-end")?.lastElementChild?.className).toBe("reply-card__fold");
    }
  });

  it("the card's id chip shows six characters after rc- behind the copy icon, and a click copies the whole id without opening the card", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-83935a1b2c3d" }));
    renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-83935a1b2c3d"]));
    const card = document.querySelector<HTMLElement>('[data-reply-card-id="rc-83935a1b2c3d"]')!;
    const chip = card.querySelector<HTMLElement>('[data-testid="reply-card-id"]')!;
    expect([chip.textContent, chip.getAttribute("aria-label"), chip.title]).toEqual([
      "#rc-83935a",
      "複製完整請示卡 ID rc-83935a1b2c3d",
      "複製完整請示卡 ID rc-83935a1b2c3d",
    ]);
    await act(async () => {
      fireEvent.click(chip);
    });
    await waitFor(() => expect(clipboard).toEqual(["rc-83935a1b2c3d"]));
    expect(chip.textContent).toBe("#rc-83935a已複製");
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  // An empty title leaves the chip alone on the row — the cell is not drawn at
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
    expect(ref.textContent).toBe("#t-3");
    expect(within(ref).getByRole("button", { name: "查看任務" })).toBeTruthy();
  });

  it("the desktop pill shows the wait in ONE unit — 1m, 11m, 23h, 3d — with the full wait as its title and accessible name; a snoozed card's pill shows its return date as MM/DD, the full return time as title and name", async () => {
    vi.spyOn(Date, "now").mockReturnValue(THU_10AM * 1000);
    for (const [id, ago] of [
      ["rc-1m", 70],
      ["rc-11m", 11 * 60 + 30],
      ["rc-23h", 23 * 3600 + 59 * 60],
      ["rc-3d", 3 * 86400 + 4 * 3600 + 5 * 60],
    ] as const) {
      __injectMockReplyCard(mkCard({ id, createdTs: THU_10AM - ago }));
    }
    __injectMockReplyCard(
      mkCard({ id: "rc-snz", createdTs: THU_10AM - 60, snoozedUntilTs: THU_10AM + 86400 + 4.5 * 3600 }),
    );
    renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1m", "rc-11m", "rc-23h", "rc-3d"]));
    const pillOf = (id: string) =>
      document.querySelector<HTMLElement>(`[data-reply-card-id="${id}"] .reply-card__head .reply-card__waited-pill`)!;
    const expected: [string, string, string][] = [
      ["rc-1m", "1m", "已等你 1 分"],
      ["rc-11m", "11m", "已等你 11 分"],
      ["rc-23h", "23h", "已等你 23 小時 59 分"],
      ["rc-3d", "3d", "已等你 3 天 4 小時 5 分"],
    ];
    for (const [id, short, full] of expected) {
      const pill = pillOf(id);
      expect(pill.getAttribute("data-testid")).toBe("waited-pill");
      expect(pill.textContent, id).toBe(short);
      expect(pill.getAttribute("aria-label")).toBe(full);
      expect(pill.getAttribute("title")).toBe(full);
      expect(pill.querySelector('svg path[d="M5 22h14"]'), "an hourglass").not.toBeNull();
    }

    await selectRepliesTab("snoozed");
    const snoozed = pillOf("rc-snz");
    expect(snoozed.getAttribute("data-testid")).toBe("snoozed-pill");
    expect(snoozed.textContent).toBe("11/06");
    expect(snoozed.querySelector('svg polyline[points="12 7 12 12 15 14"]'), "the 稍後再看 clock").not.toBeNull();
    expect(snoozed.querySelector('svg path[d="M3 3v5h5"]'), "not the 取消延後 glyph").toBeNull();
    expect(snoozed.getAttribute("aria-label")).toBe("2026/11/6（五） 14:30 回來");
    expect(snoozed.getAttribute("title")).toBe("2026/11/6（五） 14:30 回來");
  });

  it("starts every card collapsed — first line, task box, title and 開卡人 only, no question and no options", async () => {
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

    // What the collapsed card DOES show is the same card without its question
    // (owner:「折疊起來那一列的內容不要另外設計」): the first line with its
    // actions and the ticking counter, the task box, and who asked.
    const head = cards[0].querySelector(".reply-card__head")!;
    expect(cards[0].querySelector('[data-testid="reply-card-opener"]')?.textContent).toBe("Mira");
    expect(within(head as HTMLElement).getByRole("button", { name: "跳到原訊息" })).toBeTruthy();
    expect(within(head as HTMLElement).getByRole("button", { name: "標為過期" })).toBeTruthy();
    expect(head.querySelector('[data-testid="waited"]')).toBeTruthy();
    expect(cards[0].querySelector(".reply-card__task-title")?.textContent).toBe(
      "同步設定"
    );
  });

  it("an answer read from the server stays where it was opened until the label changes, and under 近期已回覆 after 24h even when that list cannot load", async () => {
    const staleTs = Date.now() / 1000 - 25 * 3600;
    const serverCard = mkCard({
      id: "rc-1",
      body: "寄出後無法撤回",
      createdTs: staleTs,
    });
    __injectMockReplyCard(serverCard);
    const listSpy = failHandledListReads();
    const { findAllByTestId, findByTestId, queryAllByTestId, getByTestId } = renderPage();
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
      answered.querySelector(".reply-card__handled-at")?.textContent
    ).toMatch(/^處理時間 (?:\d{4}\/)?\d{1,2}\/\d{1,2} \d{2}:\d{2}$/);
    expect(getByTestId("replies-tab-waiting").getAttribute("aria-selected")).toBe("true");
    expect(getByTestId("replies-count").textContent).toBe("0 張請示卡");
    await selectRepliesTab("answered");
    expect(await findByTestId("handled-load-error")).toBeTruthy();
    expect(document.querySelector('[data-testid="answered-card"]')).toBe(answered);
    await selectRepliesTab("waiting");
    expect(document.querySelector('[data-testid="answered-card"]'), "gone once the label changed").toBeNull();
    expect(listSpy).toHaveBeenCalledWith("answered");
    expect(listSpy).toHaveBeenCalledWith("expired");
    expect(
      listSpy.mock.calls.filter(([status]) => status === "waiting")
    ).toHaveLength(2);
  });

  it("when 近期已回覆 cannot load it shows only the error, and an empty 稍後再看 still says it is empty; an expiry read from the server stays where it was opened until the label changes, and under 近期已回覆 after 24h", async () => {
    const staleTs = Date.now() / 1000 - 25 * 3600;
    const serverCard = mkCard({ id: "rc-expired", body: "寄出後無法撤回", createdTs: staleTs });
    __injectMockReplyCard(serverCard);
    const listSpy = failHandledListReads();
    const { findAllByTestId, findByTestId, queryAllByTestId, getByTestId } = renderPage();
    await findAllByTestId("waiting-card");

    await selectRepliesTab("answered");
    expect(await findByTestId("handled-load-error")).toBeTruthy();
    expect(
      document.querySelector('[data-testid="replies-empty"]'),
      "no 「沒有已回覆」 claim about a list that never loaded"
    ).toBeNull();
    await selectRepliesTab("snoozed");
    expect(getByTestId("replies-empty").textContent).toBe("沒有稍後再看的請示卡");
    expect(document.querySelector('[data-testid="handled-load-error"]')).toBeNull();
    await selectRepliesTab("waiting");
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
      expired.querySelector(".reply-card__handled-at")?.textContent
    ).toMatch(/^處理時間 (?:\d{4}\/)?\d{1,2}\/\d{1,2} \d{2}:\d{2}$/);
    expect(getByTestId("replies-tab-waiting").getAttribute("aria-selected")).toBe("true");
    expect(getByTestId("replies-count").textContent).toBe("0 張請示卡");
    await selectRepliesTab("answered");
    expect(await findByTestId("handled-load-error")).toBeTruthy();
    expect(document.querySelector('[data-testid="expired-card"]')).toBe(expired);
    await selectRepliesTab("waiting");
    expect(document.querySelector('[data-testid="expired-card"]'), "gone once the label changed").toBeNull();
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

  it("顯示方式 › 排序依據 → each order re-sorts the 等待回覆 cards, and any order but the default dots the 顯示方式 button instead of being named beside the labels; 稍後再看 stays by return time and 近期已回覆 by handled time; the panel closes once a sort is picked, and the choice survives a remount", async () => {
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
    const trigger = first.getByTestId("display-options");
    expect(first.queryByTestId("display-options-changed")).toBeNull();
    expect(trigger.getAttribute("aria-label")).toBeNull();
    expect(document.querySelector(".replies__head")!.textContent).not.toContain("開卡時間");

    fireEvent.click(trigger);
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
    const chooseSort = (value: string) => {
      if (!first.queryByTestId("replies-sort")) fireEvent.click(first.getByTestId("display-options"));
      fireEvent.change(first.getByTestId("replies-sort"), { target: { value } });
    };
    for (const [value, order] of Object.entries(expected)) {
      chooseSort(value);
      expect(first.queryByTestId("display-options-panel")).toBeNull();
      expect(paneIds("waiting-card")).toEqual(order);
      expect(localStorage.getItem("oc_replies_sort")).toBe(value);
    }
    expect(first.queryByTestId("display-options-changed")).toBeNull();

    chooseSort("priority");
    expect(first.getByTestId("display-options-changed")).toBeTruthy();
    expect(trigger.getAttribute("aria-label")).toBe("顯示方式：已改為任務優先序（高到低）");
    expect(document.querySelector(".replies__head")!.textContent).not.toContain("任務優先序");
    await selectRepliesTab("snoozed");
    expect(paneIds("snoozed-card")).toEqual(["rc-snz-soon", "rc-snz-late"]);
    await selectRepliesTab("answered");
    await waitFor(() =>
      expect(paneIds("answered-card")).toEqual(["rc-handled", "rc-handled-mid", "rc-handled-early"])
    );
    first.unmount();

    const second = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(expected.priority));
    expect(second.getByTestId("display-options").getAttribute("aria-label")).toBe(
      "顯示方式：已改為任務優先序（高到低）"
    );
    localStorage.removeItem("oc_replies_sort");
  });

  it("a card naming a task → a box right under the first line: the number chip led by the jump icon (查看任務 → #tasks/<id>, the box's only button) and priority, then the title, then the due date written M/D in its tone", async () => {
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
    expect(over.className).toBe("reply-card__task");
    expect([...over.children].map((el) => el.className)).toEqual([
      "reply-card__task-line",
      "reply-card__task-title",
      "reply-card__task-due",
    ]);
    expect(
      [...over.querySelector(".reply-card__task-line")!.children].map((el) => [
        el.getAttribute("data-testid"),
        el.className,
      ])
    ).toEqual([
      ["reply-task-no", "outsource-row__chip outsource-row__chip--task"],
      ["reply-task-priority", "task-badge task-badge--prio-high"],
    ]);
    expect(over.querySelector('[data-testid="reply-task-no"]')!.textContent).toBe("#T-11");
    expect(
      over.querySelector('[data-testid="reply-task-no"] > svg polyline')?.getAttribute("points"),
      "the jump icon leads the number",
    ).toBe("15 3 21 3 21 9");
    expect(over.querySelector('[data-testid="reply-task-priority"]')!.textContent).toBe("高");
    expect(over.querySelector(".reply-card__task-title")!.textContent).toBe("修好登入");
    const due = over.querySelector<HTMLElement>('[data-testid="reply-task-due"]')!;
    const date = new Date().getFullYear() === 2026 ? "9/30" : "2026/9/30";
    const shown = [...due.childNodes].filter((n) => n.nodeType === Node.TEXT_NODE).map((n) => n.textContent).join("");
    expect(shown).toBe(`到期 ${date}`);
    expect(due.getAttribute("title")).toBe(`到期 ${date}`);
    expect(due.textContent).toBe(`到期 ${date}已逾期`);
    expect(over.querySelector('[data-testid="reply-task-due"]')!.className).toBe(
      "card-meta__chip task-card__due-chip task-card__due-date--overdue"
    );
    const jump = over.querySelector<HTMLElement>('[data-testid="reply-task-no"]')!;
    expect(within(over as HTMLElement).getAllByRole("button")).toEqual([jump]);
    expect(within(over as HTMLElement).getByRole("button", { name: "查看任務" })).toBe(jump);
    expect(jump.getAttribute("title")).toBe("查看任務");
    expect(jump.getAttribute("aria-label")).toBe("查看任務");
    expect(over.textContent).not.toContain("x");

    const card = document.querySelector('[data-reply-card-id="rc-over"]')!;
    expect(card.querySelector(".reply-card__head")!.nextElementSibling).toBe(over);

    const tone = (id: string) => rowOf(id).querySelector('[data-testid="reply-task-due"]')!.className;
    expect(tone("rc-own-lead")).toBe(
      "card-meta__chip task-card__due-chip task-card__due-date--soon"
    );
    expect(tone("rc-short-lead")).toBe("card-meta__chip task-card__due-chip");
    expect(tone("rc-workspace-lead")).toBe(
      "card-meta__chip task-card__due-chip task-card__due-date--soon"
    );

    fireEvent.click(jump);
    expect(window.location.hash).toBe("#tasks/T-11");
    expect(card.getAttribute("aria-expanded")).toBe("false");

    window.location.hash = "#replies";
    fireEvent.click(over.querySelector(".reply-card__task-title")!);
    fireEvent.click(over);
    expect(window.location.hash).toBe("#replies");
  });

  it("a task box with no priority or due date draws neither, and a card with no task draws no box", async () => {
    __injectMockReplyCard(mkCard({ task: { id: "T-20", typeKey: "", title: "沒排期" } }));
    __injectMockReplyCard(mkCard({ id: "rc-none", createdTs: Date.now() / 1000 - 7200, task: null }));
    renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toHaveLength(2));
    const box = document.querySelector('[data-reply-card-id="rc-1"] [data-testid="reply-task-ref"]')!;
    expect([...box.children].map((el) => el.className)).toEqual(["reply-card__task-line", "reply-card__task-title"]);
    expect(
      [...box.querySelector(".reply-card__task-line")!.children].map((el) => el.getAttribute("data-testid"))
    ).toEqual(["reply-task-no"]);
    expect(document.querySelector('[data-reply-card-id="rc-none"] [data-testid="reply-task-ref"]')).toBeNull();
  });

  it("稍後再看 → four presets each with its return time, then a custom row; a preset moves the card to 稍後再看 with its time and out of the nav badge", async () => {
    vi.spyOn(Date, "now").mockReturnValue(THU_10AM * 1000);
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張", createdTs: THU_10AM - 600 }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張", createdTs: THU_10AM - 1200 }));
    const snoozeSpy = vi.spyOn(api, "snoozeReplyCard");
    const { getByTestId, findByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1", "rc-2"]));
    expect(getByTestId("nav-badge").textContent).toBe("2");
    expect(paneIds("snoozed-card")).toEqual([]);

    const card = document.querySelector<HTMLElement>('[data-reply-card-id="rc-2"]')!;
    const heads = [...document.querySelectorAll(".reply-card__head")].map((h) =>
      [...h.querySelectorAll("button")].map((b) => b.textContent || b.getAttribute("aria-label"))
    );
    expect(heads).toEqual([
      ["#rc-1", "跳到原訊息", "稍後再看", "標為過期", "複製整張卡片內容"],
      ["#rc-2", "跳到原訊息", "稍後再看", "標為過期", "複製整張卡片內容"],
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
    expect(getByTestId("replies-count").textContent).toBe("1 張請示卡");
    await selectRepliesTab("snoozed");
    expect(paneIds("snoozed-card")).toEqual(["rc-2"]);
    expect(getByTestId("replies-count").textContent).toBe("1 張請示卡");
    const snoozedCard = document.querySelector('[data-testid="snoozed-card"]')!;
    expect(snoozedCard.querySelector('[data-testid="snoozed-pill"]')!.getAttribute("title")).toBe("2026/11/5（四） 13:00 回來");
    expect(
      [...snoozedCard.querySelectorAll(".reply-card__head button")]
        .slice(1)
        .map((b) => b.textContent || b.getAttribute("aria-label"))
    ).toEqual(["跳到原訊息", "取消延後", "標為過期", "複製整張卡片內容"]);
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

  it("取消延後 → the card is back in 等待回覆 and in the badge at once, even while it is open; 稍後再看 on an open card moves it back just as fast", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-1", snoozedUntilTs: now + 3600 }));
    const snoozeSpy = vi.spyOn(api, "snoozeReplyCard");
    const { getByTestId, findByTestId } = renderPage();
    await selectRepliesTab("snoozed");
    await waitFor(() => expect(paneIds("snoozed-card")).toEqual(["rc-1"]));
    expect(getByTestId("nav-badge").textContent).toBe("0");

    const openCard = async () => {
      const card = document.querySelector<HTMLElement>('[data-reply-card-id="rc-1"]')!;
      if (card.getAttribute("aria-expanded") === "true") fireEvent.click(card);
      fireEvent.click(card);
      await waitFor(() => expect(card.querySelectorAll(".reply-option").length).toBe(2));
    };
    const shown = () => document.querySelector('[data-reply-card-id="rc-1"]');
    await openCard();
    fireEvent.click(getByTestId("unsnooze-card"));
    await waitFor(() => expect(shown(), "an open card leaves 稍後再看").toBeNull());
    expect(snoozeSpy).toHaveBeenCalledWith("rc-1", null);
    expect(getByTestId("nav-badge").textContent).toBe("1");
    expect(getByTestId("replies-count").textContent).toBe("0 張請示卡");
    await selectRepliesTab("waiting");
    expect(paneIds("waiting-card")).toEqual(["rc-1"]);
    expect(getByTestId("replies-count").textContent).toBe("1 張請示卡");

    await openCard();
    fireEvent.click(getByTestId("snooze-card"));
    await findByTestId("snooze-menu-rc-1");
    fireEvent.click(getByTestId("snooze-preset-3h"));
    await waitFor(() => expect(shown(), "an open card leaves 等待回覆").toBeNull());
    expect(getByTestId("replies-count").textContent).toBe("0 張請示卡");
    await selectRepliesTab("snoozed");
    expect(paneIds("snoozed-card")).toEqual(["rc-1"]);
    expect(getByTestId("replies-count").textContent).toBe("1 張請示卡");
  });

  it("a snoozed card can still be opened and answered, or expired, from 稍後再看", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-a", summary: "延後要回答", snoozedUntilTs: now + 3600 }));
    __injectMockReplyCard(mkCard({ id: "rc-e", summary: "延後要過期", snoozedUntilTs: now + 7200 }));
    const { findByTestId } = renderPage();
    await selectRepliesTab("snoozed");
    await waitFor(() => expect(paneIds("snoozed-card")).toEqual(["rc-a", "rc-e"]));

    const answering = document.querySelector<HTMLElement>('[data-reply-card-id="rc-a"]')!;
    fireEvent.click(answering);
    await waitFor(() => expect(answering.querySelectorAll(".reply-option").length).toBe(2));
    fireEvent.click(answering.querySelectorAll(".reply-option")[0]);
    await waitFor(() => expect(paneIds("snoozed-card")).toEqual(["rc-e"]));
    expect((await mockApiCard("rc-a")).status).toBe("answered");

    const expiring = document.querySelector<HTMLElement>('[data-reply-card-id="rc-e"]')!;
    fireEvent.click(expiring.querySelector('[data-testid="expire-card"]')!);
    fireEvent.click(await findByTestId("expire-confirm-btn"));
    await waitFor(() => expect(paneIds("snoozed-card")).toEqual([]));
    expect((await mockApiCard("rc-e")).status).toBe("expired");
    await showHandled();
    await waitFor(() => expect(paneIds("answered-card")).toEqual(["rc-a"]));
    expect(document.querySelector('[data-testid="expired-card"]')?.getAttribute("data-reply-card-id")).toBe("rc-e");
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

  it("a failed 取消延後 → an error on that card, which stays in 稍後再看; a 409 instead says the card was already handled and re-reads the panes", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-1", snoozedUntilTs: now + 3600 }));
    vi.spyOn(api, "snoozeReplyCard")
      .mockRejectedValueOnce(new Error("offline"))
      .mockRejectedValueOnce(
        new ApiError("http 409 for PUT /api/reply-cards/rc-1/snooze", 409, "conflict", "already answered")
      );
    const listSpy = vi.spyOn(api, "listReplyCards");
    const { findByTestId, getByTestId, queryByTestId } = renderPage();
    await selectRepliesTab("snoozed");
    fireEvent.click(await findByTestId("unsnooze-card"));
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

  it("複製 in a card's head copies its question — summary, a blank line, the body with mentions as @名字 — turns into a check without folding the card, and says 複製失敗 when the card cannot be read", async () => {
    __injectMockReplyCard(
      mkCard({ id: "rc-1", summary: "要先備份嗎？", body: "請 [@Mira](/#office/member/mira) 看一下" })
    );
    renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));
    const card = document.querySelector<HTMLElement>('[data-reply-card-id="rc-1"]')!;
    const copy = card.querySelector<HTMLButtonElement>(".reply-card__copy button")!;
    expect(copy.getAttribute("aria-label")).toBe("複製整張卡片內容");

    await act(async () => {
      fireEvent.click(copy);
    });
    await waitFor(() => expect(clipboard).toEqual(["要先備份嗎？\n\n請 @Mira 看一下"]));
    expect(copy.getAttribute("aria-label")).toBe("已複製");
    expect(card.querySelector(".reply-card__copy [role='status']")!.innerHTML).toBe(
      '<span class="chat__sr-text">已複製</span>'
    );
    expect(card.getAttribute("aria-expanded")).toBe("false");

    vi.spyOn(api, "getReplyCard").mockRejectedValue(new Error("down"));
    await act(async () => {
      fireEvent.click(copy);
    });
    await waitFor(() =>
      expect(card.querySelector(".chat__msg-copy-status--failed")?.textContent).toBe("複製失敗")
    );
    expect(clipboard).toHaveLength(1);
    expect(card.getAttribute("aria-expanded")).toBe("false");
  });

  it("a finger held 500ms on a card opens the chat's sheet with the head's actions — 跳到原訊息, plus 稍後再看・標為過期 while it waits, then 複製整張卡片內容・選取文字, never 回覆這則 — without folding it; a mouse held as long opens nothing", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-secret",
        kind: "secret",
        options: [],
        createdTs: Date.now() / 1000 - 7200,
        secret: { state: "waiting", providedTs: null, retrievedTs: null, expiresTs: null },
      })
    );
    __injectMockReplyCard(
      mkCard({
        id: "rc-done",
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    const { getByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1", "rc-secret"]));
    const card = document.querySelector<HTMLElement>('[data-reply-card-id="rc-1"]')!;
    const summary = card.querySelector(".reply-card__summary")!;

    await hold(summary, 600, "mouse");
    expect(menuLabels()).toEqual([]);
    await hold(summary, 300);
    expect(menuLabels()).toEqual([]);

    await hold(summary, 520);
    expect(menuLabels()).toEqual(["跳到原訊息", "稍後再看", "標為過期", "複製整張卡片內容", "選取文字"]);
    expect(card.classList.contains("reply-card--pressed")).toBe(true);
    fireEvent.click(summary, { detail: 1 });
    expect(card.getAttribute("aria-expanded"), "the press's own click folded the card").toBe("false");
    act(() => {
      fireEvent.pointerDown(getByTestId("chat-msg-sheet-backdrop"));
    });
    fireEvent.click(getByTestId("chat-msg-sheet-backdrop"));
    expect(menuLabels()).toEqual([]);

    await hold(document.querySelector('[data-reply-card-id="rc-secret"] .reply-card__summary')!, 520);
    expect(menuLabels()).toEqual(["跳到原訊息", "稍後再看", "標為過期", "複製整張卡片內容", "選取文字"]);
    act(() => {
      fireEvent.pointerDown(getByTestId("chat-msg-sheet-backdrop"));
    });
    fireEvent.click(getByTestId("chat-msg-sheet-backdrop"));

    await showHandled();
    await waitFor(() => expect(paneIds("answered-card")).toEqual(["rc-done"]));
    await hold(document.querySelector('[data-reply-card-id="rc-done"] .reply-card__summary')!, 520);
    expect(menuLabels()).toEqual(["跳到原訊息", "複製整張卡片內容", "選取文字"]);
  });

  it("the sheet's 稍後再看 (取消延後 on a snoozed card), 標為過期 and 跳到原訊息 do what the head's buttons do: the same snooze menu, as a sheet; the same unsnooze; the same expire confirmation; the same chat route — and a keyboard's first activation in what the sheet opened is not swallowed", async () => {
    vi.spyOn(Date, "now").mockReturnValue(THU_10AM * 1000);
    __injectMockReplyCard(mkCard({ id: "rc-1", createdTs: THU_10AM - 600 }));
    __injectMockReplyCard(mkCard({ id: "rc-2", createdTs: THU_10AM - 1200 }));
    __injectMockReplyCard(mkCard({ id: "rc-s", createdTs: THU_10AM - 1800, snoozedUntilTs: THU_10AM + 86400 }));
    const snoozeSpy = vi.spyOn(api, "snoozeReplyCard");
    const { getByTestId, findByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1", "rc-2"]));
    const summaryOf = (id: string) =>
      document.querySelector(`[data-reply-card-id="${id}"] .reply-card__summary`)!;

    await hold(summaryOf("rc-1"), 520);
    choose("稍後再看");
    const sheet = await findByTestId("snooze-menu-rc-1");
    expect(sheet.className).toBe("reply-snooze reply-snooze--sheet");
    expect(sheet.parentElement?.getAttribute("data-testid")).toBe("snooze-backdrop");
    // A bare click, as a keyboard Enter or a screen reader sends: no
    // pointerdown first to tell the long press its own click has passed.
    fireEvent.click(getByTestId("snooze-preset-3h"), { detail: 0 });
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-2"]));
    expect(snoozeSpy).toHaveBeenCalledWith("rc-1", THU_10AM + 3 * 3600);
    expect(queryByTestId("snooze-menu-rc-1")).toBeNull();

    await selectRepliesTab("snoozed");
    expect(paneIds("snoozed-card")).toEqual(["rc-1", "rc-s"]);
    await hold(summaryOf("rc-s"), 520);
    expect(menuLabels()).toEqual(["跳到原訊息", "取消延後", "標為過期", "複製整張卡片內容", "選取文字"]);
    choose("取消延後");
    await waitFor(() => expect(paneIds("snoozed-card")).toEqual(["rc-1"]));
    expect(snoozeSpy).toHaveBeenLastCalledWith("rc-s", null);
    await selectRepliesTab("waiting");
    expect(paneIds("waiting-card")).toEqual(["rc-2", "rc-s"]);

    await hold(summaryOf("rc-2"), 520);
    choose("標為過期");
    expect(paneIds("waiting-card")).toEqual(["rc-2", "rc-s"]);
    fireEvent.click(await findByTestId("expire-confirm-btn"), { detail: 0 });
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-s"]));
    expect((await mockApiCard("rc-2")).status).toBe("expired");

    await hold(summaryOf("rc-s"), 520);
    choose("跳到原訊息");
    await waitFor(() => expect(window.location.hash).toBe("#office/chat/mira/msg/msg-1"));
  });

  it("the lifting finger's click is still swallowed when a scroll closed the sheet while the finger was down", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));
    const card = document.querySelector<HTMLElement>('[data-reply-card-id="rc-1"]')!;
    const summary = card.querySelector(".reply-card__summary")!;
    const press = (type: string) => {
      const e = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: 10, clientY: 10 });
      Object.defineProperty(e, "pointerType", { value: "touch" });
      act(() => {
        summary.dispatchEvent(e);
      });
    };
    press("pointerdown");
    await act(() => new Promise((r) => setTimeout(r, 520)));
    expect(menuLabels()).not.toEqual([]);
    act(() => {
      window.dispatchEvent(new Event("scroll"));
    });
    expect(menuLabels()).toEqual([]);
    press("pointerup");
    fireEvent.click(summary, { detail: 1 });
    expect(card.getAttribute("aria-expanded"), "the finger's own click folded the card").toBe("false");
  });

  it("after a long press, the first keyboard activation of a head button is not swallowed", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const { getByRole } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));
    await hold(document.querySelector('[data-reply-card-id="rc-1"] .reply-card__summary')!, 520);
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    expect(menuLabels()).toEqual([]);
    fireEvent.click(getByRole("button", { name: "跳到原訊息" }), { detail: 0 });
    await waitFor(() => expect(window.location.hash).toBe("#office/chat/mira/msg/msg-1"));
  });

  it("a card found by its id that, once opened, turns out answered elsewhere offers only 跳到原訊息・複製整張卡片內容・選取文字, though the lookup still says it waits", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const answered = mkCard({
      id: "rc-1",
      status: "answered",
      answeredTs: Date.now() / 1000 - 5,
      answer: { optionIdxs: [0], text: "", attachments: [] },
    });
    // The id lookup reads it waiting; the open card's own read finds it answered.
    vi.spyOn(api, "getReplyCard")
      .mockResolvedValueOnce(mkCard({ id: "rc-1" }))
      .mockResolvedValue(answered);
    const scrollIntoView = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = vi.fn();
    try {
      window.location.hash = "#replies/card/rc-1";
      const { findByTestId } = render(
        <I18nProvider>
          <WithSuggestedReplies>
            <ReplyCardsProvider>
              <RepliesPage replyCardId="rc-1" />
            </ReplyCardsProvider>
          </WithSuggestedReplies>
        </I18nProvider>
      );
      await findByTestId("answered-card");
      const card = document.querySelector('[data-reply-card-id="rc-1"]')!;
      expect(card.getAttribute("aria-expanded")).toBe("true");
      await hold(card.querySelector(".reply-card__summary")!, 520);
      expect(menuLabels()).toEqual(["跳到原訊息", "複製整張卡片內容", "選取文字"]);
    } finally {
      Element.prototype.scrollIntoView = scrollIntoView;
    }
  });

  it("a 稍後再看 opened from the long-press menu is asked for once: the card coming back to 待回覆 later does not open the menu by itself", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const { findByTestId, queryByTestId, getByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));
    await hold(document.querySelector('[data-reply-card-id="rc-1"] .reply-card__summary')!, 520);
    choose("稍後再看");
    await findByTestId("snooze-menu-rc-1");
    fireEvent.click(getByTestId("snooze-preset-3h"));
    await waitFor(() => expect(paneIds("waiting-card")).toEqual([]));
    await selectRepliesTab("snoozed");
    fireEvent.click(getByTestId("unsnooze-card"));
    await waitFor(() => expect(paneIds("snoozed-card")).toEqual([]));
    await selectRepliesTab("waiting");
    expect(paneIds("waiting-card")).toEqual(["rc-1"]);
    expect(queryByTestId("snooze-menu-rc-1")).toBeNull();
  });

  it("Esc on a 稍後再看 sheet the long-press menu opened gives focus back to the card, not to the hidden head button nor to the page", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const { findByTestId, queryByTestId, getByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]));
    const card = document.querySelector<HTMLElement>('[data-reply-card-id="rc-1"]')!;
    await hold(card.querySelector(".reply-card__summary")!, 520);
    choose("稍後再看");
    await findByTestId("snooze-menu-rc-1");
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    expect(queryByTestId("snooze-menu-rc-1")).toBeNull();
    expect(document.activeElement).not.toBe(getByTestId("snooze-card"));
    expect(document.activeElement).toBe(card);
  });

  it("選取文字 opens the card in the selection mode with the rest of the page inert; 取消 ends it with the card folded again, and 回覆 quotes the selection into the card's answer box and leaves it open", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1", body: "收件人寫在附件裡" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", createdTs: Date.now() / 1000 - 3600 }));
    const { getByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1", "rc-2"]));
    const card = document.querySelector<HTMLElement>('[data-reply-card-id="rc-1"]')!;
    const other = document.querySelector<HTMLElement>('[data-reply-card-id="rc-2"]')!;
    const summary = card.querySelector(".reply-card__summary")!;

    await hold(summary, 520);
    choose("選取文字");
    expect(getByTestId("chat-select-bar")).toBeTruthy();
    expect(card.classList.contains("reply-card--selecting")).toBe(true);
    expect(card.getAttribute("role"), "a fold toggle that does nothing in the mode").toBeNull();
    expect(card.getAttribute("aria-expanded")).toBeNull();
    expect(card.inert).toBeFalsy();
    expect(other.inert).toBe(true);
    expect(insideInert(getByTestId("replies-filter"))).toBe(true);
    expect(document.querySelector(".replies")!.classList.contains("replies--selecting")).toBe(true);
    await waitFor(() => expect(card.querySelector(".reply-card__body")).not.toBeNull());
    const tapped = card.querySelector(".reply-card__body")!;
    fireEvent.pointerDown(tapped);
    fireEvent.click(tapped);
    expect(card.querySelector(".reply-card__body"), "a tap folded the card").not.toBeNull();
    expect(getByTestId("chat-select-bar")).toBeTruthy();

    fireEvent.click(getByTestId("chat-select-cancel"));
    expect(queryByTestId("chat-select-bar")).toBeNull();
    expect(card.getAttribute("aria-expanded")).toBe("false");
    expect(other.inert).toBeFalsy();
    expect(document.activeElement).toBe(card.querySelector(".reply-card__copy button"));

    await hold(summary, 520);
    choose("選取文字");
    const body = await waitFor(() => {
      const el = card.querySelector(".reply-card__body");
      expect(el).not.toBeNull();
      return el!;
    });
    Range.prototype.getBoundingClientRect = () => new DOMRect(0, 0, 10, 10);
    Range.prototype.getClientRects = () => [] as unknown as DOMRectList;
    const text = document.createTreeWalker(body, NodeFilter.SHOW_TEXT).nextNode()!;
    const range = document.createRange();
    range.setStart(text, 0);
    range.setEnd(text, 3);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
    act(() => {
      document.dispatchEvent(new Event("selectionchange"));
    });
    await act(async () => {
      fireEvent.click(getByTestId("chat-select-copy"));
    });
    expect(clipboard).toEqual(["收件人"]);
    fireEvent.click(getByTestId("chat-select-reply"));
    expect(queryByTestId("chat-select-bar")).toBeNull();
    expect(card.getAttribute("aria-expanded")).toBe("true");
    expect(card.querySelector('[data-testid="reply-composer-quote"]')?.textContent).toBe("引用原文收件人");
    expect(document.activeElement).toBe(card.querySelector(".reply-composer textarea"));
  });


  it("in the selection mode a card whose snooze runs out stays under 稍後再看 and in the mode; a card answered elsewhere, which leaves the list, ends the mode with the rest of the page back in reach", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1", body: "收件人寫在附件裡", snoozedUntilTs: Date.now() / 1000 + 1.5 }));
    __injectMockReplyCard(mkCard({ id: "rc-2", createdTs: Date.now() / 1000 - 3600 }));
    const { getByTestId, queryByTestId } = renderPage();
    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-2"]));
    await selectRepliesTab("snoozed");
    await waitFor(() => expect(paneIds("snoozed-card")).toEqual(["rc-1"]));
    const cardEl = () => document.querySelector<HTMLElement>('[data-reply-card-id="rc-1"]');
    const bodyOf = () =>
      waitFor(() => {
        const el = cardEl()?.querySelector(".reply-card__body");
        expect(el).toBeTruthy();
        return el!;
      });
    await hold(cardEl()!.querySelector(".reply-card__summary")!, 520);
    choose("選取文字");
    await bodyOf();

    await waitFor(() => expect(paneIds("waiting-card")).toEqual(["rc-1"]), { timeout: 3000 });
    expect(getByTestId("nav-badge").textContent).toBe("2");
    expect(cardEl()!.classList.contains("reply-card--selecting")).toBe(true);
    const body = await bodyOf();
    fireEvent.pointerDown(body);
    fireEvent.click(body);
    expect(getByTestId("chat-select-bar"), "a tap inside the moved card ended the mode").toBeTruthy();
    expect(insideInert(getByTestId("replies-filter"))).toBe(true);

    await act(async () => {
      await api.answerReplyCard("rc-1", { optionIdxs: [0], text: "", attachments: [] });
    });
    await waitFor(() => expect(queryByTestId("chat-select-bar")).toBeNull());
    expect(document.querySelector(".replies")!.classList.contains("replies--selecting")).toBe(false);
    expect(insideInert(getByTestId("replies-filter"))).toBe(false);
  });
});

async function mockApiCard(id: string) {
  return api.getReplyCard(id);
}
