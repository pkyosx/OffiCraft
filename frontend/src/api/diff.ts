// api/diff.ts — the ONE place that speaks GET /api/diff.
//
// T-59: the compare screen asks for both sides in a SINGLE request, and the
// server answers each side's resolved text, the heading to put over its column,
// and an honest "this side is gone" marker when the address resolves to nothing
// (a pruned revision, a reclaimed blob, a field the document no longer carries).
// The reader resolves nothing itself — one round trip, one authority.
//
// One route for the PAIR is also the security shape, not a convenience: the
// ?sig= credential signs exactly what one request returns, so a holder of an
// external link cannot swap an address or relabel a column and still present a
// server-minted signature.
//
// PERMANENTLY HAND-WRITTEN, joining auth.ts's login() on http.ts's list of calls
// that deliberately skip the typed openapi-fetch client. The reason is the
// client's auth middleware, which is exactly wrong for THIS route: it turns any
// 401 into "the session died" (clear the token, fire oc-auth-expired). The
// signed flavour of this route is answered WITHOUT a session, so a bad ?sig=
// would log the owner out of the studio over someone else's stale link, and log
// out a tab that never had a session at all. A 401 is bounced to the auth layer
// ONLY when the call was made as the session (no ?sig=), which is when it means
// what it says.
//
// Skipping the typed CLIENT is not skipping the typed CONTRACT: the wire types
// below are the GENERATED ones, so a DTO renamed in spec/openapi.json is a tsc
// error here exactly as it is everywhere else.

import type { DiffPairView, DiffSideView } from "../types";
import type { DiffParams } from "../lib/diffLink";
import { diffSearchParams } from "../lib/diffLink";
import type { components } from "./generated/schema";
import { ownerToken } from "./auth";
import { handleUnauthorized } from "./client";
import { ApiError } from "./errors";

type WireDiffSide = components["schemas"]["DiffSideDTO"];
type WireDiffPair = components["schemas"]["DiffPairDTO"];

function toDiffSide(w: WireDiffSide): DiffSideView {
  return {
    address: w.address,
    // A GONE side carries no text, and "" is not a text either: drawing "" against
    // the other side would mark every one of its lines as added — a confident
    // wrong answer to "what changed". The screen branches on `gone` before it
    // draws anything, so this default is never what gets compared.
    text: w.text ?? "",
    // The server sends "" for a side the url gave no heading — deliberately, so
    // that the READER names a document side in its own language rather than
    // inheriting one language's label from mint time. Empty and absent are
    // therefore the same fact here, and both mean "use your own words".
    label: w.label ? w.label : undefined,
    gone: w.gone,
    goneReason: w.gone_reason ? w.gone_reason : undefined,
  };
}

export function toDiffPair(w: WireDiffPair): DiffPairView {
  return { before: toDiffSide(w.before), after: toDiffSide(w.after) };
}

/** The query this call puts on the wire — the SAME five parameter names the
 * page url spells, taken from lib/diffLink.ts rather than typed again here.
 * The data route and the page route are one grammar; spelling it twice is what
 * would let a rename redden one reader and silently pass the other. */
export function diffQuery(params: DiffParams): string {
  return diffSearchParams(params).toString();
}

export async function fetchDiffPair(params: DiffParams): Promise<DiffPairView> {
  const headers: Record<string, string> = { Accept: "application/json" };
  // ONE CREDENTIAL PER CALL, and the URL picks it. The signature is the
  // external flavour's credential; the session token is the internal one's.
  // They must not ride together: the server judges a bearer that is PRESENT
  // and never falls through to the sig (its rule, and the right one), so a
  // token that has since expired would 401 a link whose own credential is
  // perfectly good — and the reader could never get past it, because the
  // signed page is mounted AHEAD of the auth wall (main.tsx) and so never
  // probes the session that would have cleared the dead token.
  //
  // A signed link opened by a signed-in member still works: the sig alone is
  // what the server was going to check anyway, and withholding the member's
  // own bearer from someone else's link is the least authority besides.
  //
  // The other candidate — send it, then retry without it on a 401 — was
  // rejected: it spends a second round trip on every genuinely bad signature,
  // and it makes "which credential was refused" unanswerable, which is exactly
  // the question the handleUnauthorized() call below has to get right.
  if (params.sig === undefined) {
    const token = ownerToken();
    if (token) headers.Authorization = `Bearer ${token}`;
  }

  const res = await fetch(`/api/diff?${diffQuery(params)}`, { headers });
  if (!res.ok) {
    let code = "";
    let serverMessage = "";
    try {
      const body: unknown = await res.json();
      const err = (body as { error?: { code?: unknown; message?: unknown } })?.error;
      if (typeof err?.code === "string") code = err.code;
      if (typeof err?.message === "string") serverMessage = err.message;
    } catch {
      // Not JSON (a proxy error page) — keep the honest empties.
    }
    // See the header: only a SESSION call's 401 is a dead session.
    if (res.status === 401 && params.sig === undefined) handleUnauthorized();
    throw new ApiError(
      `http ${res.status} for GET /api/diff`,
      res.status,
      code,
      serverMessage,
    );
  }
  return toDiffPair((await res.json()) as WireDiffPair);
}
