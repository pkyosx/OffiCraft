import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

type Listener = (topic: string) => void;
const listeners: Listener[] = [];
const getLists = vi.fn<() => Promise<Record<string, string[]>>>();
const putList = vi.fn<(key: string, replies: string[]) => Promise<string[]>>();

vi.mock("../api", () => ({
  api: {
    subscribeEvents: (cb: Listener) => {
      listeners.push(cb);
      return () => listeners.splice(listeners.indexOf(cb), 1);
    },
  },
}));

import { useKeyedSuggestedReplies, type KeyedSuggestedRepliesSource } from "./useKeyedSuggestedReplies";

const SOURCE: KeyedSuggestedRepliesSource = {
  name: "test",
  topic: "keyed_topic",
  get: () => getLists(),
  put: (key, replies) => putList(key, replies),
};

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
  const hook = renderHook(() => useKeyedSuggestedReplies(SOURCE));
  await waitFor(() => expect(hook.result.current.loaded).toBe(true));
  return hook;
}

beforeEach(() => {
  listeners.length = 0;
  getLists.mockReset().mockResolvedValue({ mira: ["繼續"] });
  putList.mockReset();
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

describe("useKeyedSuggestedReplies", () => {
  it("is not loaded until the first read answers, then shows the server's lists", async () => {
    const read = deferred<Record<string, string[]>>();
    getLists.mockReturnValue(read.promise);
    const { result } = renderHook(() => useKeyedSuggestedReplies(SOURCE));
    expect(result.current).toMatchObject({ lists: {}, loaded: false, loadFailed: false, saveFailedFor: new Set() });
    await act(async () => read.resolve({ mira: ["繼續"] }));
    expect(result.current).toMatchObject({
      lists: { mira: ["繼續"] },
      loaded: true,
      loadFailed: false,
      saveFailedFor: new Set(),
    });
  });

  it("a failed first read says so and stays not loaded; the next delta reads again and clears it", async () => {
    getLists.mockRejectedValueOnce(new Error("down"));
    const { result } = renderHook(() => useKeyedSuggestedReplies(SOURCE));
    await waitFor(() => expect(result.current.loadFailed).toBe(true));
    expect(result.current.loaded).toBe(false);
    emit("keyed_topic");
    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.lists).toEqual({ mira: ["繼續"] });
    expect(result.current.loadFailed).toBe(false);
  });

  it("reload reads again after a failed first read, and a second failure keeps saying so", async () => {
    getLists.mockRejectedValueOnce(new Error("down")).mockRejectedValueOnce(new Error("down"));
    const { result } = renderHook(() => useKeyedSuggestedReplies(SOURCE));
    await waitFor(() => expect(result.current.loadFailed).toBe(true));
    await act(async () => result.current.reload());
    expect(getLists).toHaveBeenCalledTimes(2);
    expect(result.current).toMatchObject({ loaded: false, loadFailed: true });
    await act(async () => result.current.reload());
    expect(getLists).toHaveBeenCalledTimes(3);
    expect(result.current).toMatchObject({ lists: { mira: ["繼續"] }, loaded: true, loadFailed: false });
  });

  it("a read failing after one has answered keeps the lists and is not a load failure", async () => {
    const { result } = await mounted();
    getLists.mockRejectedValueOnce(new Error("down"));
    emit("keyed_topic");
    await waitFor(() => expect(getLists).toHaveBeenCalledTimes(2));
    await act(async () => {});
    expect(result.current).toMatchObject({ lists: { mira: ["繼續"] }, loaded: true, loadFailed: false });
  });

  it("of two reads answering out of order, the newer one's answer stays", async () => {
    const { result } = await mounted();
    const older = deferred<Record<string, string[]>>();
    const newer = deferred<Record<string, string[]>>();
    getLists.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);
    emit("keyed_topic");
    emit("keyed_topic");
    await act(async () => newer.resolve({ mira: ["新"] }));
    await act(async () => older.resolve({ mira: ["舊"] }));
    expect(result.current.lists).toEqual({ mira: ["新"] });
  });

  it("re-reads on its own topic and on no other", async () => {
    const { result } = await mounted();
    emit("other_topic");
    expect(getLists).toHaveBeenCalledTimes(1);
    getLists.mockResolvedValue({ beto: ["好"] });
    emit("keyed_topic");
    await waitFor(() => expect(result.current.lists).toEqual({ beto: ["好"] }));
  });

  it("a save shows at once, keeps other keys' lists and writes only that key", async () => {
    const put = deferred<string[]>();
    putList.mockReturnValue(put.promise);
    const { result } = await mounted();
    act(() => result.current.save("beto", ["好"]));
    expect(result.current.lists).toEqual({ mira: ["繼續"], beto: ["好"] });
    expect(putList.mock.calls).toEqual([["beto", ["好"]]]);
    await act(async () => put.resolve(["好"]));
  });

  it("edits during a PUT wait and only the newest per key is sent", async () => {
    const first = deferred<string[]>();
    putList.mockReturnValueOnce(first.promise).mockResolvedValue([]);
    const { result } = await mounted();
    act(() => result.current.save("mira", ["a"]));
    act(() => result.current.save("mira", ["b"]));
    act(() => result.current.save("mira", ["c"]));
    expect(putList).toHaveBeenCalledTimes(1);
    await act(async () => first.resolve(["a"]));
    expect(putList.mock.calls).toEqual([
      ["mira", ["a"]],
      ["mira", ["c"]],
    ]);
  });

  it("a read answer sent before a save does not put the old list back", async () => {
    const { result } = await mounted();
    const stale = deferred<Record<string, string[]>>();
    getLists.mockReturnValueOnce(stale.promise);
    emit("keyed_topic");
    putList.mockResolvedValue(["新"]);
    await act(async () => result.current.save("mira", ["新"]));
    await act(async () => stale.resolve({ mira: ["舊"] }));
    expect(result.current.lists).toEqual({ mira: ["新"] });
  });

  it("a delta during a PUT is read once the PUT has settled", async () => {
    const put = deferred<string[]>();
    putList.mockReturnValue(put.promise);
    const { result } = await mounted();
    act(() => result.current.save("mira", ["新"]));
    emit("keyed_topic");
    expect(getLists).toHaveBeenCalledTimes(1);
    getLists.mockResolvedValue({ mira: ["新"], beto: ["別的視窗"] });
    await act(async () => put.resolve(["新"]));
    expect(getLists).toHaveBeenCalledTimes(2);
    await waitFor(() => expect(result.current.lists).toEqual({ mira: ["新"], beto: ["別的視窗"] }));
  });

  it("a failed save is that key's, shows what the server holds, and only that key's next save clears it", async () => {
    putList.mockRejectedValueOnce(new Error("no"));
    getLists.mockResolvedValueOnce({ mira: ["繼續"] }).mockResolvedValueOnce({ mira: ["伺服器上的"] });
    const { result } = await mounted();
    await act(async () => result.current.save("mira", ["新"]));
    expect(getLists).toHaveBeenCalledTimes(2);
    expect(result.current.lists).toEqual({ mira: ["伺服器上的"] });
    expect(result.current.saveFailedFor).toEqual(new Set(["mira"]));

    putList.mockResolvedValue(["好"]);
    await act(async () => result.current.save("ow-a", ["好"]));
    expect(result.current.saveFailedFor).toEqual(new Set(["mira"]));

    await act(async () => result.current.save("mira", ["再一次"]));
    expect(result.current.saveFailedFor).toEqual(new Set());
  });

  it("a failed save followed by a failed re-read goes back to the last list the server acknowledged", async () => {
    const { result } = await mounted();
    putList.mockResolvedValueOnce(["存過"]);
    await act(async () => result.current.save("mira", [" 存過 "]));
    putList.mockResolvedValueOnce(["也存過"]);
    await act(async () => result.current.save("beto", ["也存過"]));

    putList.mockRejectedValueOnce(new Error("offline"));
    getLists.mockRejectedValue(new Error("offline"));
    await act(async () => result.current.save("mira", ["沒存到"]));
    expect(result.current.lists).toEqual({ mira: ["存過"], beto: ["也存過"] });
    expect(result.current.saveFailedFor).toEqual(new Set(["mira"]));
  });

  it("the session's first save failing twice goes back to what the first read answered", async () => {
    const { result } = await mounted();
    putList.mockRejectedValueOnce(new Error("offline"));
    getLists.mockRejectedValue(new Error("offline"));
    await act(async () => result.current.save("mira", ["沒存到"]));
    expect(result.current.lists).toEqual({ mira: ["繼續"] });
    expect(result.current.saveFailedFor).toEqual(new Set(["mira"]));
  });

  it("dismissing a failed save clears that key only and leaves the lists", async () => {
    const { result } = await mounted();
    putList.mockRejectedValue(new Error("no"));
    await act(async () => result.current.save("mira", ["甲"]));
    await act(async () => result.current.save("beto", ["乙"]));
    await waitFor(() => expect(result.current.saveFailedFor).toEqual(new Set(["mira", "beto"])));

    act(() => result.current.dismissSaveFailed("mira"));
    expect(result.current.saveFailedFor).toEqual(new Set(["beto"]));
    expect(result.current.lists).toEqual({ mira: ["繼續"] });
  });

  it("the same double failure for a key the server held nothing for leaves it with no list", async () => {
    const { result } = await mounted();
    putList.mockRejectedValueOnce(new Error("offline"));
    getLists.mockRejectedValue(new Error("offline"));
    await act(async () => result.current.save("beto", ["沒存到"]));
    expect(result.current.lists).toEqual({ mira: ["繼續"] });
  });

  it("an edit queued behind a failed PUT stays on screen and its own success clears the failure", async () => {
    const first = deferred<string[]>();
    const second = deferred<string[]>();
    putList.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const { result } = await mounted();
    act(() => result.current.save("mira", ["a"]));
    act(() => result.current.save("mira", ["b"]));
    await act(async () => first.reject(new Error("no")));
    expect(putList.mock.calls).toEqual([
      ["mira", ["a"]],
      ["mira", ["b"]],
    ]);
    expect(result.current.lists).toEqual({ mira: ["b"] });
    expect(result.current.saveFailedFor).toEqual(new Set());
    getLists.mockResolvedValue({ mira: ["b"] });
    await act(async () => second.resolve(["b"]));
    expect(result.current.lists).toEqual({ mira: ["b"] });
    expect(result.current.saveFailedFor).toEqual(new Set());
  });
});
