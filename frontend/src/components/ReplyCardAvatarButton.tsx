import { useI18n } from "../i18n";
import { Avatar } from "./Avatar";

// The reply-card header's avatar-as-click-target (T-a706): a bare button
// wrapper around the (aria-hidden) Avatar glyph, mirroring MemberCard's
// avatar pattern (office.css .member-card__avatar) — same button-reset +
// focus-ring CSS lives in replies.css under .reply-card__avatar. Extracted
// out of RepliesPage so the CT visual guard (ReplyCardAvatarStory) mounts
// this SAME component instead of an approximation of it.
export function ReplyCardAvatarButton({
  onClick,
  size = 34,
}: {
  onClick: () => void;
  size?: number;
}) {
  const { t } = useI18n();
  return (
    <button
      type="button"
      className="reply-card__avatar"
      aria-label={t.office.viewProfile}
      title={t.office.viewProfile}
      onClick={onClick}
    >
      <Avatar size={size} />
    </button>
  );
}
