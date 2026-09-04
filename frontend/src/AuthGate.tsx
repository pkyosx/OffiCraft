import { useCallback, useEffect, useState } from "react";
import type { ReactNode } from "react";
import App from "./App";
import { LoginPage } from "./components/LoginPage";
import { FirstRunPage } from "./components/FirstRunPage";
import { USE_MOCK, api } from "./api";
import { hasToken, clearToken } from "./api/auth";
import { ReplyCardsProvider } from "./hooks/useReplyCards";

type Wall = "checking" | "firstrun" | "login" | "app";

/**
 * Real-mode-only auth wall wrapping the app.
 *
 * Mock mode (default): USE_MOCK is true → the wall NEVER renders, the app
 * boots straight into the office exactly as today, and logout keeps it
 * mounted (pref-reset only).
 *
 * Real mode (VITE_USE_MOCK="false"): a token → App. No token → probe the
 * PUBLIC GET /api/auth/status ONCE: password not set yet (a fresh install) →
 * FirstRunPage (claim token + set password, which also logs the session in);
 * password set → LoginPage. HONEST loop — no token is ever fabricated, and an
 * unreachable/failing probe falls back to the login wall (the login itself
 * will surface the real failure).
 */
export function AuthGate({ authed }: { authed?: ReactNode } = {}) {
  const [wall, setWall] = useState<Wall>(() =>
    USE_MOCK || hasToken() ? "app" : "checking"
  );
  // Whether the login wall must also collect a TOTP code. It comes from the
  // SAME probe that decides first-run vs login, so the wall renders the right
  // fields on its first paint — there is no authenticated call available
  // before login that could answer this.
  //
  // 🔴 THEREFORE EVERY ROUTE TO THE LOGIN WALL MUST GO VIA "checking", never
  // straight to "login". This bit is only ever written by the probe below, so a
  // path that jumps to the wall directly carries whatever value the tab last
  // had — and a tab that BEGAN logged in never probed at all, so it carries
  // `false`. That produced a login wall with no code field after logout: the
  // owner typed the right password, got a flat 401, and was told the password
  // was wrong, with no way to enter a code short of a page reload. Logout and
  // the auth-expired handler both go through "checking" for this reason.
  const [mfaRequired, setMfaRequired] = useState(false);

  // Real-mode-only: resolve the "checking" wall via the first-run probe.
  useEffect(() => {
    if (wall !== "checking") return;
    let cancelled = false;
    api
      .getAuthStatus()
      .then((status) => {
        if (cancelled) return;
        setMfaRequired(status.mfaRequired);
        setWall(status.passwordSet ? "login" : "firstrun");
      })
      .catch(() => {
        if (!cancelled) setWall("login");
      });
    return () => {
      cancelled = true;
    };
  }, [wall]);

  // Real-mode-only: a gated call that hit 401 (expired/missing owner token) has
  // already cleared the token and fired "oc-auth-expired" (see api/http.ts). Drop
  // the wall back to the PROBE so the owner re-logs in — instead of a silently
  // empty office masquerading as a real empty state. Mock mode has no real 401
  // and stays on "app" permanently, so we never touch it (byte-for-byte).
  useEffect(() => {
    if (USE_MOCK) return;
    const onExpired = () => setWall("checking");
    window.addEventListener("oc-auth-expired", onExpired);
    return () => window.removeEventListener("oc-auth-expired", onExpired);
  }, []);

  // Re-read the PUBLIC probe and answer whether a code is required now.
  //
  // The wall calls this when a login is refused while it is NOT showing a code
  // field — the exact signature of a wall that is out of date, which happens
  // two ways: the owner armed the factor on another device while this tab sat
  // here, or the first-paint probe failed and left the default `false`. Without
  // it both end in "wrong password, try again" forever, with the real cause
  // invisible and only a page reload as the way out.
  //
  // A failed probe returns the CURRENT value rather than guessing: the login
  // error the caller is already handling stays the honest thing on screen.
  const refreshMfaRequired = useCallback(async (): Promise<boolean> => {
    try {
      const status = await api.getAuthStatus();
      setMfaRequired(status.mfaRequired);
      return status.mfaRequired;
    } catch {
      return mfaRequired;
    }
  }, [mfaRequired]);

  if (wall === "checking") return null; // one probe round-trip, no flash
  if (wall === "firstrun") {
    return (
      <FirstRunPage
        onSuccess={() => setWall("app")}
        onGotoLogin={() => setWall("login")}
      />
    );
  }
  if (wall === "login") {
    return (
      <LoginPage
        onSuccess={() => setWall("app")}
        mfaRequired={mfaRequired}
        refreshMfaRequired={refreshMfaRequired}
      />
    );
  }

  // T-59 — the wall is the same wall; what is BEHIND it is not always the
  // studio. An internal compare url (`/diff?…` with no signature) is a page the
  // owner has to be logged in for, so it comes through here and is rendered
  // INSTEAD of App: same probe, same first-run/login/MFA behaviour, and none of
  // App's session-shaped chrome (nav, unread badges, reply-card polling) around
  // a page whose whole job is one comparison. A SIGNED compare url never
  // reaches this component at all — see main.tsx.
  if (authed !== undefined) return <>{authed}</>;

  // ReplyCardsProvider is mounted here — ABOVE the nav badge AND the 等我回覆
  // page — so the badge count and the page list share ONE waiting snapshot and
  // ONE SSE subscription (T-e862 同源化), never two divergent state paths.
  return (
    <ReplyCardsProvider>
      <App
        onLogout={() => {
          clearToken();
          // "checking", not "login" — the probe has to run so the wall knows
          // whether to render a code field (see the note on mfaRequired). Mock
          // mode has no wall and stays on "app", byte-for-byte as before.
          setWall(USE_MOCK ? "app" : "checking");
        }}
      />
    </ReplyCardsProvider>
  );
}
