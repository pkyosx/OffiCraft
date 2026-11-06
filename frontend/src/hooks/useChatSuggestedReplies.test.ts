import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

type Listener = (topic: string) => void;
const listeners: Listener[] = [];
const getChatSuggestedReplies = vi.fn<() => Promise<Record<string, string[]>>>();
const putChatSuggestedReplies = vi.fn<(id: string, replies: string[]) => Promise<string[]>>();

vi.mock("../api", () => ({
  api: {
    getChatSuggestedReplies: () => getChatSuggestedReplies(),
    putChatSuggestedReplies: (id: string, replies: string[]) => putChatSuggestedReplies(id, replies),
    subscribeEvents: (cb: Listener) => {
      listeners.push(cb);
      return () => listeners.splice(listeners.indexOf(cb), 1);
    },
  },
}));

import { useChatSuggestedReplies } from "./useChatSuggestedReplies";

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

async function mounted() {
  const hook = renderHook(() => useChatSuggestedReplies());
  await waitFor(() => expect(hook.result.current.loaded).toBe(true));
  return hook;
}

beforeEach(() => {
  listeners.length = 0;
  getChatSuggestedReplies.mockReset().mockResolvedValue({ mira: ["繼續"] });
  putChatSuggestedReplies.mockReset();
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

describe("useChatSuggestedReplies", () => {
  it("is not loaded until the first read answers, then shows the server's lists", async () => {
    const read = deferred<Record<string, string[]>>();
    getChatSuggestedReplies.mockReturnValue(read.promise);
    const { result } = renderHook(() => useChatSuggestedReplies());
    expect(result.current).toMatchObject({ lists: {}, loaded: false, saveFailed: false });
    await act(async () => read.resolve({ mira: ["繼續"] }));
    expect(result.current).toMatchObject({ lists: { mira: ["繼續"] }, loaded: true, saveFailed: false });
  });

  it("a failed first read stays not loaded, and the next delta reads again", async () => {
    getChatSuggestedReplies.mockRejectedValueOnce(new Error("down"));
    const { result } = renderHook(() => useChatSuggestedReplies());
    await waitFor(() => expect(getChatSuggestedReplies).toHaveBeenCalledTimes(1));
    await act(async () => {});
    expect(result.current.loaded).toBe(false);
    emit("chat_suggested_reply");
    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.lists).toEqual({ mira: ["繼續"] });
  });

  it("re-reads on its own topic and on no other", async () => {
    const { result } = await mounted();
    emit("chat_group");
    expect(getChatSuggestedReplies).toHaveBeenCalledTimes(1);
    getChatSuggestedReplies.mockResolvedValue({ beto: ["好"] });
    emit("chat_suggested_reply");
    await waitFor(() => expect(result.current.lists).toEqual({ beto: ["好"] }));
  });

  it("a save shows at once, keeps other members' lists and writes only that member", async () => {
    const put = deferred<string[]>();
    putChatSuggestedReplies.mockReturnValue(put.promise);
    const { result } = await mounted();
    act(() => result.current.save("beto", ["好"]));
    expect(result.current.lists).toEqual({ mira: ["繼續"], beto: ["好"] });
    expect(putChatSuggestedReplies.mock.calls).toEqual([["beto", ["好"]]]);
    await act(async () => put.resolve(["好"]));
  });

  it("edits during a PUT wait and only the newest per member is sent", async () => {
    const first = deferred<string[]>();
    putChatSuggestedReplies.mockReturnValueOnce(first.promise).mockResolvedValue([]);
    const { result } = await mounted();
    act(() => result.current.save("mira", ["a"]));
    act(() => result.current.save("mira", ["b"]));
    act(() => result.current.save("mira", ["c"]));
    expect(putChatSuggestedReplies).toHaveBeenCalledTimes(1);
    await act(async () => first.resolve(["a"]));
    expect(putChatSuggestedReplies.mock.calls).toEqual([
      ["mira", ["a"]],
      ["mira", ["c"]],
    ]);
  });

  it("a read answer sent before a save does not put the old list back", async () => {
    const { result } = await mounted();
    const stale = deferred<Record<string, string[]>>();
    getChatSuggestedReplies.mockReturnValueOnce(stale.promise);
    emit("chat_suggested_reply");
    putChatSuggestedReplies.mockResolvedValue(["新"]);
    await act(async () => result.current.save("mira", ["新"]));
    await act(async () => stale.resolve({ mira: ["舊"] }));
    expect(result.current.lists).toEqual({ mira: ["新"] });
  });

  it("a delta during a PUT is read once the PUT has settled", async () => {
    const put = deferred<string[]>();
    putChatSuggestedReplies.mockReturnValue(put.promise);
    const { result } = await mounted();
    act(() => result.current.save("mira", ["新"]));
    emit("chat_suggested_reply");
    expect(getChatSuggestedReplies).toHaveBeenCalledTimes(1);
    getChatSuggestedReplies.mockResolvedValue({ mira: ["新"], beto: ["別的視窗"] });
    await act(async () => put.resolve(["新"]));
    expect(getChatSuggestedReplies).toHaveBeenCalledTimes(2);
    await waitFor(() => expect(result.current.lists).toEqual({ mira: ["新"], beto: ["別的視窗"] }));
  });

  it("a failed save says so and shows what the server holds; the next save clears it", async () => {
    putChatSuggestedReplies.mockRejectedValueOnce(new Error("no"));
    const { result } = await mounted();
    await act(async () => result.current.save("mira", ["新"]));
    await waitFor(() => expect(result.current.lists).toEqual({ mira: ["繼續"] }));
    expect(result.current.saveFailed).toBe(true);
    putChatSuggestedReplies.mockResolvedValue(["再一次"]);
    await act(async () => result.current.save("mira", ["再一次"]));
    expect(result.current.saveFailed).toBe(false);
  });
});
