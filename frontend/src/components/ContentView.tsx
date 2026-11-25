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

import { useEffect, useRef, useState, type ReactNode, type Ref } from "react";
import { useI18n } from "../i18n";
import type { ContentKind } from "../lib/contentKind";
import { formatJson, formatXml, parseCsv, yamlParses } from "../lib/formatContent";
import { Markdown } from "./Markdown";
import { AlertTriangleIcon, ExternalLinkIcon } from "./icons";
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
  /** The file's minted share link: an HTML page's, a PDF's or a video's address,
   * and the 「在新頁面顯示」 target for a file that cannot be drawn here.
   * `null` while it is being minted or after the mint failed; undefined when
   * no link will be made. */
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

/** `null` until the parser has answered for this exact text. A parser that
 * fails to load also stays `null`: no verdict is shown rather than a wrong one. */
function useYamlParses(text: string | null): boolean | null {
  const [verdict, setVerdict] = useState<{ text: string; ok: boolean } | null>(null);
  useEffect(() => {
    if (text === null) return;
    let live = true;
    yamlParses(text).then(
      (ok) => {
        if (live) setVerdict({ text, ok });
      },
      () => {},
    );
    return () => {
      live = false;
    };
  }, [text]);
  return verdict !== null && verdict.text === text ? verdict.ok : null;
}

export function ContentView({
  kind,
  text,
  failed,
  src,
  label,
  shareHref,
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
  const yamlOk = useYamlParses(kind === "yaml" ? (text ?? null) : null);
  const status = (line: ReactNode) => (
    <div className="md-preview__status" data-testid={testId}>
      {line}
    </div>
  );

  const malformed = (line: ReactNode) => (
    <div className="md-preview__malformed" role="note">
      <AlertTriangleIcon size={16} />
      <span>{line}</span>
    </div>
  );

  if (failed) return status(p.error);
  const linkPending = () =>
    status(shareHref === null && !shareFailed ? p.loading : (unavailable ?? p.unavailable));

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

  if (kind === "video") {
    if (typeof shareHref !== "string") return linkPending();
    return (
      <VideoView src={shareHref} label={label} unavailable={unavailable ?? p.unavailable} testId={testId} />
    );
  }

  if (kind === "html") {
    if (typeof shareHref !== "string") return linkPending();
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

  if (kind === "pdf") {
    if (typeof shareHref !== "string") return linkPending();
    // No sandbox attribute: Chromium refuses to draw a PDF in a sandboxed frame
    // (「已封鎖這個網頁」). The server's CSP sandbox, which it sends on every
    // non-HTML response, still keeps the document from scripting.
    const frame = (
      <iframe
        className={"md-preview__frame" + (fill ? "" : " md-preview__frame--fixed")}
        data-testid={testId}
        title={label}
        src={shareHref}
        referrerPolicy="no-referrer"
      />
    );
    if (!newTabLink) return frame;
    return (
      <div className="md-preview__pdf">
        {frame}
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

  if (kind === "binary") {
    if (typeof shareHref !== "string" || !newTabLink) return status(unavailable ?? p.unavailable);
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
        {unparseable && malformed(kind === "json" ? p.jsonUnparseable : p.xmlUnparseable)}
        <pre className="md-preview__text" data-testid={testId}>
          {body}
        </pre>
      </>
    );
  }

  if (kind === "yaml") {
    return (
      <>
        {yamlOk === false && malformed(p.yamlUnparseable)}
        <pre className="md-preview__text" data-testid={testId}>
          {text}
        </pre>
      </>
    );
  }

  if (kind === "csv") {
    const table = parseCsv(text);
    if ("error" in table) {
      return (
        <>
          {malformed(table.error === "too_large" ? p.csvTooLarge : p.csvUnparseable)}
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

const PLAYBACK_RATES = [0.5, 1, 1.25, 1.5, 2] as const;

function VideoView({
  src,
  label,
  unavailable,
  testId,
}: {
  src: string;
  label?: string;
  unavailable: ReactNode;
  testId?: string;
}) {
  const { t } = useI18n();
  const p = t.chat.mdPreview;
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const [rate, setRate] = useState<number>(1);
  const [unplayable, setUnplayable] = useState(false);

  useEffect(() => {
    setRate(1);
    setUnplayable(false);
  }, [src]);

  // Loading a source resets playbackRate to defaultPlaybackRate, so both are set.
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    video.defaultPlaybackRate = rate;
    video.playbackRate = rate;
  }, [rate]);

  if (unplayable) {
    return (
      <div className="md-preview__status" data-testid={testId}>
        {unavailable}
      </div>
    );
  }

  return (
    <div className="md-preview__video" data-testid={testId}>
      <video
        ref={videoRef}
        className="md-preview__video-player"
        src={src}
        controls
        playsInline
        preload="metadata"
        aria-label={label}
        onRateChange={(e) => setRate(e.currentTarget.playbackRate)}
        onError={() => setUnplayable(true)}
      />
      <div className="md-preview__speed" role="group" aria-label={p.playbackSpeed}>
        <span className="md-preview__speed-label" aria-hidden="true">
          {p.playbackSpeed}
        </span>
        <div className="md-preview__speed-options">
          {PLAYBACK_RATES.map((r) => (
            <button
              key={r}
              type="button"
              className={"md-preview__speed-option" + (r === rate ? " md-preview__speed-option--on" : "")}
              aria-pressed={r === rate}
              onClick={() => setRate(r)}
            >
              {`${r}×`}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
