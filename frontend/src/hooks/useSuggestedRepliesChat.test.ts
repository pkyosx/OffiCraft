import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";

type Listener = (topic: string) => void;
const listeners: Listener[] = [];

vi.mock("../api", () => ({
  api: {
    subscribeEvents: (cb: Listener) => {
      listeners.push(cb);
      return () => listeners.splice(listeners.indexOf(cb), 1);
    },
  },
}));

import { useSuggestedRepliesChat, useSuggestedRepliesLoreMessage } from "./useSuggestedReplies";
import * as shared from "./sharedServerSettings";

const settings = (over: object) => over as Awaited<ReturnType<typeof shared.loadServerSettings>>;
const emit = (topic: string) => act(() => listeners.forEach((cb) => cb(topic)));

describe("useSuggestedRepliesChat", () => {
  beforeEach(() => {
    listeners.length = 0;
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(shared, "loadServerSettings").mockResolvedValue(settings({ suggestedRepliesChat: ["繼續"] }));
  });
  afterEach(() => vi.restoreAllMocks());

  it("reads its own list and a settings delta from another window brings the new one in", async () => {
    const refresh = vi
      .spyOn(shared, "refreshServerSettings")
      .mockResolvedValue(settings({ suggestedRepliesChat: ["先放著"] }));
    const { result } = renderHook(() => useSuggestedRepliesChat());
    await waitFor(() => expect(result.current).toEqual(["繼續"]));
    expect(refresh).not.toHaveBeenCalled();

    emit("settings");
    await waitFor(() => expect(result.current).toEqual(["先放著"]));
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("re-reads on no other topic", async () => {
    const refresh = vi.spyOn(shared, "refreshServerSettings");
    const { result } = renderHook(() => useSuggestedRepliesChat());
    await waitFor(() => expect(result.current).toEqual(["繼續"]));
    emit("chat_suggested_reply");
    emit("member");
    expect(refresh).not.toHaveBeenCalled();
  });

  it("a failed refresh keeps the sentences on screen", async () => {
    vi.spyOn(shared, "refreshServerSettings").mockRejectedValue(new Error("down"));
    const { result } = renderHook(() => useSuggestedRepliesChat());
    await waitFor(() => expect(result.current).toEqual(["繼續"]));
    emit("settings");
    await act(async () => {});
    expect(result.current).toEqual(["繼續"]);
  });

  it("the other lists still ignore a settings delta", async () => {
    const refresh = vi.spyOn(shared, "refreshServerSettings");
    vi.spyOn(shared, "loadServerSettings").mockResolvedValue(settings({ suggestedRepliesLoreMessage: ["傳承"] }));
    const { result } = renderHook(() => useSuggestedRepliesLoreMessage());
    await waitFor(() => expect(result.current).toEqual(["傳承"]));
    expect(listeners).toEqual([]);
    expect(refresh).not.toHaveBeenCalled();
  });
});
