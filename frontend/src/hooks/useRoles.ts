// hooks/useRoles.ts — load + mutate the role-definition ROSTER.
//
// Mirrors useMonitoring: mount-fetch + reconcile-by-refetch on the "role_def"
// SSE topic. save/reset/create still fold the mutation response back into
// state (the response IS the folded doc, and a roster row is a projection of
// it), so a rename or a reset shows on the list without a re-pull.
//
// 🔴 T-1170 OVERTURNED THIS HOOK'S FOUNDING DESIGN DECISION, ON PURPOSE.
// It used to say, in this comment block: "listRoles returns the FULL folded
// docs (definition_md included), so the roles-log list AND the role-detail view
// read from the same array." That was a real decision, not an accident — one
// request served both screens and the detail page needed no fetch at all.
//
// The wire it rested on is gone: `GET /api/roles` now answers a DIRECTORY (no
// `definition_md`, only its size and the cap in force). Keeping the old shape
// would have meant the roles list downloading every persona body in the office
// so that ONE of them could be read if the owner happened to open it.
//
// What replaces it, and what it costs:
//   - `roles` is now `RoleSummaryView[]` — a TYPE that cannot carry a persona
//     body, so "read the definition off the roster" is a compile error rather
//     than a blank editor. That is the substitution, not a convention.
//   - the role page reads its own document through `useRole(key)` below
//     (`GET /api/roles/{key}`), on the same "role_def" topic, so a restore or
//     an agent's write still lands on screen.
//   - COST: opening a role is one more round trip than it used to be, and the
//     page has its own loading and its own failure to state (it can now fail
//     while the roster succeeded). The roster in exchange stops paying for
//     every document nobody opened.
//   - COST, second and smaller: the roles list can no longer preview the first
//     line of a definition, because that line is text. SettingsPage's
//     `firstBodyLine` preview went with it.

import { useCallback, useEffect, useState } from "react";
import type { RoleDefView, RoleSummaryView } from "../types";
import type { RolePatch } from "../api";
import type { RoleCreateInput, RoleCreateResult } from "../api/adapter";
import { api } from "../api";

interface UseRoles {
  roles: RoleSummaryView[];
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Lets Settings show an honest "load failed" instead of an empty
   * role list masquerading as "no roles defined". */
  error: boolean;
  refetch: () => Promise<void>;
  /** Both return the folded doc the write echoed, so the page that is showing
   * that document can ADOPT it — a page split off the roster must not have to
   * wait for an SSE frame to see its own save (see useRole.adopt). */
  save: (key: string, patch: RolePatch) => Promise<RoleDefView>;
  reset: (key: string) => Promise<RoleDefView>;
  /** Create one custom role + its founding member (M2-2). Appends the new role
   * to the roster from the response (the office roster picks the member up via
   * its own member-topic refetch). Rejections (422) propagate to the caller. */
  create: (input: RoleCreateInput) => Promise<RoleCreateResult>;
  /** HARD-delete a custom role (M2-2). Rejections propagate — a 409 (member
   * online) must reach the caller so it can surface 有成員在線上,無法刪除. */
  remove: (key: string) => Promise<void>;
}

/** A folded doc, narrowed to the roster row it projects to. The mutation
 * responses are FULL docs; storing them whole would put `definitionMd` back
 * into the roster array under a type that says it is not there — a second,
 * silent source for the text this change exists to remove. */
function toRosterRow(doc: RoleDefView): RoleSummaryView {
  const { definitionMd: _definitionMd, ...row } = doc;
  return row;
}

export function useRoles(): UseRoles {
  const [roles, setRoles] = useState<RoleSummaryView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    setRoles(await api.listRoles());
  }, []);

  // Merge one updated role back into the roster by key (no full refetch needed —
  // the mutation response is the folded doc).
  const mergeRole = useCallback((updated: RoleDefView) => {
    const row = toRosterRow(updated);
    setRoles((prev) => prev.map((r) => (r.key === row.key ? row : r)));
  }, []);

  const save = useCallback(
    async (key: string, patch: RolePatch) => {
      const doc = await api.saveRole(key, patch);
      mergeRole(doc);
      return doc;
    },
    [mergeRole]
  );

  const reset = useCallback(
    async (key: string) => {
      const doc = await api.resetRole(key);
      mergeRole(doc);
      return doc;
    },
    [mergeRole]
  );

  const create = useCallback(async (input: RoleCreateInput) => {
    const result = await api.createRole(input);
    setRoles((prev) => [...prev, toRosterRow(result.role)]);
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
          // A refetch that REJECTS leaves the roster showing pre-write rows,
          // so it sets `error` exactly as the initial load does. Swallowing it
          // into a console line made the one visible symptom — a list that has
          // silently stopped reconciling — indistinguishable from a list that
          // is up to date.
          .catch((e) => {
            console.warn("useRoles: SSE refetch failed", e);
            if (alive) setError(true);
          });
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return { roles, loading, error, refetch, save, reset, create, remove };
}

interface UseRole {
  /** The folded doc, or `null` while it has not been read (loading, failed, or
   * an unknown key). NEVER a fabricated empty document: an editor seeded from
   * one would let 完成編輯 overwrite a real persona with a blank. */
  role: RoleDefView | null;
  loading: boolean;
  /** The read REJECTED. Distinct from `role === null` while loading, and it is
   * the state the roster could not have told the page about — the roster row
   * can be present and this read still fail. */
  error: boolean;
  refetch: () => Promise<void>;
  /** Take a folded doc this page already has in hand (a write echo) as the
   * current document. Zero requests, and it means a save is on screen the
   * instant the server acknowledged it rather than whenever the SSE frame
   * arrives — the page must not depend on a stream that can be down. */
  adopt: (doc: RoleDefView) => void;
}

/**
 * ONE role definition in full (`GET /api/roles/{key}`) — what the 角色定義 page
 * reads since T-1170 took the document off the roster answer.
 *
 * Reconciles on the SAME "role_def" topic the roster listens on, which is what
 * keeps a 版本紀錄 restore, an agent's write and another tab's save landing on
 * this page: every one of them fans that topic. The page's own save does not
 * wait for it — `useRoles.save` returns the folded doc — but the refetch is
 * what makes the page correct when the write came from somewhere else.
 *
 * `key` is `""` for "no role on screen": nothing is requested and nothing is
 * subscribed, so the settings landing costs no per-role call.
 */
export function useRole(key: string): UseRole {
  const [role, setRole] = useState<RoleDefView | null>(null);
  const [loading, setLoading] = useState(key !== "");
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    if (key === "") return;
    setRole(await api.getRole(key));
    setError(false);
  }, [key]);

  useEffect(() => {
    if (key === "") {
      setRole(null);
      setLoading(false);
      setError(false);
      return;
    }
    let alive = true;
    // Drop the previous role's document immediately. Without this, switching
    // roles renders the PREVIOUS persona under the new title until the fetch
    // lands — the "old cache" failure, wearing the right heading.
    setRole(null);
    setLoading(true);

    const load = (onFail: (e: unknown) => void) =>
      api
        .getRole(key)
        .then((next) => {
          if (alive) {
            setRole(next);
            setError(false);
          }
        })
        .catch(onFail);

    load((e) => {
      console.warn("useRole: initial load failed", e);
      if (alive) setError(true);
    }).finally(() => {
      if (alive) setLoading(false);
    });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic.includes("role_def")) {
        void load((e) => {
          console.warn("useRole: SSE refetch failed", e);
          // Same rule as the roster above: a failed reconcile means what is on
          // screen is stale, and stale-but-silent is the state this page has
          // no way to recover from on its own.
          if (alive) setError(true);
        });
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [key]);

  const adopt = useCallback((doc: RoleDefView) => {
    setRole(doc);
    setError(false);
  }, []);

  return { role, loading, error, refetch, adopt };
}
