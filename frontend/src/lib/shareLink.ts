// lib/shareLink.ts — copy ONE chat attachment's share link.
//
// "permanent" used to end that sentence and no longer can: since T-62 the sig
// is derived from a key in the signing-key ring, so it lasts exactly as long as
// that key stays in the ring and dies with it.
//
// The server mints the link (`GET /api/chat/attachments/{id}/share-link`) as a
// SERVER-RELATIVE path carrying the ?sig= file-level HMAC credential; only the
// browser knows the public origin, so absolutization happens here. The sig
// grants reading exactly that one blob — sendable to anyone, no login, no
// expiry (owner-accepted beta trade-off).

import { api } from "../api";

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
