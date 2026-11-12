import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api";

export interface UseChatSuggestedReplies {
  /** member id → that member's sentences. A member with none is absent. */
  lists: Readonly<Record<string, readonly string[]>>;
  /** A read has answered. Until then an empty list means "not known", and
   * editing from it could wipe what the server holds. */
  loaded: boolean;
  /** No read has answered yet and the latest one failed, so the lists cannot be
   * edited. `reload` or a delta reads again; a read that lands clears it. */
  loadFailed: boolean;
  /** The members whose latest write failed. That member's list went back to what
   * the server last acknowledged; their next write or a dismissal clears them. */
  saveFailedFor: ReadonlySet<string>;
  dismissSaveFailed: (memberId: string) => void;
  /** Show the list at once and save it. */
  save: (memberId: string, replies: string[]) => void;
  reload: () => void;
}

type Lists = Readonly<Record<string, readonly string[]>>;

function withMember(lists: Lists, memberId: string, replies: readonly string[]): Lists {
  const { [memberId]: _replaced, ...rest } = lists;
  return replies.length > 0 ? { ...rest, [memberId]: replies } : rest;
}

/** The owner's chat 建議回覆 (GET /api/chat/suggested-replies), re-read on every
 * `chat_suggested_reply` delta. */
export function useChatSuggestedReplies(): UseChatSuggestedReplies {
  const [lists, setLists] = useState<Lists>({});
  const [loaded, setLoaded] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const [saveFailedFor, setSaveFailedFor] = useState<ReadonlySet<string>>(new Set());
  const loadedRef = useRef(false);
  const aliveRef = useRef(true);
  // What the server last answered. A failed save falls back to this, because the
  // re-read that follows can fail too and the failure message says the screen
  // shows the server's content.
  const ackedRef = useRef<Lists>({});
  // One PUT at a time, so two quick edits cannot land in the wrong order. Edits
  // made meanwhile wait, keeping only the newest per member. A delta during a
  // PUT is read after it settles: a read sent now could not see that PUT.
  const inFlightRef = useRef(false);
  const queuedRef = useRef(new Map<string, string[]>());
  const rereadRef = useRef(false);
  // One counter orders reads and writes: a read answer lands only if it is the
  // newest read and no PUT was sent after it.
  const opRef = useRef(0);
  const lastWriteRef = useRef(0);
  const latestReadRef = useRef(0);
  const loadRef = useRef<() => void>(() => {});

  useEffect(() => {
    aliveRef.current = true;
    const load = () => {
      if (inFlightRef.current) {
        rereadRef.current = true;
        return;
      }
      const op = ++opRef.current;
      latestReadRef.current = op;
      api.getChatSuggestedReplies().then(
        (next) => {
          if (!aliveRef.current || op !== latestReadRef.current || op < lastWriteRef.current) return;
          ackedRef.current = next;
          loadedRef.current = true;
          setLists(next);
          setLoaded(true);
          setLoadFailed(false);
        },
        (e) => {
          console.warn("useChatSuggestedReplies: load failed", e);
          if (aliveRef.current && !loadedRef.current) setLoadFailed(true);
        },
      );
    };
    loadRef.current = load;
    load();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "chat_suggested_reply") load();
    });
    return () => {
      aliveRef.current = false;
      unsubscribe();
    };
  }, []);

  const clearSaveFailed = useCallback((memberId: string) => {
    setSaveFailedFor((cur) => {
      if (!cur.has(memberId)) return cur;
      const next = new Set(cur);
      next.delete(memberId);
      return next;
    });
  }, []);

  const sendNext = useCallback(() => {
    const entry = queuedRef.current.entries().next();
    if (entry.done) {
      inFlightRef.current = false;
      if (rereadRef.current) {
        rereadRef.current = false;
        loadRef.current();
      }
      return;
    }
    const [memberId, replies] = entry.value;
    queuedRef.current.delete(memberId);
    inFlightRef.current = true;
    lastWriteRef.current = ++opRef.current;
    // An edit queued behind a PUT that failed goes out as that member's next save.
    clearSaveFailed(memberId);
    api.putChatSuggestedReplies(memberId, replies).then(
      (stored) => {
        ackedRef.current = withMember(ackedRef.current, memberId, stored);
        if (aliveRef.current) sendNext();
      },
      (e) => {
        console.warn("useChatSuggestedReplies: save failed", e);
        if (!aliveRef.current) return;
        setSaveFailedFor((cur) => new Set(cur).add(memberId));
        // A newer edit for this member is still on its way and decides the screen.
        if (!queuedRef.current.has(memberId)) {
          const acked = ackedRef.current[memberId] ?? [];
          setLists((cur) => withMember(cur, memberId, acked));
        }
        // What reached the server is unknown: ask it what it holds now.
        rereadRef.current = true;
        sendNext();
      },
    );
  }, [clearSaveFailed]);

  const save = useCallback(
    (memberId: string, replies: string[]) => {
      setLists((cur) => ({ ...cur, [memberId]: replies }));
      clearSaveFailed(memberId);
      queuedRef.current.set(memberId, replies);
      if (!inFlightRef.current) sendNext();
    },
    [sendNext, clearSaveFailed],
  );

  const reload = useCallback(() => loadRef.current(), []);

  return { lists, loaded, loadFailed, saveFailedFor, dismissSaveFailed: clearSaveFailed, save, reload };
}
