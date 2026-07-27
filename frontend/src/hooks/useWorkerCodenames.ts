// hooks/useWorkerCodenames.ts — lazy outsource-codename resolution for ids
// that are NOT in any list the caller already holds (T-3ed8 全站盤查).
//
// Why it exists: GET /api/members excludes kind='outsource' by design, and
// GET /api/outsource-workers serves LIVE workers only — so a RELEASED worker's
// id (task closed / reassigned away) resolves to nothing client-side and every
// display point degraded to the raw ow- id (chat sender labels, 任務卡 前任/
// 建立者 chips, 請示卡 identity row) while the left rail showed the codename.
// The per-id GET /api/outsource-workers/{id} DOES serve released rows, so this
// hook resolves unknown ow- ids through it, once each, into a module-level
// cache shared by every display point.
//
// Contract: pass ANY id list — non-ow- ids are ignored. Returns a Map of
// id → codename for every id resolved SO FAR (this render); entries appear as
// fetches land (a re-render is triggered). A failed fetch (404 / network) is
// negative-cached for the session so an unresolvable id never hammers the
// server — the caller's raw-id fallback stays, honest as before.

import { useEffect, useMemo, useState } from "react";
import { api } from "../api";

type WorkerIdentity = { codename: string; avatarUrl?: string };

// id → identity; null = fetch attempted, unresolvable (negative cache).
const cache = new Map<string, WorkerIdentity | null>();
const inflight = new Set<string>();
// Subscribers to notify when any fetch settles (multiple mounted callers).
const listeners = new Set<() => void>();

function notifyAll() {
  for (const l of listeners) l();
}

/** Test seam: reset the module cache between tests. */
export function __resetWorkerCodenameCache() {
  cache.clear();
  inflight.clear();
}

/** Keep lazy released-worker identity consumers coherent with an owner avatar
 * mutation made from a detail panel that may be open beside them. */
export function updateCachedWorkerAvatar(id: string, avatarUrl: string) {
  const current = cache.get(id);
  if (!current) return;
  cache.set(id, { ...current, avatarUrl });
  notifyAll();
}

export function useWorkerCodenames(ids: readonly string[]): Map<string, string> {
  const [tick, setTick] = useState(0);

  // The ids this caller still needs fetched (dedup, ow- only, not yet tried).
  const key = ids.filter((id) => id.startsWith("ow-")).sort().join("|");
  const wanted = useMemo(
    () =>
      Array.from(
        new Set(
          ids.filter((id) => id.startsWith("ow-") && !cache.has(id)),
        ),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [key],
  );

  useEffect(() => {
    const bump = () => setTick((n) => n + 1);
    listeners.add(bump);
    for (const id of wanted) {
      if (cache.has(id) || inflight.has(id)) continue;
      inflight.add(id);
      api
        .getOutsourceWorker(id)
        .then(
          (w) =>
            cache.set(id, { codename: w.codename, avatarUrl: w.avatarUrl }),
          () => cache.set(id, null), // honest miss — raw id stays
        )
        .then(() => {
          inflight.delete(id);
          notifyAll();
        });
    }
    return () => {
      listeners.delete(bump);
    };
  }, [wanted]);

  return useMemo(() => {
    const out = new Map<string, string>();
    for (const id of ids) {
      const identity = cache.get(id);
      if (identity?.codename) out.set(id, identity.codename);
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, wanted, tick, cache.size]);
}

/** Personal avatar URLs from the same per-id identity fetch/cache. */
export function useWorkerAvatarUrls(ids: readonly string[]): Map<string, string> {
  const codenames = useWorkerCodenames(ids);
  const key = ids.filter((id) => id.startsWith("ow-")).sort().join("|");
  return useMemo(() => {
    const out = new Map<string, string>();
    for (const id of ids) {
      const src = cache.get(id)?.avatarUrl;
      if (src) out.set(id, src);
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, codenames, cache.size]);
}
