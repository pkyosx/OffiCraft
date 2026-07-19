// api/client.ts — the typed openapi-fetch client every httpApi method rides.
//
// `createClient<paths>` types method + path + params + body against the
// generated OpenAPI schema (generated/schema.ts, the same file the ci.sh
// step-10b drift gate re-generates and diffs). A BE verb/path/query rename now
// turns into a tsc error (step 10a) instead of a runtime 405 — the drift class
// the old hand-typed getJson/sendJson path strings could not catch (the
// global-context PUT/DELETE 405 incident is the precedent).
//
// Cross-cutting auth lives HERE as middleware, once:
//   onRequest  — attach the owner JWT as `Authorization: Bearer` (gated routes
//                are deny-by-default; no token → no header → honest 401, we
//                never fabricate auth).
//   onResponse — 401 → handleUnauthorized() (clear the dead token + signal the
//                auth layer via oc-auth-expired; AuthGate listens and bounces
//                to login), THEN every non-2xx throws an ApiError carrying the
//                server's unified error envelope
//                `{"error":{"code","message"}}` (docs/design/api-error-envelope.md)
//                as structured fields: `.status` (HTTP), `.code`
//                (machine-readable snake_case) and `.serverMessage` (human
//                text). Callers read `.status`/`.code` (SettingsPage's
//                isHttpStatus(e, 409) for deleteRole's 有成員在線上 case) —
//                never parse the message. Error.message keeps the historical
//                `http <status> for <METHOD> <path>` format verbatim (pinned
//                by client.test.ts) so logs and legacy string-matching stay
//                stable. A middleware throw rejects the client call, so a
//                resolved call always carries 2xx data.
//
// NOT routed through this client (permanently hand-written, see http.ts):
// the SSE downlink (EventSource), authedAttachmentUrl (?token= URL rewrite,
// not a fetch) and auth.ts login() (public endpoint + setToken side effect).

import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "./generated/schema";
import { ownerToken, clearToken } from "./auth";
import { ApiError } from "./errors";

// A gated call answered with 401 means the owner token is missing/expired. The
// honest response is NOT a silent empty office: clear the dead token and signal
// the auth layer to bounce the user back to login (AuthGate listens for this).
// The middleware still THROWS afterward so the calling method's promise rejects
// (no fake empty state) — the 401 handling is additive, the throw is kept.
export const AUTH_EXPIRED_EVENT = "oc-auth-expired";

export function handleUnauthorized(): void {
  clearToken();
  try {
    window.dispatchEvent(new Event(AUTH_EXPIRED_EVENT));
  } catch {
    // No DOM (SSR/test) — clearing the token is enough; nothing to notify.
  }
}

// The API is same-origin, but openapi-fetch builds a real `Request` object and
// Node's (undici's) Request constructor rejects relative URLs — vitest/jsdom
// would blow up on `new Request("/api/…")`. Resolving against the page origin
// is what the browser does with a relative URL anyway, so this is
// behavior-identical in production and keeps the tests on the real code path.
const baseUrl =
  typeof window !== "undefined" && window.location?.origin
    ? window.location.origin
    : "http://localhost";

const authMiddleware: Middleware = {
  onRequest({ request }) {
    const t = ownerToken();
    if (t) request.headers.set("Authorization", `Bearer ${t}`);
  },
  async onResponse({ request, response }) {
    if (response.status === 401) handleUnauthorized();
    if (!response.ok) {
      // Read the unified error envelope `{"error":{"code","message"}}` off the
      // body (best-effort: a non-JSON body — proxy error page — yields honest
      // empties, never a second throw).
      let code = "";
      let serverMessage = "";
      try {
        const body: unknown = await response.clone().json();
        const err = (body as { error?: { code?: unknown; message?: unknown } })
          ?.error;
        if (typeof err?.code === "string") code = err.code;
        if (typeof err?.message === "string") serverMessage = err.message;
      } catch {
        // Not JSON / no body — keep the honest empties.
      }
      // Preserve the historical getJson/sendJson error-message format verbatim:
      // `http <status> for <METHOD> </api/path?query>` (path + query only,
      // never the origin). Structured callers read .status/.code instead.
      const url = new URL(request.url);
      throw new ApiError(
        `http ${response.status} for ${request.method} ${url.pathname}${url.search}`,
        response.status,
        code,
        serverMessage
      );
    }
  },
};

export const client = createClient<paths>({
  baseUrl,
  // Defer the global-fetch lookup to CALL time. createClient's default caches
  // `globalThis.fetch` at module-init, which would pin the client to the real
  // network even after a test vi.stubGlobal("fetch", …) — the wrapper keeps
  // stubbing working and is behavior-identical in production.
  fetch: (request) => globalThis.fetch(request),
});
client.use(authMiddleware);
