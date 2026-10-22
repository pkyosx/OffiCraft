// gen-theme-tokens.test.ts — the whitelist is theme.css's --color-* set, whole.
//
// It used to carry an exclusion: the --color-marker-* family (the 內建 / 自訂
// heading colour) was kept out, and these tests watched the exclusion refuse to
// drop a token silently. T-081b round 8 removed the family — the owner let a
// pack decide the heading's colour — so what has to be watched now is the
// opposite property: the generator emits EVERY token theme.css defines, and a
// token added there reaches both whitelists without anyone editing a second
// list. A silently dropped token is the failure this file exists to catch,
// whatever the reason for the drop.

import { describe, it, expect } from "vitest";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SCRIPT = join(HERE, "gen-theme-tokens.mjs");
const REAL_THEME_CSS = join(HERE, "..", "src", "styles", "theme.css");

/** Run the generator over a copy of the real theme.css, after `sabotage` has had
 *  its way with it, writing its outputs into a temp dir. `ts` / `go` are the two
 *  generated files' contents (empty when the generator refused). */
function run(sabotage?: (css: string) => string) {
  const dir = mkdtempSync(join(tmpdir(), "gen-theme-tokens-"));
  const src = join(dir, "theme.css");
  const css = readFileSync(REAL_THEME_CSS, "utf8");
  writeFileSync(src, sabotage ? sabotage(css) : css);
  const read = (name: string) => {
    try {
      return readFileSync(join(dir, name), "utf8");
    } catch {
      return "";
    }
  };
  try {
    const stdout = execFileSync("node", [SCRIPT], {
      encoding: "utf8",
      env: { ...process.env, GEN_THEME_TOKENS_SRC: src, GEN_THEME_TOKENS_OUT_DIR: dir },
      stdio: ["ignore", "pipe", "pipe"],
    });
    return { code: 0, out: stdout, ts: read("themeTokens.generated.ts"), go: read("theme_colornames_gen.go") };
  } catch (e) {
    const err = e as { status: number; stdout: string; stderr: string };
    return {
      code: err.status,
      out: `${err.stdout}${err.stderr}`,
      ts: read("themeTokens.generated.ts"),
      go: read("theme_colornames_gen.go"),
    };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

/** Every --color-* DEFINITION in a theme.css body, comments stripped (the
 *  generator's own extraction, restated independently of it). */
const tokensIn = (css: string) =>
  [
    ...new Set(
      [...css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/(--color-[a-z0-9-]+)\s*:/g)].map(
        (m) => m[1]
      )
    ),
  ].sort();

describe("gen-theme-tokens", { timeout: 60_000 }, () => {
  it("puts every --color-* token theme.css defines into both whitelists", () => {
    const { code, out, ts, go } = run();
    expect(code, out).toBe(0);
    const expected = tokensIn(readFileSync(REAL_THEME_CSS, "utf8"));
    expect(expected.length).toBeGreaterThan(0);
    for (const token of expected) {
      expect(ts, `${token} missing from THEME_COLOR_TOKENS`).toContain(`"${token}",`);
      expect(go, `${token} missing from themeColorTokens`).toContain(`"${token}":`);
    }
    // …and nothing beyond them, so a name cannot be smuggled in either.
    const listed = ts.slice(ts.indexOf("THEME_COLOR_TOKENS"), ts.indexOf("];"));
    expect((listed.match(/"--color-[a-z0-9-]+",/g) ?? []).length).toBe(expected.length);
    expect(out, out).toContain(`wrote ${expected.length} tokens`);
  });

  it("carries a newly added token through without a second list to edit", () => {
    // The whole point of extracting from theme.css: adding a colour there is the
    // only edit. No name is reserved and nothing is excluded any more, so a
    // token that fails to show up here was dropped silently — the failure this
    // file exists to catch.
    const { code, out, ts, go } = run((css) =>
      css.replace("--color-card: #242832;", "--color-card: #242832;\n  --color-marker-halo: #223;")
    );
    expect(code, out).toBe(0);
    expect(ts, ts).toContain(`"--color-marker-halo",`);
    expect(go, go).toContain(`"--color-marker-halo":`);
  });

  it("refuses to emit an empty whitelist", () => {
    // An empty THEME_COLOR_TOKENS rejects every pack while looking like a clean
    // run, so a theme.css the regex stops matching must go red, not quiet.
    const { code, out } = run((css) => css.replace(/--color-[a-z0-9-]+\s*:/g, "--x:"));
    expect(out, out).toMatch(/no --color-\* tokens found/);
    expect(code).toBe(1);
  });
});
