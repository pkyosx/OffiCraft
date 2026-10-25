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

const pad2 = (n: number) => String(n).padStart(2, "0");

/** A date input's `YYYY-MM-DD` plus an optional time input's `HH:MM`, as
 * local epoch seconds; an empty time is the end of that day (23:59:59). */
export function dueTsFromInputs(dateValue: string, timeValue: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dateValue);
  if (!m) return null;
  const t = timeValue === "" ? null : /^(\d{2}):(\d{2})(?::\d{2})?$/.exec(timeValue);
  if (timeValue !== "" && t === null) return null;
  const [h, min, sec] = t ? [Number(t[1]), Number(t[2]), 0] : [23, 59, 59];
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), h, min, sec);
  return Math.floor(d.getTime() / 1000);
}

/** Whether a due moment carries a time of its own, i.e. is not 23:59:59 local. */
export function hasDueTime(tsSeconds: number): boolean {
  const d = new Date(tsSeconds * 1000);
  return !(d.getHours() === 23 && d.getMinutes() === 59 && d.getSeconds() === 59);
}

/** The local `YYYY-MM-DD` of an epoch, for a date input's value. */
export function toDateInputValue(tsSeconds: number): string {
  const d = new Date(tsSeconds * 1000);
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

/** The local `HH:MM` of an epoch for a time input's value; "" for 23:59:59. */
export function toTimeInputValue(tsSeconds: number): string {
  if (!hasDueTime(tsSeconds)) return "";
  const d = new Date(tsSeconds * 1000);
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

export type DueTone = "overdue" | "soon";

/** The most urgent due state among the open tasks (the 任務 tab's clock). */
export function dueToneOf(counts: { overdue: number; dueSoon: number }): DueTone | null {
  if (counts.overdue > 0) return "overdue";
  if (counts.dueSoon > 0) return "soon";
  return null;
}
