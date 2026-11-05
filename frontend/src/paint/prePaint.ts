// The pre-React paint entry. index.html loads this module BEFORE main.tsx, so
// the cached theme is on <html> before React exists. It hands the token list to
// window.__ocPaintTokens; the React apply effect adopts it as its ledger seed,
// so there is still exactly ONE ledger.
import {
  LS_THEME,
  LS_THEME_PAINT,
  LS_SEASONAL_THEME,
  readValidatedPaint,
  readValidatedSchedulePaint,
  readValidatedSeasonalPaint,
  readThemeSchedule,
  applyThemeToRoot,
} from "../lib/themePaint";
import { isBuiltinTheme } from "../lib/themeBundleCore";
import { SEASONAL_SCHEDULE, activeSeasonalWindow, seasonalPickAt } from "../lib/seasonalSchedule";
import { activeScheduleEntry } from "../lib/themeSchedule";

declare global {
  interface Window {
    __ocPaintTokens?: string[];
  }
}

function readKey(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

try {
  const now = new Date();
  const scheduled = activeScheduleEntry(now, readThemeSchedule())?.theme;
  const theme = scheduled ?? readKey(LS_THEME);
  const seasonal =
    readKey(LS_SEASONAL_THEME) !== "false"
      ? activeSeasonalWindow(now, SEASONAL_SCHEDULE)
      : null;
  if (seasonal) {
    // Inside the window the display theme is not what React will show, so its
    // picture is never painted here — only the seasonal one (this day part's),
    // or nothing.
    const bundle = readValidatedSeasonalPaint(seasonalPickAt(now, seasonal).id);
    if (bundle) {
      document.documentElement.dataset.theme = "office";
      window.__ocPaintTokens = applyThemeToRoot(document.documentElement, bundle);
    }
  } else if (theme && isBuiltinTheme(theme)) {
    // A built-in's colours live in theme.css keyed off this attribute; without
    // it a light pick would paint dark until React mounts.
    document.documentElement.dataset.theme = theme;
  } else if (theme) {
    const bundle = scheduled
      ? readValidatedSchedulePaint(theme)
      : readValidatedPaint(LS_THEME_PAINT);
    if (bundle && bundle.id === theme) {
      document.documentElement.dataset.theme = "office";
      window.__ocPaintTokens = applyThemeToRoot(document.documentElement, bundle);
    }
  }
} catch {
  // never let the pre-paint break the app: fall through to today's behaviour.
}

export {};
