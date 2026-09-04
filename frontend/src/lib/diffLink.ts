// lib/diffLink.ts — the compare screen's ADDRESS.
//
// T-59 (owner 2026-09-03): a comparison is no longer an attachment somebody
// clicks, it is a URL. Two flavours of the SAME url:
//
//   internal  /diff?before=…&after=…            — no signature, needs a session
//   external  /diff?before=…&after=…&sig=…      — the server-minted signature
//                                                 opens it with no login at all
//
// No expiry, same as the file-level share link (lib/shareLink.ts) it is modelled
// on. This module is the single parse/format seam for that url — the twin of
// lib/hashRoute.ts, which owns the STUDIO's own navigational state. They are two
// different address spaces on purpose: everything in the hash is a view of the
// office the reader is logged into, while this one has to survive being pasted
// into Slack and opened by someone who is not.
//
// A SIDE is one of exactly two shapes, and the grammar is the server's:
//
//   att-<12 lowercase hex>            a stored blob
//   doc:<kind>/<key>/<at>/<field>     one field of one document at one point in
//                                     time — `at` is "current", "seed", or a
//                                     retained revision's id in decimal
//
// ⚠️ WHAT THIS MODULE DOES *NOT* DO: resolve a side. The two texts and the two
// column headings come from GET /api/diff (api/diff.ts) in one response — the
// server reads the blob or the revision and says which side is gone. Parsing
// here is only ever used to answer "is this string an address at all", so a
// malformed link fails on the reader's own screen instead of being forwarded to
// the server as a question about nothing.

/** The page's path, and the five parameter names the url spells. A real path,
 * not a hash: the hash never reaches the server, and this url has to be one the
 * SPA shell is served for (server/ocserverd/spa.go's catch-all does that
 * already — no extension, not under /api/).
 *
 * 🔴 THESE SIX SPELLINGS ARE THE SERVER'S, NOT OURS. `server/ocserverd/
 * api_diff.go` declares them; the mint answers a url built from them and the
 * data route reads a query built from them. This module is the cockpit's ONE
 * copy — the compare page's parser, its formatter and `api/diff.ts`'s query all
 * read these constants rather than spelling the words again — and
 * diffLink.mirror.test.ts confronts the copy against that file's SOURCE, the
 * way cli/ocagent does for its own. Rename one there without renaming it here
 * and the test reddens, instead of the cockpit silently asking for a parameter
 * the server stopped answering. */
export const DIFF_PATH = "/diff";
export const DIFF_PARAM_BEFORE = "before";
export const DIFF_PARAM_AFTER = "after";
export const DIFF_PARAM_LABEL_BEFORE = "label_before";
export const DIFF_PARAM_LABEL_AFTER = "label_after";
export const DIFF_PARAM_SIG = "sig";

/** The reserved spellings of a document side's `at`. Same two words the server
 * matches; a typo in either place is a side that silently falls through to
 * "not a revision id". */
export const DIFF_SIDE_AT_CURRENT = "current";
export const DIFF_SIDE_AT_SEED = "seed";

/** One field of one document at one point in time. */
export interface DiffSideDoc {
  kind: string;
  key: string;
  /** "current" | "seed" | a retained revision's id in decimal. */
  at: string;
  field: string;
}

/** A parsed side: EITHER a stored blob or a document address, never both. */
export type DiffSideAddress =
  | { attachmentId: string; doc?: never }
  | { doc: DiffSideDoc; attachmentId?: never };

/** Everything the compare url carries. `before`/`after` stay as the STRINGS the
 * url spelled them: they are the server's input, and re-serialising a parsed
 * side would make this module a second authority on the grammar. */
export interface DiffParams {
  before: string;
  after: string;
  labelBefore?: string;
  labelAfter?: string;
  /** The server-minted signature. Present ⇒ the page opens with no session. */
  sig?: string;
}

// 🔴 THE RULES BELOW ARE A TRANSCRIPTION, NOT A DESIGN. The authority is
// server/ocserverd/diffaddr.go; this is the cockpit's reader of the same
// contract, and when the two disagree the server wins. They are confronted
// against bin/tests/fixtures/diff-side-addresses.tsv — the same table the two
// Go copies are driven by — in diffLink.mirror.test.ts, so a drift reddens by
// name rather than showing up as a link that "just does nothing".
const ATTACHMENT_RE = /^att-[0-9a-f]{12}$/;
// Each part of a document address is spliced into a URL by its readers, so the
// character set excludes "/", "%", "?" and "#" outright — that removes the
// normalisation class rather than trying to sanitise it afterwards.
const DOC_SEGMENT_RE = /^[A-Za-z0-9._:@+-]+$/;
// A retained revision's id in decimal. No leading zero: the id is an int64
// everywhere else, and "007" is not how any of them are spelled.
const REVISION_AT_RE = /^[1-9][0-9]{0,18}$/;

function sayableSegment(value: string): boolean {
  // "." and ".." pass the character set and traverse anyway, so they are
  // refused by name rather than by pattern.
  return value !== "." && value !== ".." && DOC_SEGMENT_RE.test(value);
}

/** Parse one side. Returns null for anything that is not one of the two shapes
 * — which is what makes a hand-typed link fail here rather than three screens
 * later.
 *
 * A side this accepts is SAYABLE, never a promise that it still RESOLVES:
 * whether a blob is still stored or a revision still retained is a read-time
 * fact, and the compare read answers it with that side's honest "gone". */
export function parseDiffSideAddress(raw: string): DiffSideAddress | null {
  if (!raw.startsWith("doc:")) {
    // Matched AS GIVEN, never trimmed: a padded address is one no reader can
    // resolve, so accepting it would split "it was accepted" from "it will
    // draw".
    return ATTACHMENT_RE.test(raw) ? { attachmentId: raw } : null;
  }
  // Exactly four segments, split on the one character no segment may contain.
  const segs = raw.slice("doc:".length).split("/");
  if (segs.length !== 4) return null;
  const [kind, key, at, field] = segs;
  if (!sayableSegment(kind) || !sayableSegment(key) || !sayableSegment(field)) return null;
  if (at !== DIFF_SIDE_AT_CURRENT && at !== DIFF_SIDE_AT_SEED && !REVISION_AT_RE.test(at)) {
    return null;
  }
  return { doc: { kind, key, at, field } };
}

/** Format one side back into the url spelling. */
export function formatDiffSideAddress(side: DiffSideAddress): string {
  if (side.attachmentId !== undefined) return side.attachmentId;
  const { kind, key, at, field } = side.doc;
  return `doc:${kind}/${key}/${at}/${field}`;
}

/** Read the params off a query string. Null when either side is missing or is
 * not an address — a compare with one side is not a compare. */
export function parseDiffParams(search: string): DiffParams | null {
  const q = new URLSearchParams(search);
  const before = q.get(DIFF_PARAM_BEFORE);
  const after = q.get(DIFF_PARAM_AFTER);
  if (before === null || after === null) return null;
  if (parseDiffSideAddress(before) === null) return null;
  if (parseDiffSideAddress(after) === null) return null;
  const params: DiffParams = { before, after };
  const labelBefore = q.get(DIFF_PARAM_LABEL_BEFORE);
  const labelAfter = q.get(DIFF_PARAM_LABEL_AFTER);
  const sig = q.get(DIFF_PARAM_SIG);
  // An EMPTY label is not a label — heading a column with "" is the blank
  // heading the reader could not read in the first place.
  if (labelBefore) params.labelBefore = labelBefore;
  if (labelAfter) params.labelAfter = labelAfter;
  if (sig) params.sig = sig;
  return params;
}

/** The five parameters, spelled once. Shared by the page url built here and by
 * the compare read's query (api/diff.ts) — the two used to spell the same five
 * words twice, which is one rename away from a link the reader can open and a
 * request the server cannot answer. An EMPTY optional is LEFT OUT rather than
 * sent blank, matching the server's own diffPageQuery. */
export function diffSearchParams(params: DiffParams): URLSearchParams {
  const q = new URLSearchParams();
  q.set(DIFF_PARAM_BEFORE, params.before);
  q.set(DIFF_PARAM_AFTER, params.after);
  if (params.labelBefore) q.set(DIFF_PARAM_LABEL_BEFORE, params.labelBefore);
  if (params.labelAfter) q.set(DIFF_PARAM_LABEL_AFTER, params.labelAfter);
  if (params.sig) q.set(DIFF_PARAM_SIG, params.sig);
  return q;
}

/** The url for these params, relative to the origin (values URL-encoded). */
export function formatDiffUrl(params: DiffParams): string {
  return `${DIFF_PATH}?${diffSearchParams(params).toString()}`;
}

/** Is this href one of OUR compare urls, and what does it say?
 *
 * Used by the markdown renderer to decide whether a link opens the compare
 * screen in place instead of navigating (components/Markdown.tsx). Deliberately
 * strict on all three counts — SAME ORIGIN, exactly the /diff path, and params
 * that parse — because the answer to "yes" is "swallow the reader's click".
 * Anything else stays an ordinary external link, which is the safe default.
 *
 * `base` is the page origin; it is a parameter only so the tests can pin the
 * cross-origin case without touching window.location. */
export function diffParamsFromHref(
  href: string,
  base: string = typeof window !== "undefined" ? window.location.href : "http://localhost/",
): DiffParams | null {
  let url: URL;
  try {
    url = new URL(href, base);
  } catch {
    return null;
  }
  const origin = new URL(base).origin;
  if (url.origin !== origin) return null;
  if (url.pathname !== DIFF_PATH) return null;
  return parseDiffParams(url.search);
}

/** The compare params the CURRENT page address carries, or null when this is
 * not the compare page at all. The one call site is the app root (main.tsx),
 * which mounts the standalone page instead of the studio when it answers. */
export function diffRouteFromLocation(
  loc: { pathname: string; search: string } = window.location,
): DiffParams | null {
  if (loc.pathname !== DIFF_PATH) return null;
  return parseDiffParams(loc.search);
}
