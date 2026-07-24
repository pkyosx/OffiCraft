import { LogoMark } from "./icons";
import { useActiveLogo } from "../i18n";

// The top-bar brand mark (T-ea81): renders the active custom theme's studio
// logo IMAGE (an embedded, validated base64 raster) when one is present, and
// falls back to the built-in LogoMark otherwise — so the office built-in and
// every logo-less theme look exactly as before. The image is decorative
// (alt="" + aria-hidden): the wrapping topbar home button carries the
// accessible name. Only used INSIDE the app (post-login); the LoginPage /
// FirstRunPage keep the built-in LogoMark (no theme context pre-login).
export function BrandLogo({ size = 20 }: { size?: number }) {
  const logo = useActiveLogo();
  if (logo) {
    return (
      <img
        className="topbar__logo-img"
        src={logo}
        alt=""
        aria-hidden="true"
        width={size}
        height={size}
        draggable={false}
      />
    );
  }
  return <LogoMark size={size} />;
}
