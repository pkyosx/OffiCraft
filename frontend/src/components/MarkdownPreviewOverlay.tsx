// components/MarkdownPreviewOverlay.tsx — the in-cockpit .md preview overlay
// (T-a1c4). A markdown attachment is PREVIEWED here — fetched as text and
// rendered through the shared Markdown.tsx (React elements, XSS-safe) — instead
// of the browser's raw-source new tab. Preview and download are TWO separate
// actions: the header keeps a 下載 button (the same authed blob URL with a
// download attribute) alongside the render.
//
// Self-contained like Lightbox (click backdrop / × / Esc closes; a click on the
// panel does not dismiss): the caller holds the open state and passes the blob's
// serve url + display title. Shared by the chat attachment strip AND the task
// artifact popover — one preview surface, not two.
//
// THREE SOURCE MODES (one surface, still not three):
//   - `url`      — a stored blob, fetched as text (or rendered as an image when
//                  its mime says so). It carries a REQUIRED `attachmentId`, so
//                  the header keeps both blob actions: the copyable share link
//                  and the 下載 link.
//   - `source`   — text the caller ALREADY holds (a chat message body). Nothing
//                  to fetch, nothing to download and nothing to share: the bytes
//                  never were a file, so both links are absent rather than
//                  pointing at a fabricated blob url. Single newlines are HARD
//                  breaks here, matching the chat bubble the text came from.
//   - `imageSrc` — image bytes the caller ALREADY holds as a `data:` URI: a
//                  STAGED composer attachment that has not been sent yet
//                  (T-f014, retiring the separate Lightbox overlay). It is a
//                  real file, so 下載 is honest — but it has no blob id yet, so
//                  there is nothing for a share link to point at and none is
//                  rendered.
//
// T-7e68 — ZOOM MUST AFFECT LAYOUT. A `transform: scale()` on the <img> paints
// bigger pixels but leaves the layout box the original size, so the wrap's
// `overflow: auto` never has anything to scroll and every edge the zoom pushed
// past the frame is clipped and unreachable (owner report: 「可以放大，但無法左
// 右或上下移動」). The zoom is therefore the image's own width/height — the
// measured 100% box times the zoom factor, with the stylesheet's percentage
// caps switched off so they cannot scale it a second time. The wrap then has
// genuine scrollable content, and TWO ways to reach it, not one: a pointer drag
// and the native scroll (scrollbar, wheel, arrow keys on the focusable wrap).
// Both drive the SAME scrollLeft/scrollTop, so there is no second offset to
// keep in sync and returning to 100% recentres with no residue.
//
// 🔴 THE CHEAP FIX DOES NOT WORK, and it is the first thing anyone will try:
// keeping `transform: scale()` and adding `transform-origin: 0 0` (with or
// without padding on the parent to "reserve" the space). Transformed overflow
// does NOT contribute to an ancestor scroll container's scrollable region —
// measured at 400%, `scrollWidth - clientWidth` stays 0, so there is still
// nothing to scroll and the corners are still unreachable. The zoom has to be
// real layout. Do not spend the afternoon re-deriving this.
//
// T-f014 — this is now the ONLY full-size image surface in the cockpit. The old
// Lightbox overlay and its stylesheet block are gone; every image (stored or staged
// composer preview) opens here and therefore gets the same shell: filename in
// the header, 下載, close, Esc/backdrop dismissal and the zoom controls.

import type { PointerEvent as ReactPointerEvent } from "react";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { authedAttachmentUrl } from "../api/http";
import { copyAttachmentShareLink } from "../lib/shareLink";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { Markdown } from "./Markdown";
import "./md-preview.css";
import {
  CheckIcon,
  CloseIcon,
  CopyIcon,
  DownloadIcon,
  FileTextIcon,
} from "./icons";

const ZOOM_MIN = 0.5;
const ZOOM_MAX = 4;
const ZOOM_STEP = 0.25;

function clampZoom(value: number): number {
  return Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, value));
}

type MarkdownPreviewOverlayProps = {
  /** Display name shown in the header (the blob's filename, or the sender of
   * the message being read). */
  title: string;
  onClose: () => void;
} & (
  | {
      /** The blob's serve path (`/api/chat/attachment/<id>`); fetched as text. */
      url: string;
      /** The blob's own id — the subject of the copyable share link. REQUIRED
       * with `url`: a stored blob always has one, and the union is what stops a
       * caller from opening a file view with no way to share it. */
      attachmentId: string;
      /** The stored blob kind decides the body renderer. Omitted keeps the
       * original markdown-file contract for existing callers. */
      mime?: string;
      source?: never;
      imageSrc?: never;
    }
  | {
      /** Markdown text the caller already holds — rendered as-is, no fetch. */
      source: string;
      url?: never;
      /** No blob, so no share link: there is nothing for a link to point at. */
      attachmentId?: never;
      mime?: never;
      imageSrc?: never;
    }
  | {
      /** Image bytes the caller already holds (`data:` URI) — a staged composer
       * attachment. Downloadable (the bytes ARE the file) but not shareable:
       * nothing has been uploaded, so no blob id exists to mint a link from. */
      imageSrc: string;
      url?: never;
      attachmentId?: never;
      mime?: never;
      source?: never;
    }
);

export function MarkdownPreviewOverlay({
  title,
  url,
  attachmentId,
  mime,
  imageSrc,
  source: inlineSource,
  onClose,
}: MarkdownPreviewOverlayProps) {
  const { t } = useI18n();
  const [fetched, setFetched] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [copied, setCopied] = useState(false);
  const [copyFailed, setCopyFailed] = useState(false);
  // The text to render. An inline source is authoritative and synchronous — it
  // never passes through the loading/error states, which only describe a fetch.
  const image = imageSrc !== undefined || (mime?.startsWith("image/") ?? false);
  const previewableText = isPreviewableTextAttachment(mime ?? "text/markdown", title);
  const unavailable = url !== undefined && !image && !previewableText;
  const plainText = previewableText && !isMarkdownAttachment(mime ?? "text/markdown", title);
  const source = inlineSource ?? fetched;
  const [zoom, setZoom] = useState(1);
  // The bytes the header's 下載 link points at. A stored blob needs the ?token=
  // gate; a staged data: URI already IS the bytes. An inline text `source` has
  // neither — nothing is fabricated for it.
  const downloadHref =
    imageSrc ?? (url !== undefined ? authedAttachmentUrl(url) : undefined);
  // The <img> src, for whichever of the two image modes is in play. Same value
  // as the download href by construction — the preview and the download must
  // never be able to point at different bytes.
  const imageBytes = image ? downloadHref : undefined;
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const imageRef = useRef<HTMLImageElement | null>(null);
  // The image's box at 100% — the size CSS gives it after `max-width: 100%` /
  // `max-height: 70vh` have had their say. Measured rather than recomputed in
  // JS so the zoom can never disagree with the stylesheet about what "fits".
  const [fitBox, setFitBox] = useState<{ w: number; h: number } | null>(null);
  const [panning, setPanning] = useState(false);

  const measureFit = useCallback(() => {
    const el = imageRef.current;
    if (!el) return;
    // Read the box the STYLESHEET would give this image — which means the
    // inline size has to come off for the duration of the read. Above 100% the
    // inline size IS the zoom, so measuring through it would just echo the zoom
    // back as the new "fit" and every later recompute would compound on it.
    const inline = {
      width: el.style.width,
      height: el.style.height,
      maxWidth: el.style.maxWidth,
      maxHeight: el.style.maxHeight,
    };
    el.style.width = "";
    el.style.height = "";
    el.style.maxWidth = "";
    el.style.maxHeight = "";
    const rect = el.getBoundingClientRect();
    Object.assign(el.style, inline);
    // jsdom has no layout engine, so every rect is 0×0 there; a zero box is
    // "not measured yet", never a real size to zoom from.
    if (rect.width > 0 && rect.height > 0) setFitBox({ w: rect.width, h: rect.height });
  }, []);

  // THE zoom seam. The zoom is the image's own LAYOUT size, so the frame gets
  // real scrollable content; a `transform: scale()` paints bigger pixels while
  // the layout box stays put, leaving `overflow: auto` with nothing to scroll
  // and every magnified edge clipped away (T-7e68).
  //
  // The stylesheet's caps have to be turned off with it. They are percentages
  // of whatever box contains the image, so leaving them on lets the image grow
  // a second time on its own — a 1600×400 shot at 200% painted at 4× the fit
  // box, and the "200%" readout was simply a lie.
  //
  // At 100% the size is left to the stylesheet: that is the state `measureFit`
  // reads the fit box out of, so pinning it there would freeze the first
  // measurement forever.
  const zoomedSize =
    fitBox === null || zoom === 1
      ? undefined
      : { width: fitBox.w * zoom, height: fitBox.h * zoom, maxWidth: "none", maxHeight: "none" };

  // Both caps are viewport-relative, so a resize moves the 100% box and the fit
  // must be re-read — AT ANY ZOOM, not only at 100%. Skipping the re-read while
  // zoomed made the percentage lie: 300% measured at 900x700 stayed 2154px wide
  // after the window shrank to 500x420, where the true 100% box is ~394px — a
  // real 5.5x still announcing itself as 300%. (The transform version had no
  // such drift, so leaving this out would have been a regression, not a
  // leftover.) `measureFit` strips the inline size before reading, which is
  // what makes measuring while zoomed meaningful at all.
  useLayoutEffect(() => {
    if (!image) return;
    measureFit();
    window.addEventListener("resize", measureFit);
    return () => window.removeEventListener("resize", measureFit);
  }, [image, imageBytes, measureFit]);

  // Fetch the markdown text once (the authed blob URL — same ?token= gate the
  // download/thumbnail paths use). A non-ok response / network error surfaces
  // the honest error state, never a blank render.
  useEffect(() => {
    if (url === undefined || image || unavailable) return;
    let alive = true;
    setFetched(null);
    setFailed(false);
    fetch(authedAttachmentUrl(url))
      .then((r) => {
        if (!r.ok) throw new Error(`http ${r.status}`);
        return r.text();
      })
      .then((text) => {
        if (alive) setFetched(text);
      })
      .catch((e) => {
        if (alive) setFailed(true);
        console.warn("MarkdownPreviewOverlay: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, [url, image, unavailable]);

  useEffect(() => setZoom(1), [url, imageSrc]);

  // Back at 100% the stage fits the frame again, so any pan offset left over
  // from the zoomed view has to go with it — otherwise the recentred image
  // sits behind a stale scroll position.
  useEffect(() => {
    if (zoom !== 1) return;
    const wrap = wrapRef.current;
    if (!wrap) return;
    wrap.scrollLeft = 0;
    wrap.scrollTop = 0;
  }, [zoom]);

  // Wheel-zoom is bound natively and non-passively: React 18 attaches its
  // listeners at the root as passive, where `e.preventDefault()` in a JSX
  // `onWheel` is ignored and the page scrolls behind the overlay while the
  // image zooms.
  useEffect(() => {
    const wrap = wrapRef.current;
    if (!wrap || !image) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      setZoom((current) => clampZoom(current + (e.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP)));
    };
    wrap.addEventListener("wheel", onWheel, { passive: false });
    return () => wrap.removeEventListener("wheel", onWheel);
  }, [image, imageBytes]);

  // T-043e — PINCH ZOOMS THE IMAGE, NOT THE PAGE.
  //
  // Before this, two fingers on the image were left to the UA, and on a phone
  // that does not magnify the picture: an untouched viewport meta means a pinch
  // scales the VISUAL viewport, while this overlay is `position: fixed` against
  // the LAYOUT viewport — so header, buttons and backdrop grow with it and the
  // owner is looking at a blown-up modal rather than a blown-up photo. That is
  // the「整個視窗變大」half of the report, and quite possibly all of it: an
  // owner who only ever pinches never reaches the −/+ controls at all, so the
  // app's own zoom never moves and the picture never gets bigger.
  //
  // 🔴 The one-line "fix" is forbidden: `user-scalable=no` / `maximum-scale=1`
  // on the viewport meta would stop it by taking page zoom away from the WHOLE
  // app — an accessibility regression the owner explicitly rejected. The zoom
  // has to be claimed by this element instead of denied to the document.
  //
  // Claiming it is two things, because one is not portable:
  //   - `touch-action: pan-x pan-y` on the frame (md-preview.css) tells the
  //     compositor this element takes panning but NOT pinch-zoom, so the UA
  //     stops routing the gesture to page zoom before any JS runs. Measured in
  //     Chromium: this half alone already holds `visualViewport.scale` at 1.
  //   - the handlers below, which turn the two-finger distance ratio into the
  //     app's own zoom state — the half that actually magnifies anything.
  //     `preventDefault()` on the two-finger touchstart / touchmove is also
  //     what keeps the gesture ours where `touch-action` is not honoured: iOS
  //     Safari's suppression of pinch through it has historically been partial,
  //     which is why the WebKit-only `gesturestart`/`gesturechange` pair is
  //     bound as a second route.
  //
  // The two routes are mutually exclusive by `pinching`: iOS fires gesture
  // events ALONGSIDE touch events, and letting both drive `setZoom` would apply
  // the same spread twice.
  const zoomRef = useRef(zoom);
  zoomRef.current = zoom;
  useEffect(() => {
    const wrap = wrapRef.current;
    if (!wrap || !image) return;
    let pinching = false;
    let startSpread = 0;
    let startZoom = 1;
    let gestureZoom = 1;

    const spread = (touches: TouchList) =>
      Math.hypot(
        touches[0].clientX - touches[1].clientX,
        touches[0].clientY - touches[1].clientY,
      );

    const onTouchStart = (e: TouchEvent) => {
      // One finger is NOT ours — it is the native scroll of this container
      // (see the gesture-ownership note on `onPanPointerDown`).
      if (e.touches.length !== 2) return;
      e.preventDefault();
      pinching = true;
      startSpread = spread(e.touches);
      startZoom = zoomRef.current;
    };
    const onTouchMove = (e: TouchEvent) => {
      if (!pinching || e.touches.length !== 2) return;
      e.preventDefault();
      if (startSpread <= 0) return;
      setZoom(clampZoom(startZoom * (spread(e.touches) / startSpread)));
    };
    const onTouchEnd = (e: TouchEvent) => {
      if (e.touches.length < 2) pinching = false;
    };

    // WebKit-only. Bound on the frame rather than the document so a pinch
    // started anywhere ELSE on the page still zooms the page normally — the
    // accessibility escape hatch stays open, it is only the image that claims
    // the gesture.
    const onGestureStart = (e: Event) => {
      e.preventDefault();
      gestureZoom = zoomRef.current;
    };
    const onGestureChange = (e: Event) => {
      e.preventDefault();
      if (pinching) return;
      const scale = (e as Event & { scale?: number }).scale;
      if (typeof scale !== "number" || scale <= 0) return;
      setZoom(clampZoom(gestureZoom * scale));
    };

    wrap.addEventListener("touchstart", onTouchStart, { passive: false });
    wrap.addEventListener("touchmove", onTouchMove, { passive: false });
    wrap.addEventListener("touchend", onTouchEnd);
    wrap.addEventListener("touchcancel", onTouchEnd);
    wrap.addEventListener("gesturestart", onGestureStart as EventListener, { passive: false });
    wrap.addEventListener("gesturechange", onGestureChange as EventListener, { passive: false });
    wrap.addEventListener("gestureend", onGestureStart as EventListener, { passive: false });
    return () => {
      wrap.removeEventListener("touchstart", onTouchStart);
      wrap.removeEventListener("touchmove", onTouchMove);
      wrap.removeEventListener("touchend", onTouchEnd);
      wrap.removeEventListener("touchcancel", onTouchEnd);
      wrap.removeEventListener("gesturestart", onGestureStart as EventListener);
      wrap.removeEventListener("gesturechange", onGestureChange as EventListener);
      wrap.removeEventListener("gestureend", onGestureStart as EventListener);
    };
  }, [image, imageBytes]);

  // Dragging IS scrolling: the pointer delta is applied to the wrap's own
  // scroll offset, the same offset the scrollbar and the arrow keys move. One
  // source of truth for "where in the image am I", so the two routes to the
  // overflow can never drift apart.
  function onPanPointerDown(e: ReactPointerEvent<HTMLDivElement>) {
    const wrap = wrapRef.current;
    // GESTURE OWNERSHIP, ONE FINGER — on touch the BROWSER moves the image,
    // not this handler, and that is a decision rather than an omission (owner
    // asked for phone dragging on 2026-07-31, against the unfixed build where
    // nothing moved at all). Now that the zoom is real layout, one finger pans
    // this scroll container natively, with inertia and rubber-banding we would
    // otherwise have to reimplement. Running the drag below as WELL would apply
    // the same delta twice — a finger-width of travel would move the image two
    // — so exactly one of the two may be in charge. Verified with real
    // input-layer touch events: scrollLeft 0 → 451 for a 200px swipe. Deleting
    // this bail-out to "add touch support" re-introduces the double-apply.
    //
    // ⚠️ TWO fingers are NO LONGER the UA's (T-043e, 2026-07-31 owner ruling:
    // 「在手機上二指撐開，要放大的是圖片本身，頁面不動」). The pinch handler
    // above claims them and drives this component's zoom state; that is a
    // separate route on separate events, so it does not resurrect the
    // double-apply this bail-out exists to prevent. Keep both.
    if (!wrap || e.button !== 0 || e.pointerType === "touch") return;
    if (wrap.scrollWidth <= wrap.clientWidth && wrap.scrollHeight <= wrap.clientHeight) return;
    const startX = e.clientX;
    const startY = e.clientY;
    const startLeft = wrap.scrollLeft;
    const startTop = wrap.scrollTop;
    wrap.setPointerCapture(e.pointerId);
    setPanning(true);
    const onMove = (move: PointerEvent) => {
      wrap.scrollLeft = startLeft - (move.clientX - startX);
      wrap.scrollTop = startTop - (move.clientY - startY);
    };
    const onUp = () => {
      wrap.removeEventListener("pointermove", onMove);
      wrap.removeEventListener("pointerup", onUp);
      wrap.removeEventListener("pointercancel", onUp);
      setPanning(false);
    };
    wrap.addEventListener("pointermove", onMove);
    wrap.addEventListener("pointerup", onUp);
    wrap.addEventListener("pointercancel", onUp);
  }

  async function onCopyShareLink() {
    // Only a stored blob has an id to share; the button below is not rendered
    // for an inline source, so this guard is the type-level echo of that.
    if (attachmentId === undefined) return;
    setCopyFailed(false);
    try {
      await copyAttachmentShareLink(attachmentId);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch (e) {
      console.warn("MarkdownPreviewOverlay: copy share link failed", e);
      setCopyFailed(true);
      window.setTimeout(() => setCopyFailed(false), 2000);
    }
  }

  // Esc closes — as the TOP layer. The overlay only mounts open, so it holds a
  // layer for exactly its lifetime; whatever opened it (a popover, a gallery,
  // a chat thread) sits below and does not see the key.
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);

  // T-76cd — PORTALLED TO `document.body`, and that is a correctness property,
  // not tidiness. `z-index: 1100` is only worth what its nearest stacking-context
  // ancestor is worth: rendered in place, this overlay sat inside the task
  // artifacts popover, whose `z-index: 40` scoped the 1100 to that box — header
  // and tab bar then painted over the preview and the close button was
  // unreachable (owner, on his iPhone: 「看不到按關閉的按鈕, 被擋住了 且上面的 tab
  // 全部都不能按」). Every OTHER host has the same exposure, latent: any ancestor
  // with a z-index, an opacity, an `isolation`, or a `transform` (which also
  // traps a fixed element's containing block) would confine it the same way.
  // Portalling makes the overlay's root stacking context the ROOT one by
  // construction, so no host can confine it and no host has to promise not to.
  //
  // 🔴 THIS BREAKS DOM CONTAINMENT, AND ONE CALLER DEPENDED ON IT.
  // TaskArtifactsPopover's click-outside dismissal used to be satisfied for free
  // — the overlay lived inside `anchorRef`, so a click on this backdrop counted
  // as "inside" and left the popover open (owner's 2026-07-20 ruling: 「點其他
  // 地方都不會自動關閉,一定要點 X」). After the portal that containment is gone,
  // so that popover matches this root by SELECTOR instead (`closest(".md-preview")`).
  // Anything else that reasons about this overlay by ancestry has to do the same.
  return createPortal(
    <div
      ref={rootRef}
      className="md-preview"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={onClose}
    >
      <div className="md-preview__panel" onClick={(e) => e.stopPropagation()}>
        <div className="md-preview__header">
          <span className="md-preview__title">
            <FileTextIcon size={16} />
            {title}
          </span>
          <div className="md-preview__actions">
            {/* Share needs a STORED blob id. Download only needs bytes, so it
             * also serves a staged `imageSrc`. An inline text source has
             * neither — nothing is fabricated for it. */}
            {attachmentId !== undefined && (
              <button
                type="button"
                className="md-preview__download md-preview__share"
                aria-label={
                  copyFailed
                    ? t.chat.shareLinkCopyFailed
                    : copied
                      ? t.chat.shareLinkCopied
                      : t.chat.copyShareLink
                }
                title={
                  copyFailed
                    ? t.chat.shareLinkCopyFailed
                    : copied
                      ? t.chat.shareLinkCopied
                      : t.chat.copyShareLink
                }
                onClick={() => void onCopyShareLink()}
              >
                {copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
                {copyFailed
                  ? <span className="md-preview__action-label">{t.chat.shareLinkCopyFailed}</span>
                  : copied
                    ? <span className="md-preview__action-label">{t.chat.shareLinkCopied}</span>
                    : <span className="md-preview__action-label">{t.chat.copyShareLink}</span>}
              </button>
            )}
            {/* Download — the SECOND action, distinct from preview: the authed
             * blob URL (or the staged data: URI) with a download attribute. */}
            {downloadHref !== undefined && (
              <a
                className="md-preview__download"
                href={downloadHref}
                download={title || undefined}
                aria-label={t.chat.mdPreview.download}
                title={t.chat.mdPreview.download}
              >
                <DownloadIcon size={14} />
                <span className="md-preview__action-label">{t.chat.mdPreview.download}</span>
              </a>
            )}
            <button
              type="button"
              className="md-preview__close"
              aria-label={t.chat.mdPreview.close}
              onClick={onClose}
            >
              <CloseIcon size={16} />
            </button>
          </div>
        </div>
        <div className="md-preview__body">
          {image && imageBytes !== undefined ? (
            <div className="md-preview__image-viewport">
              <div
                className={
                  "md-preview__image-wrap" +
                  (fitBox !== null && zoom > 1 ? " md-preview__image-wrap--pannable" : "") +
                  (panning ? " md-preview__image-wrap--panning" : "")
                }
                ref={wrapRef}
                /* Focusable and named so the overflow is reachable without a
                 * pointer at all: arrow keys / PageUp / Home scroll a focused
                 * overflow container natively. */
                tabIndex={0}
                role="group"
                aria-label={t.chat.mdPreview.pan}
                onPointerDown={onPanPointerDown}
              >
                <img
                  className="md-preview__image"
                  ref={imageRef}
                  src={imageBytes}
                  /* The filename IS the alt text: it is the only thing known
                   * about these bytes, and a generic 「圖片」 would tell a screen
                   * reader nothing that the surrounding dialog did not. */
                  alt={title || t.chat.imageAlt}
                  draggable={false}
                  onLoad={measureFit}
                  style={zoomedSize}
                />
              </div>
              {/* The zoom cluster is a labelled group, and each control names
               * itself: the bare −/+ glyphs announce as "minus"/"plus" with no
               * hint of what they act on, and the hard-coded English label the
               * first version carried was invisible to the wording overlay. */}
              <div className="md-preview__zoom" role="group" aria-label={t.chat.mdPreview.zoomControls}>
                <button
                  type="button"
                  aria-label={t.chat.mdPreview.zoomOut}
                  title={t.chat.mdPreview.zoomOut}
                  onClick={() => setZoom((value) => clampZoom(value - ZOOM_STEP))}
                >
                  −
                </button>
                <span>{Math.round(zoom * 100)}%</span>
                <button
                  type="button"
                  aria-label={t.chat.mdPreview.zoomIn}
                  title={t.chat.mdPreview.zoomIn}
                  onClick={() => setZoom((value) => clampZoom(value + ZOOM_STEP))}
                >
                  +
                </button>
              </div>
            </div>
          ) : unavailable ? (
            <div className="md-preview__status">{t.chat.mdPreview.unavailable}</div>
          ) : failed ? (
            <div className="md-preview__status">{t.chat.mdPreview.error}</div>
          ) : source === null ? (
            <div className="md-preview__status">{t.chat.mdPreview.loading}</div>
          ) : (
            /* `.doc-md` is the SHARED markdown skin (headings, code, tables,
             * links, callouts) every other render site wears — the task manual,
             * the role doc, the reply card, the chat bubble. Rendering without
             * it left this overlay on bare UA defaults: black-on-card headings,
             * unstyled code, no callout colour. One class, same document look
             * as the surface the file was opened from. */
            plainText ? (
              <pre className="md-preview__text">{source}</pre>
            ) : <Markdown
              source={source}
              className="md-preview__md doc-md"
              /* An inline source is a CHAT message: Enter meant "new line"
               * when it was typed and the bubble renders it that way, so the
               * full-view read of the same text must keep those newlines.
               * Standard markdown folds them into spaces, which reflowed a
               * plain multi-line message into one run-on line. A fetched .md
               * blob is a document, not a chat line — it keeps standard
               * soft-wrap, same as every other document surface. */
              breaks={inlineSource !== undefined}
            />
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}

/** Whether an attachment (by mime / filename) is a markdown doc the preview
 * overlay can render. Mirrors the server's text/markdown handling; also accepts
 * a `.md`/`.markdown` filename when the mime is a generic text/plain. */
export function isMarkdownAttachment(mime: string, filename: string): boolean {
  if (mime === "text/markdown" || mime === "text/x-markdown") return true;
  const name = filename.toLowerCase();
  return name.endsWith(".md") || name.endsWith(".markdown");
}

/** The stored text formats supported by the shared attachment modal. Keep this
 * narrow: HTML/PDF and arbitrary text/* remain outside this preview contract. */
export function isPreviewableTextAttachment(mime: string, filename: string): boolean {
  return isMarkdownAttachment(mime, filename) || /\.(txt|log)$/i.test(filename);
}
