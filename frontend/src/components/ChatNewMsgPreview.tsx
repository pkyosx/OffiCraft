// ② T-48 新訊息預覽列 — the strip above the composer that replaced the
// 「有新訊息」 pill: WHO wrote it and ONE line of WHAT they said, so the owner
// can decide whether to go and look without going and looking.
//
// 🔴 ONE STRIP, ALWAYS. A second arrival REPLACES the content; it never stacks.
// The pill it replaces had nothing to update (its text was a constant), so
// "don't stack" is a new obligation that arrived with the content.
//
// 🔴 THE TWO LINES ARE CLIPPED, EACH ON ITS OWN, AND THE HEIGHT IS FIXED. This
// is `.chat__reply-banner`'s rule (owner c-850b28632cc1: 「預覽列要跟回覆訊息
// 一樣,不用顯示所有原文」) and the reason carries over with more force, not
// less: the banner's text changes only when the owner aims at another message,
// whereas THIS text changes on its own, whenever someone writes. A strip that
// grew with the message would shove the input box down under the owner's hands
// mid-sentence. The clipping lives in office.css — `white-space: nowrap` +
// `text-overflow: ellipsis` per line — so the body is passed RAW here, newlines
// and all, exactly as the reply banner passes it.
import { CloseIcon } from "./icons";
import { useI18n } from "../i18n";

export function ChatNewMsgPreview({
  who,
  body,
  onJump,
  onDismiss,
}: {
  /** Display name of whoever sent the message being previewed. */
  who: string;
  /** The message body, RAW — the stylesheet owns the one-line clipping. */
  body: string;
  onJump: () => void;
  onDismiss: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="chat__new-msg" data-testid="chat-new-msg-preview">
      {/* The strip itself is the jump — the owner's screenshot has no separate
       * affordance, and a whole-width target is the easy one to hit on a
       * phone. The x is a sibling, not a child, so its click is not the
       * strip's. */}
      <button
        type="button"
        className="chat__new-msg__hit"
        data-testid="chat-new-msg-jump"
        onClick={onJump}
      >
        <span className="chat__new-msg__text">
          <span className="chat__new-msg__who">{who}</span>
          <span className="chat__new-msg__body">{body}</span>
        </span>
      </button>
      <button
        type="button"
        className="chat__new-msg__x"
        data-testid="chat-new-msg-dismiss"
        aria-label={t.chat.newMsgPreviewDismiss}
        title={t.chat.newMsgPreviewDismiss}
        onClick={onDismiss}
      >
        <CloseIcon size={14} />
      </button>
    </div>
  );
}
