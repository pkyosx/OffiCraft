import { useEffect, useState } from "react";
import { api, type SseConnectionState } from "../api";
import { useI18n } from "../i18n";
import "./status-banner.css";

/**
 * ConnectionBanner — the cockpit saying out loud that it is no longer live.
 *
 * WHY THIS EXISTS AT ALL. Every other view in this app is delta-driven: the
 * roster, the badges, the thread, the reply cards all sit still until the SSE
 * downlink hands them a reason to move. That makes a DEAD downlink and a QUIET
 * office pixel-for-pixel identical. The owner's report (2026-08-21, relayed
 * verbatim on this ticket) is exactly that shape — 「有時候…要 refresh page 才會
 * 更新」: he could not tell that the page had stopped receiving, only that it
 * had stopped changing, and the only tool he had was F5.
 *
 * So reconnecting silently would NOT have been a fix. It would have swapped a
 * stall the owner can see for one he cannot: a page that quietly drops the
 * stream, quietly rebuilds it, and shows nothing either way still leaves him
 * unable to trust what is on screen. The transport half of the fix (api/http.ts)
 * therefore always ends a recovery in the full `resyncAll`, and this half puts
 * the down period on screen while it lasts.
 *
 * WHAT IT RENDERS, AND WHAT IT DOES NOT:
 *   "live"          → nothing. A healthy stream needs no chrome.
 *   "idle"          → nothing. Nobody is subscribed (logged out) — not a fault.
 *   "unauthorized"  → nothing. The session is dead and AuthGate is already
 *                     dropping the whole app to the login wall; a banner on top
 *                     of a login screen says nothing the login screen does not.
 *   "connecting"    → the bar, but only after GRACE_MS (below).
 */

/**
 * How long the downlink must stay down before the bar appears.
 *
 * NOT cosmetic damping. The browser's own EventSource retry recovers a routine
 * blip in well under a second, and the state legitimately dips to "connecting"
 * on every one of them (plus once on the very first connect, before the first
 * open). A bar that strobes on each of those is a bar the owner learns to look
 * past — which would cost us the exact thing this component is for.
 *
 * The ceiling on this number is set from the other side: it must stay far below
 * the time a person needs to conclude "nothing is happening" on their own. A
 * few seconds of silence reads as normal; that is the window we are allowed to
 * spend, and no more.
 */
export const CONNECTION_BANNER_GRACE_MS = 4000;

export function ConnectionBanner(): JSX.Element | null {
  const { t } = useI18n();
  const [state, setState] = useState<SseConnectionState>("idle");
  // Separate from `state` on purpose: this is "has it been down long enough to
  // be worth saying", which is a different question from "is it down".
  const [showing, setShowing] = useState(false);

  useEffect(() => api.subscribeConnection(setState), []);

  useEffect(() => {
    if (state !== "connecting") {
      // Any resolution — recovered, torn down, or auth-dead — clears the bar
      // immediately. Recovery must be as visible as the loss was.
      setShowing(false);
      return;
    }
    const timer = setTimeout(() => setShowing(true), CONNECTION_BANNER_GRACE_MS);
    return () => clearTimeout(timer);
  }, [state]);

  if (!showing) return null;

  return (
    <div
      className="status-banner"
      role="status"
      aria-live="polite"
      aria-label={t.connection.ariaLabel}
    >
      <span className="status-banner__dot" aria-hidden="true" />
      <span className="status-banner__title">{t.connection.lostTitle}</span>
      <span className="status-banner__body">{t.connection.lostBody}</span>
      {/* The manual escape hatch stays reachable while the automatic one is
          still trying. The owner's only previous remedy was F5; keeping it one
          click away costs nothing and is honest about the fact that automatic
          recovery is an attempt, not a promise. */}
      <button
        type="button"
        className="status-banner__reload"
        onClick={() => window.location.reload()}
      >
        {t.connection.reload}
      </button>
    </div>
  );
}
