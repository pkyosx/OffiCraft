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
      displayTheme: "xian",
      displayLanguage: "en",
    });
    expect(s.displayTheme).toBe("xian");
    expect(s.displayLanguage).toBe("en");
    const again = await mockApi.getServerSettings();
    expect(again.displayTheme).toBe("xian");
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
    await mockApi.patchServerSettings({ displayTheme: "xian" });
    const cleared = await mockApi.patchServerSettings({ displayTheme: "" });
    expect(cleared.displayTheme).toBe("");
  });
});
