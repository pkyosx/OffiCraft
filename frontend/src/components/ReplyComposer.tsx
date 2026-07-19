// ReplyComposer — the typed-reply input of a reply card (等我回覆卡): text +
// attachments (paste an image / pick files), submitted as ONE answer. Used by
// the 等我回覆 page's waiting cards AND its 重新決定 edit mode; B3's inline
// chat card renders this same component so the two entry points can never
// drift. Attachment staging is the SHARED useAttachmentStaging state machine
// (same caps + funnels as the chat composer) and the preview strip reuses the
// chat composer's classes — one visual language for "composing a message".

import { useLayoutEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { ChatAttachmentInput } from "../api/adapter";
import { autosizeTextarea } from "../lib/autosize";
import {
  ATTACH_ACCEPT,
  useAttachmentStaging,
} from "../hooks/useAttachmentStaging";
import { ComposerAttachmentPreview } from "./ComposerAttachmentPreview";
import { PaperclipIcon, SendIcon } from "./icons";

export function ReplyComposer({
  placeholder,
  onSend,
}: {
  placeholder: string;
  /** Submit the typed answer (text and/or attachments; never called empty).
   * The promise rejecting keeps the composer content so nothing is lost. */
  onSend: (body: string, attachments: ChatAttachmentInput[]) => Promise<void>;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState("");
  // Busy-latch while the answer POST is in flight: the send button disables so
  // a double Enter/click can never double-answer (the server would 409 the
  // second one — we just never fire it).
  const [sending, setSending] = useState(false);
  const {
    pendingAttachments,
    attachError,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
  } = useAttachmentStaging();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const draftRef = useRef<HTMLTextAreaElement>(null);
  // IME composition guard — same belt-and-braces as the chat composer: an
  // Enter that confirms a CJK candidate must never submit.
  const isComposingRef = useRef(false);

  const canSend =
    !sending && (draft.trim().length > 0 || pendingAttachments.length > 0);

  // Multi-line composer (Enter sends, Shift+Enter breaks a line — same as the
  // chat composer): auto-grow the textarea to the draft on every change; the
  // CSS max-height caps it and the textarea scrolls beyond, so a long reply is
  // always fully visible while being typed.
  useLayoutEffect(() => {
    if (draftRef.current) autosizeTextarea(draftRef.current);
  }, [draft]);

  async function submit() {
    if (!canSend) return;
    const body = draft.trim();
    const attachments: ChatAttachmentInput[] = pendingAttachments.map((a) => ({
      dataB64: a.dataUri,
      ...(a.filename ? { filename: a.filename } : {}),
      mime: a.mime,
    }));
    setSending(true);
    try {
      await onSend(body, attachments);
      // Success: the card leaves this pane (refetch) — clear for good measure.
      setDraft("");
      clearAttachments();
    } catch {
      // Failure keeps the typed content; the CALLER surfaces the error notice
      // (this component has no error strip of its own).
    } finally {
      setSending(false);
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (
      e.nativeEvent.isComposing ||
      e.keyCode === 229 ||
      isComposingRef.current
    ) {
      return;
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void submit();
    }
  }

  return (
    <div className="reply-composer">
      {(pendingAttachments.length > 0 || attachError) && (
        <ComposerAttachmentPreview
          pendingAttachments={pendingAttachments}
          attachError={attachError}
          onRemove={removeAttachment}
        />
      )}
      <div className="chat__composer-row">
        <input
          ref={fileInputRef}
          className="chat__file-input"
          type="file"
          accept={ATTACH_ACCEPT}
          multiple
          onChange={onPickFile}
          hidden
        />
        <button
          type="button"
          className="chat__attach"
          aria-label={t.chat.attachLabel}
          title={t.chat.attachLabel}
          onClick={() => fileInputRef.current?.click()}
        >
          <PaperclipIcon size={18} />
        </button>
        {/* Multi-line reply input: a bare Enter submits (onKeyDown), a shifted
         * Enter falls through to the textarea's native newline. */}
        <textarea
          ref={draftRef}
          className="chat__input"
          rows={1}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onCompositionStart={() => {
            isComposingRef.current = true;
          }}
          onCompositionEnd={(e) => {
            isComposingRef.current = false;
            // compositionend delivers the final committed text; sync the draft
            // so the last composed chunk is never dropped.
            setDraft(e.currentTarget.value);
          }}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          placeholder={placeholder}
        />
        <button
          type="button"
          className="chat__send"
          aria-label={t.chat.send}
          onClick={() => void submit()}
          disabled={!canSend}
        >
          <SendIcon size={16} />
        </button>
      </div>
    </div>
  );
}
