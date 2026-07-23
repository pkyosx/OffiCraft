// hooks/useMemberCurrentTasks.ts — the staff roster's "current task" join
// (T-3451). A 正職 member has no task field on its own DTO (unlike an outsource
// worker, whose bound task rides OutsourceWorkerView.taskTitle on the wire), so
// the office page derives each member's CURRENT task by joining the unfiltered
// task list on the FRONTEND — no wire change.
//
// "Current task" = the member's most-recently-updated NON-CLOSED task where
// executorKind === "member" && executorId === member.id (owner ruling
// 2026-07-23: a member may hold several open tasks at once; show the freshest
// by updated_ts). A member with no open task resolves to `undefined` — the row
// / header then renders the honest empty state (never a fabricated task).
//
// Reconcile-by-refetch (contract B), mirroring useMembers / useOutsourceWorkers:
// a "task" SSE delta (create / plan / status / priority / terminate / reassign)
// re-pulls the list; "member" rides along because a roster change (a new
// executor id) can change who a task binds to. Chat topics are ignored — a chat
// line never moves a task's title/executor.

import { useCallback, useEffect, useState } from "react";
import type { TaskView } from "../api/adapter";
import { api } from "../api";

export interface MemberCurrentTask {
  taskId: string;
  /** Display number (T-xxxx) — presentation only. */
  taskNo: string;
  /** The task's real title (NOT the type / manual display name). */
  title: string;
}

// deriveMemberCurrentTasks folds the task list into a member.id → current-task
// map. Pure (same result for the same tasks) so it is unit-testable without the
// hook. A task is a candidate when it is assigned to a member and still open
// (closedTs === null); among a member's candidates the freshest updatedTs wins.
export function deriveMemberCurrentTasks(
  tasks: TaskView[],
): Map<string, MemberCurrentTask> {
  const best = new Map<string, TaskView>();
  for (const t of tasks) {
    if (t.executorKind !== "member" || !t.executorId) continue;
    if (t.closedTs !== null) continue; // closed/terminated/duplicated → not current
    const prev = best.get(t.executorId);
    if (!prev || t.updatedTs > prev.updatedTs) best.set(t.executorId, t);
  }
  const out = new Map<string, MemberCurrentTask>();
  for (const [memberId, t] of best) {
    out.set(memberId, { taskId: t.id, taskNo: t.taskNo, title: t.title });
  }
  return out;
}

export function useMemberCurrentTasks(): Map<string, MemberCurrentTask> {
  const [byMember, setByMember] = useState<Map<string, MemberCurrentTask>>(
    () => new Map(),
  );

  const refetch = useCallback(async () => {
    // `{ open: true }` drops terminal (done/terminated/duplicated) rows
    // server-side — we discard every closed task anyway, so this shrinks the
    // payload with zero behaviour change (the closedTs===null filter below
    // stays as belt-and-suspenders for any client/mock that ignores the flag).
    const tasks = await api.listTasks({ open: true });
    setByMember(deriveMemberCurrentTasks(tasks));
  }, []);

  useEffect(() => {
    refetch().catch((e) =>
      // Non-fatal: a failed join simply leaves rows in their empty state — a
      // member's task title is an enrichment, never load-blocking chrome.
      console.warn("useMemberCurrentTasks: initial load failed", e),
    );

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "task" || topic === "member") {
        refetch().catch((e) =>
          console.warn("useMemberCurrentTasks: SSE refetch failed", e),
        );
      }
    });

    return () => unsubscribe();
  }, [refetch]);

  return byMember;
}
