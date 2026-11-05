import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { bundledBuiltinThemeLoaderFrom, loadBundledBuiltinTheme } from "./bundledBuiltinThemes";
import { BUNDLED_BUILTIN_THEMES, validateThemeBundleWith, type ThemeBundle } from "./themeBundleCore";
import { validateWording } from "./themeWording";
import { validateThemeBundle } from "./themeBundle";
import { paintRecordFor } from "./themePaint";

const THEMES_DIR = resolve(__dirname, "../../../themes");

describe("bundledBuiltinThemeLoaderFrom", () => {
  const day: ThemeBundle = { id: "modern-office-desk-day", name: "Day", colors: {} };

  it("loads only the file of the built-in asked for", async () => {
    const dayFile = vi.fn(() => Promise.resolve(day));
    const nightFile = vi.fn(() => Promise.resolve(day));
    const load = bundledBuiltinThemeLoaderFrom({
      "../../../themes/modern-office-desk-day.theme.json": dayFile,
      "../../../themes/modern-office-desk-night.theme.json": nightFile,
    });

    await expect(load("modern-office-desk-day")).resolves.toBe(day);
    expect(dayFile).toHaveBeenCalledTimes(1);
    expect(nightFile).not.toHaveBeenCalled();
  });

  it("rejects an id that is not a bundled built-in, even when a file by that name exists", async () => {
    const load = bundledBuiltinThemeLoaderFrom({
      "../../../themes/halloween-2026.theme.json": () => Promise.resolve(day),
    });
    await expect(load("halloween-2026")).rejects.toThrow("built-in theme file not found");
  });
});

describe("the shipped bundled built-in themes", () => {
  for (const { id } of BUNDLED_BUILTIN_THEMES) {
    const bundle = JSON.parse(
      readFileSync(resolve(THEMES_DIR, `${id}.theme.json`), "utf8")
    ) as ThemeBundle;

    it(`${id} loads through the app's own loader and carries its id`, async () => {
      expect((await loadBundledBuiltinTheme(id)).id).toBe(id);
    });

    it(`${id} passes the bundle grammar whole once its built-in id is admitted`, () => {
      const skipped: string[] = [];
      expect(
        validateThemeBundleWith(bundle, id, skipped, validateWording, { allowBundledBuiltinId: true })
      ).toBeNull();
      expect(skipped).toEqual([]);
    });

    it(`${id} validates as the pre-paint record`, () => {
      expect(
        validateThemeBundleWith(paintRecordFor(bundle).bundle, id, undefined, null, {
          allowBundledBuiltinId: true,
        })
      ).toBeNull();
    });

    it(`a custom theme under ${id} is refused as a built-in's id`, () => {
      expect(validateThemeBundle({ ...bundle, name: "copy" })).toBe(
        `theme: id "${id}" is reserved for a built-in theme`
      );
    });
  }
});
