// lib/attachmentLink.ts — a link to one stored chat blob, as messages spell it
// (`/api/chat/attachment/att-<12 hex>`, e.g. the head of a 文件註解 message).

const ATTACHMENT_PATH_RE = /^\/api\/chat\/attachment\/(att-[0-9a-f]{12})$/;

export interface AttachmentLink {
  attachmentId: string;
  /** The serve path alone: whatever query the link carried is dropped. */
  url: string;
}

/** The blob a same-origin link points at, or null for any other link. */
export function attachmentLinkFromHref(
  href: string,
  base: string = typeof window !== "undefined" ? window.location.href : "http://localhost/",
): AttachmentLink | null {
  let url: URL;
  try {
    url = new URL(href, base);
  } catch {
    return null;
  }
  if (url.origin !== new URL(base).origin) return null;
  const m = ATTACHMENT_PATH_RE.exec(url.pathname);
  return m ? { attachmentId: m[1], url: url.pathname } : null;
}
