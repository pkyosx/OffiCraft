// hooks/useGlobalContext.ts — load + mutate the folded global-context doc.
//
// Mirrors useMonitoring: mount-fetch + reconcile-by-refetch on the relevant SSE
// topic ("global_context"). The save/reset actions call the api and fold the
// returned doc straight into state (the mutation response IS the folded doc), so
// the UI never fabricates the is_default flip locally.

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

  const save = useCallback(async (text: string) => {
    setCtx(await api.saveGlobalContext(text));
  }, []);

  const reset = useCallback(async () => {
    setCtx(await api.resetGlobalContext());
  }, []);

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
