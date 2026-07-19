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
import { PaperclipIcon } from "./icons";

export function AttachmentStrip({
  attachments,
  className,
  itemClassName,
  imageClassName,
  onOpenImage,
  renderExtra,
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
  /** Makes image thumbnails clickable (role=button + keyboard): the caller
   * receives the token-authed src and opens its own Lightbox. Absent ⇒ a
   * static thumbnail (the answered-card strip's existing behaviour). */
  onOpenImage?: (src: string) => void;
  /** Per-item extra node rendered after the image/chip (ChatArea's hover
   * 複製分享連結 button). */
  renderExtra?: (att: ChatAttachmentView) => ReactNode;
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
    return (
      <a
        key={itemClassName ? undefined : att.id}
        className="chat__msg-file"
        href={authedAttachmentUrl(att.url)}
        download={att.filename || undefined}
      >
        <PaperclipIcon size={15} />
        <span className="chat__msg-file-name">
          {att.filename || t.chat.downloadAttachment}
        </span>
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
