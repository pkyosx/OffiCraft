#!/usr/bin/env node
// T-16a1 P2 — theme colour-token whitelist generator (the single source of
// truth for "which --color-* names a user theme bundle may re-value").
//
// styles/theme.css IS the token contract (the P1 lint already enforces that
// every theme-surface colour flows through a --color-* token defined there).
// Its built-in blocks' --color-* lines are themselves generated from
// themes/<id>.theme.json by gen-builtin-themes.mjs, which runs first.
// A user-imported / user-authored theme bundle is `{ "--color-x": "<value>" }`;
// its KEY set must be exactly this file's --color-* names — never a hand-kept
// second list that silently drifts. So we EXTRACT the names here (same regex
// the P1 css-token lint uses) and emit two committed generated files:
//
//   * frontend/src/styles/themeTokens.generated.ts — THEME_COLOR_TOKENS, the
//     client + mock validation whitelist.
//   * server/ocserverd/theme_colornames_gen.go — themeColorTokens, the server
//     validation whitelist.
//
// Both are checked into the tree and pinned by a CI drift gate (bin/ci.sh):
// change theme.css's --color-* set without re-running `npm run gen:tokens`
// and CI goes red. Run: `npm run gen:tokens` (or `node scripts/gen-theme-tokens.mjs`).

import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, "..", "..");
// GEN_THEME_TOKENS_SRC / _OUT_DIR re-point the input and the two outputs — the
// ONLY reason they exist is gen-theme-tokens.test.ts, which runs the generator
// over a sabotaged copy of theme.css and watches what it emits (and what it
// refuses). A generator whose refusal nobody has watched is not a refusal.
const THEME_CSS =
  process.env.GEN_THEME_TOKENS_SRC ??
  join(ROOT, "frontend", "src", "styles", "theme.css");
const OUT_DIR = process.env.GEN_THEME_TOKENS_OUT_DIR;
const TS_OUT = OUT_DIR
  ? join(OUT_DIR, "themeTokens.generated.ts")
  : join(ROOT, "frontend", "src", "styles", "themeTokens.generated.ts");
const GO_OUT = OUT_DIR
  ? join(OUT_DIR, "theme_colornames_gen.go")
  : join(ROOT, "server", "ocserverd", "theme_colornames_gen.go");

// The SAME extraction regex the P1 css-token lint uses for --color-* definitions
// (a token DECLARATION is `--color-name:`). sort + uniq gives a stable set
// regardless of definition order or repetition across the :root / office-light blocks.
// Comments are stripped first (as the two lints do): this file is full of
// token-naming prose, and a `--color-x: ...` inside a /* */ would otherwise mint
// a phantom token — and, worse for the alias scan below, a phantom alias.
const css = readFileSync(THEME_CSS, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

// EVERY --color-* token theme.css defines is pack-settable — the whitelist is
// the token set, with no exclusions (T-081b round 8, owner ruling). There WAS a
// reserved --color-marker-* family whose values a pack could not re-value (the
// colour of the 內建 / 自訂 group headings), kept out of the whitelist here. It
// was valued for the built-in dark theme, so a light pack left the headings at
// 1.98:1 — unreadable for everyone, not only for a pack out to hide them. The
// owner chose to let a pack decide the heading's colour, so the family, this
// exclusion and its guards are gone: no name is special any more, and an
// exclusion mechanism nothing excludes is a guard that can never fire.
//
// What still cannot be forged is neither the colour NOR the heading's text —
// `themeMarkers.*` went back to being ordinary overridable wording in the same
// round. It is WHICH GROUP A ROW LANDS IN: that is decided by the render, not by
// anything a pack ships, so a pack that renames itself 辦公室 and re-words the
// headings still cannot move its own row into the built-in group. The one string
// held back from packs is the built-in theme's own name (`themeIdentity.*`,
// excluded in gen-message-keys.mjs), so the shipped row keeps saying 辦公室.
const tokens = [
  ...new Set([...css.matchAll(/(--color-[a-z0-9-]+)\s*:/g)].map((m) => m[1])),
].sort();

// ALIAS DEFAULTS — a token whose definition is nothing but `var(--other)`.
// These are not colours: they are "inherit from that one unless you say
// otherwise" slots (the T-081b zone tokens --color-topbar-bg / --color-nav-bg /
// --color-main-bg). getComputedStyle RESOLVES them, so an exported bundle would
// bake the concrete built-in colour in and freeze the layering forever — the
// export path has to know which tokens are alias defaults, and it must learn it
// from theme.css rather than from a hand-kept list of three names.
const aliasDefaults = {};
for (const [, tok, value] of css.matchAll(/(--color-[a-z0-9-]+)\s*:\s*([^;}]+)/g)) {
  const m = /^var\(\s*(--[a-z0-9-]+)\s*\)$/.exec(value.trim());
  if (m) aliasDefaults[tok] = m[1];
}
const aliasNames = Object.keys(aliasDefaults).sort();

if (tokens.length === 0) {
  console.error("[gen-theme-tokens] no --color-* tokens found in theme.css — aborting");
  process.exit(1);
}

const banner = (tool) =>
  `// Code generated by frontend/scripts/gen-theme-tokens.mjs — DO NOT EDIT.\n` +
  `// Source of truth: frontend/src/styles/theme.css (the --color-* token set).\n` +
  `// Regenerate: npm run gen:tokens (${tool}).\n`;

const ts =
  banner("this file") +
  `\n/** The whitelist of --color-* token names a user theme bundle may re-value.\n` +
  ` * Extracted from theme.css; the single source of truth for both the client\n` +
  ` * theme-bundle validator (lib/themeBundle.ts) and the mock API parity check. */\n` +
  `export const THEME_COLOR_TOKENS: readonly string[] = [\n` +
  tokens.map((t) => `  ${JSON.stringify(t)},`).join("\n") +
  `\n];\n` +
  `\n/** The tokens whose theme.css definition is a pure \`var(--other)\` alias —\n` +
  ` * i.e. "follow that token unless a theme says otherwise". Maps the alias to\n` +
  ` * the token it defers to. Export uses this to tell an UNSET alias apart from\n` +
  ` * a deliberately chosen colour: getComputedStyle resolves the var(), so\n` +
  ` * without it every exported bundle would bake the inherited colour in and the\n` +
  ` * deferral would be lost for good. */\n` +
  `export const THEME_ALIAS_DEFAULT_TOKENS: Readonly<Record<string, string>> = {\n` +
  aliasNames
    .map((t) => `  ${JSON.stringify(t)}: ${JSON.stringify(aliasDefaults[t])},`)
    .join("\n") +
  `\n};\n`;

// gofmt aligns the values of a map literal (tabwriter, padding 1). Replicate
// it here so the emitted file is gofmt-clean and the CI drift gate stays byte-
// stable: pad each `"key":` out to the widest one, then a single space + value.
const keyParts = tokens.map((t) => `${JSON.stringify(t)}:`);
const widest = Math.max(...keyParts.map((k) => k.length));
const go =
  banner("server/ocserverd/theme_colornames_gen.go") +
  `\npackage main\n\n` +
  `// themeColorTokens is the whitelist of --color-* token names a user theme\n` +
  `// bundle may re-value (theme_bundle.go validation). Extracted from\n` +
  `// frontend/src/styles/theme.css — the single token contract.\n` +
  `var themeColorTokens = map[string]bool{\n` +
  keyParts.map((k) => `\t${k}${" ".repeat(widest - k.length + 1)}true,`).join("\n") +
  `\n}\n`;

writeFileSync(TS_OUT, ts);
writeFileSync(GO_OUT, go);
console.log(`[gen-theme-tokens] wrote ${tokens.length} tokens →\n  ${TS_OUT}\n  ${GO_OUT}`);
