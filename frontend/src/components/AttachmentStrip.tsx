// AttachmentStrip — the ONE renderer for STORED attachments (served refs with
// a download url), extracted from ChatArea.renderAttachment /
// ReplyCardBody.ReplyAnswerAttachments (T-5e8a) so the reply card's new
// question-side attachments never become a third copy-paste. Image → inline
// thumbnail (clickable when the caller wires `onOpenImage` — the caller owns
// the Lightbox state); non-image → a download chip under the blob's stored
// filename. Class knobs keep each call site's EXISTING markup/classes
// byte-identical — this is a move, not a redesign.
//
// Lightbox — the full-size image overlay (click backdrop / × / Esc closes),
// extracted from ChatArea so every card face (等我回覆頁 / 聊天室內卡 /
// 任務卡上的卡) gets the same 點開預覽 the chat thread has.

import { useEffect } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import type { ChatAttachmentView } from "../api/adapter";
import { authedAttachmentUrl } from "../api/http";
import { isMarkdownAttachment } from "./MarkdownPreviewOverlay";
import { PaperclipIcon } from "./icons";

export function AttachmentStrip({
  attachments,
  className,
  itemClassName,
  imageClassName,
  fileChipClassName = "chat__msg-file",
  fileNameClassName = "chat__msg-file-name",
  fileNameColClassName,
  onOpenImage,
  onPreviewMarkdown,
  renderExtra,
  renderMeta,
}: {
  attachments: ChatAttachmentView[];
  /** The container element's class (e.g. `chat__msg-attachments`,
   * `reply-card__answer-atts`) — call-site markup conservation. */
  className: string;
  /** When set, each item wraps in a div of this class (ChatArea's
   * `chat__msg-attachment` per-item wrapper); absent ⇒ items render bare
   * (the reply-card strips' existing shape). */
  itemClassName?: string;
  /** The `<img>` class for image items (call-site conservation). */
  imageClassName: string;
  /** The non-image download chip's class. Defaults to the chat bubble's
   * `chat__msg-file` so every existing call site stays byte-identical; the
   * artifacts popover (T-90df) overrides it because the chat chip is sized
   * for a bubble (`max-width:300px`, non-flexible) and cannot align inside a
   * 340px panel. */
  fileChipClassName?: string;
  /** The chip's filename `<span>` class — same defaulting contract. */
  fileNameClassName?: string;
  /** Wrapper class stacking the filename above `renderMeta`'s output
   * (T-6338 — the artifacts popover's per-row upload time/ref). Only used
   * (and only rendered at all) when `renderMeta` is supplied — absent ⇒ the
   * filename `<span>` renders bare exactly as before, so every OTHER caller
   * (chat bubble, reply-card strip) stays byte-identical. */
  fileNameColClassName?: string;
  /** Makes image thumbnails clickable (role=button + keyboard): the caller
   * receives the token-authed src and opens its own Lightbox. Absent ⇒ a
   * static thumbnail (the answered-card strip's existing behaviour). */
  onOpenImage?: (src: string) => void;
  /** T-7bc2: makes a `.md` attachment's chip itself the preview trigger — a
   * `<button>` instead of the download `<a>` (mirrors `onOpenImage`'s
   * image-thumbnail contract: the caller owns the overlay's open state).
   * Absent ⇒ every non-image chip (markdown included) stays a plain download
   * link, byte-identical to before this knob existed. */
  onPreviewMarkdown?: (att: ChatAttachmentView) => void;
  /** Per-item extra node rendered after the image/chip (ChatArea's hover
   * 複製分享連結 button). */
  renderExtra?: (att: ChatAttachmentView) => ReactNode;
  /** Per-item node rendered BELOW the filename, inside the chip (T-6338 —
   * lets a non-image download chip carry a second line without every other
   * call site knowing about it). Undefined ⇒ no second line, no wrapper. */
  renderMeta?: (att: ChatAttachmentView) => ReactNode;
}) {
  const { t } = useI18n();
  if (attachments.length === 0) return null;

  function renderOne(att: ChatAttachmentView) {
    if (att.isImage) {
      const src = authedAttachmentUrl(att.url);
      const clickable = onOpenImage
        ? {
            role: "button",
            tabIndex: 0,
            "aria-label": t.chat.viewImageLabel,
            onClick: () => onOpenImage(src ?? ""),
            onKeyDown: (e: React.KeyboardEvent) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onOpenImage(src ?? "");
              }
            },
          }
        : {};
      return (
        <img
          key={itemClassName ? undefined : att.id}
          className={imageClassName}
          src={src}
          alt={t.chat.imageAlt}
          {...clickable}
        />
      );
    }
    // Non-image → a download chip/link (Content-Disposition: attachment on
    // the serve side downloads it under its stored filename). Same gated
    // blob → same ?token= auth as the image.
    // `title` = the full filename: the chip name truncates with an ellipsis
    // when it outgrows its row, so hovering must still yield the whole name
    // (T-90df). Presentation only — href/download are untouched.
    const fullName = att.filename || t.chat.downloadAttachment;
    const meta = renderMeta?.(att);
    const content = (
      <>
        <PaperclipIcon size={15} />
        {meta ? (
          <span className={fileNameColClassName}>
            <span className={fileNameClassName}>{fullName}</span>
            {meta}
          </span>
        ) : (
          <span className={fileNameClassName}>{fullName}</span>
        )}
      </>
    );
    // A markdown file with `onPreviewMarkdown` wired ⇒ the chip itself opens
    // the in-cockpit preview (same click-target contract as the image
    // thumbnail's `onOpenImage`) instead of downloading. Accessible name
    // stays the VISIBLE filename text — no `aria-label` override (T-a706 /
    // T-5e8a lesson: `aria-label` replaces, not appends, so overriding it
    // here would just re-say the filename and add nothing).
    if (onPreviewMarkdown && isMarkdownAttachment(att.mime, att.filename)) {
      return (
        <button
          type="button"
          key={itemClassName ? undefined : att.id}
          className={fileChipClassName}
          title={fullName}
          onClick={() => onPreviewMarkdown(att)}
        >
          {content}
        </button>
      );
    }
    // Non-image → a download chip/link (Content-Disposition: attachment on
    // the serve side downloads it under its stored filename). Same gated
    // blob → same ?token= auth as the image.
    // `title` = the full filename: the chip name truncates with an ellipsis
    // when it outgrows its row, so hovering must still yield the whole name
    // (T-90df). Presentation only — href/download are untouched.
    return (
      <a
        key={itemClassName ? undefined : att.id}
        className={fileChipClassName}
        href={authedAttachmentUrl(att.url)}
        download={att.filename || undefined}
        title={fullName}
      >
        {content}
      </a>
    );
  }

  return (
    <div className={className}>
      {attachments.map((att) =>
        itemClassName ? (
          <div key={att.id} className={itemClassName}>
            {renderOne(att)}
            {renderExtra?.(att)}
          </div>
        ) : (
          renderOne(att)
        )
      )}
    </div>
  );
}

/** The full-size image overlay: click the backdrop, the × button or Esc to
 * close; a click ON the image does not dismiss. The caller holds the open
 * state (`src` null ⇒ render nothing). */
export function Lightbox({
  src,
  onClose,
}: {
  src: string | null;
  onClose: () => void;
}) {
  const { t } = useI18n();
  // Esc closes — bound only while open.
  useEffect(() => {
    if (!src) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [src, onClose]);
  if (!src) return null;
  return (
    <div
      className="chat__lightbox"
      role="dialog"
      aria-modal="true"
      aria-label={t.chat.imageAlt}
      onClick={onClose}
    >
      <button
        type="button"
        className="chat__lightbox-close"
        aria-label={t.chat.closeImageLabel}
        onClick={onClose}
      >
        ×
      </button>
      <img
        className="chat__lightbox-image"
        src={src}
        alt={t.chat.imageAlt}
        onClick={(e) => e.stopPropagation()}
      />
    </div>
  );
}
