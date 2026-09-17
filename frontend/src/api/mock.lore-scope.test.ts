// Mock adapter parity for set_lore_entry_scope and the 所有人 scope (T-236).

import { describe, it, expect, beforeEach, vi } from "vitest";

async function freshMock() {
  vi.resetModules();
  return (await import("./mock")).mockApi;
}

const L2 = {
  id: "L-2",
  seq: 2,
  scopeKind: "agent",
  scopeKey: "mira",
  taskTypeKey: "",
  scopeOptions: ["agent", "everyone"],
  title: "零命中的預設解讀是查法寫錯了",
  body: "掃描回空的時候先跑一次陽性對照，確認量具本身還會命中，再去解釋那個零。",
  authorId: "mira",
  sourceTaskId: "",
  state: "active",
  retireReason: "",
  effectiveTs: 1788400000,
  createdTs: 1788400000,
  updatedTs: 1788400000,
};
const L4 = {
  id: "L-4",
  seq: 4,
  scopeKind: "manual",
  scopeKey: "tm-mock",
  taskTypeKey: "tm-mock",
  scopeOptions: ["manual", "agent", "everyone"],
  title: "手冊傳承不進任何人的開機檔",
  body: "這一類條目只在讀那本手冊的時候拿得到，正職與外包一視同仁。",
  authorId: "mira",
  sourceTaskId: "T-1",
  state: "active",
  retireReason: "",
  effectiveTs: 1788450000,
  createdTs: 1788450000,
  updatedTs: 1788450000,
};
const L5 = {
  id: "L-5",
  seq: 5,
  scopeKind: "agent",
  scopeKey: "ow-7d8ad859dd9b",
  taskTypeKey: "tm-mock",
  scopeOptions: ["manual", "agent", "everyone"],
  title: "交接路徑要寫絕對路徑",
  body: "留給下一個人的路徑一律寫絕對路徑——對方在別的工作目錄下撲空，得到的訊息跟那一輪根本沒跑一模一樣。",
  authorId: "ow-7d8ad859dd9b",
  sourceTaskId: "",
  state: "active",
  retireReason: "",
  effectiveTs: 1788460000,
  createdTs: 1788460000,
  updatedTs: 1788460000,
};
const L7 = {
  id: "L-7",
  seq: 7,
  scopeKind: "everyone",
  scopeKey: "",
  taskTypeKey: "",
  scopeOptions: ["agent", "everyone"],
  title: "回報前先讀一次自己寫的東西",
  body: "送出前從頭讀一遍：錯字、漏掉的編號、貼錯的路徑，都是讀的人要多花一輪來回的地方。",
  authorId: "mira",
  sourceTaskId: "",
  state: "active",
  retireReason: "",
  effectiveTs: 1788470000,
  createdTs: 1788470000,
  updatedTs: 1788470000,
};

async function entry(api: Awaited<ReturnType<typeof freshMock>>, id: string) {
  const e = (await api.listLoreEntries()).entries.find((x) => x.id === id);
  if (!e) throw new Error(`no ${id} in the page`);
  return e;
}

async function refusal(p: Promise<unknown>) {
  try {
    await p;
  } catch (e) {
    const x = e as Record<string, unknown>;
    return {
      name: x.name,
      message: x.message,
      status: x.status,
      code: x.code,
      serverMessage: x.serverMessage,
      retryAfter: x.retryAfter,
    };
  }
  throw new Error("expected a refusal");
}

describe("mock setLoreEntryScope", () => {
  let api: Awaited<ReturnType<typeof freshMock>>;
  beforeEach(async () => {
    api = await freshMock();
  });

  it("a fresh mock serves the task-bound, outsource, staff and everyone fixture rows", async () => {
    expect(await entry(api, "L-4")).toEqual(L4);
    expect(await entry(api, "L-5")).toEqual(L5);
    expect(await entry(api, "L-2")).toEqual(L2);
    expect(await entry(api, "L-7")).toEqual(L7);
  });

  it("each target kind moves the entry under the key derived from the entry", async () => {
    await api.setLoreEntryScope("L-5", "manual");
    expect(await entry(api, "L-5")).toEqual({
      ...L5,
      scopeKind: "manual",
      scopeKey: "tm-mock",
      updatedTs: expect.any(Number),
    });

    await api.setLoreEntryScope("L-5", "everyone");
    expect(await entry(api, "L-5")).toEqual({
      ...L5,
      scopeKind: "everyone",
      scopeKey: "",
      updatedTs: expect.any(Number),
    });

    await api.setLoreEntryScope("L-5", "agent");
    expect(await entry(api, "L-5")).toEqual({
      ...L5,
      updatedTs: expect.any(Number),
    });
  });

  it("manual for an entry with no task type is a 400 and the entry stays where it was", async () => {
    expect(await refusal(api.setLoreEntryScope("L-2", "manual"))).toEqual({
      name: "ApiError",
      message: "http 400 for POST /api/lore/L-2/scope",
      status: 400,
      code: "validation_error",
      serverMessage:
        "lore entry L-2 has no task type to key a manual scope to — its source task " +
        "carries no type, or it has no source task and its author is not an " +
        "outsource member bound to a typed task",
      retryAfter: null,
    });
    expect(await entry(api, "L-2")).toEqual(L2);
  });

  it("an unknown entry id is a 404", async () => {
    expect(await refusal(api.setLoreEntryScope("L-999", "everyone"))).toEqual({
      name: "ApiError",
      message: "http 404 for POST /api/lore/L-999/scope",
      status: 404,
      code: "not_found",
      serverMessage: "no such lore entry: L-999",
      retryAfter: null,
    });
  });

  it("filtering only 所有人 returns its rows with the member cap; mixing kinds reports no cap", async () => {
    expect(await api.listLoreEntries({ scopeKinds: ["everyone"] })).toEqual({
      entries: [L7],
      limit: 30,
      offset: 0,
      capChars: 10000,
      firstDroppedId: "",
    });

    const mixed = await api.listLoreEntries({ scopeKinds: ["everyone", "agent"] });
    expect({ ...mixed, entries: mixed.entries.map((e) => e.id) }).toEqual({
      entries: ["L-1", "L-7", "L-5", "L-2", "L-3"],
      limit: 30,
      offset: 0,
      capChars: 0,
      firstDroppedId: "",
    });
  });

  it("a member cap that fits only the member's own entry drops it once 所有人 entries come first", async () => {
    await api.patchServerSettings({ loreCapCharsRole: 195 });

    const page = await api.listLoreEntries({ scopeKind: "agent", scopeKey: "mira" });
    expect({ ...page, entries: page.entries.map((e) => e.id) }).toEqual({
      entries: ["L-1", "L-2", "L-3"],
      limit: 30,
      offset: 0,
      capChars: 195,
      firstDroppedId: "L-1",
    });

    const alone = await api.listLoreEntries({ scopeKinds: ["everyone"] });
    expect({ ...alone, entries: alone.entries.map((e) => e.id) }).toEqual({
      entries: ["L-7"],
      limit: 30,
      offset: 0,
      capChars: 195,
      firstDroppedId: "",
    });
  });
});
