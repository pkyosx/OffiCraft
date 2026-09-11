// api/http — the SSE payload → identity projection (T-8115).
//
// spec/sse.md §2.2 carries a payload "for convenience only" and forbids merging
// it, because it is deliberately partial: it lacks every server-derived DTO
// field the cockpit renders. What it does carry faithfully is WHICH entity the
// write touched, and that is what makes a one-item refetch possible.
//
// So this seam must let identity through and drop VALUES — and the dropping has
// to be enforced here, once, rather than trusted to every hook downstream. Each
// case below is a field the payload really carries (§2.2 lists them per topic).

import { describe, it, expect } from "vitest";
import { toSseDelta } from "./http";

describe("toSseDelta", () => {
  it("keeps the member id", () => {
    const d = toSseDelta("member", {
      id: "m-1a2b3c",
      name: "kyle",
      status: "active",
      desired_state: "online",
      owner_id: "owner",
    });
    expect(d.names).toEqual({ id: "m-1a2b3c" });
    expect(d.ids).toEqual(["m-1a2b3c"]);
  });

  it("keeps a chat message's participants — and drops nothing else in", () => {
    const d = toSseDelta("chat", { id: "cm-7", from: "m-1", to: "owner" });
    expect(d.names).toEqual({ id: "cm-7", from: "m-1", to: "owner" });
    expect(d.ids).toEqual(["cm-7", "m-1", "owner"]);
  });

  it("keeps WHO read and WHERE, but not the watermark itself", () => {
    const d = toSseDelta("chat_read", {
      reader: "owner",
      peer: "m-1",
      last_read_ts: 1752192000.5,
    });
    expect(d.names).toEqual({ reader: "owner", peer: "m-1" });
    // 🔴 The watermark is a VALUE. A hook that could read it here would be one
    // step from rendering it, which is exactly what §2.2 forbids: the payload is
    // partial and the server-derived receipt list is the only truth.
    expect(JSON.stringify(d)).not.toContain("last_read_ts");
    expect(JSON.stringify(d)).not.toContain("1752192000");
  });

  it("drops status and priority from a task delta", () => {
    const d = toSseDelta("task", {
      id: "t-1",
      status: "in_progress",
      priority: "high",
    });
    expect(d.names).toEqual({ id: "t-1" });
    expect(JSON.stringify(d)).not.toContain("in_progress");
    expect(JSON.stringify(d)).not.toContain("high");
  });

  it("drops a worker's codename and status", () => {
    const d = toSseDelta("outsource_worker", {
      id: "ow-3",
      codename: "O-7",
      status: "released",
    });
    expect(d.names).toEqual({ id: "ow-3" });
    expect(JSON.stringify(d)).not.toContain("O-7");
    expect(JSON.stringify(d)).not.toContain("released");
  });

  it("names nothing for a null payload — the topics that carry none", () => {
    for (const topic of ["task_manual", "global_context", "insight", "monitoring"]) {
      const d = toSseDelta(topic, null);
      expect(d.topic).toBe(topic);
      expect(d.names).toEqual({});
      expect(d.ids).toEqual([]);
    }
  });

  it("names nothing for a payload whose identity fields are absent, blank or not strings", () => {
    // Fail-open: an unnamed delta reads as "refetch the lot", never as "nothing
    // changed". A blank id is not an id, and a numeric one is a producer we do
    // not understand.
    expect(toSseDelta("member", { id: "" }).ids).toEqual([]);
    expect(toSseDelta("member", { id: 42 }).ids).toEqual([]);
    expect(toSseDelta("member", { key: "owner::m-1" }).ids).toEqual([]);
    expect(toSseDelta("member", "not an object").ids).toEqual([]);
  });

  it("de-duplicates a name that appears twice", () => {
    // A self-chat / an echo can name the same id in two fields; a consumer
    // counting ids must not see it twice.
    const d = toSseDelta("chat", { id: "cm-1", from: "owner", to: "owner" });
    expect(d.ids).toEqual(["cm-1", "owner"]);
  });
});
