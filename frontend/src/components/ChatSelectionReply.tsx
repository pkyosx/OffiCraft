import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { RefObject } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import {
  REPLY_EXCERPT_MAX_CHARS,
  normalizeReplyExcerpt,
  replyExcerptTooLong,
} from "../lib/replyExcerpt";
import { placeUnderSelection, rangeInside } from "../lib/selectionFloat";
import { ReplyIcon } from "./icons";
import "./office.css";

const BODY_SELECTOR = ".chat__msg-text, .reply-card__summary, .reply-card__body";
const INTERACTIVE_SELECTOR =
  "button, a, input, textarea, select, [role='button'], [role='menu'], [role='dialog']";

interface Picked {
  msgId: string;
  excerpt: string;
  rect: DOMRect;
}

function bodyOf(node: Node | null, root: HTMLElement): HTMLElement | null {
  const el = node instanceof Element ? node : (node?.parentElement ?? null);
  const body = el?.closest<HTMLElement>(BODY_SELECTOR) ?? null;
  return body && root.contains(body) ? body : null;
}

/** The current selection, when it lies inside ONE message body of `root`. */
function pickSelection(root: HTMLElement | null): Picked | null {
  const sel = window.getSelection();
  if (!root || !sel || sel.isCollapsed || sel.rangeCount === 0) return null;
  const range = sel.getRangeAt(0);
  const body = bodyOf(range.startContainer, root);
  if (!body || bodyOf(range.endContainer, root) !== body) return null;
  const inside = rangeInside(range, body);
  if (!inside) return null;
  if (
    Array.from(body.querySelectorAll(INTERACTIVE_SELECTOR)).some((element) =>
      range.intersectsNode(element),
    )
  ) {
    return null;
  }
  const msgId = body.closest("[data-msg-id]")?.getAttribute("data-msg-id");
  const excerpt = normalizeReplyExcerpt(sel.toString());
  if (!msgId || !excerpt) return null;
  return { msgId, excerpt, rect: inside.getBoundingClientRect() };
}

/** The floating 「回覆這段」 button for a text selection in a chat bubble body.
 * Portaled to <body> with fixed coordinates so it never takes space in the
 * thread. */
export function ChatSelectionReply({
  root,
  onReply,
}: {
  root: RefObject<HTMLElement | null>;
  onReply: (msgId: string, excerpt: string) => void;
}) {
  const { t } = useI18n();
  const [picked, setPicked] = useState<Picked | null>(null);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  const refresh = useCallback(() => {
    setPicked(pickSelection(root.current));
  }, [root]);

  useEffect(() => {
    document.addEventListener("selectionchange", refresh);
    window.addEventListener("scroll", refresh, true);
    window.addEventListener("resize", refresh);
    return () => {
      document.removeEventListener("selectionchange", refresh);
      window.removeEventListener("scroll", refresh, true);
      window.removeEventListener("resize", refresh);
    };
  }, [refresh]);

  useLayoutEffect(() => {
    const btn = buttonRef.current;
    if (!picked || !btn) {
      setPos(null);
      return;
    }
    setPos(placeUnderSelection(picked.rect, btn.getBoundingClientRect()));
  }, [picked]);

  if (!picked) return null;
  const tooLong = replyExcerptTooLong(picked.excerpt);
  const label = tooLong
    ? t.chat.replyExcerptTooLong(REPLY_EXCERPT_MAX_CHARS)
    : t.chat.replyExcerptAction;
  return createPortal(
    <button
      ref={buttonRef}
      type="button"
      className="chat__selection-reply"
      data-testid="chat-selection-reply"
      disabled={tooLong}
      style={
        pos
          ? { left: pos.left, top: pos.top }
          : { left: 0, top: 0, visibility: "hidden" }
      }
      // Pressing a button would otherwise collapse the selection it acts on
      // before the click lands, and the button would vanish under the pointer.
      onPointerDown={(e) => e.preventDefault()}
      onMouseDown={(e) => e.preventDefault()}
      onClick={() => {
        onReply(picked.msgId, picked.excerpt);
        window.getSelection()?.removeAllRanges();
        setPicked(null);
      }}
    >
      {!tooLong && <ReplyIcon size={12} aria-hidden="true" />}
      <span>{label}</span>
    </button>,
    document.body,
  );
}
