// hooks/useAttachmentOpener.tsx — "preview this stored blob in place", for
// links to one written in markdown. Filled by components/AttachmentModalHost;
// kept apart from it for the same import-cycle reason as useDiffOpener.
//
// Null outside a provider: the link then stays an ordinary anchor.

import { createContext, useContext } from "react";
import type { AttachmentLink } from "../lib/attachmentLink";
import type { DocCommentTarget } from "../lib/docComments";

/** Returns a refresher for that preview's comment target, so the recipient's
 * roster state stays live while it is open; it has no visible effect once that
 * preview is closed or another one is opened. */
export type AttachmentOpener = (
  link: AttachmentLink & { name: string; docComments?: DocCommentTarget },
) => (docComments: DocCommentTarget | undefined) => void;

export const AttachmentOpenerContext = createContext<AttachmentOpener | null>(null);

export function useAttachmentOpener(): AttachmentOpener | null {
  return useContext(AttachmentOpenerContext);
}

/** Where comments on a document opened from a link go: provided by the surface
 * the link is rendered on (a chat room, a reply card, a task card). The nearest
 * provider wins; null, or no provider, opens the document read-only. */
export const AttachmentLinkCommentsContext = createContext<DocCommentTarget | null>(null);

export function useAttachmentLinkComments(): DocCommentTarget | null {
  return useContext(AttachmentLinkCommentsContext);
}
