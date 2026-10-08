// The pre-React paint entry. index.html loads this module BEFORE main.tsx, so
// the cached theme is on <html> before React exists. It hands the token list to
// window.__ocPaintTokens; the React apply effect adopts it as its ledger seed,
// so there is still exactly ONE ledger.
import {
  LS_THEME,
  LS_THEME_PAINT,
  LS_SEASONAL_THEME,
  LS_SEASONAL_PAINT,
  readValidatedPaint,
  applyThemeToRoot,
} from "../lib/themePaint";
import { isBuiltinTheme } from "../lib/themeBundleCore";
import { SEASONAL_SCHEDULE, activeSeasonalWindow } from "../lib/seasonalSchedule";

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
  const theme = readKey(LS_THEME);
  const seasonal =
    readKey(LS_SEASONAL_THEME) !== "false"
      ? activeSeasonalWindow(new Date(), SEASONAL_SCHEDULE)
      : null;
  if (seasonal) {
    // Inside the window the display theme is not what React will show, so its
    // picture is never painted here — only the seasonal one, or nothing.
    const bundle = readValidatedPaint(LS_SEASONAL_PAINT);
    if (bundle && bundle.id === seasonal.id) {
      document.documentElement.dataset.theme = "office";
      window.__ocPaintTokens = applyThemeToRoot(document.documentElement, bundle);
    }
  } else if (theme && isBuiltinTheme(theme)) {
    // A built-in's colours live in theme.css keyed off this attribute; without
    // it a light pick would paint dark until React mounts.
    document.documentElement.dataset.theme = theme;
  } else if (theme) {
    const bundle = readValidatedPaint(LS_THEME_PAINT);
    if (bundle && bundle.id === theme) {
      document.documentElement.dataset.theme = "office";
      window.__ocPaintTokens = applyThemeToRoot(document.documentElement, bundle);
    }
  }
} catch {
  // never let the pre-paint break the app: fall through to today's behaviour.
}

export {};
