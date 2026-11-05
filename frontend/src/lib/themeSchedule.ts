// The time-of-day theme schedule (display_theme_schedule). The pre-paint script
// bundles this module with esbuild on its own: no import.meta or other
// Vite-only syntax here.
import { THEME_ID_RE, isBuiltinTheme } from "./themeBundleCore";
import {
  MAX_DAY_PARTS,
  MIN_DAY_PARTS,
  activeDayPart,
  dayPartsProblem,
  nextDayPart,
  type DayPartProblem,
} from "./dayParts";

export interface ThemeScheduleEntry {
  /** "HH:MM", viewer-local wall clock. */
  start: string;
  theme: string;
}

/** The appearance a fresh install gets and 恢復預設 restores, with the
 * seasonal theme on. ⚠️ Twin of the literals in server migration 00120, which
 * wrote it onto every station still on office / office-light. */
export const DEFAULT_THEME_SCHEDULE: readonly ThemeScheduleEntry[] = [
  { start: "06:00", theme: "modern-office-desk-day" },
  { start: "16:00", theme: "modern-office-desk-dusk" },
  { start: "18:30", theme: "modern-office-desk-night" },
];
/** display_theme under the default: what shows if the schedule is turned off. */
export const DEFAULT_DISPLAY_THEME = "modern-office-desk-day";

export const MIN_SCHEDULE_ENTRIES = MIN_DAY_PARTS;
export const MAX_SCHEDULE_ENTRIES = MAX_DAY_PARTS;

export type ThemeScheduleProblem = DayPartProblem | "theme";

/** The server's shape rules (theme existence aside): 0 entries, or 2..6 with
 * unique HH:MM starts. */
export function themeScheduleProblem(
  entries: readonly ThemeScheduleEntry[]
): ThemeScheduleProblem | null {
  const shape = dayPartsProblem(entries);
  if (shape) return shape;
  return entries.some((e) => !e.theme) ? "theme" : null;
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
    if (!isBuiltinTheme(theme) && !THEME_ID_RE.test(theme)) return [];
    out.push({ start, theme });
  }
  if (themeScheduleProblem(out) !== null) return [];
  return out.sort((a, b) => (a.start < b.start ? -1 : a.start > b.start ? 1 : 0));
}

/** The entry whose start is the latest at or before `now` (local time); before
 * the first start of the day it is the previous day's last entry. */
export function activeScheduleEntry(
  now: Date,
  schedule: readonly ThemeScheduleEntry[]
): ThemeScheduleEntry | null {
  return activeDayPart(now, schedule);
}

/** The first schedule start strictly after `now`, or null without a schedule. */
export function nextScheduleBoundary(
  now: Date,
  schedule: readonly ThemeScheduleEntry[]
): Date | null {
  return nextDayPart(now, schedule)?.at ?? null;
}
