// hooks/useRoles.ts — load + mutate the folded role-definition roster.
//
// Mirrors useMonitoring: mount-fetch + reconcile-by-refetch on the "role_def"
// SSE topic. listRoles returns the FULL folded docs (definition_md included), so
// the roles-log list AND the role-detail view read from the same array. save/
// reset fold the returned doc back into state (the response IS the folded doc).

import { useCallback, useEffect, useState } from "react";
import type { RoleDefView } from "../types";
import type { RolePatch } from "../api";
import type { RoleCreateInput, RoleCreateResult } from "../api/adapter";
import { api } from "../api";

interface UseRoles {
  roles: RoleDefView[];
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Lets Settings show an honest "load failed" instead of an empty
   * role list masquerading as "no roles defined". */
  error: boolean;
  refetch: () => Promise<void>;
  save: (key: string, patch: RolePatch) => Promise<void>;
  reset: (key: string) => Promise<void>;
  /** Create one custom role + its founding member (M2-2). Appends the new role
   * to the roster from the response (the office roster picks the member up via
   * its own member-topic refetch). Rejections (422) propagate to the caller. */
  create: (input: RoleCreateInput) => Promise<RoleCreateResult>;
  /** HARD-delete a custom role (M2-2). Rejections propagate — a 409 (member
   * online) must reach the caller so it can surface 有成員在線上,無法刪除. */
  remove: (key: string) => Promise<void>;
}

export function useRoles(): UseRoles {
  const [roles, setRoles] = useState<RoleDefView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setRoles(await api.listRoles());
  }, []);

  // Merge one updated role back into the roster by key (no full refetch needed —
  // the mutation response is the folded doc).
  const mergeRole = useCallback((updated: RoleDefView) => {
    setRoles((prev) =>
      prev.map((r) => (r.key === updated.key ? updated : r))
    );
  }, []);

  const save = useCallback(
    async (key: string, patch: RolePatch) => {
      mergeRole(await api.saveRole(key, patch));
    },
    [mergeRole]
  );

  const reset = useCallback(
    async (key: string) => {
      mergeRole(await api.resetRole(key));
    },
    [mergeRole]
  );

  const create = useCallback(async (input: RoleCreateInput) => {
    const result = await api.createRole(input);
    setRoles((prev) => [...prev, result.role]);
    return result;
  }, []);

  const remove = useCallback(async (key: string) => {
    await api.deleteRole(key); // throws on 403/404/409 — caller surfaces it
    setRoles((prev) => prev.filter((r) => r.key !== key));
  }, []);

  useEffect(() => {
    let alive = true;

    api
      .listRoles()
      .then((next) => {
        if (alive) {
          setRoles(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useRoles: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic.includes("role_def")) {
        api
          .listRoles()
          .then((next) => {
            if (alive) {
              setRoles(next);
              setError(false);
            }
          })
          .catch((e) => console.warn("useRoles: SSE refetch failed", e));
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return { roles, loading, error, refetch, save, reset, create, remove };
}
