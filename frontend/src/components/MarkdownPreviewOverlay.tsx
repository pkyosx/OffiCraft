// components/MarkdownPreviewOverlay.tsx — the in-cockpit .md preview overlay
// (T-a1c4). A markdown attachment is PREVIEWED here — fetched as text and
// rendered through the shared Markdown.tsx (React elements, XSS-safe) — instead
// of the browser's raw-source new tab. Preview and download are TWO separate
// actions: the header keeps a 下載 button (the same authed blob URL with a
// download attribute) alongside the render.
//
// Self-contained like Lightbox (click backdrop / × / Esc closes; a click on the
// panel does not dismiss): the caller holds the open state and passes the blob's
// serve url + display title. Shared by the chat attachment strip AND the task
// artifact popover — one preview surface, not two.

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { authedAttachmentUrl } from "../api/http";
import { Markdown } from "./Markdown";
import { CloseIcon, DownloadIcon, FileTextIcon } from "./icons";

export function MarkdownPreviewOverlay({
  title,
  url,
  onClose,
}: {
  /** Display name shown in the header (the blob's filename). */
  title: string;
  /** The blob's serve path (`/api/chat/attachment/<id>`); fetched as text. */
  url: string;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [source, setSource] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  // Fetch the markdown text once (the authed blob URL — same ?token= gate the
  // download/thumbnail paths use). A non-ok response / network error surfaces
  // the honest error state, never a blank render.
  useEffect(() => {
    let alive = true;
    setSource(null);
    setFailed(false);
    fetch(authedAttachmentUrl(url))
      .then((r) => {
        if (!r.ok) throw new Error(`http ${r.status}`);
        return r.text();
      })
      .then((text) => {
        if (alive) setSource(text);
      })
      .catch((e) => {
        if (alive) setFailed(true);
        console.warn("MarkdownPreviewOverlay: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, [url]);

  // Esc closes — bound only while mounted (the overlay only mounts open).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="md-preview"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={onClose}
    >
      <div className="md-preview__panel" onClick={(e) => e.stopPropagation()}>
        <div className="md-preview__header">
          <span className="md-preview__title">
            <FileTextIcon size={16} />
            {title}
          </span>
          <div className="md-preview__actions">
            {/* Download — the SECOND action, distinct from preview: the authed
             * blob URL with a download attribute (server forces the bytes). */}
            <a
              className="md-preview__download"
              href={authedAttachmentUrl(url)}
              download={title || undefined}
            >
              <DownloadIcon size={14} />
              {t.chat.mdPreview.download}
            </a>
            <button
              type="button"
              className="md-preview__close"
              aria-label={t.chat.mdPreview.close}
              onClick={onClose}
            >
              <CloseIcon size={16} />
            </button>
          </div>
        </div>
        <div className="md-preview__body">
          {failed ? (
            <div className="md-preview__status">{t.chat.mdPreview.error}</div>
          ) : source === null ? (
            <div className="md-preview__status">{t.chat.mdPreview.loading}</div>
          ) : (
            <Markdown source={source} className="md-preview__md" />
          )}
        </div>
      </div>
    </div>
  );
}

/** Whether an attachment (by mime / filename) is a markdown doc the preview
 * overlay can render. Mirrors the server's text/markdown handling; also accepts
 * a `.md`/`.markdown` filename when the mime is a generic text/plain. */
export function isMarkdownAttachment(mime: string, filename: string): boolean {
  if (mime === "text/markdown" || mime === "text/x-markdown") return true;
  const name = filename.toLowerCase();
  return name.endsWith(".md") || name.endsWith(".markdown");
}
