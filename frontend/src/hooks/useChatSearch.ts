import { useRef, useState } from "react";
import { api } from "../api";
import type { ChatMessage } from "../api/adapter";

export type ChatSearchStatus = "idle" | "loading" | "failed" | "done";

export interface ChatSearch {
  draft: string;
  setDraft: (next: string) => void;
  /** The trimmed query the results on screen answer. */
  query: string;
  status: ChatSearchStatus;
  /** Newest first. */
  results: ChatMessage[];
  hasOlder: boolean;
  olderStatus: "idle" | "loading" | "failed";
  submit: () => void;
  loadOlder: () => void;
}

/** One conversation's keyword search. Lives in ChatArea rather than the panel
 * so closing and reopening the panel keeps the query and its results. */
export function useChatSearch(withId: string): ChatSearch {
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<ChatSearchStatus>("idle");
  const [results, setResults] = useState<ChatMessage[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [olderStatus, setOlderStatus] = useState<"idle" | "loading" | "failed">(
    "idle",
  );
  // ⚠️ Only the latest search may land: an earlier, slower answer would
  // otherwise replace the newer query's results under the newer query's words.
  const searchSeq = useRef(0);
  const olderInFlight = useRef(false);

  const submit = () => {
    const q = draft.trim();
    if (q === "") return;
    const seq = ++searchSeq.current;
    olderInFlight.current = false;
    setQuery(q);
    setStatus("loading");
    setResults([]);
    setNextCursor(undefined);
    setOlderStatus("idle");
    api.searchChat(withId, q).then(
      (page) => {
        if (seq !== searchSeq.current) return;
        setResults([...page.messages].reverse());
        setNextCursor(page.nextCursor);
        setStatus("done");
      },
      (e) => {
        if (seq !== searchSeq.current) return;
        console.warn("useChatSearch: search failed", e);
        setStatus("failed");
      },
    );
  };

  const loadOlder = () => {
    if (nextCursor === undefined || olderInFlight.current) return;
    const seq = searchSeq.current;
    olderInFlight.current = true;
    setOlderStatus("loading");
    api.searchChat(withId, query, nextCursor).then(
      (page) => {
        if (seq !== searchSeq.current) return;
        olderInFlight.current = false;
        setResults((cur) => [...cur, ...[...page.messages].reverse()]);
        setNextCursor(page.nextCursor);
        setOlderStatus("idle");
      },
      (e) => {
        if (seq !== searchSeq.current) return;
        olderInFlight.current = false;
        console.warn("useChatSearch: older page failed", e);
        setOlderStatus("failed");
      },
    );
  };

  return {
    draft,
    setDraft,
    query,
    status,
    results,
    hasOlder: nextCursor !== undefined,
    olderStatus,
    submit,
    loadOlder,
  };
}
