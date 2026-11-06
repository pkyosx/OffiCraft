import { useEffect, useRef, type DragEvent, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { SuggestedRepliesEditor } from "./SuggestedRepliesEditor";
import { CloseIcon } from "./icons";
import "./chat-groups.css";
import "./suggested-replies.css";

/** One member's chat 建議回覆, opened from the chat room's header. */
export function ChatSuggestedRepliesDialog({
  memberName,
  replies,
  saveFailed,
  onSave,
  onClose,
}: {
  memberName: string;
  replies: readonly string[];
  saveFailed: boolean;
  onSave: (next: string[]) => void;
  /** Must move focus out of the dialog before unmounting it: a row saves on
   * blur, and an input that unmounts while focused drops what was being typed. */
  onClose: () => void;
}) {
  const { t } = useI18n();
  const boxRef = useRef<HTMLDivElement>(null);
  const titleId = "chat-suggested-replies-title";

  useEscapeLayer(onClose, boxRef);
  const lastFocusRef = useRef<HTMLElement | null>(null);
  const focusFirstControl = () =>
    boxRef.current?.querySelector<HTMLElement>("input, .sugg-edit__add:not(:disabled)")?.focus();
  useEffect(focusFirstControl, []);
  // A list that changes under the reader (a failed save rolled back, another
  // window saved) can remove or disable the focused control. Focus then lands on
  // <body>, where the Tab trap on this dialog no longer hears the key. A reader
  // who clicked away on purpose has no control to rescue: taking focus back would
  // reopen the phone keyboard they just closed.
  useEffect(() => {
    const last = lastFocusRef.current;
    if (last && (!last.isConnected || last.matches(":disabled"))) focusFirstControl();
  }, [replies]);

  const onKeyDown = (e: KeyboardEvent<HTMLElement>) => {
    // This Esc cancels the IME's candidates. preventDefault is what tells the
    // Esc layer the key is spent; without it the dialog closes and blur saves the
    // half-typed text.
    if (e.key === "Escape" && (e.nativeEvent.isComposing || e.keyCode === 229)) {
      e.preventDefault();
      return;
    }
    if (e.key !== "Tab" || !boxRef.current) return;
    const items = Array.from(
      boxRef.current.querySelectorAll<HTMLElement>("button:not(:disabled), input"),
    );
    const at = items.indexOf(document.activeElement as HTMLElement);
    const next = e.shiftKey ? (at <= 0 ? items.length - 1 : at - 1) : (at + 1) % items.length;
    e.preventDefault();
    items[next]?.focus();
  };

  // React bubbles through the portal, so an uncaught drag reaches the chat room
  // behind this dialog and stages the file in its composer. A file still needs
  // preventDefault or the browser navigates to it; dragged text stays droppable
  // on the inputs.
  const onDragOver = (e: DragEvent<HTMLElement>) => {
    e.stopPropagation();
    if (e.dataTransfer.types.includes("Files")) e.preventDefault();
  };
  const onDrop = (e: DragEvent<HTMLElement>) => {
    e.stopPropagation();
    if (e.dataTransfer.files.length > 0) e.preventDefault();
  };

  return createPortal(
    <div
      className="chat-groups__dialog-backdrop"
      onPointerDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      onClick={(e) => e.stopPropagation()}
      onDragOver={onDragOver}
      onDrop={onDrop}
    >
      <div
        ref={boxRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="chat-groups__dialog chat-suggested-replies__dialog"
        data-testid="chat-suggested-replies-dialog"
        onKeyDown={onKeyDown}
        onFocus={(e) => {
          lastFocusRef.current = e.target;
        }}
        onBlur={(e) => {
          // Recorded here, not in onFocus: the save this blur triggers can land
          // before the clicked control takes focus, and a rescue then would pull
          // focus away from it. Switching windows also blurs with no target, and a
          // control being disabled blurs too; neither is the reader leaving.
          if (e.relatedTarget) lastFocusRef.current = e.relatedTarget as HTMLElement;
          else if (document.hasFocus() && !e.target.matches(":disabled")) lastFocusRef.current = null;
        }}
      >
        <div className="chat-suggested-replies__head">
          <div>
            <h2 id={titleId} className="chat-groups__dialog-title">
              {t.chat.suggestedRepliesLabel} · {memberName}
            </h2>
            <p className="chat-groups__dialog-hint">{t.chat.suggestedRepliesSub}</p>
          </div>
          <button
            type="button"
            className="chat-suggested-replies__close"
            aria-label={t.chat.suggestedRepliesClose}
            title={t.chat.suggestedRepliesClose}
            onClick={onClose}
          >
            <CloseIcon size={16} />
          </button>
        </div>
        <SuggestedRepliesEditor
          label={t.chat.suggestedRepliesLabel}
          idPrefix="chat-suggested-reply"
          value={replies}
          onCommit={onSave}
          emptyNote={t.chat.suggestedRepliesEmpty}
          keepFocusOnRemove
        />
        {saveFailed && (
          <div className="sugg-edit__error" role="alert">
            {t.chat.suggestedRepliesSaveError}
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}
