// The owner's reply-card queue order before age: priority, then due date, with
// a card that has a due date ahead of one that has none. Mirrors
// sortReplyCardQueue on the server; age is the caller's tiebreak.
const PRIORITY_RANK: Record<string, number> = { high: 0, mid: 1, low: 2 };

type Ranked = { priority?: string; dueTs?: number | null };

export function compareReplyCardRank(a: Ranked, b: Ranked): number {
  const ra = PRIORITY_RANK[a.priority ?? "mid"] ?? 1;
  const rb = PRIORITY_RANK[b.priority ?? "mid"] ?? 1;
  if (ra !== rb) return ra - rb;
  const da = a.dueTs ?? null;
  const db = b.dueTs ?? null;
  if (da === null || db === null) return da === db ? 0 : da === null ? 1 : -1;
  return da - db;
}
