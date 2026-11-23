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

beforeEach(() => {
  listeners.length = 0;
  getChatSuggestedReplies.mockReset().mockResolvedValue({ mira: ["繼續"] });
  putChatSuggestedReplies.mockReset();
});

describe("useChatSuggestedReplies", () => {
  it("shows every member's chat list, re-read on a chat_suggested_reply delta and on no other", async () => {
    const { result } = renderHook(() => useChatSuggestedReplies());
    await waitFor(() => expect(result.current.lists).toEqual({ mira: ["繼續"] }));
    getChatSuggestedReplies.mockResolvedValue({ beto: ["好"] });
    emit("task_manual_suggested_reply");
    await act(async () => {});
    expect(result.current.lists).toEqual({ mira: ["繼續"] });
    emit("chat_suggested_reply");
    await waitFor(() => expect(result.current.lists).toEqual({ beto: ["好"] }));
  });

  it("a save writes that member's chat list", async () => {
    putChatSuggestedReplies.mockResolvedValue(["好"]);
    const { result } = renderHook(() => useChatSuggestedReplies());
    await waitFor(() => expect(result.current.loaded).toBe(true));
    await act(async () => result.current.save("beto", ["好"]));
    expect(putChatSuggestedReplies.mock.calls).toEqual([["beto", ["好"]]]);
    expect(result.current.lists).toEqual({ mira: ["繼續"], beto: ["好"] });
  });
});
