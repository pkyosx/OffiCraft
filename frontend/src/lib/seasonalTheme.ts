import type { ThemeBundle } from "./themeBundleCore";
import { themeFileLoaders } from "./themeFileFetch";
import {
  SEASONAL_SCHEDULE,
  type SeasonalDayPart,
  type SeasonalScheduleEntry,
  type SeasonalWindow,
} from "./seasonalSchedule";

// Only the URLs are in the app bundle; each theme file ships as its own JSON
// asset, fetched when a window opens and load() is called. The theme.css
// built-ins' files feed theme.css through gen-builtin-themes.mjs and must not
// ship at all; the bundled built-ins are lib/bundledBuiltinThemes.ts's.
const THEME_FILE_URLS = import.meta.glob<string>(
  [
    "../../../themes/*.theme.json",
    "!../../../themes/office.theme.json",
    "!../../../themes/office-light.theme.json",
    "!../../../themes/modern-office-desk-*.theme.json",
  ],
  { query: "?url&no-inline", import: "default", eager: true }
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
  themeFileLoaders(THEME_FILE_URLS)
);
