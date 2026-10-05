#!/usr/bin/env node
// T-081b — token ROLE lint (keeps the split tokens split).
//
// Three colour tokens used to carry two semantically OPPOSITE jobs each, so no
// single value could satisfy both and every light theme broke on the same wall:
//
//   --color-overlay  translucent veil base      ×  opaque foreground on a fill
//   --color-shadow   box-shadow ink             ×  sunken-surface / backdrop wash
//   --color-indigo   saturated fill under text  ×  scrollbar thumb vs the page
//
// T-081b split the second job of each into its own token. Nothing stops a later
// change from quietly routing a new call site back through the original token —
// and that re-merge is INVISIBLE in the built-in dark theme (where both jobs
// happen to agree), so it would ship and only break users on a light theme.
// This lint pins the partition so the re-merge fails in CI instead.
//
// The invariant per token is "it may only appear in ONE syntactic form":
//   * --color-overlay  → only inside color-mix(..., transparent)
//   * --color-shadow   → only inside a box-shadow declaration
//   * --color-indigo   → never as a scrollbar colour
// plus: each token split out must stay a real independent value (not aliased
// back to the token it came from) and must still be used somewhere (a token
// nobody references means the call sites were reverted).
//
// A re-merge is only ever true of the COMPUTED value, so neither the checks nor
// the parser may work on surface text. The CSS is parsed brace-aware and the
// enclosing selector is a first-class part of a declaration (`background`
// inside `::-webkit-scrollbar-thumb` IS a scrollbar colour), and every value is
// first expanded through the token graph, so an alias hop of any length lands
// on the same verdict as writing the parent token directly.
//
// Run: `npm run lint:token-roles` (also wired into bin/ci.sh).

import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, relative } from "node:path";

// TOKEN_ROLES_SRC re-points the scanned tree — the ONLY reason it exists is
// check-token-roles.test.ts, which copies the real stylesheets to a temp dir,
// sabotages one of them and asserts this script goes red. A guard nobody has
// watched fail is not a guard.
const SRC = process.env.TOKEN_ROLES_SRC
  ? process.env.TOKEN_ROLES_SRC
  : join(dirname(fileURLToPath(import.meta.url)), "..", "src");
const THEME = "styles/theme.css";

// Tokens split out by T-081b → the token each was carved from.
const SPLIT_FROM = {
  "--color-on-danger": "--color-overlay",
  "--color-on-indigo": "--color-overlay",
  "--color-on-backdrop": "--color-overlay",
  "--color-knob": "--color-overlay",
  "--color-surface-sunken": "--color-shadow",
  "--color-backdrop": "--color-shadow",
  "--color-scrollbar-thumb": "--color-indigo",
};
const PARENTS = new Set(Object.values(SPLIT_FROM));
// Expansion STOPS at these names. Parents are opaque because naming one is
// exactly what the rules below look for. Split tokens are opaque because naming
// one IS the correct call site — its own default may legitimately alias the
// parent (that alias is what keeps already-imported light packs working, see the
// note at the bottom of this file), and expanding through it would turn every
// correct call site into a false "you used the parent" violation.
const OPAQUE = new Set([...PARENTS, ...Object.keys(SPLIT_FROM)]);

function stripComments(text) {
  let out = "";
  let inComment = false;
  for (let i = 0; i < text.length; i++) {
    if (inComment) {
      if (text[i] === "*" && text[i + 1] === "/") {
        inComment = false;
        i++;
      } else if (text[i] === "\n") out += "\n";
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
    if (statSync(full).isDirectory()) collectCss(full, acc);
    else if (name.endsWith(".css")) acc.push(full);
  }
  return acc;
}

/** Split on top-level commas; commas nested in parens stay put. */
function splitTop(text) {
  const parts = [];
  let depth = 0;
  let buf = "";
  for (const c of text) {
    if (c === "(") depth++;
    else if (c === ")") depth--;
    if (c === "," && depth === 0) {
      parts.push(buf);
      buf = "";
    } else buf += c;
  }
  parts.push(buf);
  return parts;
}

/** Brace-aware parse → { selector, prop, value, rel, lineNo }. `selector` is the
 *  whole enclosing prelude chain, so an @media wrapper cannot hide a rule and a
 *  ::-webkit-scrollbar-thumb block stays recognisable from any declaration in
 *  it, first or last. Declarations are cut on `;`/`}` at paren depth 0 only, so
 *  a multi-line box-shadow and a `url(a;b)` both survive intact. */
function declarations(files) {
  const out = [];
  for (const file of files) {
    const rel = relative(SRC, file).split("\\").join("/");
    const text = stripComments(readFileSync(file, "utf8"));
    const stack = [];
    let buf = "";
    let bufLine = 1;
    let line = 1;
    let depth = 0;
    let quote = "";

    const flush = () => {
      const decl = buf.replace(/\s+/g, " ").trim();
      buf = "";
      const colon = decl.indexOf(":");
      if (colon <= 0 || !stack.length) return;
      out.push({
        selector: stack.join(" "),
        prop: decl.slice(0, colon).trim(),
        value: decl.slice(colon + 1).trim(),
        rel,
        lineNo: bufLine,
      });
    };

    for (let i = 0; i < text.length; i++) {
      const c = text[i];
      if (c === "\n") line++;
      if (quote) {
        buf += c;
        if (c === quote && text[i - 1] !== "\\") quote = "";
        continue;
      }
      if (c === '"' || c === "'") {
        quote = c;
        buf += c;
        continue;
      }
      if (c === "(") depth++;
      else if (c === ")") depth = Math.max(0, depth - 1);
      if (depth === 0) {
        if (c === "{") {
          stack.push(buf.replace(/\s+/g, " ").trim());
          buf = "";
          continue;
        }
        if (c === "}") {
          flush();
          stack.pop();
          continue;
        }
        if (c === ";") {
          flush();
          continue;
        }
      }
      if (!buf && /\s/.test(c)) continue;
      if (!buf) bufLine = line;
      buf += c;
    }
  }
  return out;
}

const isDef = (d) => d.prop.startsWith("--");

/** Every custom property → the value(s) declared for it, in any file. */
function tokenDefs(decls) {
  const defs = new Map();
  for (const d of decls) {
    if (!isDef(d)) continue;
    if (!defs.has(d.prop)) defs.set(d.prop, []);
    defs.get(d.prop).push(d.value);
  }
  return defs;
}

const refsOf = (value) => [...value.matchAll(/var\(\s*(--[\w-]+)/gi)].map((m) => m[1]);

/** Substitute every var() by its definition, EXCEPT the three parent tokens —
 *  those stay visible as names, in whatever syntactic position the alias chain
 *  actually put them. A fallback is a possible value too, so
 *  `var(--split, var(--parent))` still exposes the parent. */
// Set by expandVars when it hits the hop limit. A chain that deep is either a
// mistake or someone hiding a re-merge behind indirection; either way the answer
// is NOT "looks clean" — the caller turns this into a violation (fail CLOSED).
let aliasTooDeep = false;

function expandVars(value, defs, seen = new Set(), depth = 0) {
  if (depth > 8) {
    aliasTooDeep = true;
    return value;
  }
  const lower = value.toLowerCase();
  let out = "";
  let i = 0;
  while (i < value.length) {
    const at = lower.indexOf("var(", i);
    if (at < 0) return out + value.slice(i);
    out += value.slice(i, at);
    let d = 0;
    let j = at + 3;
    for (; j < value.length; j++) {
      if (value[j] === "(") d++;
      else if (value[j] === ")" && --d === 0) break;
    }
    const args = splitTop(value.slice(at + 4, j));
    const token = args[0].trim();
    const fallback = args.slice(1).join(",").trim();
    const next = new Set(seen).add(token);
    const fb = fallback ? expandVars(fallback, defs, next, depth + 1) : "";
    if (OPAQUE.has(token) || seen.has(token) || !defs.has(token)) {
      out += `var(${token}${fb ? ", " + fb : ""})`;
    } else {
      out += defs.get(token).map((v) => expandVars(v, defs, next, depth + 1)).join(" ");
      if (fb) out += " " + fb;
    }
    i = j + 1;
  }
  return out;
}

/** Shortest hop chain from something the author actually wrote to `parent`.
 *  Without it the message points at `var(--color-ink)` and explains nothing. */
function aliasChain(value, parent, defs) {
  const queue = refsOf(value).filter((t) => t !== parent).map((t) => [t]);
  const seen = new Set(queue.map((q) => q[0]));
  while (queue.length) {
    const path = queue.shift();
    for (const v of defs.get(path[path.length - 1]) || []) {
      for (const t of refsOf(v)) {
        if (t === parent) return [...path, parent];
        if (!seen.has(t)) {
          seen.add(t);
          queue.push([...path, t]);
        }
      }
    }
  }
  return null;
}

const usesRaw = (value, token) => new RegExp(`var\\(\\s*${token}\\s*[,)]`, "i").test(value);

/** Every color-mix(...) span, nested ones included. */
function colorMixSpans(value) {
  const spans = [];
  for (const m of value.matchAll(/color-mix\s*\(/gi)) {
    let d = 0;
    let j = m.index + m[0].length - 1;
    for (; j < value.length; j++) {
      if (value[j] === "(") d++;
      else if (value[j] === ")" && --d === 0) break;
    }
    spans.push({ start: m.index, end: j + 1, inner: value.slice(m.index + m[0].length, j) });
  }
  return spans;
}

/** The veil form: color-mix(in srgb, var(--token) N%, transparent). Both halves
 *  matter — a reference outside any color-mix is the opaque job, and so is a
 *  color-mix whose other colour is not transparent (100% toward #000 is the
 *  token itself, wearing a veil's clothes). */
function veilOnly(value, token) {
  const spans = colorMixSpans(value);
  for (const span of spans) {
    if (!usesRaw(span.inner, token)) continue;
    const args = splitTop(span.inner);
    const last = args[args.length - 1].trim();
    const m = /^transparent(?:\s+([\d.]+)%)?$/i.exec(last);
    if (!m) return false;
    // A color-mix WITH transparent is not automatically a veil: the percentages
    // decide. `token 100%, transparent` and `token, transparent 0%` are both
    // fully opaque — the opaque-foreground job wearing veil syntax, which is
    // exactly the re-merge this lint exists to catch.
    if (m[1] !== undefined && Number(m[1]) <= 0) return false;
    const tokenArg = args.find((a) => usesRaw(a, token));
    const pct = tokenArg && /([\d.]+)%\s*$/.exec(tokenArg.trim());
    if (pct && Number(pct[1]) >= 100) return false;
  }
  let outside = value;
  for (const span of spans) {
    outside =
      outside.slice(0, span.start) + " ".repeat(span.end - span.start) + outside.slice(span.end);
  }
  return !usesRaw(outside, token);
}

// Which carved-out token the author most likely wanted, read off the call site.
const OVERLAY_FIX = [
  [/danger|error|unread/i, "--color-on-danger"],
  [/backdrop|lightbox|preview|scrim/i, "--color-on-backdrop"],
  [/knob|switch|toggle/i, "--color-knob"],
  [/indigo|badge|send|seg|active|tab/i, "--color-on-indigo"],
];
const overlayFix = (d) => {
  const hit = OVERLAY_FIX.find(([re]) => re.test(`${d.selector} ${d.prop}`));
  return hit
    ? `Use ${hit[1]}.`
    : "Use --color-on-danger / --color-on-indigo / --color-on-backdrop / --color-knob.";
};
const shadowFix = (d) =>
  /backdrop|lightbox|preview|scrim|modal/i.test(`${d.selector} ${d.prop}`)
    ? "Use --color-backdrop (lightbox/preview scrim)."
    : "Use --color-surface-sunken (sunken surface), or --color-backdrop for a lightbox/preview scrim.";

const files = collectCss(SRC, []);
const decls = declarations(files);
const defs = tokenDefs(decls);
const violations = [];

const flag = (d, parent, why) => {
  const chain = usesRaw(d.value, parent) ? null : aliasChain(d.value, parent, defs);
  if (!chain) return violations.push({ ...d, why });
  // A call site that reaches the parent only because the SPLIT token it uses was
  // aliased is innocent — send the reader to the definition, not to this line.
  const via = `(reached via ${chain.join(" → ")})`;
  violations.push({
    ...d,
    why: SPLIT_FROM[chain[0]] === parent
      ? `${chain[0]} now resolves to ${parent} ${via}; this call site is correct — undo the alias in ${THEME}.`
      : `${why} ${via}`,
  });
};

for (const d of decls) {
  // A custom property is a definition, not a use: which role its value plays is
  // decided wherever it is consumed, and that call site is checked on its own
  // (its value is expanded through this definition). Aliasing a SPLIT token back
  // to its parent is the separate, always-wrong case checked further down.
  if (isDef(d)) continue;
  aliasTooDeep = false;
  const value = expandVars(d.value, defs);
  if (aliasTooDeep) {
    flag(
      d,
      "(alias chain)",
      "alias chain is deeper than 8 hops, so this value cannot be resolved to a " +
        "token — flatten it; an unresolvable value cannot be shown to keep the split."
    );
  }

  // --color-overlay: veil base only. A bare var(--color-overlay) as a colour is
  // the opaque-foreground job that T-081b moved to --color-on-*/--color-knob.
  if (usesRaw(value, "--color-overlay") && !veilOnly(value, "--color-overlay")) {
    flag(
      d,
      "--color-overlay",
      "--color-overlay used as an opaque colour; it is the translucent veil base " +
        `only (color-mix(..., transparent)). ${overlayFix(d)}`
    );
  }

  // --color-shadow: box-shadow ink only. Any other property means it is being
  // used as a surface wash again → --color-surface-sunken / --color-backdrop.
  if (usesRaw(value, "--color-shadow") && !/(^|-)box-shadow$/.test(d.prop)) {
    flag(
      d,
      "--color-shadow",
      `--color-shadow used on '${d.prop}'; it is the box-shadow ink only. ${shadowFix(d)}`
    );
  }

  // --color-indigo: never the scrollbar. Its contrast partner is the text on
  // top of it, not the page behind it — and the scrollbar job lives as much in
  // the selector (::-webkit-scrollbar-thumb { background }) as in the property
  // (scrollbar-color), so both have to be looked at.
  if (usesRaw(value, "--color-indigo") && /scrollbar/i.test(`${d.selector} ${d.prop}`)) {
    flag(d, "--color-indigo", "--color-indigo used as a scrollbar colour; use --color-scrollbar-thumb.");
  }
}

// The split tokens must stay independent values, and must stay in use.
for (const [token, parent] of Object.entries(SPLIT_FROM)) {
  const def = decls.find((d) => d.rel === THEME && d.prop === token);
  if (!def) {
    violations.push({
      rel: THEME, lineNo: 0, prop: token, value: "(missing)",
      why: `${token} is no longer defined — T-081b split it out of ${parent}; removing it re-merges the two jobs.`,
    });
    continue;
  }
  // NOTE (T-081b, review round 2 / lens-A B1): the DEFAULT deliberately aliases
  // the parent — `--color-knob: var(--color-overlay)` and friends. That is not a
  // re-merge: a theme that names the split token overrides the alias, so the two
  // jobs stay independently settable, which is the whole point. It is also the
  // only thing that keeps ALREADY-IMPORTED light packs working: such a pack
  // overrides the parent (e.g. --color-overlay: #000) and, with a literal
  // default here, every moved call site would silently snap back to the built-in
  // dark value — measured 16.28:1 → 1.29:1 on the webhook badge, i.e. this
  // ticket's own bug re-introduced by upgrading. What must still fail is a CALL
  // SITE reverting to the parent (the per-declaration rules above) or the split
  // token falling out of use (the check below) — those are the real re-merges.
  if (!decls.some((d) => d.prop !== token && usesRaw(d.value, token))) {
    violations.push({
      ...def,
      why: `${token} is defined but referenced nowhere — its call sites were reverted to ${parent}.`,
    });
  }
}

// ── The unread badge must clear WCAG AA ──────────────────────────────────────
// The same partition idea one level down. The three unread pills used to be
// painted --color-danger (#f0736b) and carry the count in white: 2.85:1, below
// the 4.5:1 AA floor for normal text. --color-danger could not simply be pressed
// darker — it is also the FOREGROUND red of the error text and the logout row,
// where darker means LESS readable on the dark cockpit. So the badge FILL got
// its own slot, and the ratio is asserted here rather than eyeballed: a later
// re-value of either slot that drops below AA fails CI instead of shipping.
const BADGE_FILL = "--color-danger-badge";
const BADGE_TEXT = "--color-on-danger";
// The pill's 1px ring. It exists because the pill's background is NOT the page:
// on an active nav tab it is --color-indigo (2.74:1 against the fill) and on a
// selected member card --color-card (3.26:1), and no fill colour clears 4.5:1
// against the text AND 3:1 against all of those. So the ring is what separates
// the pill from whatever is behind it (T-081b review round 3, SHOULD-4).
//
// T-d593: the ring now has its OWN theme slot instead of borrowing --color-bg,
// and owner 2026-08-01 (rc-1d57d0adc87d 選②) ruled it "外框完全自由,不留下限
// (主題調到看不見也算你的選擇)". So what is pinned here changed shape:
//   * STILL PINNED — every pill actually declares the ring token (below). That is
//     a WIRING check: it is what keeps all seven render sites on one slot, so a
//     theme author who repaints the ring repaints all of them.
//   * GONE — the ring-vs-fill CONTRAST FLOOR (the old MIN_PILL_VS_PAGE = 3).
//     A theme may now set the ring to the fill colour, or to transparent, and the
//     pill may stop reading as a pill. That is the owner's explicit trade.
// The count-vs-fill AA floor below is UNTOUCHED — the owner did not rule on it,
// and it is a separate checkRatio() call, so dropping one did not disturb it.
const BADGE_RING = "--color-danger-badge-ring";
const BADGE_SELECTORS = [".nav-tab__badge", ".office__tab-badge", ".member-card__unread"];
const AA_CONTRAST = 4.5;

/** Whether a selector PRELUDE targets a given element selector.
 *
 *  Round 4 found four bypasses that all reduce to the same mistake — treating
 *  the prelude as one string with `.split(" ").at(-1)`:
 *
 *    * `:root:root { … }`      compound, higher specificity, WINS on screen
 *    * `.nav-tab__badge.is-hot` compound, a live re-paint of the same element
 *    * `.nav-tab__badge, .zz`   a selector LIST — `.at(-1)` read `.zz`
 *    * `@media … { :root }`     an at-rule wrapper, which must still NOT count
 *
 *  So the question is asked per COMMA-SEPARATED selector, on its last compound
 *  (the subject), and the subject is compared by its PARTS: `:root:root` and
 *  `.nav-tab__badge.is-hot` both target the wanted element, `.zz` does not.
 *  `prelude` is the whole enclosing chain, so a rule nested in an at-rule still
 *  carries the at-rule text and is excluded by `atRuleFree` below — that is what
 *  keeps the round-3 `@media print` fix intact (SHOULD-3 C). */
function targets(prelude, wanted) {
  for (const one of splitTop(prelude)) {
    const subject = one.trim().split(/[\s>+~]+/).filter(Boolean).at(-1);
    if (!subject) continue;
    // Split a compound into its simple selectors: `.a.b`, `:root:root`,
    // `div.a:hover` → the parts, so a compound that CONTAINS the wanted
    // selector counts (it still matches the same element).
    const parts = subject.match(/(^[a-zA-Z*][\w-]*|[.#:][\w-]+(?:\([^)]*\))?)/g) ?? [];
    if (parts.includes(wanted)) return true;
  }
  return false;
}

/** A prelude chain holding nothing but selectors — no `@media` / `@supports` /
 *  `@layer` wrapper. A declaration inside an at-rule may or may not apply, so it
 *  can never be the measured truth (round 3, SHOULD-3 C). */
const atRuleFree = (prelude) => !/(^|\s)@/.test(prelude);

/** Every custom property DEFINED at `:root`, anywhere in the tree and under any
 *  `:root`-targeting selector, excluding at-rules.
 *
 *  Two things this must NOT do, both learned the hard way:
 *
 *    * it must not read the whole tree indiscriminately — a compliant value
 *      parked in `@media print { :root { … } }` made this script report 4.52:1
 *      while the screen showed 2.85:1 (round 3, SHOULD-3 C); hence `atRuleFree`.
 *    * it must not read theme.css ALONE, nor only the literal selector `:root`.
 *      Round 4 got a NON-compliant value onto the screen twice over: once with
 *      `:root:root` in theme.css and once with a plain `:root` in global.css.
 *      Both were invisible because the guard looked at one file and one exact
 *      string. So EVERY css file is scanned and `targets()` decides. */
const rootDefDecls = decls.filter(
  (d) => isDef(d) && atRuleFree(d.selector) && targets(d.selector, ":root")
);

// Built-in themes are theme.css blocks: office is the bare `:root`, and each
// other built-in is a `:root[data-theme="<id>"]` block layered over it, which is
// how i18n/index.tsx and prePaint.ts select one. Every block is measured on its
// own. A :root-targeting selector of any other shape (`:root:root`, a `:root` in
// another file's rule with a class, …) belongs to no theme and may win under all
// of them, so it counts as a candidate in every theme's tally below.
const THEME_BLOCK_RE = /^:root\[data-theme=["']([a-z0-9][a-z0-9-]*)["']\]$/;
const BASE_THEME = "office";
const scopeOf = (d) => {
  if (d.selector === ":root") return BASE_THEME;
  const m = d.rel === THEME ? THEME_BLOCK_RE.exec(d.selector) : null;
  return m ? m[1] : "*";
};
const BUILTIN_THEMES = [
  BASE_THEME,
  ...new Set(rootDefDecls.map(scopeOf).filter((s) => s !== BASE_THEME && s !== "*")),
];

/** The candidate definitions of `token` under built-in `theme`. A non-base block
 *  that leaves a token out inherits the base `:root` definition, exactly as the
 *  cascade does on screen. */
function candidates(token, theme) {
  const of = (scope) => rootDefDecls.filter((d) => d.prop === token && scopeOf(d) === scope);
  const own = theme === BASE_THEME ? of(BASE_THEME) : of(theme);
  const picked = own.length || theme === BASE_THEME ? own : of(BASE_THEME);
  return [...picked, ...of("*")];
}

/** Follow a token's definition under `theme` through any var() alias hops to a
 *  literal value. A var() resolves against the same theme, because the alias and
 *  its target are both custom properties on the one root element. */
function concreteValue(token, theme, hops = 0) {
  const value = candidates(token, theme).at(-1)?.value;
  if (value === undefined || hops > 8) return null;
  const alias = /^var\(\s*(--[\w-]+)\s*\)$/.exec(value.trim());
  return alias ? concreteValue(alias[1], theme, hops + 1) : value.trim();
}

// ── One candidate, so there is nothing to guess ──────────────────────────────
// Rounds 3, 4 and the round-4 recheck each caught this guard modelling the
// cascade with one more axis than the round before, and each round found a new
// axis it still did not model: `!important`, `:root:root:root` vs `:root:root`
// (a rank, not a specificity), and — worst — the assumption that theme.css is
// imported FIRST, which was written in a comment, derived from nothing, and
// silently inverted by anyone reordering two lines in main.tsx. The guard never
// read main.tsx at all.
//
// So it stops guessing. For every token the contrast floors are measured on
// (and every token those reach through an alias hop) there must be EXACTLY ONE
// at-rule-free candidate per built-in theme, and it must not carry `!important`.
// Then no cascade question exists inside a theme: one candidate cannot lose to
// another, whatever the specificity, the file or the import order.
const IMPORTANT_RE = /!\s*important\b/i;
const MEASURED_TOKENS = [BADGE_FILL, BADGE_TEXT, BADGE_RING];

/** The measured tokens plus everything their values alias through under
 *  `theme` — an ambiguous definition one hop down decides the colour just as
 *  completely. */
function aliasClosure(seeds, theme) {
  const out = new Set();
  const queue = [...seeds];
  while (queue.length) {
    const token = queue.shift();
    if (out.has(token)) continue;
    out.add(token);
    for (const d of candidates(token, theme)) queue.push(...refsOf(d.value));
  }
  return [...out];
}

const reported = new Set();
for (const theme of BUILTIN_THEMES) {
  for (const token of aliasClosure(MEASURED_TOKENS, theme)) {
    const defs = candidates(token, theme);
    const key = `${token} ${defs.map((d) => `${d.rel}:${d.lineNo}`).join(",")}`;
    if (reported.has(key)) continue;
    reported.add(key);
    if (defs.length > 1) {
      violations.push({
        ...defs[0],
        why:
          `${token} has ${defs.length} :root definitions under the ${theme} theme (` +
          defs.map((d) => `${d.rel}:${d.lineNo} { ${d.selector}`).join(", ") +
          `) — the unread badge's contrast floor is measured on this token, so ` +
          `exactly ONE may exist outside an at-rule. Which of several wins depends ` +
          `on specificity, !important and stylesheet load order; this guard ` +
          `deliberately models none of them, because every attempt to has been ` +
          `walked past. Delete all but one.`,
      });
    }
    for (const d of defs.filter((d) => IMPORTANT_RE.test(d.value))) {
      violations.push({
        ...d,
        why:
          `${token}'s :root definition carries !important — the contrast floor is ` +
          `measured on this token and !important changes which declaration wins ` +
          `without changing anything this guard can see. Remove it.`,
      });
    }
  }
}
function relativeLuminance(hex) {
  const m = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.exec(hex);
  if (!m) return null;
  const h = m[1].length === 3 ? [...m[1]].map((c) => c + c).join("") : m[1];
  const [r, g, b] = [0, 2, 4].map((i) => {
    const c = parseInt(h.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrastRatio(a, b) {
  const [la, lb] = [relativeLuminance(a), relativeLuminance(b)];
  if (la === null || lb === null) return null;
  const [hi, lo] = la > lb ? [la, lb] : [lb, la];
  return (hi + 0.05) / (lo + 0.05);
}

// NIT-9: the two badge tokens must be DEFINED in the theme's :root, like every
// token in SPLIT_FROM. Without this the ratios above are measured on a token
// whose definition could have moved anywhere in the tree (or into an at-rule),
// which is precisely how a compliant-looking number stopped describing the
// screen.
const badgeDefs = new Map(
  [BADGE_FILL, BADGE_TEXT].map((token) => [
    token,
    decls.find((d) => d.rel === THEME && d.selector === ":root" && d.prop === token),
  ])
);
for (const [token, def] of badgeDefs) {
  if (def) continue;
  violations.push({
    rel: THEME, lineNo: 0, prop: token, value: "(missing)",
    why:
      `${token} is not defined in the :root block of ${THEME} — the unread badge's ` +
      `contrast floor is measured on that definition, so it must live where the ` +
      `built-in theme actually reads it.`,
  });
}

const badgeDef = badgeDefs.get(BADGE_FILL);
const checkRatio = (theme, againstToken, floor, what) => {
  const ratio = contrastRatio(concreteValue(BADGE_FILL, theme), concreteValue(againstToken, theme));
  const at = candidates(BADGE_FILL, theme)[0] ?? badgeDef ?? {
    rel: THEME, lineNo: 0, prop: BADGE_FILL, value: "(missing)",
  };
  if (ratio === null) {
    violations.push({
      ...at,
      why:
        `[${theme}] ${BADGE_FILL} vs ${againstToken} cannot be measured — both must ` +
        `resolve to a plain #rgb/#rrggbb in ${THEME} for the contrast floor to mean ` +
        `anything.`,
    });
    return;
  }
  if (ratio < floor) {
    violations.push({
      ...at,
      why:
        `[${theme}] ${BADGE_FILL} vs ${againstToken} is ${ratio.toFixed(2)}:1, below the ` +
        `${floor}:1 floor — ${what}`,
    });
  }
};
for (const theme of BUILTIN_THEMES) {
  checkRatio(
    theme,
    BADGE_TEXT,
    AA_CONTRAST,
    `the unread count on the pill fails WCAG AA. Measured against ${BADGE_TEXT}, the ` +
      `colour the pill's own text is painted with (every ${BADGE_SELECTORS.length} pill ` +
      `selectors are checked below for actually using it).`
  );
}
// NOTE (T-d593): there used to be a second checkRatio() here asserting
// BADGE_FILL vs BADGE_RING >= 3:1 ("the pill still reads as a pill"). The owner
// removed that floor deliberately — see the BADGE_RING comment above. Do not add
// it back without a new ruling: a theme setting the ring to the fill colour is a
// SUPPORTED choice now, so this check would fail legitimate themes.

for (const selector of BADGE_SELECTORS) {
  // EVERY rule that TARGETS the selector, not the first one and not only the
  // ones whose prelude is that exact string. CSS gives the LAST declaration, so
  // a second `background` appended anywhere later in the tree (a theme variant
  // block, an @media, the end of the file) is what actually paints — checking
  // only the first let exactly that through (round 3, SHOULD-3 B). And the
  // prelude is not one string: `.nav-tab__badge, .zz { … }` and
  // `.nav-tab__badge.is-hot { … }` both re-paint this element while
  // `.split(" ").at(-1)` read `.zz` / `.nav-tab__badge.is-hot` and skipped the
  // rule entirely (round 4, SHOULD-B C and D). targets() asks the question per
  // comma-separated selector, on its subject compound's parts.
  const rules = decls.filter((d) => targets(d.selector, selector));
  // Both halves of the AA claim are pinned. The FILL was, the TEXT was not, so
  // the count colour could be swapped for a grey (measured ~1.9:1 on the fill)
  // while this script still printed "4.52:1 on text" (SHOULD-3 A). The RING is
  // the third: it is what makes the 3:1-vs-page number describe the screen.
  // Each entry is a PROPERTY FAMILY: the shorthand that must be present,
  // followed by the longhands that can repaint the same thing afterwards. A
  // shorthand-only check was bypassable — `outline: 1px solid var(--color-bg)`
  // followed by `outline-color: transparent` removes the ring on screen while
  // the guard, comparing `prop === "outline"`, still saw a ring (round 4,
  // SHOULD-B E). So the shorthand must EXIST and every member of the family must
  // carry the token.
  const required = [
    [["background", "background-color"], BADGE_FILL, `white on --color-danger is 2.85:1 and fails WCAG AA`],
    [["color"], BADGE_TEXT, `the pill's own count colour is what the AA ratio is measured on`],
    [["outline", "outline-color"], BADGE_RING, `every pill's ring must come from the ONE ring slot, or a theme author repainting it moves some pills and not others (there are 7 render sites behind these 3 selectors)`],
  ];
  for (const [props, token, why] of required) {
    const [primary] = props;
    const own = rules.filter((d) => props.includes(d.prop));
    if (!own.some((d) => d.prop === primary)) {
      violations.push({
        rel: THEME, lineNo: 0, prop: `${selector} { ${primary}`, value: "(missing)",
        why: `${selector} has no ${primary} declaration — ${why}.`,
      });
      continue;
    }
    for (const rule of own) {
      if (usesRaw(rule.value, token)) continue;
      violations.push({
        ...rule,
        why: `${selector}'s ${rule.prop} does not use ${token} — ${why}.`,
      });
    }
  }
}

if (violations.length) {
  console.error(
    `\n[token-roles] ${violations.length} violation(s) — a T-081b token split has been undone.\n` +
      `These re-merges look FINE in the built-in dark theme and break only on light themes,\n` +
      `which is why they are pinned here rather than left to review:\n`
  );
  for (const v of violations) {
    console.error(`  ${v.rel}:${v.lineNo}  ${v.selector ? v.selector + " { " : ""}${v.prop}: ${v.value}`);
    console.error(`      ${v.why}`);
  }
  console.error("");
  process.exit(1);
}

const ratioOf = (theme, token) =>
  contrastRatio(concreteValue(BADGE_FILL, theme), concreteValue(token, theme))?.toFixed(2);
console.log(
  `[token-roles] ok — 3 split tokens keep to one role each; ` +
    `${Object.keys(SPLIT_FROM).length} carved-out tokens defined independently and in use; ` +
    `BUILT-IN themes' unread badge: ` +
    BUILTIN_THEMES.map(
      (theme) =>
        `${theme} ${ratioOf(theme, BADGE_TEXT)}:1 vs ${BADGE_TEXT} / ` +
        `${ratioOf(theme, BADGE_RING)}:1 vs ${BADGE_RING} (its 1px ring)`
    ).join("; ") +
    `. ONLY THE FIRST RATIO IS PINNED — the ring's is ` +
    `printed for information since T-d593 (owner: 外框完全自由,不留下限), so a theme ` +
    `may drive it to 1:1. Both tokens are still pinned as the ring/text SLOTS at the ` +
    `${BADGE_SELECTORS.length} pill call sites. ` +
    `A theme pack may re-value ${BADGE_FILL}, so these numbers describe the built-in themes only.`
);
