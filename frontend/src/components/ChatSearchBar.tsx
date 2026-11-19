import { useRef } from "react";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { ChatSearch } from "../hooks/useChatSearch";
import {
  ChevronDownIcon,
  ChevronUpIcon,
  CloseIcon,
  MessageDotsIcon,
  SearchIcon,
} from "./icons";
import "./chat-search.css";

export function ChatSearchBar({
  search,
  listOpen,
  onToggleList,
  onExit,
}: {
  search: ChatSearch;
  listOpen: boolean;
  onToggleList: () => void;
  onExit: () => void;
}) {
  const { t, msg } = useI18n();
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onExit, rootRef);

  const status =
    search.status === "loading"
      ? t.chat.searchLoading
      : search.status === "failed"
        ? t.chat.searchFailed
        : search.status === "done" && search.results.length === 0
          ? msg.chatSearchEmpty(search.query)
          : search.status === "done" && search.olderStatus === "failed"
            ? t.chat.searchFailed
            : search.status === "done"
              ? msg.chatSearchCount(search.position, search.matchCount)
              : "";

  return (
    <div ref={rootRef} className="chat-search__bar" role="search">
      <div className="chat-search__box">
        <SearchIcon size={16} className="chat-search__box-icon" />
        <input
          className="chat-search__input"
          type="text"
          value={search.draft}
          placeholder={t.chat.searchPlaceholder}
          aria-label={t.chat.searchPlaceholder}
          autoFocus
          onChange={(e) => search.setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== "Enter" || e.nativeEvent.isComposing) return;
            e.preventDefault();
            search.submit();
          }}
        />
        {status !== "" && (
          <span
            className={
              "chat-search__status" +
              (search.status === "failed" || search.olderStatus === "failed"
                ? " chat-search__status--error"
                : "")
            }
            role="status"
            title={status}
          >
            {status}
          </span>
        )}
        {search.current && (
          <>
            <button
              type="button"
              className="chat-search__step"
              aria-label={t.chat.searchPrev}
              title={t.chat.searchPrev}
              disabled={!search.canOlder}
              onClick={search.older}
            >
              <ChevronUpIcon size={16} />
            </button>
            <button
              type="button"
              className="chat-search__step"
              aria-label={t.chat.searchNext}
              title={t.chat.searchNext}
              disabled={!search.canNewer}
              onClick={search.newer}
            >
              <ChevronDownIcon size={16} />
            </button>
          </>
        )}
      </div>
      <button
        type="button"
        className="chat-search__action"
        aria-label={t.chat.searchList}
        title={t.chat.searchList}
        aria-expanded={listOpen}
        onClick={onToggleList}
      >
        <MessageDotsIcon size={18} />
      </button>
      <button
        type="button"
        className="chat-search__action"
        aria-label={t.chat.searchExit}
        title={t.chat.searchExit}
        onClick={onExit}
      >
        <CloseIcon size={18} />
      </button>
    </div>
  );
}
