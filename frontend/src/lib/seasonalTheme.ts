import type { ThemeBundle } from "./themeBundleCore";
import {
  SEASONAL_SCHEDULE,
  type SeasonalScheduleEntry,
  type SeasonalWindow,
} from "./seasonalSchedule";

// eager:false keeps every theme file in its own chunk: nothing is fetched until
// a window opens and load() is called. The built-in themes' files feed
// theme.css at build time and must not ship as chunks of their own.
const THEME_FILES = import.meta.glob<ThemeBundle>(
  [
    "../../../themes/*.theme.json",
    "!../../../themes/office.theme.json",
    "!../../../themes/office-light.theme.json",
  ],
  { import: "default" }
);

export interface LoadableSeasonalWindow extends SeasonalWindow {
  load: () => Promise<ThemeBundle>;
}

export function seasonalWindowsFrom(
  schedule: readonly SeasonalScheduleEntry[],
  files: Record<string, () => Promise<ThemeBundle>>
): LoadableSeasonalWindow[] {
  return schedule.map((e) => ({
    id: e.id,
    start: e.start,
    end: e.end,
    load: () => {
      const file = files[`../../../themes/${e.theme}`];
      return file
        ? file()
        : Promise.reject(new Error(`seasonal theme file not found: themes/${e.theme}`));
    },
  }));
}

export const SEASONAL_WINDOWS: readonly LoadableSeasonalWindow[] = seasonalWindowsFrom(
  SEASONAL_SCHEDULE,
  THEME_FILES
);
