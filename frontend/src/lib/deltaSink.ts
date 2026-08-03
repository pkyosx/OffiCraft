// lib/deltaSink.ts — one refetch decision per BURST of SSE deltas, instead of
// one per delta (T-8115).
//
// Why this exists: a resync (api/http.ts `resyncAll`) fans one synthetic delta
// per closed topic — 13 of them — SYNCHRONOUSLY to every subscriber. A hook that
// listens to four of those topics therefore ran four identical refetches for one
// resync, and there is exactly one resync per reconnect AND per return to the
// foreground. Measured on the six-hook cockpit harness: one resync cost 21
// requests, of which 12 were duplicates of a request already in flight.
//
// The coalescing has to happen HERE — at the point that decides to refetch — and
// not in the fan-out: the transport cannot know which topics a given subscriber
// reacts to, so it cannot know that its 13 calls will collapse into one refetch.
// What it CAN guarantee is that the fan is synchronous, so accumulating whatever
// arrives before the next microtask captures the whole burst exactly.
//
// It also, deliberately, coalesces bursts of REAL deltas that land in the same
// tick. That is sound because a refetch answers "what is the current truth of
// this list", never "apply this one change": two deltas in one tick have one
// answer. What it must NOT do is coalesce across ticks — that would be a
// debounce, i.e. a deliberate delay of the screen, which nothing here wants.

import type { SseDelta } from "../api/adapter";

/** Everything one burst of deltas said, unioned. */
export interface DeltaBatch {
  /** The topics that arrived. A hook filters on this exactly as it used to
   * filter on the single `topic` argument. */
  topics: Set<string>;
  /** Every entity the burst NAMED (identity only — see `SseDelta`). Empty means
   * nothing was named, so nothing narrower than a full refetch is justified. */
  ids: Set<string>;
  /** The deltas themselves, in arrival order — for the rare consumer that needs
   * a name's ROLE (e.g. "was the reader the peer?") and not just its value. */
  deltas: SseDelta[];
  /** True when at least one delta in the burst named NOTHING (a resync, or a
   * topic whose payload is null, or a transport that supplies no delta at all).
   * Such a burst can never be served by a per-item refetch: it is the honest
   * "you may have missed anything". */
  unnamed: boolean;
}

/**
 * Wrap a batch handler into a `subscribeEvents` callback. Every delta that
 * arrives before the next microtask is folded into ONE batch, and `run` is
 * called once with it.
 *
 * `run` must be safe to call from a microtask (i.e. it may not assume it is
 * inside the React event that produced the delta) — every current caller only
 * kicks off a fetch, which was already asynchronous.
 */
export function createDeltaSink(
  run: (batch: DeltaBatch) => void
): (topic: string, delta?: SseDelta) => void {
  let pending: DeltaBatch | null = null;
  return (topic: string, delta?: SseDelta) => {
    if (pending === null) {
      pending = {
        topics: new Set(),
        ids: new Set(),
        deltas: [],
        unnamed: false,
      };
      // Capture the batch and clear the slot BEFORE running, so a delta that
      // arrives while `run` is on the stack starts the next batch instead of
      // being silently folded into one already being acted on.
      queueMicrotask(() => {
        const batch = pending as DeltaBatch;
        pending = null;
        run(batch);
      });
    }
    pending.topics.add(topic);
    if (delta === undefined || delta.ids.length === 0) {
      pending.unnamed = true;
    }
    if (delta !== undefined) {
      pending.deltas.push(delta);
      for (const id of delta.ids) pending.ids.add(id);
    }
  };
}

/**
 * Narrow a batch to the ids this consumer is already holding.
 *
 * Three outcomes, and the difference between the last two is the whole point:
 *  - `null` — the batch cannot be narrowed at all (something in it named
 *    nothing). Only a full refetch answers it.
 *  - a NON-EMPTY array — exactly the held items the batch named.
 *  - an EMPTY array — the batch named things, and NONE of them is ours. That is
 *    not the same as "unknown": for a topic that cannot change this consumer's
 *    list membership, it means there is genuinely nothing to do. A caller whose
 *    membership CAN change on the topic in hand must treat it as a full refetch
 *    instead (a brand-new row is only discoverable from the list).
 */
export function narrowToHeld(
  batch: DeltaBatch,
  held: (id: string) => boolean
): string[] | null {
  if (batch.unnamed) return null;
  const touched: string[] = [];
  for (const id of batch.ids) {
    if (held(id)) touched.push(id);
  }
  return touched;
}
