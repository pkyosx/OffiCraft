#!/usr/bin/env node
// T-48 R13-5 — `<ChatArea>` is mounted PER CONVERSATION, and this is what keeps
// it that way.
//
// 🔴 WHAT THIS GUARDS, AND WHY IT IS WORTH A CI STEP OF ITS OWN.
//
// `OfficePage` renders `<ChatArea>` from three branches of ONE conditional
// expression. Without a `key`, React reuses a single component instance across
// every conversation, so a switch is nothing but a prop change: the message
// list, the composer, the jump reactor, the read watermark, every latch in
// `useChat`, every in-flight fetch and every open overlay carry straight over
// into a room they do not belong to.
//
// Twelve rounds of review found twelve instances of exactly that, and each was
// answered by re-implementing `key` a little further inside the component: a
// visit token, `useKeyedState`, `useKeyedRecord`, a `messagesPeer` stamp on the
// thread, a `PeerLastRead` object, a `target` stamp on every staged file, a
// machine-maintained census of everything that had to be keyed. All of it
// existed to make one reused instance behave like one instance per conversation
// — which is what a `key` means. R13-5 deleted the lot and added the key.
//
// So the key is now load-bearing for a dozen separate behaviours, and NOTHING
// ELSE GOES RED IF IT IS REMOVED. The component tests supply their own keys (a
// test that renders `<ChatArea>` directly is choosing its own lifetime); the
// behaviour they assert stays true under those keys no matter what `OfficePage`
// does. That is the gap this file exists to close, and it is the reason it is a
// lint rather than a test: it is a rule about how a component is MOUNTED, which
// is source text, not behaviour.
//
// The rule has two halves, and the second one exists because the first is not
// enough:
//
//   (a) every `<ChatArea` in the app's own `.tsx` carries a `key=` prop;
//   (b) that key is an expression ending in `.id`, whose root identifier also
//       appears elsewhere on the same element.
//
// (a) alone is what a review asked for and it is NOT sufficient: `key="chat"`
// satisfies it while being the same thing as no key at all — one constant key
// means one instance, and all twelve behaviours come back with nothing red. (b)
// is deliberately shallow: it proves the key VARIES WITH SOMETHING THIS ELEMENT
// ALREADY KNOWS ABOUT, not that the something is the right peer. Proving that
// needs types, and a lint that reaches for types is a lint that breaks on a
// rename. What (b) buys is that the two edits which silently disable the key —
// a literal, and an id borrowed from nowhere on this element — are no longer
// silent.
//
// ⚠️ A BARE VARIABLE (`key={peerId}`) IS REFUSED ON PURPOSE, and it is the one
// thing this rule cannot do better: from source text alone `key={peerId}` and
// `key={CHAT_KEY}` are the same shape, and one of them is the defect. So the key
// has to name the room through something the element also holds — write
// `key={member.id}` beside `member={member}`. The failure message says exactly
// that, and must keep naming a form this rule actually accepts.
//
// Run: `npm run lint:chat-area-key` (also wired into bin/ci.sh).

import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// CHAT_AREA_KEY_SRC re-points the scanned tree — the ONLY reason it exists is
// check-chat-area-key.test.ts, which copies the real sources to a temp dir,
// removes a key and asserts this script goes red.
const SRC = process.env.CHAT_AREA_KEY_SRC
  ? resolve(process.env.CHAT_AREA_KEY_SRC)
  : resolve(dirname(fileURLToPath(import.meta.url)), "..", "src");

function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    // 🔴 PRODUCT CODE ONLY. A test that renders `<ChatArea>` directly is
    // choosing the component lifetime it wants to drive — several deliberately
    // drive a bare rerender to show what the key BUYS. The rule here is about
    // how the APP mounts it, which is the thing nothing else can go red about.
    else if (/\.tsx$/.test(p) && !/\.test\.tsx$/.test(p)) out.push(p);
  }
  return out.sort();
}

/** Comments are prose ABOUT the component, not a mount of it — several files
 * spell `<ChatArea>` in a sentence. Blanked rather than deleted so line numbers
 * in the report stay true. */
function stripComments(code) {
  return code
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "))
    .replace(/(^|[^:])\/\/[^\n]*/g, (m, p1) => p1 + " ".repeat(m.length - p1.length));
}

/** Every `<ChatArea …>` element in the file, as {line, text} — the element's
 * whole opening tag, so a `key` on the second line of a multi-line element is
 * found. */
function chatAreaElements(rawCode) {
  const code = stripComments(rawCode);
  const out = [];
  const re = /<ChatArea(?![A-Za-z0-9_])/g;
  let m;
  while ((m = re.exec(code)) !== null) {
    // Walk to the end of the opening tag, counting brace depth so a `>` inside
    // a `{...}` expression does not end it early.
    let depth = 0;
    let i = m.index + m[0].length;
    for (; i < code.length; i++) {
      const c = code[i];
      if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (c === ">" && depth === 0) break;
    }
    out.push({
      line: code.slice(0, m.index).split("\n").length,
      text: code.slice(m.index, i + 1),
    });
  }
  return out;
}

/** The key's value as written, or null when there is no `key=` at all. Reads
 * the balanced `{...}` so a key spanning lines comes back whole. */
function keyValue(text) {
  const m = /(^|\s)key\s*=/.exec(text);
  if (m === null) return null;
  let i = m.index + m[0].length;
  while (i < text.length && /\s/.test(text[i])) i++;
  if (text[i] !== "{") return text.slice(i).trimEnd();
  let depth = 0;
  for (let j = i; j < text.length; j++) {
    if (text[j] === "{") depth++;
    else if (text[j] === "}") {
      depth--;
      if (depth === 0) return text.slice(i, j + 1);
    }
  }
  return text.slice(i);
}

/** `{a.b.id}` / `{a?.b.id}` — an identifier chain ending in `.id`. A literal
 * (`"chat"`, `{"chat"}`, `{1}`) and a bare identifier are both rejected. */
const KEY_SHAPE = /^\{\s*([A-Za-z_$][\w$]*)((?:\??\.[A-Za-z_$][\w$]*)*\.id)\s*\}$/;

const missing = [];
const constant = [];
let checked = 0;
for (const file of walk(SRC)) {
  const code = readFileSync(file, "utf8");
  if (!code.includes("<ChatArea")) continue;
  for (const el of chatAreaElements(code)) {
    checked++;
    const where = `${relative(SRC, file)}:${el.line}`;
    const key = keyValue(el.text);
    if (key === null) {
      missing.push(where);
      continue;
    }
    const shape = KEY_SHAPE.exec(key);
    if (shape === null) {
      constant.push(`${where}  key=${key}`);
      continue;
    }
    // The root identifier must be something this element already talks about.
    // `key={somethingElse.id}` next to `member={selected}` is a key that varies
    // with a value this mount has no other relationship to — which is how a key
    // ends up constant in practice without ever being written as a literal.
    const root = shape[1];
    const rest = el.text.replace(key, "");
    if (!new RegExp(`(^|[^\\w$.])${root}(?![\\w$])`).test(rest)) {
      constant.push(`${where}  key=${key} (\`${root}\` appears nowhere else on this element)`);
    }
  }
}

// A rule that matches nothing passes vacuously, which is how a renamed component
// silently disables its own guard.
if (checked === 0) {
  console.error(
    "[chat-area-key] FAIL — no <ChatArea> element found anywhere under src/.",
  );
  console.error(
    "  Either the component was renamed (point this lint at the new name) or the scan is broken.",
  );
  process.exit(1);
}

if (missing.length > 0 || constant.length > 0) {
  if (missing.length > 0) {
    console.error("[chat-area-key] FAIL — <ChatArea> mounted without a `key`:");
    for (const o of missing) console.error(`    ${o}`);
  }
  if (constant.length > 0) {
    console.error(
      "[chat-area-key] FAIL — <ChatArea> mounted with a `key` that does not identify the conversation:",
    );
    for (const o of constant) console.error(`    ${o}`);
    console.error(
      "  A constant key is the same thing as no key: one instance, reused across every room.",
    );
  }
  console.error(
    "  Without a key React REUSES one instance across conversations, and every piece",
  );
  console.error(
    "  of per-conversation state in ChatArea/useChat survives into the next room —",
  );
  console.error(
    "  the defect family T-48 spent twelve review rounds on. Mount it as",
  );
  console.error("  `<ChatArea key={member.id} member={member} …>`.");
  process.exit(1);
}

console.log(
  `[chat-area-key] ok — ${checked} <ChatArea> mount${checked === 1 ? "" : "s"}, every one keyed`,
);
