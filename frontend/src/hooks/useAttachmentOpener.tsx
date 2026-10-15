// hooks/useAttachmentOpener.tsx — "preview this stored blob in place", for
// links to one written in markdown. Filled by components/AttachmentModalHost;
// kept apart from it for the same import-cycle reason as useDiffOpener.
//
// Null outside a provider: the link then stays an ordinary anchor.

import { createContext, useContext } from "react";
import type { AttachmentLink } from "../lib/attachmentLink";

export type AttachmentOpener = (link: AttachmentLink & { name: string }) => void;

export const AttachmentOpenerContext = createContext<AttachmentOpener | null>(null);

export function useAttachmentOpener(): AttachmentOpener | null {
  return useContext(AttachmentOpenerContext);
}
