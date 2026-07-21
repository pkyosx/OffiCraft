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
// answer/re-answer/expire actions also refetch directly so the mock (whose
// subscribeEvents is a no-op) behaves identically.
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
// NOTE (follow-up, intentionally NOT in this change): api.listReplyCards is a
// non-atomic N+1 (a light index then a per-id hydrate). Sequencing makes a
// stale snapshot harmless, but a single snapshot can still be an internally
// skewed slice, and hoisting the list app-wide for the badge means that N+1
// now runs app-wide on every delta. The clean fix is an ATOMIC list endpoint
// (server returns full-enough rows in one shot); left as a follow-up because
// it is a server + shared agent-tool-contract change, out of this FE scope.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { ReplyCard, ReplyCardAnswerInput } from "../api/adapter";
import { api } from "../api";

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
  /** Answer a WAITING card (the positive close), then refetch. */
  answer: (id: string, input: ReplyCardAnswerInput) => Promise<void>;
  /** Revise an ANSWERED card's answer (重新決定), then refetch. */
  reanswer: (id: string, input: ReplyCardAnswerInput) => Promise<void>;
  /** Mark a WAITING card expired (標為過期 — terminal, not an answer), then
   * refetch. */
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
      setWaiting(w);
      setHandledCount(counts.answered + counts.expired);
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
    setHandled(
      [...answered, ...expired].sort((a, b) => handledTs(b) - handledTs(a))
    );
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

  const refetchAfterAction = useCallback(async () => {
    await refetchWaiting();
    if (handledLoadedRef.current) await refetchHandled();
  }, [refetchWaiting, refetchHandled]);

  const answer = useCallback(
    async (id: string, input: ReplyCardAnswerInput) => {
      await api.answerReplyCard(id, input);
      await refetchAfterAction();
    },
    [refetchAfterAction]
  );

  const reanswer = useCallback(
    async (id: string, input: ReplyCardAnswerInput) => {
      await api.reanswerReplyCard(id, input);
      await refetchAfterAction();
    },
    [refetchAfterAction]
  );

  const expire = useCallback(
    async (id: string) => {
      await api.expireReplyCard(id);
      await refetchAfterAction();
    },
    [refetchAfterAction]
  );

  return {
    waiting,
    handled,
    refresh: refetchAfterAction,
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
