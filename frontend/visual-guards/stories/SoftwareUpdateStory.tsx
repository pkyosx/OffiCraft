// CT story: the REAL 設定 › 軟體更新 card, in a real browser, against the real
// settings.css.
//
// Unlike the skeleton stories in this folder, this one mounts the ACTUAL
// <SettingsPage> — the layout contracts under test (owner round-2 items ① and
// ②) are properties of the shipped JSX + CSS together, and a hand-copied DOM
// skeleton could drift green while the real card regressed.
//
// The one thing that must be faked is the server's verdict: the mock adapter's
// checkRelease() only ever answers `up_to_date`, so `update_available` and
// `unknown` — precisely the two states owner's screenshots caught — are
// unreachable otherwise. `api` is a plain object export, so the story patches
// its checkRelease in place before render (the same seam the vitest suite spies
// on). Nothing else about the app is stubbed.
import { useEffect } from "react";
import { I18nProvider } from "../../src/i18n";
import { SettingsPage } from "../../src/components/SettingsPage";
import { api } from "../../src/api";
import type { ReleaseCheckView } from "../../src/types";

export type SwVerdict =
  | "up_to_date"
  | "update_available"
  | "unknown"
  // A realistic LONG release tag (prerelease + build metadata is ordinary on
  // GitHub, and 接收 Beta makes prereleases reachable here). This is the case
  // that makes the pill's contents exceed one line at 375px.
  | "update_available_long_tag";

const VERDICTS: Record<SwVerdict, ReleaseCheckView> = {
  up_to_date: {
    status: "up_to_date",
    currentVersion: "0.4.7",
    latestTag: null,
    releaseUrl: null,
  },
  update_available: {
    status: "update_available",
    currentVersion: "0.4.7",
    latestTag: "v0.9.9",
    releaseUrl: "https://github.com/pkyosx/OffiCraft/releases/tag/v0.9.9",
  },
  update_available_long_tag: {
    status: "update_available",
    currentVersion: "0.4.7",
    latestTag: "v0.9.9-rc.4+build.20260720.arm64",
    releaseUrl:
      "https://github.com/pkyosx/OffiCraft/releases/tag/v0.9.9-rc.4+build.20260720.arm64",
  },
  unknown: {
    status: "unknown",
    currentVersion: "0.4.7",
    latestTag: null,
    releaseUrl: null,
  },
};

export function SoftwareUpdateStory({
  verdict,
  theme = "office",
}: {
  verdict: SwVerdict;
  theme?: "office" | "xian";
}) {
  // Patch BEFORE the first render commits so the click in the spec always
  // resolves to the requested verdict.
  api.checkRelease = async () => VERDICTS[verdict];

  useEffect(() => {
    const root = document.documentElement;
    if (theme === "xian") root.setAttribute("data-theme", "xian");
    else root.removeAttribute("data-theme");
    // The card sits on the app background in production; CT's harness page has
    // none, so paint it here — otherwise a translucent pill would composite
    // against white and every colour measurement would be fiction.
    document.body.style.background = "var(--color-bg)";
    return () => root.removeAttribute("data-theme");
  }, [theme]);

  return (
    <I18nProvider>
      <div style={{ height: 600 }}>
        <SettingsPage />
      </div>
    </I18nProvider>
  );
}
