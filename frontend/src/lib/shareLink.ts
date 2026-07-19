// lib/shareLink.ts — copy ONE chat attachment's permanent share link.
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
  const path = await api.getChatAttachmentShareLink(attachmentId);
  const url = new URL(path, window.location.origin).toString();
  await navigator.clipboard.writeText(url);
}
