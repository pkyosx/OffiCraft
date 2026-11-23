import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";

export interface UseKeyedSuggestedReplies {
  /** key (a member id, a task type) → its sentences. A key with none is absent. */
  lists: Readonly<Record<string, readonly string[]>>;
  /** A read has answered. Until then an empty list means "not known", and
   * editing from it could wipe what the server holds. */
  loaded: boolean;
  /** No read has answered yet and the latest one failed, so the lists cannot be
   * edited. `reload` or a delta reads again; a read that lands clears it. */
  loadFailed: boolean;
  /** The keys whose latest write failed. That key's list went back to what the
   * server last acknowledged; its next write or a dismissal clears it. */
  saveFailedFor: ReadonlySet<string>;
  dismissSaveFailed: (key: string) => void;
  /** Show the list at once and save it. */
  save: (key: string, replies: string[]) => void;
  reload: () => void;
}

type Lists = Readonly<Record<string, readonly string[]>>;

function withKey(lists: Lists, key: string, replies: readonly string[]): Lists {
  const { [key]: _replaced, ...rest } = lists;
  return replies.length > 0 ? { ...rest, [key]: replies } : rest;
}

export interface KeyedSuggestedRepliesSource {
  /** Names the hook in console warnings. */
  name: string;
  /** The SSE topic whose delta means "read again". */
  topic: string;
  get: () => Promise<Record<string, string[]>>;
  put: (key: string, replies: string[]) => Promise<string[]>;
}

/** One owner-only map of 建議回覆 lists, re-read on every `source.topic` delta.
 * `source` must be a module constant: a new object each render re-subscribes. */
export function useKeyedSuggestedReplies(source: KeyedSuggestedRepliesSource): UseKeyedSuggestedReplies {
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
  // made meanwhile wait, keeping only the newest per key. A delta during a
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
      source.get().then(
        (next) => {
          if (!aliveRef.current || op !== latestReadRef.current || op < lastWriteRef.current) return;
          ackedRef.current = next;
          loadedRef.current = true;
          setLists(next);
          setLoaded(true);
          setLoadFailed(false);
        },
        (e) => {
          console.warn(`${source.name}: load failed`, e);
          if (aliveRef.current && !loadedRef.current) setLoadFailed(true);
        },
      );
    };
    loadRef.current = load;
    load();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === source.topic) load();
    });
    return () => {
      aliveRef.current = false;
      unsubscribe();
    };
  }, [source]);

  const clearSaveFailed = useCallback((key: string) => {
    setSaveFailedFor((cur) => {
      if (!cur.has(key)) return cur;
      const next = new Set(cur);
      next.delete(key);
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
    const [key, replies] = entry.value;
    queuedRef.current.delete(key);
    inFlightRef.current = true;
    lastWriteRef.current = ++opRef.current;
    // An edit queued behind a PUT that failed goes out as that key's next save.
    clearSaveFailed(key);
    source.put(key, replies).then(
      (stored) => {
        ackedRef.current = withKey(ackedRef.current, key, stored);
        if (aliveRef.current) sendNext();
      },
      (e) => {
        console.warn(`${source.name}: save failed`, e);
        if (!aliveRef.current) return;
        setSaveFailedFor((cur) => new Set(cur).add(key));
        // A newer edit for this key is still on its way and decides the screen.
        if (!queuedRef.current.has(key)) {
          const acked = ackedRef.current[key] ?? [];
          setLists((cur) => withKey(cur, key, acked));
        }
        // What reached the server is unknown: ask it what it holds now.
        rereadRef.current = true;
        sendNext();
      },
    );
  }, [clearSaveFailed, source]);

  const save = useCallback(
    (key: string, replies: string[]) => {
      setLists((cur) => ({ ...cur, [key]: replies }));
      clearSaveFailed(key);
      queuedRef.current.set(key, replies);
      if (!inFlightRef.current) sendNext();
    },
    [sendNext, clearSaveFailed],
  );

  const reload = useCallback(() => loadRef.current(), []);

  return useMemo(
    () => ({ lists, loaded, loadFailed, saveFailedFor, dismissSaveFailed: clearSaveFailed, save, reload }),
    [lists, loaded, loadFailed, saveFailedFor, clearSaveFailed, save, reload],
  );
}

const noop = () => {};
export const KEYED_SUGGESTED_REPLIES_NOT_PROVIDED: UseKeyedSuggestedReplies = {
  lists: {},
  loaded: false,
  loadFailed: false,
  saveFailedFor: new Set(),
  dismissSaveFailed: noop,
  save: noop,
  reload: noop,
};
