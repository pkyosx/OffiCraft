import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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

afterEach(() => {
  vi.restoreAllMocks();
});

describe("mock avatar-index creation parity", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("assigns a founding staff member from the active theme pool", async () => {
    const png = (tail: number) =>
      "data:image/png;base64," +
      btoa(
        String.fromCharCode(
          0x89,
          0x50,
          0x4e,
          0x47,
          0x0d,
          0x0a,
          0x1a,
          0x0a,
          tail,
        ),
      );
    await mockApi.patchServerSettings({
      customThemes: [{
        id: "portraits",
        name: "Portraits",
        colors: { "--color-bg": "#000000" },
        avatarPools: { member: [png(1), png(2), png(3)] },
      }],
      displayTheme: "portraits",
    });
    vi.spyOn(Math, "random").mockReturnValue(0.75);

    const created = await mockApi.createRole({ name: "Pool role" });
    expect(created.member.avatarIndex).toBe(2);
  });
});
