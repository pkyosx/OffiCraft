// mock 換版交代單 — server parity (T-79).
//
// WHY THIS FILE EXISTS. The mock is what the cockpit runs against in offline
// preview and in every jsdom test, so a mock that is MORE GENEROUS than the
// server validates a broken card green. Each case here is one the mock could
// get wrong silently:
//
//  - hand-over ORDER (open first, oldest→newest) — get it wrong and the list
//    reads in a different order from the message the assistant is handed;
//  - `open_count` counted off the OPEN rows, not the array length;
//  - 🔴 THE FIRST TICK WINS — a mock that let the second tick overwrite
//    `done_by` would validate a card that reports the wrong person;
//  - the blank / over-long refusals, in RUNES, with the wire envelope;
//  - the body cap itself, read out of the Go source rather than copied.

import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { mockApi, __resetMock } from "./mock";
import { ApiError } from "./errors";

const API_GO = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  "server",
  "ocserverd",
  "api_upgrade_instructions.go",
);

async function refusalOf(p: Promise<unknown>): Promise<ApiError> {
  try {
    await p;
  } catch (e) {
    if (e instanceof ApiError) return e;
    throw new Error(`the mock threw a ${typeof e}, not the wire envelope: ${e}`);
  }
  throw new Error("the mock ACCEPTED what the server refuses");
}

describe("mock 換版交代單 — server parity", () => {
  beforeEach(() => __resetMock());

  it("serves the hand-over order: open first, each group oldest→newest", async () => {
    const { instructions, openCount } = await mockApi.getUpgradeInstructions();

    const opens = instructions.filter((i) => !i.done);
    const dones = instructions.filter((i) => i.done);
    expect(opens.length).toBeGreaterThan(0);
    expect(dones.length).toBeGreaterThan(0);

    // Every open row comes before every finished one.
    const lastOpenAt = instructions.map((i) => i.done).lastIndexOf(false);
    const firstDoneAt = instructions.map((i) => i.done).indexOf(true);
    expect(lastOpenAt).toBeLessThan(firstDoneAt);

    // And within the open group, oldest first — that is the order the owner
    // asked for them in, and the order she reads them in.
    const openTs = opens.map((i) => i.createdTs);
    expect([...openTs].sort((a, b) => a - b)).toEqual(openTs);

    // The count is the OPEN rows, not the array length.
    expect(openCount).toBe(opens.length);
    expect(openCount).not.toBe(instructions.length);
  });

  it("keeps a finished instruction in the list", async () => {
    // It is the only evidence the work was ever picked up, so the list is not
    // an open-only view. A mock that dropped them would validate a card that
    // makes "she did it" look like "it was never written".
    const { instructions } = await mockApi.getUpgradeInstructions();
    expect(instructions.some((i) => i.done)).toBe(true);
  });

  it("narrows an open instruction's done_ts/done_by to null, never 0 and \"\"", async () => {
    const created = await mockApi.createUpgradeInstruction("新的一張");
    expect(created.done).toBe(false);
    expect(created.doneTs).toBeNull();
    expect(created.doneBy).toBeNull();
    expect(created.id.startsWith("uin-")).toBe(true);
  });

  it("trims the body the way the server does, and refuses a blank one", async () => {
    const created = await mockApi.createUpgradeInstruction("  留白的兩側  ");
    expect(created.body).toBe("留白的兩側");

    for (const blank of ["", "   ", "\n\t "]) {
      const e = await refusalOf(mockApi.createUpgradeInstruction(blank));
      expect(e.status).toBe(422);
      expect(e.serverMessage).toContain("blank");
    }
  });

  it("counts the body cap in RUNES, at the number the Go source states", async () => {
    // 🔴 THE CAP IS A SECOND COPY, and this is what keeps the two honest. The
    // authority is upgradeInstructionBodyCap in api_upgrade_instructions.go;
    // reading it here means changing one and not the other goes RED instead of
    // letting the mock accept what the server refuses.
    const go = readFileSync(API_GO, "utf8");
    const m = go.match(/const upgradeInstructionBodyCap = (\d+)/);
    expect(m, "upgradeInstructionBodyCap not found in the Go source").toBeTruthy();
    const cap = Number(m![1]);

    await expect(
      mockApi.createUpgradeInstruction("a".repeat(cap)),
    ).resolves.toBeTruthy();

    const e = await refusalOf(
      mockApi.createUpgradeInstruction("a".repeat(cap + 1)),
    );
    expect(e.status).toBe(422);
    expect(e.serverMessage).toContain("too long");

    // Runes, not UTF-16 code units. An emoji is two code units and one rune;
    // a `.length` check would refuse this at half the real limit.
    await expect(
      mockApi.createUpgradeInstruction("🙂".repeat(cap)),
    ).resolves.toBeTruthy();
  });

  it("lets the FIRST tick win and answers the second with the row unchanged", async () => {
    const created = await mockApi.createUpgradeInstruction("要做的事");

    const first = await mockApi.markUpgradeInstructionDone(created.id);
    expect(first.done).toBe(true);
    expect(first.doneBy).toBe("mira");
    const firstTs = first.doneTs;
    expect(firstTs).not.toBeNull();

    // 🔴 A second tick is a 200 with NOTHING changed — not a 409. Two of the
    // assistant's sessions holding the same instruction is the ordinary case,
    // and a mock that overwrote done_by here would validate a card that
    // reports whoever ticked LAST as the person who did the work.
    const second = await mockApi.markUpgradeInstructionDone(created.id);
    expect(second.done).toBe(true);
    expect(second.doneBy).toBe("mira");
    expect(second.doneTs).toBe(firstTs);
  });

  it("drops a ticked instruction out of the open count but not out of the list", async () => {
    const created = await mockApi.createUpgradeInstruction("要做的事");
    const before = await mockApi.getUpgradeInstructions();
    await mockApi.markUpgradeInstructionDone(created.id);
    const after = await mockApi.getUpgradeInstructions();

    expect(after.openCount).toBe(before.openCount - 1);
    expect(after.instructions.some((i) => i.id === created.id)).toBe(true);
  });

  it("answers a withdraw with the row that was removed, then 404s on it", async () => {
    const created = await mockApi.createUpgradeInstruction("寫錯的一張");
    const removed = await mockApi.deleteUpgradeInstruction(created.id);
    // The caller asked for it by id and this is the last moment anyone can
    // read what it said.
    expect(removed.id).toBe(created.id);
    expect(removed.body).toBe("寫錯的一張");

    const { instructions } = await mockApi.getUpgradeInstructions();
    expect(instructions.some((i) => i.id === created.id)).toBe(false);

    const e = await refusalOf(mockApi.deleteUpgradeInstruction(created.id));
    expect(e.status).toBe(404);
  });

  it("404s on an id that names nothing, on both id-taking verbs", async () => {
    for (const call of [
      () => mockApi.markUpgradeInstructionDone("uin-nosuchrow00"),
      () => mockApi.deleteUpgradeInstruction("uin-nosuchrow00"),
    ]) {
      const e = await refusalOf(call());
      expect(e.status).toBe(404);
      expect(e.serverMessage).toContain("uin-nosuchrow00");
    }
  });
});
