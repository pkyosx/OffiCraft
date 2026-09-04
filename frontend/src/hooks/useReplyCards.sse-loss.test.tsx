// A successful reply-card WRITE must reconcile the cockpit BY ITSELF — with no
// event stream at all (T-a3e4 step 8 follow-up).
//
// The defect this pins: T-a3e4 step 8 removed the action-path refetch and left
// the `reply_card` delta as the ONLY reconcile trigger for answer / expire (等我
// 回覆 page + nav badge) and for the inline chat card's first answer. That is
// correct only while the stream is up. With the EventSource disconnected or one
// frame missed, the server has ACCEPTED the answer while the cockpit still
// renders the card as waiting: the owner clicks the already-handled card again
// and hits a 409, and the nav waiting badge stays wrong until reconnect /
// foreground resync / reload.
//
// 🔴 WHY THE CHECKED-IN TESTS CANNOT SEE THIS. The mock adapter fans its own
// `reply_card` topic SYNCHRONOUSLY from inside answerReplyCard / expireReplyCard
// (`emitTopic`), so every existing test gets the delta for free and the
// event-less case is never exercised. This file removes exactly one thing —
// the event subscription — and changes nothing else: still the REAL mock
// adapter, still a real click on a real rendered card.
//
// 🔴 AND IT ASSERTS PIXELS, NOT CALL COUNTS, on purpose — the mirror image of
// `useReplyCards.one-round.test.tsx`. Its budget does NOT tolerate zero (the
// assertion is `=== 1`; deleting the SSE reconcile branch so the action path
// costs zero rounds reddens 3 of its tests — measured, do not repeat the earlier
// version of this note, which claimed zero satisfied it). The gap is RANGE: that
// budget measures how many rounds a write costs WITH THE STREAM UP, and the
// pre-fix code did cost exactly one there — so it has nothing to say about the
// stream being down. Cost and correctness need one witness each; neither
// assertion can stand in for the other.
//
// The fix these tests demand costs no extra round trip: the write's OWN response
// already carries the fresh card (`answerReplyCard` / `expireReplyCard` /
// `reanswerReplyCard` all return `ReplyCard`), so the action path adopts it. The
// one-round budget is therefore untouched — both files are green together.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "../components/RepliesPage";
import { ChatReplyCard } from "../components/ChatReplyCard";
import { TaskReplyCard } from "../components/TaskReplyCard";
import { ReplyCardsProvider } from "./useReplyCards";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import { api } from "../api";
import type { ReplyCard } from "../api/adapter";

/** 🔴 開卡時間必須逐張不同,而且同一個 id 每次都拿到同一個戳記。
 *
 * 這個 fixture 以前對每張卡算 `Date.now()/1000 - 25*60`,於是三張卡的戳記在
 * 「同一毫秒內注入完」時**相等** —— 而等待面的顯示順序完全由戳記決定:mock
 * adapter 依 `createdTs` **升冪**出清單(最久沒回的在前),`RepliesPage` 再依
 * `createdTs` **降冪**重排。戳記相等 ⇒ 兩次排序都退化成 stable sort 的「維持
 * 原順序」,順序就變成「`Date.now()` 有沒有在兩次 `mkCard` 之間跳一格」的函式:
 * 沒跳 → [rc-1, rc-2];跳了 → [rc-2, rc-1](第二張變成比較新的那張)。
 * 完整 CI 那種負載下毫秒邊界很容易被跨過,於是靠位置點卡的測試會改點到 rc-2,
 * 剩下的正好是 `[rc-1, rc-3]` —— 那就是實際看到的紅。**這不是產品排序不保證,
 * 是這個 fixture 沒有把排序講清楚。**
 *
 * 因此:BASE 在 module 載入時取一次(同 id 永遠同戳記,重建快照不會重排),
 * 偏移由 id 的數字尾碼決定 —— rc-1 最舊、rc-3 最新 ⇒ 等待面的顯示順序恆為
 * [rc-3, rc-2, rc-1]。⚠️ 順序既然是明確的,選卡就更不該靠位置:見
 * `waitingCardById`。 */
const FIXTURE_BASE_TS = Date.now() / 1000 - 25 * 60;

function createdTsFor(id: string): number {
  const m = /^rc-(\d+)$/.exec(id);
  return FIXTURE_BASE_TS + (m ? Number(m[1]) * 60 : 0);
}

function mkCard(over: Partial<ReplyCard> = {}): ReplyCard {
  const id = over.id ?? "rc-1";
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
    createdTs: createdTsFor(id),
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

/** The whole point of this file: the cockpit is subscribed to NOTHING. Installed
 * before render, so the provider's mount effect gets the no-op too. */
function killEventStream() {
  return vi.spyOn(api, "subscribeEvents").mockImplementation(() => () => {});
}

/** A stream that is alive at mount and whose LAST frame we fire by hand — the
 * shape of an EventSource that dies right after delivering one delta. Returns
 * the captured handler. */
function captureEventStream() {
  let fire: ((topic: string) => void) | null = null;
  vi.spyOn(api, "subscribeEvents").mockImplementation((onTopic) => {
    fire = (topic: string) => onTopic(topic);
    return () => {
      fire = null;
    };
  });
  return {
    deliver: (topic: string) => {
      if (!fire) throw new Error("nobody subscribed");
      fire(topic);
    },
  };
}

/** Makes the NEXT `listReplyCards(<pane>)` hang, and hands back its resolver
 * so the test decides exactly when that (by then stale) snapshot lands. Every
 * other call goes to the real mock adapter. */
function hangNextRead(pane: "waiting" | "answered" | "expired") {
  const real = api.listReplyCards.bind(api);
  let release: ((rows: ReplyCard[]) => void) | null = null;
  let armed = true;
  vi.spyOn(api, "listReplyCards").mockImplementation((status) => {
    if (armed && status === pane) {
      armed = false;
      return new Promise<ReplyCard[]>((resolve) => {
        release = resolve;
      });
    }
    return real(status);
  });
  return {
    inFlight: () => release !== null,
    landWith: (rows: ReplyCard[]) => {
      if (!release) throw new Error(`no ${pane} read is in flight`);
      release(rows);
    },
  };
}

/** 🔴 要回答哪一張卡,用 id 指名 —— 這些測試**靠 id 斷言結果**,所以也必須靠 id
 * 選擇輸入,否則「點到哪一張」與「該剩下哪一張」是兩套座標系,排序一動就對不上
 * (那正是上面那顆紅的形狀)。回傳的是等待面上那一張,`#reply-card-<id>` 在
 * 已處理面也存在,所以要連 testid 一起限定。 */
function waitingCardById(id: string): HTMLElement {
  const el = document.querySelector<HTMLElement>(
    `[data-testid="waiting-card"]#reply-card-${id}`
  );
  if (!el) throw new Error(`no waiting card rendered for ${id}`);
  return el;
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

describe("reply-card writes reconcile without any event stream", () => {
  it("answering a waiting card removes it from the pane with the stream DOWN", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));
    __injectMockReplyCard(mkCard({ id: "rc-3", summary: "第三張" }));

    killEventStream();

    const { findAllByTestId, queryAllByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(3);

    fireEvent.click(
      waitingCardById("rc-1").querySelectorAll(".reply-option")[0]
    );

    // The write landed (the mock accepted it and flipped the card to answered).
    // The cockpit must not keep rendering it as waiting — that is what sends the
    // owner back into an already-handled card for a 409. The nav badge reads the
    // length of this SAME array (T-e862 同源化), so this assertion covers it too.
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(2)
    );
  });

  it("expiring a waiting card removes it from the pane with the stream DOWN", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));

    killEventStream();

    const { findAllByTestId, findByTestId, queryAllByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);

    fireEvent.click(
      waitingCardById("rc-1").querySelector('[data-testid="expire-card"]')!
    );
    const confirm = await findByTestId("expire-confirm-btn");
    fireEvent.click(confirm);

    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(1)
    );
  });

  it("an in-flight PRE-WRITE snapshot cannot paint the answered card back", async () => {
    // 🔴 The reason adopting is not enough on its own, and it is NOT an exotic
    // race: the only precondition is "a delta arrived shortly before the click",
    // i.e. the stream was alive and then dropped — which is the ordinary shape of
    // an EventSource dying (the last frame it delivered left a refetch in
    // flight). The outcome is identical to the original blocker: the card is
    // waiting again, the badge is wrong again, and with the stream down there is
    // no newer refetch to correct it. T-e862's generation guard does not help —
    // it only drops a snapshot once a NEWER one exists.
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));

    const stream = captureEventStream();
    const { findAllByTestId, queryAllByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);

    // A peer's delta kicks a refetch — and that read never comes back before the
    // owner acts (slow response, then the stream drops).
    const read = hangNextRead("waiting");
    stream.deliver("reply_card");
    await waitFor(() => expect(read.inFlight()).toBe(true));

    fireEvent.click(
      waitingCardById("rc-1").querySelectorAll(".reply-option")[0]
    );
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(1)
    );

    // Now the pre-write snapshot lands: rc-1 still listed as waiting, because it
    // was read before the answer was accepted.
    read.landWith([mkCard({ id: "rc-1" }), mkCard({ id: "rc-2" })]);

    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(1)
    );
    expect(
      queryAllByTestId("waiting-card").map((el) => el.id)
    ).toEqual(["reply-card-rc-2"]);
  });

  it("...and that same snapshot's NEW card still arrives", async () => {
    // 🔴 This is the half that rejects the cheap fix. Bumping the generation (or
    // otherwise DISCARDING the in-flight snapshot) turns the test above green,
    // and throws away everything else that snapshot carried — including a card a
    // peer just opened. With the stream down no later refetch brings it back, so
    // that trade is one silent failure for another: the owner is simply never
    // told there is a card waiting for them. The hold is therefore per-id.
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));

    const stream = captureEventStream();
    const { findAllByTestId, queryAllByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);

    const read = hangNextRead("waiting");
    stream.deliver("reply_card");
    await waitFor(() => expect(read.inFlight()).toBe(true));

    fireEvent.click(
      waitingCardById("rc-1").querySelectorAll(".reply-option")[0]
    );
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(1)
    );

    // The same stale snapshot ALSO carries a card someone else just opened.
    read.landWith([
      mkCard({ id: "rc-1" }),
      mkCard({ id: "rc-2" }),
      mkCard({ id: "rc-3", summary: "別人剛開的" }),
    ]);

    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(2)
    );
    expect(
      queryAllByTestId("waiting-card").map((el) => el.id).sort()
    ).toEqual(["reply-card-rc-2", "reply-card-rc-3"]);
  });

  it("an in-flight PRE-WRITE read cannot put the inline card's options back", async () => {
    // 🔴 The THIRD site of the same class, and the one the two tests above are
    // blind to: they kill the stream from the start (`killEventStream`), so no
    // read is ever in flight. ChatReplyCard fetches on EVERY reply_card delta
    // while its card is still waiting, so the ordinary "stream was alive, then
    // dropped" shape leaves exactly such a read in flight — and it lands after
    // the answer, re-seeding statusRef to "waiting" and re-rendering the chips.
    __injectMockReplyCard(mkCard({ id: "rc-inline", summary: "要寄出嗎?" }));

    const stream = captureEventStream();
    const realGet = api.getReplyCard.bind(api);
    let release: ((c: ReplyCard) => void) | null = null;
    let armed = false;
    vi.spyOn(api, "getReplyCard").mockImplementation((id) => {
      if (armed) {
        armed = false;
        return new Promise<ReplyCard>((resolve) => {
          release = resolve;
        });
      }
      return realGet(id);
    });

    const { container } = render(
      <I18nProvider>
        <ChatReplyCard
          replyCardId="rc-inline"
          fallbackSummary="要寄出嗎?"
          initialStatus={null}
        />
      </I18nProvider>
    );
    // Every inline card mounts collapsed (owner 2026-09-04); open it so there
    // are chips for the stale read to try to put back.
    fireEvent.click(container.querySelector(".reply-card__collapsed-row")!);
    await waitFor(() =>
      expect(container.querySelector(".reply-option")).toBeTruthy()
    );

    // A peer's delta kicks this card's own refetch; it hangs, then the stream dies.
    armed = true;
    stream.deliver("reply_card");
    await waitFor(() => expect(release).not.toBeNull());

    fireEvent.click(container.querySelectorAll(".reply-option")[0]);
    await waitFor(() =>
      expect(container.querySelector(".reply-card__answer-text")).toBeTruthy()
    );

    // The pre-write card lands last. It must not un-answer the card.
    release!(mkCard({ id: "rc-inline", summary: "要寄出嗎?" }));

    await waitFor(() =>
      expect(container.querySelector(".reply-card__answer-text")).toBeTruthy()
    );
    expect(container.querySelectorAll(".reply-option")).toHaveLength(0);
  });

  it("an in-flight PRE-WRITE read cannot put the TASK card's options back", async () => {
    // The FIFTH site, found by sweeping rather than reported: TaskReplyCard (the
    // gate card embedded in a task) fetches on every delta the same way.
    // ⚠️ Its exposure is narrower — `doAnswer` there DOES refetch, so it never
    // depended on the stream — but ORDERING between that post-write read and an
    // older in-flight one was missing all the same.
    __injectMockReplyCard(mkCard({ id: "rc-gate", summary: "要放行嗎?" }));

    const stream = captureEventStream();
    const realGet = api.getReplyCard.bind(api);
    let release: ((c: ReplyCard) => void) | null = null;
    let armed = false;
    vi.spyOn(api, "getReplyCard").mockImplementation((id) => {
      if (armed) {
        armed = false;
        return new Promise<ReplyCard>((resolve) => {
          release = resolve;
        });
      }
      return realGet(id);
    });

    const { container } = render(
      <I18nProvider>
        <TaskReplyCard replyCardId="rc-gate" initialStatus={null} />
      </I18nProvider>
    );
    await waitFor(() =>
      expect(container.querySelector(".reply-option")).toBeTruthy()
    );

    armed = true;
    stream.deliver("reply_card");
    await waitFor(() => expect(release).not.toBeNull());

    fireEvent.click(container.querySelectorAll(".reply-option")[0]);
    await waitFor(() =>
      expect(container.querySelector(".reply-card__answer-text")).toBeTruthy()
    );

    release!(mkCard({ id: "rc-gate", summary: "要放行嗎?" }));

    await waitFor(() =>
      expect(container.querySelector(".reply-card__answer-text")).toBeTruthy()
    );
    expect(container.querySelectorAll(".reply-option")).toHaveLength(0);
  });

  it("an in-flight PRE-WRITE handled snapshot cannot drop the card just answered", async () => {
    // 🔴 The FOURTH site. "The pane is collapsed by default" does NOT make this
    // unreachable: expanding it once sets handledLoaded for the rest of the visit,
    // and from then on every delta refetches the handled lists — so the same
    // in-flight snapshot lands on 近期已處理 and takes the card the owner just
    // answered back out of it. (Expanding once is the entire point of that pane.)
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(
      mkCard({
        id: "rc-old",
        summary: "早先答過的",
        status: "answered",
        answeredTs: Date.now() / 1000 - 3600,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );

    const stream = captureEventStream();
    const { findAllByTestId, findByTestId, queryAllByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(1);

    // Expand 近期已處理 once — that is what arms the per-delta handled refetch.
    fireEvent.click(await findByTestId("answered-toggle"));
    await waitFor(() =>
      expect(queryAllByTestId("answered-card")).toHaveLength(1)
    );

    const read = hangNextRead("answered");
    stream.deliver("reply_card");
    await waitFor(() => expect(read.inFlight()).toBe(true));

    const cards = await findAllByTestId("waiting-card");
    fireEvent.click(cards[0].querySelectorAll(".reply-option")[0]);
    await waitFor(() =>
      expect(queryAllByTestId("answered-card")).toHaveLength(2)
    );

    // The pre-write answered snapshot lands. It knows nothing about the card the
    // owner just answered — but it DOES carry one a peer answered, and that is
    // what makes this test discriminating: waiting for the peer's card proves the
    // stale snapshot was really committed.
    // 🔴 Without it the assertion below is satisfied by the state adoption
    // already left behind and the test passes on broken code — measured: the
    // first version of this test was GREEN against a mutant with the whole
    // handled merge deleted.
    read.landWith([
      mkCard({
        id: "rc-old",
        status: "answered",
        answeredTs: Date.now() / 1000 - 3600,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      }),
      mkCard({
        id: "rc-peer",
        summary: "別人答的",
        status: "answered",
        answeredTs: Date.now() / 1000 - 120,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      }),
    ]);

    // The snapshot committed (rc-peer is on screen) AND the card the owner just
    // answered is still in 近期已處理 — three, not two.
    await waitFor(() =>
      expect(
        queryAllByTestId("answered-card").map((el) => el.id).sort()
      ).toEqual(["reply-card-rc-1", "reply-card-rc-old", "reply-card-rc-peer"])
    );
  });

  it("a PRE-WRITE handled snapshot with an OLDER stamp cannot revert a 重新決定", async () => {
    // 🔴 THE STAMP COMPARISON'S OWN WITNESS. The handled hold releases on
    // "listed AND its stamp is at least as new as ours" — the `>=` in
    // refetchHandled — precisely because a 重新決定 RE-STAMPS answeredTs, so a
    // pre-write snapshot lists the very same card with the OLD stamp and the OLD
    // answer. Presence alone must not count as confirmation.
    //
    // ⚠️ The test above cannot see that arm at all: a first answer takes the
    // `i < 0` (push) path, so the whole comparison is skipped. Measured:
    // replacing the comparison with `true` (the degenerate "presence confirms"
    // version this file argues against) left all 8 of those tests green.
    const now = Date.now() / 1000;
    const answeredFixture = (over: Partial<ReplyCard> = {}): ReplyCard =>
      mkCard({
        id: "rc-A",
        options: [{ text: "OPT-ZERO", aiPick: true }, { text: "OPT-ONE", aiPick: false }],
        selectMode: "single",
        status: "answered",
        createdTs: now - 3600,
        answeredTs: now - 600,
        answer: { optionIdxs: [0], text: "", attachments: [] },
        ...over,
      });
    __injectMockReplyCard(answeredFixture());

    const stream = captureEventStream();
    const { findByTestId, findAllByTestId } = renderPage();
    fireEvent.click(await findByTestId("answered-toggle"));
    const cards = await findAllByTestId("answered-card");
    expect(cards).toHaveLength(1);
    const answerOf = (el: HTMLElement) =>
      el.querySelector(".reply-card__answer-option")?.textContent ?? "";
    expect(answerOf(cards[0])).toBe("OPT-ZERO");

    // A peer's delta kicks the handled refetch; it hangs, holding a snapshot
    // taken BEFORE the revision below.
    const read = hangNextRead("answered");
    stream.deliver("reply_card");
    await waitFor(() => expect(read.inFlight()).toBe(true));

    // 重新決定 → OPT-ONE.
    fireEvent.click(cards[0].querySelector(".reply-card__toggle")!);
    fireEvent.click(cards[0].querySelector(".reply-card__redecide")!);
    fireEvent.click(cards[0].querySelectorAll(".reply-option")[1]);
    await waitFor(async () =>
      expect(answerOf((await findAllByTestId("answered-card"))[0])).toBe(
        "OPT-ONE"
      )
    );

    // The stale snapshot lands: SAME card, OLDER stamp, OLD answer. Taking it
    // would show the owner the answer they just replaced.
    read.landWith([answeredFixture()]);

    await waitFor(async () => {
      const rows = await findAllByTestId("answered-card");
      expect(rows).toHaveLength(1);
      expect(answerOf(rows[0])).toBe("OPT-ONE");
    });
    // Settle any commit the stale snapshot may still be queueing, then re-assert
    // — without this the check above can pass on state that is about to be
    // overwritten (the trap the handled test above fell into on its first draft).
    await new Promise((r) => setTimeout(r, 40));
    const rows = await findAllByTestId("answered-card");
    expect(rows).toHaveLength(1);
    expect(answerOf(rows[0])).toBe("OPT-ONE");
  });

  it("an inline chat card flips to answered in place with the stream DOWN", async () => {
    // The third site (ChatReplyCard.doAnswer). Same write, same missing delta:
    // the card the owner just answered keeps showing its option chips.
    __injectMockReplyCard(mkCard({ id: "rc-inline", summary: "要寄出嗎?" }));

    killEventStream();

    const { container } = render(
      <I18nProvider>
        <ChatReplyCard
          replyCardId="rc-inline"
          fallbackSummary="要寄出嗎?"
          initialStatus={null}
        />
      </I18nProvider>
    );
    // Every inline card mounts collapsed (owner 2026-09-04); open it.
    fireEvent.click(container.querySelector(".reply-card__collapsed-row")!);
    await waitFor(() =>
      expect(container.querySelector(".reply-option")).toBeTruthy()
    );

    fireEvent.click(container.querySelectorAll(".reply-option")[0]);

    await waitFor(() =>
      expect(container.querySelector(".reply-card__answer-text")).toBeTruthy()
    );
  });
});
