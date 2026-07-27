import { useEffect, useState } from "react";
import { UserIcon } from "./icons";
import { useActiveAvatars } from "../i18n";
import type { AvatarKind } from "../lib/themeBundle";
import { authedAttachmentUrl } from "../api/http";

interface AvatarProps {
  size?: number;
  /** The role this avatar stands for (T-16a1 P5; extended per role in T-ea81):
   * 正職 "member" (the default) / 外包 "outsource" / CEO "owner" / 助理
   * "assistant". Selects which of the active theme's avatar images to render;
   * when the theme carries none for this kind, the built-in UserIcon glyph is
   * used (office never degrades). */
  kind?: AvatarKind;
  /** Stable-member personal image. It outranks the theme image and falls back
   * through theme -> glyph if the authenticated blob can no longer load. */
  src?: string;
}

// Pure identity glyph — NO presence dot. Presence is carried exclusively by the
// shared PresenceBadge (its 5-state LifecycleDot colour is the single presence
// signal); an avatar-corner dot would be a second, contradicting presence
// system, so it is gone everywhere.
//
// T-16a1 P5 (extended per role in T-ea81): a custom theme MAY carry a per-role
// avatar IMAGE (an embedded, validated base64 raster). When the active theme provides one for
// this `kind`, it renders as an <img> inside the same .avatar frame (round
// clip, same box); otherwise the built-in UserIcon glyph is used — so the
// office built-in and every avatars-less theme look exactly as before. The
// image is decorative (alt="" + aria-hidden): callers that need an accessible
// name label the button/container that wraps the Avatar (e.g.
// .member-card__avatar), so that wrapper's label stays the only accessible name.
export function Avatar({ size = 40, kind = "member", src }: AvatarProps) {
  const avatars = useActiveAvatars();
  const personal = authedAttachmentUrl(src);
  const theme = avatars?.[kind];
  const [failed, setFailed] = useState<string[]>([]);
  useEffect(() => setFailed([]), [personal, theme]);
  const selected =
    personal && !failed.includes(personal)
      ? personal
      : theme && !failed.includes(theme)
        ? theme
        : undefined;
  return (
    <span className="avatar" style={{ width: size, height: size }}>
      {selected ? (
        <img
          className="avatar__img"
          src={selected}
          alt=""
          aria-hidden="true"
          width={size}
          height={size}
          draggable={false}
          onError={() => setFailed((current) => [...current, selected])}
        />
      ) : (
        <UserIcon size={Math.round(size * 0.5)} className="avatar__glyph avatar__glyph--office" />
      )}
    </span>
  );
}
