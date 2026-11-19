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
  matchCount: number;
  /** The current match, or null before a search lands or when nothing matched. */
  current: ChatMessage | null;
  /** 1-based, counted from the OLDEST match; 0 when there is no current match. */
  position: number;
  hasOlder: boolean;
  canOlder: boolean;
  canNewer: boolean;
  olderStatus: "idle" | "loading" | "failed";
  submit: () => void;
  /** Step to the next older match, fetching the next page first when needed. */
  older: () => void;
  newer: () => void;
  /** Make this listed result the current match. */
  pick: (msgId: string) => void;
  loadOlder: () => void;
  reset: () => void;
}

/** One conversation's keyword search. Every change of the current match is
 * handed to `onJump`, which moves the thread there. */
export function useChatSearch(
  withId: string,
  onJump: (msgId: string) => void,
): ChatSearch {
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<ChatSearchStatus>("idle");
  const [results, setResults] = useState<ChatMessage[]>([]);
  const [matchCount, setMatchCount] = useState(0);
  const [index, setIndex] = useState(-1);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);
  const [olderStatus, setOlderStatus] = useState<"idle" | "loading" | "failed">(
    "idle",
  );
  const jumpRef = useRef(onJump);
  jumpRef.current = onJump;
  const indexRef = useRef(index);
  indexRef.current = index;
  // ⚠️ Only the latest search may land: an earlier, slower answer would
  // otherwise replace the newer query's results under the newer query's words.
  const searchSeq = useRef(0);
  const olderInFlight = useRef(false);

  const go = (list: ChatMessage[], i: number) => {
    setIndex(i);
    setOlderStatus((cur) => (cur === "failed" ? "idle" : cur));
    jumpRef.current(list[i].id);
  };

  const reset = () => {
    searchSeq.current += 1;
    olderInFlight.current = false;
    setDraft("");
    setQuery("");
    setStatus("idle");
    setResults([]);
    setMatchCount(0);
    setIndex(-1);
    setNextCursor(undefined);
    setOlderStatus("idle");
  };

  const submit = () => {
    const q = draft.trim();
    if (q === "") return;
    const seq = ++searchSeq.current;
    olderInFlight.current = false;
    setQuery(q);
    setStatus("loading");
    setResults([]);
    setMatchCount(0);
    setIndex(-1);
    setNextCursor(undefined);
    setOlderStatus("idle");
    api.searchChat(withId, q).then(
      (page) => {
        if (seq !== searchSeq.current) return;
        const list = [...page.messages].reverse();
        setResults(list);
        setMatchCount(page.matchCount);
        setNextCursor(page.nextCursor);
        setStatus("done");
        if (list.length > 0) go(list, 0);
      },
      (e) => {
        if (seq !== searchSeq.current) return;
        console.warn("useChatSearch: search failed", e);
        setStatus("failed");
      },
    );
  };

  /** Fetches the next older page; `then` gets the grown list. */
  const fetchOlder = (then?: (list: ChatMessage[]) => void) => {
    if (nextCursor === undefined || olderInFlight.current) return;
    const seq = searchSeq.current;
    olderInFlight.current = true;
    setOlderStatus("loading");
    api.searchChat(withId, query, nextCursor).then(
      (page) => {
        if (seq !== searchSeq.current) return;
        olderInFlight.current = false;
        const list = [...results, ...[...page.messages].reverse()];
        // matchCount stays the first page's: positions are counted from the
        // oldest match, and a match that arrived since is not in `results`.
        setResults(list);
        setNextCursor(page.nextCursor);
        setOlderStatus("idle");
        then?.(list);
      },
      (e) => {
        if (seq !== searchSeq.current) return;
        olderInFlight.current = false;
        console.warn("useChatSearch: older page failed", e);
        setOlderStatus("failed");
      },
    );
  };

  const paging = olderStatus === "loading";
  const canOlder =
    !paging && index >= 0 && (index + 1 < results.length || nextCursor !== undefined);
  const canNewer = !paging && index > 0;

  const older = () => {
    if (!canOlder) return;
    if (index + 1 < results.length) go(results, index + 1);
    else {
      const from = index;
      fetchOlder((list) => {
        if (indexRef.current === from && from + 1 < list.length) go(list, from + 1);
      });
    }
  };

  const newer = () => {
    if (canNewer) go(results, index - 1);
  };

  const pick = (msgId: string) => {
    const i = results.findIndex((m) => m.id === msgId);
    if (i >= 0) go(results, i);
  };

  const current = index >= 0 ? results[index] ?? null : null;

  return {
    draft,
    setDraft,
    query,
    status,
    results,
    matchCount,
    current,
    position: current ? matchCount - index : 0,
    hasOlder: nextCursor !== undefined,
    canOlder,
    canNewer,
    olderStatus,
    submit,
    older,
    newer,
    pick,
    loadOlder: () => fetchOlder(),
    reset,
  };
}
