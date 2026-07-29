import { beforeEach, describe, expect, it } from "vitest";
import {
  __injectMockOutsourceWorker,
  __resetMock,
  mockApi,
} from "./mock";

describe("mock avatar-index mutation parity", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("emits the same kind-specific refetch topics as the server", async () => {
    const seen: string[] = [];
    const unsubscribe = mockApi.subscribeEvents((topic) => seen.push(topic));
    await mockApi.updateMemberAvatarIndex("mira", 2);
    expect((await mockApi.getMember("mira")).avatarIndex).toBe(2);
    expect(seen).toEqual(["member"]);

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
    await mockApi.updateMemberAvatarIndex("ow-avatar", 4);
    expect((await mockApi.getOutsourceWorker("ow-avatar")).avatarIndex).toBe(4);
    expect(seen).toEqual(["outsource_worker"]);

    unsubscribe();
  });
});
