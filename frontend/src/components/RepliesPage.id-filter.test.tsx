// 請示 page — the ID 篩選 (T-93 round 2/3, re-pinned for T-118).
//
// ⚠️ THIS FILE HAS BEEN REWRITTEN TWICE, AND THE SECOND REWRITE IS THE ONE YOU
// ARE READING. Round 1 pinned a 篩選列 that narrowed the ALREADY-LOADED cards on
// every keystroke; round 2/3 replaced it with a funnel button, an in-page
// expanding panel, a DRAFT committed by 套用篩選, and a 「N 筆 · 已篩選：<chip ×>
// · 清除全部」 strip. owner 2026-09-06 20:07 (c-c3d681fe05da) then overturned
// THAT shape too, in his own words:
//
//   「我想改一下,不要多filter那一層了,全部拉出來,而任務編號那邊就是按enter
//     或是點外面就視為apply了,然後也不用再顯示14筆已篩選跟那一行跟案件那個
//     子標了,案件跟請示卡都一樣」
//
// restated for this page at 20:19 (c-38c7759e6377):「請示卡跟任務都要改成一樣的
// 呈現方式,一樣請示卡的子標題拿掉」.
//
// So the funnel, the panel, 取消／套用篩選, the 已篩選 strip with its chips and
// 清除全部, and the 「請示卡」 sub-title above the list are all GONE. Every spec
// that pinned one of them is REPLACED below — in place, with a marker saying
// what it used to hold and who overturned it — never deleted, because a deleted
// spec is a behaviour nobody is watching any more.
//
// 🔴 WHAT DID **NOT** CHANGE, and must not be weakened by this rewrite:
//   · the id is ASKED OF THE SERVER (`api.getReplyCard`), not matched against
//     the rows the page happens to hold. Round 1 was dishonest precisely
//     because it filtered loaded rows: a card older than the handled pane's 24h
//     window came back as 「沒有符合篩選條件的請示」— the same sentence a card
//     that does not exist produces. The owner read that collapse as "no such
//     card" in review, and it is the whole reason this ticket exists.
//   · found / 404 / server-never-reached stay THREE distinguishable screens.
//   · 近期已處理 stays on screen saying 0 while a filter is applied.
//   · a card no pane carries is still findable.
//   · typing is still inert. What changed is only WHEN a draft commits: Enter
//     or blur (「按enter或是點外面就視為apply了」) instead of 套用篩選. An applied
//     id is a server request, so a commit per keystroke is a fetch per
//     keystroke — the shape owner rejected twice before this round.

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

function idField(): HTMLInputElement {
  return document.querySelector(
    '[data-testid="filter-reply-card-id"]'
  ) as HTMLInputElement;
}

/** Type an id into 請示卡編號 and COMMIT it with Enter.
 *
 * 🔁 WAS 「open the funnel → type → press 套用篩選」. OVERTURNED BY owner
 * 2026-09-06 (c-c3d681fe05da):「不要多filter那一層了…按enter或是點外面就視為
 * apply了」— there is nothing to open and no button to press. Mirrors
 * `src/test/tasksFilter.ts#applyIdFilter`, deliberately: the two pages wear the
 * same gesture now, and a helper that drifted would hide that. */
function applyId(id: string) {
  fireEvent.change(idField(), { target: { value: id } });
  fireEvent.keyDown(idField(), { key: "Enter" });
}

/** Type into 請示卡編號 and STOP — no Enter, no blur, so nothing is applied.
 * This is the half of the gesture the owner asked to be inert. */
function typeId(id: string) {
  fireEvent.change(idField(), { target: { value: id } });
}

/** Commit whatever is in the box by clicking away from it (「點外面」). */
function blurId() {
  fireEvent.blur(idField());
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

describe("請示 ID 篩選（常駐欄位版，T-118）", () => {
  it("a link carrying a card id shows only that card, out of several waiting", async () => {
    // 🔁 KEPT — the link behaviour survives untouched. What changed is the last
    // assertion: it used to read the 已篩選 chip (「編號：rc-bbb」), because a
    // COLLAPSED panel could otherwise hide a live filter. OVERTURNED BY owner
    // 2026-09-06 (c-c3d681fe05da):「也不用再顯示14筆已篩選跟那一行」— with the
    // field permanently on screen the field itself is what says an id is
    // applied, which is exactly what made removing the strip safe.
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));
    __injectMockReplyCard(mkCard({ id: "rc-ccc", summary: "第三張" }));

    const { findAllByTestId, findByTestId } = renderPage("rc-bbb");

    const cards = await findAllByTestId("waiting-card");
    expect(cards).toHaveLength(1);
    expect(cards[0].textContent).toContain("第二張");
    expect(
      ((await findByTestId("filter-reply-card-id")) as HTMLInputElement).value,
      "the field is the only thing left that says why one card is here"
    ).toBe("rc-bbb");
  });

  it("the 編號 field is on the page from the first render, already holding the applied id", async () => {
    // 🔁 REPLACES 「opening the panel shows the APPLIED id in the field, not a
    // blank one」. That spec pinned the funnel + draft-replay of round 2/3.
    // OVERTURNED BY owner 2026-09-06 (c-c3d681fe05da):「不要多filter那一層了,
    // 全部拉出來」. There is no panel to open, so the property the old spec
    // guarded (「the field must not lie about what is applied」) is now a
    // first-render fact, and the affordances that gated it must be GONE rather
    // than merely hidden.
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findByTestId, queryByTestId } = renderPage("rc-bbb");
    expect(await findByTestId("waiting-card")).toBeTruthy();

    expect(
      ((await findByTestId("filter-reply-card-id")) as HTMLInputElement).value
    ).toBe("rc-bbb");
    // The shell is still there — it is the ROW now, not an expander.
    expect(await findByTestId("replies-filter")).toBeTruthy();
    expect(await findByTestId("replies-filter-fields")).toBeTruthy();
    for (const gone of [
      "replies-filter-toggle",
      "replies-filter-form",
      "replies-filter-apply",
      "replies-filter-cancel",
    ]) {
      expect(queryByTestId(gone), `${gone} must not exist any more`).toBeNull();
    }
  });

  it("🔴 typing in 請示卡編號 without Enter and without blur applies NOTHING — no request, and the same cards", async () => {
    // 🆕 T-118's own guard, and the single most important one in this file.
    // owner 2026-09-06 (c-c3d681fe05da) named exactly one timing —「按enter或是
    // 點外面就視為apply了」— and an applied id here is `api.getReplyCard`, so a
    // version that commits on change is a REQUEST PER KEYSTROKE: the shape he
    // rejected twice (「每次都要全部都撈回來才濾不合理」, and an independent
    // review's 「一個字元一個請求」 on this very page). The 任務頁 carries the
    // equivalent guard; without this one 請示卡 would be the weaker of the two
    // and the next person would "tidy" onCommit back onto onChange.
    const getSpy = vi.spyOn(api, "getReplyCard");
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findAllByTestId, queryByText } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);

    for (const value of ["r", "rc", "rc-", "rc-b", "rc-bb", "rc-bbb"]) {
      typeId(value);
    }
    // Give any (wrongly) scheduled effect a chance to land before concluding.
    await Promise.resolve();
    await waitFor(() => expect(idField().value).toBe("rc-bbb"));

    expect(
      getSpy,
      "six keystrokes must cost zero requests"
    ).not.toHaveBeenCalled();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);
    expect(queryByText("第一張"), "typing alone must not narrow the list").toBeTruthy();
    expect(queryByText("第二張")).toBeTruthy();
  });

  it("Enter applies the typed id, and asks the server for it exactly once", async () => {
    // 🔁 REPLACES 「typing changes NOTHING until 套用篩選」. That spec pinned the
    // draft/apply split of round 2/3. OVERTURNED BY owner 2026-09-06
    // (c-c3d681fe05da): the commit is Enter now, not a button. The half that
    // matters is UNCHANGED and lives in the spec above — typing costs nothing;
    // this one holds the other half, that a COMMITTED id costs exactly one read.
    const getSpy = vi.spyOn(api, "getReplyCard");
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findAllByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);

    applyId("rc-bbb");

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(1)
    );
    expect((await findAllByTestId("waiting-card"))[0].textContent).toContain(
      "第二張"
    );
    expect(getSpy).toHaveBeenCalledTimes(1);
    expect(getSpy).toHaveBeenCalledWith("rc-bbb");
  });

  it("clicking away from the field applies it too (「點外面」)", async () => {
    // 🔁 REPLACES 「Cancel commits nothing, and the abandoned draft does not
    // survive to the next open」. That spec pinned 取消 and the panel's
    // draft-replay. OVERTURNED BY owner 2026-09-06 (c-c3d681fe05da): both
    // buttons are gone, so there is no abandon — the second door he named is
    // blur, and a version that wires only Enter passes the spec above while
    // still failing him.
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findAllByTestId, queryByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);

    typeId("rc-bbb");
    expect(
      await findAllByTestId("waiting-card"),
      "still nothing applied while the text just sits there"
    ).toHaveLength(2);

    blurId();

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(1)
    );
    expect((await findAllByTestId("waiting-card"))[0].textContent).toContain(
      "第二張"
    );
    expect(queryByTestId("replies-filter-cancel")).toBeNull();
  });

  it("the 已篩選 strip, its chips and 清除全部 are gone — applied or not", async () => {
    // 🔁 REPLACES 「the chip's × drops that one axis, hash included」.
    // OVERTURNED BY owner 2026-09-06 (c-c3d681fe05da):「也不用再顯示14筆已篩選跟
    // 那一行」. The strip existed to keep a COLLAPSED panel honest; with the
    // field permanently on screen there is no collapsed state to protect
    // against. The ESCAPE the × offered did not go with it — it is the spec
    // below (empty the box and commit), which also keeps the hash half.
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));

    const { findAllByTestId, queryByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);
    for (const gone of [
      "replies-filter-summary",
      "replies-filter-chip",
      "replies-filter-chip-x",
      "replies-filter-clear",
    ]) {
      expect(queryByTestId(gone), `${gone} must not exist any more`).toBeNull();
    }

    applyId("rc-bbb");
    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(1)
    );
    // Still no strip with a filter ON — and the field is what says so.
    expect(queryByTestId("replies-filter-summary")).toBeNull();
    expect(queryByTestId("replies-filter-chip")).toBeNull();
    expect(idField().value).toBe("rc-bbb");
  });

  it("the 「請示卡」 sub-title row above the list is gone", async () => {
    // 🔁 REPLACES the panel-header half of the round-3 specs (the row that
    // carried 「請示卡」 + the funnel). OVERTURNED BY owner 2026-09-06
    // (c-c3d681fe05da):「也不用再顯示…跟案件那個子標了,案件跟請示卡都一樣」,
    // restated for this page at 20:19 (c-38c7759e6377):「一樣請示卡的子標題拿
    // 掉」. The nav already names the page; this was a second, redundant title
    // sitting directly above the list.
    //
    // Asserted on the CLASSES rather than on the text: 「請示卡」 is a substring
    // of the field's own label 請示卡編號, so a text query would go green on a
    // page that still rendered the header and red on one that merely renamed
    // the field.
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    const { findByTestId } = renderPage();
    await findByTestId("filter-reply-card-id");

    expect(
      document.querySelector(".filter-panel__header"),
      "the header row that carried 請示卡 + the funnel must not exist"
    ).toBeNull();
    expect(document.querySelector(".filter-panel__title")).toBeNull();
  });

  it("🔴 404 renders the ordinary filtered-empty result — the bespoke notice is gone by owner ruling", async () => {
    // 🔁 KEPT, gesture untouched (a hash-seeded id needs no gesture at all).
    // 🔴 owner 2026-09-06 (rc-f603bbd447f4 →「為什麼要顯示這種東西 拿掉!」→
    // 「UI不是本來就秀0筆了嗎」). Round 2's dedicated 404 sentence is removed;
    // a 404 now falls through to 沒有符合篩選條件的請示.
    //
    // WHAT THIS SPEC STILL HOLDS, and why removing the sentence did not undo
    // round 1's defect: round 1 was dishonest because the page FILTERED THE
    // CARDS IT HAPPENED TO HOLD, so a card that merely had not been fetched and
    // a card that does not exist produced the same screen as a matter of fact.
    // The by-id lookup asks the SERVER, so those are now two different facts —
    // and the spec below this one pins the half that must still look different:
    // a server we never reached may NOT render this sentence.
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));

    const { findByTestId, queryByTestId } = renderPage("rc-nope");

    await findByTestId("replies-empty");
    expect(queryByTestId("replies-lookup-missing")).toBeNull();
    expect(queryByTestId("replies-lookup-failed")).toBeNull();
    // Non-vacuity: the card that DOES exist is not on screen either — the id
    // really replaced the list rather than the list being empty by accident.
    expect(queryByTestId("replies-list")).toBeNull();
  });

  it("🔴 a non-404 failure says the server was never reached, and never says 找不到", async () => {
    // 🔁 KEPT — only the GESTURE moved (funnel → type → 套用篩選 became type →
    // Enter, owner 2026-09-06 c-c3d681fe05da). The rule is untouched.
    // MUTANT (and the exact wrong thing to do): treat every rejection as
    // "missing". Then an offline cockpit tells the owner a card he is looking
    // at in another window does not exist.
    vi.spyOn(api, "getReplyCard").mockRejectedValue(
      new ApiError("http 500 for GET /api/reply-cards/rc-x", 500, "internal_error", "boom")
    );
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));

    const { findByTestId, queryByTestId } = renderPage();
    await findByTestId("filter-reply-card-id");
    applyId("rc-x");

    const failed = await findByTestId("replies-lookup-failed");
    expect(failed.textContent).toContain("沒能問到伺服器");
    expect(failed.textContent).not.toContain("找不到");
    expect(queryByTestId("replies-lookup-missing")).toBeNull();
    expect(queryByTestId("replies-empty")).toBeNull();
  });

  it("with no filter at all the ✓ copy is still the one that shows", async () => {
    // 🔁 KEPT verbatim. Non-vacuity control: the copies really are different
    // strings and this page really can still produce the ✓ one.
    const { findByTestId } = renderPage();
    expect((await findByTestId("replies-empty")).textContent).toBe(
      "✓ 目前沒有待處理的請示"
    );
  });

  it("emptying the 編號 field and clicking away restores every card AND drops the id from the URL", async () => {
    // 🔁 REPLACES 「清除全部 restores every card AND drops the id from the URL」.
    // That button lived on the 已篩選 strip and was REMOVED WITH IT by owner
    // 2026-09-06 (c-c3d681fe05da). The BEHAVIOUR it guarded is still required
    // and is still the owner's only escape from a filter he did not mean to
    // apply — it just has no dedicated control any more, so the gesture is
    // emptying the box where the reader is already looking.
    // 🔴 The URL half is the part that must not be lost: leave the id in the
    // hash and a refresh re-applies the filter, which reads as "clear is
    // broken". This one clears by BLUR; the spec below clears by Enter, because
    // both doors have to take the hash with them.
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));
    window.location.hash = "#replies/card/rc-bbb";

    const { findAllByTestId } = renderPage("rc-bbb");
    expect(await findAllByTestId("waiting-card")).toHaveLength(1);

    typeId("");
    blurId();

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(2)
    );
    expect(window.location.hash).toBe("#replies");
  });

  it("committing an EMPTY field with Enter clears the filter and the hash with it", async () => {
    // 🔁 KEPT — only the GESTURE moved (「套用篩選」 → Enter, owner 2026-09-06
    // c-c3d681fe05da). 🔴 The round-1 version of this hole: the owner could
    // delete the value by hand and be left on #replies/card/<id> with the hash
    // still filtering after a reload. The draft/applied split moves the hole
    // rather than closing it — committing an empty draft must take the stale
    // hash id with it, or the next reload seeds the old card straight back.
    __injectMockReplyCard(mkCard({ id: "rc-aaa", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-bbb", summary: "第二張" }));
    window.location.hash = "#replies/card/rc-bbb";

    const { findAllByTestId } = renderPage("rc-bbb");
    expect(await findAllByTestId("waiting-card")).toHaveLength(1);

    applyId("");

    await waitFor(async () =>
      expect(await findAllByTestId("waiting-card")).toHaveLength(2)
    );
    expect(window.location.hash).toBe("#replies");
  });

  it("a link to a WAITING card does not fetch the handled pane", async () => {
    // 🔁 KEPT verbatim — no gesture at all in this one. Still true, and still
    // worth pinning: the handled lists are two requests for a pane the owner
    // never asked about. What CHANGED from round 1 is the 「· N」 — with a
    // filter applied it is now「how many cards match」(0 here), not the server's
    // whole-pane count, because the id is answered by a single by-id read
    // rather than by narrowing loaded rows.
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
    // 🔁 KEPT — only the GESTURE moved (owner 2026-09-06, c-c3d681fe05da).
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
    await findByTestId("filter-reply-card-id");
    applyId("rc-live");

    await waitFor(async () =>
      expect((await findByTestId("answered-toggle")).textContent).toContain(
        "近期已處理 · 0"
      )
    );
  });

  it("🔴 finds a card NO pane carries — answered three days ago, invisible to round 1", async () => {
    // 🔁 KEPT — only the GESTURE moved (owner 2026-09-06, c-c3d681fe05da).
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

    applyId("rc-ancient");

    expect(await findByText("三天前回過的")).toBeTruthy();
    expect(await findByTestId("answered-card")).toBeTruthy();
  });

  it("a found card in the COLLAPSED handled pane is rendered, not hidden behind the fold", async () => {
    // 🔁 KEPT verbatim — the 「找到了但畫面上沒有」 guard on its own: arrive with
    // the pane shut (its default) and never touch the toggle.
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
