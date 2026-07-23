import { UserIcon } from "./icons";
import { useActiveAvatars } from "../i18n";
import type { AvatarKind } from "../lib/themeBundle";

interface AvatarProps {
  size?: number;
  /** The member type this avatar stands for (T-16a1 P5): 正職 "member" (the
   * default) or 外包 "outsource". Selects which of the active theme's avatar
   * images to render; when the theme carries none for this kind, the built-in
   * UserIcon glyph is used (office never degrades). */
  kind?: AvatarKind;
}

// Pure identity glyph — NO presence dot. Presence is carried exclusively by the
// shared PresenceBadge (its 5-state LifecycleDot colour is the single presence
// signal); an avatar-corner dot would be a second, contradicting presence
// system, so it is gone everywhere.
//
// T-16a1 P5: a custom theme MAY carry a per-member-type avatar IMAGE (an
// embedded, validated base64 raster). When the active theme provides one for
// this `kind`, it renders as an <img> inside the same .avatar frame (round
// clip, same box); otherwise the built-in UserIcon glyph is used — so the
// office built-in and every avatars-less theme look exactly as before. The
// image is decorative (alt="" + aria-hidden): callers that need an accessible
// name label the button/container that wraps the Avatar (e.g.
// .member-card__avatar), so that wrapper's label stays the only accessible name.
export function Avatar({ size = 40, kind = "member" }: AvatarProps) {
  const avatars = useActiveAvatars();
  const src = avatars?.[kind];
  return (
    <span className="avatar" style={{ width: size, height: size }}>
      {src ? (
        <img
          className="avatar__img"
          src={src}
          alt=""
          aria-hidden="true"
          width={size}
          height={size}
          draggable={false}
        />
      ) : (
        <UserIcon size={Math.round(size * 0.5)} className="avatar__glyph avatar__glyph--office" />
      )}
    </span>
  );
}
