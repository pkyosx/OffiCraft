import { useI18n } from "../i18n";
import { Avatar } from "./Avatar";
import type { AvatarKind } from "../lib/themeBundle";

// The 開卡人 chip on a reply card: avatar and name, and a click opens the
// asker's profile like every other avatar in the cockpit. The CT guard
// (ReplyCardOpenerStory) mounts this same component.
export function ReplyCardOpenerChip({
  onClick,
  name,
  kind = "staff",
  src,
}: {
  onClick: () => void;
  name: string;
  // The asker's real kind: an outsource asker shows the theme's 外包 avatar,
  // never a made-up 正職 one.
  kind?: AvatarKind;
  src?: string;
}) {
  const { t } = useI18n();
  return (
    <button
      type="button"
      className="reply-card__opener"
      aria-label={`${t.office.viewProfile} ${name}`}
      title={t.office.viewProfile}
      data-testid="reply-card-opener"
      onClick={onClick}
    >
      <Avatar size={18} kind={kind} src={src} />
      <span className="reply-card__name" title={name}>
        {name}
      </span>
    </button>
  );
}
