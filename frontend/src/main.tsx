import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { I18nProvider } from "./i18n";
import { AuthGate } from "./AuthGate";
import { DiffPage } from "./components/DiffPage";
import { diffRouteFromLocation } from "./lib/diffLink";
import "./styles/theme.css";
import "./styles/global.css";

// THE ONE PATH-LEVEL ROUTE (T-59). Everything else the cockpit navigates to
// lives in the URL HASH (lib/hashRoute.ts) — which is exactly why a comparison
// could not: a hash never reaches the server, so it can carry no signature and
// no unauthenticated page could be built on one. `/diff?before=…&after=…` is a
// real path (the SPA shell is already served for it — server/ocserverd/spa.go's
// catch-all takes any extension-free non-/api path), read ONCE here at boot.
// A refresh re-reads it and lands on the same comparison; Back leaves the page,
// as it does for any other address the reader typed or was sent.
//
// 🔴 THE SIGNED FLAVOUR MUST NOT MEET THE AUTH WALL. A ?sig= link is opened by
// someone with no session at all — that is its whole purpose — so it is mounted
// AHEAD of AuthGate, not inside it: the wall would probe /api/auth/status and
// put a login form in front of a page whose credential is in its own url.
// Without a signature the url is the internal flavour and does need a session,
// so it goes through the wall like everything else and the comparison becomes
// the authenticated view instead of the studio.
const diffRoute = diffRouteFromLocation();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <I18nProvider>
      {diffRoute === null ? (
        <AuthGate />
      ) : diffRoute.sig !== undefined ? (
        <DiffPage params={diffRoute} />
      ) : (
        <AuthGate authed={<DiffPage params={diffRoute} />} />
      )}
    </I18nProvider>
  </StrictMode>
);
