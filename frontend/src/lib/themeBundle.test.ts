// Unit coverage for the client theme-bundle validator (the twin of the server
// grammar in server/ocserverd/theme_bundle.go). The colour-value grammar is the
// security boundary, so the illegal-value table is the load-bearing case.

import { describe, it, expect } from "vitest";
import {
  isValidColorValue,
  isValidFontValue,
  isValidAvatarValue,
  validateAvatars,
  validateAvatarPools,
  validateLogo,
  validateNavIcons,
  validateBackgrounds,
  validateBackgroundModes,
  validateThemeBundle,
  validateThemeBundles,
  validateWording,
  trimThemeName,
  validateFonts,
  isValidDisplayTheme,
  MAX_AVATAR_BYTES,
  MAX_BACKGROUND_BYTES,
  MAX_WORDING_ENTRIES_PER_LANG,
} from "./themeBundle";
import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import { SAFE_FONT_FAMILIES } from "../styles/themeFonts.generated";
import {
  MESSAGE_KEYS,
} from "../i18n/messageKeys.generated";

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

  it("rejects a name carrying control, formatting, private-use, surrogate or line/paragraph separator characters", () => {
    // Written as escapes on purpose: these characters are INVISIBLE, and a
    // reviewer must be able to see which one each case is testing.
    for (const name of [
      "Mid\u0000night", // NUL
      "Mid\u000Anight", // newline
      "Mid\u007Fnight", // DEL
      "Mid\u009Fnight", // C1 control
      "\u202EMidnight", // RIGHT-TO-LEFT OVERRIDE
      "Mid\u202Dnight", // LEFT-TO-RIGHT OVERRIDE
      "Mid\u202Anight", // LEFT-TO-RIGHT EMBEDDING
      "Mid\u2066night", // LEFT-TO-RIGHT ISOLATE
      "Mid\u2069night", // POP DIRECTIONAL ISOLATE
      "Mid\u200Enight", // LEFT-TO-RIGHT MARK
      "Mid\u200Fnight", // RIGHT-TO-LEFT MARK
      // ZERO-WIDTH class (T-081b review round 3, BLOCKER-1). U+FEFF is the
      // load-bearing one: it is the ONE codepoint String.prototype.trim() strips
      // and Go's strings.TrimSpace does not, so while it was left to the trim the
      // AUTHORITATIVE server accepted 「\uFEFF辦公室」 and only this client rejected
      // it. The twin table lives in server/ocserverd/theme_bundle_test.go.
      "\uFEFF辦公室", // BOM prefix — renders as 「辦公室」
      "辦公室\uFEFF", // BOM suffix
      "\uFEFFOffice", // BOM prefix, en spelling
      "Office\uFEFF", // BOM suffix, en spelling
      "辦\u200B公室", // ZERO WIDTH SPACE
      "Off\u200Bice",
      "Off\u200Cice", // ZERO WIDTH NON-JOINER
      "Off\u200Dice", // ZERO WIDTH JOINER
      "Office\u2060", // WORD JOINER
      "Off\u061Cice", // ARABIC LETTER MARK (a bidi char the first list missed)
      // ── round 4, SHOULD-C: the members of the SAME categories the round-3
      // codepoint list never thought of. Listing codepoints is what missed them;
      // the rule is now the CATEGORY (Cc/Cf/Co/Cs/Zl/Zp).
      "Off\u00ADice", // SOFT HYPHEN (Cf) — renders as 「Office」
      "Off\u180Eice", // MONGOLIAN VOWEL SEPARATOR (Cf)
      "Office\u{E0041}", // TAG LATIN CAPITAL A (Cf) — the classic invisible payload
      "Office\uE000", // PRIVATE USE (Co) — renders as whatever the font decides
      "Mid\u2028night", // LINE SEPARATOR (Zl)
      "Mid\u2029night", // PARAGRAPH SEPARATOR (Zp)
    ]) {
      expect(
        validateThemeBundle({ ...ok, name }),
        JSON.stringify(name)
      ).toMatch(
        /control, formatting, private-use, surrogate or line\/paragraph separator/
      );
    }
    // Zs is NOT in that set — every space separator is NORMALISED to U+0020
    // first (round 4 recheck, SHOULD-3), so these are ordinary names that simply
    // lose their padding. Round 8 removed the reserved-name rule that used to
    // catch them on the way out; what the assertion is about now is the trim —
    // the accepted name must be the normalised one, not the padded bytes.
    for (const name of [
      "\u00A0Office\u00A0", // NO-BREAK SPACE (Zs) — renders as 「Office」
      "\u3000辦公室\u3000", // IDEOGRAPHIC SPACE (Zs) — renders as 「辦公室」
      "\u1680Office", // OGHAM SPACE MARK (Zs) — blank in most fonts
    ]) {
      expect(validateThemeBundle({ ...ok, name }), JSON.stringify(name)).toBeNull();
      expect(trimThemeName(name), JSON.stringify(name)).not.toMatch(
        /[\u00A0\u3000\u1680]/
      );
    }
    // …and a name that is nothing BUT spaces has no name left after the
    // normalise + trim, in every Zs spelling.
    for (const name of ["\u3000", "\u00A0", " \u3000 ", "\u1680\u2000"]) {
      expect(validateThemeBundle({ ...ok, name }), JSON.stringify(name)).toMatch(
        /name must be 1\.\./
      );
    }
  });

  it("accepts a name that matches the built-in theme's display name", () => {
    // Until round 8 these were rejected: a pack calling itself 辦公室 put a second
    // 辦公室 row in the picker. The owner dropped the rule — 「這是大家自己用的,自己
    // 要怎麼搞我們不用特別管」 — so a duplicate display name is the user's own
    // business now. The built-in's OWN name is what still cannot move: it comes
    // from the non-overridable themeIdentity subtree, so the shipped row keeps
    // saying 辦公室 whatever a pack calls itself. Only the NAME is free — the id
    // stays reserved (RESERVED_THEME_IDS).
    for (const name of ["辦公室", "Office", "office", "  OFFICE  ", " 辦公室 "]) {
      expect(validateThemeBundle({ ...ok, name }), name).toBeNull();
    }
    expect(validateThemeBundle({ ...ok, id: "office", name: "Whatever" })).toMatch(
      /is reserved for a built-in theme/
    );
  });

  it("accepts every legitimate name shape, including the new-theme default", () => {
    // The rule must not become a general-purpose name filter: CJK, emoji,
    // spaces and punctuation are all ordinary theme names. `新主題` / `New theme`
    // live in the SAME themeIdentity subtree as the built-in's name but are the
    // default name a NEW custom theme is created with — banning them would
    // reject the app's own create-theme flow.
    for (const name of [
      "精靈村",
      "深海の夜",
      "밤하늘",
      "🌙 Midnight 🌙",
      "Mid night — v2 (beta)!",
      "新主題",
      "New theme",
      "Officescape",
      "辦公室的夜",
      "OFF\u0130CE", // LATIN CAPITAL LETTER I WITH DOT ABOVE — folded by neither side
      // Not a general-purpose filter: the categories rejected above are the
      // invisible ones only. Scripts, emoji (variation selectors included —
      // U+FE0F is Mn and stays legal on purpose), ordinary spaces and
      // punctuation all pass (T-081b review round 4, SHOULD-C).
      "Heart \u2764\uFE0F", // VARIATION SELECTOR-16 — how an emoji name is spelled
      "سمة داكنة", // Arabic, ordinary letters + ASCII space
      "ערכת נושא כהה", // Hebrew, ordinary letters + ASCII space
      "Tiếng Việt", // combining marks (Mn) in an ordinary Latin name
      // Zs is NORMALISED, not rejected (round 4 recheck, SHOULD-3): a full-width
      // space is what a Chinese IME emits for the space bar, and a NO-BREAK
      // SPACE is what a paste out of a document carries. Both are ordinary
      // names, and rejecting them told the user nothing they could act on.
      "深海\u3000之夜", // IDEOGRAPHIC SPACE inside a perfectly legitimate name
      "深\u3000海\u3000之\u3000夜", // …several of them
      "Deep\u00A0Ocean", // NO-BREAK SPACE inside an ordinary name
      "\u3000深海之夜\u3000", // padded — but not with a built-in's name
    ]) {
      expect(validateThemeBundle({ ...ok, name }), name).toBeNull();
    }
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
  // Chunked: `String.fromCharCode(...bytes)` blows the argument limit
  // ("Maximum call stack size exceeded") once the background-cap cases push
  // half a megabyte through here.
  let s = "";
  for (let i = 0; i < bytes.length; i += 8192) {
    s += String.fromCharCode(...bytes.slice(i, i + 8192));
  }
  return btoa(s);
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
  it("accepts undefined and the canonical owner/assistant singleton overlay", () => {
    expect(validateAvatars(undefined)).toBeNull();
    expect(
      validateAvatars({ owner: okJpeg, assistant: okPng })
    ).toBeNull();
  });

  it("rejects a non-object, an unknown kind, and an invalid image", () => {
    expect(validateAvatars([])).toMatch(/must be an object/);
    expect(validateAvatars({ boss: okPng })).toMatch(
      /not allowed \(only owner, assistant\)/
    );
    expect(
      validateAvatars({ assistant: avatarURI("image/svg+xml", [0x3c]) })
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

describe("validateAvatarPools", () => {
  it("accepts ordered member/outsource pools and caps each at 12", () => {
    expect(validateAvatarPools({ member: [okPng], outsource: [okWebp] })).toBeNull();
    expect(validateAvatarPools({ member: Array(13).fill(okPng) })).toMatch(
      /at most 12/
    );
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

describe("validateBackgrounds", () => {
  it("accepts undefined (optional) and the canvas zone", () => {
    expect(validateBackgrounds(undefined)).toBeNull();
    expect(validateBackgrounds({ canvas: okPng })).toBeNull();
  });

  it("rejects a non-object and every zone but the outer canvas", () => {
    expect(validateBackgrounds([])).toMatch(/must be an object/);
    for (const zone of ["topbar", "nav", "main"]) {
      expect(validateBackgrounds({ [zone]: okPng })).toMatch(
        /is not allowed \(only canvas\)/
      );
    }
  });

  it("runs the same SAFETY gate as an avatar — SVG, bad magic — but its own size cap", () => {
    // Over the BACKGROUND cap, not the avatar one (T-72da).
    const oversize = avatarURI(
      "image/png",
      PNG_MAGIC.concat(new Array(MAX_BACKGROUND_BYTES).fill(0))
    );
    for (const v of [
      avatarURI("image/svg+xml", [0x3c, 0x73, 0x76, 0x67]),
      avatarURI("image/png", JPEG_MAGIC),
      oversize,
    ]) {
      expect(validateBackgrounds({ canvas: v })).toMatch(/not a valid image/);
    }
  });

  it("accepts a background past the avatar cap, up to its own (T-72da)", () => {
    // The owner overturned T-081b's "the cap is NOT raised for backgrounds" on
    // 2026-08-03. Without these two the relaxation has no witness here — the
    // reject cases above still pass with the cap left at 64 KiB.
    const pastAvatarCap = avatarURI(
      "image/png",
      PNG_MAGIC.concat(new Array(MAX_AVATAR_BYTES).fill(0))
    );
    expect(validateBackgrounds({ canvas: pastAvatarCap })).toBeNull();
    // And an avatar of that same size is STILL refused — the relaxation did not
    // leak across, which is the whole point of splitting the caps. Probe a
    // SINGLETON kind: T-cd6f moved member / outsource images into avatarPools,
    // so "member" would be refused on the KIND and prove nothing about the cap.
    expect(validateAvatars({ assistant: pastAvatarCap })).toMatch(
      /not a valid image/
    );
    // The pool is the other entry point into the same cap, so it must refuse it
    // too — otherwise the split leaks through the path member images now take.
    expect(validateAvatarPools({ member: [pastAvatarCap] })).toMatch(
      /not a valid image/
    );

    // Exactly at the background cap. This also proves the raw string-length
    // pre-filter moved: 512 KiB decoded is ~683 KiB encoded, so a stale
    // MAX_BACKGROUND_VALUE_LEN would reject it before the decoded cap is read.
    const atCap = avatarURI(
      "image/png",
      PNG_MAGIC.concat(new Array(MAX_BACKGROUND_BYTES - PNG_MAGIC.length).fill(0))
    );
    expect(validateBackgrounds({ canvas: atCap })).toBeNull();
  });

  it("flows through validateThemeBundle", () => {
    const good = {
      id: "midnight",
      name: "Midnight",
      colors: { [aToken]: "#111111" },
      backgrounds: { canvas: okWebp },
    };
    expect(validateThemeBundle(good)).toBeNull();
    expect(
      validateThemeBundle({ ...good, backgrounds: { topbar: okWebp } })
    ).toMatch(/only canvas/);
  });
});

describe("validateBackgroundModes", () => {
  const images = { canvas: okPng };

  it("accepts undefined (every zone tiles) and both modes on an imaged zone", () => {
    expect(validateBackgroundModes(undefined, images)).toBeNull();
    expect(validateBackgroundModes({ canvas: "tile" }, images)).toBeNull();
    expect(validateBackgroundModes({ canvas: "sides" }, images)).toBeNull();
    expect(validateBackgroundModes({ canvas: "cover" }, images)).toBeNull();
  });

  it("rejects a non-object, an unknown zone and an unknown mode", () => {
    expect(validateBackgroundModes([], images)).toMatch(/must be an object/);
    expect(validateBackgroundModes({ topbar: "tile" }, images)).toMatch(
      /is not allowed \(only canvas\)/
    );
    for (const mode of ["Tile", "contain", "", "sides "]) {
      expect(validateBackgroundModes({ canvas: mode }, images)).toMatch(
        /not a valid mode/
      );
    }
  });

  it("rejects a mode on a zone that carries no image", () => {
    expect(validateBackgroundModes({ canvas: "sides" }, undefined)).toMatch(
      /has no image in backgrounds/
    );
    expect(validateBackgroundModes({ canvas: "sides" }, { canvas: "" })).toMatch(
      /has no image in backgrounds/
    );
  });

  it("flows through validateThemeBundle", () => {
    const good = {
      id: "midnight",
      name: "Midnight",
      colors: { [aToken]: "#111111" },
      backgrounds: { canvas: okWebp },
      backgroundModes: { canvas: "sides" },
    };
    expect(validateThemeBundle(good)).toBeNull();
    expect(
      validateThemeBundle({ ...good, backgroundModes: { canvas: "contain" } })
    ).toMatch(/not a valid mode/);
    expect(
      validateThemeBundle({
        id: "midnight",
        name: "Midnight",
        colors: { [aToken]: "#111111" },
        backgroundModes: { canvas: "sides" },
      })
    ).toMatch(/has no image in backgrounds/);
  });
});

describe("validateThemeBundle backward compatibility", () => {
  it("accepts a legacy member/outsource-only bundle with no logo/navIcons/backgrounds", () => {
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

  it("drops an unknown message code and keeps the known ones", () => {
    // T-081b removed the theme-identity keys from the whitelist, so shipped
    // packs carry "profile.themeOffice" — such a pack must stay importable.
    const wording = {
      zh: { [aKey]: "文字", "profile.themeOffice": "精靈村", "typo.not.a.key": "x" },
    };
    const skipped: string[] = [];
    expect(validateWording(wording, "theme", skipped)).toBeNull();
    expect(wording.zh["profile.themeOffice"]).toBeUndefined();
    expect(wording.zh["typo.not.a.key"]).toBeUndefined();
    expect(wording.zh[aKey]).toBe("文字");
    // The drop is reported, not silent — that channel is what the import UI warns from.
    expect(skipped.sort()).toEqual(["profile.themeOffice", "typo.not.a.key"]);
  });

  it("keeps a themeMarkers override and still drops a theme's identity", () => {
    // Round 8 handed the 內建 / 自訂 labels back to the pack (owner: 「自己要怎麼搞
    // 我們不用特別管」), so an overlay aiming at themeMarkers.* now SURVIVES —
    // including a full-length one. The one code that must still be dropped is a
    // theme's own name: themeIdentity.* is the single remaining exclusion, and
    // settings.themeCopyTag / themeMarkers.copyTag are the retired 副本 tag's
    // paths (round 10 removed the tag itself — the built-in's download is named
    // after the built-in and nothing a pack writes reaches that name).
    const wording = {
      zh: {
        [aKey]: "文字",
        "settings.themeCopyTag": "副本\u202E",
        "themeMarkers.copyTag": "x".repeat(200),
        "themeMarkers.builtinGroup": "自訂".repeat(100),
        "themeMarkers.customGroup": "內建",
        "themeIdentity.office": "精靈村",
      },
    };
    const skipped: string[] = [];
    expect(validateWording(wording, "theme", skipped)).toBeNull();
    expect(skipped.sort()).toEqual([
      "settings.themeCopyTag",
      "themeIdentity.office",
      "themeMarkers.copyTag",
    ]);
    expect(wording.zh["themeMarkers.builtinGroup"]).toBe("自訂".repeat(100));
    expect(wording.zh[aKey]).toBe("文字");
  });

  it("names a code dropped from several languages only once", () => {
    const skipped: string[] = [];
    expect(
      validateWording(
        { zh: { "profile.themeOffice": "精靈村" }, en: { "profile.themeOffice": "Elf" } },
        "theme",
        skipped
      )
    ).toBeNull();
    expect(skipped).toEqual(["profile.themeOffice"]);
  });

  it("rejects a bad language, an over-cap entry count, and illegal values", () => {
    expect(validateWording({ xian: { [aKey]: "仙" } })).toMatch(/language/);
    expect(validateWording({ zh: { [aKey]: "a\nb" } })).toMatch(/control/);
    expect(validateWording({ zh: { [aKey]: "   " } })).toMatch(/1\.\.200/);
    expect(validateWording({ zh: { [aKey]: "字".repeat(201) } })).toMatch(/1\.\.200/);
    // The cap counts the RAW submitted entries, so unknown keys cannot be
    // smuggled past it in bulk behind the new leniency.
    const over: Record<string, string> = {};
    for (let i = 0; i <= MAX_WORDING_ENTRIES_PER_LANG; i++) over[`junk.key.${i}`] = "x";
    expect(validateWording({ zh: over })).toMatch(
      new RegExp(`more than ${MAX_WORDING_ENTRIES_PER_LANG} entries`)
    );
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

describe("trimThemeName", () => {
  it("normalises every Zs to U+0020 and trims ASCII whitespace only", () => {
    // The twin table lives in server/ocserverd/theme_bundle_test.go
    // (TestTrimThemeName). The two validators disagreed on 「\uFEFF辦公室」 because
    // each called its own language's trim; the fix is a normaliser identical BY
    // CONSTRUCTION, so it is pinned character by character rather than through a
    // validator verdict. Case is NOT folded — round 8 removed the name
    // comparison that needed a fold, and the two sides' case mappings disagree.
    for (const [input, want] of [
      ["Office", "Office"],
      ["  OFFICE  ", "OFFICE"],
      ["\tOFFICE\r\n", "OFFICE"],
      ["辦公室", "辦公室"],
      ["OFF\u0130CE", "OFF\u0130CE"],
      ["\uFF2F\uFF26\uFF26\uFF29\uFF23\uFF25", "\uFF2F\uFF26\uFF26\uFF29\uFF23\uFF25"],
      ["\u212ANIGHT", "\u212ANIGHT"],
      // Every Zs is folded onto U+0020 BEFORE the ASCII trim, so a full-width
      // padded name trims exactly like an ASCII-padded one (round 4 recheck,
      // SHOULD-3).
      ["\u3000辦公室", "辦公室"],
      ["辦公室\u3000", "辦公室"],
      ["\u00A0Office", "Office"],
      ["深海\u3000之夜", "深海 之夜"],
      ["\u1680Deep\u2000Ocean\u3000", "Deep Ocean"],
    ] as const) {
      expect(trimThemeName(input), JSON.stringify(input)).toBe(want);
    }
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
