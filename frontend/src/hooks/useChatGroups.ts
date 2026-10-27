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
  // One PUT at a time. Changes made while it is out are shown at once and sent
  // together when it answers, so the next body can name a group the answer
  // just minted. A chat_group event during a PUT is not read right away (that
  // read could not see the PUT); it is read once the PUT has settled.
  const inFlightRef = useRef(false);
  const dirtyRef = useRef(false);
  const rereadRef = useRef(false);
  // One counter orders reads and writes: a read answer lands only if it is
  // the newest read and no PUT was sent after it.
  const opRef = useRef(0);
  const lastWriteRef = useRef(0);
  const latestReadRef = useRef(0);
  const loadRef = useRef<() => void>(() => {});
  // A group created here keeps its local id on screen after the server mints
  // its own, so state keyed by group id (collapsed sections, an open menu)
  // survives the echo. local id → server id.
  const aliasRef = useRef(new Map<string, string>());
  const toView = (list: readonly ChatGroup[]) => {
    const local = new Map([...aliasRef.current].map(([l, srv]) => [srv, l]));
    return list.map((g) => (local.has(g.id) ? { ...g, id: local.get(g.id)! } : g));
  };
  const toServerId = (id: string) => aliasRef.current.get(id) ?? (id.startsWith(LOCAL) ? null : id);
  const knownOnly = (list: readonly ChatGroup[]) => {
    const known = knownRef.current;
    return known ? list.map((g) => ({ ...g, memberIds: g.memberIds.filter((id) => known.has(id)) })) : list;
  };

  const show = (next: readonly ChatGroup[]) => {
    groupsRef.current = next;
    setGroups(next);
  };

  useEffect(() => {
    aliveRef.current = true;
    const load = () => {
      if (inFlightRef.current) {
        rereadRef.current = true;
        return;
      }
      const op = ++opRef.current;
      latestReadRef.current = op;
      api
        .getChatGroups()
        .then((list) => {
          if (!aliveRef.current) return;
          loadedRef.current = true;
          setSettled(true);
          if (op !== latestReadRef.current || op < lastWriteRef.current) return;
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

  const send = useCallback(() => {
    const sent = knownOnly(groupsRef.current);
    const body = sent.map((g) => ({ ...g, id: toServerId(g.id) }));
    lastWriteRef.current = ++opRef.current;
    inFlightRef.current = true;
    dirtyRef.current = false;
    api.putChatGroups(body).then(
      (list) => {
        // Names are unique within a tab, so they pair each new group with the
        // id the server minted for it.
        body.forEach((b, i) => {
          if (b.id !== null) return;
          const minted = list.find((g) => g.tab === b.tab && g.name === b.name.trim());
          if (minted) aliasRef.current.set(sent[i].id, minted.id);
        });
        if (!aliveRef.current) return;
        if (dirtyRef.current) {
          send();
          return;
        }
        inFlightRef.current = false;
        show(toView(list));
        if (rereadRef.current) {
          rereadRef.current = false;
          loadRef.current();
        }
      },
      (e) => {
        console.warn("useChatGroups: save failed", e);
        if (!aliveRef.current) return;
        inFlightRef.current = false;
        dirtyRef.current = false;
        rereadRef.current = false;
        // What reached the server is unknown: ask it what it holds now.
        setError("save");
        loadRef.current();
      },
    );
  }, []);

  const update = useCallback(
    (change: (current: readonly ChatGroup[]) => ChatGroup[]) => {
      // Without the server's groups any write would be a guess; read again.
      if (!loadedRef.current) {
        loadRef.current();
        return;
      }
      const current = knownOnly(groupsRef.current);
      const next = change(current);
      if (JSON.stringify(next) === JSON.stringify(current)) return;
      show(next);
      setError(null);
      if (inFlightRef.current) dirtyRef.current = true;
      else send();
    },
    [send],
  );

  const newGroupId = useCallback(() => `${LOCAL}${++seq.current}`, []);
  return { groups, settled, error, update, newGroupId };
}
