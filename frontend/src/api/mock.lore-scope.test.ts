// Mock adapter parity for set_lore_entry_scope and the 所有人 scope (T-236).

import { describe, it, expect, beforeEach, vi } from "vitest";

async function freshMock() {
  vi.resetModules();
  return (await import("./mock")).mockApi;
}

function row(
  entries: { id: string; scopeKind: string; scopeKey: string; scopeOptions: string[] }[],
  id: string,
) {
  const e = entries.find((x) => x.id === id);
  if (!e) throw new Error(`no ${id} in the page`);
  return [e.scopeKind, e.scopeKey, e.scopeOptions];
}

describe("mock setLoreEntryScope", () => {
  let api: Awaited<ReturnType<typeof freshMock>>;
  beforeEach(async () => {
    api = await freshMock();
  });

  it("serves the three fixture scenarios with the server's option lists", async () => {
    const { entries } = await api.listLoreEntries();
    expect(row(entries, "L-4")).toEqual(["manual", "tm-mock", ["manual", "agent", "everyone"]]);
    expect(row(entries, "L-5")).toEqual([
      "agent",
      "ow-7d8ad859dd9b",
      ["manual", "agent", "everyone"],
    ]);
    expect(row(entries, "L-2")).toEqual(["agent", "mira", ["agent", "everyone"]]);
    expect(row(entries, "L-7")).toEqual(["everyone", "", ["agent", "everyone"]]);
  });

  it("derives the key from the entry for each target kind", async () => {
    expect(await api.setLoreEntryScope("L-5", "manual")).toMatchObject({
      id: "L-5",
      scopeKind: "manual",
      scopeKey: "tm-mock",
    });
    expect(row((await api.listLoreEntries()).entries, "L-5").slice(0, 2)).toEqual([
      "manual",
      "tm-mock",
    ]);

    await api.setLoreEntryScope("L-5", "everyone");
    expect(row((await api.listLoreEntries()).entries, "L-5").slice(0, 2)).toEqual([
      "everyone",
      "",
    ]);

    await api.setLoreEntryScope("L-5", "agent");
    expect(row((await api.listLoreEntries()).entries, "L-5").slice(0, 2)).toEqual([
      "agent",
      "ow-7d8ad859dd9b",
    ]);
  });

  it("refuses manual for an entry with no task type and leaves it where it was", async () => {
    await expect(api.setLoreEntryScope("L-2", "manual")).rejects.toMatchObject({
      status: 400,
      serverMessage:
        "lore entry L-2 has no task type to key a manual scope to — its source task " +
        "carries no type, or it has no source task and its author is not an " +
        "outsource member bound to a typed task",
    });
    expect(row((await api.listLoreEntries()).entries, "L-2").slice(0, 2)).toEqual([
      "agent",
      "mira",
    ]);
  });

  it("answers 404 for an entry that does not exist", async () => {
    await expect(api.setLoreEntryScope("L-999", "everyone")).rejects.toMatchObject({
      status: 404,
      serverMessage: "no such lore entry: L-999",
    });
  });

  it("filters 所有人 without a key and reports the member cap for it", async () => {
    const page = await api.listLoreEntries({ scopeKinds: ["everyone"] });
    expect(page.entries.map((e) => e.id)).toEqual(["L-7"]);
    expect(page.capChars).toBe(10000);
    expect(page.firstDroppedId).toBe("");

    const mixed = await api.listLoreEntries({ scopeKinds: ["everyone", "agent"] });
    expect(mixed.capChars).toBe(0);
  });

  it("draws a member's agent line after the everyone entries that share its cap", async () => {
    // The cap fits Mira's pinned L-1 on its own, but not after L-7 (所有人).
    const own = (await api.listLoreEntries({ scopeKind: "agent", scopeKey: "mira" })).entries;
    const l1 = own.find((e) => e.id === "L-1")!;
    const cost = [...l1.title].length + [...l1.body].length;
    await api.patchServerSettings({ loreCapCharsRole: cost });

    const page = await api.listLoreEntries({ scopeKind: "agent", scopeKey: "mira" });
    expect(page.capChars).toBe(cost);
    expect(page.firstDroppedId).toBe("L-1");

    const alone = await api.listLoreEntries({ scopeKinds: ["everyone"] });
    expect(alone.firstDroppedId).toBe("");
  });
});
