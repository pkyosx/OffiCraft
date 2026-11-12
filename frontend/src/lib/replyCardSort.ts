// The 請示 page's 排序依據 for the waiting pane. Every order falls back to the
// newest-opened card first on a tie.

import type { ReplyCardRow } from "../api/adapter";
import { priorityRank } from "./taskPriority";

export const WAITING_SORTS = ["createdDesc", "createdAsc", "priority", "due"] as const;
export type WaitingSort = (typeof WAITING_SORTS)[number];

export const WAITING_SORT_KEY = "oc_replies_sort";

// After frozen: a card that names no task has no priority to rank by.
const NO_TASK_RANK = priorityRank("frozen") + 1;

const rankOf = (row: ReplyCardRow) =>
  row.task ? priorityRank(row.task.priority ?? "") : NO_TASK_RANK;

const newestFirst = (a: ReplyCardRow, b: ReplyCardRow) => b.createdTs - a.createdTs;

export function sortWaitingRows(rows: readonly ReplyCardRow[], sort: WaitingSort): ReplyCardRow[] {
  const out = [...rows];
  switch (sort) {
    case "createdDesc":
      return out.sort(newestFirst);
    case "createdAsc":
      return out.sort((a, b) => a.createdTs - b.createdTs);
    case "priority":
      return out.sort((a, b) => rankOf(a) - rankOf(b) || newestFirst(a, b));
    case "due":
      return out.sort(
        (a, b) =>
          (a.task?.dueTs ?? Infinity) - (b.task?.dueTs ?? Infinity) || newestFirst(a, b)
      );
  }
}
