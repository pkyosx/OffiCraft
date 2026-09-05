// 請示 page — the ID 篩選 (T-93).
//
// owner asked for one thing and ruled out a second (rc-2085e5ec60be and
// c-0e183a7fbb10, 2026-09-05):
//   ✅ an ordinary filter, behaving like the ones on the 任務頁; a URL carrying
//      an id only PRE-FILLS it —「只是連過去幫忙帶篩選參數而已」.
//   ❌ NO by-id fetch to reach a card the panes did not carry —「不用，就直接用
//      畫面上的ID篩選一樣就好」. A card answered/expired more than 24h ago is
//      therefore NOT findable here, knowingly.
//
// What these specs pin, and why each one can go red:
//   1. filtering actually narrows (the whole point);
//   2. an empty RESULT does not say 「你沒有待處理的請示」 — that copy would tell
//      the owner he is caught up while cards sit behind the filter;
//   3. clearing drops the id from the URL too, or a reload re-seeds it and the
//      clear looks broken;
//   4. 🔴 a card that lives only in the COLLAPSED 近期已處理 pane is still found.
//      The pane is not fetched until it opens, so a filter that ignored it
//      would answer "no match" for a card that is right there — a FALSE empty,
//      indistinguishable from the real one. This is the guard Kyle asked for
//      (2026-09-05): without it, someone can narrow the filter back to the
//      loaded rows and every other assertion here stays green.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import type { ReplyCard } from "../api/adapter";

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要幫你寄出這封信嗎？",
    body: "",
    options: [{ text: "寄出", aiPick: false }],
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

function renderPage(replyCardId?: string) {
  return render(
    <I18nProvider>
      <ReplyCardsProvider>
        <RepliesPage replyCardId={replyCardId} />
      </ReplyCardsProvider>
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  // jsdom implements neither of these; the page calls them when a URL carries
  // a card id. They are viewport effects, not behaviour these specs assert.
  Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("請示 ID 篩選", () => {
  it("a link carrying a card id shows only that card, out of several waiting", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));
    __injectMockReplyCard(mkCard({ id: "rc-ccc", summary: "第三張" }));

    const { findAllByTestId } = renderPage("rc-bbb");

    const cards = await findAllByTestId("waiting-card");
    expect(cards).toHaveLength(1);
    expect(cards[0].textContent).toContain("第二張");
    // The field is pre-filled, so the owner can see WHY only one card is here
    // and can edit or clear it — the URL is not a hidden state.
    const field = document.querySelector<HTMLInputElement>(
      '[data-testid="filter-reply-card-id"]'
    );
    expect(field?.value).toBe("rc-bbb");
  });

  it("typing narrows the waiting list without any link involved", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findAllByTestId, findByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);

    fireEvent.change(await findByTestId("filter-reply-card-id"), {
      target: { value: "rc-bbb" },
    });

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(1)
    );
  });

  it("an empty RESULT says no card matches — NOT that nothing is waiting", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findByTestId } = renderPage("rc-nope");

    const empty = await findByTestId("replies-empty");
    // 驗收條件: the owner must not be told he is caught up while two cards wait.
    expect(empty.textContent).toBe("沒有符合篩選條件的請示");
    expect(empty.textContent).not.toBe("✓ 目前沒有待處理的請示");
  });

  it("with no filter at all the ✓ copy is still the one that shows", async () => {
    // Non-vacuity control for the spec above: the two copies really are
    // different strings and this page really can still produce the ✓ one, so
    // that assertion is not passing just because both branches say the same.
    const { findByTestId } = renderPage();
    expect((await findByTestId("replies-empty")).textContent).toBe(
      "✓ 目前沒有待處理的請示"
    );
  });

  it("clearing restores every card AND drops the id from the URL", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));
    window.location.hash = "#replies/card/rc-bbb";

    const { findAllByTestId, findByTestId } = renderPage("rc-bbb");
    expect(await findAllByTestId("waiting-card")).toHaveLength(1);

    fireEvent.click(await findByTestId("clear-filters"));

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(2)
    );
    // 🔴 The URL half: leave the id in the hash and a refresh re-applies the
    // filter, which reads as "clear is broken".
    expect(window.location.hash).toBe("#replies");
  });

  it("finds a card that lives ONLY in the collapsed 近期已處理 pane", async () => {
    // The handled pane is collapsed AND unfetched on arrival, so this card is
    // not among the rows the page starts with — the filter has to open and
    // load it. MUTANT (the regression this guards): key the auto-open effect
    // on the URL id alone, or filter only the already-loaded rows, and this
    // goes red while every other spec here stays green.
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-waiting", summary: "還在等的" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-handled",
        summary: "已經回過的",
        status: "answered",
        answeredTs: now - 3600,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );

    const { findByTestId, findByText } = renderPage();

    fireEvent.change(await findByTestId("filter-reply-card-id"), {
      target: { value: "rc-handled" },
    });

    expect(await findByText("已經回過的")).toBeTruthy();
  });
});
