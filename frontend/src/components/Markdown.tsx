// components/Markdown.tsx — a minimal, XSS-safe markdown renderer.
//
// Deliberately NOT a full markdown engine and NOT an npm dependency: it renders
// only the subset the role-def / global-context seeds, owner-authored task
// manuals, and (T-13af) task cards' description/step-DoD/reply-card body
// actually use, and it builds React ELEMENTS exclusively — it NEVER uses
// dangerouslySetInnerHTML, so no seed text, owner edit, or agent-authored task
// text can inject markup. Any syntax it does not understand falls through as
// plain text (safe by construction).
//
// Block-level:  # / ## / ### headings, "- " / "* " unordered lists,
//               "1. " ordered lists (source numbering preserved), "> "
//               blockquotes, ``` fenced code blocks, and paragraphs. List items
//               absorb their indented continuation (sub-lists, code, prose) and
//               render it nested — so a numbered step with indented sub-bullets
//               stays ONE list with continuous numbering instead of collapsing
//               into many single-item lists each restarting at 1.
// Inline:       **bold**, `code`, and [text](url) links (http/https/mailto
//               only — any other scheme, e.g. "javascript:", falls through as
//               literal text instead of becoming an <a>; real links carry
//               target="_blank" rel="noopener noreferrer").

import { Fragment, type ReactNode } from "react";

interface MarkdownProps {
  source: string;
  className?: string;
  /** Treat a single newline inside a paragraph as a HARD line break (<br>)
   * instead of markdown's default "soft wrap" (join with a space).
   *
   * Off by default, so every pre-existing call site (task description, step
   * DoD, reply-card body, manuals, seeds) keeps standard markdown semantics.
   *
   * Chat (T-84c8) turns it ON: a chat bubble is a LINE/Slack-style surface
   * where Enter means "new line", and the bubble already preserved newlines
   * via `white-space: pre-wrap` BEFORE it rendered markdown. Without this,
   * routing chat through the renderer would silently collapse every
   * multi-line plain message into one run-on line — a regression on the most
   * common message shape, not an improvement. Same reason GitHub/Slack render
   * markdown with hard breaks in comment/message fields. */
  breaks?: boolean;
}

const LINK_RE = /^\[([^\]]+)\]\(([^)]+)\)$/;
const SAFE_URL_RE = /^(https?:|mailto:)/i;

// Split one line of text into inline nodes: `code` spans, **bold** runs, and
// [text](url) links, everything else literal. Code takes precedence (its
// content is not re-parsed).
function renderInline(text: string): ReactNode[] {
  // Capturing split keeps the delimiters as their own array entries.
  const parts = text.split(
    /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g
  );
  return parts
    .filter((p) => p !== "")
    .map((part, i) => {
      if (part.length >= 2 && part.startsWith("`") && part.endsWith("`")) {
        return <code key={i}>{part.slice(1, -1)}</code>;
      }
      if (part.length >= 4 && part.startsWith("**") && part.endsWith("**")) {
        return <strong key={i}>{part.slice(2, -2)}</strong>;
      }
      const link = LINK_RE.exec(part);
      if (link) {
        const [, label, url] = link;
        // Unrecognized/unsafe scheme (javascript:, data:, …) — render the
        // literal source text instead of a clickable anchor.
        if (!SAFE_URL_RE.test(url.trim())) {
          return <Fragment key={i}>{part}</Fragment>;
        }
        return (
          <a key={i} href={url.trim()} target="_blank" rel="noopener noreferrer">
            {label}
          </a>
        );
      }
      return <Fragment key={i}>{part}</Fragment>;
    });
}

const HEADING_RE = /^(#{1,3})\s+(.*)$/;
const ULIST_RE = /^[-*]\s+(.*)$/;
const OLIST_RE = /^(\d+)\.\s+(.*)$/;
const QUOTE_RE = /^>\s?(.*)$/;
const FENCE_RE = /^```/;

function indentOf(line: string): number {
  return line.length - line.trimStart().length;
}

// Strip the common leading indentation off a block of lines (blank lines
// ignored when measuring) so a nested block re-parses at its own base.
function dedent(lines: string[]): string[] {
  const indents = lines
    .filter((l) => l.trim() !== "")
    .map(indentOf);
  const min = indents.length ? Math.min(...indents) : 0;
  return lines.map((l) => l.slice(min));
}

// Render the lines of ONE paragraph. Default markdown folds them into a single
// run (join with a space); `breaks` keeps each source line on its own visual
// line by separating them with <br>.
function renderParagraph(lines: string[], breaks: boolean): ReactNode[] {
  if (!breaks) return renderInline(lines.join(" "));
  return lines.map((line, idx) => (
    <Fragment key={idx}>
      {idx > 0 ? <br /> : null}
      {renderInline(line)}
    </Fragment>
  ));
}

/** Parse the markdown source into an array of block-level React nodes. */
function renderBlocks(source: string, breaks = false): ReactNode[] {
  const lines = source.replace(/\r\n/g, "\n").split("\n");
  const blocks: ReactNode[] = [];
  let i = 0;
  let key = 0;

  while (i < lines.length) {
    const line = lines[i];

    // Blank line → block separator.
    if (line.trim() === "") {
      i++;
      continue;
    }

    // Fenced code block — ``` … ``` (language after the fence is ignored).
    if (FENCE_RE.test(line.trimStart())) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !FENCE_RE.test(lines[i].trimStart())) {
        body.push(lines[i]);
        i++;
      }
      i++; // consume the closing fence (a missing one just ends at EOF)
      blocks.push(
        <pre key={key++}>
          <code>{dedent(body).join("\n")}</code>
        </pre>
      );
      continue;
    }

    // Heading (# / ## / ###).
    const heading = HEADING_RE.exec(line);
    if (heading) {
      const level = heading[1].length;
      const content = renderInline(heading[2]);
      if (level === 1) blocks.push(<h1 key={key++}>{content}</h1>);
      else if (level === 2) blocks.push(<h2 key={key++}>{content}</h2>);
      else blocks.push(<h3 key={key++}>{content}</h3>);
      i++;
      continue;
    }

    // List (ordered or unordered) — the first marker at this level fixes the
    // list type. Each item absorbs its indented continuation (sub-lists, code,
    // prose), rendered nested; blank lines between items stay in the list as
    // long as another same-level marker follows.
    const ordered = OLIST_RE.test(line);
    if (ordered || ULIST_RE.test(line)) {
      const items: ReactNode[] = [];
      while (i < lines.length) {
        // Skip blank lines; the list continues only if another same-type
        // marker follows at base indent, else it ends here.
        let j = i;
        while (j < lines.length && lines[j].trim() === "") j++;
        if (j >= lines.length) {
          i = j;
          break;
        }
        const cand = lines[j];
        const m =
          indentOf(cand) === 0
            ? ordered
              ? OLIST_RE.exec(cand)
              : ULIST_RE.exec(cand)
            : null;
        if (!m) break;
        i = j + 1;
        const num = ordered ? parseInt(m[1], 10) : 0;
        const head = ordered ? m[2] : m[1];

        // Gather continuation: indented (or blank) lines belonging to this item.
        const cont: string[] = [];
        while (i < lines.length) {
          const c = lines[i];
          if (c.trim() === "") {
            cont.push("");
            i++;
            continue;
          }
          if (indentOf(c) > 0) {
            cont.push(c);
            i++;
            continue;
          }
          break; // back to base indent — next item or list end
        }
        while (cont.length && cont[cont.length - 1] === "") cont.pop();
        const inner =
          cont.length > 0 ? renderBlocks(dedent(cont).join("\n"), breaks) : [];

        items.push(
          <li key={items.length} value={ordered ? num : undefined}>
            {renderInline(head)}
            {inner.length > 0 ? inner : null}
          </li>
        );
      }
      blocks.push(
        ordered ? (
          <ol key={key++}>{items}</ol>
        ) : (
          <ul key={key++}>{items}</ul>
        )
      );
      continue;
    }

    // Blockquote — consume consecutive "> " lines.
    if (QUOTE_RE.test(line)) {
      const quoted: string[] = [];
      while (i < lines.length) {
        const m = QUOTE_RE.exec(lines[i]);
        if (!m) break;
        quoted.push(m[1]);
        i++;
      }
      blocks.push(
        <blockquote key={key++}>{renderInline(quoted.join(" "))}</blockquote>
      );
      continue;
    }

    // Paragraph — consume consecutive plain lines until a blank / block start.
    const para: string[] = [];
    while (i < lines.length) {
      const l = lines[i];
      if (
        l.trim() === "" ||
        FENCE_RE.test(l.trimStart()) ||
        HEADING_RE.test(l) ||
        ULIST_RE.test(l) ||
        OLIST_RE.test(l) ||
        QUOTE_RE.test(l)
      ) {
        break;
      }
      para.push(l);
      i++;
    }
    blocks.push(<p key={key++}>{renderParagraph(para, breaks)}</p>);
  }

  return blocks;
}

/** Render a trusted-but-untyped markdown string as safe React elements. */
export function Markdown({ source, className, breaks = false }: MarkdownProps) {
  return <div className={className}>{renderBlocks(source, breaks)}</div>;
}
