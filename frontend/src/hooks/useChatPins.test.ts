import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

type Listener = (topic: string) => void;
const listeners: Listener[] = [];
const getChatPins = vi.fn<() => Promise<string[]>>();
const putChatPins = vi.fn<(ids: string[]) => Promise<string[]>>();

vi.mock("../api", () => ({
  api: {
    getChatPins: () => getChatPins(),
    putChatPins: (ids: string[]) => putChatPins(ids),
    subscribeEvents: (cb: Listener) => {
      listeners.push(cb);
      return () => listeners.splice(listeners.indexOf(cb), 1);
    },
  },
}));

import { useChatPins } from "./useChatPins";

const emit = (topic: string) => act(() => listeners.forEach((cb) => cb(topic)));

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
  getChatPins.mockReset().mockResolvedValue(["mira", "ow-1"]);
  putChatPins.mockReset();
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

describe("useChatPins", () => {
  it("on mount shows the server's pin list and reports the first read settled", async () => {
    const { result } = renderHook(() => useChatPins(null));
    expect(result.current.pins).toEqual([]);
    expect(result.current.settled).toBe(false);
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "ow-1"]));
    expect(result.current.settled).toBe(true);
    expect(getChatPins).toHaveBeenCalledTimes(1);
  });

  it("toggling an unpinned member sends the whole new list, shows it at once, then adopts the server's answer", async () => {
    const put = deferred<string[]>();
    putChatPins.mockReturnValue(put.promise);
    const { result } = renderHook(() => useChatPins(null));
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "ow-1"]));

    act(() => result.current.toggle("beto", ["mira"]));
    expect(putChatPins).toHaveBeenCalledWith(["mira", "beto", "ow-1"]);
    expect(result.current.pins).toEqual(["mira", "beto", "ow-1"]);

    await act(async () => put.resolve(["beto", "mira", "ow-1"]));
    expect(result.current.pins).toEqual(["beto", "mira", "ow-1"]);
    expect(result.current.error).toBe(null);
  });

  it("reordering sends the list with the member moved to its new place among its tab's pins", async () => {
    putChatPins.mockResolvedValue(["ow-1", "mira"]);
    const { result } = renderHook(() => useChatPins(null));
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "ow-1"]));
    getChatPins.mockResolvedValue(["mira", "clara", "ow-1"]);
    emit("chat_pin");
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "clara", "ow-1"]));

    act(() => result.current.reorder("clara", 0, ["mira", "clara"]));
    expect(putChatPins).toHaveBeenCalledWith(["clara", "mira", "ow-1"]);
  });

  it("a refused write reports the failure and shows what a fresh read of the server returns", async () => {
    putChatPins.mockRejectedValueOnce(new Error("http 422 for PUT /api/chat/pins"));
    const { result } = renderHook(() => useChatPins(null));
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "ow-1"]));
    getChatPins.mockResolvedValue(["ow-1", "mira"]);

    act(() => result.current.toggle("mira", ["mira"]));
    expect(result.current.pins).toEqual(["ow-1"]);
    await waitFor(() => expect(result.current.error).toBe("save"));
    await waitFor(() => expect(result.current.pins).toEqual(["ow-1", "mira"]));
    expect(getChatPins).toHaveBeenCalledTimes(2);

    putChatPins.mockResolvedValueOnce(["mira"]);
    act(() => result.current.toggle("ow-1", []));
    expect(result.current.error).toBe(null);
  });

  it("when an earlier write lands and the latest one fails, the list is the server's (with the earlier write), not the list from before both", async () => {
    const put1 = deferred<string[]>();
    const put2 = deferred<string[]>();
    putChatPins.mockReturnValueOnce(put1.promise).mockReturnValueOnce(put2.promise);
    const { result } = renderHook(() => useChatPins(null));
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "ow-1"]));

    act(() => result.current.toggle("beto", ["mira"]));
    act(() => result.current.toggle("clara", ["mira", "beto"]));
    expect(putChatPins.mock.calls).toEqual([
      [["mira", "beto", "ow-1"]],
      [["mira", "beto", "clara", "ow-1"]],
    ]);
    await act(async () => put1.resolve(["mira", "beto", "ow-1"]));
    getChatPins.mockResolvedValue(["mira", "beto", "ow-1"]);
    await act(async () => put2.reject(new Error("http 500")));

    await waitFor(() => expect(result.current.pins).toEqual(["mira", "beto", "ow-1"]));
    expect(result.current.error).toBe("save");
  });

  it("when a re-read sent during the latest write answers first and that write then fails, the failure still shows", async () => {
    const put1 = deferred<string[]>();
    const put2 = deferred<string[]>();
    putChatPins.mockReturnValueOnce(put1.promise).mockReturnValueOnce(put2.promise);
    const { result } = renderHook(() => useChatPins(null));
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "ow-1"]));

    act(() => result.current.toggle("beto", ["mira"]));
    act(() => result.current.toggle("clara", ["mira", "beto"]));
    await act(async () => put1.resolve(["mira", "beto", "ow-1"]));
    getChatPins.mockResolvedValue(["mira", "beto", "ow-1"]);
    emit("chat_pin");
    await waitFor(() => expect(getChatPins).toHaveBeenCalledTimes(2));
    expect(result.current.pins).toEqual(["mira", "beto", "clara", "ow-1"]);

    await act(async () => put2.reject(new Error("http 500")));
    await waitFor(() => expect(result.current.error).toBe("save"));
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "beto", "ow-1"]));
  });

  it("when the first read fails, the failure shows and a toggle reads again instead of writing", async () => {
    getChatPins.mockRejectedValueOnce(new Error("http 500"));
    const { result } = renderHook(() => useChatPins(null));
    await waitFor(() => expect(result.current.error).toBe("load"));
    expect(result.current.settled).toBe(true);
    expect(result.current.pins).toEqual([]);

    act(() => result.current.toggle("beto", []));
    expect(putChatPins).not.toHaveBeenCalled();
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "ow-1"]));
    expect(getChatPins).toHaveBeenCalledTimes(2);
    expect(result.current.error).toBe(null);
  });

  it("a chat_pin delta re-reads the list and shows the new one; other topics do not", async () => {
    const { result } = renderHook(() => useChatPins(null));
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "ow-1"]));
    getChatPins.mockResolvedValue(["clara"]);

    emit("member");
    expect(getChatPins).toHaveBeenCalledTimes(1);
    emit("chat_pin");
    await waitFor(() => expect(result.current.pins).toEqual(["clara"]));
    expect(getChatPins).toHaveBeenCalledTimes(2);
  });

  it("a write leaves out pinned ids that are no longer among the known members", async () => {
    getChatPins.mockResolvedValue(["mira", "gone", "ow-1"]);
    putChatPins.mockResolvedValue(["mira", "beto", "ow-1"]);
    const known = new Set(["mira", "beto", "ow-1"]);
    const { result } = renderHook(() => useChatPins(known));
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "gone", "ow-1"]));

    act(() => result.current.toggle("beto", ["mira"]));
    expect(putChatPins).toHaveBeenCalledWith(["mira", "beto", "ow-1"]);
  });

  it("a toggle before the first list arrives sends nothing", async () => {
    const get = deferred<string[]>();
    getChatPins.mockReturnValue(get.promise);
    const { result } = renderHook(() => useChatPins(null));

    act(() => result.current.toggle("beto", []));
    expect(putChatPins).not.toHaveBeenCalled();
    expect(result.current.pins).toEqual([]);
    await act(async () => get.resolve(["mira"]));
    expect(result.current.pins).toEqual(["mira"]);
  });

  it("a read sent before a write and answered after it does not undo the write", async () => {
    const { result } = renderHook(() => useChatPins(null));
    await waitFor(() => expect(result.current.pins).toEqual(["mira", "ow-1"]));
    const slowGet = deferred<string[]>();
    getChatPins.mockReturnValue(slowGet.promise);
    emit("chat_pin");
    putChatPins.mockResolvedValue(["ow-1"]);

    act(() => result.current.toggle("mira", ["mira"]));
    await waitFor(() => expect(putChatPins).toHaveBeenCalledTimes(1));
    await act(async () => slowGet.resolve(["mira", "ow-1"]));
    expect(result.current.pins).toEqual(["ow-1"]);
  });
});
