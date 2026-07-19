// hooks/useIsMobile.ts — narrow-viewport breakpoint probe (presentation only).
//
// The office is a desktop master-detail layout (roster rail + chat/detail). On a
// phone the two columns cannot coexist, so OfficePage switches to single-page
// navigation (roster XOR chat) driven by this hook. The 720px threshold matches
// the existing `@media (max-width: 720px)` rules in office.css / member-detail.css
// / chrome.css so the JS pivot and the CSS pivot flip at the SAME width (no
// in-between state where the grid collapsed but the nav did not).

import { useEffect, useState } from "react";

/** px width at/below which the app is treated as a phone (single-page nav). */
export const MOBILE_BREAKPOINT = 720;

/**
 * True when the viewport is at/below MOBILE_BREAKPOINT. SSR-safe (defaults to
 * false when `window` is absent) and subscribes to a matchMedia listener so a
 * live resize / orientation change re-renders consumers.
 */
export function useIsMobile(): boolean {
  const query = `(max-width: ${MOBILE_BREAKPOINT}px)`;
  const [isMobile, setIsMobile] = useState<boolean>(() => {
    if (typeof window === "undefined" || !window.matchMedia) return false;
    return window.matchMedia(query).matches;
  });

  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) return;
    const mql = window.matchMedia(query);
    const onChange = (e: MediaQueryListEvent) => setIsMobile(e.matches);
    // Sync once in case the width changed between the initial render and effect.
    setIsMobile(mql.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, [query]);

  return isMobile;
}
