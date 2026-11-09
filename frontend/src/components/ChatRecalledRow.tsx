import { useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import "./office.css";

/** What a recalled message shows in the owner's thread instead of its bubble:
 * 「此訊息已收回 ▸」, which opens the original below it in quote style. The
 * original is passed in and rendered only while open. */
export function ChatRecalledRow({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  return (
    <div className="chat__msg-recalled" data-testid="msg-recalled">
      <button
        type="button"
        className="chat__msg-recalled__toggle"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {t.chat.recalledNotice}
        <span aria-hidden="true">{open ? " ▾" : " ▸"}</span>
      </button>
      {open && (
        <div
          className="chat__msg-recalled__original"
          data-testid="msg-recalled-original"
        >
          {children}
        </div>
      )}
    </div>
  );
}
