// hooks/useLessons.ts — load + mutate the folded PER-ROLE lessons doc for one
// role_key (T-2 removed the task_type axis; role_key is the whole address).
//
// Mirrors useGlobalContext: mount-fetch + reconcile-by-refetch on the relevant
// SSE topic ("lessons"). 🔴 T-91: save WRITES THEN RE-READS. It used to fold the
// write's own answer into state because that answer WAS the folded doc; the
// lessons receipt is about to carry size + sha256 and no `text`, and adopting
// that would blank this journal on save without raising anything. The re-read is
// also what keeps the is_default flip the server's statement rather than one the
// UI invented. Per-role-learnings step1: the
// doc is scoped to role_key — agents sharing a role share it, but a researcher's
// learnings no longer pollute an assistant's.

import { useCallback, useEffect, useState } from "react";
import type { LessonsView } from "../types";
import { api } from "../api";

interface UseLessons {
  lessons: LessonsView | null;
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Lets the UI tell a failed load apart from an honest empty doc. */
  error: boolean;
  refetch: () => Promise<void>;
  save: (text: string) => Promise<void>;
}

export function useLessons(roleKey: string): UseLessons {
  const [lessons, setLessons] = useState<LessonsView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setLessons(await api.getLessons(roleKey));
  }, [roleKey]);

  const save = useCallback(
    async (text: string) => {
      await api.saveLessons(roleKey, text);
      await refetch();
    },
    [roleKey, refetch]
  );

  useEffect(() => {
    let alive = true;

    api
      .getLessons(roleKey)
      .then((next) => {
        if (alive) {
          setLessons(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useLessons: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic.includes("lessons")) {
        api
          .getLessons(roleKey)
          .then((next) => {
            if (alive) {
              setLessons(next);
              setError(false);
            }
          })
          .catch((e) => console.warn("useLessons: SSE refetch failed", e));
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [roleKey]);

  return { lessons, loading, error, refetch, save };
}
