import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  __injectMockOutsourceWorker,
  __resetMock,
  mockApi,
} from "./mock";
import { themeIconId } from "../lib/themeIconId";

const png = (tail: number) =>
  "data:image/png;base64," +
  btoa(String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, tail));

async function installThemes(active: string, pools: Record<string, string[]>) {
  for (const [id, images] of Object.entries(pools)) {
    await mockApi.putTheme({
      id,
      name: id,
      colors: { "--color-bg": "#000000" },
      avatarPools: { member: images.map((image) => ({ image })) },
    });
  }
  await mockApi.patchServerSettings({ displayTheme: active });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("mock theme-avatar parity", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("emits the same kind-specific refetch topics as the server", async () => {
    await installThemes("portraits", { portraits: [png(1), png(2)] });
    const chosen = await themeIconId(png(2));
    const seen: string[] = [];
    const unsubscribe = mockApi.subscribeEvents((topic) => seen.push(topic));

    await mockApi.setMemberThemeAvatar("mira", "portraits", chosen);
    expect((await mockApi.getMember("mira")).avatarIconId).toBe(chosen);
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
    await mockApi.setMemberThemeAvatar("ow-avatar", "portraits", chosen);
    expect(seen).toEqual(["outsource_worker"]);

    unsubscribe();
  });

  // The headline requirement: a choice in theme A survives a trip through
  // theme B and back, and neither overwrites the other.
  it("keeps one choice per theme and restores it on switching back", async () => {
    await installThemes("alpha", { alpha: [png(1), png(2)], beta: [png(3), png(4)] });
    const inAlpha = await themeIconId(png(2));
    const inBeta = await themeIconId(png(3));

    await mockApi.setMemberThemeAvatar("mira", "alpha", inAlpha);
    await mockApi.setMemberThemeAvatar("mira", "beta", inBeta);

    await mockApi.patchServerSettings({ displayTheme: "beta" });
    expect((await mockApi.getMember("mira")).avatarIconId).toBe(inBeta);
    await mockApi.patchServerSettings({ displayTheme: "alpha" });
    expect((await mockApi.getMember("mira")).avatarIconId).toBe(inAlpha);
  });

  // A new member has chosen nothing, and nothing writes a default for it.
  it("leaves a fresh member with no recorded choice", async () => {
    await installThemes("portraits", { portraits: [png(1), png(2), png(3)] });
    const created = await mockApi.createRole({ name: "Pool role" });
    expect(created.member.avatarIconId).toBeNull();
  });

  it("refuses an icon the named theme's pool cannot resolve", async () => {
    await installThemes("portraits", { portraits: [png(1)] });
    await expect(
      mockApi.setMemberThemeAvatar("mira", "portraits", "icn-nope"),
    ).rejects.toThrow();
    await expect(
      mockApi.setMemberThemeAvatar("mira", "missing", await themeIconId(png(1))),
    ).rejects.toThrow();
  });

  // Removing ANOTHER image must not move this member; removing the CHOSEN one
  // drops the association rather than rebinding it to a neighbour.
  it("drops a selection whose image was removed, and moves nobody else", async () => {
    await installThemes("portraits", { portraits: [png(1), png(2)] });
    const kept = await themeIconId(png(1));
    const dropped = await themeIconId(png(2));
    __injectMockOutsourceWorker({
      id: "ow-loser",
      codename: "O-1",
      model: "claude-opus-4-8",
      effort: "high",
      status: "active",
      taskId: "t-1",
      taskTitle: "Task",
      taskStatus: "in_progress",
      createdTs: 1,
    });
    await mockApi.setMemberThemeAvatar("mira", "portraits", kept);
    await mockApi.setMemberThemeAvatar("ow-loser", "portraits", dropped);

    await installThemes("portraits", { portraits: [png(1)] });
    // The member whose image survived does not move; the one whose image was
    // removed falls back rather than inheriting its neighbour's face.
    expect((await mockApi.getMember("mira")).avatarIconId).toBe(kept);
    expect(
      (await mockApi.getOutsourceWorker("ow-loser")).avatarIconId,
    ).toBeNull();
  });

  it("drops the selections of a deleted theme and keeps the others", async () => {
    await installThemes("alpha", { alpha: [png(1)], beta: [png(3)] });
    const inAlpha = await themeIconId(png(1));
    const inBeta = await themeIconId(png(3));
    await mockApi.setMemberThemeAvatar("mira", "alpha", inAlpha);
    await mockApi.setMemberThemeAvatar("mira", "beta", inBeta);

    await mockApi.patchServerSettings({ displayTheme: "beta" });
    await mockApi.deleteTheme("alpha");
    expect((await mockApi.getMember("mira")).avatarIconId).toBe(inBeta);
    // Recreate alpha under the SAME id: its old selections must not come back,
    // or a reused id would resurrect a stranger's face.
    await mockApi.putTheme({
      id: "alpha", name: "alpha", colors: { "--color-bg": "#000000" },
      avatarPools: { member: [{ image: png(1) }] },
    });
    await mockApi.patchServerSettings({ displayTheme: "alpha" });
    // alpha was deleted and recreated with the same id; its old selection must
    // NOT come back, or a reused id would resurrect a stranger's face.
    expect((await mockApi.getMember("mira")).avatarIconId).toBeNull();
  });
});
