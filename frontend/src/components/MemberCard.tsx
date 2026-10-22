import { useI18n } from "../i18n";
import type { Member } from "../types";
import { useWindowActive } from "../hooks/useWindowActive";
import { Avatar } from "./Avatar";
import { avatarKindForMember } from "../lib/avatarKind";
import { PresenceBadge } from "./PresenceBadge";
import { PresenceAvatar } from "./PresenceAvatar";
import { NameWithActivity } from "./MemberActivity";

interface MemberCardProps {
  member: Member;
  selected: boolean;
  /** avatar click → open the member detail panel */
  onOpenDetail: () => void;
  /** row click (anywhere but the avatar) → open the chat room for this member */
  onChat: () => void;
}

export function MemberCard({
  member,
  selected,
  onOpenDetail,
  onChat,
}: MemberCardProps) {
  const { t } = useI18n();
  // Is the owner actually looking at the cockpit (window focused + tab
  // visible)? Drives the badge suppression below.
  const windowActive = useWindowActive();

  return (
    // Click semantics (owner feedback): the WHOLE ROW is the chat entry —
    // tapping anywhere jumps straight to this member's conversation (the old
    // dedicated 聊聊 button is gone). The AVATAR alone is the second target:
    // it opens the member detail panel (the old row-body behaviour), with
    // stopPropagation so it never also triggers the row's chat jump.
    <div
      className={`member-card${selected ? " member-card--selected" : ""}`}
      onClick={onChat}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onChat();
        }
      }}
      role="button"
      tabIndex={0}
    >
      {/* The avatar is its own click/keyboard target (member detail) nested in
       * the chat row — stopPropagation on both events keeps the two apart. */}
      <PresenceAvatar presence={member.lifecycle}>
        <button
          type="button"
          className="member-card__avatar"
          aria-label={t.office.viewProfile}
          title={t.office.viewProfile}
          onClick={(e) => {
            e.stopPropagation();
            onOpenDetail();
          }}
          onKeyDown={(e) => e.stopPropagation()}
        >
          <Avatar size={40} kind={avatarKindForMember(member)} src={member.avatarUrl} />
        </button>
      </PresenceAvatar>

      <div className="member-card__body">
        <div className="member-card__line">
          <NameWithActivity
            activity={member.activity}
            memberName={member.name}
            testId={`activity-icon-${member.id}`}
            layout="list"
          >
            <span className="member-card__name">{member.name}</span>
          </NameWithActivity>
        </div>
        {/* Line 2: the runtime warning mark and the role. Presence is the
         * avatar-corner dot (PresenceAvatar), the same on every identity
         * surface. */}
        <div className="member-card__presence">
          <PresenceBadge member={member} dot={false} />
        </div>
      </div>

      {/* M2-1 unread COUNT badge (the red dot, upgraded) — at the row's flex
       * END, the spot the dedicated 聊聊 button occupied until Seth removed it
       * (2026-07-13 ruling, overrides the mockup: the button duplicated the
       * row's own chat-entry click; the slot now carries ONLY the unread
       * signal — shown when unread exists, nothing otherwise). A red pill
       * showing HOW MANY messages this member has sent the owner past the
       * owner's read watermark (server-computed `member.unreadCount`; >99
       * renders "99+").
       * count 0 → not rendered at all. Suppressed while THIS member's chat is
       * open (`selected`) AND the owner is actually looking (`windowActive`):
       * in the open, watched conversation ChatArea marks each new message read
       * as it lands, so the badge never accumulates. A BACKGROUNDED window is
       * different — nothing marks anything read there, unread genuinely
       * accumulates, and the badge must show it even on the selected card;
       * returning to the foreground marks and clears it (badge-flash fix).
       * INDEPENDENT of the PresenceBadge — an offline member can be unread.
       * No handler of its own: the badge sits inside the row (= the chat
       * entry), so tapping it IS tapping the row = opening the chat. */}
      {member.unreadCount > 0 && !(selected && windowActive) && (
        <span className="member-card__unread" data-testid="unread-badge">
          {member.unreadCount > 99 ? "99+" : member.unreadCount}
        </span>
      )}
    </div>
  );
}
