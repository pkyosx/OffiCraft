// paintArtifact.test.ts — T-1500 gate 4b. The BUILD-ARTIFACT half of the
// pre-React paint guard: what the bundler actually emitted, not what the source
// says.
//
// Why an artifact test and not a source review: the bundler is entitled to
// rewrite the architecture you think you wrote. Two measured examples from this
// ticket's own history —
//   * a second `<script type="module">` entry gets folded into the 659 kB main
//     chunk, so the "pre-paint" waits for the whole app to download;
//   * `injectTo: "head"` put the inline script AFTER the app's module script,
//     and `injectTo: "head-prepend"` pushed `<meta charset>` out to byte 10,043,
//     past the 1,024-byte window the HTML spec gives encoding sniffing.
// Both are invisible in the source and invisible to tsc. They are visible here.
//
// This runs in the EXISTING vitest gate and needs no browser, so it survives the
// browser-level guards being dropped for cost. It builds with
// VITE_USE_MOCK=false because that is what ships (see bin/build).

import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { gzipSync } from "node:zlib";
import { beforeAll, describe, expect, it } from "vitest";
import { LS_SEASONAL_PAINT, LS_SEASONAL_THEME, LS_THEME, LS_THEME_PAINT } from "./themePaint";
import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";
import { RESERVED_THEME_IDS } from "./themeBundleCore";

const FE_ROOT = resolve(__dirname, "../..");
// A dedicated outDir: `dist/` belongs to the developer and to the gate-4c paint
// guard, and a test must never race them or clobber a build someone is serving.
const OUT_DIR = "dist-paint-guard";

let html = "";
/** Byte offsets of every `<script` tag, in document order. */
let scriptTags: number[] = [];

beforeAll(() => {
  execFileSync(
    "npx",
    [
      "--no-install",
      "vite",
      "build",
      "--outDir",
      OUT_DIR,
      "--emptyOutDir",
      "--logLevel",
      "warn",
    ],
    {
      cwd: FE_ROOT,
      env: { ...process.env, VITE_USE_MOCK: "false" },
      stdio: "pipe",
    }
  );
  // latin1 so string indices ARE byte offsets — the charset window is a byte
  // budget and this file's <title> is Chinese (3 bytes per char in UTF-8).
  html = readFileSync(resolve(FE_ROOT, OUT_DIR, "index.html"), "latin1");
  scriptTags = [];
  for (let i = html.indexOf("<script"); i !== -1; i = html.indexOf("<script", i + 1)) {
    scriptTags.push(i);
  }
}, 300_000);

describe("dist/index.html — the pre-paint script is present, FIRST, and complete", () => {
  it("A. the inline pre-paint script is in the HTML at all", () => {
    expect(scriptTags.length).toBeGreaterThanOrEqual(2);
    expect(html).toContain(JSON.stringify(LS_THEME_PAINT));
  });

  it("B. it is the FIRST script tag — earlier than the app bundle", () => {
    const marker = html.indexOf(JSON.stringify(LS_THEME_PAINT));
    expect(marker).toBeGreaterThan(scriptTags[0]);
    expect(marker).toBeLessThan(scriptTags[1]);
    // Spell the converse out too: tag[0] must not be the app's module script.
    // Today's ordering only survives because that script is `type="module"`
    // (defer); if it ever becomes classic or async, order stops being cosmetic.
    expect(html.slice(scriptTags[0], scriptTags[1])).not.toContain('type="module"');
  });

  it("C. <meta charset> is inside the 1024-byte sniffing window AND before any script", () => {
    // ANCHORED to the declaration: a bare `"charset"` substring search passes on
    // any minified chunk that happens to contain the word.
    const charset = html.indexOf("<meta charset");
    expect(charset).toBeGreaterThanOrEqual(0);
    expect(charset).toBeLessThan(1024);
    expect(charset).toBeLessThan(scriptTags[0]);
  });

  it("D. the WHOLE generated colour whitelist is compiled in, not a subset", () => {
    // The point of building the inline script from the real module is that there
    // is exactly one whitelist. A hand-written "simplified" copy would silently
    // shrink the security boundary; every token being present rules that out.
    const missing = THEME_COLOR_TOKENS.filter((t) => !html.includes(t));
    expect(missing, "colour tokens absent from the inlined whitelist").toEqual([]);
    expect(THEME_COLOR_TOKENS.length).toBeGreaterThan(20);
  });

  it("E. every localStorage key comes from the module, not from a literal", () => {
    // Read the values FROM the module and look for them in the artifact: if
    // someone re-hardcodes a key in prePaint.ts and then changes the module's
    // value, the artifact stops carrying the new value and this goes red.
    expect(html).toContain(JSON.stringify(LS_THEME));
    expect(html).toContain(JSON.stringify(LS_THEME_PAINT));
    expect(html).toContain(JSON.stringify(LS_SEASONAL_THEME));
    expect(html).toContain(JSON.stringify(LS_SEASONAL_PAINT));
  });
});

describe("dist/index.html — the inline script stays cheap", () => {
  it("carries none of the 25 kB wording whitelist", () => {
    // The paint path passes wordingFn=null, so the message-key whitelist must not
    // be in its import graph. That is what the -60 % gzip win is made of; without
    // an assertion it evaporates the first time someone imports the wrong module.
    const leaked = MESSAGE_KEYS.filter(
      (k) => k.length > 8 && html.includes(JSON.stringify(k))
    );
    expect(leaked.slice(0, 5), "message keys leaked into the inline script").toEqual([]);
  });

  it("is under the agreed per-load budget", () => {
    // Measured at land: 11,042 B raw / 3,896 B gzip, paid on EVERY load and not
    // cacheable.
    //
    // ⚠️ The ceiling is NOT tight, and the comment here used to claim it was.
    // 6,000 against a measured 3,896 is +54% of headroom: this catches a
    // doubling, not a creep. It is a blast-radius cap, not a budget — do not
    // cite it as evidence that the payload has not grown. If you want the
    // conversation-forcing version, the number has to come down to roughly
    // 4,200, and then it has to be re-baselined whenever the token whitelist
    // legitimately grows.
    const gz = gzipSync(Buffer.from(html, "latin1")).length;
    expect(gz).toBeLessThan(6_000);
  });
});

describe("no second source of truth for the theme storage keys", () => {
  // Asserting the artifact carries the module's VALUE only catches a drift once
  // the values have already diverged (a two-step regression: re-hardcode, then
  // change the constant). This catches step one — the moment a literal is
  // reintroduced — which is while a reviewer can still act on it.
  const THEME_KEY_LITERALS = /"(oc\.theme|oc\.themePaint|oc\.seasonalTheme|oc\.seasonalPaint)"/g;
  const SOURCES = ["src/paint/prePaint.ts", "src/i18n/index.tsx"] as const;

  for (const rel of SOURCES) {
    it(`${rel} imports the keys instead of spelling them`, () => {
      const src = readFileSync(resolve(FE_ROOT, rel), "utf8");
      const hits = [...src.matchAll(THEME_KEY_LITERALS)].map((m) => m[1]);
      expect(
        hits,
        `hardcoded theme storage keys in ${rel} — import the LS_* keys from lib/themePaint`
      ).toEqual([]);
    });
  }
});

describe("dist/ — every seasonal theme file ships as its own lazily loaded chunk", () => {
  const themesDir = resolve(FE_ROOT, "../themes");
  const schedule = JSON.parse(
    readFileSync(resolve(themesDir, "seasonal-schedule.json"), "utf8")
  ) as { theme: string }[];
  const assetsDir = resolve(FE_ROOT, OUT_DIR, "assets");

  for (const { theme } of schedule) {
    it(`${theme} is in no script the page loads up front, and in exactly one other chunk`, () => {
      const bundle = JSON.parse(readFileSync(resolve(themesDir, theme), "utf8")) as {
        backgrounds?: { canvas?: string };
        avatars?: Record<string, string>;
      };
      const image = bundle.backgrounds?.canvas ?? Object.values(bundle.avatars ?? {})[0];
      expect(image, `${theme} carries no image to recognise its chunk by`).toBeTruthy();
      const needle = (image as string).slice(-64);

      const entries = [...html.matchAll(/<script[^>]*\ssrc="\/assets\/([^"]+)"/g)].map((m) => m[1]);
      expect(entries.length).toBeGreaterThan(0);
      expect(html.includes(needle), "inlined into index.html").toBe(false);
      const carriers = readdirSync(assetsDir).filter((f) =>
        readFileSync(resolve(assetsDir, f), "utf8").includes(needle)
      );
      expect(carriers).toHaveLength(1);
      expect(entries).not.toContain(carriers[0]);
      const preloaded = [...html.matchAll(/<link[^>]*href="\/assets\/([^"]+)"/g)].map((m) => m[1]);
      expect(preloaded).not.toContain(carriers[0]);
    });
  }

  it("ships no chunk for a built-in theme's file — those reach the page through theme.css", () => {
    const shipped = readdirSync(assetsDir);
    for (const id of RESERVED_THEME_IDS) {
      expect(shipped.filter((f) => f.startsWith(`${id}.theme-`)), id).toEqual([]);
    }
  });
});
