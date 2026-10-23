import { useEffect, useRef } from "react";
import { useI18n } from "../i18n";
import { mentionOptionId, type MentionCandidate } from "../lib/chatMention";
import "./chat-mention.css";

export interface ChatMentionListProps {
  id: string;
  items: MentionCandidate[];
  active: number;
  onPick: (c: MentionCandidate) => void;
}

/** `below` is for a composer in the middle of a scrolling page: a list that
 * opens upward past the scroller's top is clipped with no way to scroll to it,
 * while one that opens downward extends the scroll area. */
export function ChatMentionList({
  id,
  items,
  active,
  onPick,
  placement = "above",
}: ChatMentionListProps & { placement?: "above" | "below" }) {
  const { t } = useI18n();
  const listRef = useRef<HTMLUListElement>(null);
  useEffect(() => {
    listRef.current?.children[active]?.scrollIntoView?.({ block: "nearest" });
  }, [active]);
  return (
    <ul
      ref={listRef}
      id={id}
      role="listbox"
      className={placement === "below" ? "chat-mention chat-mention--below" : "chat-mention"}
      aria-label={t.chat.mentionListLabel}
      data-testid="chat-mention-list"
    >
      {items.map((c, i) => (
        <li
          key={`${c.kind}:${c.id}`}
          id={mentionOptionId(id, i)}
          role="option"
          aria-selected={i === active}
          className={
            i === active
              ? "chat-mention__option chat-mention__option--active"
              : "chat-mention__option"
          }
          // Keeps focus (and the phone keyboard) in the composer.
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => onPick(c)}
        >
          <span className="chat-mention__name">{c.name}</span>
          {c.roleName && <span className="chat-mention__meta">{c.roleName}</span>}
          {c.taskNo && (
            <>
              <span className="chat-mention__task-no">{c.taskNo}</span>
              <span className="chat-mention__meta">{c.taskTitle}</span>
            </>
          )}
        </li>
      ))}
    </ul>
  );
}
