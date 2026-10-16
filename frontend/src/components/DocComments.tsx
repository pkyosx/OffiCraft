// components/DocComments.tsx — comment mode of the document preview: select
// text (or tap an image) in the rendered document, attach a comment to it, and
// send every comment to the AI as one chat message.

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { RefObject } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import {
  formatDocCommentMessage,
  imageCommentSrc,
  imageShortName,
  loadDocCommentDraft,
  locateExcerpt,
  locateImage,
  saveDocCommentDraft,
} from "../lib/docComments";
import { placeUnderSelection, rangeInside } from "../lib/selectionFloat";
import type { DocComment, DocCommentAnchor, DocCommentTarget } from "../lib/docComments";
import "./doc-comments.css";
import { PencilIcon, SendIcon, TrashIcon } from "./icons";

const HIGHLIGHT_NAME = "oc-doc-comment";
const IMAGE_MARK = "data-doc-comment";

type HighlightRegistry = { set(name: string, h: unknown): void; delete(name: string): void };
type HighlightCtor = new (...ranges: Range[]) => unknown;

function highlightApi(): { registry: HighlightRegistry; Highlight: HighlightCtor } | null {
  const registry = (globalThis.CSS as { highlights?: HighlightRegistry } | undefined)?.highlights;
  const Highlight = (globalThis as { Highlight?: HighlightCtor }).Highlight;
  return registry && Highlight ? { registry, Highlight } : null;
}

function newCommentId(): string {
  return `c${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
}

type Pick = { anchor: DocCommentAnchor; rect: DOMRect };

/** Reads the selection inside `docRef` and offers 加註解 next to it. */
function useDocSelection(docRef: RefObject<HTMLElement | null>, active: boolean) {
  const [selection, setSelection] = useState<Pick | null>(null);
  useEffect(() => {
    if (!active) {
      setSelection(null);
      return;
    }
    const read = () => {
      const sel = document.getSelection();
      const root = docRef.current;
      if (!sel || sel.rangeCount === 0 || sel.isCollapsed || !root) {
        setSelection(null);
        return;
      }
      const inside = rangeInside(sel.getRangeAt(0), root);
      const excerpt = sel.toString().trim();
      if (excerpt === "" || !inside) {
        setSelection(null);
        return;
      }
      setSelection({ anchor: { kind: "text", excerpt }, rect: inside.getBoundingClientRect() });
    };
    document.addEventListener("selectionchange", read);
    document.addEventListener("mouseup", read);
    document.addEventListener("keyup", read);
    return () => {
      document.removeEventListener("selectionchange", read);
      document.removeEventListener("mouseup", read);
      document.removeEventListener("keyup", read);
    };
  }, [docRef, active]);
  return [selection, () => setSelection(null)] as const;
}

/** The image last clicked (tapped) inside `docRef`. The listener sits on the
 * document root rather than `document`: iOS does not deliver a tap on a plain
 * image to a listener on `document`. */
function useDocImagePick(docRef: RefObject<HTMLElement | null>, active: boolean) {
  const [pick, setPick] = useState<Pick | null>(null);
  useEffect(() => {
    const root = docRef.current;
    if (!active || !root) {
      setPick(null);
      return;
    }
    const onClick = (e: MouseEvent) => {
      const img = e.target instanceof Element ? e.target.closest("img") : null;
      const src = img?.getAttribute("src");
      if (!img || !src) {
        setPick(null);
        return;
      }
      setPick({
        anchor: { kind: "image", alt: img.getAttribute("alt") ?? "", src: imageCommentSrc(src) },
        rect: img.getBoundingClientRect(),
      });
    };
    root.addEventListener("click", onClick);
    return () => root.removeEventListener("click", onClick);
  }, [docRef, active]);
  return [pick, () => setPick(null)] as const;
}

function newComment(anchor: DocCommentAnchor, comment: string): DocComment {
  const id = newCommentId();
  return anchor.kind === "image"
    ? { id, kind: "image", alt: anchor.alt, src: anchor.src, comment }
    : { id, excerpt: anchor.excerpt, comment };
}

function anchorOf(c: DocComment): DocCommentAnchor {
  return c.kind === "image"
    ? { kind: "image", alt: c.alt, src: c.src }
    : { kind: "text", excerpt: c.excerpt };
}

export function DocCommentPanel({
  attachmentId,
  filename,
  target,
  docRef,
  docReady,
  imagePickable,
  onSent,
}: {
  attachmentId: string;
  filename: string;
  target: DocCommentTarget;
  /** The rendered document the excerpts are selected from and highlighted in. */
  docRef: RefObject<HTMLElement | null>;
  docReady: boolean;
  /** Whether the body can show images, i.e. whether a click on one is a pick. */
  imagePickable: boolean;
  onSent: () => void;
}) {
  const { t } = useI18n();
  const tc = t.chat.mdPreview.comments;
  const [comments, setComments] = useState<DocComment[]>(() => loadDocCommentDraft(attachmentId));
  const [composing, setComposing] = useState<{ anchor: DocCommentAnchor; text: string } | null>(null);
  const [editing, setEditing] = useState<{ id: string; text: string } | null>(null);
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState<string | null>(null);
  const [unlocated, setUnlocated] = useState<Set<string>>(new Set());
  const [selection, clearSelection] = useDocSelection(docRef, docReady);
  const [imagePick, clearImagePick] = useDocImagePick(docRef, docReady && imagePickable);
  const pick = selection ?? imagePick;
  const floatRef = useRef<HTMLButtonElement>(null);
  const composeRef = useRef<HTMLTextAreaElement>(null);

  function commit(next: DocComment[]) {
    setComments(next);
    saveDocCommentDraft(attachmentId, next);
  }

  useEffect(() => {
    const root = docRef.current;
    if (!docReady || !root) return;
    const ranges: Range[] = [];
    const images: HTMLImageElement[] = [];
    const missing = new Set<string>();
    for (const c of comments) {
      if (c.kind === "image") {
        const found = locateImage(root, c.src);
        if (found.length > 0) images.push(...found);
        else missing.add(c.id);
        continue;
      }
      const range = locateExcerpt(root, c.excerpt);
      if (range) ranges.push(range);
      else missing.add(c.id);
    }
    setUnlocated(missing);
    // An attribute React does not render, so reconciling the <img> leaves it be.
    for (const img of images) img.setAttribute(IMAGE_MARK, "");
    const hl = highlightApi();
    if (hl) hl.registry.set(HIGHLIGHT_NAME, new hl.Highlight(...ranges));
    return () => {
      for (const img of images) img.removeAttribute(IMAGE_MARK);
      hl?.registry.delete(HIGHLIGHT_NAME);
    };
  }, [comments, docReady, docRef]);

  useLayoutEffect(() => {
    const el = floatRef.current;
    if (!el || !pick) return;
    const { left, top } = placeUnderSelection(pick.rect, {
      width: el.offsetWidth,
      height: el.offsetHeight,
    });
    el.style.left = `${left}px`;
    el.style.top = `${top}px`;
  }, [pick]);

  useEffect(() => {
    if (composing) composeRef.current?.focus();
  }, [composing]);

  function startComposing() {
    if (!pick) return;
    setComposing({ anchor: pick.anchor, text: "" });
    document.getSelection()?.removeAllRanges();
    clearSelection();
    clearImagePick();
  }

  function saveComposing() {
    if (!composing || composing.text.trim() === "") return;
    commit([...comments, newComment(composing.anchor, composing.text.trim())]);
    setComposing(null);
  }

  function saveEditing() {
    if (!editing || editing.text.trim() === "") return;
    commit(comments.map((c) => (c.id === editing.id ? { ...c, comment: editing.text.trim() } : c)));
    setEditing(null);
  }

  const recipient = target.recipient;
  const blockedReason =
    recipient === null
      ? tc.recipientUnknown
      : recipient.roster === "left"
        ? tc.recipientGone
        : recipient.roster === "not-member"
          ? tc.recipientNotMember
          : null;
  const canSend =
    blockedReason === null && recipient?.roster === "active" && comments.length > 0 && !sending;

  async function send() {
    if (!canSend || recipient === null) return;
    setSending(true);
    setSendError(null);
    try {
      await api.postChat({
        to: recipient.id,
        body: formatDocCommentMessage({ filename, attachmentId, task: target.task, comments }),
      });
      const sent = new Set(comments.map((c) => c.id));
      saveDocCommentDraft(
        attachmentId,
        loadDocCommentDraft(attachmentId).filter((c) => !sent.has(c.id)),
      );
      onSent();
    } catch (e) {
      console.warn("DocCommentPanel: send failed", e);
      setSendError(e instanceof Error && e.message ? `${tc.sendFailed}（${e.message}）` : tc.sendFailed);
      setSending(false);
    }
  }

  const cancelOnEscape = (cancel: () => void) => (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      cancel();
    }
  };

  return (
    <aside className="doc-comments" aria-label={tc.panelTitle}>
      <div className="doc-comments__head">{tc.panelTitle}</div>
      <div className="doc-comments__list">
        {composing && (
          <div className="doc-comments__item doc-comments__item--compose">
            <Excerpt anchor={composing.anchor} />
            <textarea
              ref={composeRef}
              className="doc-comments__input"
              aria-label={tc.inputLabel}
              placeholder={tc.inputPlaceholder}
              value={composing.text}
              onChange={(e) => setComposing({ ...composing, text: e.target.value })}
              onKeyDown={cancelOnEscape(() => setComposing(null))}
            />
            <div className="doc-comments__row">
              <button type="button" className="doc-comments__btn" onClick={() => setComposing(null)}>
                {tc.cancel}
              </button>
              <button
                type="button"
                className="doc-comments__btn doc-comments__btn--primary"
                disabled={composing.text.trim() === ""}
                onClick={saveComposing}
              >
                {tc.save}
              </button>
            </div>
          </div>
        )}
        {comments.length === 0 && !composing && (
          <p className="doc-comments__empty">{imagePickable ? tc.empty : tc.emptyTextOnly}</p>
        )}
        {comments.map((c, i) => (
          <div key={c.id} className="doc-comments__item" data-testid="doc-comment">
            <Excerpt anchor={anchorOf(c)} />
            {unlocated.has(c.id) && (
              <p className="doc-comments__note">
                {c.kind === "image" ? tc.imageNotFound : tc.excerptNotFound}
              </p>
            )}
            {editing?.id === c.id ? (
              <>
                <textarea
                  className="doc-comments__input"
                  aria-label={tc.inputLabel}
                  value={editing.text}
                  autoFocus
                  onChange={(e) => setEditing({ id: c.id, text: e.target.value })}
                  onKeyDown={cancelOnEscape(() => setEditing(null))}
                />
                <div className="doc-comments__row">
                  <button type="button" className="doc-comments__btn" onClick={() => setEditing(null)}>
                    {tc.cancel}
                  </button>
                  <button
                    type="button"
                    className="doc-comments__btn doc-comments__btn--primary"
                    disabled={editing.text.trim() === ""}
                    onClick={saveEditing}
                  >
                    {tc.save}
                  </button>
                </div>
              </>
            ) : (
              <div className="doc-comments__row doc-comments__row--item">
                <p className="doc-comments__text">
                  <span className="doc-comments__index">{i + 1}.</span> {c.comment}
                </p>
                <button
                  type="button"
                  className="doc-comments__icon"
                  aria-label={tc.edit}
                  title={tc.edit}
                  onClick={() => setEditing({ id: c.id, text: c.comment })}
                >
                  <PencilIcon size={13} />
                </button>
                <button
                  type="button"
                  className="doc-comments__icon"
                  aria-label={tc.remove}
                  title={tc.remove}
                  onClick={() => commit(comments.filter((x) => x.id !== c.id))}
                >
                  <TrashIcon size={13} />
                </button>
              </div>
            )}
          </div>
        ))}
      </div>
      <div className="doc-comments__foot">
        <div className="doc-comments__recipient">
          {tc.recipientLabel}
          <strong>{recipient?.name || "—"}</strong>
        </div>
        {blockedReason && <p className="doc-comments__blocked">{blockedReason}</p>}
        {sendError && (
          <p className="doc-comments__error" role="alert">
            {sendError}
          </p>
        )}
        <button
          type="button"
          className="doc-comments__btn doc-comments__btn--primary doc-comments__send"
          disabled={!canSend}
          onClick={() => void send()}
        >
          <SendIcon size={13} />
          {tc.send}
        </button>
      </div>
      {/* Must stay after all panel text: a triple-click range can end at the panel's first text node. */}
      {pick && (
        <button
          ref={floatRef}
          type="button"
          className="doc-comments__float"
          onPointerDown={(e) => e.preventDefault()}
          onMouseDown={(e) => e.preventDefault()}
          onClick={startComposing}
        >
          <PencilIcon size={13} />
          {tc.add}
        </button>
      )}
    </aside>
  );
}

function Excerpt({ anchor }: { anchor: DocCommentAnchor }) {
  const { t } = useI18n();
  return (
    <blockquote className="doc-comments__excerpt">
      {anchor.kind === "image"
        ? t.chat.mdPreview.comments.imageExcerpt(anchor.alt.trim() || imageShortName(anchor.src))
        : anchor.excerpt}
    </blockquote>
  );
}
