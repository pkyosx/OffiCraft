// The seasonal schedule is data (themes/seasonal-schedule.json at the repo
// root). This module must stay importable by the pre-paint script, which
// esbuild bundles on its own: no import.meta.glob or other Vite-only syntax
// here — the theme files are loaded by seasonalTheme.ts.
import schedule from "../../../themes/seasonal-schedule.json" with { type: "json" };
import { activeDayPart, dayPartsProblem, nextDayPart } from "./dayParts";

export interface SeasonalWindow {
  id: string;
  /** Inclusive, viewer-local. */
  start: Date;
  /** Exclusive, viewer-local. */
  end: Date;
}

/** One day part of a seasonal window: from `start` ("HH:MM", viewer-local)
 * the window shows the theme file `theme`, whose bundle id is its file stem. */
export interface SeasonalDayPart {
  start: string;
  theme: string;
}

/** Exactly one of `theme` (one file for the whole window) or `schedule` (2..6
 * day parts) is set; seasonalEntryProblem holds the shipped file to that. */
export interface SeasonalScheduleEntry extends SeasonalWindow {
  /** File name of the theme bundle inside themes/. */
  theme?: string;
  schedule?: readonly SeasonalDayPart[];
}

type SeasonalShape = SeasonalWindow & { theme?: string; schedule?: readonly SeasonalDayPart[] };

const THEME_FILE_RE = /^([a-z0-9][a-z0-9-]{1,63})\.theme\.json$/;

/** The bundle id a day part's theme file must carry: its stem. */
export function themeIdOfFile(file: string): string {
  return file.replace(/\.theme\.json$/, "");
}

/** What the window shows at `now`: the bundle id to expect (and to key its
 * paint record on) and the file to load. A single-theme window's bundle carries
 * the window's id. */
export function seasonalPickAt(now: Date, w: SeasonalShape): { id: string; file: string | undefined } {
  const row = w.schedule?.length ? activeDayPart(now, w.schedule) : null;
  return row ? { id: themeIdOfFile(row.theme), file: row.theme } : { id: w.id, file: w.theme };
}

/** Every bundle id a seasonal entry can show. */
export function seasonalThemeIds(e: { id: string; schedule?: readonly SeasonalDayPart[] }): string[] {
  return e.schedule?.length ? e.schedule.map((r) => themeIdOfFile(r.theme)) : [e.id];
}

/** Why one raw seasonal-schedule.json entry is malformed, or null. Dates are
 * checked by the caller (parseLocalDateTime). */
export function seasonalEntryProblem(raw: unknown): string | null {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return "not an object";
  const e = raw as Record<string, unknown>;
  const keys = Object.keys(e).sort().join(",");
  if (keys !== "end,id,start,theme" && keys !== "end,id,schedule,start") {
    return "needs id, start, end and exactly one of theme or schedule";
  }
  if (typeof e.id !== "string" || typeof e.start !== "string" || typeof e.end !== "string") {
    return "id, start and end must be strings";
  }
  if ("theme" in e) {
    return typeof e.theme === "string" && THEME_FILE_RE.test(e.theme) ? null : "theme must name a .theme.json file";
  }
  if (!Array.isArray(e.schedule)) return "schedule must be a list";
  const rows = e.schedule as unknown[];
  for (const r of rows) {
    if (typeof r !== "object" || r === null || Object.keys(r).sort().join(",") !== "start,theme") {
      return "each schedule row needs exactly start and theme";
    }
    const { start, theme } = r as Record<string, unknown>;
    if (typeof start !== "string" || typeof theme !== "string" || !THEME_FILE_RE.test(theme)) {
      return "each schedule row's start is HH:MM and its theme names a .theme.json file";
    }
  }
  const shape = dayPartsProblem(rows as SeasonalDayPart[]);
  if (shape === "count" || rows.length === 0) return "schedule must have 2 to 6 rows";
  if (shape === "start") return "schedule starts must be HH:MM, 00:00 through 23:59";
  if (shape === "duplicate") return "schedule starts must be unique";
  return null;
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
  raw: readonly {
    id: string;
    start: string;
    end: string;
    theme?: string;
    schedule?: readonly SeasonalDayPart[];
  }[]
): SeasonalScheduleEntry[] {
  return raw.map((e) => ({
    id: e.id,
    start: parseLocalDateTime(e.start),
    end: parseLocalDateTime(e.end),
    ...(e.schedule
      ? { schedule: [...e.schedule].sort((a, b) => (a.start < b.start ? -1 : a.start > b.start ? 1 : 0)) }
      : { theme: e.theme }),
  }));
}

export const SEASONAL_SCHEDULE: readonly SeasonalScheduleEntry[] = scheduleFrom(schedule);

export function activeSeasonalWindow<W extends SeasonalWindow>(
  now: Date,
  windows: readonly W[]
): W | null {
  const t = now.getTime();
  // Overlapping windows are allowed; the one listed first wins (owner ruling).
  return windows.find((w) => w.start.getTime() <= t && t < w.end.getTime()) ?? null;
}

/** The first window start or end, or day-part start inside an open window,
 * strictly after `now`; null when none is left. */
export function nextSeasonalBoundary(
  now: Date,
  windows: readonly (SeasonalWindow & { schedule?: readonly SeasonalDayPart[] })[]
): Date | null {
  const t = now.getTime();
  let next: number | null = null;
  for (const w of windows) {
    const open = w.start.getTime() <= t && t < w.end.getTime();
    const part = open && w.schedule?.length ? nextDayPart(now, w.schedule)?.at.getTime() : undefined;
    for (const b of [w.start.getTime(), w.end.getTime(), ...(part === undefined ? [] : [part])]) {
      if (b > t && (next === null || b < next)) next = b;
    }
  }
  return next === null ? null : new Date(next);
}
