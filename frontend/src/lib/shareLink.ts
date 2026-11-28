// lib/shareLink.ts — share links to one attachment or one comparison, and the
// cockpit's own one-hour preview URL for an attachment.
//
// The server answers every URL SERVER-RELATIVE; only the browser knows the
// origin the reader reached this station on, so it is absolutized here.

import { api } from "../api";
import type { ShareLinkCreated, ShareLinkFilter, ShareLinkView } from "../types";
import type { DiffParams } from "./diffLink";

export type ShareTarget =
  | { kind: "attachment"; attachmentId: string }
  | { kind: "diff"; params: DiffParams };

function absolute(path: string): string {
  return new URL(path, window.location.origin).toString();
}

/** Mint a NEW link to `target`. `expiresTs` null = never expires. The URL in
 * the answer is the only time it is ever shown. */
export async function mintShareLink(
  target: ShareTarget,
  expiresTs: number | null,
): Promise<ShareLinkCreated> {
  const created =
    target.kind === "attachment"
      ? await api.createAttachmentShareLink(target.attachmentId, expiresTs)
      : await api.createDiffShareLink(target.params, expiresTs);
  return { url: absolute(created.url), link: created.link };
}

export function shareLinkFilterOf(target: ShareTarget): ShareLinkFilter {
  return target.kind === "attachment"
    ? { attachmentId: target.attachmentId }
    : { before: target.params.before, after: target.params.after };
}

export function listShareLinksFor(target: ShareTarget): Promise<ShareLinkView[]> {
  return api.listShareLinks(shareLinkFilterOf(target));
}

/** The attachment's one-hour preview URL, absolutized — what a preview frame
 * loads and a 「在新頁面顯示」 anchor opens. Never a ?token= URL: a whole owner
 * session has no business in another tab's address bar or in a frame running
 * agent-written script. Throws on API failure. */
export async function attachmentPreviewUrl(attachmentId: string): Promise<string> {
  return absolute(await api.getChatAttachmentPreviewLink(attachmentId));
}

/** How a comparison is named in lists and confirmations: its column headings
 * where it has them, else its addresses. */
export function diffTargetName(
  before: string,
  after: string,
  labelBefore?: string | null,
  labelAfter?: string | null,
): string {
  return `${labelBefore || before} → ${labelAfter || after}`;
}

export function shareLinkTargetName(link: ShareLinkView): string {
  return link.kind === "attachment"
    ? link.attachmentFilename || link.attachmentId || ""
    : diffTargetName(link.before ?? "", link.after ?? "", link.labelBefore, link.labelAfter);
}
