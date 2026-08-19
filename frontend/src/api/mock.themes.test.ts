// Mock adapter parity for the THEME RESOURCE (T-83ef: 主題包自己一個資源).
//
// These claims used to be asserted through `PATCH /api/settings { custom_themes }`
// — the whole-array write that re-sent every embedded image to change one
// colour. `custom_themes` is gone from BOTH faces of settings; the claims are
// not. Every one of them lives here now, on the door it actually happens
// through: listThemes / getTheme / putTheme / deleteTheme.
//
// 🔴 The list row is NOT a bundle (see api/dtoParity.ts): `GET /api/themes`
// answers id + name only, and anything wanting colours / wording / images has
// to read the ONE theme. That asymmetry is asserted here so a mock that got
// generous cannot make a component pass against a wire that does not exist.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { ApiError } from "./errors";
import { MAX_WORDING_ENTRIES_PER_LANG } from "../lib/themeBundleCore";

const MIDNIGHT = {
  id: "midnight",
  name: "Midnight",
  colors: { "--color-bg": "#101018", "--color-accent": "transparent" },
};

/** A valid tiny PNG data URI (magic-checked by the shared image gate). */
function png(tag: number): string {
  return (
    "data:image/png;base64," +
    btoa(
      String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, tag)
    )
  );
}

describe("mock themes — the theme resource (T-83ef)", () => {
  beforeEach(() => __resetMock());

  it("starts with no saved themes", async () => {
    expect(await mockApi.listThemes()).toEqual([]);
  });

  it("saves a legal bundle, lists it as id+name ONLY, and reads it back in full", async () => {
    const receipt = await mockApi.putTheme(MIDNIGHT);
    expect(receipt.id).toBe("midnight");
    expect(receipt.created).toBe(true);
    expect(receipt.orderIdx).toBe(0);

    const list = await mockApi.listThemes();
    expect(list).toEqual([{ id: "midnight", name: "Midnight" }]);
    // The list row carries NO bundle fields — a caller that read `colors` off a
    // row here would find it undefined against the real server.
    expect(Object.keys(list[0]).sort()).toEqual(["id", "name"]);

    const full = await mockApi.getTheme("midnight");
    expect(full.colors["--color-bg"]).toBe("#101018");
    expect(full.name).toBe("Midnight");
  });

  it("replacing a theme keeps its position; creating appends", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.putTheme({ id: "sunrise", name: "Sunrise", colors: { "--color-bg": "#fff" } });
    const replaced = await mockApi.putTheme({
      ...MIDNIGHT,
      name: "Midnight II",
      colors: { "--color-bg": "#000000" },
    });
    expect(replaced.created).toBe(false);
    expect(replaced.orderIdx).toBe(0);
    expect((await mockApi.listThemes()).map((x) => x.id)).toEqual([
      "midnight",
      "sunrise",
    ]);
    expect((await mockApi.getTheme("midnight")).name).toBe("Midnight II");
  });

  it("404s a getTheme / deleteTheme for a theme that does not exist", async () => {
    await expect(mockApi.getTheme("ghost")).rejects.toBeInstanceOf(ApiError);
    await expect(mockApi.deleteTheme("ghost")).rejects.toBeInstanceOf(ApiError);
  });

  it("422s a bundle with a non-whitelisted token, writing nothing", async () => {
    await expect(
      mockApi.putTheme({ id: "x", name: "X", colors: { "--color-bogus": "#fff" } })
    ).rejects.toBeInstanceOf(ApiError);
    expect(await mockApi.listThemes()).toEqual([]);
  });

  it("422s a bundle with an illegal colour value, writing nothing", async () => {
    await expect(
      mockApi.putTheme({ id: "x", name: "X", colors: { "--color-bg": "url(evil)" } })
    ).rejects.toBeInstanceOf(ApiError);
    expect(await mockApi.listThemes()).toEqual([]);
  });

  it("saves a legal wording overlay and reads it back durably", async () => {
    await mockApi.putTheme({
      id: "worded",
      name: "Worded",
      colors: { "--color-bg": "#101018" },
      wording: {
        // profile.themeOffice is no longer whitelisted (T-081b): the write
        // still succeeds, and the code is dropped rather than stored.
        zh: { "nav.tasks": "待辦", "profile.themeOffice": "精靈村" },
        en: { "nav.office": "Office Mode" },
      },
    });
    const again = await mockApi.getTheme("worded");
    expect(again.wording?.zh["nav.tasks"]).toBe("待辦");
    expect(again.wording?.zh["profile.themeOffice"]).toBeUndefined();
    expect(again.wording?.en["nav.office"]).toBe("Office Mode");
  });

  it("saves an avatar overlay and reads it back durably", async () => {
    const pngAvatar = png(0x01);
    await mockApi.putTheme({
      id: "faced",
      name: "Faced",
      colors: { "--color-bg": "#101018" },
      avatars: { outsource: pngAvatar },
    });
    // The regression: a fresh read must still carry avatars — the read-back
    // mapper dropping this field was the "avatar lost after refresh" defect.
    const again = await mockApi.getTheme("faced");
    expect(again.avatars?.outsource).toBe(pngAvatar);
  });

  it("saves logo + nav-icon overlays and reads them back durably", async () => {
    const pngLogo = png(0x02);
    const pngIcon = png(0x03);
    await mockApi.putTheme({
      id: "brand",
      name: "Brand",
      colors: { "--color-bg": "#101018" },
      logo: pngLogo,
      navIcons: { office: pngIcon, tasks: pngIcon },
    });
    // The regression: a fresh read must still carry logo + navIcons — the
    // read-back mapper dropping these two fields was the "uploaded logo / nav
    // icons lost after refresh and missing from export" defect T-ea81 shipped.
    // Avatars were copied; these two were not.
    const again = await mockApi.getTheme("brand");
    expect(again.logo).toBe(pngLogo);
    expect(again.navIcons?.office).toBe(pngIcon);
    expect(again.navIcons?.tasks).toBe(pngIcon);
  });

  it("422s an illegal wording overlay, writing nothing", async () => {
    const overCap: Record<string, string> = {};
    for (let i = 0; i <= MAX_WORDING_ENTRIES_PER_LANG; i++) overCap[`junk.key.${i}`] = "x";
    const bad: Record<string, Record<string, string>>[] = [
      { xian: { "nav.tasks": "仙" } }, // language not in {zh,en}
      { zh: overCap }, // over the per-language cap, counted on RAW entries
      { zh: { "nav.tasks": "字".repeat(201) } }, // over the 200-rune cap
      { zh: { "nav.tasks": "a\nb" } }, // control character (newline)
      { zh: { "nav.tasks": "   " } }, // empty after trimming
    ];
    for (const wording of bad) {
      await expect(
        mockApi.putTheme({ id: "w2", name: "W2", colors: { "--color-bg": "#111" }, wording })
      ).rejects.toBeInstanceOf(ApiError);
    }
    expect(await mockApi.listThemes()).toEqual([]);
  });

  it("resets display_theme to \"\" when the ACTIVE theme is deleted, and says so", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.patchServerSettings({ displayTheme: "midnight" });

    const result = await mockApi.deleteTheme("midnight");
    expect(result.deleted).toBe(true);
    // The flag IS how the caller learns its theme changed without re-reading
    // settings — the coupling the old whole-array settings write performed.
    expect(result.displayThemeReset).toBe(true);
    expect((await mockApi.getServerSettings()).displayTheme).toBe("");
    expect(await mockApi.listThemes()).toEqual([]);
  });

  it("leaves display_theme alone when a NON-active theme is deleted", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.putTheme({ id: "sunrise", name: "Sunrise", colors: { "--color-bg": "#fff" } });
    await mockApi.patchServerSettings({ displayTheme: "midnight" });

    const result = await mockApi.deleteTheme("sunrise");
    expect(result.displayThemeReset).toBe(false);
    expect((await mockApi.getServerSettings()).displayTheme).toBe("midnight");
  });
});
