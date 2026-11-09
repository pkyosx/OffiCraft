import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import "./office.css";

/** What a deleted message shows in the owner's thread instead of its bubble:
 * 「此訊息已刪除」, which opens the original in the same overlay as the quote
 * row's 看原訊息. The row itself never renders any of the original. */
export function ChatDeletedRow({
  onOpen,
  failure,
}: {
  onOpen: () => void;
  /** The overlay's own failure sentence for this message, or null. */
  failure: ReactNode;
}) {
  const { t } = useI18n();
  return (
    <div className="chat__msg-deleted" data-testid="msg-deleted">
      <button
        type="button"
        className="chat__msg-deleted__open"
        aria-haspopup="dialog"
        onClick={onOpen}
      >
        {t.chat.deletedPlaceholder}
      </button>
      {failure}
    </div>
  );
}
