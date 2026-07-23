// Shared fixtures for the T-3451 current-task-title CT stories. Kept in a
// non-story module so the two stories (roster row + chat header) can share the
// long title + member/worker builders without either importing the other — the
// Playwright CT mount transform injects a registration import per mounted
// component, so each story lives in its OWN file (one component per story file,
// matching the existing visual-guards convention).
import type { OutsourceWorkerView } from "../../src/api/adapter";

// A deliberately LONG title — it must exceed two lines even at the WIDEST tested
// width (a mobile viewport where the rail spans the full width), or the clamp
// guard is vacuous (clientHeight == scrollHeight even unclamped, as an earlier
// too-short title fit in exactly two lines at 390px and never exercised the
// clamp). Long enough that every row/width combo truncates.
export const LONG_TITLE =
  "成員列表顯示每個成員「當前任務 title」：列表列 1–2 行加上超出時的「…」截斷、hover 顯示完整全文；成員詳情 header（聊天區頂端選中成員那一條）顯示完整 title 不截斷，Staff 與 Outsource 兩個 tab 都要適用且窄版不破版";

export function mkWorker(
  over: Partial<OutsourceWorkerView>,
): OutsourceWorkerView {
  return {
    id: "ow-1",
    codename: "O-30",
    model: "Opus 4.6",
    effort: "high",
    status: "active",
    taskId: "t-1",
    taskTitle: LONG_TITLE,
    taskStatus: "in_progress",
    taskNo: "T-3ed8",
    taskTypeName: "OC 開發",
    presence: "online",
    ...over,
  };
}
