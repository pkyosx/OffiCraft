import { useEffect } from "react";
import { I18nProvider } from "../../src/i18n";
import { mockApi } from "../../src/api/mock";
import { setToken } from "../../src/api/auth";
import { TINY_PNG } from "../../src/lib/paintFixtures";
import { loadBundledBuiltinTheme } from "../../src/lib/bundledBuiltinThemes";
import type { ThemeBundle } from "../../src/lib/themeBundle";

export type StoryTheme = "builtin-desk" | "custom-animated" | "custom-still";

const DESK = "modern-office-desk-day";

async function customTheme(kind: "custom-animated" | "custom-still"): Promise<ThemeBundle> {
  const canvas =
    kind === "custom-animated" ? (await loadBundledBuiltinTheme(DESK)).backgrounds!.canvas! : TINY_PNG;
  return {
    id: kind,
    name: kind,
    colors: { "--color-bg": "#101820" },
    backgrounds: { canvas },
    backgroundModes: { canvas: "cover" },
  };
}

export function MobileStillCanvasStory({ theme }: { theme: StoryTheme }) {
  useEffect(() => {
    void (async () => {
      let id = DESK;
      if (theme !== "builtin-desk") {
        const bundle = await customTheme(theme);
        await mockApi.putTheme(bundle);
        id = bundle.id;
      }
      await mockApi.patchServerSettings({ displayTheme: id, displayThemeSchedule: [] });
      setToken("ct-owner-token");
    })();
  }, [theme]);
  return (
    <I18nProvider>
      <div />
    </I18nProvider>
  );
}
