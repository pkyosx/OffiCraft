import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api";
import type { ChatGroup } from "../lib/chatGroups";

export type ChatGroupsError = "load" | "save";

interface UseChatGroups {
  /** Both tabs' groups, staff first; [] until loaded. */
  groups: readonly ChatGroup[];
  /** The first read has finished, answered or not. */
  settled: boolean;
  /** "load": the groups could not be read, so they cannot be changed yet (the
   * next change reads again instead). "save": the latest write failed and the
   * groups were re-read from the server. A read the hook adopts clears "load";
   * "save" stays until the next write starts. */
  error: ChatGroupsError | null;
  /** Change both tabs' state; the whole result is what PUT sends. */
  update: (change: (current: readonly ChatGroup[]) => ChatGroup[]) => void;
  /** An id for a group about to be created; the server mints the real one. */
  newGroupId: () => string;
}

const LOCAL = "cg-local-";

/**
 * The owner's chat-list groups (GET / PUT /api/chat/groups), re-read on every
 * `chat_group` delta — another browser's write, our own echo, a worker that
 * auto-joined, or a resync.
 *
 * `knownIds` is every member the sidebar can currently show, or null while
 * that is not known. A write drops members not in it: the server leaves a
 * removed member out of its answer without telling us, and a stale id in a PUT
 * is a 422.
 */
export function useChatGroups(knownIds: ReadonlySet<string> | null): UseChatGroups {
  const [groups, setGroups] = useState<readonly ChatGroup[]>([]);
  const [settled, setSettled] = useState(false);
  const [error, setError] = useState<ChatGroupsError | null>(null);
  const groupsRef = useRef<readonly ChatGroup[]>([]);
  const loadedRef = useRef(false);
  const knownRef = useRef(knownIds);
  knownRef.current = knownIds;
  const aliveRef = useRef(true);
  const seq = useRef(0);
  // One counter orders reads and writes. A write answer lands only if it is
  // the latest write. A read answer lands only if, when the read was SENT, no
  // write was in flight (it could not see that write), no write has been sent
  // since, and it is the newest read. Dropping the others loses nothing: every
  // write is followed by a read (its own chat_group echo, or the re-read after
  // a failure).
  const opRef = useRef(0);
  const lastWriteRef = useRef(0);
  const lastWriteSettledRef = useRef(true);
  const latestReadRef = useRef(0);
  const loadRef = useRef<() => void>(() => {});
  // A group created here keeps its local id on screen after the server mints
  // its own, so state keyed by group id (collapsed sections, an open menu)
  // survives the echo. server id → local id.
  const aliasRef = useRef(new Map<string, string>());
  const toView = (list: readonly ChatGroup[]) =>
    list.map((g) => (aliasRef.current.has(g.id) ? { ...g, id: aliasRef.current.get(g.id)! } : g));
  const toServerId = (id: string) => {
    for (const [server, local] of aliasRef.current) if (local === id) return server;
    return id.startsWith(LOCAL) ? null : id;
  };

  const show = (next: readonly ChatGroup[]) => {
    groupsRef.current = next;
    setGroups(next);
  };

  useEffect(() => {
    aliveRef.current = true;
    const load = () => {
      const op = ++opRef.current;
      latestReadRef.current = op;
      const sentClean = lastWriteSettledRef.current;
      api
        .getChatGroups()
        .then((list) => {
          if (!aliveRef.current) return;
          loadedRef.current = true;
          setSettled(true);
          if (!sentClean || op !== latestReadRef.current || op < lastWriteRef.current) return;
          show(toView(list));
          setError((e) => (e === "load" ? null : e));
        })
        .catch((e) => {
          console.warn("useChatGroups: load failed", e);
          if (!aliveRef.current) return;
          setSettled(true);
          if (!loadedRef.current) setError("load");
        });
    };
    loadRef.current = load;
    load();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "chat_group") load();
    });
    return () => {
      aliveRef.current = false;
      unsubscribe();
    };
  }, []);

  const update = useCallback((change: (current: readonly ChatGroup[]) => ChatGroup[]) => {
    // Without the server's groups any write would be a guess; read again.
    if (!loadedRef.current) {
      loadRef.current();
      return;
    }
    const known = knownRef.current;
    const current = known
      ? groupsRef.current.map((g) => ({ ...g, memberIds: g.memberIds.filter((id) => known.has(id)) }))
      : groupsRef.current;
    const next = change(current);
    const op = ++opRef.current;
    lastWriteRef.current = op;
    lastWriteSettledRef.current = false;
    show(next);
    setError(null);
    const body = next.map((g) => ({ ...g, id: toServerId(g.id) }));
    api
      .putChatGroups(body)
      .then(
        (list) => {
          // Names are unique within a tab, so they pair each new group with
          // the id the server minted for it.
          body.forEach((sent, i) => {
            if (sent.id !== null) return;
            const minted = list.find((g) => g.tab === sent.tab && g.name === sent.name.trim());
            if (minted && !aliasRef.current.has(minted.id)) aliasRef.current.set(minted.id, next[i].id);
          });
          if (!aliveRef.current || op !== lastWriteRef.current) return;
          lastWriteSettledRef.current = true;
          show(toView(list));
        },
        (e) => {
          console.warn("useChatGroups: save failed", e);
          if (!aliveRef.current || op !== lastWriteRef.current) return;
          lastWriteSettledRef.current = true;
          // An earlier write may have landed, so the last state we adopted can
          // be stale: ask the server what it holds now.
          setError("save");
          loadRef.current();
        },
      );
  }, []);

  const newGroupId = useCallback(() => `${LOCAL}${++seq.current}`, []);
  return { groups, settled, error, update, newGroupId };
}
