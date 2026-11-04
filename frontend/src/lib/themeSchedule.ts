// The time-of-day theme schedule (display_theme_schedule). The pre-paint script
// bundles this module with esbuild on its own: no import.meta or other
// Vite-only syntax here.
import { isBuiltinTheme } from "./themeBundleCore";

export interface ThemeScheduleEntry {
  /** "HH:MM", viewer-local wall clock. */
  start: string;
  theme: string;
}

export const MIN_SCHEDULE_ENTRIES = 2;
export const MAX_SCHEDULE_ENTRIES = 6;

const START_RE = /^([01]\d|2[0-3]):[0-5]\d$/;
const CUSTOM_THEME_ID_RE = /^[a-z0-9][a-z0-9-]{1,63}$/;

export type ThemeScheduleProblem = "count" | "start" | "duplicate" | "theme";

/** The server's shape rules (theme existence aside): 0 entries, or 2..6 with
 * unique HH:MM starts. */
export function themeScheduleProblem(
  entries: readonly ThemeScheduleEntry[]
): ThemeScheduleProblem | null {
  if (
    entries.length !== 0 &&
    (entries.length < MIN_SCHEDULE_ENTRIES || entries.length > MAX_SCHEDULE_ENTRIES)
  ) {
    return "count";
  }
  const seen = new Set<string>();
  for (const e of entries) {
    if (!START_RE.test(e.start)) return "start";
    if (seen.has(e.start)) return "duplicate";
    seen.add(e.start);
    if (!e.theme) return "theme";
  }
  return null;
}

/** Validate an untrusted value (the localStorage cache is attacker-writable)
 * into a schedule sorted by start; anything malformed is no schedule. */
export function parseThemeSchedule(raw: unknown): ThemeScheduleEntry[] {
  if (!Array.isArray(raw)) return [];
  const out: ThemeScheduleEntry[] = [];
  for (const e of raw) {
    if (typeof e !== "object" || e === null) return [];
    const { start, theme } = e as Record<string, unknown>;
    if (typeof start !== "string" || typeof theme !== "string") return [];
    if (!isBuiltinTheme(theme) && !CUSTOM_THEME_ID_RE.test(theme)) return [];
    out.push({ start, theme });
  }
  if (themeScheduleProblem(out) !== null) return [];
  return out.sort((a, b) => (a.start < b.start ? -1 : a.start > b.start ? 1 : 0));
}

function minutesOf(start: string): number {
  return Number(start.slice(0, 2)) * 60 + Number(start.slice(3, 5));
}

/** The entry whose start is the latest at or before `now` (local time); before
 * the first start of the day it is the previous day's last entry. */
export function activeScheduleEntry(
  now: Date,
  schedule: readonly ThemeScheduleEntry[]
): ThemeScheduleEntry | null {
  const t = now.getHours() * 60 + now.getMinutes();
  let today: ThemeScheduleEntry | null = null;
  let last: ThemeScheduleEntry | null = null;
  for (const e of schedule) {
    const m = minutesOf(e.start);
    if (m <= t && (!today || m > minutesOf(today.start))) today = e;
    if (!last || m > minutesOf(last.start)) last = e;
  }
  return today ?? last;
}

/** The first schedule start strictly after `now`, or null without a schedule. */
export function nextScheduleBoundary(
  now: Date,
  schedule: readonly ThemeScheduleEntry[]
): Date | null {
  let next: number | null = null;
  for (const e of schedule) {
    const m = minutesOf(e.start);
    for (const dayOffset of [0, 1]) {
      const b = new Date(
        now.getFullYear(),
        now.getMonth(),
        now.getDate() + dayOffset,
        Math.floor(m / 60),
        m % 60
      ).getTime();
      if (b > now.getTime() && (next === null || b < next)) next = b;
    }
  }
  return next === null ? null : new Date(next);
}
