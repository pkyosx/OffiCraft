// hooks/useInsight.ts — load + mutate the folded PER-ROLE insight doc for one
// role_key (T-3809). The role journal's third block, beside Duty (the role
// definition) and Learning (the lessons doc).
//
// Shaped after useLessons, with two deliberate differences:
//
//  1. NO task_type axis. That axis belongs to lessons; insight is one document
//     per role, keyed on the bare role_key.
//  2. The view it returns KEEPS size_chars / cap_chars. useLessons drops the
//     wire's bookkeeping fields as noise; here the cap is the number the card
//     header shows, and it is the only place an owner can read the live
//     doc.cap_chars setting without being admin.
//
// Reconcile-by-refetch on the "insight" SSE topic, same posture as useLessons on
// "lessons". 🔴 That subscription is the ONLY thing that makes a restore
// performed on another surface show up here without a page reload, and nothing
// in CI proves it is wired: the server-side test proves the frame is PUBLISHED,
// not that this hook hears it. If you are editing this file, that is the line
// not to break.

import { useCallback, useEffect, useState } from "react";
import type { InsightView } from "../types";
import { api } from "../api";

interface UseInsight {
  insight: InsightView | null;
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Lets the UI tell a failed load apart from an honest empty doc
   * — and for THIS document those two are especially easy to confuse, because
   * an untouched insight doc is legitimately empty. */
  error: boolean;
  refetch: () => Promise<void>;
  save: (text: string) => Promise<void>;
}

export function useInsight(roleKey: string): UseInsight {
  const [insight, setInsight] = useState<InsightView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setInsight(await api.getInsight(roleKey));
  }, [roleKey]);

  const save = useCallback(
    async (text: string) => {
      setInsight(await api.saveInsight(roleKey, text));
    },
    [roleKey]
  );

  useEffect(() => {
    let alive = true;

    api
      .getInsight(roleKey)
      .then((next) => {
        if (alive) {
          setInsight(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useInsight: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic.includes("insight")) {
        api
          .getInsight(roleKey)
          .then((next) => {
            if (alive) {
              setInsight(next);
              setError(false);
            }
          })
          .catch((e) => console.warn("useInsight: SSE refetch failed", e));
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [roleKey]);

  return { insight, loading, error, refetch, save };
}
