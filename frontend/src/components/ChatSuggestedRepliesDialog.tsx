import { useEffect, useRef, type KeyboardEvent } from "react";
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
  useEffect(() => {
    boxRef.current?.querySelector<HTMLElement>("input, .sugg-edit__add")?.focus();
  }, []);

  const trapTab = (e: KeyboardEvent<HTMLElement>) => {
    if (e.key !== "Tab" || !boxRef.current) return;
    const items = Array.from(
      boxRef.current.querySelectorAll<HTMLElement>("button:not(:disabled), input"),
    );
    const at = items.indexOf(document.activeElement as HTMLElement);
    const next = e.shiftKey ? (at <= 0 ? items.length - 1 : at - 1) : (at + 1) % items.length;
    e.preventDefault();
    items[next]?.focus();
  };

  return createPortal(
    <div
      className="chat-groups__dialog-backdrop"
      onPointerDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      onClick={(e) => e.stopPropagation()}
    >
      <div
        ref={boxRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="chat-groups__dialog chat-suggested-replies__dialog"
        data-testid="chat-suggested-replies-dialog"
        onKeyDown={trapTab}
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
