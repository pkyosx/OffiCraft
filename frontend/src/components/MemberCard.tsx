import { useI18n } from "../i18n";
import type { Member } from "../types";
import { useWindowActive } from "../hooks/useWindowActive";
import { Avatar } from "./Avatar";
import { PresenceBadge } from "./PresenceBadge";

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
      {/* No status dot on the avatar: the PresenceBadge below carries the single
       * authoritative lifecycle dot (5-state), so a 2-state avatar dot beside it
       * would contradict it. Same reason the detail panel dropped its avatar dot.
       * The avatar is its own click/keyboard target (member detail) nested in
       * the chat row — stopPropagation on both events keeps the two apart. */}
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
        {/* Roster cards are 正職 members (T-16a1 P5) — outsource workers live in
            the OutsourcePanel, not this roster. */}
        <Avatar size={40} kind="member" />
      </button>

      <div className="member-card__body">
        <div className="member-card__line">
          <span className="member-card__name">{member.name}</span>
          {/* No 離線 text badge beside the name (owner 2026-07-17, retires the
           * mockup's badge): the PresenceBadge dot below already turns grey the
           * moment a member goes offline/stopped, so the word restated a fact
           * the dot had just made. Screen readers keep the fact — the dot names
           * itself now (LifecycleDot role="img" + aria-label), which is what
           * had to land BEFORE this badge could go. */}
        </div>
        {/* Single presence truth: the SHARED PresenceBadge (lifecycle dot +
         * role) — the SAME component the monitor session row and the detail
         * panel render. The dot's COLOUR carries all five real presence states
         * (offline/waking/online/stopping/stopped); the roster paints no status
         * word and no last-seen, so presence reads identically (and once)
         * across all three cockpit surfaces. */}
        {/* No settings gear here: owner 2026-07-17 moved the role ⚙ OFF the
         * roster row and INTO the member detail panel's identity card — the
         * roster row stays a pure presence line. */}
        <div className="member-card__presence">
          <PresenceBadge member={member} />
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
       * in the open, watched conversation a new message is read immediately
       * (listChat auto-mark), so the badge never accumulates. A BACKGROUNDED
       * window is different — the open thread stops consuming reads there
       * (useChat peeks read-only), unread genuinely accumulates, and the badge
       * must show it even on the selected card; returning to the foreground
       * re-marks and clears it (badge-flash fix).
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
