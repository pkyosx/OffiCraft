// hooks/useOutsourceWorkers.ts — the office 外包 panel's data (SPEC §4): the
// LIVE outsource-worker roster (codename · 任務狀態 + 任務標題 + the bound task's
// T-xxxx / type / created stamp, ALL riding the worker DTO), ordered 依任務建立
// 時間新→舊, plus the global parallel cap (settings.outsource_max_parallel)
// behind the panel's 「N / 上限」 + 齒輪.
//
// Reconcile-by-refetch (contract B): "outsource_worker" (assignment / release)
// and "task" (the bound task's status/title/type echo + the created_ts sort key)
// re-pull the SAME small list — `GET /api/outsource-workers`, nothing else.
//
// "chat" / "chat_read" affect ONE row's unread badge and nothing else, so since
// T-8115 they re-read just that row (`GET /api/outsource-workers/{id}`) when the
// delta names a worker on the rail, and do NOTHING when it names a peer that is
// not on it — a chat line can neither assign nor release a worker. See
// frontend/CLAUDE.md 「一則通知 = 一次『只抓它碰到的那一項』」.
//
// 🔴 T-b17f narrowed that further: naming a worker on the rail is NOT enough.
// The badge is `UnreadCounts(…, owner)`, so only a line addressed TO THE OWNER
// can move it — `m-other → ow-1` names ow-1 and still costs zero, while
// `ow-1 → owner` keeps its one read. The predicate lives in `lib/ownerUnread.ts`
// and is shared with useMembers / useChatUnread; the rail must not grow a second
// copy of that invariant.
//
// 🔴 T-a3e4: it used to also pull `GET /api/tasks` (UNFILTERED — the entire task
// history) and `GET /api/task-manuals` on every worker/task delta, purely to
// join a sort key and two labels onto a handful of rows. The server folds those
// into the worker DTO now (task_no / task_created_ts / task_type_key /
// task_type_name), so the join is gone and with it the split "full vs chat-only"
// refetch that existed only to dodge that download (T-ec2c). Do NOT re-add a
// task-list fetch here: the DTO already carries every field this panel renders.
//
// The cap knob has no SSE topic — the PATCH echo is adopted directly (the same
// server-confirmed-values rule as useServerSettings).

import { useCallback, useEffect, useRef, useState } from "react";
import type { OutsourceWorkerView } from "../api/adapter";
import { api } from "../api";
import { createDeltaSink, narrowToHeld } from "../lib/deltaSink";
import { burstMovesNoOwnerUnread } from "../lib/ownerUnread";
import {
  adoptServerSettings,
  loadServerSettings,
} from "./sharedServerSettings";

interface UseOutsourceWorkers {
  /** LIVE workers, sorted by the bound task's created_ts DESC (新→舊). */
  workers: OutsourceWorkerView[];
  /** True until the first mount fetch settles (parity with useMembers). A
   * caller resolving a chat peer from a worker id must wait for this: an
   * `ow-` chatId that is simply not-yet-loaded is NOT a released worker, and
   * treating it as one would flash the released-peer identity before the live
   * list arrives. */
  loading: boolean;
  /** True when the mount fetch REJECTED — a failed load must never read as
   * "no outsource workers". */
  error: boolean;
  /** The global cap (0 ⇒ assignment paused); null until the settings load
   * (or when it failed) — the panel then omits the cap display honestly. */
  maxParallel: number | null;
  /** PATCH outsource_max_parallel (0..20); adopts the server echo. Rejects
   * (422/network) propagate to the caller for inline error surfacing. */
  saveMaxParallel: (n: number) => Promise<void>;
}

// sortWorkers orders the panel rows 依綁定任務建立時間新→舊. The key is the wire
// `task_created_ts`; a worker whose task cannot be resolved (0) falls back to its
// own mint stamp — an honest proxy, never fabricated.
function sortWorkers(workers: OutsourceWorkerView[]): OutsourceWorkerView[] {
  const sortKey = (x: OutsourceWorkerView) =>
    x.taskCreatedTs || x.createdTs || 0;
  return [...workers].sort((a, b) => sortKey(b) - sortKey(a));
}

// The topics whose ONLY effect on a row is its unread badge
// (OutsourceWorkerDTO.unread_count). A chat line cannot assign or release a
// worker, and cannot move a row: the order is the BOUND TASK's created_ts, which
// no chat line touches. So a chat delta naming a worker we already hold is one
// GET /api/outsource-workers/{id}, not the whole list. "outsource_worker" and
// "task" stay full re-pulls — the first IS list membership (assignment /
// release), and the second can change the sort key and the row's labels.
const BADGE_ONLY_TOPICS = new Set(["chat", "chat_read"]);

// The topics this panel reconciles on (four, all through ONE path).
const WORKER_TOPICS = new Set([
  "outsource_worker",
  "task",
  "chat",
  "chat_read",
]);

export function useOutsourceWorkers(): UseOutsourceWorkers {
  const [workers, setWorkers] = useState<OutsourceWorkerView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [maxParallel, setMaxParallel] = useState<number | null>(null);
  // Which worker ids are on screen, readable from the SSE callback without a
  // stale-closure state read. Membership only — values come from the server.
  const heldRef = useRef<Set<string>>(new Set());

  // ONE full refetch path: the workers list carries everything the rows render,
  // so there is nothing left for a delta-specific cheaper variant to skip —
  // beyond re-reading a SINGLE row, which is what patchOne below does.
  const refetch = useCallback(async () => {
    const next = sortWorkers(await api.listOutsourceWorkers());
    heldRef.current = new Set(next.map((w) => w.id));
    setWorkers(next);
    setError(false);
  }, []);

  useEffect(() => {
    let alive = true;

    refetch()
      .catch((e) => {
        console.warn("useOutsourceWorkers: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    loadServerSettings()
      .then((s) => {
        if (alive) setMaxParallel(s.outsourceMaxParallel);
      })
      .catch((e) =>
        console.warn("useOutsourceWorkers: settings load failed", e)
      );

    // Re-read exactly the named rows, in place. The order is the bound task's
    // created_ts and these topics cannot change it, so no re-sort is needed —
    // and must not happen, or a row would jump for a badge change.
    const patchOne = (ids: string[]) =>
      Promise.all(ids.map((id) => api.getOutsourceWorker(id)))
        .then((fresh) => {
          if (!alive) return;
          setWorkers((prev) =>
            prev.map((w) => fresh.find((f) => f.id === w.id) ?? w)
          );
          setError(false);
        })
        .catch((e) => {
          console.warn("useOutsourceWorkers: worker refetch failed", e);
          return refetch().catch(() => {});
        });

    // ONE decision per burst of deltas: a resync fans 13 topics synchronously
    // and this hook listens to four of them — that used to be four identical
    // list re-pulls for one reconnect.
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        const mine = [...batch.topics].filter((t) => WORKER_TOPICS.has(t));
        if (mine.length === 0) return;
        const badgeOnly = mine.every((t) => BADGE_ONLY_TOPICS.has(t));
        const touched = badgeOnly
          ? narrowToHeld(batch, (id) => heldRef.current.has(id))
          : null;
        if (touched === null) {
          refetch().catch((e) =>
            console.warn("useOutsourceWorkers: SSE refetch failed", e)
          );
          return;
        }
        // 🔴 T-b17f: the rail's badge is the SAME `UnreadCounts(…, owner)` fold
        // (`api_outsource.go` :136/:199/:358 all pass `currentActor(r)`), so a
        // chat line NOT addressed to the owner cannot move it — and each of
        // those handlers pays a full `ListChat()` table scan to answer. This
        // rail sees plenty of such traffic: a member talking to a worker names
        // that worker, and used to cost one `GET /api/outsource-workers/{id}`
        // for a row whose number could not have changed.
        //
        // ⚠️ It really is only that half. `ow-1 → owner` is a GENUINE refetch —
        // the recipient IS the owner, so that row's badge really moves — and the
        // predicate keeps it, because it asks about `to`, not about whether a
        // worker was named. There is a control test for exactly that shape.
        if (burstMovesNoOwnerUnread(batch, mine)) return;
        // Named somebody, none of them a worker on this rail: a chat line cannot
        // assign or release a worker (that is the "outsource_worker" topic), so
        // every chat line between the owner and a MEMBER used to re-pull this
        // whole list for a badge that could not move.
        if (touched.length > 0) void patchOne(touched);
      })
    );

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [refetch]);

  const saveMaxParallel = useCallback(async (n: number) => {
    const next = await api.patchServerSettings({ outsourceMaxParallel: n });
    adoptServerSettings(next); // shared snapshot invalidation point (T-8115)
    setMaxParallel(next.outsourceMaxParallel);
  }, []);

  return { workers, loading, error, maxParallel, saveMaxParallel };
}
