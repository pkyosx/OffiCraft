// themeName.parity.test.ts — the SAFETY NET under the Unicode-category name
// rule (T-081b review round 4, SHOULD-C).
//
// Round 3 made the two name validators agree by hand-listing the same six
// codepoints on each side. Round 4 replaced both lists with Unicode general
// CATEGORIES — Go reads them from the standard library's `unicode` tables, TS
// from the engine's `u`-flag property escapes. That is what stops the next
// unlisted Cf character walking through, but it also hands the two ends two
// SEPARATE tables, which can drift on a Unicode version bump: a codepoint one
// runtime classifies as Cf and the other does not is precisely the shape of a
// server-ACCEPT / client-REJECT split, and the server is the authority.
//
// So the agreement is measured, not argued: the same 61 names (themeName.cases.
// json, the corpus round 4's reviewers built) go through BOTH validators in one
// run — this file for TS, `go test -run TestThemeNameVerdictsEmit` for Go — and
// ANY difference in verdict OR in reason fails.
//
// The only normalisation is the error message's path prefix: the two harnesses
// call in at different places ("theme: " vs "custom_themes[0]: "). Everything
// after it must match character for character.

import { describe, it, expect } from "vitest";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { validateThemeBundle } from "./themeBundle";

const HERE = dirname(fileURLToPath(import.meta.url));
const CASES = join(HERE, "themeName.cases.json");
const SERVER = join(HERE, "..", "..", "..", "server", "ocserverd");

interface NameCase {
  k: string;
  n: string;
}

/** This side's verdict, with the harness's path prefix stripped. */
function tsVerdict(name: string): string {
  const err = validateThemeBundle({
    id: "midnight",
    name,
    colors: { "--color-bg": "#101018" },
  });
  return err === null ? "ACCEPT" : `REJECT: ${err.replace(/^theme: /, "")}`;
}

/** The Go side's verdicts for the same file, one subprocess. */
function goVerdicts(): Record<string, string> {
  const dir = mkdtempSync(join(tmpdir(), "name-parity-"));
  const out = join(dir, "verdicts.json");
  try {
    execFileSync(
      "go",
      ["test", "./", "-tags", "octool", "-run", "^TestThemeNameVerdictsEmit$", "-count=1"],
      {
        cwd: SERVER,
        encoding: "utf8",
        env: {
          ...process.env,
          OC_THEME_NAME_CASES: CASES,
          OC_THEME_NAME_VERDICTS: out,
        },
        stdio: ["ignore", "pipe", "pipe"],
      }
    );
    return JSON.parse(readFileSync(out, "utf8"));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

describe("theme name validation · Go/TS parity", () => {
  const cases: NameCase[] = JSON.parse(readFileSync(CASES, "utf8"));

  it("carries a corpus that covers every rejected category and the legal ones", () => {
    // A parity check over an empty (or quietly shrunken) corpus proves nothing.
    expect(cases.length).toBeGreaterThanOrEqual(61);
    const byKey = new Map(cases.map((c) => [c.k, c.n]));
    for (const k of [
      "tag_char", // Cf, astral
      "soft_hyphen", // Cf
      "mongolian_vowel_sep", // Cf
      "line_sep_u2028", // Zl
      "para_sep_u2029", // Zp
      "nbsp_pad_office", // Zs — normalised away, then an ordinary name
      "ideographic_space_pad", // Zs — same
      "ogham_space", // Zs — same
      "ideographic_space_inner", // Zs — normalised, and LEGAL: 「深海　之夜」
      "nbsp_inner", // Zs — normalised, and legal
      "variation_sel_office", // Mn — deliberately NOT rejected
      "cjk",
      "korean",
      "arabic_plain",
      "hebrew_plain",
      "emoji_vs16_only",
    ]) {
      expect(byKey.has(k), k).toBe(true);
    }
  });

  it("returns the identical verdict on both ends for every name", () => {
    const go = goVerdicts();
    const divergent: string[] = [];
    for (const c of cases) {
      const ts = tsVerdict(c.n);
      if (go[c.k] !== ts) {
        divergent.push(
          `${c.k} (${JSON.stringify(c.n)})\n    go: ${go[c.k]}\n    ts: ${ts}`
        );
      }
    }
    expect(divergent.join("\n  "), divergent.join("\n  ")).toBe("");
    expect(Object.keys(go).length).toBe(cases.length);
  });

  it("rejects every invisible-category name and keeps every legitimate one", () => {
    // The parity check alone would still pass if BOTH ends went wrong the same
    // way, so the verdicts themselves are pinned too — the eight names round 4
    // found walking through, and the ones that must never be caught.
    const verdict = Object.fromEntries(cases.map((c) => [c.k, tsVerdict(c.n)]));
    for (const k of [
      "tag_char",
      "soft_hyphen",
      "mongolian_vowel_sep",
      "line_sep_u2028",
      "para_sep_u2029",
    ]) {
      expect(verdict[k], k).toMatch(
        /^REJECT: name must not contain control, formatting, private-use, surrogate or line\/paragraph separator characters$/
      );
    }
    // Zs is NORMALISED to U+0020, not rejected (round 4 recheck, SHOULD-3), and
    // round 8 removed the reserved-name rule that caught these on the way out —
    // 「　辦公室　」 is simply a name the user chose. Both ends must agree on that,
    // which is what the parity check above measures; here it is pinned outright.
    for (const k of [
      "nbsp_pad_office",
      "ideographic_space_pad",
      "ideographic_space_office",
      "ogham_space",
    ]) {
      expect(verdict[k], k).toBe("ACCEPT");
    }
    for (const k of ["nbsp_only", "ideographic_space_only"]) {
      expect(verdict[k], k).toMatch(/^REJECT: name must be 1\.\./);
    }
    for (const k of [
      "plain_ascii",
      "cjk",
      "korean",
      "arabic_plain",
      "hebrew_plain",
      "emoji_simple",
      "emoji_vs16_only",
      "variation_sel_office", // U+FE0F is Mn — a legal emoji spelling, see below
      // The names round 8 freed: 「辦公室」/「Office」 and every trim/case/NFD
      // spelling of them are ordinary custom-theme names now.
      "builtin_zh",
      "builtin_en",
      "builtin_upper",
      "builtin_pad_ascii",
      "builtin_dotted_I",
      "builtin_fullwidth",
      "builtin_kelvin",
      "nfd_office",
      "spoof_builtin_marker_zh",
      "spoof_builtin_marker_en",
      "new_theme_zh",
      "new_theme_en",
      "ideographic_space_inner", // 「深海　之夜」 — what a full-width IME emits
      "ideographic_space_inner_many",
      "nbsp_inner",
      "ideographic_space_pad_custom",
    ]) {
      expect(verdict[k], k).toBe("ACCEPT");
    }
  });
});
