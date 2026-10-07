// The seasonal schedule is data (themes/seasonal-schedule.json at the repo
// root). This module must stay importable by the pre-paint script, which
// esbuild bundles on its own: no import.meta.glob or other Vite-only syntax
// here — the theme files are loaded by seasonalTheme.ts.
import schedule from "../../../themes/seasonal-schedule.json" with { type: "json" };

export interface SeasonalWindow {
  id: string;
  /** Inclusive, viewer-local. */
  start: Date;
  /** Exclusive, viewer-local. */
  end: Date;
}

export interface SeasonalScheduleEntry extends SeasonalWindow {
  /** File name of the theme bundle inside themes/. */
  theme: string;
}

const LOCAL_DATE_TIME_RE = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/;

/** "2026-10-24T00:00" → that wall-clock moment in the viewer's time zone; an
 * invalid Date for anything else, which no window comparison can match. */
export function parseLocalDateTime(s: string): Date {
  const m = LOCAL_DATE_TIME_RE.exec(s);
  if (!m) return new Date(NaN);
  const [y, mo, d, h, mi] = m.slice(1).map(Number);
  const day = new Date(y, mo - 1, d);
  const isCalendarDay =
    day.getFullYear() === y && day.getMonth() === mo - 1 && day.getDate() === d;
  // The hour is not round-tripped: a wall-clock time inside a DST gap does not
  // exist, and the Date constructor moving it forward is the right reading.
  if (!isCalendarDay || h > 23 || mi > 59) return new Date(NaN);
  return new Date(y, mo - 1, d, h, mi);
}

export function scheduleFrom(
  raw: readonly { id: string; start: string; end: string; theme: string }[]
): SeasonalScheduleEntry[] {
  return raw.map((e) => ({
    id: e.id,
    start: parseLocalDateTime(e.start),
    end: parseLocalDateTime(e.end),
    theme: e.theme,
  }));
}

export const SEASONAL_SCHEDULE: readonly SeasonalScheduleEntry[] = scheduleFrom(schedule);

export function activeSeasonalWindow<W extends SeasonalWindow>(
  now: Date,
  windows: readonly W[]
): W | null {
  const t = now.getTime();
  return windows.find((w) => w.start.getTime() <= t && t < w.end.getTime()) ?? null;
}

/** The first window start or end strictly after `now`, or null when none is left. */
export function nextSeasonalBoundary(now: Date, windows: readonly SeasonalWindow[]): Date | null {
  const t = now.getTime();
  let next: number | null = null;
  for (const w of windows) {
    for (const b of [w.start.getTime(), w.end.getTime()]) {
      if (b > t && (next === null || b < next)) next = b;
    }
  }
  return next === null ? null : new Date(next);
}
