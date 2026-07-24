// Mock adapter parity for the dual-layer display prefs (settings; T-0b41-p2):
// "" out of the box, a PATCH persists within the session (so a reload reads it
// back), an out-of-enum value 422s (writing nothing), and an empty value clears
// it. Mirrors the owner-nickname (owner_name) mock parity. Like owner_name these
// never enter the agent read path, so there is no global-context leg.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { ApiError } from "./errors";

describe("mock settings — display prefs (display_theme / display_language)", () => {
  beforeEach(() => __resetMock());

  it("defaults both display prefs to \"\"", async () => {
    const s = await mockApi.getServerSettings();
    expect(s.displayTheme).toBe("");
    expect(s.displayLanguage).toBe("");
  });

  it("PATCHes display prefs and reads them back durably", async () => {
    const s = await mockApi.patchServerSettings({
      displayTheme: "office",
      displayLanguage: "en",
    });
    expect(s.displayTheme).toBe("office");
    expect(s.displayLanguage).toBe("en");
    const again = await mockApi.getServerSettings();
    expect(again.displayTheme).toBe("office");
    expect(again.displayLanguage).toBe("en");
  });

  it("422s an out-of-enum display_theme, writing nothing", async () => {
    await expect(
      mockApi.patchServerSettings({ displayTheme: "neon" })
    ).rejects.toBeInstanceOf(ApiError);
    const s = await mockApi.getServerSettings();
    expect(s.displayTheme).toBe(""); // unchanged
  });

  it("422s an out-of-enum display_language, writing nothing", async () => {
    await expect(
      mockApi.patchServerSettings({ displayLanguage: "fr" })
    ).rejects.toBeInstanceOf(ApiError);
    const s = await mockApi.getServerSettings();
    expect(s.displayLanguage).toBe(""); // unchanged
  });

  it("clears a display pref back to \"\" on an empty patch value", async () => {
    await mockApi.patchServerSettings({ displayTheme: "office" });
    const cleared = await mockApi.patchServerSettings({ displayTheme: "" });
    expect(cleared.displayTheme).toBe("");
  });

  it("defaults custom_themes to an empty array", async () => {
    const s = await mockApi.getServerSettings();
    expect(s.customThemes).toEqual([]);
  });

  it("saves a legal custom theme bundle and lets display_theme point at its id", async () => {
    const s = await mockApi.patchServerSettings({
      customThemes: [
        {
          id: "midnight",
          name: "Midnight",
          colors: { "--color-bg": "#101018", "--color-accent": "transparent" },
        },
      ],
      displayTheme: "midnight",
    });
    expect(s.customThemes).toHaveLength(1);
    expect(s.displayTheme).toBe("midnight");
    const again = await mockApi.getServerSettings();
    expect(again.customThemes[0].id).toBe("midnight");
  });

  it("422s a bundle with a non-whitelisted token, writing nothing", async () => {
    await expect(
      mockApi.patchServerSettings({
        customThemes: [{ id: "x", name: "X", colors: { "--color-bogus": "#fff" } }],
      })
    ).rejects.toBeInstanceOf(ApiError);
    const s = await mockApi.getServerSettings();
    expect(s.customThemes).toEqual([]);
  });

  it("422s a bundle with an illegal colour value, writing nothing", async () => {
    await expect(
      mockApi.patchServerSettings({
        customThemes: [{ id: "x", name: "X", colors: { "--color-bg": "url(evil)" } }],
      })
    ).rejects.toBeInstanceOf(ApiError);
    const s = await mockApi.getServerSettings();
    expect(s.customThemes).toEqual([]);
  });

  it("saves a legal wording overlay and reads it back durably", async () => {
    const s = await mockApi.patchServerSettings({
      customThemes: [
        {
          id: "worded",
          name: "Worded",
          colors: { "--color-bg": "#101018" },
          wording: {
            zh: { "nav.tasks": "待辦" },
            en: { "profile.themeOffice": "Office Mode" },
          },
        },
      ],
    });
    expect(s.customThemes[0].wording?.zh["nav.tasks"]).toBe("待辦");
    const again = await mockApi.getServerSettings();
    expect(again.customThemes[0].wording?.en["profile.themeOffice"]).toBe(
      "Office Mode"
    );
  });

  it("saves an avatar overlay and reads it back durably", async () => {
    // A valid tiny PNG data URI (magic-checked by isValidAvatarValue).
    const pngAvatar =
      "data:image/png;base64," +
      btoa(
        String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01)
      );
    const s = await mockApi.patchServerSettings({
      customThemes: [
        {
          id: "faced",
          name: "Faced",
          colors: { "--color-bg": "#101018" },
          avatars: { outsource: pngAvatar },
        },
      ],
    });
    expect(s.customThemes[0].avatars?.outsource).toBe(pngAvatar);
    // The regression: a fresh read must still carry avatars — the read-back
    // mapper dropping this field was the "avatar lost after refresh" defect.
    const again = await mockApi.getServerSettings();
    expect(again.customThemes[0].avatars?.outsource).toBe(pngAvatar);
  });

  it("422s an illegal wording overlay, writing nothing", async () => {
    const bad: Record<string, Record<string, string>>[] = [
      { zh: { "not.a.real.key": "x" } }, // non-whitelisted code
      { xian: { "nav.tasks": "仙" } }, // language not in {zh,en}
      { zh: { "nav.tasks": "字".repeat(201) } }, // over the 200-rune cap
      { zh: { "nav.tasks": "a\nb" } }, // control character (newline)
      { zh: { "nav.tasks": "   " } }, // empty after trimming
    ];
    for (const wording of bad) {
      await expect(
        mockApi.patchServerSettings({
          customThemes: [
            { id: "w2", name: "W2", colors: { "--color-bg": "#111" }, wording },
          ],
        })
      ).rejects.toBeInstanceOf(ApiError);
    }
    const s = await mockApi.getServerSettings();
    expect(s.customThemes).toEqual([]);
  });

  it("422s a display_theme pointing at a non-existent custom id", async () => {
    await expect(
      mockApi.patchServerSettings({ displayTheme: "ghost" })
    ).rejects.toBeInstanceOf(ApiError);
  });

  it("resets display_theme to \"\" when the active custom theme is deleted", async () => {
    await mockApi.patchServerSettings({
      customThemes: [{ id: "midnight", name: "Midnight", colors: { "--color-bg": "#101018" } }],
      displayTheme: "midnight",
    });
    const after = await mockApi.patchServerSettings({ customThemes: [] });
    expect(after.displayTheme).toBe("");
  });
});
