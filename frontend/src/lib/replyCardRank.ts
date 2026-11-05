// The owner's reply-card queue order before age: the priority of the task the
// card names, then that task's due date, with a dated card ahead of an undated
// one. A card naming no task (priority null) ranks as mid with no due date.
// Mirrors sortReplyCardQueue on the server; age is the caller's tiebreak.
const PRIORITY_RANK: Record<string, number> = { high: 0, mid: 1, low: 2, frozen: 3 };

type Ranked = { priority?: string | null; dueTs?: number | null };

export function compareReplyCardRank(a: Ranked, b: Ranked): number {
  const ra = PRIORITY_RANK[a.priority ?? "mid"] ?? 3;
  const rb = PRIORITY_RANK[b.priority ?? "mid"] ?? 3;
  if (ra !== rb) return ra - rb;
  const da = a.dueTs ?? null;
  const db = b.dueTs ?? null;
  if (da === null || db === null) return da === db ? 0 : da === null ? 1 : -1;
  return da - db;
}
