import { useEffect, useState } from "react";
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
export function AuthGate() {
  const [wall, setWall] = useState<Wall>(() =>
    USE_MOCK || hasToken() ? "app" : "checking"
  );

  // Real-mode-only: resolve the "checking" wall via the first-run probe.
  useEffect(() => {
    if (wall !== "checking") return;
    let cancelled = false;
    api
      .getAuthStatus()
      .then((passwordSet) => {
        if (!cancelled) setWall(passwordSet ? "login" : "firstrun");
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
  // the wall back to LoginPage so the owner re-logs in — instead of a silently
  // empty office masquerading as a real empty state. Mock mode has no real 401
  // and stays on "app" permanently, so we never touch it (byte-for-byte).
  useEffect(() => {
    if (USE_MOCK) return;
    const onExpired = () => setWall("login");
    window.addEventListener("oc-auth-expired", onExpired);
    return () => window.removeEventListener("oc-auth-expired", onExpired);
  }, []);

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
    return <LoginPage onSuccess={() => setWall("app")} />;
  }

  // ReplyCardsProvider is mounted here — ABOVE the nav badge AND the 等我回覆
  // page — so the badge count and the page list share ONE waiting snapshot and
  // ONE SSE subscription (T-e862 同源化), never two divergent state paths.
  return (
    <ReplyCardsProvider>
      <App
        onLogout={() => {
          clearToken();
          setWall(USE_MOCK ? "app" : "login");
        }}
      />
    </ReplyCardsProvider>
  );
}
