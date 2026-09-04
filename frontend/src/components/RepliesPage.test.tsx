// 請示 page (M2 回覆卡 B2). Locked here — the SPEC §2 acceptance behaviors:
//   1. Empty 請示 list → the ✓ 目前沒有待處理的請示 state.
//   2. Waiting cards render newest ask first (createdTs desc); the LONGEST-
//      waiting one keeps the highlight, each card with initiator identity,
//      已等你 {t}, and the ai_pick option tagged AI 建議 (position means nothing).
//   3. Answering (option click OR typed reply) moves the card 請示 →
//      近期已回覆; the final answer is tagged 你選的 (+ AI 建議 when a circled
//      option IS the
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
//
// KNOWN BOUNDARY (T-ee17). The task-title assertions below cover BOTH surfaces
// at once: the chat inline card (ChatReplyCard) and this Ask page render the
// same ReplyCardTaskRef, so one set of assertions is all there is.
// 若日後有人在聊天面單獨加條件（例如聊天面不顯示標題），不會有任何東西變紅。

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import {
  __resetMock,
  __injectMockMember,
  __injectMockChat,
  __injectMockReplyCard,
} from "../api/mock";
import { api } from "../api";
import type { ReplyCard } from "../api/adapter";
import { ApiError } from "../api/errors";

// Released-worker codename cache (T-3ed8): fixed map (the hook has its own
// tests) — only "ow-rel" resolves; other ids keep the raw-id fallback.
vi.mock("../hooks/useWorkerCodenames", () => ({
  useWorkerCodenames: (ids: readonly string[]) =>
    new Map(ids.filter((id) => id === "ow-rel").map((id) => [id, "R-2"])),
  useWorkerAvatarUrls: (ids: readonly string[]) =>
    new Map(
      ids
        .filter((id) => id === "ow-rel")
        .map((id) => [id, "/api/chat/attachment/ava-worker"]),
    ),
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
    const names = cards.map(
      (c) => c.querySelector(".reply-card__name")?.textContent
    );
    expect(names).toContain("外包 · R-2");
    expect(names).toContain("ow-9");
    expect(names).not.toContain("ow-rel");
  });

  it("tags the option that carries ai_pick and leaves every other chip untagged", async () => {
    __injectMockReplyCard(mkCard({}));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");
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

    fireEvent.click(card.querySelectorAll(".reply-option")[1]);

    // The answered pane is collapsed by default — expand it to see the card.
    // (Expanding a pane is not answering anything: the ONLY click aimed at the
    // card itself was the one above.)
    fireEvent.click(await findByTestId("answered-toggle"));
    const answeredCard = await findByTestId("answered-card");
    // The ask is GONE from 待回覆 and standing in 近期已回覆 — the owner's own
    // test of "did my tap take?".
    await waitFor(() => expect(queryAllByTestId("waiting-card")).toHaveLength(0));
    const final = answeredCard.querySelector('[data-testid="final-answer"]');
    // The circled option IS the ai_pick one → the AI 建議 tag rides alongside.
    expect(final?.textContent).toBe("你選的AI 建議先不要");
  });

  // T-4166: a 409 on answer is TERMINAL for that card — its task closed
  // underneath it (orphan), or it was already handled. The old code showed the
  // same「回覆失敗，請稍後重試」it shows a network blip and left the dead card on
  // screen, so the owner clicked a road that is 409 a hundred times out of a
  // hundred. Assert the DISTINCT message (a shared string would make this test
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
    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    fireEvent.click(card.querySelectorAll(".reply-option")[1]);

    const notice = await findByTestId("replies-action-error");
    expect(notice.textContent).toBe(zh.replies.answerStale);
  });

  // The regression side: a NON-409 failure keeps the retry wording — the 409
  // branch must not swallow honest transient errors.
  it("a 500 answer keeps the retryable 回覆失敗，請稍後重試 wording", async () => {
    __injectMockReplyCard(mkCard({}));
    vi.spyOn(api, "answerReplyCard").mockRejectedValue(
      new ApiError("http 500 for POST /api/reply-cards/rc-1/answer", 500,
        "internal_error", "boom")
    );
    const { findAllByTestId, findByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    fireEvent.click(card.querySelectorAll(".reply-option")[0]);

    const notice = await findByTestId("replies-action-error");
    expect(notice.textContent).toBe(zh.replies.answerError);
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
    // A free-text answer circles nothing, so it is not the AI pick either.
    expect(final?.textContent).toBe("你選的收件人是誰？");
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

  it("does NOT fetch the handled lists while collapsed; expanding pulls them (owner answered 區收合不 fetch)", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-wait" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-ans",
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [0], text: "", attachments: [] },
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
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
    const { findByTestId, getByText } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    const card = await findByTestId("answered-card");

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

  it("重新決定 re-arms the options; picking another updates the answer in place", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdxs: [1], text: "", attachments: [] },
      })
    );
    const { findByTestId, getByText, getByPlaceholderText } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    let card = await findByTestId("answered-card");

    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    // Edit mode: options pickable + the SAME typed composer appears.
    const options = card.querySelectorAll(".reply-option");
    expect((options[0] as HTMLButtonElement).disabled).toBe(false);
    expect(getByPlaceholderText("或直接打字改寫回覆…")).toBeTruthy();

    // Single-select 重新決定 also lands on the click.
    fireEvent.click(options[0]);

    await waitFor(async () => {
      card = await findByTestId("answered-card");
      const final = card.querySelector('[data-testid="final-answer"]');
      // No longer the AI pick → the AI 建議 tag is gone; 你選的 stays.
      expect(final?.textContent).toBe("你選的寄出");
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
    __injectMockMember({ id: "ow-rel", kind: "outsource", name: "R-2" });
    __injectMockReplyCard(mkCard({ id: "rc-ow", from: "ow-rel" }));
    const { findAllByTestId } = renderPage();
    const [card] = await findAllByTestId("waiting-card");

    expect(card.querySelector(".reply-card__avatar img")?.getAttribute("src")).toBe(
      "/api/chat/attachment/ava-worker",
    );
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
    const ref = await findByTestId("reply-task-ref");
    expect(ref.textContent).toContain("把開機說明改成座艙可編輯");
  });

  // Owner 2026-08-14, T-ee17 acceptance:「這個不能夠放到最一開始嗎？」— at the
  // bottom of the card the row only answered WHICH work after the whole ask had
  // been read. Judged by DOM order, not by geometry: a rendered-later row can
  // still be painted high by CSS, and then the reading order screen readers and
  // keyboards follow would disagree with the picture.
  it("puts the task row ahead of the summary on both a waiting and a handled card", async () => {
    __injectMockReplyCard(
      mkCard({ task: { id: "t-4", typeKey: "review-pr", title: "改設定頁" } }),
    );
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answer: { optionIdxs: [0], text: "好", attachments: [] },
        answeredTs: Date.now() / 1000 - 60,
        task: { id: "t-5", typeKey: "review-pr", title: "改開機說明" },
      }),
    );
    const { findByTestId } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));

    for (const testId of ["waiting-card", "answered-card"]) {
      const card = await findByTestId(testId);
      const ref = card.querySelector('[data-testid="reply-task-ref"]')!;
      const summary = card.querySelector(".reply-card__summary")!;
      expect(ref).toBeTruthy();
      expect(summary).toBeTruthy();
      expect(
        ref.compareDocumentPosition(summary) &
          Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy();
    }
  });

  // An empty title leaves the jump alone on the row — the cell is not drawn at
  // all, so there is no empty box and no placeholder standing in for a title
  // nobody wrote. The typeKey does NOT step in as a substitute: the chip is
  // gone on every card, including the ones with nothing else to say.
  it("draws no title cell when the task's title is empty", async () => {
    __injectMockReplyCard(
      mkCard({ task: { id: "t-3", typeKey: "review-pr", title: "" } }),
    );
    const { findByTestId } = renderPage();
    const ref = await findByTestId("reply-task-ref");
    expect(ref.querySelector(".reply-card__task-title")).toBeNull();
    expect(ref.querySelector(".reply-card__task-type")).toBeNull();
    expect(ref.textContent).not.toContain("review-pr");
    expect(ref.textContent).toContain("查看任務詳情");
  });

  // The negative half: no task means the row says nothing about a task —
  // NOT an empty slot, which reads as "this ask has no title" rather than
  // "this ask has no task".
  it("shows no task row at all on a pure chat ask", async () => {
    __injectMockReplyCard(mkCard({ task: null }));
    const { findByTestId, queryByTestId } = renderPage();
    await findByTestId("waiting-card");
    expect(queryByTestId("reply-task-ref")).toBeNull();
  });
});
