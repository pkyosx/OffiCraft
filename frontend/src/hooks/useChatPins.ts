import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api";
import { pinAt, togglePin } from "../lib/chatPins";

interface UseChatPins {
  /** The pin list, staff and outsource ids in ONE order; [] until loaded. */
  pins: readonly string[];
  /** The last write was refused or never answered; the list is back at the
   * server's version. Cleared by the next write. */
  saveFailed: boolean;
  toggle: (id: string, kindPinnedIds: readonly string[]) => void;
  reorder: (id: string, index: number, kindPinnedIds: readonly string[]) => void;
}

/**
 * The owner's chat-list pins (GET / PUT /api/chat/pins), refetched on every
 * `chat_pin` delta — another browser's write, our own echo, or a resync.
 *
 * `knownIds` is every member the sidebar can currently show, or null while
 * that is not known. A write sends only known ids: the server drops a removed
 * member from its list without telling us, and a stale id in a PUT is a 422.
 */
export function useChatPins(knownIds: ReadonlySet<string> | null): UseChatPins {
  const [pins, setPins] = useState<readonly string[]>([]);
  const [saveFailed, setSaveFailed] = useState(false);
  const pinsRef = useRef<readonly string[]>([]);
  const serverRef = useRef<readonly string[]>([]);
  const loadedRef = useRef(false);
  const knownRef = useRef(knownIds);
  knownRef.current = knownIds;
  // Only the most recently SENT request may land: an older GET finishing after
  // a newer write would otherwise put the pre-write list back on screen.
  const seqRef = useRef(0);
  const aliveRef = useRef(true);

  const show = (list: readonly string[]) => {
    pinsRef.current = list;
    setPins(list);
  };
  const adopt = (seq: number, list: string[]) => {
    if (!aliveRef.current || seq !== seqRef.current) return;
    loadedRef.current = true;
    serverRef.current = list;
    show(list);
  };

  useEffect(() => {
    aliveRef.current = true;
    const load = () => {
      const seq = ++seqRef.current;
      api
        .getChatPins()
        .then((list) => adopt(seq, list))
        .catch((e) => console.warn("useChatPins: load failed", e));
    };
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
    // Before the first load there is no list to change, and writing one would
    // replace the server's with a guess.
    if (!loadedRef.current) return;
    const known = knownRef.current;
    const current = known ? pinsRef.current.filter((id) => known.has(id)) : pinsRef.current;
    const next = change(current);
    const seq = ++seqRef.current;
    show(next);
    setSaveFailed(false);
    api
      .putChatPins(next)
      .then((list) => adopt(seq, list))
      .catch((e) => {
        console.warn("useChatPins: save failed", e);
        if (!aliveRef.current || seq !== seqRef.current) return;
        show(serverRef.current);
        setSaveFailed(true);
      });
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

  return { pins, saveFailed, toggle, reorder };
}
