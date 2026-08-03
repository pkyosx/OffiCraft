// lib/deltaSink — the coalescing rule, on its own. The cockpit-level cost is
// pinned in hooks/sseFanout.test.tsx; this file pins the two edges that are easy
// to get wrong and invisible from up there: what counts as ONE burst, and the
// three-way difference between "named nothing", "named someone else" and "named
// something I hold".

import { describe, it, expect } from "vitest";
import type { SseDelta } from "../api/adapter";
import { createDeltaSink, narrowToHeld, type DeltaBatch } from "./deltaSink";

function delta(topic: string, names: SseDelta["names"] = {}): SseDelta {
  return {
    topic,
    names,
    ids: Object.values(names).filter((v): v is string => typeof v === "string"),
  };
}

const flush = () => new Promise<void>((r) => queueMicrotask(() => r()));

describe("createDeltaSink", () => {
  it("folds a SYNCHRONOUS fan of many topics into one batch", async () => {
    const batches: DeltaBatch[] = [];
    const sink = createDeltaSink((b) => batches.push(b));

    // This is resyncAll's shape: 13 topics, one after another, no awaits.
    for (const t of ["member", "chat", "chat_read", "task"]) sink(t, delta(t));
    expect(batches).toHaveLength(0); // nothing runs mid-fan
    await flush();

    expect(batches).toHaveLength(1);
    expect([...batches[0].topics].sort()).toEqual([
      "chat",
      "chat_read",
      "member",
      "task",
    ]);
  });

  it("does NOT fold across ticks — coalescing is not a debounce", async () => {
    const batches: DeltaBatch[] = [];
    const sink = createDeltaSink((b) => batches.push(b));

    sink("chat", delta("chat", { id: "cm-1" }));
    await flush();
    sink("chat", delta("chat", { id: "cm-2" }));
    await flush();

    // Two separate messages are two separate answers; delaying the second one
    // would be a deliberate lag on the screen.
    expect(batches).toHaveLength(2);
    expect([...batches[0].ids]).toEqual(["cm-1"]);
    expect([...batches[1].ids]).toEqual(["cm-2"]);
  });

  it("a delta arriving WHILE the handler runs starts the next batch", async () => {
    const seen: string[][] = [];
    let sink!: (topic: string, d?: SseDelta) => void;
    sink = createDeltaSink((b) => {
      seen.push([...b.topics]);
      if (seen.length === 1) sink("task", delta("task", { id: "t-1" }));
    });

    sink("chat", delta("chat", { id: "cm-1" }));
    await flush();
    await flush();

    // Folding it into the batch being acted on would drop it entirely.
    expect(seen).toEqual([["chat"], ["task"]]);
  });

  it("marks a batch unnamed when ANY delta named nothing", async () => {
    const batches: DeltaBatch[] = [];
    const sink = createDeltaSink((b) => batches.push(b));
    sink("chat", delta("chat", { id: "cm-1" }));
    sink("lessons", delta("lessons")); // payload null on the wire
    await flush();
    expect(batches[0].unnamed).toBe(true);
  });

  it("marks a batch unnamed when the transport supplies NO delta at all", async () => {
    // The mock adapter (and any older producer) calls back with the topic only.
    // Reading that as "nothing changed" would freeze the mock cockpit.
    const batches: DeltaBatch[] = [];
    const sink = createDeltaSink((b) => batches.push(b));
    sink("reply_card");
    await flush();
    expect(batches[0].unnamed).toBe(true);
    expect(batches[0].topics.has("reply_card")).toBe(true);
  });

  it("keeps the deltas so a consumer can read a name's ROLE, not just its value", async () => {
    const batches: DeltaBatch[] = [];
    const sink = createDeltaSink((b) => batches.push(b));
    sink("chat_read", delta("chat_read", { reader: "owner", peer: "m-1" }));
    await flush();
    // "owner read m-1" and "m-1 read owner" have the SAME id set — only the
    // roles tell them apart, which is what useChat's echo gate turns on.
    expect(batches[0].deltas[0].names.reader).toBe("owner");
    expect([...batches[0].ids].sort()).toEqual(["m-1", "owner"]);
  });
});

describe("narrowToHeld", () => {
  const batchOf = (d: SseDelta[], unnamed = false): DeltaBatch => ({
    topics: new Set(d.map((x) => x.topic)),
    ids: new Set(d.flatMap((x) => x.ids)),
    deltas: d,
    unnamed,
  });

  it("returns null for an unnamed batch — only a full refetch answers it", () => {
    expect(narrowToHeld(batchOf([delta("chat")], true), () => true)).toBeNull();
  });

  it("returns the held ids the batch named", () => {
    const b = batchOf([delta("chat", { id: "cm-1", from: "m-2", to: "owner" })]);
    expect(narrowToHeld(b, (id) => id === "m-2")).toEqual(["m-2"]);
  });

  it("returns an EMPTY array (not null) when it named only ids we do not hold", () => {
    // The distinction matters: empty means "nothing of mine changed", which a
    // consumer whose membership cannot change on this topic may act on, while
    // null means "unknown". Collapsing the two loses one of the two fixes.
    const b = batchOf([delta("chat", { id: "cm-1", from: "ow-9", to: "owner" })]);
    expect(narrowToHeld(b, (id) => id === "m-2")).toEqual([]);
  });
});
