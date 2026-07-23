#!/usr/bin/env node
// T-16a1 P1 — CSS colour-token lint (the geometry that keeps the theme layer
// whole). A theme = re-valuing the semantic tokens in styles/theme.css; that
// only works if every theme-surface colour flows THROUGH a token. So component
// CSS must never hard-code a raw colour literal (#hex / rgb() / rgba() / hsl()):
// a hard-coded colour is invisible to the theme switch and to user-defined
// themes, and it is exactly how a new theme sprouts un-restyled patches.
//
// This lint scans frontend/src/**/*.css EXCEPT the token layer itself
// (styles/theme.css, where the literals legitimately live) and fails on any raw
// colour literal outside a /* comment */. The house pattern for a tokenised
// colour-with-alpha is `color-mix(in srgb, var(--token) N%, transparent)` — that
// carries no literal and passes. Fix a violation by adding/rerouting through a
// semantic token in theme.css, never by silencing it here.
//
// ALLOWLIST is intentionally empty: P1 migrated the whole surface. A genuinely
// theme-invariant exception must be added here WITH a one-line justification —
// treat every addition as debt, not as an escape hatch.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, relative } from "node:path";

const SRC = join(dirname(fileURLToPath(import.meta.url)), "..", "src");

// The one file where raw colour literals are the point (token definitions).
const EXCLUDE = new Set(["styles/theme.css"]);

// { file: "<src-relative path>", line: <n>, reason: "<why invariant>" } — empty.
const ALLOWLIST = [];

// Raw colour literals a theme switch cannot reach.
//  - #rgb / #rgba / #rrggbb / #rrggbbaa hex
//  - rgb()/rgba()/hsl()/hsla() functional notation
// color-mix(... var(--x) N% ...) carries none of these and is fine.
const HEX = /#[0-9a-fA-F]{3,8}\b/;
const FUNC = /\b(?:rgba?|hsla?)\s*\(/;

/** Strip /* *​/ comments while preserving line count, so reported line numbers
 *  match the source. Returns the comment-stripped text line-for-line. */
function stripComments(text) {
  let out = "";
  let inComment = false;
  for (let i = 0; i < text.length; i++) {
    if (inComment) {
      if (text[i] === "*" && text[i + 1] === "/") {
        inComment = false;
        i++;
      } else if (text[i] === "\n") {
        out += "\n"; // keep newlines so line numbers stay aligned
      }
      continue;
    }
    if (text[i] === "/" && text[i + 1] === "*") {
      inComment = true;
      i++;
      continue;
    }
    out += text[i];
  }
  return out;
}

function collectCss(dir, acc) {
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    const st = statSync(full);
    if (st.isDirectory()) collectCss(full, acc);
    else if (name.endsWith(".css")) acc.push(full);
  }
  return acc;
}

// Tokens actually defined in the theme layer. A `var(--x)` with NO fallback
// pointing at a name absent here is a DEAD declaration (invalid at compute time
// → the property silently drops to its initial value) — the exact class of bug
// a raw-literal scan can't see. Only the no-fallback form is flagged; a
// `var(--x, <fallback>)` is intentional and still renders, so it is left alone.
const themeText = readFileSync(join(SRC, "styles", "theme.css"), "utf8");
const DEFINED = new Set(
  [...themeText.matchAll(/(--(?:color|radius|font)-[a-z0-9-]+)\s*:/g)].map((m) => m[1])
);
// var(--token) with the closing paren right after the name = no fallback.
const VAR_NO_FALLBACK = /var\(\s*(--(?:color|radius|font)-[a-z0-9-]+)\s*\)/g;

const literals = [];
const deadRefs = [];
for (const file of collectCss(SRC, [])) {
  const rel = relative(SRC, file).split("\\").join("/");
  if (EXCLUDE.has(rel)) continue;
  const stripped = stripComments(readFileSync(file, "utf8"));
  stripped.split("\n").forEach((line, idx) => {
    const lineNo = idx + 1;
    if ((HEX.test(line) || FUNC.test(line)) &&
        !ALLOWLIST.some((a) => a.file === rel && a.line === lineNo)) {
      literals.push({ rel, lineNo, text: line.trim() });
    }
    for (const m of line.matchAll(VAR_NO_FALLBACK)) {
      if (!DEFINED.has(m[1])) deadRefs.push({ rel, lineNo, token: m[1], text: line.trim() });
    }
  });
}

if (literals.length || deadRefs.length) {
  if (literals.length) {
    console.error(
      `\n[css-tokens] ${literals.length} raw colour literal(s) outside the token layer ` +
        `(styles/theme.css). Route the colour through a semantic token in theme.css ` +
        `(or color-mix(in srgb, var(--token) N%, transparent) for alpha):\n`
    );
    for (const v of literals) console.error(`  ${v.rel}:${v.lineNo}  ${v.text}`);
  }
  if (deadRefs.length) {
    console.error(
      `\n[css-tokens] ${deadRefs.length} var(--token) reference(s) to a token NOT defined in ` +
        `styles/theme.css and with no fallback (dead declaration — drops to initial). ` +
        `Define the token in theme.css or point at an existing one:\n`
    );
    for (const v of deadRefs) console.error(`  ${v.rel}:${v.lineNo}  ${v.token}   ${v.text}`);
  }
  console.error("");
  process.exit(1);
}

console.log("[css-tokens] ok — no raw colour literals, no dead token refs outside the token layer.");
