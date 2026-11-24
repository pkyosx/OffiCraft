import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import type { ReactNode, RefObject } from "react";
import { createPortal, flushSync } from "react-dom";
import { useI18n } from "../i18n";
import {
  REPLY_EXCERPT_MAX_CHARS,
  excerptFromSelection,
  replyExcerptTooLong,
} from "../lib/replyExcerpt";
import { placeUnderSelection, rangeInside } from "../lib/selectionFloat";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { CopiedMark, useCopyLabel, type CopyState } from "./ChatMessageCopyButton";
import { CopyIcon, ReplyIcon } from "./icons";
import "./office.css";

const BAR_RESERVE_VAR = "--chat-select-bar-reserve";
const ROW_CLEARANCE_PX = 8;
// iOS's selection end handle reaches this far below the line, and its touch
// area further; a tap on the bar that lands on it moves the handle instead.
const SELECTION_END_CLEARANCE_PX = 48;
const SELECTION_SETTLE_MS = 120;
const CARD_BODY_SELECTOR = ".reply-card__summary, .reply-card__body";
const BODY_SELECTOR = `.chat__msg-text, ${CARD_BODY_SELECTOR}`;
const INTERACTIVE_SELECTOR =
  "button, a, input, textarea, select, [role='button'], [role='menu'], [role='dialog']";
// Inline words of the text itself, rendered as controls: a mention is a
// <button>, a link an <a>. A selection may run through them.
const INLINE_CONTENT_SELECTOR = ".md-mention, .md-doclink, a";

type QuoteIntoCard = (excerpt: string) => void;

const ReplyCardQuoteContext = createContext<Map<string, QuoteIntoCard> | null>(
  null,
);

/** Where 回覆這段 on a reply card's question lands: the answer box of that
 * card, registered by the card's own waiting body. Wrap the host's selection
 * root AND its ChatSelectionReply in one provider. */
export function ReplyCardQuoteProvider({ children }: { children: ReactNode }) {
  const [targets] = useState(() => new Map<string, QuoteIntoCard>());
  return (
    <ReplyCardQuoteContext.Provider value={targets}>
      {children}
    </ReplyCardQuoteContext.Provider>
  );
}

/** Registers `onQuote` as card `cardId`'s 回覆這段 target while mounted. A card
 * with no answer box (answered, expired) registers nothing, so its question
 * offers no 回覆這段. */
export function useReplyCardQuoteTarget(cardId: string, onQuote: QuoteIntoCard) {
  const targets = useContext(ReplyCardQuoteContext);
  const latest = useRef(onQuote);
  useLayoutEffect(() => {
    latest.current = onQuote;
  });
  useEffect(() => {
    if (!targets) return;
    const target: QuoteIntoCard = (excerpt) => latest.current(excerpt);
    targets.set(cardId, target);
    return () => {
      if (targets.get(cardId) === target) targets.delete(cardId);
    };
  }, [targets, cardId]);
}

interface Picked {
  excerpt: string;
  /** The selection as the browser reads it out, for 複製. */
  text: string;
  rect: DOMRect;
  /** Null where nothing can be answered: a card that is no longer waiting. */
  reply: (() => void) | null;
}

function bodyOf(node: Node | null, root: HTMLElement): HTMLElement | null {
  const el = node instanceof Element ? node : (node?.parentElement ?? null);
  const body = el?.closest<HTMLElement>(BODY_SELECTOR) ?? null;
  return body && root.contains(body) ? body : null;
}

/** The current selection, when it lies inside ONE message body of `root`. */
function pickSelection(
  root: HTMLElement | null,
  cardTargets: Map<string, QuoteIntoCard> | null,
  onReply: ((msgId: string, excerpt: string) => void) | undefined,
): Picked | null {
  const sel = window.getSelection();
  if (!root || !sel || sel.isCollapsed || sel.rangeCount === 0) return null;
  const range = sel.getRangeAt(0);
  const startBody = bodyOf(range.startContainer, root);
  const endBody = bodyOf(range.endContainer, root);
  if (startBody && endBody && startBody !== endBody) return null;
  const body = startBody ?? endBody;
  if (!body) return null;
  if (
    Array.from(root.querySelectorAll<HTMLElement>(BODY_SELECTOR)).some(
      (candidate) => candidate !== body && range.intersectsNode(candidate),
    )
  ) {
    return null;
  }
  const inside = rangeInside(range, body);
  if (!inside) return null;
  if (
    Array.from(body.querySelectorAll(INTERACTIVE_SELECTOR)).some(
      (element) =>
        !element.matches(INLINE_CONTENT_SELECTOR) && range.intersectsNode(element),
    )
  ) {
    return null;
  }
  const text = sel.toString();
  const excerpt = excerptFromSelection(text);
  if (!excerpt) return null;
  const rect = inside.getBoundingClientRect();
  // A card's question is answered on the card itself (owner rc-cf02eac19977),
  // never by a chat message to the row that carries it.
  if (body.matches(CARD_BODY_SELECTOR)) {
    const cardId = body
      .closest("[data-reply-card-id]")
      ?.getAttribute("data-reply-card-id");
    const quote = cardId ? cardTargets?.get(cardId) : undefined;
    return { excerpt, text, rect, reply: quote ? () => quote(excerpt) : null };
  }
  const msgId = body.closest("[data-msg-id]")?.getAttribute("data-msg-id");
  if (!msgId || !onReply) return null;
  return { excerpt, text, rect, reply: () => onReply(msgId, excerpt) };
}

// What a message says, as opposed to its sender line, the message it quotes,
// its toolbar or its answer box.
const ROW_CONTENT_SELECTOR = `.chat__msg-text, ${CARD_BODY_SELECTOR}`;

/** Whether `range` takes in words of `row` that are not its content. Words
 * that cannot be selected anyway (not rendered, hidden, or user-select: none)
 * do not count. */
function reachesPastContent(range: Range, row: HTMLElement): boolean {
  const walker = document.createTreeWalker(row, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = node as Text;
    if (!range.intersectsNode(text)) continue;
    const from = text === range.startContainer ? range.startOffset : 0;
    const to = text === range.endContainer ? range.endOffset : text.length;
    if (!text.data.slice(from, to).trim()) continue;
    const el = text.parentElement;
    if (!el || el.closest(ROW_CONTENT_SELECTOR) || el.getClientRects().length === 0) continue;
    const style = getComputedStyle(el);
    if (style.visibility === "hidden") continue;
    if ((style.userSelect || style.webkitUserSelect) === "none") continue;
    return true;
  }
  return false;
}

/** The selection-mode message (`.chat__msg--selecting`): whatever is selected
 * in its content (a card's title and question) is what 複製 copies and 回覆
 * quotes. A selection that runs into anything else offers neither. */
function pickInSelectedRow(
  root: HTMLElement | null,
  cardTargets: Map<string, QuoteIntoCard> | null,
  onReply: ((msgId: string, excerpt: string) => void) | undefined,
): Picked | null {
  const sel = window.getSelection();
  if (!root || !sel || sel.isCollapsed || sel.rangeCount === 0) return null;
  const row = root.querySelector<HTMLElement>(".chat__msg--selecting");
  const range = sel.getRangeAt(0);
  if (!row || !row.contains(range.startContainer) || !row.contains(range.endContainer)) {
    return null;
  }
  const excerpt = excerptFromSelection(sel.toString());
  if (!excerpt || reachesPastContent(range, row)) return null;
  const text = excerpt;
  const rect = range.getBoundingClientRect();
  const cardId = row
    .querySelector("[data-reply-card-id]")
    ?.getAttribute("data-reply-card-id");
  if (cardId) {
    const quote = cardTargets?.get(cardId);
    return { excerpt, text, rect, reply: quote ? () => quote(excerpt) : null };
  }
  const msgId = row.getAttribute("data-msg-id");
  return {
    excerpt,
    text,
    rect,
    reply: msgId && onReply ? () => onReply(msgId, excerpt) : null,
  };
}

/** The live selection inside `root`, re-read on every change. `onPicked` sees
 * each new pick before it is committed. */
function useSelectionPick(
  root: RefObject<HTMLElement | null>,
  onReply: ((msgId: string, excerpt: string) => void) | undefined,
  onPicked?: (picked: Picked) => void,
  pick: typeof pickSelection = pickSelection,
) {
  const cardTargets = useContext(ReplyCardQuoteContext);
  const [picked, setPicked] = useState<Picked | null>(null);
  const latest = useRef({ onReply, onPicked });
  useLayoutEffect(() => {
    latest.current = { onReply, onPicked };
  });

  const refresh = useCallback(() => {
    const next = pick(root.current, cardTargets, latest.current.onReply);
    if (next) latest.current.onPicked?.(next);
    setPicked(next);
  }, [root, cardTargets, pick]);

  useEffect(() => {
    refresh();
    document.addEventListener("selectionchange", refresh);
    window.addEventListener("scroll", refresh, true);
    window.addEventListener("resize", refresh);
    return () => {
      document.removeEventListener("selectionchange", refresh);
      window.removeEventListener("scroll", refresh, true);
      window.removeEventListener("resize", refresh);
    };
  }, [refresh]);

  return [picked, setPicked] as const;
}

// Pressing a button would otherwise collapse the selection it acts on before
// the click lands.
const keepSelection = {
  onPointerDown: (e: { preventDefault: () => void }) => e.preventDefault(),
  onMouseDown: (e: { preventDefault: () => void }) => e.preventDefault(),
};

/** The floating 「回覆這段」 button for a text selection in a chat bubble body
 * (`onReply`: a chat reply to that message) or in a waiting reply card's
 * question (into that card's own answer box). Portaled to <body> with fixed
 * coordinates so it never takes space in the thread. */
export function ChatSelectionReply({
  root,
  onReply,
}: {
  root: RefObject<HTMLElement | null>;
  onReply?: (msgId: string, excerpt: string) => void;
}) {
  const { t } = useI18n();
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  // A selection made with a finger gets the button docked at the bottom of the
  // screen: next to the selection is where iOS and Android put their own
  // Copy／Look Up bar, which would cover it.
  const [docked, setDocked] = useState(false);
  const lastPointer = useRef<string | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const [picked, setPicked] = useSelectionPick(root, onReply, () =>
    setDocked(
      lastPointer.current
        ? lastPointer.current === "touch"
        : typeof window.matchMedia === "function" &&
            window.matchMedia("(pointer: coarse)").matches,
    ),
  );

  useEffect(() => {
    const onDown = (e: PointerEvent) => {
      lastPointer.current = e.pointerType;
    };
    document.addEventListener("pointerdown", onDown, true);
    return () => document.removeEventListener("pointerdown", onDown, true);
  }, []);

  useLayoutEffect(() => {
    const btn = buttonRef.current;
    if (!picked || !btn || docked) {
      setPos(null);
      return;
    }
    setPos(placeUnderSelection(picked.rect, btn.getBoundingClientRect()));
  }, [picked, docked]);

  if (!picked?.reply) return null;
  const reply = picked.reply;
  const tooLong = replyExcerptTooLong(picked.excerpt);
  const label = tooLong
    ? t.chat.replyExcerptTooLong(REPLY_EXCERPT_MAX_CHARS)
    : t.chat.replyExcerptAction;
  return createPortal(
    <button
      ref={buttonRef}
      type="button"
      className={"chat__selection-reply" + (docked ? " chat__selection-reply--docked" : "")}
      data-testid="chat-selection-reply"
      disabled={tooLong}
      style={
        docked
          ? undefined
          : pos
            ? { left: pos.left, top: pos.top }
            : { left: 0, top: 0, visibility: "hidden" }
      }
      {...keepSelection}
      onClick={() => {
        reply();
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

/** Scrolls `thread` just enough that `row` ends above `edge` (a viewport y),
 * keeping its top in view; a row already clear of it does not move, nor does
 * one whose top is already above the thread: the reader is inside it, and a
 * resize must not take them back to its top. */
export function revealRowAbove(row: HTMLElement, thread: HTMLElement, edge: number) {
  const r = row.getBoundingClientRect();
  const under = r.bottom + ROW_CLEARANCE_PX - edge;
  const above = r.top - thread.getBoundingClientRect().top;
  if (under > 0 && above > 0) thread.scrollTop += Math.min(under, above);
}

/** The selection the mode starts with: the whole message, put there by
 * ChatArea and left where entering the mode placed it. */
function isWholeMessage(range: Range, row: HTMLElement): boolean {
  const text = row.querySelector(".chat__msg-text");
  return (
    !!text &&
    range.startContainer === text &&
    range.endContainer === text &&
    range.startOffset === 0 &&
    range.endOffset === text.childNodes.length
  );
}

function endRectOf(range: Range): DOMRect | null {
  const end = range.cloneRange();
  end.collapse(false);
  const caret = end.getBoundingClientRect();
  if (caret.height > 0) return caret;
  const rects = range.getClientRects();
  for (let i = rects.length - 1; i >= 0; i--) if (rects[i].height > 0) return rects[i];
  return null;
}

/** The bar of a message's selection mode (選取文字 in the long-press sheet):
 * 取消・複製・回覆 at the bottom of the chat, in the composer's place (the
 * composer is hidden for the mode). The thread ends at the bar's top edge,
 * the message is brought up clear of the bar if it would sit under it, and a
 * selection ending near the bar is scrolled clear of it. 複製 takes any
 * selection in the message and shows its result on itself for a moment (a
 * check, when it worked); 回覆 the same selection where the message can be
 * answered (not a card that is no longer waiting), and ends the mode. Focus
 * starts on the bar; Esc cancels. */
export function ChatSelectionModeBar({
  root,
  onReply,
  onCopy,
  copyState,
  onCancel,
  onReplied,
}: {
  root: RefObject<HTMLElement | null>;
  onReply: (msgId: string, excerpt: string) => void;
  onCopy: (text: string) => void;
  /** The last 複製's result. */
  copyState: CopyState;
  onCancel: () => void;
  /** After 回覆這段 has handed the excerpt on (the mode ends). */
  onReplied: () => void;
}) {
  const { t } = useI18n();
  const ref = useRef<HTMLDivElement>(null);
  const [picked] = useSelectionPick(root, onReply, undefined, pickInSelectedRow);
  const copyResult = useCopyLabel(copyState);
  // Read while rendering, before the commit hides the composer: by the first
  // layout effect the thread has grown and the browser has already clamped
  // its scrollTop, which would drop every row by the composer's height.
  const [scrollTopBefore] = useState(() => root.current?.scrollTop ?? 0);
  useEscapeLayer(onCancel, ref);
  useEffect(() => {
    ref.current?.focus({ preventScroll: true });
  }, []);
  useLayoutEffect(() => {
    const bar = ref.current;
    const thread = root.current;
    const row = thread?.querySelector<HTMLElement>(".chat__msg--selecting");
    const pane = thread?.closest<HTMLElement>(".chat__body");
    if (!bar || !thread || !row || !pane) return;
    const reserve = () => {
      const barTop = bar.getBoundingClientRect().top;
      const covered = Math.max(0, pane.getBoundingClientRect().bottom - barTop);
      pane.style.setProperty(BAR_RESERVE_VAR, `${covered}px`);
      return barTop;
    };
    const fit = () => revealRowAbove(row, thread, reserve());
    reserve();
    thread.scrollTop = scrollTopBefore;
    fit();
    // A card opens up only after the mode has begun, so the row is watched
    // as well as the bar.
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(fit);
    ro?.observe(bar);
    ro?.observe(row);
    ro?.observe(thread);
    return () => {
      ro?.disconnect();
      pane.style.removeProperty(BAR_RESERVE_VAR);
    };
  }, [root]);
  useEffect(() => {
    const bar = ref.current;
    const thread = root.current;
    if (!bar || !thread) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const keepEndClear = () => {
      const row = thread.querySelector<HTMLElement>(".chat__msg--selecting");
      const selection = window.getSelection();
      if (!row || !selection || selection.rangeCount === 0 || selection.isCollapsed) return;
      const range = selection.getRangeAt(0);
      if (!row.contains(range.endContainer) || isWholeMessage(range, row)) return;
      const end = endRectOf(range);
      if (!end) return;
      const under = end.bottom - (bar.getBoundingClientRect().top - SELECTION_END_CLEARANCE_PX);
      if (under > 0) thread.scrollTop += under;
    };
    // Once the selection has settled, not on every step of a handle's drag:
    // following each step would run the thread away under the finger.
    const onSelectionChange = () => {
      clearTimeout(timer);
      timer = setTimeout(keepEndClear, SELECTION_SETTLE_MS);
    };
    document.addEventListener("selectionchange", onSelectionChange);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("selectionchange", onSelectionChange);
    };
  }, [root]);
  const reply = picked?.reply ?? null;
  const tooLong = picked !== null && replyExcerptTooLong(picked.excerpt);
  return (
    <div
      ref={ref}
      className="chat__select-bar"
      role="group"
      aria-label={t.chat.selectionMode}
      tabIndex={-1}
      data-testid="chat-select-bar"
    >
      <span className="chat__sr-text" role="status">
        {copyState === "idle" ? "" : copyResult}
      </span>
      <button
        type="button"
        className="chat__select-bar__btn"
        data-testid="chat-select-cancel"
        {...keepSelection}
        onClick={onCancel}
      >
        {t.chat.selectionCancel}
      </button>
      <button
        type="button"
        className="chat__select-bar__btn"
        data-testid="chat-select-copy"
        disabled={!picked}
        {...keepSelection}
        onClick={() => picked && onCopy(picked.text)}
      >
        {copyState === "copied" ? (
          <CopiedMark label={copyResult} size={18} />
        ) : (
          <>
            <CopyIcon size={14} />
            <span>{copyState === "failed" ? copyResult : t.chat.copySelection}</span>
          </>
        )}
      </button>
      <button
        type="button"
        className="chat__select-bar__btn chat__select-bar__btn--primary"
        data-testid="chat-select-reply"
        disabled={!reply || tooLong}
        {...keepSelection}
        onClick={() => {
          if (!reply) return;
          // A card's answer box is hidden during the mode and cannot take the
          // focus its quote hands it until the mode has ended.
          flushSync(onReplied);
          reply();
        }}
      >
        {!tooLong && <ReplyIcon size={14} aria-hidden="true" />}
        <span>
          {tooLong
            ? t.chat.replyExcerptTooLong(REPLY_EXCERPT_MAX_CHARS)
            : t.chat.selectionReply}
        </span>
      </button>
    </div>
  );
}
