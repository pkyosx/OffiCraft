// hooks/useInsight.ts — load + mutate the folded PER-ROLE insight doc for one
// role_key (T-3809). The role journal's third block, beside Duty (the role
// definition) and Learning (the lessons doc).
//
// Shaped after useLessons, with one deliberate difference:
//
//  * The view it returns KEEPS size_chars / cap_chars. useLessons drops the
//     wire's bookkeeping fields as noise; here the cap is the number the card
//     header shows, and it is the only place an owner can read the live
//     doc.cap_chars.insight setting without being admin.
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
  /** Back to the per-role factory seed (T-6501). RE-READS the doc after the
   * write, so the person who clicked always sees the restored text even if the
   * `insight` SSE frame is dropped — and without depending on the write to echo
   * that text back (T-91). */
  reset: () => Promise<void>;
}

export function useInsight(roleKey: string): UseInsight {
  const [insight, setInsight] = useState<InsightView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setInsight(await api.getInsight(roleKey));
  }, [roleKey]);

  // 🔴 T-91: WRITE THEN RE-READ, both of them. These used to adopt the write's
  // own answer, which was the folded doc; the insight receipt keeps the sizes and
  // has_seed and drops `text`, so adopting it would leave this card showing an
  // empty journal after a save — and this document is legitimately empty
  // sometimes, which is precisely why nobody would notice.
  const save = useCallback(
    async (text: string) => {
      await api.saveInsight(roleKey, text);
      // 🔴 THE WRITE IS FINISHED HERE. The re-read is the second half of the
      // T-91 shape above, and it answers a different question — InsightCard maps a
      // rejection to 儲存失敗, so a blip on the GET would deny a saved journal.
      try {
        await refetch();
      } catch (e) {
        console.warn(
          "useInsight: post-save refetch failed (the insight was saved)",
          e
        );
      }
    },
    [roleKey, refetch]
  );

  const reset = useCallback(async () => {
    await api.resetInsight(roleKey);
    // Likewise the restore: the seed is back on the server before this read runs.
    try {
      await refetch();
    } catch (e) {
      console.warn(
        "useInsight: post-reset refetch failed (the insight was reset)",
        e
      );
    }
  }, [roleKey, refetch]);

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

  return { insight, loading, error, refetch, save, reset };
}
