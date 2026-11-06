// The task card's read-only chips, for surfaces that show a task without being
// one (the 請示 page's task row). Same classes as TaskCard's own, so a task's
// number, priority and due colour look the same wherever it is named.

import { useI18n } from "../i18n";
import { useDueState } from "../hooks/useDueState";
import { dueRemaining, hasDueTime } from "../lib/dueDate";
import { TasksIcon } from "./icons";
import "./tasks.css";

export function TaskNoBadge({ taskNo, testId }: { taskNo: string; testId?: string }) {
  return (
    <span className="task-card__id-badge" data-testid={testId}>
      <TasksIcon size={13} />#{taskNo}
    </span>
  );
}

export function TaskPriorityBadge({
  priority,
  testId,
}: {
  priority: string;
  testId?: string;
}) {
  const { t } = useI18n();
  return (
    <span className={`task-badge task-badge--prio-${priority}`} data-testid={testId}>
      {t.tasks.priority[priority] ?? priority}
    </span>
  );
}

/** The due date with its remaining time, yellow inside the lead and red once
 * passed — re-coloured at the exact moment it crosses either line. */
export function TaskDueChip({
  dueTs,
  leadMinutes,
  testId,
}: {
  dueTs: number;
  /** The task's own lead, else the workspace's (`effectiveLeadMinutes`). */
  leadMinutes: number;
  testId?: string;
}) {
  const { t, msg } = useI18n();
  const { nowSec, due } = useDueState(dueTs, leadMinutes);
  const tone = due === null || due.kind === "later" ? "" : ` task-card__due-date--${due.kind}`;
  return (
    <span className={`task-card__chip task-card__due-chip${tone}`} data-testid={testId}>
      {hasDueTime(dueTs) ? msg.taskDueDateTime(dueTs) : msg.taskDueDate(dueTs)}
      <span className={`task-card__due-remaining${tone}`}>
        {msg.taskDueRemaining(dueRemaining(dueTs, nowSec))}
      </span>
      {due?.kind === "soon" && (
        <span className="task-card__due-status">{t.tasks.dueSoonHint}</span>
      )}
    </span>
  );
}
