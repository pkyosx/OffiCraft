import type { ReactNode } from "react";
import { useActiveNavIcons } from "../i18n";
import type { NavIconKey } from "../lib/themeBundle";

// A nav-tab icon (T-ea81): renders the active custom theme's per-tab icon IMAGE
// (an embedded, validated base64 raster) when one is present for this tab, and
// falls back to the built-in icon otherwise — so the office built-in and every
// navIcons-less theme look exactly as before (a tab the theme omits keeps its
// built-in icon; office never degrades). The image is decorative (alt="" +
// aria-hidden): the tab's own text label carries the accessible name.
export function NavIcon({
  tabKey,
  size = 15,
  fallback,
}: {
  tabKey: NavIconKey;
  size?: number;
  fallback: ReactNode;
}) {
  const src = useActiveNavIcons()?.[tabKey];
  if (src) {
    return (
      <img
        className="nav-tab__icon-img"
        src={src}
        alt=""
        aria-hidden="true"
        width={size}
        height={size}
        draggable={false}
      />
    );
  }
  return <>{fallback}</>;
}
