// lib/shareLink.ts — copy an EXTERNAL, server-signed share link.
//
// Two subjects, one idiom: ONE chat attachment's blob, and ONE comparison
// (T-59). Both are minted by the server as a SERVER-RELATIVE path carrying a
// ?sig= credential, both are absolutized here against the page origin, and both
// throw rather than resolve on failure — the caller must never fake a
// 「已複製」 for a link it does not hold.
//
// NEITHER SIG IS "permanent", though both used to be described that way: since
// T-62 each is derived from a key in the signing-key ring, so it lasts exactly
// as long as that key stays in the ring and dies with it. Removing a key voids
// every link it signed at once; no single link can be withdrawn.
//
// The attachment link (`GET /api/chat/attachments/{id}/share-link`) grants
// reading exactly that one blob. The comparison link
// (`GET /api/diff/share-link`) opens exactly that one `/diff` page — the two
// signatures are made under keys derived apart, so neither can be replayed as
// the other. Both are sendable to anyone, with no login.

import { api } from "../api";
import type { DiffParams } from "./diffLink";

/** Fetch the attachment's share link, absolutize it against the page origin,
 * and place it on the clipboard. Throws on API/clipboard failure — callers
 * surface feedback only on success (never fake a 「已複製」). */
export async function copyAttachmentShareLink(
  attachmentId: string,
): Promise<void> {
  await navigator.clipboard.writeText(await attachmentShareLinkUrl(attachmentId));
}

/** The attachment's share link, absolutized against the page origin — the
 * SAME value `copyAttachmentShareLink` puts on the clipboard, handed back
 * instead of copied. Callers that need it as an `href` (the preview overlay's
 * 「在新頁面顯示」 anchor) use this: the ?sig= credential is minted by the
 * server, so there is no way to build the URL synchronously and a real
 * `<a target="_blank" rel="noopener">` needs it before the click.
 * Throws on API failure — a caller must render nothing rather than a link that
 * would 404. */
export async function attachmentShareLinkUrl(
  attachmentId: string,
): Promise<string> {
  const path = await api.getChatAttachmentShareLink(attachmentId);
  return new URL(path, window.location.origin).toString();
}

/** Fetch the EXTERNAL link to this comparison, absolutize it against the page
 * origin, and place it on the clipboard. `params.sig` is ignored — the server
 * mints the signature, so a link already carrying one is re-minted rather than
 * echoed. Throws on API/clipboard failure, on the same terms as the attachment
 * pair above: feedback only on a real success. */
export async function copyDiffShareLink(params: DiffParams): Promise<void> {
  await navigator.clipboard.writeText(await diffShareLinkUrl(params));
}

/** The comparison's external link, absolutized against the page origin — the
 * SAME value `copyDiffShareLink` puts on the clipboard, handed back instead of
 * copied. Throws on API failure. */
export async function diffShareLinkUrl(params: DiffParams): Promise<string> {
  const path = await api.getDiffShareLink(params);
  return new URL(path, window.location.origin).toString();
}
