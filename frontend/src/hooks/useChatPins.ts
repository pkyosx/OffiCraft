import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api";
import { pinAt, togglePin } from "../lib/chatPins";

export type ChatPinsError = "load" | "save";

interface UseChatPins {
  /** The pin list, staff and outsource ids in ONE order; [] until loaded. */
  pins: readonly string[];
  /** The first read has finished, answered or not. */
  settled: boolean;
  /** "load": the list could not be read, so pins cannot be changed yet (the
   * next toggle reads again). "save": the latest write failed and the list was
   * re-read from the server. A read the hook adopts clears "load"; "save"
   * stays until the next write starts. */
  error: ChatPinsError | null;
  toggle: (id: string, kindPinnedIds: readonly string[]) => void;
  reorder: (id: string, index: number, kindPinnedIds: readonly string[]) => void;
}

/**
 * The owner's chat-list pins (GET / PUT /api/chat/pins), re-read on every
 * `chat_pin` delta — another browser's write, our own echo, or a resync.
 *
 * `knownIds` is every member the sidebar can currently show, or null while
 * that is not known. A write sends only known ids: the server drops a removed
 * member from its list without telling us, and a stale id in a PUT is a 422.
 */
export function useChatPins(knownIds: ReadonlySet<string> | null): UseChatPins {
  const [pins, setPins] = useState<readonly string[]>([]);
  const [settled, setSettled] = useState(false);
  const [error, setError] = useState<ChatPinsError | null>(null);
  const pinsRef = useRef<readonly string[]>([]);
  const loadedRef = useRef(false);
  const knownRef = useRef(knownIds);
  knownRef.current = knownIds;
  const aliveRef = useRef(true);
  // One counter orders reads and writes. A write answer lands only if it is
  // the latest write. A read answer lands only if, when the read was SENT, no
  // write was in flight (it could not see that write), no write has been sent
  // since, and it is the newest read. Dropping the others loses nothing: every
  // write is followed by a read (its own chat_pin echo, or the re-read after a
  // failure).
  const opRef = useRef(0);
  const lastWriteRef = useRef(0);
  const lastWriteSettledRef = useRef(true);
  const latestReadRef = useRef(0);
  const loadRef = useRef<() => void>(() => {});

  const show = (list: readonly string[]) => {
    pinsRef.current = list;
    setPins(list);
  };

  useEffect(() => {
    aliveRef.current = true;
    const load = () => {
      const op = ++opRef.current;
      latestReadRef.current = op;
      const sentClean = lastWriteSettledRef.current;
      api
        .getChatPins()
        .then((list) => {
          if (!aliveRef.current) return;
          loadedRef.current = true;
          setSettled(true);
          if (!sentClean || op !== latestReadRef.current || op < lastWriteRef.current) return;
          show(list);
          setError((e) => (e === "load" ? null : e));
        })
        .catch((e) => {
          console.warn("useChatPins: load failed", e);
          if (!aliveRef.current) return;
          setSettled(true);
          if (!loadedRef.current) setError("load");
        });
    };
    loadRef.current = load;
    load();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "chat_pin") load();
    });
    return () => {
      aliveRef.current = false;
      unsubscribe();
    };
  }, []);

  const write = useCallback((change: (current: readonly string[]) => string[]) => {
    // Without a list from the server any write would be a guess; read again.
    if (!loadedRef.current) {
      loadRef.current();
      return;
    }
    const known = knownRef.current;
    const current = known ? pinsRef.current.filter((id) => known.has(id)) : pinsRef.current;
    const next = change(current);
    const op = ++opRef.current;
    lastWriteRef.current = op;
    lastWriteSettledRef.current = false;
    show(next);
    setError(null);
    api
      .putChatPins(next)
      .then(
        (list) => {
          if (!aliveRef.current || op !== lastWriteRef.current) return;
          lastWriteSettledRef.current = true;
          show(list);
        },
        (e) => {
          console.warn("useChatPins: save failed", e);
          if (!aliveRef.current || op !== lastWriteRef.current) return;
          lastWriteSettledRef.current = true;
          // An earlier write may have landed, so the last list we adopted can
          // be stale: ask the server what it holds now.
          setError("save");
          loadRef.current();
        },
      );
  }, []);

  const toggle = useCallback(
    (id: string, kindPinnedIds: readonly string[]) =>
      write((current) => togglePin(current, id, kindPinnedIds)),
    [write],
  );
  const reorder = useCallback(
    (id: string, index: number, kindPinnedIds: readonly string[]) =>
      write((current) => pinAt(current, id, index, kindPinnedIds)),
    [write],
  );

  return { pins, settled, error, toggle, reorder };
}
