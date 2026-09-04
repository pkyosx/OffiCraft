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
import { useIsMobile } from "../hooks/useIsMobile";
import { enterShouldSend } from "../lib/composerKeys";
import {
  ATTACH_ACCEPT,
  useAttachmentStaging,
  STAGING_TARGET_PER_MOUNT,
} from "../hooks/useAttachmentStaging";
import { ComposerAttachmentPreview } from "./ComposerAttachmentPreview";
import { PaperclipIcon, SendIcon } from "./icons";

export function ReplyComposer({
  placeholder,
  hasSelection = false,
  sendNowRef,
  onSend,
}: {
  placeholder: string;
  /** Whether the card face has quick-reply options STAGED. This button is the
   * card's ONE send: the answer it fires carries the ticked options and the
   * typed text together, so a staged selection alone must be sendable even with
   * an empty draft — and, symmetrically, an answer with NOTHING staged and
   * NOTHING typed must not be sendable at all. */
  hasSelection?: boolean;
  /** Handed a callback that fires this composer's CURRENT content (typed text
   * + staged attachments) as part of an answer the CARD is submitting for its
   * own reason — a single-select chip click, which IS the decision. The
   * empty-answer refusal does not apply there (the click carries the option),
   * so the callback bypasses `canSend`; the in-flight latch still holds, so a
   * second click during the POST cannot double-answer. */
  sendNowRef?: React.MutableRefObject<(() => void) | null>;
  /** Submit the answer — the typed text and attachments, which the caller
   * combines with whatever it has staged. Never called on a wholly empty
   * answer. The promise rejecting keeps the composer content so nothing is
   * lost. */
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
  } = useAttachmentStaging(
    // Mounted under a key that changes with the card/task it belongs to, so a
    // switch UNMOUNTS this surface and nothing can outlive it (T-48, R9-1).
    STAGING_TARGET_PER_MOUNT,
  );
  const fileInputRef = useRef<HTMLInputElement>(null);
  const draftRef = useRef<HTMLTextAreaElement>(null);
  // IME composition guard — same belt-and-braces as the chat composer: an
  // Enter that confirms a CJK candidate must never submit.
  const isComposingRef = useRef(false);
  // Phone viewport → Enter inserts a newline, send button sends (same rule as
  // the chat composer; no physical keyboard means Shift+Enter is impossible).
  const isMobile = useIsMobile();

  const canSend =
    !sending &&
    (hasSelection ||
      draft.trim().length > 0 ||
      pendingAttachments.length > 0);

  // Multi-line composer (desktop: Enter sends, Shift+Enter breaks a line;
  // mobile: Enter breaks a line — same as the chat composer): auto-grow the
  // textarea to the draft on every change; the
  // CSS max-height caps it and the textarea scrolls beyond, so a long reply is
  // always fully visible while being typed.
  useLayoutEffect(() => {
    if (draftRef.current) autosizeTextarea(draftRef.current);
  }, [draft]);

  async function submit({ force = false }: { force?: boolean } = {}) {
    if (sending) return;
    if (!force && !canSend) return;
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

  // Re-published on EVERY render so the callback the card holds closes over
  // this render's draft/attachments — a stale one would send yesterday's text.
  useLayoutEffect(() => {
    if (!sendNowRef) return;
    sendNowRef.current = () => void submit({ force: true });
    return () => {
      sendNowRef.current = null;
    };
  });

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // Shared send rule (IME gate + mobile-newline) — see lib/composerKeys.
    // A mobile Enter returns false and is left un-prevented so the textarea
    // inserts a native newline.
    if (enterShouldSend(e, { isMobile, composing: isComposingRef.current })) {
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
        {/* Multi-line reply input. Desktop: a bare Enter submits (onKeyDown), a
         * shifted Enter falls through to the native newline. Mobile: Enter
         * falls through too (send is via the button). */}
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
