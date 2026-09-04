// AttachmentStrip — the ONE renderer for STORED attachments (served refs with
// a download url), extracted from ChatArea.renderAttachment /
// ReplyCardBody.ReplyAnswerAttachments (T-5e8a) so the reply card's new
// question-side attachments never become a third copy-paste. Class knobs keep
// each call site's EXISTING markup/classes byte-identical.
//
// EVERY item — image thumbnail and non-image chip alike — opens the SAME
// MarkdownPreviewOverlay. The strip owns that state; callers do not pass an
// image-open callback and cannot route images anywhere else. Before T-f014 the
// image branch nominally deferred to a caller-owned Lightbox via an
// `onOpenImage` prop; the prop had already stopped being read, so five call
// sites were passing a handler into a component that ignored it and mounting a
// second overlay that could never open.

import { Fragment, useState } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import type { ChatAttachmentView } from "../api/adapter";
import { authedAttachmentUrl } from "../api/http";
import { copyAttachmentShareLink } from "../lib/shareLink";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";
import { CheckIcon, CopyIcon, PaperclipIcon } from "./icons";

export function AttachmentStrip({
  attachments,
  className,
  itemClassName,
  imageClassName,
  fileChipClassName = "chat__msg-file",
  fileNameClassName = "chat__msg-file-name",
  fileNameColClassName,
  showShareLink = false,
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
  /** Stored blobs expose one canonical share control even when their body is
   * download-only (PDF/binary). ChatArea supplies its own hover control. */
  showShareLink?: boolean;
  /** Per-item extra node rendered after the image/chip (ChatArea's hover
   * 複製分享連結 button). */
  renderExtra?: (att: ChatAttachmentView) => ReactNode;
  /** Per-item node rendered BELOW the filename, inside the chip (T-6338 —
   * lets a non-image download chip carry a second line without every other
   * call site knowing about it). Undefined ⇒ no second line, no wrapper. */
  renderMeta?: (att: ChatAttachmentView) => ReactNode;
}) {
  const { t } = useI18n();
  // 🔴 THE OPEN PREVIEW IS AN ITEM OF THE LIST ON SCREEN, NOT A REMEMBERED
  // OBJECT (T-48, R11-1). This used to hold the `ChatAttachmentView` itself, so
  // the overlay outlived the list it was opened from: hand this strip another
  // room's / another task's / another card's attachments and it kept rendering
  // the old file's name and the old file's bytes over the new owner's screen.
  // The tenth review measured exactly that in chat and the fix landed on
  // `ChatArea.mdPreview`, which is a DIFFERENT overlay — the chip has always
  // opened this one.
  //
  // Storing the id and looking it up makes the answer a derivation instead of a
  // remembered fact: the preview exists only while its row is still in
  // `attachments`, on the SAME render, with no effect to fire and no guard for
  // anybody to forget. It also needs no notion of a "visit", which is right —
  // three of this component's four mount points (the two reply-card strips and
  // the task-artifacts popover) do not live in a conversation at all.
  const [previewId, setPreviewId] = useState<string | null>(null);
  const preview = attachments.find((a) => a.id === previewId) ?? null;
  const [shareCopiedId, setShareCopiedId] = useState<string | null>(null);

  if (attachments.length === 0) return null;

  function renderOne(att: ChatAttachmentView) {
    const share = showShareLink ? (
      <button
        type="button"
        className="chat__share-btn"
        aria-label={shareCopiedId === att.id ? t.chat.shareLinkCopied : t.chat.copyShareLink}
        title={shareCopiedId === att.id ? t.chat.shareLinkCopied : t.chat.copyShareLink}
        onClick={() => {
          const id = att.backingAttachmentId ?? att.id;
          void copyAttachmentShareLink(id)
            .then(() => {
              setShareCopiedId(att.id);
              window.setTimeout(() => setShareCopiedId((current) => current === att.id ? null : current), 2000);
            })
            .catch((error) => console.warn("AttachmentStrip: copy share link failed", error));
        }}
      >
        {shareCopiedId === att.id ? <CheckIcon size={13} /> : <CopyIcon size={13} />}
      </button>
    ) : null;
    if (att.isImage) {
      const src = authedAttachmentUrl(att.url);
      const clickable = {
        role: "button",
        tabIndex: 0,
        "aria-label": t.chat.viewImageLabel,
        onClick: () => setPreviewId(att.id),
        onKeyDown: (e: React.KeyboardEvent) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setPreviewId(att.id);
          }
        },
      };
      return <Fragment key={itemClassName ? undefined : att.id}>
        <img
          key={itemClassName ? undefined : att.id}
          className={imageClassName}
          src={src}
          alt={t.chat.imageAlt}
          {...clickable}
        />
        {share}
      </Fragment>;
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
    // Files with no specialized caller (PDF and opaque binary attachments in
    // particular) still enter the common shell; it supplies download/share
    // and an honest non-previewable body instead of navigating away.
    return <Fragment key={itemClassName ? undefined : att.id}>
      <button
        type="button"
        key={itemClassName ? undefined : att.id}
        className={fileChipClassName}
        title={fullName}
        onClick={() => setPreviewId(att.id)}
      >
        {content}
      </button>
      {share}
    </Fragment>;
  }

  return (
    <>
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
    {preview && <MarkdownPreviewOverlay title={preview.filename || t.chat.downloadAttachment} url={preview.url} attachmentId={preview.backingAttachmentId ?? preview.id} mime={preview.mime} onClose={() => setPreviewId(null)} />}
    </>
  );
}
