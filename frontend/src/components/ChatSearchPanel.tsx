import { useRef } from "react";
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

/** The search results list, opened from the search row's speech bubble. */
export function ChatSearchPanel({
  search,
  labelOf,
  onPick,
  onClose,
}: {
  search: ChatSearch;
  labelOf: (m: ChatMessage) => string;
  onPick: (msgId: string) => void;
  onClose: () => void;
}) {
  const { t, msg } = useI18n();
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);
  const now = Date.now() / 1000;

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
      {search.status === "loading" ? (
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
      ) : null}
    </div>
  );
}
