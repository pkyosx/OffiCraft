// hooks/useReplyCards.tsx — the 等我回覆 page's data AND the nav badge's waiting
// count, from ONE shared source (T-e862 同源化). The WAITING list is the
// always-live pane (mount + every "reply_card" SSE delta), fetched alongside
// the cheap counts. The HANDLED list (answered + expired, merged newest-first
// by their handled stamp) is DEFERRED (owner 已回覆卡預設不載 / 收合狀態下不
// fetch): the collapsed 近期已處理 pane shows its 「· N」 header from the
// counts alone (answered + expired), and the lists are pulled only when the
// owner expands the pane (loadHandled). Reconcile-by-refetch (contract B): a
// delta REFETCHES — the waiting list + counts always, and the handled lists
// only when currently loaded (expanded) — never merges an event payload. The
// answer/re-answer/expire actions do NOT refetch directly — see T-a3e4 step 8
// below.
//
// T-e862 (狀態競態修復):
//  ① REQUEST SEQUENCING. refetchWaiting/refetchHandled are fired concurrently
//     and out of order (a resolve + its own reply_card fan-out + a peer's new
//     card each kick one), each ending in a bare setWaiting(w). With no
//     ordering guard the LAST promise to resolve won — a late-arriving STALE
//     snapshot could clobber a newer, fuller one and silently drop a card
//     (badge said 2, list showed 1, until refresh). Each refetch now stamps a
//     monotonic generation id and only commits if it is still the latest —
//     late stale responses are dropped, killing the last-write-wins.
//  ② SAME SOURCE for badge + list + title. The nav badge (useReplyCardCount)
//     used to ride a SEPARATE count-endpoint fetch on a SEPARATE hook with its
//     OWN SSE subscription, so it and the list sat on two different snapshots
//     from two different instants — the structural crack behind "badge 2 /
//     list 1". The waiting list now lives in ONE app-wide provider; the badge
//     is literally waiting.length off that same authoritative array, and the
//     page title 「待回覆 · N」 reads the same length. One source, one
//     subscription — they cannot disagree.
//
// T-a3e4 step 8 (一次動作只重抓一輪):
//   ONE owner action used to cost TWO complete refetch rounds. The action path
//   (answer/reanswer/expire → refetchAfterAction) and the SSE handler each
//   fired their own refetch for the SAME write, because the server publishes
//   its `reply_card` delta for the owner's own write too. ①'s generation guard
//   dedupes the COMMIT, not the REQUEST — the loser's response was downloaded
//   in full and then thrown away. Measured against a real ocserverd over a
//   25-card waiting pane: one answered card = 48 per-card GETs (24 cards × 2)
//   and 100,952 B, against 25 GETs / 51,599 B for the same pane on mount.
//   An isolation control pinned the cause: a delta the cockpit did NOT cause
//   (someone else opening a card) produced exactly ONE round, so the second
//   round belonged to the local action path, not to a doubled stream.
//
//   ⇒ The actions no longer refetch. 🔴 But they DO reconcile: each action
//   folds the transition its own write returned into the card this pane had
//   already read (`adoptWrite` below), which costs zero requests. T-91 changed
//   that fold from a REPLACE to a MERGE, and did NOT rename the function: it is
//   still called `adoptWrite`, but it no longer adopts the answer wholesale —
//   see `mergeReplyCardWrite`. The rename is deliberately out of this package's
//   scope, so read the name as history, not as a description of what it does.
//   The earlier version of this note said the delta was "the
//   single reconcile trigger" for the action path too — that made the pane's
//   correctness depend on an OPTIONAL live event, and with the EventSource down
//   or one frame missed the server had accepted the answer while the pane (and
//   therefore the nav badge) still showed the card as waiting, sending the owner
//   back into it for a 409. Do NOT re-derive that as "the accepted trade": the
//   trade step 8 actually bought was one fewer ROUND, not a lost fallback.
//   The delta remains the reconcile trigger for every write this cockpit did NOT
//   make, and it is sufficient in BOTH adapters — this is the load-bearing fact, so
//   check it before touching either: the http adapter gets the delta from the
//   server (`publishReplyCard` runs AFTER the row is committed and BEFORE the
//   response is flushed, so a delta-triggered read can never precede the
//   write), and the mock fans its OWN `reply_card` topic from inside
//   answer/reanswer/expire (`emitTopic`, called synchronously after it mutates
//   the in-memory card). The old comment here claimed the direct refetch
//   existed "so the mock behaves identically" — that stopped being true when
//   the mock grew emitTopic, and the stale justification is what kept the
//   duplicate alive.
//   ⚠️ `refresh()` is NOT part of this and still refetches unconditionally: its
//   caller (a 409 answer — the card was already handled elsewhere) learned its
//   snapshot is stale from a write it did not make, so there is no delta of its
//   own on the way.
//
// T-a3e4 step 8, second half — THE N+1 IS GONE (this note used to say the
// follow-up was "still NOT in this change"; the very commit that carried step 8
// to main, PR #76, is the one that did it — owner approved `?view=full` on
// 2026-08-02, card rc-73a3f49b180e). `api.listReplyCards` no longer walks a
// light index and hydrates per id: it issues ONE request per pane,
// `GET /api/reply-cards?status=<s>&view=full`, and the server answers with
// whole cards — each full row byte-identical to that card's own
// `GET /api/reply-cards/{card_id}`, pinned by the server test
// `TestListReplyCardsViewFullRowsEqualTheSingleCardResponse`. The DEFAULT is
// still the light index (`view` absent or `light` is byte-for-byte the old
// wire), and `view` is deliberately absent from the agent-facing
// `list_reply_cards` MCP tool.
// ⚠️ The per-card GET counts in the paragraph above are therefore HISTORICAL —
// they describe the pane BEFORE this landed. Do not benchmark against them.
//
// 🔴 It is O(1) per pane, NOT "one request for the whole screen". This provider
// still makes its own fixed count/status reads, and an EXPANDED 近期已處理 pane
// is three list requests (waiting + answered + expired), not one. Say "a fixed
// number of requests instead of one per card".
// 🔴 The win is ROUND TRIPS, not bandwidth. Re-measured AFTER the change against
// a real ocserverd (isolated port, fresh DB, population re-counted from the
// server at measurement time: 25 waiting / 15 answered / 10 expired), varying
// only this adapter: one cockpit load of the waiting pane went 26 reply-card
// requests / 27,537 B → 1 request / 21,294 B; one delta with the handled pane
// expanded went 54 / 58,509 B → 3 / 44,195 B. That is 51 fewer round trips for
// roughly a quarter fewer bytes. Never sell this as saving bandwidth — on a
// slow link the latency is the whole cost, and a full pane is very nearly the
// same size either way.
//
// A single pane snapshot is also no longer an internally skewed slice: it is
// one response, not an index plus N later reads. The three panes are still
// three separate requests, so the skew between PANES is unchanged.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type {
  ReplyCard,
  ReplyCardWriteReceipt,
  ReplyCardAnswerInput,
} from "../api/adapter";
import { api } from "../api";
import { mergeReplyCardWrite } from "../lib/replyCardReceipt";

/** A handled card's pane stamp: answeredTs on an answered card, expiredTs on
 * an expired one (each null on the other kind). */
function handledTs(c: ReplyCard): number {
  return c.status === "expired" ? (c.expiredTs ?? 0) : (c.answeredTs ?? 0);
}

interface UseReplyCards {
  /** Cards still waiting for the owner — server-ordered LONGEST-WAITING FIRST.
   * This IS the single authoritative waiting source: the nav badge counts its
   * length, the page renders it, the title reads its length. */
  waiting: ReplyCard[];
  /** Cards answered OR expired within the last 24h — merged, newest handled
   * first. EMPTY until `loadHandled()` is called (the pane is collapsed by
   * default). */
  handled: ReplyCard[];
  /** Recently-handled (24h) count from the cheap count endpoint (answered +
   * expired) — drives the collapsed 近期已處理 · N header (and its zero-hide)
   * WITHOUT the lists. */
  handledCount: number;
  /** True once the handled lists have actually been fetched (pane expanded). */
  handledLoaded: boolean;
  loading: boolean;
  /** True when the mount fetch REJECTED (500/network; 401 already bounced to
   * login) — so a failed load never masquerades as the ✓ empty state. */
  error: boolean;
  /** Pull the handled lists on demand (the owner expanded the pane). Idempotent
   * and safe to call repeatedly; a repeat just refreshes them. */
  loadHandled: () => void;
  /** Re-pull the panes on demand — the caller learned the local snapshot is
   * stale (T-4166: a 409 answer means the card is already handled or orphaned,
   * so it must stop rendering as if it still waits). */
  refresh: () => Promise<void>;
  /** Answer a WAITING card (the positive close). Resolving means the WRITE
   * landed AND its own response has been adopted — the card has left the waiting
   * pane by then, with or without the `reply_card` delta (see `adoptWrite`). The
   * other panes' full re-read still rides that delta (T-a3e4 step 8). */
  answer: (id: string, input: ReplyCardAnswerInput) => Promise<void>;
  /** Revise an ANSWERED card's answer (重新決定). Same resolve semantics as
   * `answer`. */
  reanswer: (id: string, input: ReplyCardAnswerInput) => Promise<void>;
  /** Mark a WAITING card expired (標為過期 — terminal, not an answer). Same
   * resolve semantics as `answer`. */
  expire: (id: string) => Promise<void>;
}

/** The one shared reply-cards state, driven by the app-wide provider. Both the
 * page (useReplyCards) and the nav badge (useReplyCardCount) read it, so they
 * are the SAME source and can never diverge. */
const ReplyCardsContext = createContext<UseReplyCards | null>(null);

/** The provider that owns the always-live waiting fetch (with request
 * sequencing) and the deferred handled fetch. Mounted app-wide (above the nav
 * badge AND the page) so both share one snapshot and one SSE subscription. */
export function ReplyCardsProvider({ children }: { children: ReactNode }) {
  const value = useReplyCardsState();
  return (
    <ReplyCardsContext.Provider value={value}>
      {children}
    </ReplyCardsContext.Provider>
  );
}

function useReplyCardsState(): UseReplyCards {
  const [waiting, setWaiting] = useState<ReplyCard[]>([]);
  const [handled, setHandled] = useState<ReplyCard[]>([]);
  const [handledCount, setHandledCount] = useState(0);
  const [handledLoaded, setHandledLoaded] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // Live "is the handled pane loaded" flag for the SSE closure (which depends
  // only on the stable refetchers and must not re-subscribe on each load).
  const handledLoadedRef = useRef(false);
  // ① Monotonic generation ids: every refetch takes a ticket on entry and only
  // commits its result if the ticket is still current. A late stale response
  // (an older refetch resolving AFTER a newer one) fails the check and is
  // dropped — this is what kills the last-write-wins that dropped cards.
  const waitingGenRef = useRef(0);
  const handledGenRef = useRef(0);
  // Live mirror of `waiting`, so adoptWrite below can compute the next array
  // WITHOUT taking `waiting` as a dependency (the action callbacks it feeds are
  // handed to the cards as props; a new identity on every snapshot is churn we
  // do not need). Written wherever `setWaiting` is.
  const waitingRef = useRef<ReplyCard[]>([]);
  // ③ Cards THIS cockpit closed with its own write, held until a server snapshot
  // agrees. See `adoptWrite` — without this, a refetch that was already in flight
  // when the owner clicked resolves with a PRE-WRITE snapshot and undoes the
  // adoption. TWO holds, because the two panes are confirmed by OPPOSITE
  // evidence and one release rule cannot serve both:
  //  • waiting — confirmed when a snapshot NO LONGER lists the id.
  //  • handled — confirmed when a snapshot DOES list it with a handled stamp at
  //    least as new as ours (a 重新決定 re-stamps, so mere presence is not
  //    confirmation: a pre-write snapshot lists that card with its OLD stamp).
  const heldFromWaitingRef = useRef<Set<string>>(new Set());
  const adoptedHandledRef = useRef<Map<string, ReplyCard>>(new Map());
  // Live mirror of `handled`, for the same reason `waitingRef` mirrors
  // `waiting`: adoptWrite has to read the row it is folding a write onto (a
  // 重新決定 revises a card this pane READ, not one it adopted) without taking
  // `handled` as a dependency. Written wherever `setHandled` is.
  const handledRef = useRef<ReplyCard[]>([]);

  // The always-live cheap fetch: the waiting list + the counts. Runs on mount
  // and on every reply_card delta.
  const refetchWaiting = useCallback(async () => {
    const gen = ++waitingGenRef.current;
    try {
      const [w, counts] = await Promise.all([
        api.listReplyCards("waiting"),
        api.getReplyCardCount(),
      ]);
      // Superseded by a newer refetch while we were in flight → drop this
      // (possibly stale) snapshot rather than clobber the fresher one.
      if (gen !== waitingGenRef.current) return;
      // ③ Filter out cards we have already closed ourselves but this snapshot
      // still lists — it was read BEFORE our write landed. Everything ELSE in
      // it is adopted as usual (a card a peer just opened arrives normally),
      // which is the whole reason this is a per-id hold and not "drop the
      // snapshot": dropping it would trade a resurrected card for a new card
      // that never shows up, and with the stream down nothing would correct
      // either.
      const adopted = heldFromWaitingRef.current;
      const heldBack = adopted.size ? w.filter((c) => adopted.has(c.id)) : [];
      // A snapshot that no longer lists one of our closed cards IS the server
      // confirming it — stop holding that id, so nothing is suppressed forever
      // (and a future card reusing the id could never be hidden).
      if (adopted.size) {
        for (const id of [...adopted]) {
          if (!w.some((c) => c.id === id)) adopted.delete(id);
        }
      }
      const next = heldBack.length ? w.filter((c) => !adopted.has(c.id)) : w;
      setWaiting(next);
      waitingRef.current = next;
      // The counts came from the SAME (pre-write) snapshot as the rows we just
      // held back, so add them: otherwise the 近期已處理 header would drop by
      // one for as long as the hold lasts.
      setHandledCount(counts.answered + counts.expired + heldBack.length);
      setError(false);
    } catch (e) {
      // Only the latest attempt owns the error surface — a stale rejection must
      // not flip the page into its error state after a newer fetch succeeded.
      if (gen === waitingGenRef.current) setError(true);
      throw e;
    }
  }, []);

  // The deferred handled fetch: only ever runs once the pane is expanded
  // (loadHandled), then re-runs on deltas while it stays loaded.
  const refetchHandled = useCallback(async () => {
    const gen = ++handledGenRef.current;
    const [answered, expired] = await Promise.all([
      api.listReplyCards("answered"),
      api.listReplyCards("expired"),
    ]);
    // Same generation guard as the waiting list — drop a superseded snapshot.
    if (gen !== handledGenRef.current) return;
    const merged = [...answered, ...expired];
    // ③ The handled half of the same hazard. Once the owner has expanded this
    // pane, EVERY delta refetches it for the rest of the visit
    // (`handledLoadedRef` is not reset on collapse, and the deep-link path calls
    // loadHandled() without a click at all) — so a read can be in flight when
    // the owner answers, and a pre-write snapshot would take the card they just
    // handled back OUT of 近期已處理.
    //
    // ⚠️ Confirmation here is the OPPOSITE evidence to the waiting pane's, and
    // mere presence is not enough: a 重新決定 re-stamps answeredTs, so a
    // pre-write snapshot lists that same card with its OLD stamp. Take the
    // snapshot's row only once its stamp is at least as new as the one we
    // adopted; until then substitute (or insert) ours.
    //
    // 🔴 THE TWO SIDES ARE NOT SYMMETRIC — do not "tidy" them into one rule.
    // On the waiting side ABSENCE is the confirmation; here absence means the
    // snapshot predates the write, so the row is pushed back and the hold STAYS.
    // ⚠️ Known consequence, measured: once a card ages out of the server's 24h
    // handled window every later snapshot omits it, so its entry in
    // adoptedHandledRef is NEVER released and `handled` keeps one ghost row per
    // such card for the rest of the visit. It is invisible on screen (RepliesPage
    // filters by the same 24h window), so this is memory hygiene, not a
    // correctness bug — but it is why the `delete` below is hygiene ONLY:
    // `merged[i] = mine` fires only when OUR stamp is strictly newer, so
    // correctness never depends on the delete having run.
    const adoptedH = adoptedHandledRef.current;
    if (adoptedH.size) {
      for (const [id, mine] of [...adoptedH]) {
        const i = merged.findIndex((c) => c.id === id);
        if (i < 0) {
          merged.push(mine); // snapshot predates the write entirely
        } else if (handledTs(merged[i]) >= handledTs(mine)) {
          adoptedH.delete(id); // the server has caught up — stop overriding
        } else {
          merged[i] = mine; // same card, older stamp: ours is the newer truth
        }
      }
    }
    const sorted = merged.sort((a, b) => handledTs(b) - handledTs(a));
    handledRef.current = sorted;
    setHandled(sorted);
    setHandledLoaded(true);
    handledLoadedRef.current = true;
  }, []);

  const loadHandled = useCallback(() => {
    refetchHandled().catch((e) =>
      console.warn("useReplyCards: handled load failed", e)
    );
  }, [refetchHandled]);

  useEffect(() => {
    let alive = true;

    refetchWaiting()
      // refetchWaiting owns its own (generation-guarded) error state; here we
      // only swallow the rejection to avoid an unhandled promise and clear the
      // initial loading flag.
      .catch((e) => console.warn("useReplyCards: initial load failed", e))
      .finally(() => {
        if (alive) setLoading(false);
      });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic !== "reply_card") return;
      refetchWaiting().catch((e) =>
        console.warn("useReplyCards: SSE refetch failed", e)
      );
      // Keep the handled pane fresh only while it is actually loaded — a
      // collapsed (never-expanded) pane stays unfetched on deltas too.
      if (handledLoadedRef.current) {
        refetchHandled().catch((e) =>
          console.warn("useReplyCards: SSE handled refetch failed", e)
        );
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [refetchWaiting, refetchHandled]);

  // The UNCONDITIONAL re-read, for a caller that learned its snapshot is stale
  // from a write it did not make (the 409 path). NOT used by the actions below
  // — see T-a3e4 step 8 in the header for why they must not re-read.
  // ADOPT-FROM-RESPONSE: the action path's own reconciliation, so correctness
  // never depends on an optional live event (T-a3e4 step 8 follow-up). Step 8
  // was right that the action must not spend a SECOND refetch round; it was
  // wrong to leave the `reply_card` delta as the ONLY reconciler. With the
  // EventSource down or one frame missed, the server had ACCEPTED the answer
  // while the pane kept rendering the card as waiting — the owner clicks it
  // again and eats a 409, and the nav badge (this array's length) stays wrong
  // until reconnect / foreground resync / reload.
  //
  // The write already answers with the fresh card (`answerReplyCard` /
  // `reanswerReplyCard` / `expireReplyCard` all return `ReplyCard`), so this
  // costs ZERO extra requests — step 8's one-round budget is untouched, and the
  // delta still drives the pane for everyone ELSE's writes.
  // ⚠️ This is an adoption of the SERVER's own response for ONE identified card,
  // not a merge of an SSE payload (contract B still holds: deltas refetch, never
  // merge). It never re-orders the WAITING pane and never adds a row to it — a
  // card it does not already hold there is left to the delta / next refetch. (It
  // DOES append to the HANDLED list when that pane is loaded, including a card
  // that was not in it before: a 重新決定 on a card whose 24h window had lapsed
  // re-enters that window, and the sort below puts it where its stamp says.)
  //
  // ③ THE IN-FLIGHT SNAPSHOT. Adopting is not enough on its own: a refetch that
  // was already in flight when the owner clicked (a peer's delta a moment before
  // the stream died is exactly how that happens) resolves with a snapshot read
  // BEFORE the write and paints the card back into the pane — same wrong screen
  // as the original defect, and with the stream down nothing comes to correct
  // it. T-e862's generation guard does not cover this: it only drops a snapshot
  // once a NEWER refetch exists, and when the stream is down there is no newer
  // one. So the id is HELD until a snapshot agrees — one hold per pane, because
  // the two panes are confirmed by opposite evidence (see the two refs above and
  // each refetch's release rule).
  //
  // ⚠️ WHY THE HOLD CANNOT LEAK A CARD FOREVER — the honest reason, not the one
  // this comment used to give. It is NOT "a future card reusing this id could
  // never be hidden": the waiting hold releases only on an id the snapshot has
  // stopped listing, so an id that keeps appearing as waiting would keep being
  // filtered (measured). What makes that harmless is two facts OUTSIDE this
  // function: card ids are `rc-` + 12 hex from crypto/rand (server
  // api_replycards.go) so they are not reused, and the card state machine has no
  // answered/expired → waiting edge, so the card we just closed cannot come back
  // as waiting. Both are server properties: if either changes, this hold needs a
  // TTL or a stamp comparison like the handled side's.
  const adoptWrite = useCallback((receipt: ReplyCardWriteReceipt) => {
    // Every one of the three writes settles the card (answer/re-answer → answered,
    // expire → expired), so `receipt.status !== "waiting"` always holds here.
    // There is deliberately no "still waiting" branch: the one that used to sit
    // here was unreachable, and an unreachable branch reads like a supported case.
    //
    // 🔴 T-91: MERGE, DO NOT REPLACE. This used to store the write's answer as
    // the card. The write is about to stop echoing the question, its options,
    // its attachments and its task ref (they are not what it decided), so a
    // replacement would blank those the day the receipt lands — silently, since
    // nothing here would throw. `mergeReplyCardWrite` folds only the transition
    // in and keeps the rest of the card THIS pane already read. Still zero extra
    // requests, so the reason adoption exists at all — the pane converges from
    // the write instead of waiting for a `reply_card` frame that may never
    // arrive, which is what once left an answered card showing 待回覆 until the
    // next click hit a 409 — is untouched.
    const prev = waitingRef.current;
    heldFromWaitingRef.current.add(receipt.id);
    const wasWaiting = prev.find((c) => c.id === receipt.id);
    if (wasWaiting) {
      const next = prev.filter((c) => c.id !== receipt.id);
      waitingRef.current = next;
      setWaiting(next);
      // It left the waiting pane, so the 近期已處理 header's count gained it.
      // (Only when it really WAS waiting — a 重新決定 revises an already-handled
      // card and must not re-count it.)
      setHandledCount((n) => n + 1);
    }
    // Keep the handled lists coherent too, but only while they are actually
    // loaded — a collapsed, never-expanded pane stays unfetched (the same gate
    // the SSE path respects), and a later expand reads POST-write anyway.
    if (handledLoadedRef.current) {
      // The card to merge onto: the waiting row it just left, else the handled
      // row already on screen (the 重新決定 path reads it from there), else our
      // own earlier adoption.
      //
      // 🔴 With NONE of those, this pane never held the card and there is
      // nothing to merge the transition into. The receipt is NOT a card — it
      // carries no question, no options, no attachments and no task ref — so
      // the handled list is left alone rather than gaining a blank row. The
      // pane converges the ordinary way instead (the `reply_card` delta, or the
      // read a later expand issues); what is given up is only the zero-request
      // shortcut, and only for a card this pane was not showing anyway.
      const before =
        wasWaiting ??
        handledRef.current.find((c) => c.id === receipt.id) ??
        adoptedHandledRef.current.get(receipt.id);
      if (before) {
        const card = mergeReplyCardWrite(before, receipt);
        adoptedHandledRef.current.set(receipt.id, card);
        const next = [
          ...handledRef.current.filter((c) => c.id !== receipt.id),
          card,
        ].sort((a, b) => handledTs(b) - handledTs(a));
        handledRef.current = next;
        setHandled(next);
      }
    }
  }, []);

  const refresh = useCallback(async () => {
    await refetchWaiting();
    if (handledLoadedRef.current) await refetchHandled();
  }, [refetchWaiting, refetchHandled]);

  const answer = useCallback(
    async (id: string, input: ReplyCardAnswerInput) => {
      adoptWrite(await api.answerReplyCard(id, input));
    },
    [adoptWrite]
  );

  const reanswer = useCallback(
    async (id: string, input: ReplyCardAnswerInput) => {
      adoptWrite(await api.reanswerReplyCard(id, input));
    },
    [adoptWrite]
  );

  const expire = useCallback(
    async (id: string) => {
      adoptWrite(await api.expireReplyCard(id));
    },
    [adoptWrite]
  );

  return {
    waiting,
    handled,
    refresh,
    handledCount,
    handledLoaded,
    loading,
    error,
    loadHandled,
    answer,
    reanswer,
    expire,
  };
}

/** The 等我回覆 page's data — the shared waiting/handled source. MUST be used
 * under a <ReplyCardsProvider>. */
export function useReplyCards(): UseReplyCards {
  const ctx = useContext(ReplyCardsContext);
  if (!ctx) {
    throw new Error("useReplyCards must be used within a <ReplyCardsProvider>");
  }
  return ctx;
}

/** The nav badge's waiting count — literally the length of the SAME
 * authoritative waiting array the page renders (T-e862 同源化), so the badge
 * and the list can never show different numbers. MUST be used under a
 * <ReplyCardsProvider>. */
export function useReplyCardWaitingCount(): number {
  return useReplyCards().waiting.length;
}
