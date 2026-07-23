// Unit coverage for the client theme-bundle validator (the twin of the server
// grammar in server/ocserverd/theme_bundle.go). The colour-value grammar is the
// security boundary, so the illegal-value table is the load-bearing case.

import { describe, it, expect } from "vitest";
import {
  isValidColorValue,
  validateThemeBundle,
  validateThemeBundles,
  validateWording,
  isValidDisplayTheme,
} from "./themeBundle";
import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";

const aKey = MESSAGE_KEYS[0];

const aToken = THEME_COLOR_TOKENS[0];

describe("isValidColorValue", () => {
  it("accepts concrete hex / rgb / rgba / hsl / transparent", () => {
    for (const v of [
      "#fff",
      "#ffff",
      "#101018",
      "#101018ff",
      "rgb(1, 2, 3)",
      "rgba(1, 2, 3, 0.5)",
      "rgba(1 2 3 / 40%)",
      "hsl(120deg, 50%, 40%)",
      "hsla(120, 50%, 40%, 0.5)",
      "transparent",
    ]) {
      expect(isValidColorValue(v)).toBe(true);
    }
  });

  it("rejects CSS-injection and non-concrete values", () => {
    for (const v of [
      "",
      "url(https://evil)",
      "red;}",
      "<script>",
      "expression(1)",
      "var(--x)",
      "color-mix(in srgb, red, blue)",
      "#fff;background:url(x)",
      "red", // a named colour other than transparent
      "f".repeat(70), // over the 64-char cap
    ]) {
      expect(isValidColorValue(v)).toBe(false);
    }
  });
});

describe("validateThemeBundle", () => {
  const ok = { id: "midnight", name: "Midnight", colors: { [aToken]: "#101018" } };

  it("accepts a well-formed bundle", () => {
    expect(validateThemeBundle(ok)).toBeNull();
  });

  it("rejects a bad id, a reserved id, an empty name, and an unknown token", () => {
    expect(validateThemeBundle({ ...ok, id: "Bad Id" })).toMatch(/id must match/);
    expect(validateThemeBundle({ ...ok, id: "office" })).toMatch(/reserved/);
    expect(validateThemeBundle({ ...ok, name: "  " })).toMatch(/name must be/);
    expect(
      validateThemeBundle({ ...ok, colors: { "--color-bogus": "#fff" } })
    ).toMatch(/not a theme colour token/);
    expect(validateThemeBundle({ ...ok, colors: {} })).toMatch(/colors must hold/);
  });

  it("accepts a bundle with a legal wording overlay and rejects an illegal one", () => {
    expect(
      validateThemeBundle({ ...ok, wording: { zh: { [aKey]: "覆蓋" } } })
    ).toBeNull();
    expect(
      validateThemeBundle({ ...ok, wording: { fr: { [aKey]: "x" } } })
    ).toMatch(/language/);
  });
});

describe("validateWording", () => {
  it("accepts undefined (optional) and a legal zh/en overlay", () => {
    expect(validateWording(undefined)).toBeNull();
    expect(validateWording({ zh: { [aKey]: "文字" }, en: { [aKey]: "text" } })).toBeNull();
  });

  it("rejects a bad language, an unknown code, and illegal values", () => {
    expect(validateWording({ xian: { [aKey]: "仙" } })).toMatch(/language/);
    expect(validateWording({ zh: { "not.a.key": "x" } })).toMatch(/message code/);
    expect(validateWording({ zh: { [aKey]: "a\nb" } })).toMatch(/control/);
    expect(validateWording({ zh: { [aKey]: "   " } })).toMatch(/1\.\.200/);
    expect(validateWording({ zh: { [aKey]: "字".repeat(201) } })).toMatch(/1\.\.200/);
  });
});

describe("validateThemeBundles", () => {
  it("rejects a non-array and duplicate ids", () => {
    expect(validateThemeBundles({})).toMatch(/must be an array/);
    const b = { id: "dup", name: "D", colors: { [aToken]: "#111111" } };
    expect(validateThemeBundles([b, b])).toMatch(/duplicate id/);
  });

  it("accepts an empty array and a unique set", () => {
    expect(validateThemeBundles([])).toBeNull();
    expect(
      validateThemeBundles([
        { id: "aa", name: "A", colors: { [aToken]: "#111111" } },
        { id: "bb", name: "B", colors: { [aToken]: "#222222" } },
      ])
    ).toBeNull();
  });
});

describe("isValidDisplayTheme", () => {
  it("admits \"\", the office built-in, and an existing custom id only", () => {
    const ids = new Set(["midnight"]);
    expect(isValidDisplayTheme("", ids)).toBe(true);
    expect(isValidDisplayTheme("office", ids)).toBe(true);
    expect(isValidDisplayTheme("midnight", ids)).toBe(true);
    // "xian" is no longer a built-in — it is only admissible as a custom id.
    expect(isValidDisplayTheme("xian", ids)).toBe(false);
    expect(isValidDisplayTheme("xian", new Set(["xian"]))).toBe(true);
    expect(isValidDisplayTheme("ghost", ids)).toBe(false);
  });
});
