/** Lower-cases A–Z only — the server's matching rule (SQLite `lower()`), so a
 * highlight never claims a match the server would not make (e.g. 「Ä」/「ä」).
 * Length-preserving, so indices into the result are indices into `s`. */
export function asciiLower(s: string): string {
  return s.replace(/[A-Z]/g, (c) => c.toLowerCase());
}

/** Does `body` contain `q` under the server's search rule? */
export function chatBodyMatches(body: string, q: string): boolean {
  const needle = asciiLower(q.trim());
  return needle === "" || asciiLower(body).includes(needle);
}

export interface SnippetPart {
  text: string;
  match: boolean;
}

export interface ChatSearchSnippet {
  cutStart: boolean;
  cutEnd: boolean;
  parts: SnippetPart[];
}

const isLowSurrogate = (code: number) => code >= 0xdc00 && code <= 0xdfff;

/** The part of `body` around its FIRST match of `q`, up to `context` UTF-16
 * units each side, split into plain and matched runs (every occurrence inside
 * the cut, case-insensitive for ASCII). A body with no match is cut from its
 * start with nothing marked. */
export function chatSearchSnippet(
  body: string,
  q: string,
  context = 40,
): ChatSearchSnippet {
  const needle = asciiLower(q.trim());
  const hay = asciiLower(body);
  const first = needle === "" ? -1 : hay.indexOf(needle);
  let start = first < 0 ? 0 : Math.max(0, first - context);
  let end =
    first < 0
      ? Math.min(body.length, context * 2)
      : Math.min(body.length, first + needle.length + context);
  if (start > 0 && isLowSurrogate(body.charCodeAt(start))) start -= 1;
  if (end < body.length && isLowSurrogate(body.charCodeAt(end))) end += 1;

  const parts: SnippetPart[] = [];
  let at = start;
  let hit = first;
  while (hit >= 0 && hit < end) {
    const hitEnd = hit + needle.length;
    if (hitEnd > end) end = hitEnd;
    if (hit > at) parts.push({ text: body.slice(at, hit), match: false });
    parts.push({ text: body.slice(hit, hitEnd), match: true });
    at = hitEnd;
    hit = hay.indexOf(needle, hitEnd);
  }
  if (at < end) parts.push({ text: body.slice(at, end), match: false });
  return { cutStart: start > 0, cutEnd: end < body.length, parts };
}
