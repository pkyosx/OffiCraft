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
//               blockquotes, ``` fenced code blocks, GFM tables (T-bc3e:
//               header row + |---| delimiter row + data rows; :--- / :---: /
//               ---: alignment; leading/trailing pipes optional; a header row
//               whose column count does not match the delimiter row is NOT a
//               table and falls through as text), "---" thematic breaks (a
//               line of 3+ "-"/"*"/"_" alone where a block starts), and
//               paragraphs. List items
//               absorb their indented continuation (sub-lists, code, prose) and
//               render it nested — so a numbered step with indented sub-bullets
//               stays ONE list with continuous numbering instead of collapsing
//               into many single-item lists each restarting at 1.
// Inline:       **bold**, `code`, and [text](url) links (http/https/mailto
//               only — any other scheme, e.g. "javascript:", falls through as
//               literal text instead of becoming an <a>; real links carry
//               target="_blank" rel="noopener noreferrer"). A SECOND, opt-in
//               link class exists for the 使用說明 doc page only: repo-relative
//               `*.md` targets resolved through `resolveDocLink` into IN-APP
//               navigation (T-68f1) — see the prop's doc comment.

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
  /** Resolve a block-level image reference (`![alt](src)` on its own line) into
   * a real <img> src. OFF by default — without it, every existing call site
   * (chat / manuals / seeds / task text) keeps rendering `![…](…)` as literal
   * text (no image loads, no regression). The 使用說明 doc page turns it ON,
   * passing a resolver that same-origin `/api/docs/assets/…` paths ride the
   * gated `?token=` auth on (authedAttachmentUrl) — a bare <img> cannot send an
   * Authorization header. Unsafe/foreign schemes fall through as literal text. */
  resolveImageSrc?: (src: string) => string;
  /** Resolve a REPO-RELATIVE markdown reference (`[看這個](docs/guide/why.md)`,
   * `[env](../dev/agent-env.md)`) into an in-app navigation action, or null to
   * keep the current literal-text fallback. OFF by default — exactly like
   * `resolveImageSrc`, so every existing call site (chat / manuals / seeds /
   * task text, all of which carry AGENT-authored, untrusted text) keeps
   * rendering such a reference as literal text. The 使用說明 doc page turns it
   * ON, because it is the only surface whose source is the build-time doc
   * embed AND the only surface with somewhere to navigate to.
   *
   * SECURITY: this is a THIRD link class, NOT a loosening of SAFE_URL_RE. The
   * external-scheme allowlist is evaluated FIRST and unchanged; only targets
   * matching DOC_REL_PATH_RE (a positive allowlist that cannot contain ":" and
   * cannot start with "/") are ever handed to the resolver, so `javascript:`,
   * `data:` and protocol-relative `//evil.com` never reach it and stay literal
   * text. The result is a <button>, not an <a href>, so there is no URL for an
   * open redirect to target.
   *
   * INPUT CONTRACT — what the allowlist does NOT promise. DOC_REL_PATH_RE is a
   * SHAPE filter, not a sanitiser: `..`, `.` and `/` are all inside its
   * character class, so a target handed to this resolver may legitimately look
   * like a traversal (`../../../etc/passwd.md`) or like a bare host
   * (`evil.com/x.md`) — both MATCH and both DO reach the resolver
   * (reviewer-measured, review3 §1.2). What keeps that harmless today is the
   * resolver, not the regex: the one call site (UserGuideDoc) reduces the
   * target to its BASENAME and then requires that slug to be in the list the
   * server actually served, so the worst case is "navigate to a doc that
   * already exists".
   * Therefore any resolver — this one or a future one — MUST NOT use the
   * target as a path: no `fetch('/api/docs/' + target)`, no path.join, no
   * filesystem or URL construction from it. Treat it as an opaque token to be
   * matched against a known-good list. */
  resolveDocLink?: (target: string) => (() => void) | null;
}

const LINK_RE = /^\[([^\]]+)\]\(([^)]+)\)$/;
const IMG_BLOCK_RE = /^!\[([^\]]*)\]\(([^)]+)\)$/;
const SAFE_URL_RE = /^(https?:|mailto:)/i;
// An image src is safe to load when it is http/https OR a same-origin absolute
// API path (the server rewrites doc-relative `assets/…` refs to `/api/docs/
// assets/…`). data:/javascript:/relative fall through as literal text.
const SAFE_IMG_SRC_RE = /^(https?:\/\/|\/)/i;
// A repo-relative markdown path — the ONLY link shape ever handed to
// `resolveDocLink`. A POSITIVE allowlist, deliberately built so the dangerous
// shapes cannot spell themselves with it:
//   • the character class has no ":" → no scheme at all (javascript:, data:,
//     vbscript:, http:) can match, so a scheme can never reach the resolver;
//   • the first segment cannot start with "/" → "/abs/x.md" and the
//     protocol-relative "//evil.com/x.md" are both excluded;
//   • it must END in ".md" → "#anchor", "?q=…", a bare "evil.com" (no .md
//     tail) are excluded.
// What it does NOT exclude, and was once documented as if it did: "..", "."
// and "/" are all inside the class, so "../../../etc/passwd.md" and
// "evil.com/x.md" both MATCH and are both handed to the resolver. Containing
// them is the RESOLVER's job — see the resolveDocLink prop's INPUT CONTRACT.
// Matching here only makes a target ELIGIBLE; the resolver still has to
// recognise it (the doc page checks it against the docs actually embedded), and
// a null answer keeps the literal-text fallback.
const DOC_REL_PATH_RE = /^[A-Za-z0-9._~-]+(?:\/[A-Za-z0-9._~-]+)*\.md$/;

/** Inline-render options threaded from <Markdown> down every block path. */
interface InlineOpts {
  resolveDocLink?: (target: string) => (() => void) | null;
}

// Split one line of text into inline nodes: `code` spans, **bold** runs, and
// [text](url) links, everything else literal. Code takes precedence (its
// content is not re-parsed).
function renderInline(text: string, opts?: InlineOpts): ReactNode[] {
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
        // Re-parse the bold run's INSIDE. Emitting it as a raw string left
        // `code` and [links] unrendered inside bold only — the same page then
        // showed `claude` as a green chip in prose and as bare backticks two
        // lines later inside **…**, which reads as an unfinished renderer.
        // Terminates: the split pattern's bold body is `[^*]+`, so a bold run
        // can never contain another bold run.
        return <strong key={i}>{renderInline(part.slice(2, -2), opts)}</strong>;
      }
      const link = LINK_RE.exec(part);
      if (link) {
        const [, label, url] = link;
        const target = url.trim();
        // The external-scheme allowlist runs FIRST and is unchanged.
        if (!SAFE_URL_RE.test(target)) {
          // Second chance, opt-in only: a repo-relative *.md reference the host
          // surface knows how to navigate to in-app. Anything the positive
          // path allowlist does not match — and anything the resolver declines
          // — keeps the literal-text fallback.
          if (opts?.resolveDocLink && DOC_REL_PATH_RE.test(target)) {
            const navigate = opts.resolveDocLink(target);
            if (navigate) {
              return (
                <button
                  key={i}
                  type="button"
                  className="md-doclink"
                  data-doc-target={target}
                  onClick={navigate}
                >
                  {label}
                </button>
              );
            }
          }
          return <Fragment key={i}>{part}</Fragment>;
        }
        return (
          <a key={i} href={target} target="_blank" rel="noopener noreferrer">
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
// Thematic break — a line that is nothing but 3+ of the same "-", "*" or "_".
// Deliberately NARROW: it is tested only where a BLOCK starts (i.e. after a
// blank line or another block), NOT as a paragraph terminator. GFM would read
// "---" directly under a prose line as a setext heading, which this renderer
// has never supported; keeping the check out of the paragraph accumulator
// leaves that case exactly as it is today (absorbed as prose) instead of
// silently reinterpreting it. Every "---" in docs/guide/ is blank-line
// separated, which is the shape this covers.
const HR_RE = /^(?:-{3,}|\*{3,}|_{3,})$/;
// GitHub alert marker — the first line of a blockquote, alone: "> [!NOTE]".
const ALERT_RE = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]$/i;

// ── GFM tables (T-bc3e) ──────────────────────────────────────────────────────
// A table starts where a line containing "|" is IMMEDIATELY followed by a valid
// delimiter row with the SAME column count (the GFM gate). Anything that fails
// that gate — header only, malformed delimiter, column-count mismatch — is not
// a table and falls through to the paragraph path as plain text.

type CellAlign = "left" | "center" | "right" | null;

// Split one table row into trimmed cell strings, GFM style: one optional
// leading and trailing pipe is decoration, inner pipes separate cells.
function splitRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|")) s = s.slice(0, -1);
  return s.split("|").map((c) => c.trim());
}

// Parse a candidate delimiter row ("| --- | :---: |") into per-column
// alignments, or null if any cell is not `:?-+:?` (then the whole construct is
// not a table).
function parseDelimiterRow(line: string): CellAlign[] | null {
  const t = line.trim();
  // Cheap reject + guarantee there is at least one dash (`|  |` is no delimiter).
  if (!t.includes("-") || !/^[|\s:-]+$/.test(t)) return null;
  const aligns: CellAlign[] = [];
  for (const cell of splitRow(t)) {
    if (!/^:?-+:?$/.test(cell)) return null;
    const left = cell.startsWith(":");
    const right = cell.endsWith(":");
    aligns.push(left && right ? "center" : right ? "right" : left ? "left" : null);
  }
  return aligns;
}

// True when lines[i] opens a table: it carries a "|", the NEXT line is a valid
// delimiter row, and the column counts agree. Shared by the block dispatcher
// and the paragraph accumulator (so a table right after a paragraph line still
// starts a table instead of being swallowed as prose — matters in `breaks`
// mode, where every chat line is paragraph-shaped).
function isTableStart(lines: string[], i: number): boolean {
  const line = lines[i];
  if (!line.includes("|") || line.trim() === "") return false;
  if (i + 1 >= lines.length) return false;
  const aligns = parseDelimiterRow(lines[i + 1]);
  return aligns !== null && splitRow(line).length === aligns.length;
}

function alignStyle(
  a: CellAlign
): { textAlign: "left" | "center" | "right" } | undefined {
  return a ? { textAlign: a } : undefined;
}

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
function renderParagraph(
  lines: string[],
  breaks: boolean,
  opts?: InlineOpts
): ReactNode[] {
  if (!breaks) return renderInline(lines.join(" "), opts);
  return lines.map((line, idx) => (
    <Fragment key={idx}>
      {idx > 0 ? <br /> : null}
      {renderInline(line, opts)}
    </Fragment>
  ));
}

/** Parse the markdown source into an array of block-level React nodes. */
function renderBlocks(
  source: string,
  breaks = false,
  resolveImageSrc?: (src: string) => string,
  opts?: InlineOpts
): ReactNode[] {
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

    // Block-level image (`![alt](src)` alone on a line) — only when a resolver
    // is wired AND the src is safe; otherwise it falls through as prose (the
    // literal-text default keeps every non-guide call site unchanged).
    if (resolveImageSrc) {
      const img = IMG_BLOCK_RE.exec(line.trim());
      if (img && SAFE_IMG_SRC_RE.test(img[2].trim())) {
        blocks.push(
          <img
            key={key++}
            src={resolveImageSrc(img[2].trim())}
            alt={img[1]}
            style={{ maxWidth: "100%" }}
          />
        );
        i++;
        continue;
      }
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

    // Thematic break (`---`). Tested AFTER the fence branch so a "---" inside
    // a code block is still code, and it can never steal a table delimiter row
    // (those carry a "|", which HR_RE does not allow).
    if (HR_RE.test(line.trim())) {
      blocks.push(<hr key={key++} />);
      i++;
      continue;
    }

    // Heading (# / ## / ###).
    const heading = HEADING_RE.exec(line);
    if (heading) {
      const level = heading[1].length;
      const content = renderInline(heading[2], opts);
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
          cont.length > 0
            ? renderBlocks(dedent(cont).join("\n"), breaks, resolveImageSrc, opts)
            : [];

        items.push(
          <li key={items.length} value={ordered ? num : undefined}>
            {renderInline(head, opts)}
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

    // GFM table (T-bc3e) — header row + delimiter row + data rows. The rows
    // are consumed as WHOLE lines here (never through renderParagraph), so
    // chat's `breaks` mode cannot inject <br> into a table. Data rows are
    // normalized to the header's column count (extra cells dropped, missing
    // cells empty — GFM behaviour), and cell content goes through renderInline
    // so bold/code/links work inside cells.
    if (isTableStart(lines, i)) {
      const headerCells = splitRow(line);
      const aligns = parseDelimiterRow(lines[i + 1])!;
      i += 2;
      const rows: string[][] = [];
      while (
        i < lines.length &&
        lines[i].trim() !== "" &&
        lines[i].includes("|")
      ) {
        const cells = splitRow(lines[i]).slice(0, headerCells.length);
        while (cells.length < headerCells.length) cells.push("");
        rows.push(cells);
        i++;
      }
      blocks.push(
        <table key={key++}>
          <thead>
            <tr>
              {headerCells.map((c, ci) => (
                <th key={ci} style={alignStyle(aligns[ci])}>
                  {renderInline(c, opts)}
                </th>
              ))}
            </tr>
          </thead>
          {rows.length > 0 ? (
            <tbody>
              {rows.map((r, ri) => (
                <tr key={ri}>
                  {r.map((c, ci) => (
                    <td key={ci} style={alignStyle(aligns[ci])}>
                      {renderInline(c, opts)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          ) : null}
        </table>
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
      // GitHub alert syntax (`> [!NOTE]` / `> [!WARNING]` …, T-68f1). The
      // marker line is CONSUMED — never shown as literal "[!NOTE]" noise — and
      // its severity survives as a class on the blockquote, so a stylesheet can
      // render a callout WITHOUT this renderer having to own any label text
      // (no i18n strings, no per-type markup). Surfaces that do not style
      // `.md-alert` simply get a clean blockquote.
      const alert = quoted.length > 0 ? ALERT_RE.exec(quoted[0].trim()) : null;
      if (alert) quoted.shift();
      blocks.push(
        <blockquote
          key={key++}
          className={
            alert
              ? `md-alert md-alert--${alert[1].toLowerCase()}`
              : undefined
          }
        >
          {/* BLOCK-level, not renderInline (fixed after the first real-page
              render of docs/guide/install.md). A blockquote's content is
              markdown in its own right — GitHub alerts in particular wrap
              prose AND fenced code AND lists. The previous
              `renderInline(quoted.join(" "))` flattened all of it onto one
              inline run, so a ```bash fence inside a `> [!WARNING]` rendered
              as bare backticks, the language tag became part of the command,
              and the following paragraph was swallowed into the code run.
              Re-entering renderBlocks (joined with "\n", NOT " ") is what
              makes the quote's inner structure survive; plain one-paragraph
              quotes are unaffected because a paragraph joins its lines with a
              space exactly as before. */}
          {renderBlocks(quoted.join("\n"), breaks, resolveImageSrc, opts)}
        </blockquote>
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
        QUOTE_RE.test(l) ||
        isTableStart(lines, i)
      ) {
        break;
      }
      para.push(l);
      i++;
    }
    blocks.push(<p key={key++}>{renderParagraph(para, breaks, opts)}</p>);
  }

  return blocks;
}

/** Render a trusted-but-untyped markdown string as safe React elements. */
export function Markdown({
  source,
  className,
  breaks = false,
  resolveImageSrc,
  resolveDocLink,
}: MarkdownProps) {
  return (
    <div className={className}>
      {renderBlocks(source, breaks, resolveImageSrc, { resolveDocLink })}
    </div>
  );
}
