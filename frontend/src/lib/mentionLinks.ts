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
// inside a mention link's destination.
const CUT_MENTION_LINK_RE = /\[@((?:\\.|[^\]\\])+)\]\(\/#office\/[^)\s]*…$/;

function linkLine(line: string, byLongest: NamedMentionTarget[]): string {
  const ranges: Array<[number, number]> = [];
  for (const m of line.matchAll(PROTECTED_RE)) {
    ranges.push([m.index, m.index + m[0].length]);
  }
  let out = "";
  let i = 0;
  let r = 0;
  while (i < line.length) {
    if (r < ranges.length && i === ranges[r][0]) {
      out += line.slice(i, ranges[r][1]);
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
        out += `[@${escapeLabel(hit.name)}](${mentionHref(hit)})`;
        i += 1 + hit.name.length;
        continue;
      }
    }
    out += line[i];
    i += 1;
  }
  return out;
}

/** Turn every `@<name>` (at line start or after whitespace) that names one of
 * `targets` into a mention link; longest name wins. Fenced blocks, code spans
 * and existing links are left untouched. */
export function linkMentions(text: string, targets: NamedMentionTarget[]): string {
  const byLongest = targets
    .filter((t) => t.name !== "")
    .sort((a, b) => b.name.length - a.name.length);
  if (byLongest.length === 0 || !text.includes("@")) return text;
  let inFence = false;
  return text
    .split("\n")
    .map((line) => {
      if (FENCE_RE.test(line.trimStart())) {
        inFence = !inFence;
        return line;
      }
      return inFence ? line : linkLine(line, byLongest);
    })
    .join("\n");
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
    .replace(CUT_MENTION_LINK_RE, (_whole, label: string) => `@${unescapeLabel(label)}…`);
}
