// api/index.ts — the single swap point for the api client seam.
//
// The UI imports `api` from here and NOWHERE else imports mock/http. Flipping
// USE_MOCK (or wiring it to an env flag) swaps the whole backend with zero UI
// changes — that is the entire point of the seam.

import type { Api } from "./adapter";
import { hasToken, ownerToken } from "./auth";
import type { Member } from "../types";
import { mockApi } from "./mock";
import { httpApi } from "./http";

// M1 defaults to the mock adapter. Set VITE_USE_MOCK="false" at build time to
// swap the WHOLE backend to httpApi (real server) with zero UI changes — that is
// the point of the seam. Default (unset / any non-"false" value) stays mock, so
// the running :8770 deploy is unaffected until a build explicitly opts in.
export const USE_MOCK = import.meta.env.VITE_USE_MOCK !== "false";

export const api: Api = USE_MOCK ? mockApi : httpApi;

/**
 * May the principal this SPA is authenticated as force a task closed
 * (`POST /api/tasks/{id}/force-done`)? T-192.
 *
 * 🔴 THIS DEFENDS NOTHING, and reading it as a permission check is the one way
 * to get it wrong. The REAL gate is the server's route floor —
 * `Gated(principalAdminAgent, …)` in `server/ocserverd/routes.go` — which admits
 * the owner and the admin assistant and 403s every other principal, the task's
 * OWN executor included. A caller who forces a `true` out of this function still
 * gets that 403. What this decides is only whether the cockpit OFFERS the
 * control, under one rule: never render a button that could only ever fail.
 *
 * The body is `USE_MOCK || hasToken()` — the SAME predicate `AuthGate` already
 * uses to decide that a session is the owner's — and its honesty rests on a fact
 * about this SPA rather than a guess about roles: THE COCKPIT HAS EXACTLY ONE
 * PRINCIPAL. `/api/login` mints an OWNER token (`TokenDTO.owner_id`) and every
 * gated call rides it; there is no member login, no impersonation, and
 * `/api/auth/status` discloses nothing about who is asking (`password_set` /
 * `mfa_required`, nothing else). So "this session holds an owner token" IS "this
 * session is inside the set the route floor admits". The mock half is not a
 * loophole: mock mode never renders the wall at all (`AuthGate`), so a token
 * test there would say "not the owner" about the only principal that exists.
 *
 * The negative arm is therefore real, not decorative: a real-mode page with no
 * owner token — a cleared or expired session, the unauthenticated share-link
 * surfaces — is a caller the server refuses, and it is not shown the control.
 *
 * ⚠️ IF A MEMBER-SCOPED COCKPIT EVER EXISTS, THIS IS WRONG RATHER THAN MERELY
 * INCOMPLETE: a plain member's token would make `hasToken()` true while the
 * route floor still refuses them. Whoever adds that login must replace this body
 * with a real principal read. Nothing in the type system would notice, which is
 * why the warning is here and not in a ticket.
 */
export function viewerMayForceTaskDone(): boolean {
  return USE_MOCK || hasToken();
}

function tokenClaims(): Record<string, unknown> | null {
  const payload = ownerToken().split(".")[1];
  if (!payload) return null;
  try {
    const b64 = payload.replace(/-/g, "+").replace(/_/g, "/");
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const claims: unknown = JSON.parse(new TextDecoder().decode(bytes));
    return claims && typeof claims === "object"
      ? (claims as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

/** May this viewer switch a 傳承 entry's scope (`POST /api/lore/{id}/scope`)?
 * Mirrors the server's admin floor (`resolvePrincipal` in
 * server/ocserverd/authz.go): an owner token, or a member token whose roster
 * row holds the assistant role. Unlike `viewerMayForceTaskDone` it reads who
 * the token belongs to, so a plain member's token is not offered the menu.
 * Display only — the server still refuses everyone else. */
export function viewerMaySetLoreScope(members: readonly Member[]): boolean {
  if (USE_MOCK) return true;
  const claims = tokenClaims();
  if (!claims) return false;
  if (claims.scope === "owner") return true;
  const sub = typeof claims.sub === "string" ? claims.sub : "";
  if (!sub) return false;
  const me = members.find((m) => m.id === sub);
  return !!me && me.kind !== "warden" && me.role === "assistant";
}

export type {
  Api,
  SseConnectionState,
  ChatMessage,
  MemberPatch,
  RolePatch,
  AliasPatch,
  OnboardOptions,
  ServerSettingsView,
  ServerSettingsPatch,
  OnboardingReportView,
  OnboardingStepView,
} from "./adapter";
