import { UserIcon } from "./icons";

interface AvatarProps {
  size?: number;
}

// Pure identity glyph — NO presence dot. Presence is carried exclusively by the
// shared PresenceBadge (its 5-state LifecycleDot colour is the single presence
// signal); an avatar-corner dot would be a second, contradicting presence
// system, so it is gone everywhere.
export function Avatar({ size = 40 }: AvatarProps) {
  return (
    <span className="avatar" style={{ width: size, height: size }}>
      <UserIcon size={Math.round(size * 0.5)} className="avatar__glyph avatar__glyph--office" />
    </span>
  );
}
