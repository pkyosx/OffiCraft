// api/auth.ts — owner-token helpers + the login call.
//
// Single source of truth for the localStorage token key (http.ts imports
// TOKEN_KEY from here). HONEST: login() only ever stores what the server
// mints — a wrong password yields a 401 and we throw, never fabricating a
// token. The /api/login endpoint is PUBLIC, so we send NO Authorization here.

export const TOKEN_KEY = "oc_token";

/** The owner JWT for gated routes — localStorage `oc_token` (Joey's isolated
 * e2e: POST /api/login → store the minted owner token here; the :8770 prod
 * login flow is §3.5 / wake-e2e step3), falling back to the VITE_OC_TOKEN
 * build-time injection. HONEST: "" when neither exists — gated calls then send
 * no Authorization and the server denies (401); we never fabricate auth.
 * Single source of truth for every token reader (client.ts middleware, the SSE
 * downlink and authedAttachmentUrl in http.ts). */
export function ownerToken(): string {
  const envToken =
    typeof import.meta.env.VITE_OC_TOKEN === "string"
      ? import.meta.env.VITE_OC_TOKEN
      : "";
  try {
    return localStorage.getItem(TOKEN_KEY) ?? envToken;
  } catch {
    return envToken;
  }
}

/** True when an owner token exists (localStorage OR the env fallback). */
export function hasToken(): boolean {
  return ownerToken().length > 0;
}

export function setToken(t: string): void {
  try {
    localStorage.setItem(TOKEN_KEY, t);
  } catch {
    // best-effort persistence — ignore
  }
}

export function clearToken(): void {
  try {
    localStorage.removeItem(TOKEN_KEY);
  } catch {
    // best-effort — ignore
  }
}

/**
 * POST /api/login {password} → TokenDTO. On success, persist the minted
 * owner token. On any non-ok response (401 wrong/empty password) THROW so
 * the LoginPage surfaces the failure. NO Authorization header — the endpoint
 * is public.
 *
 * PERMANENTLY HAND-WRITTEN — deliberately NOT routed through the
 * openapi-fetch client (api/client.ts): that client's middleware attaches the
 * owner Bearer token and fires oc-auth-expired on 401, both of which are
 * WRONG here (public endpoint; a wrong password must surface as a login
 * failure, never bounce an auth-expired event). The bare fetch is the honest
 * shape for the one call that mints the token everything else rides on.
 */
export async function login(password: string): Promise<void> {
  const res = await fetch("/api/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
  });
  if (!res.ok) {
    throw new Error(`login failed: http ${res.status}`);
  }
  const data = (await res.json()) as { token: string };
  setToken(data.token);
}
