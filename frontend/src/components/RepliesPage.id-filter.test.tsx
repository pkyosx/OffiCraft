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
import { api } from "../api";
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

  it("matches on a PREFIX and ignores case — the two things this field does that pasting a whole id does not", async () => {
    // The headline behaviour of this control, and the one an independent
    // review found unguarded: with only whole-id, all-lowercase specs, both
    // `.includes` → `===` AND dropping `.toLowerCase()` stayed green.
    __injectMockReplyCard(mkCard({ id: "rc-aaa111", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb222", summary: "第二張" }));

    const { findAllByTestId, findByTestId, findByText } = renderPage();
    const field = await findByTestId("filter-reply-card-id");

    // A half-typed id still narrows instead of collapsing to nothing.
    fireEvent.change(field, { target: { value: "rc-bbb" } });
    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(1)
    );
    expect(await findByText("第二張")).toBeTruthy();

    // And the same id shouted back matches too (ids are lower-case on the
    // wire, so an upper-case paste must not come back empty).
    fireEvent.change(field, { target: { value: "RC-BBB222" } });
    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(1)
    );
  });

  it("a link to a WAITING card neither fetches the handled pane nor blanks its count", async () => {
    // The most common path there is, and the one an earlier cut of this filter
    // broke twice over: (a) it fired the auto-load during the mount window,
    // when `waiting` is still [] and an empty list means "not known yet"; and
    // (b) it narrowed the header count to a list nobody had loaded, printing a
    // 0 that the section's zero-hide then turned into "the pane is gone".
    // MUTANTS: drop the `loading` guard → the fetch assertion goes red; narrow
    // the count while unloaded → the 「· 1」 assertion goes red.
    const listSpy = vi.spyOn(api, "listReplyCards");
    __injectMockReplyCard(mkCard({ id: "rc-live", summary: "還在等的" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-old",
        summary: "已經回過的",
        status: "answered",
        answeredTs: Date.now() / 1000 - 3600,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );

    const { findByTestId } = renderPage("rc-live");
    expect((await findByTestId("waiting-card")).textContent).toContain(
      "還在等的"
    );

    // Nothing was fetched for a pane the owner never asked about.
    expect(
      listSpy.mock.calls.filter(([status]) => status === "answered")
    ).toHaveLength(0);
    // And the pane is still there, still counted by the server — the count is
    // 「what is in the last 24h」, not 「what matched」, because nothing has been
    // matched against yet.
    expect((await findByTestId("answered-toggle")).textContent).toContain(
      "近期已處理 · 1"
    );
  });

  it("keeps 近期已處理 visible, saying 0, when it IS loaded and nothing matches", async () => {
    // The other half of the same failure: once the pane is loaded, an empty
    // FILTERED list is a real 0 — but hiding the section on that 0 makes "no
    // card matched" look identical to "this pane does not exist", and takes
    // away the handle. MUTANT: restore the bare `handledShown > 0` render
    // condition and this goes red.
    __injectMockReplyCard(mkCard({ id: "rc-live", summary: "還在等的" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-old",
        summary: "已經回過的",
        status: "answered",
        answeredTs: Date.now() / 1000 - 3600,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );

    const { findByTestId } = renderPage();
    // Open it first, so the pane is genuinely LOADED before the filter lands.
    fireEvent.click(await findByTestId("answered-toggle"));
    expect(await findByTestId("answered-card")).toBeTruthy();

    fireEvent.change(await findByTestId("filter-reply-card-id"), {
      target: { value: "rc-live" },
    });

    const toggle = await findByTestId("answered-toggle");
    expect(toggle.textContent).toContain("近期已處理 · 0");
  });

  it("fetches the handled pane at most once however many characters are typed", async () => {
    // `loadHandled` is two requests with no in-flight de-duplication and the
    // loaded flag only flips when they RETURN, so the amplification only
    // appears while they are in flight — a mock that resolves immediately
    // hides it completely. Hence the never-settling stub: it holds the window
    // open for the whole test.
    // MUTANT: remove the once-per-visit latch → one pair per keystroke.
    const listSpy = vi
      .spyOn(api, "listReplyCards")
      .mockImplementation((status) =>
        status === "waiting"
          ? Promise.resolve([])
          : new Promise<never>(() => {})
      );
    __injectMockReplyCard(mkCard({ id: "rc-live", summary: "還在等的" }));

    const { findByTestId } = renderPage();
    const field = await findByTestId("filter-reply-card-id");

    for (const value of ["r", "rc", "rc-", "rc-z", "rc-zz", "rc-zzz"]) {
      fireEvent.change(field, { target: { value } });
    }
    await waitFor(() =>
      expect(
        listSpy.mock.calls.filter(([status]) => status === "answered")
          .length
      ).toBeGreaterThan(0)
    );
    expect(
      listSpy.mock.calls.filter(([status]) => status === "answered")
    ).toHaveLength(1);
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
