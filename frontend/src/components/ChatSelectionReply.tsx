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

const CARD_BODY_SELECTOR = ".reply-card__summary, .reply-card__body";
const BODY_SELECTOR = `.chat__msg-text, ${CARD_BODY_SELECTOR}`;
const INTERACTIVE_SELECTOR =
  "button, a, input, textarea, select, [role='button'], [role='menu'], [role='dialog']";

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
  rect: DOMRect;
  reply: () => void;
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
    Array.from(body.querySelectorAll(INTERACTIVE_SELECTOR)).some((element) =>
      range.intersectsNode(element),
    )
  ) {
    return null;
  }
  const excerpt = normalizeReplyExcerpt(sel.toString());
  if (!excerpt) return null;
  const rect = inside.getBoundingClientRect();
  // A card's question is answered on the card itself (owner rc-cf02eac19977),
  // never by a chat message to the row that carries it.
  if (body.matches(CARD_BODY_SELECTOR)) {
    const cardId = body
      .closest("[data-reply-card-id]")
      ?.getAttribute("data-reply-card-id");
    const quote = cardId ? cardTargets?.get(cardId) : undefined;
    if (!quote) return null;
    return { excerpt, rect, reply: () => quote(excerpt) };
  }
  const msgId = body.closest("[data-msg-id]")?.getAttribute("data-msg-id");
  if (!msgId || !onReply) return null;
  return { excerpt, rect, reply: () => onReply(msgId, excerpt) };
}

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
  const cardTargets = useContext(ReplyCardQuoteContext);
  const [picked, setPicked] = useState<Picked | null>(null);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const onReplyRef = useRef(onReply);
  useLayoutEffect(() => {
    onReplyRef.current = onReply;
  });

  const refresh = useCallback(() => {
    setPicked(pickSelection(root.current, cardTargets, onReplyRef.current));
  }, [root, cardTargets]);

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
        picked.reply();
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
