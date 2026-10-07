import { describe, it, expect } from "vitest";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { RESERVED_THEME_IDS } from "../src/lib/themeBundleCore";

const HERE = dirname(fileURLToPath(import.meta.url));
const SCRIPT = join(HERE, "gen-builtin-themes.mjs");
const REAL_THEME_CSS = join(HERE, "..", "src", "styles", "theme.css");
const REAL_THEMES_DIR = join(HERE, "..", "..", "themes");

type Theme = { id: string; colors: Record<string, string> };

const readTheme = (id: string): Theme =>
  JSON.parse(readFileSync(join(REAL_THEMES_DIR, `${id}.theme.json`), "utf8"));

/** Run the generator over copies of theme.css and the two built-in theme files,
 * after the callers' edits. `css` is the output ("" when it refused). */
function run(edit: {
  css?: (css: string) => string;
  office?: (t: Theme) => unknown;
  light?: (t: Theme) => unknown;
} = {}) {
  const dir = mkdtempSync(join(tmpdir(), "gen-builtin-themes-"));
  const themes = join(dir, "themes");
  mkdirSync(themes);
  const src = join(dir, "theme.css");
  const out = join(dir, "out.css");
  const css = readFileSync(REAL_THEME_CSS, "utf8");
  writeFileSync(src, edit.css ? edit.css(css) : css);
  const office = readTheme("office");
  const light = readTheme("office-light");
  writeFileSync(join(themes, "office.theme.json"), JSON.stringify(edit.office ? edit.office(office) : office));
  writeFileSync(
    join(themes, "office-light.theme.json"),
    JSON.stringify(edit.light ? edit.light(light) : light)
  );
  const env = {
    ...process.env,
    GEN_BUILTIN_THEMES_DIR: themes,
    GEN_BUILTIN_THEMES_CSS: src,
    GEN_BUILTIN_THEMES_OUT: out,
  };
  try {
    const stdout = execFileSync("node", [SCRIPT], { encoding: "utf8", env, stdio: ["ignore", "pipe", "pipe"] });
    return { code: 0, out: stdout, css: readFileSync(out, "utf8"), before: css };
  } catch (e) {
    const err = e as { status: number; stdout: string; stderr: string };
    return { code: err.status, out: `${err.stdout}${err.stderr}`, css: "", before: css };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

describe("gen-builtin-themes.mjs", () => {
  it("over the shipped theme files, reproduces the committed theme.css byte for byte", () => {
    const r = run();
    expect(r.code).toBe(0);
    expect(r.css).toBe(r.before);
  });

  it("writes every built-in colour from its theme file even when theme.css holds other values", () => {
    const r = run({
      css: (css) => css.replace(/^([ \t]+--color-[a-z0-9-]+[ \t]*:[ \t]*)[^;]+;/gm, "$1#000000;"),
    });
    expect(r.code).toBe(0);
    expect(r.css).toBe(r.before);
  });

  it("generates exactly the built-in themes, in picker order", () => {
    const r = run();
    expect(r.out).toContain(
      'office (:root) 74 tokens, office-light (:root[data-theme="office-light"]) 73 tokens'
    );
    expect(RESERVED_THEME_IDS).toEqual(["office", "office-light"]);
  });

  it("changes only the edited value, in the edited block, keeping its spacing and trailing comment", () => {
    const r = run({
      office: (t) => ({ ...t, colors: { ...t.colors, "--color-overlay": "#eeeeee" } }),
    });
    expect(r.code).toBe(0);
    const expected = r.before.replace(
      "  --color-overlay: #fff;   /* border/hover/背景 白疊層基色;淺色主題覆寫成深色即自動翻轉 */",
      "  --color-overlay: #eeeeee;   /* border/hover/背景 白疊層基色;淺色主題覆寫成深色即自動翻轉 */"
    );
    expect(expected).not.toBe(r.before);
    expect(r.css).toBe(expected);
  });

  it("adds a token only the file has after the block's last colour line, and drops one only theme.css has", () => {
    const r = run({
      light: (t) => {
        const colors: Record<string, string> = { ...t.colors, "--color-new-slot": "#123456" };
        delete colors["--color-knob"];
        return { ...t, colors };
      },
    });
    expect(r.code).toBe(0);
    const expected = r.before
      .replace("  --color-knob: #ffffff;\n", "")
      .replace(
        "  --color-onboarding-fg: #5a3a16;\n}",
        "  --color-onboarding-fg: #5a3a16;\n  --color-new-slot: #123456;\n}"
      );
    expect(r.css).toBe(expected);
  });

  it("leaves a declaration written inside a comment alone", () => {
    const r = run({
      css: (css) => css.replace(":root {\n", ":root {\n  /* --color-bg: #ffffff; */\n"),
    });
    expect(r.code).toBe(0);
    expect(r.css).toContain(":root {\n  /* --color-bg: #ffffff; */\n  --color-bg: #191c24;\n");
  });

  it("refuses an alias outside the default theme, naming the token", () => {
    const r = run({
      light: (t) => ({ ...t, colors: { ...t.colors, "--color-card": "var(--color-bg)" } }),
    });
    expect(r.code).toBe(1);
    expect(r.out).toContain("--color-card must be a concrete colour outside the default theme");
  });

  it("refuses a value carrying CSS structure", () => {
    const r = run({
      office: (t) => ({ ...t, colors: { ...t.colors, "--color-bg": "#000; } body { color: red" } }),
    });
    expect(r.code).toBe(1);
    expect(r.out).toContain("--color-bg carries CSS structure");
  });

  it("refuses a theme file whose id is not its built-in's", () => {
    const r = run({ light: (t) => ({ ...t, id: "office-dim" }) });
    expect(r.code).toBe(1);
    expect(r.out).toContain('id must be "office-light"');
  });

  it("refuses a non-colour token name", () => {
    const r = run({ office: (t) => ({ ...t, colors: { ...t.colors, "--font-sans": "serif" } }) });
    expect(r.code).toBe(1);
    expect(r.out).toContain('"--font-sans" is not a --color-* token name');
  });

  it("refuses a token declared twice in one block", () => {
    const r = run({
      css: (css) => css.replace("  --color-card: #242832;\n", "  --color-card: #242832;\n  --color-card: #242832;\n"),
    });
    expect(r.code).toBe(1);
    expect(r.out).toContain('--color-card is declared twice in ":root"');
  });
});
