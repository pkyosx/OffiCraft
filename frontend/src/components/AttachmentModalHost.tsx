// components/AttachmentModalHost.tsx — the studio's answer to a markdown link
// to a stored chat blob: preview it in place. A new tab cannot open that link —
// the serve route wants the token this page holds, and the token must not be
// written into an address.

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { authedAttachmentUrl } from "../api/http";
import { AttachmentOpenerContext, type AttachmentOpener } from "../hooks/useAttachmentOpener";
import { attachmentMimeForName } from "../lib/attachmentMime";
import type { AttachmentLink } from "../lib/attachmentLink";
import type { DocCommentTarget } from "../lib/docComments";
import { MarkdownPreviewOverlay, isPreviewableTextAttachment } from "./MarkdownPreviewOverlay";

const OCTET_STREAM = "application/octet-stream";

type Opened = AttachmentLink & { name: string };

function sameTarget(a: DocCommentTarget | undefined, b: DocCommentTarget | undefined): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

function previewable(mime: string, name: string): boolean {
  return mime.startsWith("image/") || isPreviewableTextAttachment(mime, name);
}

/** The serve route's Content-Type for the blob, without its bytes. */
async function servedMime(url: string): Promise<string> {
  const res = await fetch(authedAttachmentUrl(url), { method: "HEAD" });
  if (!res.ok) throw new Error(`http ${res.status}`);
  return (res.headers.get("content-type") ?? OCTET_STREAM).split(";")[0]!.trim().toLowerCase();
}

export function AttachmentModalHost({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState<Opened | null>(null);
  // The link text is a name only when it ends in a known extension; an image
  // comment's link text is the image's alt, so the server is asked instead.
  const [probed, setProbed] = useState<{ attachmentId: string; mime: string } | null>(null);
  const [comments, setComments] = useState<{ for: Opened; target?: DocCommentTarget } | null>(null);
  const value = useMemo<AttachmentOpener>(
    () =>
      ({ docComments, ...link }) => {
        setOpen(link);
        setComments({ for: link, target: docComments });
        return (target) =>
          setComments((prev) =>
            prev === null || prev.for !== link || sameTarget(prev.target, target)
              ? prev
              : { for: link, target },
          );
      },
    [],
  );

  const nameMime = open === null ? OCTET_STREAM : attachmentMimeForName(open.name);
  const needsProbe = open !== null && !previewable(nameMime, open.name);

  useEffect(() => {
    if (open === null || !needsProbe) return;
    let alive = true;
    const { attachmentId } = open;
    servedMime(open.url)
      .catch((e) => {
        console.warn("AttachmentModalHost: type probe failed", e);
        return OCTET_STREAM;
      })
      .then((mime) => {
        if (alive) setProbed({ attachmentId, mime });
      });
    return () => {
      alive = false;
    };
  }, [open, needsProbe]);

  const probedMime = probed !== null && probed.attachmentId === open?.attachmentId ? probed.mime : null;
  const mime = needsProbe ? probedMime ?? undefined : nameMime;

  return (
    <AttachmentOpenerContext.Provider value={value}>
      {children}
      {open !== null && (
        <MarkdownPreviewOverlay
          key={open.attachmentId}
          title={open.name}
          url={open.url}
          attachmentId={open.attachmentId}
          mime={mime}
          typePending={needsProbe && probedMime === null}
          docComments={comments?.for === open ? comments.target : undefined}
          onClose={() => setOpen(null)}
        />
      )}
    </AttachmentOpenerContext.Provider>
  );
}
