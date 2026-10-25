// themePaint.test.ts — T-1500 gate 4b (jsdom). The VALIDATION half of the
// pre-React paint guard, and the half that does not need a browser.
//
// Why this file exists at all: the pre-React inline script's only protection
// against an attacker-writable localStorage record reaching
// element.style.setProperty is that it calls readValidatedPaint(). With that
// protection removed the CSS injection hole is wide open — and BOTH ways of
// removing it kept tsc green, so a reviewer reading types alone sees nothing.
//
// 🔴 THE TWO WAYS ARE CAUGHT BY DIFFERENT FILES. An independent review measured
// this, and the earlier version of this comment had it backwards — it claimed
// the assertions below go red for the inline-bypass mutant. They do not:
//
//   M1  gut readValidatedPaint ITSELF (bare JSON.parse in this module)
//       → THIS file goes red (6) + paintCache.test.tsx goes red (4).
//   M2  leave the validator intact, have the INLINE SCRIPT bypass it
//       (prePaint.ts reads localStorage and JSON.parses it directly)
//       → this file 19/19 GREEN, all three jsdom files 40/40 GREEN, tsc clean.
//       Only paint-guards/payloadInjection.paint.spec.ts goes red (5 of 6
//       payloads; the sixth is CSSOM-stopped and declares itself uncovered).
//
// Getting this backwards is not a cosmetic slip: it reads as "the jsdom layer
// already covers the inline bypass", which is exactly the argument someone
// reaches for when they want to drop the browser gate for cost. It does not.
// The two layers are complements, and neither replaces the other.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it } from "vitest";
import {
  LS_THEME_PAINT,
  PAINT_CACHE_VERSION,
  applyThemeToRoot,
  paintRecordFor,
  readValidatedPaint,
} from "./themePaint";
import {
  CACHED_BG_HEX,
  EXPECT_APPLIED_TOKENS,
  EXPECT_APPLIED_VALUES,
  MALICIOUS_PAINT_CASES,
  PAINT_THEME_ID,
  SAFE_SANS_STACK,
  SERVER_THEME_JSON_PATH,
  TINY_PNG,
  VALID_RICH_BUNDLE,
  paintRecordJSON,
} from "./paintFixtures";

function seed(raw: string): void {
  localStorage.setItem(LS_THEME_PAINT, raw);
}

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("style");
});

describe("the fixture has exactly one meaning across all three layers", () => {
  it("matches the declared constants", () => {
    expect(VALID_RICH_BUNDLE.id).toBe(PAINT_THEME_ID);
    expect(VALID_RICH_BUNDLE.colors["--color-bg"]).toBe(CACHED_BG_HEX);
    expect(VALID_RICH_BUNDLE.fonts?.["--font-sans"]).toBe(SAFE_SANS_STACK);
    expect(VALID_RICH_BUNDLE.backgrounds?.canvas).toBe(TINY_PNG);
    expect(VALID_RICH_BUNDLE.backgroundModes?.canvas).toBe("cover");
  });

  it("matches the JSON copy the stub server serves", () => {
    // paint-guards/settingsStub.mjs is plain Node: it cannot import this module.
    // It reads the JSON instead, so the JSON is a second copy of these bytes — and
    // an un-asserted second copy is exactly the kind of drift this ticket is about.
    // If this fails, the browser guard is asserting against a DIFFERENT theme than
    // the one measured here, silently.
    const onDisk = JSON.parse(
      readFileSync(resolve(__dirname, "../..", SERVER_THEME_JSON_PATH), "utf8")
    );
    expect(onDisk).toEqual(VALID_RICH_BUNDLE);
  });

  it("is a bundle the shared validator actually accepts", () => {
    // A fixture the grammar rejects would make every positive assertion vacuous.
    seed(paintRecordJSON(VALID_RICH_BUNDLE));
    expect(readValidatedPaint()).not.toBeNull();
  });
});

describe("readValidatedPaint — rejects the whole record, never throws", () => {
  for (const c of MALICIOUS_PAINT_CASES) {
    // `colour-css-injection` is in the fixture list for regression value but is
    // dropped by CSSOM rather than by this validator — it is NOT evidence this
    // gate works, so it is not asserted as a rejection here.
    const shouldReject = !c.notGuardedByValidator;
    it(`${c.name} → ${shouldReject ? "null" : "(CSSOM's job, not asserted)"}`, () => {
      seed(paintRecordJSON(c.bundle));
      const got = readValidatedPaint();
      if (shouldReject) {
        expect(got).toBeNull();
      } else {
        // Only assert it does not throw; whether it parses is CSSOM's business.
        expect(() => readValidatedPaint()).not.toThrow();
      }
    });
  }

  it("a record whose bundle is not an object is rejected", () => {
    for (const bad of ["", "null", "[]", "42", '"str"', "{}", "not json at all"]) {
      seed(bad === "" ? "" : `{"v":1,"bundle":${bad === "not json at all" ? '"x"' : bad}}`);
      // Both halves, and the second is the one with teeth. `not.toThrow()`
      // alone lets a validator that ACCEPTS `{}` pass this test while its title
      // says "rejected" — measured: with readValidatedPaint gutted to a bare
      // JSON.parse, `{"v":1,"bundle":{}}` came back non-null and this test
      // stayed green. The title now has to be earned.
      expect(() => readValidatedPaint()).not.toThrow();
      expect(readValidatedPaint()).toBeNull();
    }
    seed("this is not json");
    expect(readValidatedPaint()).toBeNull();
  });

  it("a record from a future/older cache version is rejected", () => {
    seed(paintRecordJSON(VALID_RICH_BUNDLE, PAINT_CACHE_VERSION + 1));
    expect(readValidatedPaint()).toBeNull();
    seed(paintRecordJSON(VALID_RICH_BUNDLE, PAINT_CACHE_VERSION - 1));
    expect(readValidatedPaint()).toBeNull();
  });

  it("an absent record is null, not a throw", () => {
    expect(readValidatedPaint()).toBeNull();
  });
});

describe("readValidatedPaint — ACCEPTS the valid rich record", () => {
  it("round-trips colours + fonts + canvas image + canvas mode", () => {
    seed(paintRecordJSON(VALID_RICH_BUNDLE));
    const got = readValidatedPaint();
    expect(got).not.toBeNull();
    expect(got).toEqual(VALID_RICH_BUNDLE);
  });

  it("survives paintRecordFor (the writer) → readValidatedPaint (the reader)", () => {
    // The write path must never produce a record its own reader rejects.
    seed(JSON.stringify(paintRecordFor(VALID_RICH_BUNDLE)));
    expect(readValidatedPaint()).toEqual(VALID_RICH_BUNDLE);
  });

  it("paintRecordFor prunes wording, so a wording-bearing bundle is still writable", () => {
    const withWording = {
      ...VALID_RICH_BUNDLE,
      wording: { "zh-Hant": { "app.title": "x" } },
    };
    const rec = paintRecordFor(withWording);
    expect(rec.bundle.wording).toBeUndefined();
    seed(JSON.stringify(rec));
    // …and the pruned record passes the wordingFn=null path.
    expect(readValidatedPaint()).toEqual(VALID_RICH_BUNDLE);
  });
});

describe("applyThemeToRoot — POSITIVE: every branch actually reaches the DOM", () => {
  // This is the assertion that a "forbidden substring never appears" suite
  // cannot make. Deleting the fonts loop or the canvas branch from
  // applyThemeToRoot satisfies every absence-only check; it fails here.
  it("writes the colour, the font AND all five canvas properties", () => {
    const root = document.documentElement;
    const ledger = applyThemeToRoot(root, VALID_RICH_BUNDLE);

    for (const [token, value] of Object.entries(EXPECT_APPLIED_VALUES)) {
      expect(root.style.getPropertyValue(token), `value of ${token}`).toBe(value);
    }
    // The ledger must name every token it wrote — the React apply effect uses it
    // as the removal list, so a missing entry leaks a property forever.
    expect([...ledger].sort()).toEqual([...EXPECT_APPLIED_TOKENS].sort());
  });

  it("the canvas mode is honoured, not silently defaulted", () => {
    const root = document.documentElement;
    applyThemeToRoot(root, VALID_RICH_BUNDLE); // mode: "cover"
    expect(root.style.getPropertyValue("--canvas-bg-size")).toBe("cover");

    const tiled = document.createElement("div");
    applyThemeToRoot(tiled, { ...VALID_RICH_BUNDLE, backgroundModes: { canvas: "tile" } });
    expect(tiled.style.getPropertyValue("--canvas-bg-repeat")).toBe("repeat");
    expect(tiled.style.getPropertyValue("--canvas-bg-size")).toBe("auto");

    const sides = document.createElement("div");
    applyThemeToRoot(sides, { ...VALID_RICH_BUNDLE, backgroundModes: { canvas: "sides" } });
    expect(sides.style.getPropertyValue("--canvas-bg-attachment")).toBe("fixed, fixed");
  });

  it("an absent backgroundModes falls back to the documented default (tile)", () => {
    const el = document.createElement("div");
    const { backgroundModes: _omit, ...noMode } = VALID_RICH_BUNDLE;
    applyThemeToRoot(el, noMode);
    expect(el.style.getPropertyValue("--canvas-bg-repeat")).toBe("repeat");
  });

  it("a colours-only bundle writes exactly the colour tokens and the colour scheme they imply, nothing else", () => {
    const el = document.createElement("div");
    const ledger = applyThemeToRoot(el, {
      id: "x",
      name: "x",
      colors: { "--color-bg": "#010203" },
    });
    expect(ledger).toEqual(["--color-bg", "color-scheme"]);
    expect(el.style.getPropertyValue("--canvas-bg-image")).toBe("");
  });

  it("marks a dark-background theme dark and a light one light, so native controls such as the date picker icon stay visible", () => {
    const dark = document.createElement("div");
    applyThemeToRoot(dark, { id: "d", name: "d", colors: { "--color-bg": "#15101f" } });
    expect(dark.style.getPropertyValue("color-scheme")).toBe("dark");
    const light = document.createElement("div");
    applyThemeToRoot(light, { id: "l", name: "l", colors: { "--color-bg": "#b8d8e3" } });
    expect(light.style.getPropertyValue("color-scheme")).toBe("light");
    const unreadable = document.createElement("div");
    const ledger = applyThemeToRoot(unreadable, {
      id: "u",
      name: "u",
      colors: { "--color-bg": "color-mix(in srgb, red 50%, blue)" },
    });
    expect(unreadable.style.getPropertyValue("color-scheme")).toBe("");
    expect(ledger).toEqual(["--color-bg"]);
  });
});
