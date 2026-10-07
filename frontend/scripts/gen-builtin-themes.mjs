#!/usr/bin/env node
// Writes the built-in themes' --color-* declarations in styles/theme.css from
// their theme files, themes/<id>.theme.json at the repo root — the one place a
// built-in colour is edited. Only the declarations inside each built-in block
// are rewritten: comments, spacing, trailing per-line comments and every
// non-colour line (canvas, radius, font, color-scheme) stay as written.
//
//   * a token in the file and the block gets the file's value;
//   * a token only in the file is added after the block's last --color-* line;
//   * a --color-* declaration only in the block is removed.
//
// theme.css stays what every other reader (gen-theme-tokens.mjs, the css-token
// lints, the contrast guards) reads, so the generated whitelists come out of
// the same text as before. Run: `npm run gen:builtin-themes`.
//
// GEN_BUILTIN_THEMES_DIR / _CSS / _OUT re-point the inputs and the output so
// gen-builtin-themes.test.ts can run this over sabotaged copies.

import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, "..", "..");
const THEMES_DIR = process.env.GEN_BUILTIN_THEMES_DIR ?? join(ROOT, "themes");
const THEME_CSS =
  process.env.GEN_BUILTIN_THEMES_CSS ?? join(ROOT, "frontend", "src", "styles", "theme.css");
const OUT = process.env.GEN_BUILTIN_THEMES_OUT ?? THEME_CSS;

// Picker order, twin of BUILTIN_THEMES in src/lib/themeBundleCore.ts (pinned by
// gen-builtin-themes.test.ts). The first one is the default: its block is the
// bare :root that also defines every token; the others override it under
// <html data-theme>.
const BUILTINS = [
  { id: "office", selector: ":root" },
  { id: "office-light", selector: ':root[data-theme="office-light"]' },
];

const TOKEN_RE = /^--color-[a-z0-9-]+$/;

function fail(msg) {
  console.error(`[gen-builtin-themes] ${msg}`);
  process.exit(1);
}

function readTheme({ id }, isDefault) {
  const path = join(THEMES_DIR, `${id}.theme.json`);
  let theme;
  try {
    theme = JSON.parse(readFileSync(path, "utf8"));
  } catch (e) {
    fail(`${path}: ${e.message}`);
  }
  if (typeof theme !== "object" || theme === null || Array.isArray(theme)) {
    fail(`${path}: must be a JSON object`);
  }
  if (theme.id !== id) fail(`${path}: id must be "${id}", got ${JSON.stringify(theme.id)}`);
  const colors = theme.colors;
  if (typeof colors !== "object" || colors === null || Array.isArray(colors)) {
    fail(`${path}: colors must be an object`);
  }
  const entries = Object.entries(colors);
  if (entries.length === 0) fail(`${path}: colors is empty`);
  for (const [tok, val] of entries) {
    if (!TOKEN_RE.test(tok)) fail(`${path}: "${tok}" is not a --color-* token name`);
    if (typeof val !== "string" || val.trim() === "" || val !== val.trim()) {
      fail(`${path}: ${tok} must be a non-empty, trimmed string`);
    }
    if (/[;{}\n]|\/\*|\*\//.test(val)) fail(`${path}: ${tok} carries CSS structure: ${val}`);
    // gen-theme-tokens.mjs collects var() aliases from EVERY block, so an alias
    // here would mint a phantom alias-default token for the whole whitelist.
    if (!isDefault && val.includes("var(")) {
      fail(`${path}: ${tok} must be a concrete colour outside the default theme, got ${val}`);
    }
  }
  return colors;
}

/** Comment bodies blanked to spaces, so offsets still line up with `css`. */
const mask = (css) => css.replace(/\/\*[\s\S]*?\*\//g, (c) => " ".repeat(c.length));

function blockRange(masked, selector) {
  const opener = new RegExp(
    `(^|\\n)${selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*\\{`,
    "g"
  );
  const hits = [...masked.matchAll(opener)];
  if (hits.length !== 1) fail(`theme.css: expected one "${selector} {" block, found ${hits.length}`);
  const start = hits[0].index + hits[0][0].length;
  const end = masked.indexOf("}", start);
  if (end === -1) fail(`theme.css: "${selector}" block is not closed`);
  return [start, end];
}

let css = readFileSync(THEME_CSS, "utf8");
const summary = [];

BUILTINS.forEach((builtin, i) => {
  const colors = readTheme(builtin, i === 0);
  const masked = mask(css);
  const [start, end] = blockRange(masked, builtin.selector);
  const body = masked.slice(start, end);

  const decls = [];
  const declRE = /(^|\n)([ \t]*)(--color-[a-z0-9-]+)([ \t]*:[ \t]*)([^;]*?)([ \t]*;)/g;
  for (const m of body.matchAll(declRE)) {
    const lineStart = start + m.index + m[1].length;
    const valueStart = lineStart + m[2].length + m[3].length + m[4].length;
    decls.push({
      token: m[3],
      lineStart,
      valueStart,
      valueEnd: valueStart + m[5].length,
    });
  }
  const seen = new Set();
  for (const d of decls) {
    if (seen.has(d.token)) fail(`theme.css: ${d.token} is declared twice in "${builtin.selector}"`);
    seen.add(d.token);
  }

  const edits = [];
  for (const d of decls) {
    if (Object.hasOwn(colors, d.token)) {
      edits.push({ from: d.valueStart, to: d.valueEnd, text: colors[d.token] });
    } else {
      const nl = css.indexOf("\n", d.lineStart);
      edits.push({ from: d.lineStart, to: nl === -1 ? css.length : nl + 1, text: "" });
    }
  }
  const added = Object.keys(colors).filter((t) => !seen.has(t));
  if (added.length > 0) {
    const last = decls[decls.length - 1];
    const at = last ? css.indexOf("\n", last.lineStart) + 1 : start + (css[start] === "\n" ? 1 : 0);
    edits.push({
      from: at,
      to: at,
      text: added.map((t) => `  ${t}: ${colors[t]};\n`).join(""),
    });
  }
  edits.sort((a, b) => b.from - a.from);
  for (const e of edits) css = css.slice(0, e.from) + e.text + css.slice(e.to);

  summary.push(`${builtin.id} (${builtin.selector}) ${Object.keys(colors).length} tokens`);
});

writeFileSync(OUT, css);
console.log(`[gen-builtin-themes] ${summary.join(", ")} → ${OUT}`);
