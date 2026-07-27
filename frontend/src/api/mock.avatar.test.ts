import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  __injectMockOutsourceWorker,
  __resetMock,
  mockApi,
} from "./mock";

describe("mock personal-avatar mutation parity", () => {
  beforeEach(() => {
    __resetMock();
    vi.stubGlobal("URL", {
      createObjectURL: vi.fn(() => "blob:mock-avatar"),
      revokeObjectURL: vi.fn(),
    });
  });

  afterEach(() => vi.unstubAllGlobals());

  it("emits the same kind-specific refetch topics as the server", async () => {
    const seen: string[] = [];
    const unsubscribe = mockApi.subscribeEvents((topic) => seen.push(topic));
    const file = new File(["png"], "avatar.png", { type: "image/png" });

    await mockApi.updateMemberAvatar("mira", file);
    await mockApi.removeMemberAvatar("mira");
    expect(seen).toEqual(["member", "member"]);

    __injectMockOutsourceWorker({
      id: "ow-avatar",
      codename: "O-9",
      model: "claude-opus-4-8",
      effort: "high",
      status: "active",
      taskId: "t-avatar",
      taskTitle: "Avatar task",
      taskStatus: "in_progress",
      createdTs: 1,
    });
    seen.length = 0;
    await mockApi.updateMemberAvatar("ow-avatar", file);
    expect((await mockApi.getOutsourceWorker("ow-avatar")).avatarUrl).toBe(
      "blob:mock-avatar",
    );
    await mockApi.removeMemberAvatar("ow-avatar");
    expect(seen).toEqual(["outsource_worker", "outsource_worker"]);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:mock-avatar");

    unsubscribe();
  });
});
