// hooks/useReplyCards.ts — the 等我回覆 page's data. The WAITING list is the
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

import { useCallback, useEffect, useRef, useState } from "react";
import type { ReplyCard, ReplyCardAnswerInput } from "../api/adapter";
import { api } from "../api";

/** A handled card's pane stamp: answeredTs on an answered card, expiredTs on
 * an expired one (each null on the other kind). */
function handledTs(c: ReplyCard): number {
  return c.status === "expired" ? (c.expiredTs ?? 0) : (c.answeredTs ?? 0);
}

interface UseReplyCards {
  /** Cards still waiting for the owner — server-ordered LONGEST-WAITING FIRST. */
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
  /** Answer a WAITING card (the positive close), then refetch. */
  answer: (id: string, input: ReplyCardAnswerInput) => Promise<void>;
  /** Revise an ANSWERED card's answer (重新決定), then refetch. */
  reanswer: (id: string, input: ReplyCardAnswerInput) => Promise<void>;
  /** Mark a WAITING card expired (標為過期 — terminal, not an answer), then
   * refetch. */
  expire: (id: string) => Promise<void>;
}

export function useReplyCards(): UseReplyCards {
  const [waiting, setWaiting] = useState<ReplyCard[]>([]);
  const [handled, setHandled] = useState<ReplyCard[]>([]);
  const [handledCount, setHandledCount] = useState(0);
  const [handledLoaded, setHandledLoaded] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // Live "is the handled pane loaded" flag for the SSE closure (which depends
  // only on the stable refetchers and must not re-subscribe on each load).
  const handledLoadedRef = useRef(false);

  // The always-live cheap fetch: the waiting list + the counts. Runs on mount
  // and on every reply_card delta.
  const refetchWaiting = useCallback(async () => {
    const [w, counts] = await Promise.all([
      api.listReplyCards("waiting"),
      api.getReplyCardCount(),
    ]);
    setWaiting(w);
    setHandledCount(counts.answered + counts.expired);
    setError(false);
  }, []);

  // The deferred handled fetch: only ever runs once the pane is expanded
  // (loadHandled), then re-runs on deltas while it stays loaded.
  const refetchHandled = useCallback(async () => {
    const [answered, expired] = await Promise.all([
      api.listReplyCards("answered"),
      api.listReplyCards("expired"),
    ]);
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
      .catch((e) => {
        console.warn("useReplyCards: initial load failed", e);
        if (alive) setError(true);
      })
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
