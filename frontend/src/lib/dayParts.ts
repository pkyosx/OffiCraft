// The day-part clock shared by the user's theme schedule and a seasonal
// window's own day parts: rows of {start: "HH:MM", …}, viewer-local. Bundled
// with esbuild by the pre-paint script, and imported by seasonalSchedule.ts,
// so it imports nothing: no import.meta or other Vite-only syntax here.

export const MIN_DAY_PARTS = 2;
export const MAX_DAY_PARTS = 6;

const START_RE = /^([01]\d|2[0-3]):[0-5]\d$/;

export type DayPartProblem = "count" | "start" | "duplicate";

/** 0 rows, or 2..6 with unique HH:MM starts. */
export function dayPartsProblem(rows: readonly { start: string }[]): DayPartProblem | null {
  if (rows.length !== 0 && (rows.length < MIN_DAY_PARTS || rows.length > MAX_DAY_PARTS)) {
    return "count";
  }
  const seen = new Set<string>();
  for (const r of rows) {
    if (!START_RE.test(r.start)) return "start";
    if (seen.has(r.start)) return "duplicate";
    seen.add(r.start);
  }
  return null;
}

function minutesOf(start: string): number {
  return Number(start.slice(0, 2)) * 60 + Number(start.slice(3, 5));
}

/** The row whose start is the latest at or before `now` (local time); before
 * the first start of the day it is the previous day's last row. */
export function activeDayPart<R extends { start: string }>(now: Date, rows: readonly R[]): R | null {
  const t = now.getHours() * 60 + now.getMinutes();
  let today: R | null = null;
  let last: R | null = null;
  for (const r of rows) {
    const m = minutesOf(r.start);
    if (m <= t && (!today || m > minutesOf(today.start))) today = r;
    if (!last || m > minutesOf(last.start)) last = r;
  }
  return today ?? last;
}

/** The first start strictly after `now`, with the row it begins; null without rows. */
export function nextDayPart<R extends { start: string }>(
  now: Date,
  rows: readonly R[]
): { at: Date; row: R } | null {
  let next: { at: number; row: R } | null = null;
  for (const r of rows) {
    const m = minutesOf(r.start);
    for (const dayOffset of [0, 1]) {
      const b = new Date(
        now.getFullYear(),
        now.getMonth(),
        now.getDate() + dayOffset,
        Math.floor(m / 60),
        m % 60
      ).getTime();
      if (b > now.getTime() && (next === null || b < next.at)) next = { at: b, row: r };
    }
  }
  return next && { at: new Date(next.at), row: next.row };
}
