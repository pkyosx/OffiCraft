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
  loreType: "instruction_supplement",
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
  loreType: "owner_preference",
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
  loreType: "other",
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
  loreType: "other",
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
      entries: [{ ...L7, dropped: false }],
      limit: 30,
      offset: 0,
      total: 1,
      capChars: 25000,
      firstDroppedId: "",
      usedChars: 53,
      firstDroppedChars: 0,
      budgetSegments: [
        { scopeKind: "everyone", entryCount: 1, chars: 53, fittedCount: 1, fittedChars: 53 },
      ],
    });

    const mixed = await api.listLoreEntries({ scopeKinds: ["everyone", "agent"] });
    expect({ ...mixed, entries: mixed.entries.map((e) => e.id) }).toEqual({
      entries: ["L-1", "L-7", "L-5", "L-2", "L-3"],
      limit: 30,
      offset: 0,
      total: 5,
      capChars: 0,
      firstDroppedId: "",
      usedChars: 0,
      firstDroppedChars: 0,
      budgetSegments: [],
    });
  });

  it("a member cap that fits only the member's pinned entry drops 所有人's next one: the member's own line is at its next entry, the whole list's line at 所有人's", async () => {
    await api.patchServerSettings({ loreCapCharsRole: 195 });
    const segments = [
      { scopeKind: "everyone", entryCount: 1, chars: 53, fittedCount: 0, fittedChars: 0 },
      { scopeKind: "agent", entryCount: 2, chars: 244, fittedCount: 1, fittedChars: 195 },
    ];

    const page = await api.listLoreEntries({ scopeKind: "agent", scopeKey: "mira" });
    expect({ ...page, entries: page.entries.map((e) => [e.id, e.dropped]) }).toEqual({
      entries: [["L-1", false], ["L-2", true], ["L-3", undefined]],
      limit: 30,
      offset: 0,
      total: 3,
      capChars: 195,
      firstDroppedId: "L-2",
      usedChars: 195,
      firstDroppedChars: 49,
      budgetSegments: segments,
    });

    const whole = await api.listLoreEntries({ scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"] });
    expect({ ...whole, entries: whole.entries.map((e) => [e.id, e.dropped]) }).toEqual({
      entries: [["L-1", false], ["L-7", true], ["L-2", true], ["L-3", undefined]],
      limit: 30,
      offset: 0,
      total: 4,
      capChars: 195,
      firstDroppedId: "L-7",
      usedChars: 195,
      firstDroppedChars: 53,
      budgetSegments: segments,
    });

    const searched = await api.listLoreEntries({ scopeKinds: ["everyone", "agent"], scopeKeys: ["mira"], q: "零命中" });
    expect(searched.entries.map((e) => [e.id, e.dropped])).toEqual([["L-2", true]]);

    const alone = await api.listLoreEntries({ scopeKinds: ["everyone"] });
    expect({ ...alone, entries: alone.entries.map((e) => [e.id, e.dropped]) }).toEqual({
      entries: [["L-7", false]],
      limit: 30,
      offset: 0,
      total: 1,
      capChars: 195,
      firstDroppedId: "",
      usedChars: 53,
      firstDroppedChars: 0,
      budgetSegments: [
        { scopeKind: "everyone", entryCount: 1, chars: 53, fittedCount: 1, fittedChars: 53 },
      ],
    });
  });
});

describe("mock lore_type", () => {
  beforeEach(() => {
    vi.spyOn(Date, "now").mockReturnValue(1790000000000);
  });

  it("filters on loreTypes like the server: one type, two types, every type", async () => {
    const api = await freshMock();
    const ids = async (loreTypes: Parameters<typeof api.listLoreEntries>[0]) =>
      (await api.listLoreEntries(loreTypes)).entries.map((e) => [e.id, e.loreType]);
    const everyEntry = [
      ["L-1", "owner_decision"],
      ["L-7", "other"],
      ["L-5", "other"],
      ["L-4", "owner_preference"],
      ["L-6", "other"],
      ["L-2", "instruction_supplement"],
      ["L-3", "instruction_conflict"],
    ];
    expect(await ids({})).toEqual(everyEntry);
    expect(await ids({ loreTypes: ["owner_preference"] })).toEqual([["L-4", "owner_preference"]]);
    expect(await ids({ loreTypes: ["other", "instruction_conflict"] })).toEqual([
      ["L-7", "other"],
      ["L-5", "other"],
      ["L-6", "other"],
      ["L-3", "instruction_conflict"],
    ]);
    expect(
      await ids({
        loreTypes: [
          "instruction_conflict",
          "instruction_supplement",
          "owner_decision",
          "owner_preference",
          "other",
        ],
      }),
    ).toEqual(everyEntry);
  });

  it("an unknown lore type is a 400 on the list and on the write, and the write stores nothing", async () => {
    const api = await freshMock();
    expect(
      await refusal(api.listLoreEntries({ loreTypes: ["owner_whim" as never] })),
    ).toEqual({
      name: "ApiError",
      message: "http 400 for GET /api/lore",
      status: 400,
      code: "validation_error",
      serverMessage:
        'lore_types must be one of instruction_conflict, instruction_supplement, owner_decision, owner_preference or other — got "owner_whim"',
      retryAfter: null,
    });
    expect(
      await refusal(
        api.writeLoreEntry({ title: "t", body: "b", loreType: "owner_whim" as never }),
      ),
    ).toEqual({
      name: "ApiError",
      message: "http 400 for POST /api/lore",
      status: 400,
      code: "validation_error",
      serverMessage:
        'lore_type must be one of instruction_conflict, instruction_supplement, owner_decision, owner_preference or other — got "owner_whim"; nothing was written',
      retryAfter: null,
    });
    expect((await api.listLoreEntries()).entries).toHaveLength(7);
  });

  it("a write stores its lore type, and one without a type is refused with the five values", async () => {
    const api = await freshMock();
    await api.writeLoreEntry({ title: "有標籤", body: "b", loreType: "owner_decision" });
    expect(
      await refusal(api.writeLoreEntry({ title: "沒標籤", body: "b", loreType: "" as never })),
    ).toEqual({
      name: "ApiError",
      message: "http 400 for POST /api/lore",
      status: 400,
      code: "validation_error",
      serverMessage:
        "lore_type is required — nothing was written. Set it to the entry's type: " +
        "instruction_conflict (instruction conflict), instruction_supplement (instruction supplement), owner_decision (owner decision), " +
        "owner_preference (owner preference) or other. Put the type in lore_type, not as a prefix in the title.",
      retryAfter: null,
    });
    expect(
      (await refusal(api.writeLoreEntry({ title: "空白", body: "b", loreType: "  " as never })))
        .serverMessage,
    ).toBe(
      "lore_type is required — nothing was written. Set it to the entry's type: " +
        "instruction_conflict (instruction conflict), instruction_supplement (instruction supplement), owner_decision (owner decision), " +
        "owner_preference (owner preference) or other. Put the type in lore_type, not as a prefix in the title.",
    );
    const written = (await api.listLoreEntries()).entries.filter((e) => e.seq > 7);
    expect(written.map((e) => [e.id, e.title, e.loreType])).toEqual([["L-8", "有標籤", "owner_decision"]]);
  });
});

describe("mock listLoreBudgets", () => {
  let api: Awaited<ReturnType<typeof freshMock>>;
  beforeEach(async () => {
    api = await freshMock();
  });

  const target = (scopeKind: string, scopeKey: string, usedChars: number, entryCount: number, droppedCount = 0, capChars = 25000) => ({
    scopeKind,
    scopeKey,
    capChars,
    usedChars,
    entryCount,
    droppedCount,
  });

  it("answers 所有人, every staff and outsource member and every manual, a member counting 所有人 too", async () => {
    expect(await api.listLoreBudgets()).toEqual([
      target("everyone", "", 53, 1),
      target("agent", "mira", 297, 3),
      target("agent", "ow-7d8ad859dd9b", 112, 2),
      target("manual", "builtin-remote-access", 0, 0),
      target("manual", "builtin-role-design", 0, 0),
      target("manual", "builtin-task-manual-design", 0, 0),
    ]);
  });

  it("agrees with the list's numbers when the member cap leaves 所有人's entry and Mira's older one out", async () => {
    await api.patchServerSettings({ loreCapCharsRole: 195 });
    const mira = (await api.listLoreBudgets()).find((t) => t.scopeKey === "mira");
    expect(mira).toEqual(target("agent", "mira", 195, 3, 2, 195));
  });
});

describe("mock editLoreEntry", () => {
  let api: Awaited<ReturnType<typeof freshMock>>;
  beforeEach(async () => {
    api = await freshMock();
  });

  it("changes only the fields sent and leaves id, scope, state and order alone", async () => {
    await api.editLoreEntry("L-2", { body: "先跑陽性對照。", loreType: "owner_decision" });

    const page = await api.listLoreEntries({ scopeKind: "agent", scopeKey: "mira" });
    expect(page.entries.map((e) => e.id)).toEqual(["L-1", "L-2", "L-3"]);
    const { updatedTs, ...rest } = page.entries[1];
    const { updatedTs: wasTs, ...l2 } = L2;
    expect(rest).toEqual({ ...l2, body: "先跑陽性對照。", loreType: "owner_decision", dropped: false });
    expect(updatedTs).toBeGreaterThan(wasTs);
  });

  it("a shorter 所有人 body makes the member's entry it dropped readable", async () => {
    await api.patchServerSettings({ loreCapCharsRole: 270 });
    const read = async () => {
      const page = await api.listLoreEntries({ scopeKind: "agent", scopeKey: "mira" });
      return [page.firstDroppedId, page.usedChars, page.firstDroppedChars];
    };
    expect(await read()).toEqual(["L-2", 248, 49]);

    await api.editLoreEntry("L-7", { body: "送出前讀一遍。" });

    expect(await read()).toEqual(["", 264, 0]);
  });

  it("refuses an empty change, a blank title, an over-cap body, an unknown type and an unknown id, and stores nothing", async () => {
    const before = await entry(api, "L-2");
    expect(await refusal(api.editLoreEntry("L-2", {}))).toMatchObject({ status: 400 });
    expect(await refusal(api.editLoreEntry("L-2", { title: "  " }))).toMatchObject({
      status: 400,
      serverMessage: "title must not be blank",
    });
    expect(await refusal(api.editLoreEntry("L-2", { body: "長".repeat(1251) }))).toMatchObject({
      status: 400,
      serverMessage: "body is over the 1250-character cap",
    });
    expect(
      await refusal(api.editLoreEntry("L-2", { loreType: "nope" as never })),
    ).toMatchObject({ status: 400 });
    expect(await refusal(api.editLoreEntry("L-999", { title: "x" }))).toMatchObject({
      status: 404,
    });
    expect(await entry(api, "L-2")).toEqual(before);
  });
});

describe("mock lore_entry version history", () => {
  let api: Awaited<ReturnType<typeof freshMock>>;
  beforeEach(async () => {
    api = await freshMock();
  });

  async function retained() {
    const rows = await api.listDocumentHistory("lore_entry", "L-2");
    return Promise.all(
      rows.map(async (r) => (await api.getDocumentRevision("lore_entry", "L-2", r.id)).content),
    );
  }

  it("an edit that changes the text retains the title and body it replaced; a type-only edit retains nothing", async () => {
    await api.editLoreEntry("L-2", { loreType: "other" });
    expect(await retained()).toEqual([]);

    await api.editLoreEntry("L-2", { title: "新標題" });

    expect(await retained()).toEqual([{ title: L2.title, body: L2.body }]);
  });

  it("keeps the newest three", async () => {
    for (const n of [1, 2, 3, 4]) await api.editLoreEntry("L-2", { body: `第 ${n} 版` });

    expect(await retained()).toEqual([
      { title: L2.title, body: "第 3 版" },
      { title: L2.title, body: "第 2 版" },
      { title: L2.title, body: "第 1 版" },
    ]);
  });

  it("a restore puts title and body back, leaves type and state alone, and retains what it overwrote", async () => {
    await api.editLoreEntry("L-2", { title: "新標題", body: "新內容", loreType: "owner_decision" });
    const [version] = await api.listDocumentHistory("lore_entry", "L-2");

    await api.restoreDocumentHistory("lore_entry", "L-2", version.id);

    const e = await entry(api, "L-2");
    expect([e.title, e.body, e.loreType, e.state]).toEqual([L2.title, L2.body, "owner_decision", "active"]);
    expect(await retained()).toEqual([
      { title: "新標題", body: "新內容" },
      { title: L2.title, body: L2.body },
    ]);
  });

  it("the list filters on whole entry ids", async () => {
    const page = await api.listLoreEntries({ entryIds: ["L-2", "L-7"] });
    expect(page.entries.map((x) => x.id)).toEqual(["L-7", "L-2"]);
    expect((await api.listLoreEntries({ entryIds: ["L-"] })).entries).toEqual([]);
  });
});
