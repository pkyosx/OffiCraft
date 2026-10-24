// Due-date math for the task card's 到期日 row and the 任務頁 due lanes. All day
// math is the viewer's LOCAL calendar; every "now" is a parameter.

/** The server's shipped `task_due_soon_lead_days`. */
export const DEFAULT_DUE_SOON_LEAD_DAYS = 1;

const DAY_S = 86_400;

export type DueState =
  | { kind: "later" }
  | { kind: "soon" }
  | { kind: "overdue"; days: number };

/** The lead a task is judged by: its own, else the workspace's. */
export function effectiveLeadDays(
  taskLeadDays: number | null | undefined,
  workspaceLeadDays: number
): number {
  return taskLeadDays != null && taskLeadDays > 0 ? taskLeadDays : workspaceLeadDays;
}

function localDayNumber(tsSeconds: number): number {
  const d = new Date(tsSeconds * 1000);
  return Math.round(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()) / 86_400_000);
}

/** Calendar days from `fromTs` to `toTs`, local time (DST-safe). */
export function calendarDayDiff(fromTs: number, toTs: number): number {
  return localDayNumber(toTs) - localDayNumber(fromTs);
}

export function isOverdue(dueTs: number, nowTs: number): boolean {
  return dueTs <= nowTs;
}

export function isDueSoon(dueTs: number, nowTs: number, leadDays: number): boolean {
  return nowTs < dueTs && dueTs <= nowTs + leadDays * DAY_S;
}

export function dueState(dueTs: number, nowTs: number, leadDays: number): DueState {
  if (isOverdue(dueTs, nowTs)) {
    return { kind: "overdue", days: calendarDayDiff(dueTs, nowTs) };
  }
  if (isDueSoon(dueTs, nowTs, leadDays)) return { kind: "soon" };
  return { kind: "later" };
}

/** The next moment the due state changes on its own (entering the lead
 * window, then passing due); null once overdue. The 逾期 N 天 count rolling
 * over at local midnight is not included — useNowSeconds covers that. */
export function nextDueChangeTs(
  dueTs: number,
  nowTs: number,
  leadDays: number
): number | null {
  const soonAt = dueTs - leadDays * DAY_S;
  if (nowTs < soonAt) return soonAt;
  if (nowTs < dueTs) return dueTs;
  return null;
}

/** 23:59:59 local of a `YYYY-MM-DD` date-input value, as epoch seconds. */
export function endOfLocalDayTs(dateValue: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dateValue);
  if (!m) return null;
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), 23, 59, 59);
  return Math.floor(d.getTime() / 1000);
}

/** The local `YYYY-MM-DD` of an epoch, for a date input's value. */
export function toDateInputValue(tsSeconds: number): string {
  const d = new Date(tsSeconds * 1000);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export type DueTone = "overdue" | "soon";

/** The most urgent due state among the open tasks (the 任務 tab's clock). */
export function dueToneOf(counts: { overdue: number; dueSoon: number }): DueTone | null {
  if (counts.overdue > 0) return "overdue";
  if (counts.dueSoon > 0) return "soon";
  return null;
}
