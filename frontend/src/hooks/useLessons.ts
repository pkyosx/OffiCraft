// hooks/useLessons.ts — load + mutate the folded PER-ROLE lessons doc for one
// role_key + task_type.
//
// Mirrors useGlobalContext: mount-fetch + reconcile-by-refetch on the relevant
// SSE topic ("lessons"). The save action calls the api and folds the returned
// doc straight into state (the mutation response IS the folded doc), so the UI
// never fabricates the is_default flip locally. Per-role-learnings step1: the
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

export function useLessons(roleKey: string, taskType: string): UseLessons {
  const [lessons, setLessons] = useState<LessonsView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setLessons(await api.getLessons(roleKey, taskType));
  }, [roleKey, taskType]);

  const save = useCallback(
    async (text: string) => {
      setLessons(await api.saveLessons(roleKey, taskType, text));
    },
    [roleKey, taskType]
  );

  useEffect(() => {
    let alive = true;

    api
      .getLessons(roleKey, taskType)
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
          .getLessons(roleKey, taskType)
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
  }, [roleKey, taskType]);

  return { lessons, loading, error, refetch, save };
}
