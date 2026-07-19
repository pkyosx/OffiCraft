// ComposerAttachmentPreview — the staged-attachment preview strip of a
// composer (thumbnails + filename chips, each with a × remove control, plus
// the shared error line), extracted from the three copy-pastes in ChatArea /
// ReplyComposer / TaskCard (T-5e8a — one visual language for "composing a
// message", one markup to maintain). Pure move: same `chat__composer-preview`
// / `chat__preview-*` classes, same behaviour — the chat composer's clickable
// thumbnail (open in the lightbox) rides the optional `onOpenImage`.
//
// The caller keeps the `(pendingAttachments.length > 0 || attachError)`
// mount guard — this component always renders its strip.

import { useI18n } from "../i18n";
import type { PendingAttachment } from "../hooks/useAttachmentStaging";
import { formatAttachmentSize } from "../hooks/useAttachmentStaging";
import { PaperclipIcon } from "./icons";

export function ComposerAttachmentPreview({
  pendingAttachments,
  attachError,
  onRemove,
  onOpenImage,
}: {
  pendingAttachments: PendingAttachment[];
  attachError: string | null;
  onRemove: (key: string) => void;
  /** When set, image thumbnails are clickable (role=button + keyboard) and
   * hand their data-URI to the caller's lightbox — the chat composer's
   * behaviour. Absent ⇒ static thumbnails (ReplyComposer / TaskCard). */
  onOpenImage?: (src: string) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="chat__composer-preview">
      {pendingAttachments.map((att) =>
        att.isImage ? (
          // Image → thumbnail with a per-item × remove control.
          <div key={att.key} className="chat__preview-thumb">
            <img
              className={
                "chat__preview-image" +
                (onOpenImage ? " chat__msg-image--clickable" : "")
              }
              src={att.dataUri}
              alt={t.chat.pastedImageAlt}
              {...(onOpenImage
                ? {
                    role: "button",
                    tabIndex: 0,
                    "aria-label": t.chat.viewImageLabel,
                    onClick: () => onOpenImage(att.dataUri),
                    onKeyDown: (e: React.KeyboardEvent) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onOpenImage(att.dataUri);
                      }
                    },
                  }
                : {})}
            />
            <button
              type="button"
              className="chat__preview-remove"
              aria-label={t.chat.removeAttachmentLabel}
              onClick={() => onRemove(att.key)}
            >
              ×
            </button>
          </div>
        ) : (
          // Non-image → a filename chip (name + size) with the same ×.
          <div key={att.key} className="chat__preview-file">
            <PaperclipIcon size={15} />
            <span className="chat__preview-file-name">
              {att.filename || t.chat.downloadAttachment}
            </span>
            <span className="chat__preview-file-size">
              {formatAttachmentSize(att.size)}
            </span>
            <button
              type="button"
              className="chat__preview-remove chat__preview-remove--inline"
              aria-label={t.chat.removeAttachmentLabel}
              onClick={() => onRemove(att.key)}
            >
              ×
            </button>
          </div>
        )
      )}
      {attachError && (
        <span className="chat__preview-error">{attachError}</span>
      )}
    </div>
  );
}
