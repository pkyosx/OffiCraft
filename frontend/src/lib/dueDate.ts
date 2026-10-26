// Due-date math for the task card's 到期日 row and the 任務頁 due lanes. All day
// math is the viewer's LOCAL calendar; every "now" is a parameter.

/** The server's shipped `task_due_soon_lead_minutes`. */
export const DEFAULT_DUE_SOON_LEAD_MINUTES = 1440;

export const DUE_SOON_LEAD_MINUTES_MIN = 1;
export const DUE_SOON_LEAD_MINUTES_MAX = 30 * 1440;

const MINUTE_S = 60;

export type LeadUnit = "day" | "hour" | "minute";

export const LEAD_UNITS: readonly LeadUnit[] = ["day", "hour", "minute"];

export const LEAD_UNIT_MINUTES: Record<LeadUnit, number> = { day: 1440, hour: 60, minute: 1 };

/** A lead in the largest unit that divides it exactly (90 → 90 minutes). */
export function splitLeadMinutes(minutes: number): { amount: number; unit: LeadUnit } {
  const unit = LEAD_UNITS.find((u) => minutes % LEAD_UNIT_MINUTES[u] === 0) ?? "minute";
  return { amount: minutes / LEAD_UNIT_MINUTES[unit], unit };
}

/** A whole-number amount draft in `unit` as minutes: "" is null, and
 * undefined means the draft cannot be saved (not a whole number, or outside
 * 1 minute..30 days). */
export function leadMinutesFromDraft(
  amountDraft: string,
  unit: LeadUnit
): number | null | undefined {
  if (amountDraft === "") return null;
  const amount = Number(amountDraft);
  if (!Number.isInteger(amount)) return undefined;
  const minutes = amount * LEAD_UNIT_MINUTES[unit];
  return minutes >= DUE_SOON_LEAD_MINUTES_MIN && minutes <= DUE_SOON_LEAD_MINUTES_MAX
    ? minutes
    : undefined;
}

export type DueState =
  | { kind: "later" }
  | { kind: "soon" }
  | { kind: "overdue"; days: number };

/** The lead a task is judged by: its own, else the workspace's. */
export function effectiveLeadMinutes(
  taskLeadMinutes: number | null | undefined,
  workspaceLeadMinutes: number
): number {
  return taskLeadMinutes != null && taskLeadMinutes > 0 ? taskLeadMinutes : workspaceLeadMinutes;
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

export function isDueSoon(dueTs: number, nowTs: number, leadMinutes: number): boolean {
  return nowTs < dueTs && dueTs <= nowTs + leadMinutes * MINUTE_S;
}

export function dueState(dueTs: number, nowTs: number, leadMinutes: number): DueState {
  if (isOverdue(dueTs, nowTs)) {
    return { kind: "overdue", days: calendarDayDiff(dueTs, nowTs) };
  }
  if (isDueSoon(dueTs, nowTs, leadMinutes)) return { kind: "soon" };
  return { kind: "later" };
}

/** The next moment the due state changes on its own (entering the lead
 * window, then passing due); null once overdue. The 逾期 N 天 count rolling
 * over at local midnight is not included — useNowSeconds covers that. */
export function nextDueChangeTs(
  dueTs: number,
  nowTs: number,
  leadMinutes: number
): number | null {
  const soonAt = dueTs - leadMinutes * MINUTE_S;
  if (nowTs < soonAt) return soonAt;
  if (nowTs < dueTs) return dueTs;
  return null;
}

const HOUR_S = 3600;
const DAY_S = 86_400;

/** How far off the due moment is, in one unit: calendar days from a full day
 * out (and once overdue past local midnight), whole hours under a day, whole
 * minutes (at least 1) under an hour. */
export type DueRemaining = { overdue: boolean; amount: number; unit: LeadUnit };

export function dueRemaining(dueTs: number, nowTs: number): DueRemaining {
  const overdue = isOverdue(dueTs, nowTs);
  const gap = overdue ? nowTs - dueTs : dueTs - nowTs;
  const days = overdue ? calendarDayDiff(dueTs, nowTs) : gap >= DAY_S ? calendarDayDiff(nowTs, dueTs) : 0;
  if (days > 0) return { overdue, amount: days, unit: "day" };
  if (gap >= HOUR_S) return { overdue, amount: Math.floor(gap / HOUR_S), unit: "hour" };
  return { overdue, amount: Math.max(1, Math.floor(gap / MINUTE_S)), unit: "minute" };
}

/** The next moment `dueRemaining`'s answer changes, other than at local
 * midnight (useNowSeconds wakes there anyway); null when only midnight
 * changes it. A second past each boundary, so a timer landing exactly on it
 * already reads the new value. */
export function nextDueRemainingChangeTs(dueTs: number, nowTs: number): number | null {
  if (isOverdue(dueTs, nowTs)) {
    if (calendarDayDiff(dueTs, nowTs) > 0) return null;
    const gap = nowTs - dueTs;
    if (gap >= HOUR_S) return dueTs + (Math.floor(gap / HOUR_S) + 1) * HOUR_S;
    return dueTs + Math.max(2, Math.floor(gap / MINUTE_S) + 1) * MINUTE_S;
  }
  const gap = dueTs - nowTs;
  if (gap >= DAY_S) return dueTs - DAY_S + 1;
  if (gap >= HOUR_S) return dueTs - Math.floor(gap / HOUR_S) * HOUR_S + 1;
  if (gap >= 2 * MINUTE_S) return dueTs - Math.floor(gap / MINUTE_S) * MINUTE_S + 1;
  return dueTs;
}

/** The next moment a card's due colour or remaining time changes. */
export function nextDueWake(dueTs: number, nowTs: number, leadMinutes: number): number | null {
  const wakes = [nextDueChangeTs(dueTs, nowTs, leadMinutes), nextDueRemainingChangeTs(dueTs, nowTs)];
  const pending = wakes.filter((ts): ts is number => ts !== null);
  return pending.length === 0 ? null : Math.min(...pending);
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
