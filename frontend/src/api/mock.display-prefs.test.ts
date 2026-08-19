// Mock adapter parity for the dual-layer display prefs (settings; T-0b41-p2):
// "" out of the box, a PATCH persists within the session (so a reload reads it
// back), an out-of-enum value 422s (writing nothing), and an empty value clears
// it. Mirrors the owner-nickname (owner_name) mock parity. Like owner_name these
// never enter the agent read path, so there is no global-context leg.
//
// T-83ef: `custom_themes` is gone from BOTH faces of /api/settings — the bundles
// are their own resource now. Every claim about SAVING / READING / DELETING a
// bundle moved verbatim to `mock.themes.test.ts`; what stays here is what
// settings still owns: WHICH theme is active, and validating that id against the
// theme store.

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

  it("lets display_theme point at a SAVED theme id (settings validates against the theme store)", async () => {
    // The bundles now live in their own resource (T-83ef) — settings only holds
    // WHICH one is active, and validates that id against that store.
    await mockApi.putTheme({
      id: "midnight",
      name: "Midnight",
      colors: { "--color-bg": "#101018", "--color-accent": "transparent" },
    });
    const s = await mockApi.patchServerSettings({ displayTheme: "midnight" });
    expect(s.displayTheme).toBe("midnight");
    expect((await mockApi.getServerSettings()).displayTheme).toBe("midnight");
  });

  it("422s a display_theme pointing at a non-existent custom id", async () => {
    await expect(
      mockApi.patchServerSettings({ displayTheme: "ghost" })
    ).rejects.toBeInstanceOf(ApiError);
  });

  it("defaults display_wide to false (the shipped narrow column)", async () => {
    const s = await mockApi.getServerSettings();
    expect(s.displayWide).toBe(false);
  });

  it("PATCHes display_wide both ways and reads it back durably", async () => {
    let s = await mockApi.patchServerSettings({ displayWide: true });
    expect(s.displayWide).toBe(true);
    expect((await mockApi.getServerSettings()).displayWide).toBe(true);

    s = await mockApi.patchServerSettings({ displayWide: false });
    expect(s.displayWide).toBe(false);
    expect((await mockApi.getServerSettings()).displayWide).toBe(false);
  });

  it("leaves display_wide alone when the patch omits it (PATCH semantics)", async () => {
    await mockApi.patchServerSettings({ displayWide: true });
    const s = await mockApi.patchServerSettings({ displayLanguage: "en" });
    expect(s.displayWide).toBe(true);
  });
});
