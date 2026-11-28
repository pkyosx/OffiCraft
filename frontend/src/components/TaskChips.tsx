// Chips for surfaces that show a task without being one (the 請示 page's task
// row, the outsource row). Same classes as the owners' own, so a task's number,
// priority and due colour look the same wherever it is named.

import { useI18n } from "../i18n";
import { useDueState } from "../hooks/useDueState";
import { formatDue } from "../lib/dueDate";
import { JumpToMessageIcon } from "./replyCardActionIcons";
import "./tasks.css";
import "./office.css";
import "./card-meta.css";

/** The task-number chip that jumps to the task, led by the jump icon every
 * id that takes you somewhere wears: the outsource rail row's, the worker chat
 * header's and the reply card's. stopPropagation so the jump never also
 * toggles the row or card it sits in. */
export function TaskIdChip({
  taskNo,
  label,
  onOpen,
  testId,
}: {
  taskNo: string;
  label: string;
  onOpen: () => void;
  testId?: string;
}) {
  return (
    <button
      type="button"
      className="outsource-row__chip outsource-row__chip--task"
      title={label}
      aria-label={label}
      data-testid={testId}
      onClick={(e) => {
        e.stopPropagation();
        onOpen();
      }}
      onKeyDown={(e) => e.stopPropagation()}
    >
      <JumpToMessageIcon size={11} />
      {taskNo}
    </button>
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
  const { t } = useI18n();
  const { nowSec, due } = useDueState(dueTs, leadMinutes);
  const tone = due === null || due.kind === "later" ? "" : ` task-card__due-date--${due.kind}`;
  const text = formatDue(dueTs, nowSec);
  return (
    <span
      className={`card-meta__chip task-card__due-chip${tone}`}
      data-testid={testId}
      title={`${t.replies.taskDueLabel} ${text}`}
    >
      <span className="task-card__due-status">{t.replies.taskDueLabel} </span>
      {text}
      {due !== null && due.kind !== "later" && (
        <span className="task-card__due-status">
          {due.kind === "soon" ? t.tasks.dueSoonHint : t.tasks.dueOverdueHint}
        </span>
      )}
    </span>
  );
}
