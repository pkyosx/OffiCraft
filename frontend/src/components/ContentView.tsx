// ContentView — the body of a stored file, drawn the same way on every surface
// that shows one: the preview overlay (chat attachments, task artifacts, a
// 檔案庫 name), the 檔案庫 page, and the version readers. Each host keeps its
// own shell (header, download, share, comments, zoom, pager, Esc); this is only
// what goes inside it.
//
// The class names keep the `md-preview__` prefix they were born with in the
// overlay; md-preview.css owns them and this file imports it.
//
// An HTML page is never drawn from its text and never loaded from an address
// carrying the owner's token: the host mints the page's share link (`?sig=`)
// and hands it in as `shareHref`. The frame's sandbox omits allow-same-origin,
// so agent-written script cannot run on the studio's origin; the server's CSP
// sandbox for sig-only HTML applies on top, so a flag added here does nothing
// unless api_chat.go carries it too.

import type { ReactNode, Ref } from "react";
import { useI18n } from "../i18n";
import type { ContentKind } from "../lib/contentKind";
import { formatJson, formatXml, parseCsv } from "../lib/formatContent";
import { Markdown } from "./Markdown";
import { ExternalLinkIcon } from "./icons";
import "./md-preview.css";

export type ContentViewProps = {
  kind: ContentKind;
  /** The file's text, for the text kinds. `null` while it is being read. */
  text?: string | null;
  /** Reading the bytes failed. */
  failed?: boolean;
  /** The image's address (already authorised by the host). */
  src?: string;
  /** What the image or page is called, for its alt text / frame title. */
  label?: string;
  /** The file's minted share link: an HTML page's frame address, and the
   * 「在新頁面顯示」 target for a file that cannot be drawn here. `null` while
   * it is being minted or when the browser would download the file. */
  shareHref?: string | null;
  shareFailed?: boolean;
  frameRef?: Ref<HTMLIFrameElement>;
  /** The page frame fills a flex host (the overlay). Elsewhere it takes a
   * fixed height of its own. */
  fill?: boolean;
  /** Markdown: single newlines are line breaks (a chat message, not a file). */
  breaks?: boolean;
  /** Markdown keeps `.doc-md`'s own page padding, for a host whose layout was
   * built around it (DocCard). Elsewhere the host's body owns the gutters. */
  padded?: boolean;
  /** Markdown: show block images, through this address resolver. */
  resolveImageSrc?: (src: string) => string;
  /** The host has no 「在新頁面顯示」 of its own, so a file that cannot be
   * drawn here offers one in the body. */
  newTabLink?: boolean;
  /** What a file that cannot be drawn says when there is no tab to open it in,
   * or an HTML page whose share link could not be made — the host knows
   * whether a download is on offer. */
  unavailable?: ReactNode;
  testId?: string;
};

export function ContentView({
  kind,
  text,
  failed,
  src,
  label,
  shareHref = null,
  shareFailed,
  frameRef,
  fill,
  breaks,
  resolveImageSrc,
  padded,
  newTabLink,
  unavailable,
  testId,
}: ContentViewProps) {
  const { t } = useI18n();
  const p = t.chat.mdPreview;
  // A line above a body that carries testId passes `null`: `undefined` would
  // fall back to testId and name two elements.
  const status = (line: ReactNode, id: string | null | undefined = testId) => (
    <div className="md-preview__status" data-testid={id ?? undefined}>
      {line}
    </div>
  );

  if (failed) return status(p.error);

  if (kind === "image") {
    return (
      <img
        className="md-preview__still"
        data-testid={testId}
        src={src}
        alt={label || t.chat.imageAlt}
      />
    );
  }

  if (kind === "html") {
    if (shareHref === null) return status(shareFailed ? (unavailable ?? p.unavailable) : p.loading);
    return (
      <iframe
        ref={frameRef}
        className={"md-preview__frame" + (fill ? "" : " md-preview__frame--fixed")}
        data-testid={testId}
        title={label}
        src={shareHref}
        referrerPolicy="no-referrer"
        sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox"
      />
    );
  }

  if (kind === "pdf" || kind === "binary") {
    if (shareHref === null) return status(unavailable ?? p.unavailable);
    if (!newTabLink) return status(p.unavailableOpenInNewTab);
    return (
      <div className="md-preview__status" data-testid={testId}>
        <span>{p.unavailableHere}</span>
        <a
          className="md-preview__new-tab md-preview__new-tab--inline"
          href={shareHref}
          target="_blank"
          rel="noopener noreferrer"
        >
          <ExternalLinkIcon size={14} />
          {p.openInNewTab}
        </a>
      </div>
    );
  }

  if (text === null || text === undefined) return status(p.loading);

  if (kind === "markdown") {
    return (
      <Markdown
        source={text}
        className={padded ? "doc-md" : "md-preview__md doc-md"}
        breaks={breaks}
        resolveImageSrc={resolveImageSrc}
        testId={testId}
      />
    );
  }

  if (kind === "json" || kind === "xml") {
    const { body, unparseable } = kind === "json" ? formatJson(text) : formatXml(text);
    return (
      <>
        {unparseable && status(kind === "json" ? p.jsonUnparseable : p.xmlUnparseable, null)}
        <pre className="md-preview__text" data-testid={testId}>
          {body}
        </pre>
      </>
    );
  }

  if (kind === "csv") {
    const table = parseCsv(text);
    if ("error" in table) {
      return (
        <>
          {status(table.error === "too_large" ? p.csvTooLarge : p.csvUnparseable, null)}
          <pre className="md-preview__text" data-testid={testId}>
            {text}
          </pre>
        </>
      );
    }
    const [head, ...rows] = table.rows;
    const width = Math.max(...table.rows.map((r) => r.length));
    const cells = (row: string[]) => Array.from({ length: width }, (_, i) => row[i] ?? "");
    return (
      <div className="md-preview__md md-preview__csv doc-md" data-testid={testId}>
        <table>
          <thead>
            <tr>
              {cells(head!).map((cell, i) => (
                <th key={i}>{cell}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, r) => (
              <tr key={r}>
                {cells(row).map((cell, i) => (
                  <td key={i}>{cell}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }

  return (
    <pre className="md-preview__text" data-testid={testId}>
      {text}
    </pre>
  );
}
