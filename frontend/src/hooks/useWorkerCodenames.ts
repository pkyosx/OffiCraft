// hooks/useWorkerCodenames.ts — lazy outsource-codename resolution for ids
// that are NOT in any list the caller already holds (T-3ed8 全站盤查).
//
// Why it exists: GET /api/members DOES carry kind='outsource' rows, but it
// drops every roster_status='removed' one — and release sets exactly that —
// and GET /api/outsource-workers serves LIVE workers only. So a RELEASED
// worker's id (task closed / reassigned away) resolves to nothing client-side
// and every display point degraded to the raw ow- id (chat sender labels,
// 任務卡 前任/建立者 chips, 請示卡 identity row) while the left rail showed the
// codename.
// The per-id GET /api/outsource-workers/{id} DOES serve released rows, so this
// hook resolves unknown ow- ids through it, once each, into a module-level
// cache shared by every display point.
//
// Contract: pass ANY id list — non-ow- ids are ignored. Returns a Map of
// id → codename for every id resolved SO FAR (this render); entries appear as
// fetches land (a re-render is triggered). A failed fetch (404 / network) is
// negative-cached for the session so an unresolvable id never hammers the
// server — the caller's raw-id fallback stays, honest as before.

import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import type { OutsourceWorkerView } from "../api/adapter";
import { createDeltaSink } from "../lib/deltaSink";

// The WHOLE worker row is kept, not the two fields the first cut stored
// (T-196). `GET /api/outsource-workers/{id}` already answers the complete
// projection — codename, avatar AND the bound task (taskId / taskNo /
// taskTitle / taskTypeName) — so a display point that needs the worker's
// CURRENT TASK is one accessor on this cache, not a second read and not a
// second rule. Keeping only `{codename, avatarUrl}` was what forced the reply
// card list to have no answer for "which task is this outsource on".
type WorkerIdentity = OutsourceWorkerView;

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
          (w) => cache.set(id, w),
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

// The topics that can change WHICH TASK a worker is on: assignment / release
// ("outsource_worker") and the task's own labels or closure ("task") — the same
// two the rail treats as full re-pulls. `chat` / `chat_read` move only the
// unread badge, which no consumer of THIS accessor renders.
//
// ⚠️ Deliberately NOT narrowed by the batch's ids: a "task" delta names the
// TASK, and this cache is keyed by WORKER. Matching worker ids against it would
// answer "none of them are mine" for every task event and the line would then
// never update at all — the silent-staleness shape, not a saving.
const CURRENT_TASK_TOPICS = new Set(["outsource_worker", "task"]);

/**
 * The worker rows behind a set of ids, for display points that render a
 * worker's CURRENT TASK (T-196: the reply card list's identity row, beside the
 * office rail's outsource row).
 *
 * Same per-id read and same module cache as the codename/avatar accessors, so
 * one card cannot say 代號 while disagreeing about the task beside it — and it
 * RESOLVES released workers, which `GET /api/outsource-workers` drops on purpose
 * (`api_outsource.go`) and which every reply card list holds plenty of.
 *
 * ⚠️ Resolving one is not the same as it HAVING a current task. Release flips
 * the status and leaves `task_id` alone, so a released row still carries the
 * task it finished — a true record, and a false answer to "what is it on now".
 * This accessor hands back the row as the server tells it; deciding what a
 * released row means is the display point's job, and the reply card list reads
 * `status` to make it (a caller that forgets will silently show finished work
 * as current).
 *
 * 🔴 It ALSO subscribes, and that is the difference from the accessors above: a
 * codename and an avatar do not change while the page is open, a current task
 * does. Without this the line would be a fact that was true when the page
 * loaded, rendered as if it were true now — and nothing on screen would say so.
 * Only the ids THIS caller asked for are re-read, so no other consumer of the
 * cache pays for the subscription.
 */
export function useWorkerCurrentTasks(
  ids: readonly string[],
): Map<string, OutsourceWorkerView> {
  const base = useWorkerCodenames(ids); // one fetch path, shared cache
  const key = ids.filter((id) => id.startsWith("ow-")).sort().join("|");
  const [tick, setTick] = useState(0);
  // The ids to re-read, readable from the SSE callback without a stale closure.
  const wantedRef = useRef<string[]>([]);
  wantedRef.current = key ? key.split("|") : [];
  // True while a refresh round is in flight; `pending` remembers that a burst
  // arrived while it was — see the coalescing note below.
  const refreshingRef = useRef(false);
  const pendingRef = useRef(false);

  useEffect(() => {
    let alive = true;

    /** What every pass publishes once its reads have landed. */
    const settleRound = () => {
      // The cache is shared and global, so this wakes EVERY mounted consumer
      // (codename / avatar) — `cache.size` does not move on an overwrite, so
      // nothing recomputes on its own. It runs even when THIS hook has since
      // unmounted: the other consumers are still on screen holding the row we
      // just replaced, and the same reasoning is why `updateCachedWorkerAvatar`
      // notifies rather than mutating quietly.
      notifyAll();
      if (alive) setTick((n) => n + 1);
    };

    /** One refresh round over the ids this caller holds; runs again if a burst
     * arrived while it was in flight. */
    const runRound = async (): Promise<void> => {
      refreshingRef.current = true;
      // 🔴 `finally`, not a line after the await. The latch decides whether ANY
      // future delta is served, so the one way to lose the refresh for good is
      // to leave it stuck true — and every read here currently settles, which
      // makes that impossible by accident and trivial to reintroduce: deleting
      // the "useless" empty reject handler below is enough, and the whole suite
      // stays green while the line on screen freezes on a stale fact. The
      // guarantee belongs to the latch, not to the callers that happen not to
      // throw today. (Third-round review finding.)
      try {
        do {
          pendingRef.current = false;
          const mine = wantedRef.current.filter((id) => cache.get(id));
          await Promise.all(
            mine.map((id) =>
              api.getOutsourceWorker(id).then(
                (w) => cache.set(id, w),
                // Keep the last known row: a failed re-read is not evidence the
                // worker is gone, and blanking the line would say it is.
                () => {},
              ),
            ),
          );
          settleRound();
          // A burst that arrived mid-round is served by one more pass — a flat
          // loop rather than recursion, so "one more, then stop" is the shape
          // of the code and not a chain of pending promises.
        } while (pendingRef.current);
      } finally {
        refreshingRef.current = false;
      }
    };


    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        if (![...batch.topics].some((t) => CURRENT_TASK_TOPICS.has(t))) return;
        const mine = wantedRef.current.filter((id) => cache.get(id));
        if (mine.length === 0) return;
        // One round in flight at a time. A refresh costs one read PER held id
        // (the per-id endpoint is the only one that covers released workers),
        // and task deltas arrive in bursts — without this, a busy studio turns
        // a page holding N askers into N reads per burst, several bursts deep.
        //
        // 🔴 COALESCED, NOT DROPPED, and the difference is a real defect. A
        // burst that lands mid-round reports a change the in-flight reads were
        // sent BEFORE, so they cannot carry it: finishing the round writes back
        // the state as it was a moment ago. Dropping it outright would leave
        // that stale row on screen until some UNRELATED later delta happens to
        // arrive — and in a quiet studio the release of the very worker on
        // screen can be the last event of the hour, so "later" means never. So
        // the round remembers it was overtaken and runs once more; the second
        // round is the one that sees the release.
        if (refreshingRef.current) {
          pendingRef.current = true;
          return;
        }
        void runRound();
      }),
    );
    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return useMemo(() => {
    const out = new Map<string, OutsourceWorkerView>();
    for (const id of ids) {
      const identity = cache.get(id);
      if (identity) out.set(id, identity);
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, base, tick]);
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
