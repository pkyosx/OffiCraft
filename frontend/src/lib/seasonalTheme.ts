import type { ThemeBundle } from "./themeBundleCore";
import {
  SEASONAL_SCHEDULE,
  type SeasonalDayPart,
  type SeasonalScheduleEntry,
  type SeasonalWindow,
} from "./seasonalSchedule";

// A lazy glob keeps every theme file in its own chunk: nothing is fetched until
// a window opens and load() is called. The theme.css built-ins' files feed
// theme.css through gen-builtin-themes.mjs and must not ship as chunks; the
// bundled built-ins are lib/bundledBuiltinThemes.ts's.
const THEME_FILES = import.meta.glob<ThemeBundle>(
  [
    "../../../themes/*.theme.json",
    "!../../../themes/office.theme.json",
    "!../../../themes/office-light.theme.json",
    "!../../../themes/modern-office-desk-*.theme.json",
  ],
  { import: "default" }
);

export interface LoadableSeasonalWindow extends SeasonalWindow {
  schedule?: readonly SeasonalDayPart[];
  /** Loads `file` (a day part's), or the window's own theme file without one. */
  load: (file?: string) => Promise<ThemeBundle>;
}

export function seasonalWindowsFrom(
  schedule: readonly SeasonalScheduleEntry[],
  files: Record<string, () => Promise<ThemeBundle>>
): LoadableSeasonalWindow[] {
  return schedule.map((e) => ({
    id: e.id,
    start: e.start,
    end: e.end,
    ...(e.schedule ? { schedule: e.schedule } : {}),
    load: (name = e.theme) => {
      const file = name ? files[`../../../themes/${name}`] : undefined;
      return file
        ? file()
        : Promise.reject(new Error(`seasonal theme file not found: themes/${name}`));
    },
  }));
}

export const SEASONAL_WINDOWS: readonly LoadableSeasonalWindow[] = seasonalWindowsFrom(
  SEASONAL_SCHEDULE,
  THEME_FILES
);
