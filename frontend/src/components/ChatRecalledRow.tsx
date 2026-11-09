import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import "./office.css";

/** What a recalled message shows in the owner's thread instead of its bubble:
 * 「此訊息已刪除」, which opens the original in the same overlay as the quote
 * row's 看原訊息. The row itself never renders any of the original. */
export function ChatRecalledRow({
  onOpen,
  failure,
}: {
  onOpen: () => void;
  /** The overlay's own failure sentence for this message, or null. */
  failure: ReactNode;
}) {
  const { t } = useI18n();
  return (
    <div className="chat__msg-recalled" data-testid="msg-recalled">
      <button
        type="button"
        className="chat__msg-recalled__open"
        aria-haspopup="dialog"
        onClick={onOpen}
      >
        {t.chat.recalledPlaceholder}
      </button>
      {failure}
    </div>
  );
}
