import { UserIcon } from "./icons";

interface AvatarProps {
  size?: number;
}

/* 修仙 avatar glyph — a robed cultivator (topknot bun, head, crossed-collar
 * robe) in parchment gold. Rendered full-bleed inside the circular xian avatar
 * container (see office.css) so the robe hem is clipped by the circle. */
function XianCultivatorGlyph({ size }: { size: number }) {
  return (
    <svg
      className="avatar__glyph avatar__glyph--xian"
      width={size}
      height={size}
      viewBox="0 0 44 44"
      fill="#ecd58a"
      aria-hidden
    >
      {/* topknot bun */}
      <circle cx="22" cy="7.6" r="2.8" />
      {/* head */}
      <circle cx="22" cy="15" r="6.3" />
      {/* robe body */}
      <path d="M22 22c-6.6 0-10 4.2-11 9.6L10 43h24l-1-11.4C32 26.2 28.6 22 22 22Z" />
      {/* crossed-collar notch */}
      <path d="M22 23l-4.3 8h8.6z" fill="#17110a" />
    </svg>
  );
}

// Pure identity glyph — NO presence dot. Presence is carried exclusively by the
// shared PresenceBadge (its 5-state LifecycleDot colour is the single presence
// signal); an avatar-corner dot would be a second, contradicting presence
// system, so it is gone everywhere.
//
// Theme switching: both glyphs are always rendered and CSS picks one via the
// html[data-theme] attribute selector (see office.css) — same pure-CSS pattern
// as every other xian restyle, no JS theme read in this component.
export function Avatar({ size = 40 }: AvatarProps) {
  return (
    <span className="avatar" style={{ width: size, height: size }}>
      <UserIcon size={Math.round(size * 0.5)} className="avatar__glyph avatar__glyph--office" />
      <XianCultivatorGlyph size={size} />
    </span>
  );
}
