import { afterEach, describe, expect, it } from "vitest";
import {
  bundleFilename,
  exportComputedTheme,
  exportOfficeBaseTheme,
  nextCustomThemeId,
  parseImportedBundle,
  serializeBundle,
} from "./themeExport";
import { isValidColorValue } from "./themeBundle";
import { applyWording } from "../i18n/wording";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";
import { zh } from "../i18n/locales/zh";
import {
  THEME_ALIAS_DEFAULT_TOKENS,
  THEME_COLOR_TOKENS,
} from "../styles/themeTokens.generated";

function freshRoot(): HTMLElement {
  const el = document.createElement("div");
  document.body.appendChild(el);
  return el;
}

afterEach(() => {
  document.body.innerHTML = "";
  document.documentElement.removeAttribute("style");
});

describe("exportComputedTheme", () => {
  it("packs the resolved value of each --color-* token set on the element", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#123456");
    el.style.setProperty("--color-bg", "rgb(10, 20, 30)");

    const bundle = exportComputedTheme("mine", "Mine", el);

    expect(bundle.id).toBe("mine");
    expect(bundle.name).toBe("Mine");
    expect(bundle.colors["--color-accent"]).toBe("#123456");
    expect(bundle.colors["--color-bg"]).toBe("rgb(10, 20, 30)");
  });

  it("omits tokens with no value and tokens that resolve to a non-concrete colour", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#abcabc");
    // an unresolved color-mix() must never poison the exported bundle
    el.style.setProperty("--color-bg", "color-mix(in srgb, red, blue)");

    const bundle = exportComputedTheme("mine", "Mine", el);

    expect(bundle.colors["--color-accent"]).toBe("#abcabc");
    expect(bundle.colors).not.toHaveProperty("--color-bg");
    expect(bundle.colors).not.toHaveProperty("--color-text");
  });

  it("omits an alias-default token still sitting on its alias, keeps one that was moved off it", () => {
    // The zone tokens default to `var(--color-bg)` so an untouched theme stays
    // one flat backdrop and layers only once someone picks a zone colour.
    // getComputedStyle RESOLVES that var(), so baking the resolved value in
    // would pin every newly seeded theme's zones to the built-in colour: the
    // author edits --color-bg and nothing but the body moves (in the wide
    // layout, where the gutter is 0, nothing visible moves at all).
    expect(Object.keys(THEME_ALIAS_DEFAULT_TOKENS)).toContain(
      "--color-topbar-bg"
    );
    const el = freshRoot();
    // Each alias default follows a DIFFERENT target (the zone colours follow
    // --color-bg; the T-081b split tokens follow --color-overlay / --color-shadow
    // / --color-indigo), so give every target its own value and every follower
    // exactly that value — which is what the browser reports for an untouched
    // alias, and the only state "still following" can be recognised from.
    const targetValue: Record<string, string> = {};
    let n = 0;
    for (const target of new Set(Object.values(THEME_ALIAS_DEFAULT_TOKENS))) {
      targetValue[target] = `#11${(++n).toString().padStart(2, "0")}22`;
      el.style.setProperty(target, targetValue[target]);
    }
    for (const [tok, target] of Object.entries(THEME_ALIAS_DEFAULT_TOKENS)) {
      el.style.setProperty(tok, targetValue[target]);
    }
    el.style.setProperty("--color-topbar-bg", "#ff0000"); // deliberately chosen

    const bundle = exportComputedTheme("mine", "Mine", el);

    expect(bundle.colors["--color-bg"]).toBe(targetValue["--color-bg"]);
    expect(bundle.colors["--color-topbar-bg"]).toBe("#ff0000");
    for (const tok of Object.keys(THEME_ALIAS_DEFAULT_TOKENS)) {
      if (tok === "--color-topbar-bg") continue;
      expect(bundle.colors).not.toHaveProperty(tok);
    }
  });

  it("cannot carry the canvas background image — no colour token holds a url()", () => {
    // The image lives on bundle.backgrounds, never in the colour map: every
    // colour token is exported through isValidColorValue, which no url("data:…")
    // can pass, so a token-shaped image would be silently dropped here.
    expect(THEME_COLOR_TOKENS.some((tok) => /image/.test(tok))).toBe(false);
    expect(isValidColorValue('url("data:image/png;base64,iVBORw0KGgo=")')).toBe(
      false
    );
  });

  it("produces a bundle that re-imports without loss", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#0af");

    const round = parseImportedBundle(
      serializeBundle(exportComputedTheme("mine", "Mine", el))
    );

    expect("bundle" in round).toBe(true);
  });
});

describe("exportOfficeBaseTheme", () => {
  it("reads through an active theme's inline overrides and restores them", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#111111"); // theme.css :root default stand-in
    // an "active custom theme" override is layered on top
    el.style.setProperty("--color-accent", "#abcdef");

    const bundle = exportOfficeBaseTheme("custom-1", "New theme", el);

    // exportOfficeBaseTheme strips the inline override to read the base; in jsdom
    // there is no stylesheet base, so the stripped token drops out entirely —
    // the point under test is that the override is gone during the read and put
    // BACK afterwards (no permanent mutation of the live element).
    expect(bundle.id).toBe("custom-1");
    expect(bundle.name).toBe("New theme");
    expect(el.style.getPropertyValue("--color-accent")).toBe("#abcdef");
  });

  it("uses a non-reserved id ('office-base') so its download re-imports — unlike 'office'", () => {
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#0af");
    // The office 列下載 path exports under id "office-base"; the base read is
    // simulated here (jsdom has no stylesheet base) via exportComputedTheme.
    const round = parseImportedBundle(
      serializeBundle(exportComputedTheme("office-base", "我的辦公室", el))
    );
    expect("bundle" in round).toBe(true);
    // The reserved built-in id would be rejected on re-import.
    expect("error" in parseImportedBundle(
      serializeBundle(exportComputedTheme("office", "我的辦公室", el))
    )).toBe(true);
  });

  it("downloads the built-in under a name no theme bundle can reach", () => {
    // 下載 on the built-in row names the file after the BUILT-IN — 辦公室, read
    // from the themeIdentity subtree, the one subtree the wording whitelist
    // excludes. Round 10 removed the 「(副本)」 tag that used to be appended
    // (owner: 「我覺得檔名不用附註副本」); the tag was the ONE pack-settable string
    // that reached this name, and while it did, a pack could stretch it until the
    // built-in row's download produced a file the product refused to import back
    // — breaking the escape hatch to the shipped palette (review round 9,
    // SHOULD-2). What pins that now is this: a pack that overrides EVERY code on
    // the whitelist must not move this name by one character.
    const el = freshRoot();
    el.style.setProperty("--color-accent", "#0af");
    const forged: Record<string, string> = {};
    for (const code of MESSAGE_KEYS) forged[code] = "x".repeat(200);
    const t = applyWording(zh, forged);

    const name = t.themeIdentity.office;

    expect(name).toBe(zh.themeIdentity.office);
    expect(name).not.toContain("x");
    expect(
      "bundle" in parseImportedBundle(
        serializeBundle(exportComputedTheme("office-base", name, el))
      )
    ).toBe(true);
  });
});

describe("nextCustomThemeId", () => {
  it("returns custom-1 when nothing is taken", () => {
    expect(nextCustomThemeId([])).toBe("custom-1");
  });

  it("skips taken ids and the reserved built-in", () => {
    expect(nextCustomThemeId(["custom-1", "custom-2"])).toBe("custom-3");
    expect(nextCustomThemeId(["office"])).toBe("custom-1");
  });
});

describe("parseImportedBundle", () => {
  it("returns the normalized bundle for admissible JSON", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "midnight",
        name: "Midnight",
        colors: { "--color-accent": "#0b1020" },
      })
    );
    expect(res).toEqual({
      bundle: {
        id: "midnight",
        name: "Midnight",
        colors: { "--color-accent": "#0b1020" },
      },
      skippedWording: [],
    });
  });

  it("carries a valid wording overlay through (T-16a1 P3)", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "worded",
        name: "Worded",
        colors: { "--color-accent": "#0b1020" },
        wording: { zh: { "nav.tasks": "任務榜" } },
      })
    );
    expect("bundle" in res && res.bundle.wording).toEqual({
      zh: { "nav.tasks": "任務榜" },
    });
    expect("bundle" in res && res.skippedWording).toEqual([]);
  });

  it("carries a valid avatars overlay through (bb2e3b4)", () => {
    const pngAvatar =
      "data:image/png;base64," +
      btoa(
        String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01)
      );
    const res = parseImportedBundle(
      JSON.stringify({
        id: "faced",
        name: "Faced",
        colors: { "--color-accent": "#0b1020" },
        avatars: { outsource: pngAvatar },
      })
    );
    expect("bundle" in res && res.bundle.avatarPools).toEqual({
      outsource: [pngAvatar],
    });
  });

  it("carries a valid logo and navIcons overlay through (T-ea81)", () => {
    const png =
      "data:image/png;base64," +
      btoa(
        String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01)
      );
    const res = parseImportedBundle(
      JSON.stringify({
        id: "branded",
        name: "Branded",
        colors: { "--color-accent": "#0b1020" },
        logo: png,
        navIcons: { office: png, tasks: png },
      })
    );
    expect("bundle" in res && res.bundle.logo).toBe(png);
    expect("bundle" in res && res.bundle.navIcons).toEqual({ office: png, tasks: png });
  });

  it("round-trips a canvas background image through serialize → import (T-081b)", () => {
    // The image value is url("data:…") — NOT a colour — so it must ride its own
    // field. A colour token would be filtered out by the export's
    // isValidColorValue and rejected by the bundle's colour grammar.
    const png =
      "data:image/png;base64," +
      btoa(
        String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01)
      );
    const exported = serializeBundle({
      id: "tiled",
      name: "Tiled",
      colors: { "--color-bg": "#0b1020" },
      backgrounds: { canvas: png },
      backgroundModes: { canvas: "sides" },
    });
    const res = parseImportedBundle(exported);

    expect("bundle" in res && res.bundle.backgrounds).toEqual({ canvas: png });
    // the mode rides along — dropping it would round-trip a "sides" theme back
    // as a tiled one, i.e. silently change how the theme looks
    expect("bundle" in res && res.bundle.backgroundModes).toEqual({
      canvas: "sides",
    });
    // and the colour beside it survives the same trip — colour and image coexist
    expect("bundle" in res && res.bundle.colors["--color-bg"]).toBe("#0b1020");
  });

  it("round-trips the badge ring slot, and keeps a still-unset one deferred (T-d593)", () => {
    // DoD 3. The ring got its own slot in T-d593, and the whole point of that
    // slot is that a theme author can carry it to another install. Until this
    // test the ring rode on the GENERIC alias/whitelist machinery only: the
    // alias test above loops over THEME_ALIAS_DEFAULT_TOKENS, so dropping the
    // ring OUT of that map makes the loop simply not visit it and stays green.
    // Both halves below are therefore named explicitly.
    const RING = "--color-danger-badge-ring";

    // (a) the token is exportable at all — outside THEME_COLOR_TOKENS the
    //     export never packs it and an import naming it is REJECTED, so a
    //     hand-written pack setting the ring would fail to load.
    expect(THEME_COLOR_TOKENS).toContain(RING);

    // (b) still following the page background ⇒ omitted, so importing this
    //     bundle elsewhere leaves the ring free to follow THAT install's
    //     --color-bg. Baking the resolved value here is the documented
    //     regression: a theme whose --color-bg was edited would get a ring
    //     pinned to the built-in dark blue. jsdom does not resolve var(), so
    //     "still following" is spelled the way the browser reports it — the
    //     follower holding exactly its target's value (same trick as the
    //     alias-default test above).
    expect(Object.keys(THEME_ALIAS_DEFAULT_TOKENS)).toContain(RING);
    expect(THEME_ALIAS_DEFAULT_TOKENS[RING]).toBe("--color-bg");
    const unset = freshRoot();
    unset.style.setProperty("--color-bg", "#0b1020");
    unset.style.setProperty(RING, "#0b1020");
    expect(exportComputedTheme("t1", "T1", unset).colors).not.toHaveProperty(RING);

    // (c) a ring the author actually CHOSE survives export → serialize →
    //     import byte-identically, and is not confused with the page colour it
    //     used to borrow: --color-bg keeps its own, different value.
    const chosen = freshRoot();
    chosen.style.setProperty("--color-bg", "#0b1020");
    chosen.style.setProperty(RING, "#ff00aa");

    const bundle = exportComputedTheme("t2", "T2", chosen);
    expect(bundle.colors[RING]).toBe("#ff00aa");

    const json = serializeBundle(bundle);
    expect(json).toContain(RING);
    const res = parseImportedBundle(json);
    expect("bundle" in res && res.bundle.colors[RING]).toBe("#ff00aa");
    expect("bundle" in res && res.bundle.colors["--color-bg"]).toBe("#0b1020");
  });

  it("imports a pack overriding a de-whitelisted code, without that code", () => {
    // The real-world 精靈村 pack: T-081b removed profile.themeOffice from the
    // whitelist, and the pack must still import (owner ruling 2026-07-27) —
    // minus that code, so a re-export carries only live codes.
    const res = parseImportedBundle(
      JSON.stringify({
        id: "worded",
        name: "Worded",
        colors: { "--color-accent": "#0b1020" },
        wording: {
          zh: { "nav.tasks": "任務榜", "profile.themeOffice": "精靈村", "not.a.real.code": "x" },
        },
      })
    );
    expect("bundle" in res && res.bundle.wording).toEqual({
      zh: { "nav.tasks": "任務榜" },
    });
    // …and it REPORTS what it dropped, so the import UI can warn (owner
    // 2026-07-27: silent dropping is not acceptable).
    expect("bundle" in res && res.skippedWording).toEqual([
      "profile.themeOffice",
      "not.a.real.code",
    ]);
  });

  it("rejects malformed JSON with a plain-language error", () => {
    const res = parseImportedBundle("{ not json");
    expect("error" in res && res.error).toBe("不是有效的 JSON");
  });

  it("rejects a bundle carrying an injection-shaped colour value", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "evil",
        name: "Evil",
        colors: { "--color-bg": "red; } body { background: url(x)" },
      })
    );
    expect("error" in res).toBe(true);
  });

  it("rejects a bundle whose id is reserved for a built-in", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "office",
        name: "Nope",
        colors: { "--color-accent": "#fff" },
      })
    );
    expect("error" in res).toBe(true);
  });

  it("rejects a token name outside the theme.css whitelist", () => {
    const res = parseImportedBundle(
      JSON.stringify({
        id: "sneaky",
        name: "Sneaky",
        colors: { "--color-not-a-token": "#fff" },
      })
    );
    expect("error" in res).toBe(true);
  });
});

describe("bundleFilename", () => {
  it("derives a filesystem-safe name from the bundle id", () => {
    expect(bundleFilename({ id: "midnight", name: "M", colors: {} })).toBe(
      "officraft-theme-midnight.json"
    );
  });
});
