// Due-date math for the task card chip and the 任務頁 due filters. All day
// math is the viewer's LOCAL calendar; every "now" is a parameter.

export const DUE_SOON_WINDOW_S = 86_400;

export type DueState =
  | { kind: "later"; month: number; day: number }
  | { kind: "today" }
  | { kind: "tomorrow" }
  | { kind: "overdue"; days: number };

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

export function isDueSoon(dueTs: number, nowTs: number): boolean {
  return nowTs < dueTs && dueTs <= nowTs + DUE_SOON_WINDOW_S;
}

export function dueState(dueTs: number, nowTs: number): DueState {
  if (isOverdue(dueTs, nowTs)) {
    return { kind: "overdue", days: calendarDayDiff(dueTs, nowTs) };
  }
  if (isDueSoon(dueTs, nowTs)) {
    return calendarDayDiff(nowTs, dueTs) === 0
      ? { kind: "today" }
      : { kind: "tomorrow" };
  }
  const d = new Date(dueTs * 1000);
  return { kind: "later", month: d.getMonth() + 1, day: d.getDate() };
}

/** The next moment the chip's colour changes on its own (entering the 24h
 * window, then passing due); null once overdue. Day-label rollovers at local
 * midnight are not included — useNowSeconds covers those. */
export function nextDueChangeTs(dueTs: number, nowTs: number): number | null {
  const soonAt = dueTs - DUE_SOON_WINDOW_S;
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
