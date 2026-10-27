import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatGroupView, ChatGroupWrite } from "../api/adapter";

type Listener = (topic: string) => void;
const listeners: Listener[] = [];
const getChatGroups = vi.fn<() => Promise<ChatGroupView[]>>();
const putChatGroups = vi.fn<(groups: ChatGroupWrite[]) => Promise<ChatGroupView[]>>();

vi.mock("../api", () => ({
  api: {
    getChatGroups: () => getChatGroups(),
    putChatGroups: (groups: ChatGroupWrite[]) => putChatGroups(groups),
    subscribeEvents: (cb: Listener) => {
      listeners.push(cb);
      return () => listeners.splice(listeners.indexOf(cb), 1);
    },
  },
}));

import { useChatGroups } from "./useChatGroups";
import { createGroup, moveMember, renameGroup } from "../lib/chatGroups";

const emit = (topic: string) => act(() => listeners.forEach((cb) => cb(topic)));
const g = (id: string, name: string, memberIds: string[]): ChatGroupView => ({
  id,
  tab: "staff",
  name,
  memberIds,
  taskTypeKeys: [],
});
const CORE = g("cg-1", "核心", ["mira"]);
const CORE_BETO = g("cg-1", "核心", ["mira", "beto"]);
const addBeto = (s: readonly ChatGroupView[]) => moveMember(s, "beto", "cg-1");

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  listeners.length = 0;
  getChatGroups.mockReset().mockResolvedValue([CORE]);
  putChatGroups.mockReset();
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

describe("useChatGroups", () => {
  it("on mount shows the server's groups and reports the first read settled", async () => {
    const { result } = renderHook(() => useChatGroups(null));
    expect(result.current.groups).toEqual([]);
    expect(result.current.settled).toBe(false);
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));
    expect(result.current.settled).toBe(true);
  });

  it("a change sends the whole state with a new group's id as null and shows it at once; the new group keeps its local id on screen and later writes send the server's", async () => {
    const put = deferred<ChatGroupView[]>();
    putChatGroups.mockReturnValue(put.promise);
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));

    act(() =>
      result.current.update((s) =>
        createGroup(s, { id: result.current.newGroupId(), tab: "staff", name: "工程", memberIds: ["beto"] }),
      ),
    );
    expect(putChatGroups).toHaveBeenCalledWith([
      { id: null, tab: "staff", name: "工程", memberIds: ["beto"], taskTypeKeys: [] },
      { id: "cg-1", tab: "staff", name: "核心", memberIds: ["mira"], taskTypeKeys: [] },
    ]);
    expect(result.current.groups).toEqual([g("cg-local-1", "工程", ["beto"]), CORE]);

    await act(async () => put.resolve([g("cg-9", "工程", ["beto"]), CORE]));
    expect(result.current.groups).toEqual([g("cg-local-1", "工程", ["beto"]), CORE]);
    expect(result.current.error).toBe(null);

    putChatGroups.mockResolvedValue([g("cg-9", "工程", ["beto", "mira"]), g("cg-1", "核心", [])]);
    act(() => result.current.update((s) => moveMember(s, "mira", "cg-local-1")));
    expect(putChatGroups).toHaveBeenLastCalledWith([
      { id: "cg-9", tab: "staff", name: "工程", memberIds: ["beto", "mira"], taskTypeKeys: [] },
      { id: "cg-1", tab: "staff", name: "核心", memberIds: [], taskTypeKeys: [] },
    ]);
    await waitFor(() =>
      expect(result.current.groups).toEqual([g("cg-local-1", "工程", ["beto", "mira"]), g("cg-1", "核心", [])]),
    );
  });

  it("the screen shows the server's answer to a write, not what was sent", async () => {
    putChatGroups.mockResolvedValue([CORE]);
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));
    act(() => result.current.update(addBeto));
    expect(result.current.groups).toEqual([CORE_BETO]);
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));
  });

  it("a change leaves out members no longer among the known ones", async () => {
    getChatGroups.mockResolvedValue([g("cg-1", "核心", ["mira", "gone"])]);
    putChatGroups.mockResolvedValue([CORE_BETO]);
    const { result } = renderHook(() => useChatGroups(new Set(["mira", "beto"])));
    await waitFor(() => expect(result.current.groups).toEqual([g("cg-1", "核心", ["mira", "gone"])]));

    act(() => result.current.update(addBeto));
    expect(putChatGroups).toHaveBeenCalledWith([
      { id: "cg-1", tab: "staff", name: "核心", memberIds: ["mira", "beto"], taskTypeKeys: [] },
    ]);
  });

  it("a refused write reports the failure and shows what a fresh read of the server returns", async () => {
    putChatGroups.mockRejectedValueOnce(new Error("http 422 for PUT /api/chat/groups"));
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));
    getChatGroups.mockResolvedValue([g("cg-1", "核心", [])]);

    act(() => result.current.update(addBeto));
    expect(result.current.groups).toEqual([CORE_BETO]);
    await waitFor(() => expect(result.current.error).toBe("save"));
    await waitFor(() => expect(result.current.groups).toEqual([g("cg-1", "核心", [])]));
    expect(getChatGroups).toHaveBeenCalledTimes(2);

    putChatGroups.mockResolvedValueOnce([CORE]);
    act(() => result.current.update((s) => moveMember(s, "mira", "cg-1")));
    expect(result.current.error).toBe(null);
  });

  it("when an earlier write lands and the latest one fails, the groups are the server's (with the earlier write)", async () => {
    const put1 = deferred<ChatGroupView[]>();
    const put2 = deferred<ChatGroupView[]>();
    putChatGroups.mockReturnValueOnce(put1.promise).mockReturnValueOnce(put2.promise);
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));

    act(() => result.current.update(addBeto));
    act(() => result.current.update((s) => moveMember(s, "clara", "cg-1")));
    await act(async () => put1.resolve([CORE_BETO]));
    getChatGroups.mockResolvedValue([CORE_BETO]);
    await act(async () => put2.reject(new Error("http 500")));

    await waitFor(() => expect(result.current.groups).toEqual([CORE_BETO]));
    expect(result.current.error).toBe("save");
  });

  it("changes made while a save is out wait for it and go in one PUT that names the group the first one created; ids on screen stay put", async () => {
    const put1 = deferred<ChatGroupView[]>();
    const put2 = deferred<ChatGroupView[]>();
    putChatGroups.mockReturnValueOnce(put1.promise).mockReturnValueOnce(put2.promise);
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));

    act(() =>
      result.current.update((s) =>
        createGroup(s, { id: result.current.newGroupId(), tab: "staff", name: "工程", memberIds: ["beto"] }),
      ),
    );
    act(() => result.current.update((s) => moveMember(s, "mira", "cg-local-1")));
    act(() => result.current.update((s) => renameGroup(s, "cg-local-1", "後端")));
    expect(putChatGroups).toHaveBeenCalledTimes(1);
    expect(result.current.groups).toEqual([g("cg-local-1", "後端", ["beto", "mira"]), g("cg-1", "核心", [])]);

    await act(async () => put1.resolve([g("cg-A", "工程", ["beto"]), CORE]));
    expect(putChatGroups).toHaveBeenCalledTimes(2);
    expect(putChatGroups.mock.calls[1][0]).toEqual([
      { id: "cg-A", tab: "staff", name: "後端", memberIds: ["beto", "mira"], taskTypeKeys: [] },
      { id: "cg-1", tab: "staff", name: "核心", memberIds: [], taskTypeKeys: [] },
    ]);
    expect(result.current.groups).toEqual([g("cg-local-1", "後端", ["beto", "mira"]), g("cg-1", "核心", [])]);

    await act(async () => put2.resolve([g("cg-A", "後端", ["beto", "mira"]), g("cg-1", "核心", [])]));
    expect(result.current.groups).toEqual([g("cg-local-1", "後端", ["beto", "mira"]), g("cg-1", "核心", [])]);
    putChatGroups.mockResolvedValue([g("cg-A", "後端", ["beto"]), g("cg-1", "核心", ["mira"])]);
    act(() => result.current.update((s) => moveMember(s, "mira", "cg-1")));
    expect(putChatGroups.mock.calls[2][0].map((x) => x.id)).toEqual(["cg-A", "cg-1"]);
  });

  it("a chat_group event during a save is read once the save settles, and the next save keeps what it brought", async () => {
    const put = deferred<ChatGroupView[]>();
    putChatGroups.mockReturnValueOnce(put.promise);
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));

    act(() => result.current.update(addBeto));
    emit("chat_group");
    expect(getChatGroups).toHaveBeenCalledTimes(1);
    getChatGroups.mockResolvedValue([g("cg-1", "核心", ["mira", "beto", "ow-new"])]);
    await act(async () => put.resolve([CORE_BETO]));
    await waitFor(() => expect(result.current.groups).toEqual([g("cg-1", "核心", ["mira", "beto", "ow-new"])]));
    expect(getChatGroups).toHaveBeenCalledTimes(2);

    putChatGroups.mockResolvedValue([g("cg-1", "核心", ["beto", "ow-new"])]);
    act(() => result.current.update((s) => moveMember(s, "mira", null)));
    expect(putChatGroups).toHaveBeenLastCalledWith([
      { id: "cg-1", tab: "staff", name: "核心", memberIds: ["beto", "ow-new"], taskTypeKeys: [] },
    ]);
  });

  it("a change that changes nothing sends nothing", async () => {
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));
    act(() => result.current.update((s) => moveMember(s, "mira", "cg-1")));
    expect(putChatGroups).not.toHaveBeenCalled();
  });

  it("when two reads answer out of order, the newer read's groups stay", async () => {
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));
    const older = deferred<ChatGroupView[]>();
    const newer = deferred<ChatGroupView[]>();
    getChatGroups.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);
    emit("chat_group");
    emit("chat_group");

    await act(async () => newer.resolve([CORE_BETO]));
    await act(async () => older.resolve([g("cg-1", "核心", [])]));

    expect(result.current.groups).toEqual([CORE_BETO]);
  });

  it("a read sent before a write and answered after it does not undo the write", async () => {
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));
    const slowGet = deferred<ChatGroupView[]>();
    getChatGroups.mockReturnValue(slowGet.promise);
    emit("chat_group");
    putChatGroups.mockResolvedValue([CORE_BETO]);

    act(() => result.current.update(addBeto));
    await waitFor(() => expect(putChatGroups).toHaveBeenCalledTimes(1));
    await act(async () => slowGet.resolve([CORE]));
    expect(result.current.groups).toEqual([CORE_BETO]);
  });

  it("a chat_group delta re-reads and shows the new groups; other topics do not", async () => {
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));
    getChatGroups.mockResolvedValue([CORE_BETO]);

    emit("member");
    expect(getChatGroups).toHaveBeenCalledTimes(1);
    emit("chat_group");
    await waitFor(() => expect(result.current.groups).toEqual([CORE_BETO]));
    expect(getChatGroups).toHaveBeenCalledTimes(2);
  });

  it("when the first read fails, the failure shows and a change reads again instead of writing", async () => {
    getChatGroups.mockRejectedValueOnce(new Error("http 500"));
    const { result } = renderHook(() => useChatGroups(null));
    await waitFor(() => expect(result.current.error).toBe("load"));
    expect(result.current.settled).toBe(true);
    expect(result.current.groups).toEqual([]);

    act(() => result.current.update(addBeto));
    expect(putChatGroups).not.toHaveBeenCalled();
    await waitFor(() => expect(result.current.groups).toEqual([CORE]));
    expect(getChatGroups).toHaveBeenCalledTimes(2);
    expect(result.current.error).toBe(null);
  });

  it("a change before the first groups arrive sends nothing", async () => {
    const get = deferred<ChatGroupView[]>();
    getChatGroups.mockReturnValueOnce(get.promise);
    const { result } = renderHook(() => useChatGroups(null));

    act(() => result.current.update(addBeto));
    expect(putChatGroups).not.toHaveBeenCalled();
    expect(result.current.groups).toEqual([]);
    await act(async () => get.resolve([CORE]));
    expect(result.current.groups).toEqual([CORE]);
  });
});
