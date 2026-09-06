// 請示 page — the ID 篩選 (T-93, ROUND 2).
//
// ⚠️ THIS FILE WAS REWRITTEN, NOT EXTENDED. Round 1 pinned a 篩選列 that
// narrowed the ALREADY-LOADED cards on every keystroke, and owner 2026-09-06
// overruled that shape twice:
//   ①「很常我們要找一張任務或票而已，但每次都要全部都撈回來才濾不合理 你可以
//      設計成要給完搜尋條件要再按 search 的版本嗎」⇒ the fields are a DRAFT;
//      nothing happens until 套用篩選. The per-keystroke specs are therefore
//      gone — replaced by their opposite, which now asserts the new rule.
//   ②「按搜尋時不要再跳出新modal」 ⇒ the panel expands IN the page.
//   ③ On a reply card he picked option ①: the id is a condition like the
//      others, and it is ASKED OF THE SERVER.
//
// What ③ buys, and why it is the reason this ticket exists: round 1 could only
// see waiting cards plus whatever was answered/expired in the last 24h, so an
// older card came back as 「沒有符合篩選條件的請示」 — a sentence that a card
// which genuinely does not exist produces too. The owner read that collapse as
// "no such card" in review. So the three outcomes of a by-id read are pinned
// here as THREE DIFFERENT THINGS on screen:
//   found (even for a card no pane carries) / 404 / server never reached.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "./RepliesPage";
import { ReplyCardsProvider } from "../hooks/useReplyCards";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import { api } from "../api";
import { ApiError } from "../api/errors";
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

/** Open the panel, put `id` in the field, press 套用篩選 — the whole gesture
 * the owner asked for, in one helper so every spec below drives the real
 * affordance rather than reaching into state. */
async function applyId(
  findByTestId: (id: string) => Promise<HTMLElement>,
  id: string
) {
  fireEvent.click(await findByTestId("replies-filter-toggle"));
  fireEvent.change(await findByTestId("filter-reply-card-id"), {
    target: { value: id },
  });
  fireEvent.click(await findByTestId("replies-filter-apply"));
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

describe("請示 ID 篩選（面板版）", () => {
  it("a link carrying a card id shows only that card, out of several waiting", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));
    __injectMockReplyCard(mkCard({ id: "rc-ccc", summary: "第三張" }));

    const { findAllByTestId, findByTestId } = renderPage("rc-bbb");

    const cards = await findAllByTestId("waiting-card");
    expect(cards).toHaveLength(1);
    expect(cards[0].textContent).toContain("第二張");
    // 🔴 With the panel SHUT, the strip is the only thing saying why one card
    // is here — a collapsed panel hiding a live filter is the failure mode of
    // every filter that lives behind a button.
    expect((await findByTestId("replies-filter-chip")).textContent).toContain(
      "編號：rc-bbb"
    );
  });

  it("opening the panel shows the APPLIED id in the field, not a blank one", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findByTestId } = renderPage("rc-bbb");
    expect(await findByTestId("waiting-card")).toBeTruthy();

    fireEvent.click(await findByTestId("replies-filter-toggle"));
    expect(
      (await findByTestId("filter-reply-card-id")) as HTMLInputElement
    ).toHaveProperty("value", "rc-bbb");
  });

  it("typing changes NOTHING until 套用篩選 — the rule that replaced per-keystroke filtering", async () => {
    // 🔴 THIS SPEC ASSERTS THE OPPOSITE OF ITS ROUND-1 ANCESTOR ("typing
    // narrows the waiting list"), because owner overruled that behaviour:
    //「每次都要全部都撈回來才濾不合理」. A draft that took effect while being
    // typed is the thing he rejected, so it is now a red condition, not a
    // green one.
    const getSpy = vi.spyOn(api, "getReplyCard");
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findAllByTestId, findByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);

    fireEvent.click(await findByTestId("replies-filter-toggle"));
    for (const value of ["r", "rc", "rc-", "rc-b", "rc-bb", "rc-bbb"]) {
      fireEvent.change(await findByTestId("filter-reply-card-id"), {
        target: { value },
      });
    }
    // Six keystrokes, no narrowing and — the half the owner actually
    // complained about — not one request.
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);
    expect(getSpy).not.toHaveBeenCalled();

    fireEvent.click(await findByTestId("replies-filter-apply"));
    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(1)
    );
    expect(getSpy).toHaveBeenCalledTimes(1);
    expect(getSpy).toHaveBeenCalledWith("rc-bbb");
  });

  it("Cancel commits nothing, and the abandoned draft does not survive to the next open", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findAllByTestId, findByTestId, queryByTestId } = renderPage();
    fireEvent.click(await findByTestId("replies-filter-toggle"));
    fireEvent.change(await findByTestId("filter-reply-card-id"), {
      target: { value: "rc-bbb" },
    });
    fireEvent.click(await findByTestId("replies-filter-cancel"));

    // Nothing applied: both cards, and no 「已篩選」 strip at all.
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);
    expect(queryByTestId("replies-filter-summary")).toBeNull();

    // …and re-opening shows the applied (empty) value, not the abandoned one.
    fireEvent.click(await findByTestId("replies-filter-toggle"));
    expect(
      (await findByTestId("filter-reply-card-id")) as HTMLInputElement
    ).toHaveProperty("value", "");
  });

  it("🔴 404 says the SERVER does not have this id — not that a list has not loaded", async () => {
    // The defect this ticket exists to remove. Round 1 answered 「沒有符合篩選
    // 條件的請示」 both for a card that does not exist and for one it simply
    // had not fetched, and the owner read the second as the first.
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));

    const { findByTestId, queryByTestId } = renderPage("rc-nope");

    const missing = await findByTestId("replies-lookup-missing");
    expect(missing.textContent).toBe(
      "找不到「rc-nope」。這是跟伺服器要過的結果，不是還沒載進來——這個編號現在不存在。"
    );
    // The three outcomes are three different nodes: this one is not the plain
    // filtered-empty copy, and it is not the never-reached-the-server one.
    expect(queryByTestId("replies-empty")).toBeNull();
    expect(queryByTestId("replies-lookup-failed")).toBeNull();
  });

  it("🔴 a non-404 failure says the server was never reached, and never says 找不到", async () => {
    // MUTANT (and the exact wrong thing to do): treat every rejection as
    // "missing". Then an offline cockpit tells the owner a card he is looking
    // at in another window does not exist.
    vi.spyOn(api, "getReplyCard").mockRejectedValue(
      new ApiError("http 500 for GET /api/reply-cards/rc-x", 500, "internal_error", "boom")
    );
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));

    const { findByTestId, queryByTestId } = renderPage();
    await applyId(findByTestId, "rc-x");

    const failed = await findByTestId("replies-lookup-failed");
    expect(failed.textContent).toContain("沒能問到伺服器");
    expect(failed.textContent).not.toContain("找不到");
    expect(queryByTestId("replies-lookup-missing")).toBeNull();
    expect(queryByTestId("replies-empty")).toBeNull();
  });

  it("with no filter at all the ✓ copy is still the one that shows", async () => {
    // Non-vacuity control: the copies really are different strings and this
    // page really can still produce the ✓ one.
    const { findByTestId } = renderPage();
    expect((await findByTestId("replies-empty")).textContent).toBe(
      "✓ 目前沒有待處理的請示"
    );
  });

  it("清除全部 restores every card AND drops the id from the URL", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));
    window.location.hash = "#replies/card/rc-bbb";

    const { findAllByTestId, findByTestId } = renderPage("rc-bbb");
    expect(await findAllByTestId("waiting-card")).toHaveLength(1);

    fireEvent.click(await findByTestId("replies-filter-clear"));

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(2)
    );
    // 🔴 The URL half: leave the id in the hash and a refresh re-applies the
    // filter, which reads as "clear is broken".
    expect(window.location.hash).toBe("#replies");
  });

  it("the chip's × drops that one axis, hash included", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));
    window.location.hash = "#replies/card/rc-bbb";

    const { findAllByTestId, findByTestId, queryByTestId } = renderPage("rc-bbb");
    expect(await findAllByTestId("waiting-card")).toHaveLength(1);

    fireEvent.click(
      (await findByTestId("replies-filter-chip")).querySelector("button")!
    );

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(2)
    );
    expect(queryByTestId("replies-filter-summary")).toBeNull();
    expect(window.location.hash).toBe("#replies");
  });

  it("applying an EMPTY field clears the filter and the hash with it", async () => {
    // 🔴 The round-1 version of this hole: the owner could delete the value by
    // hand and be left on #replies/card/<id> with the hash still filtering
    // after a reload. The draft/applied split moves the hole rather than
    // closing it — applying an empty draft must take the stale hash id with
    // it, or the next reload seeds the old card straight back.
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));
    window.location.hash = "#replies/card/rc-bbb";

    const { findAllByTestId, findByTestId } = renderPage("rc-bbb");
    expect(await findAllByTestId("waiting-card")).toHaveLength(1);

    fireEvent.click(await findByTestId("replies-filter-toggle"));
    fireEvent.change(await findByTestId("filter-reply-card-id"), {
      target: { value: "" },
    });
    fireEvent.click(await findByTestId("replies-filter-apply"));

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(2)
    );
    expect(window.location.hash).toBe("#replies");
  });

  it("a link to a WAITING card does not fetch the handled pane", async () => {
    // Still true, and still worth pinning: the handled lists are two requests
    // for a pane the owner never asked about. What CHANGED from round 1 is the
    // 「· N」 — with a filter applied it is now「how many cards match」(0 here),
    // not the server's whole-pane count, because the id is answered by a
    // single by-id read rather than by narrowing loaded rows.
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

    expect(
      listSpy.mock.calls.filter(([status]) => status === "answered")
    ).toHaveLength(0);
    expect((await findByTestId("answered-toggle")).textContent).toContain(
      "近期已處理 · 0"
    );
  });

  it("keeps 近期已處理 on screen, saying 0, while a filter is applied", async () => {
    // Hiding the section on that 0 removes the only handle for opening the
    // pane and makes "no match" indistinguishable from "this pane does not
    // exist". MUTANT: restore a bare `handledShown > 0` render condition.
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
    await applyId(findByTestId, "rc-live");

    await waitFor(async () =>
      expect((await findByTestId("answered-toggle")).textContent).toContain(
        "近期已處理 · 0"
      )
    );
  });

  it("🔴 finds a card NO pane carries — answered three days ago, invisible to round 1", async () => {
    // THE WHOLE TICKET. The panes hold waiting cards plus the last 24h of
    // handled ones, so this card was unreachable while LOOKING exactly like a
    // card that does not exist. It is now the server's answer, and it lands on
    // screen with its pane expanded — 「找到了但畫面上沒有」 is the same silent
    // nothing as the false empty it replaces.
    // MUTANT: filter the loaded rows instead of reading by id → red here while
    // every other spec in this file stays green.
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "rc-live", summary: "還在等的" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-ancient",
        summary: "三天前回過的",
        status: "answered",
        createdTs: now - 3 * 86400 - 600,
        answeredTs: now - 3 * 86400,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );

    const { findByTestId, findByText, queryByText, queryByTestId } = renderPage();
    // Control: it really is absent to begin with. The 近期已處理 section is not
    // even on screen — the pane's count is 0 because the card fell out of the
    // 24h window — so there is no fold to open and nothing loaded to match.
    expect(await findByTestId("waiting-card")).toBeTruthy();
    expect(queryByTestId("answered-toggle")).toBeNull();
    expect(queryByText("三天前回過的")).toBeNull();

    await applyId(findByTestId, "rc-ancient");

    expect(await findByText("三天前回過的")).toBeTruthy();
    expect(await findByTestId("answered-card")).toBeTruthy();
  });

  it("a found card in the COLLAPSED handled pane is rendered, not hidden behind the fold", async () => {
    // The 「找到了但畫面上沒有」 guard on its own: arrive with the pane shut
    // (its default) and never touch the toggle.
    const now = Date.now() / 1000;
    __injectMockReplyCard(
      mkCard({
        id: "rc-handled",
        summary: "已經回過的",
        status: "answered",
        answeredTs: now - 3600,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );

    const { findByTestId, findByText } = renderPage("rc-handled");

    expect(await findByText("已經回過的")).toBeTruthy();
    expect(
      (await findByTestId("answered-toggle")).getAttribute("aria-expanded")
    ).toBe("true");
  });
});
