// The send-time half of chat @mentions: the composer keeps the plain `@name`
// the owner typed (draft, failure restore), and only the outgoing body carries
// `[@name](/#office/member/<id>)`, which chat bubbles render as an in-app link.

export type MentionKind = "member" | "worker";

export interface MentionTarget {
  kind: MentionKind;
  id: string;
}

export interface NamedMentionTarget extends MentionTarget {
  name: string;
}

const MENTION_HREF_RE = /^\/#office\/(member|worker)\/([^/?#\s]+)$/;
const FENCE_RE = /^```/;
// Inline code spans and existing [label](dest) links are copied through as-is.
const PROTECTED_RE = /`[^`]+`|\[(?:\\.|[^\]\\])+\]\([^)]+\)/g;
// A name only matches whole: `@Eva` is not a mention of "Ev". CJK letters are
// deliberately NOT a boundary breaker, because Chinese prose runs straight on
// after a name (`@小明你好`).
const NAME_CONTINUES_RE = /[A-Za-z0-9_]/;

export function mentionHref(target: MentionTarget): string {
  return `/#office/${target.kind}/${encodeURIComponent(target.id)}`;
}

export function mentionTargetFromHref(href: string): MentionTarget | null {
  const m = MENTION_HREF_RE.exec(href);
  if (!m) return null;
  try {
    return { kind: m[1] as MentionKind, id: decodeURIComponent(m[2]) };
  } catch {
    return null;
  }
}

function escapeLabel(name: string): string {
  return name.replace(/[\\[\]]/g, "\\$&");
}

function unescapeLabel(label: string): string {
  return label.replace(/\\([\\[\]])/g, "$1");
}

const MENTION_LINK_RE = /\[@((?:\\.|[^\]\\])+)\]\(([^)\s]+)\)/g;
// The server cuts a quote line at a rune cap and appends "…", which can land
// anywhere inside a mention link: in the name, on "](", or in the destination.
const CUT_MENTION_LINK_RE = /\[@((?:\\.|[^\]\\])+?)(?:\](?:\(([^)\s]*))?)?…$/;
const MENTION_HREF_PREFIX = "/#office/";

function isCutMentionDestination(partial: string | undefined): boolean {
  if (partial === undefined) return true;
  return MENTION_HREF_PREFIX.startsWith(partial) || partial.startsWith(MENTION_HREF_PREFIX);
}

export interface MentionRange {
  start: number;
  /** Exclusive. */
  end: number;
  target: NamedMentionTarget;
}

function findLineMentions(
  line: string,
  offset: number,
  byLongest: NamedMentionTarget[],
  out: MentionRange[],
): void {
  const ranges: Array<[number, number]> = [];
  for (const m of line.matchAll(PROTECTED_RE)) {
    ranges.push([m.index, m.index + m[0].length]);
  }
  let i = 0;
  let r = 0;
  while (i < line.length) {
    if (r < ranges.length && i === ranges[r][0]) {
      i = ranges[r][1];
      r += 1;
      continue;
    }
    if (line[i] === "@" && (i === 0 || /\s/.test(line[i - 1]))) {
      const limit = r < ranges.length ? ranges[r][0] : line.length;
      const hit = byLongest.find((n) => {
        const end = i + 1 + n.name.length;
        return (
          end <= limit &&
          line.startsWith(n.name, i + 1) &&
          !NAME_CONTINUES_RE.test(line[end] ?? "")
        );
      });
      if (hit) {
        out.push({ start: offset + i, end: offset + i + 1 + hit.name.length, target: hit });
        i += 1 + hit.name.length;
        continue;
      }
    }
    i += 1;
  }
}

/** Every `@<name>` (at line start or after whitespace) that names one of
 * `targets`, in text order; longest name wins. Fenced blocks, code spans and
 * existing links are skipped. What the composer marks is exactly what
 * `linkMentions` turns into links. */
export function findMentions(text: string, targets: NamedMentionTarget[]): MentionRange[] {
  const byLongest = targets
    .filter((t) => t.name !== "")
    .sort((a, b) => b.name.length - a.name.length);
  const found: MentionRange[] = [];
  if (byLongest.length === 0 || !text.includes("@")) return found;
  let inFence = false;
  let offset = 0;
  for (const line of text.split("\n")) {
    if (FENCE_RE.test(line.trimStart())) {
      inFence = !inFence;
    } else if (!inFence) {
      findLineMentions(line, offset, byLongest, found);
    }
    offset += line.length + 1;
  }
  return found;
}

/** Turn every mention `findMentions` sees into a mention link. */
export function linkMentions(text: string, targets: NamedMentionTarget[]): string {
  let out = "";
  let last = 0;
  for (const m of findMentions(text, targets)) {
    out += text.slice(last, m.start) + `[@${escapeLabel(m.target.name)}](${mentionHref(m.target)})`;
    last = m.end;
  }
  return out + text.slice(last);
}

/** The plain-text reading of a body `linkMentions` wrote: every mention link
 * becomes the `@name` it was written from. For views that show a body as text
 * rather than Markdown; never for what is stored or sent. */
export function unlinkMentions(text: string): string {
  if (!text.includes("[@")) return text;
  return text
    .replace(MENTION_LINK_RE, (whole, label: string, href: string) =>
      mentionTargetFromHref(href) ? `@${unescapeLabel(label)}` : whole,
    )
    .replace(CUT_MENTION_LINK_RE, (whole, label: string, partial: string | undefined) =>
      isCutMentionDestination(partial) ? `@${unescapeLabel(label)}…` : whole,
    );
}

export type MentionPiece = string | (MentionTarget & { label: string });

/** Split a body into plain runs and the mention links in it, for a surface
 * that shows the body as text rather than Markdown. `label` is the written
 * `@name`. */
export function splitMentions(text: string): MentionPiece[] {
  if (!text.includes("[@")) return [text];
  const pieces: MentionPiece[] = [];
  let last = 0;
  for (const m of text.matchAll(MENTION_LINK_RE)) {
    const target = mentionTargetFromHref(m[2]);
    if (!target) continue;
    if (m.index > last) pieces.push(text.slice(last, m.index));
    pieces.push({ ...target, label: `@${unescapeLabel(m[1])}` });
    last = m.index + m[0].length;
  }
  if (last < text.length) pieces.push(text.slice(last));
  return pieces;
}
