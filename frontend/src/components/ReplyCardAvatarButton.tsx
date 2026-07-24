import { useI18n } from "../i18n";
import { Avatar } from "./Avatar";
import type { AvatarKind } from "../lib/themeBundle";

// The reply-card header's avatar-as-click-target (T-a706): a bare button
// wrapper around the (aria-hidden) Avatar glyph, mirroring MemberCard's
// avatar pattern (office.css .member-card__avatar) — same button-reset +
// focus-ring CSS lives in replies.css under .reply-card__avatar. Extracted
// out of RepliesPage so the CT visual guard (ReplyCardAvatarStory) mounts
// this SAME component instead of an approximation of it.
export function ReplyCardAvatarButton({
  onClick,
  size = 34,
  kind = "member",
}: {
  onClick: () => void;
  size?: number;
  // The card initiator's REAL kind (T-3738). A reply card CAN be raised by an
  // outsource asker (RepliesPage resolves ow- initiators to their 代號 and
  // routes their profile to the worker panel), so the caller passes
  // "outsource" for an ow- initiator; the avatar then shows the theme's 外包
  // image instead of fabricating a 正職 identity.
  kind?: AvatarKind;
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
      <Avatar size={size} kind={kind} />
    </button>
  );
}
