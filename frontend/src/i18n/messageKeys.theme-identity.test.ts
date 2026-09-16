// messageKeys.theme-identity.test.ts — T-081b §6.
//
// A theme bundle's `wording` overlay may re-word the product; it may NOT rename
// a THEME. A theme's name is its identity: the row in the theme picker, the
// theme-settings heading, and the `name` written into the file on export. A pack
// that could re-word it would leave two identically named rows in the picker and
// no way back to the shipped one (owner report 2026-07-27). The whitelist
// generator (scripts/gen-message-keys.mjs) skips the `themeIdentity` subtree.
//
// The 內建 / 自訂 labels are ordinary wording and ARE overridable (owner: 「我們
// 只要確定主題名稱不會隨著主題改變就好」).

import { describe, it, expect } from "vitest";
import { MESSAGE_KEYS } from "./messageKeys.generated";
import { zh } from "./locales/zh";
import { en } from "./locales/en";

const keys = new Set(MESSAGE_KEYS);

describe("MESSAGE_KEYS", () => {
  it("does not let a theme bundle rename a theme", () => {
    for (const name of Object.keys(zh.themeIdentity)) {
      expect(
        keys.has(`themeIdentity.${name}`),
        `themeIdentity.${name} is a THEME'S OWN NAME — a theme pack that can ` +
          `re-word it renames the built-in theme and the owner loses the way back`
      ).toBe(false);
    }
  });

  it("does let a theme bundle re-word the 內建 / 自訂 labels", () => {
    // owner:「這是大家自己用的,自己要怎麼搞我們不用特別管」.
    for (const name of Object.keys(zh.themeMarkers)) {
      expect(keys.has(`themeMarkers.${name}`), `themeMarkers.${name}`).toBe(true);
    }
  });

  it("keeps the 內建 / 自訂 labels present and non-empty in both languages", () => {
    for (const dict of [zh, en]) {
      for (const value of Object.values(dict.themeMarkers)) {
        expect(value.length).toBeGreaterThan(0);
      }
    }
  });

  it("still lets a theme bundle rename the PLACE", () => {
    // nav.office is the 辦公室 place name on the nav tab — re-wording the world
    // is the whole point of a theme pack, so this one must stay reachable.
    expect(keys.has("nav.office")).toBe(true);
  });

  it("keeps the theme-identity names present and non-empty in both languages", () => {
    // Excluding them from the overlay must not have made them disappear: the
    // picker still has to render a name for the built-in theme.
    for (const dict of [zh, en]) {
      for (const value of Object.values(dict.themeIdentity)) {
        expect(value.length).toBeGreaterThan(0);
      }
    }
  });
});
