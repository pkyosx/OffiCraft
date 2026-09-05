// hooks/useGlobalContext.ts — load + mutate the folded global-context doc.
//
// Mirrors useMonitoring: mount-fetch + reconcile-by-refetch on the relevant SSE
// topic ("global_context").
//
// 🔴 T-91: save/reset WRITE THEN RE-READ. They used to fold the write's own
// answer into state, because that answer WAS the folded doc. It is not one any
// more (GlobalContextReceiptDTO carries identity + sizes, not `text`) — this
// sentence used to read "is about to stop", written while the frontend half of
// T-91 went in FIRST ON PURPOSE, before the server half; both are in the same
// package now, so it is past tense. The
// the failure mode of leaving this as it was is a page that empties itself after
// a save with nothing thrown and nothing to show the reader. The re-read costs
// one GET per save, on a surface a person edits by hand — and it is still the
// server's answer that lands on screen, so the UI never fabricates the
// is_default flip locally.

import { useCallback, useEffect, useState } from "react";
import type { GlobalContextView } from "../types";
import { api } from "../api";

interface UseGlobalContext {
  ctx: GlobalContextView | null;
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Lets the UI tell a failed load apart from an honest doc. */
  error: boolean;
  refetch: () => Promise<void>;
  save: (text: string) => Promise<void>;
  reset: () => Promise<void>;
}

export function useGlobalContext(): UseGlobalContext {
  const [ctx, setCtx] = useState<GlobalContextView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setCtx(await api.getGlobalContext());
  }, []);

  const save = useCallback(
    async (text: string) => {
      await api.saveGlobalContext(text);
      // The context is already written. Re-reading it is a separate promise about
      // a separate thing, so it may not reject this one: a blip on the GET used to
      // put 儲存失敗 under text the server had already accepted.
      try {
        await refetch();
      } catch (e) {
        console.warn(
          "useGlobalContext: post-save refetch failed (the context was saved)",
          e
        );
      }
    },
    [refetch]
  );

  const reset = useCallback(async () => {
    await api.resetGlobalContext();
    // Same for the restore: the factory context is back server-side whatever the
    // read that follows does.
    try {
      await refetch();
    } catch (e) {
      console.warn(
        "useGlobalContext: post-reset refetch failed (the context was reset)",
        e
      );
    }
  }, [refetch]);

  useEffect(() => {
    let alive = true;

    api
      .getGlobalContext()
      .then((next) => {
        if (alive) {
          setCtx(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useGlobalContext: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic.includes("global_context")) {
        api
          .getGlobalContext()
          .then((next) => {
            if (alive) {
              setCtx(next);
              setError(false);
            }
          })
          .catch((e) => console.warn("useGlobalContext: SSE refetch failed", e));
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return { ctx, loading, error, refetch, save, reset };
}
