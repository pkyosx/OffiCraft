import { useEffect, useRef, type KeyboardEvent, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { chatSearchSnippet } from "../lib/chatSearch";
import { formatAbsolute } from "../lib/dateFormat";
import type { ChatMessage } from "../api/adapter";
import type { ChatSearch } from "../hooks/useChatSearch";
import { CloseIcon } from "./icons";
import "./chat-search.css";

function Snippet({ body, query }: { body: string; query: string }) {
  const { cutStart, cutEnd, parts } = chatSearchSnippet(body, query);
  return (
    <span className="chat-search__snippet">
      {cutStart && "…"}
      {parts.map((p, i) =>
        p.match ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>,
      )}
      {cutEnd && "…"}
    </span>
  );
}

/** Tab and Shift+Tab go round the buttons inside `box`. */
function trapTab(e: KeyboardEvent<HTMLElement>, box: HTMLElement | null) {
  if (e.key !== "Tab" || !box) return;
  const buttons = Array.from(box.querySelectorAll<HTMLButtonElement>("button:not(:disabled)"));
  if (buttons.length === 0) return;
  const i = buttons.indexOf(document.activeElement as HTMLButtonElement);
  const next = e.shiftKey ? (i <= 0 ? buttons.length - 1 : i - 1) : (i + 1) % buttons.length;
  e.preventDefault();
  buttons[next].focus();
}

/** The search results list, opened from the search row's speech bubble: a
 * side panel over the thread, or a bottom sheet on a phone. */
export function ChatSearchPanel({
  search,
  labelOf,
  onPick,
  onClose,
  mobile = false,
  returnFocusTo,
}: {
  search: ChatSearch;
  labelOf: (m: ChatMessage) => string;
  onPick: (msgId: string) => void;
  onClose: () => void;
  mobile?: boolean;
  /** Where focus goes back to when the phone sheet closes. */
  returnFocusTo?: RefObject<HTMLElement | null>;
}) {
  const { t, msg } = useI18n();
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);

  useEffect(() => {
    if (!mobile) return;
    rootRef.current?.focus();
    const back = returnFocusTo?.current;
    return () => back?.focus({ preventScroll: true });
  }, [mobile, returnFocusTo]);
  const now = Date.now() / 1000;

  const results =
    search.status === "loading" ? (
      <div className="chat-search__state">{t.chat.searchLoading}</div>
    ) : search.status === "failed" ? (
      <div className="chat-search__state chat-search__state--error" role="alert">
        {t.chat.searchFailed}
      </div>
    ) : search.status === "done" && search.results.length === 0 ? (
      <div className="chat-search__state">{msg.chatSearchEmpty(search.query)}</div>
    ) : search.status === "done" ? (
      <div className="chat-search__list">
        <div className="chat-search__hint">{t.chat.searchOrderHint}</div>
        {search.results.map((m) => (
          <button
            type="button"
            key={m.id}
            className="chat-search__item"
            aria-current={m.id === search.current?.id ? "true" : undefined}
            onClick={() => onPick(m.id)}
          >
            <span className="chat-search__meta">
              <span className="chat-search__who">{labelOf(m)}</span>
              <span className="chat-search__time">{formatAbsolute(m.ts, now)}</span>
            </span>
            <Snippet body={m.body} query={search.query} />
          </button>
        ))}
        {search.olderStatus === "failed" && (
          <div className="chat-search__state chat-search__state--error" role="alert">
            {t.chat.searchFailed}
          </div>
        )}
        {search.hasOlder && (
          <button
            type="button"
            className="chat-search__older"
            disabled={search.olderStatus === "loading"}
            onClick={search.loadOlder}
          >
            {search.olderStatus === "loading"
              ? t.chat.searchLoading
              : t.chat.searchLoadOlder}
          </button>
        )}
      </div>
    ) : null;

  if (mobile) {
    return createPortal(
      <div
        className="chat-search__sheet-backdrop"
        data-testid="chat-search-sheet-backdrop"
        onClick={(e) => {
          if (e.target === e.currentTarget) onClose();
        }}
      >
        <div
          ref={rootRef}
          className="chat-search__sheet"
          role="dialog"
          aria-modal="true"
          aria-label={t.chat.searchResultsTitle}
          tabIndex={-1}
          onKeyDown={(e) => trapTab(e, rootRef.current)}
        >
          <div className="chat-search__sheet-head">
            <span className="chat-search__title">{t.chat.searchResultsTitle}</span>
            <button type="button" className="chat-search__done" onClick={onClose}>
              {t.office.groups.done}
            </button>
          </div>
          {results}
        </div>
      </div>,
      document.body,
    );
  }

  return (
    <div
      ref={rootRef}
      className="chat-search__panel"
      role="dialog"
      aria-label={t.chat.searchResultsTitle}
    >
      <div className="chat-search__header">
        <span className="chat-search__title">{t.chat.searchResultsTitle}</span>
        <button
          type="button"
          className="chat-search__close"
          aria-label={t.chat.searchClose}
          onClick={onClose}
        >
          <CloseIcon size={16} />
        </button>
      </div>
      {results}
    </div>
  );
}
