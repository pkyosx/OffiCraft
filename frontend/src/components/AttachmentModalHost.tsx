// components/AttachmentModalHost.tsx — the studio's answer to a markdown link
// to a stored chat blob: preview it in place. A new tab cannot open that link —
// the serve route wants the token this page holds, and the token must not be
// written into an address.

import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import { AttachmentOpenerContext, type AttachmentOpener } from "../hooks/useAttachmentOpener";
import { attachmentMimeForName } from "../lib/attachmentMime";
import type { AttachmentLink } from "../lib/attachmentLink";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";

export function AttachmentModalHost({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState<(AttachmentLink & { name: string }) | null>(null);
  const value = useMemo<AttachmentOpener>(() => (link) => setOpen(link), []);

  return (
    <AttachmentOpenerContext.Provider value={value}>
      {children}
      {open !== null && (
        <MarkdownPreviewOverlay
          key={open.attachmentId}
          // The link carries no metadata, and no route serves it apart from
          // the bytes: the link text is the only name there is to type it by.
          // A text with no known extension previews as "cannot show", with
          // its download still offered.
          title={open.name}
          url={open.url}
          attachmentId={open.attachmentId}
          mime={attachmentMimeForName(open.name)}
          onClose={() => setOpen(null)}
        />
      )}
    </AttachmentOpenerContext.Provider>
  );
}
