import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import { __resetMock, mockApi } from "../api/mock";
import { useTaskManualSuggestedReplies } from "./useTaskManualSuggestedReplies";

describe("useTaskManualSuggestedReplies", () => {
  beforeEach(() => __resetMock());
  afterEach(() => __resetMock());

  it("shows every type's stored list once the first read answers", async () => {
    await mockApi.putTaskManualSuggestedReplies("builtin-role-design", ["照角色手冊"]);
    const { result } = renderHook(() => useTaskManualSuggestedReplies());
    await waitFor(() => expect(result.current.loaded).toBe(true));
    expect(result.current.lists).toEqual({ "builtin-role-design": ["照角色手冊"] });
  });

  it("follows another window's write and a manual delete that took the type's list with it", async () => {
    const created = await mockApi.createTaskManual("報價");
    const { result } = renderHook(() => useTaskManualSuggestedReplies());
    await waitFor(() => expect(result.current.loaded).toBe(true));

    await act(async () => {
      await mockApi.putTaskManualSuggestedReplies(created.typeKey, ["報價單已附"]);
    });
    await waitFor(() => expect(result.current.lists).toEqual({ [created.typeKey]: ["報價單已附"] }));

    await act(async () => {
      await mockApi.deleteTaskManual(created.typeKey);
    });
    await waitFor(() => expect(result.current.lists).toEqual({}));
  });

  it("a save shows at once and is what the server then holds", async () => {
    const { result } = renderHook(() => useTaskManualSuggestedReplies());
    await waitFor(() => expect(result.current.loaded).toBe(true));

    act(() => result.current.save("builtin-role-design", ["照角色手冊"]));
    expect(result.current.lists).toEqual({ "builtin-role-design": ["照角色手冊"] });
    await waitFor(async () =>
      expect(await mockApi.getTaskManualSuggestedReplies()).toEqual({ "builtin-role-design": ["照角色手冊"] })
    );
  });
});
