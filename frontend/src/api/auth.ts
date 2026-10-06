// api/auth.ts — owner-token helpers + the login call.
//
// Single source of truth for the localStorage token key (http.ts imports
// TOKEN_KEY from here). HONEST: login() only ever stores what the server
// mints — a wrong password yields a 401 and we throw, never fabricating a
// token. The /api/login endpoint is PUBLIC, so we send NO Authorization here.

import { ApiError, parseRetryAfter } from "./errors";

export const TOKEN_KEY = "oc_token";

/** Fired on the window the instant an owner token is minted (login /
 * login link / set-password / change-password — every `setToken` call). Symmetric with
 * `oc-auth-expired` (api/client.ts): that signals a session dying, this signals
 * one being born. The dual-layer display prefs (T-0b41-p2) listen for it to
 * reconcile the server's theme/language in after login, since /api/settings is
 * owner-gated and unreachable pre-auth. Lives here — the single mint chokepoint
 * — rather than in each caller, and here rather than in client.ts to avoid an
 * import cycle (client.ts already imports this module). */
export const AUTH_LOGIN_EVENT = "oc-auth-login";

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
  try {
    window.dispatchEvent(new Event(AUTH_LOGIN_EVENT));
  } catch {
    // No DOM (SSR/test) — persisting the token is enough; nothing to notify.
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
export async function login(password: string, code?: string): Promise<void> {
  const res = await fetch("/api/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    // `code` is OMITTED entirely when absent rather than sent as null/"" —
    // LoginDTO is additionalProperties:false and the field is optional, so the
    // absent form is the one an install without MFA has always spoken.
    body: JSON.stringify(code ? { password, code } : { password }),
  });
  if (!res.ok) {
    // Throws ApiError, not a bare Error: the wall has to tell a WRONG
    // CREDENTIAL (401) apart from the attempt brake (429 + Retry-After), and
    // telling an owner "wrong password" while they are merely rate-limited
    // sends them hunting a typo that does not exist.
    throw await credentialError(res, "login");
  }
  const data = (await res.json()) as { token: string };
  setToken(data.token);
}

/**
 * POST /api/auth/login-link {code} → TokenDTO: the one-time owner login link
 * minted on the station host (`ocserverd login-link`). Hand-written and bare
 * for the same reasons as login(): public endpoint, and a refused link must
 * surface on the wall rather than fire oc-auth-expired.
 */
export async function redeemLoginLink(code: string): Promise<void> {
  const res = await fetch("/api/auth/login-link", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ code }),
  });
  if (!res.ok) throw await credentialError(res, "login link");
  const data = (await res.json()) as { token: string };
  setToken(data.token);
}

async function credentialError(res: Response, what: string): Promise<ApiError> {
  let errCode = "";
  let serverMessage = "";
  try {
    const parsed: unknown = await res.json();
    const err = (parsed as { error?: { code?: unknown; message?: unknown } })
      ?.error;
    if (typeof err?.code === "string") errCode = err.code;
    if (typeof err?.message === "string") serverMessage = err.message;
  } catch {
    // Not JSON — keep the honest empties.
  }
  return new ApiError(
    `${what} failed: http ${res.status}`,
    res.status,
    errCode,
    serverMessage,
    parseRetryAfter(res.headers.get("Retry-After")),
  );
}
