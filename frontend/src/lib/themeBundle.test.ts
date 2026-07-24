// Unit coverage for the client theme-bundle validator (the twin of the server
// grammar in server/ocserverd/theme_bundle.go). The colour-value grammar is the
// security boundary, so the illegal-value table is the load-bearing case.

import { describe, it, expect } from "vitest";
import {
  isValidColorValue,
  isValidFontValue,
  isValidAvatarValue,
  validateAvatars,
  validateLogo,
  validateNavIcons,
  validateThemeBundle,
  validateThemeBundles,
  validateWording,
  validateFonts,
  isValidDisplayTheme,
  MAX_AVATAR_BYTES,
} from "./themeBundle";
import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import { SAFE_FONT_FAMILIES } from "../styles/themeFonts.generated";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";

const aFontStack = SAFE_FONT_FAMILIES[0].stack;

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

  it("accepts a bundle with a legal fonts overlay and rejects an illegal one", () => {
    expect(
      validateThemeBundle({ ...ok, fonts: { "--font-sans": aFontStack } })
    ).toBeNull();
    expect(
      validateThemeBundle({ ...ok, fonts: { "--font-bogus": aFontStack } })
    ).toMatch(/not a theme font token/);
    expect(
      validateThemeBundle({ ...ok, fonts: { "--font-sans": "Comic Sans, sans-serif" } })
    ).toMatch(/invalid font value/);
  });
});

describe("isValidFontValue", () => {
  it("accepts every curated safe family stack", () => {
    for (const f of SAFE_FONT_FAMILIES) {
      expect(isValidFontValue(f.stack)).toBe(true);
    }
  });

  it("rejects arbitrary strings and CSS/url/@font-face injection", () => {
    for (const v of [
      "",
      "Arial", // not on the allowlist
      "Comic Sans MS, sans-serif", // plausible but not curated
      "sans-serif", // bare generic, not a curated stack
      'url("https://evil/x.woff2")',
      "@font-face{font-family:x;src:url(y)}",
      "system-ui;}",
      "system-ui, <script>",
      "var(--x)",
      "javascript:alert(1)",
      SAFE_FONT_FAMILIES[0].stack + " ", // trailing space defeats exact match
      "f".repeat(200), // over the length cap
    ]) {
      expect(isValidFontValue(v)).toBe(false);
    }
  });
});

describe("validateFonts", () => {
  it("accepts undefined (optional) and a legal token→stack overlay", () => {
    expect(validateFonts(undefined)).toBeNull();
    expect(
      validateFonts({ "--font-sans": aFontStack, "--font-title": aFontStack })
    ).toBeNull();
  });

  it("rejects a non-object, an unknown token, and an off-allowlist value", () => {
    expect(validateFonts([])).toMatch(/must be an object/);
    expect(validateFonts({ "--color-bg": aFontStack })).toMatch(
      /not a theme font token/
    );
    expect(validateFonts({ "--font-sans": "url(https://evil)" })).toMatch(
      /invalid font value/
    );
    expect(validateFonts({ "--font-title": "Times New Roman" })).toMatch(
      /invalid font value/
    );
  });
});

// ── avatar images (T-16a1 P5) — the security boundary is the image VALUE ──
function b64(bytes: number[]): string {
  return btoa(String.fromCharCode(...bytes));
}
function avatarURI(mime: string, bytes: number[]): string {
  return `data:${mime};base64,${b64(bytes)}`;
}
const PNG_MAGIC = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01];
const JPEG_MAGIC = [0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10];
const WEBP_MAGIC = [0x52, 0x49, 0x46, 0x46, 0x10, 0, 0, 0, 0x57, 0x45, 0x42, 0x50, 0];
const okPng = avatarURI("image/png", PNG_MAGIC);
const okJpeg = avatarURI("image/jpeg", JPEG_MAGIC);
const okWebp = avatarURI("image/webp", WEBP_MAGIC);

describe("isValidAvatarValue", () => {
  it("accepts a valid PNG / JPEG / WEBP base64 data URI", () => {
    expect(isValidAvatarValue(okPng)).toBe(true);
    expect(isValidAvatarValue(okJpeg)).toBe(true);
    expect(isValidAvatarValue(okWebp)).toBe(true);
  });

  it("rejects SVG, foreign schemes, bad base64, magic mismatch, oversize, non-data-URI", () => {
    const oversize = avatarURI(
      "image/png",
      PNG_MAGIC.concat(new Array(MAX_AVATAR_BYTES).fill(0))
    );
    for (const v of [
      "", // empty
      "https://evil/x.png", // not a data URI
      "javascript:alert(1)", // foreign scheme
      "data:text/html,<script>alert(1)</script>", // not base64, not image
      avatarURI("image/svg+xml", [0x3c, 0x73, 0x76, 0x67]), // SVG rejected outright
      avatarURI("text/html", [0x3c, 0x73]), // non-image mime
      avatarURI("image/gif", [0x47, 0x49, 0x46, 0x38]), // gif not whitelisted
      "data:image/png;base64,!!!!notbase64!!!!", // bad base64
      avatarURI("image/png", JPEG_MAGIC), // declares png, carries jpeg bytes
      avatarURI("image/png", [0x3c, 0x73, 0x76, 0x67, 0x20]), // png claim, svg payload → magic fail
      avatarURI("image/jpeg", PNG_MAGIC), // jpeg claim, png bytes
      "data:image/png,iVBOR", // missing ;base64
      oversize, // decoded bytes over the 64 KiB cap
    ]) {
      expect(isValidAvatarValue(v), `must reject: ${v.slice(0, 40)}`).toBe(false);
    }
  });
});

describe("validateAvatars", () => {
  it("accepts undefined (optional) and a legal member/outsource/owner/assistant overlay", () => {
    expect(validateAvatars(undefined)).toBeNull();
    expect(
      validateAvatars({ member: okPng, outsource: okWebp, owner: okJpeg, assistant: okPng })
    ).toBeNull();
  });

  it("rejects a non-object, an unknown kind, and an invalid image", () => {
    expect(validateAvatars([])).toMatch(/must be an object/);
    expect(validateAvatars({ boss: okPng })).toMatch(
      /not allowed \(only member, outsource, owner, assistant\)/
    );
    expect(
      validateAvatars({ member: avatarURI("image/svg+xml", [0x3c]) })
    ).toMatch(/not a valid image/);
  });

  it("flows through validateThemeBundle", () => {
    const good = {
      id: "midnight",
      name: "Midnight",
      colors: { [aToken]: "#111111" },
      avatars: { owner: okPng, assistant: okWebp },
    };
    expect(validateThemeBundle(good)).toBeNull();
    const bad = { ...good, avatars: { member: avatarURI("image/svg+xml", [0x3c]) } };
    expect(validateThemeBundle(bad)).toMatch(/not a valid image/);
  });
});

describe("validateLogo", () => {
  it("accepts undefined/null (optional) and a legal raster image", () => {
    expect(validateLogo(undefined)).toBeNull();
    expect(validateLogo(null)).toBeNull();
    expect(validateLogo(okPng)).toBeNull();
  });

  it("rejects an SVG and any non-image value", () => {
    expect(validateLogo(avatarURI("image/svg+xml", [0x3c]))).toMatch(
      /logo is not a valid image/
    );
    expect(validateLogo("https://evil/x.png")).toMatch(/logo is not a valid image/);
    expect(validateLogo(42)).toMatch(/logo is not a valid image/);
  });

  it("flows through validateThemeBundle", () => {
    const good = { id: "midnight", name: "Midnight", colors: { [aToken]: "#111111" }, logo: okPng };
    expect(validateThemeBundle(good)).toBeNull();
    expect(
      validateThemeBundle({ ...good, logo: avatarURI("image/svg+xml", [0x3c]) })
    ).toMatch(/logo is not a valid image/);
  });
});

describe("validateNavIcons", () => {
  it("accepts undefined (optional) and the five legal nav-tab keys", () => {
    expect(validateNavIcons(undefined)).toBeNull();
    expect(
      validateNavIcons({
        office: okPng,
        replies: okJpeg,
        tasks: okWebp,
        monitor: okPng,
        guide: okJpeg,
      })
    ).toBeNull();
  });

  it("rejects a non-object, an unknown key, and an image that fails the gate", () => {
    expect(validateNavIcons([])).toMatch(/must be an object/);
    expect(validateNavIcons({ settings: okPng })).toMatch(
      /nav icon key "settings" is not allowed \(only office, replies, tasks, monitor, guide\)/
    );
    expect(
      validateNavIcons({ office: avatarURI("image/svg+xml", [0x3c]) })
    ).toMatch(/not a valid image/);
  });

  it("flows through validateThemeBundle", () => {
    const good = {
      id: "midnight",
      name: "Midnight",
      colors: { [aToken]: "#111111" },
      navIcons: { tasks: okPng },
    };
    expect(validateThemeBundle(good)).toBeNull();
    expect(
      validateThemeBundle({ ...good, navIcons: { nope: okPng } })
    ).toMatch(/not allowed/);
  });
});

describe("validateThemeBundle backward compatibility", () => {
  it("accepts a legacy member/outsource-only bundle with no logo/navIcons", () => {
    expect(
      validateThemeBundle({
        id: "legacy",
        name: "Legacy",
        colors: { [aToken]: "#101018" },
        avatars: { member: okPng, outsource: okWebp },
      })
    ).toBeNull();
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
