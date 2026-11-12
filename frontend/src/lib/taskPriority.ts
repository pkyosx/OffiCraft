// The task priority vocabulary and its sort order, shared by the 任務頁 and the
// 請示 page's 任務優先序 sort so the two can never rank the same task differently.

export const PRIORITIES = ["high", "mid", "low", "frozen"] as const;

// 凍結永遠最後 (spec §2.2); an out-of-vocabulary priority sorts with low —
// never ahead of real high/mid, never displacing frozen's tail position.
const PRIORITY_RANK: Record<string, number> = {
  high: 0,
  mid: 1,
  low: 2,
  frozen: 4,
};

export function priorityRank(priority: string): number {
  return PRIORITY_RANK[priority] ?? PRIORITY_RANK.low;
}
