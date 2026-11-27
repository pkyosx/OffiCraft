import { ChevronDownIcon } from "./icons";
import "./chat-groups.css";

export function HeadingBody({ label, collapsed, unread }: { label: string; collapsed: boolean; unread: number }) {
  return (
    <>
      <span className="chat-groups__heading-label">{label}</span>
      <ChevronDownIcon size={14} className="chat-groups__chevron" />
      <span className="member-card__unread-slot chat-groups__pill-slot">
        {collapsed && unread > 0 && (
          <span className="member-card__unread chat-groups__heading-unread">{unread > 99 ? "99+" : unread}</span>
        )}
      </span>
    </>
  );
}
