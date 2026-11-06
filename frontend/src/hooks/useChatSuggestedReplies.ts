import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api";

export interface UseChatSuggestedReplies {
  /** member id → that member's sentences. A member with none is absent. */
  lists: Readonly<Record<string, readonly string[]>>;
  /** A read has answered. Until then an empty list means "not known", and
   * editing from it could wipe what the server holds. */
  loaded: boolean;
  /** The latest write failed and the lists were re-read from the server; clears
   * when the next write starts. */
  saveFailed: boolean;
  /** Show the list at once and save it. */
  save: (memberId: string, replies: string[]) => void;
}

/** The owner's chat 建議回覆 (GET /api/chat/suggested-replies), re-read on every
 * `chat_suggested_reply` delta. */
export function useChatSuggestedReplies(): UseChatSuggestedReplies {
  const [lists, setLists] = useState<Readonly<Record<string, readonly string[]>>>({});
  const [loaded, setLoaded] = useState(false);
  const [saveFailed, setSaveFailed] = useState(false);
  const aliveRef = useRef(true);
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
          setLists(next);
          setLoaded(true);
        },
        (e) => {
          console.warn("useChatSuggestedReplies: load failed", e);
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
    api.putChatSuggestedReplies(memberId, replies).then(
      () => {
        if (aliveRef.current) sendNext();
      },
      (e) => {
        console.warn("useChatSuggestedReplies: save failed", e);
        if (!aliveRef.current) return;
        setSaveFailed(true);
        // What reached the server is unknown: ask it what it holds now.
        rereadRef.current = true;
        sendNext();
      },
    );
  }, []);

  const save = useCallback(
    (memberId: string, replies: string[]) => {
      setLists((cur) => ({ ...cur, [memberId]: replies }));
      setSaveFailed(false);
      queuedRef.current.set(memberId, replies);
      if (!inFlightRef.current) sendNext();
    },
    [sendNext],
  );

  return { lists, loaded, saveFailed, save };
}
