// The pre-React paint entry. index.html loads this module BEFORE main.tsx, so
// the cached theme is on <html> before React exists. It hands the token list to
// window.__ocPaintTokens; the React apply effect adopts it as its ledger seed,
// so there is still exactly ONE ledger.
import { LS_THEME, readValidatedPaint, applyThemeToRoot } from "../lib/themePaint";
import { isBuiltinTheme } from "../lib/themeBundleCore";

declare global {
  interface Window {
    __ocPaintTokens?: string[];
  }
}

try {
  let theme: string | null = null;
  try {
    theme = localStorage.getItem(LS_THEME);
  } catch {
    theme = null;
  }
  if (theme && isBuiltinTheme(theme)) {
    // A built-in's colours live in theme.css keyed off this attribute; without
    // it a light pick would paint dark until React mounts.
    document.documentElement.dataset.theme = theme;
  } else if (theme) {
    const bundle = readValidatedPaint();
    if (bundle && bundle.id === theme) {
      document.documentElement.dataset.theme = "office";
      window.__ocPaintTokens = applyThemeToRoot(document.documentElement, bundle);
    }
  }
} catch {
  // never let the pre-paint break the app: fall through to today's behaviour.
}

export {};
