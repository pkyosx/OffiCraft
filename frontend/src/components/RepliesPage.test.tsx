// 請示 page (M2 回覆卡 B2). Locked here — the SPEC §2 acceptance behaviors:
//   1. Empty 請示 list → the ✓ 目前沒有待處理的請示 state.
//   2. Waiting cards render newest ask first (createdTs desc); the LONGEST-
//      waiting one keeps the highlight, each card with initiator identity,
//      已等你 {t}, and options[0] tagged AI 建議.
//   3. Answering (option click OR typed reply) moves the card 請示 →
//      近期已回覆; the final answer is tagged 你選的 (+ AI 建議 when it IS the
//      AI pick). No close/skip control exists anywhere.
//   3b. 近期已回覆 is COLLAPSED by default — only the toggle row (title · N +
//      hint) renders; clicking expands the answered cards, clicking again
//      collapses. Not persisted (component state only).
//   4. 查看當初選項 expands the original options; 重新決定 re-arms them + shows
//      the composer; picking another option updates the answer in place
//      (stays answered); 取消 keeps the original answer.
//   5. 跳到原訊息 routes to that member's chat room WITH the ask message id
//      (#office/chat/<id>/msg/<msgId>) — ChatArea locates + highlights it (B3).
//   6. Answering never touches the chat unread red dot — the badge and the
//      red dot clear independently (red dot clears only by entering the
//      conversation).

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import {
  __resetMock,
  __injectMockChat,
  __injectMockReplyCard,
} from "../api/mock";
import { api } from "../api";
import type { ReplyCard } from "../api/adapter";

// Released-worker codename cache (T-3ed8): fixed map (the hook has its own
// tests) — only "ow-rel" resolves; other ids keep the raw-id fallback.
vi.mock("../hooks/useWorkerCodenames", () => ({
  useWorkerCodenames: (ids: readonly string[]) =>
    new Map(ids.filter((id) => id === "ow-rel").map((id) => [id, "R-2"])),
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

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
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
    expect(cards).toHaveLength(3);
    // Newest ask first (開卡時間 新→舊), whatever order the cards arrived in.
    expect(cards[0].textContent).toContain("新的請示");
    expect(cards[1].textContent).toContain("中間的請示");
    expect(cards[2].textContent).toContain("等很久的請示");
    // No card wears a highlight — owner ruled the longest-waiting accent out
    // (T-9ea9): every card carries the identical base class.
    for (const card of cards) {
      expect(card.className).toBe("reply-card");
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
  });

  it("resolves an outsource asker to its 外包 代號, not the raw ow- id", async () => {
    // "ow-rel" is never in the members roster (server excludes
    // kind='outsource') — the identity row resolves through the codename
    // cache; an unresolvable ow- id keeps the honest raw-id fallback.
    __injectMockReplyCard(
      mkCard({ id: "rc-ow", from: "ow-rel", summary: "外包的請示" })
    );
    __injectMockReplyCard(
      mkCard({ id: "rc-gone", from: "ow-9", summary: "查無外包的請示" })
    );
    const { findAllByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");
    const names = cards.map(
      (c) => c.querySelector(".reply-card__name")?.textContent
    );
    expect(names).toContain("外包 · R-2");
    expect(names).toContain("ow-9");
    expect(names).not.toContain("ow-rel");
  });

  it("tags the FIRST quick-reply option as the AI pick", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
    const options = card.querySelectorAll(".reply-option");
    expect(options).toHaveLength(2);
    expect(options[0].textContent).toContain("AI 建議");
    expect(options[1].textContent).not.toContain("AI 建議");
  });

  it("answering via an option moves the card to 近期已回覆 tagged 你選的 + AI 建議", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId, findByTestId, queryAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    fireEvent.click(card.querySelectorAll(".reply-option")[0]);

    // The answered pane is collapsed by default — expand it to see the card.
    fireEvent.click(await findByTestId("answered-toggle"));
    const answeredCard = await findByTestId("answered-card");
    await waitFor(() => expect(queryAllByTestId("waiting-card")).toHaveLength(0));
    const final = answeredCard.querySelector('[data-testid="final-answer"]');
    expect(final?.textContent).toContain("寄出");
    expect(final?.textContent).toContain("你選的");
    // The pick IS options[0] → the AI 建議 tag rides alongside.
    expect(final?.textContent).toContain("AI 建議");
  });

  it("answering via the typed composer closes the card with the free text as the answer", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId, findByTestId, getByPlaceholderText } =
      renderPage();
    await findAllByTestId("waiting-card");

    const input = getByPlaceholderText("輸入回覆…");
    fireEvent.change(input, { target: { value: "收件人是誰？" } });
    fireEvent.keyDown(input, { key: "Enter" });

    fireEvent.click(await findByTestId("answered-toggle"));
    const answeredCard = await findByTestId("answered-card");
    const final = answeredCard.querySelector('[data-testid="final-answer"]');
    expect(final?.textContent).toContain("收件人是誰？");
    expect(final?.textContent).toContain("你選的");
    // A free-text answer is NOT the AI pick.
    expect(final?.textContent).not.toContain("AI 建議");
  });

  it("近期已處理 is collapsed by default; the title row toggles it open and shut", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
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

  it("does NOT fetch the handled lists while collapsed; expanding pulls them (owner answered 區收合不 fetch)", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-wait" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-ans",
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const listSpy = vi.spyOn(api, "listReplyCards");
    const { findByTestId } = renderPage();

    // Mount fetched the WAITING list + the counts (so the header knows · 1),
    // but NEVER the answered LIST while the pane is collapsed.
    const toggle = await findByTestId("answered-toggle");
    expect(toggle.textContent).toContain("近期已處理 · 1");
    const listed = () => listSpy.mock.calls.map((c) => c[0]);
    expect(listed()).toContain("waiting");
    expect(listed()).not.toContain("answered");
    expect(listed()).not.toContain("expired");

    // Expanding is what pulls the handled lists (answered + expired).
    fireEvent.click(toggle);
    await findByTestId("answered-card");
    expect(listed()).toContain("answered");
    expect(listed()).toContain("expired");
  });

  it("查看當初選項 expands the original options with the standing pick marked", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 1, text: "", attachments: [] },
      })
    );
    const { findByTestId, getByText } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    const card = await findByTestId("answered-card");

    fireEvent.click(getByText("查看當初選項"));
    const options = card.querySelectorAll(".reply-option");
    expect(options).toHaveLength(2);
    // Review mode: options render but are NOT pickable yet.
    expect((options[0] as HTMLButtonElement).disabled).toBe(true);
    expect(options[1].textContent).toContain("目前");
    // 重新決定 sits at the expansion's bottom.
    expect(getByText("重新決定")).toBeTruthy();
  });

  it("重新決定 re-arms the options; picking another updates the answer in place", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const { findByTestId, getByText, getByPlaceholderText } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    let card = await findByTestId("answered-card");

    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    // Edit mode: options pickable + the SAME typed composer appears.
    const options = card.querySelectorAll(".reply-option");
    expect((options[1] as HTMLButtonElement).disabled).toBe(false);
    expect(getByPlaceholderText("或直接打字改寫回覆…")).toBeTruthy();

    fireEvent.click(options[1]);

    await waitFor(async () => {
      card = await findByTestId("answered-card");
      const final = card.querySelector('[data-testid="final-answer"]');
      expect(final?.textContent).toContain("先不要");
      // No longer the AI pick → the AI 建議 tag is gone; 你選的 stays.
      expect(final?.textContent).not.toContain("AI 建議");
    });
  });

  it("取消 leaves 重新決定 mode without touching the answer", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const { findByTestId, getByText, queryByPlaceholderText } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    const card = await findByTestId("answered-card");

    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    fireEvent.click(getByText("取消"));

    expect(queryByPlaceholderText("或直接打字改寫回覆…")).toBeNull();
    const final = card.querySelector('[data-testid="final-answer"]');
    expect(final?.textContent).toContain("寄出");
    expect(final?.textContent).toContain("AI 建議");
  });

  it("跳到原訊息 routes to the member's chat with the ask message id (B3 locate target)", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId, getByText } = renderPage();
    await findAllByTestId("waiting-card");

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

    fireEvent.click(card.querySelector(".reply-card__avatar")!);
    // T-a706 owner-acceptance finding: without the /from/replies tag,
    // OfficePage's own 返回 button reset to its default chat view instead of
    // back here — see OfficePage.member-detail-backto.test.tsx for the fix
    // proven from the OTHER side of this hash contract.
    expect(window.location.hash).toBe("#office/member/mira/from/replies");
  });

  it("clicking an outsource asker's avatar opens the worker panel (#office/worker/<id>), not the member one, same 返回 tag", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-ow", from: "ow-rel" }));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    fireEvent.click(card.querySelector(".reply-card__avatar")!);
    expect(window.location.hash).toBe("#office/worker/ow-rel/from/replies");
  });

  it("the avatar has an accessible name (aria-label) — Avatar's inner glyphs are aria-hidden", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    const avatarBtn = card.querySelector(".reply-card__avatar")!;
    expect(avatarBtn.tagName).toBe("BUTTON");
    expect(avatarBtn.getAttribute("aria-label")).toBe(zh.office.viewProfile);
  });

  it("標為過期 double-confirms, closes the card without an answer, and lands it 已過期 in 近期已處理", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findByTestId, findAllByTestId, queryAllByTestId, queryByTestId } =
      renderPage();
    const [card] = await findAllByTestId("waiting-card");

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

    fireEvent.click(card.querySelectorAll(".reply-option")[0]);
    fireEvent.click(await findByTestId("answered-toggle"));
    await findByTestId("answered-card");

    // The card closed, but Mira's unread count still stands — only entering
    // the conversation (listChat) clears it.
    const members = await api.listMembers();
    expect(members.find((m) => m.id === "mira")?.unreadCount).toBe(1);
  });
});
