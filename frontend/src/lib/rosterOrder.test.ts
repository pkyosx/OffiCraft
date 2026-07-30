// T-ed38 — the 正職 roster comparator.
//
// The old ordering shipped with ZERO tests, and every rule here fails the same
// silent way: a row lands in the wrong place, nothing throws, and the screen
// still looks plausible. So each layer gets a case that is UNAMBIGUOUS about
// which layer decided it (every other key held equal), plus the two negative
// properties that motivated the design:
//
//   · a pinned pair is NOT reshuffled by unread / recency (契約 2);
//   · shuffling the input never changes the output (S2 — the order is total).

import { describe, it, expect } from "vitest";
import { compareMembers, pinIndexOf, splitPinned } from "./rosterOrder";
import type { Member } from "../types";

function mkMember(over: Partial<Member> & { id: string }): Member {
  return {
    memberId: "",
    name: over.id,
    role: "",
    roleName: "",
    status: "offline",
    lifecycle: "offline",
    model: "",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    lastActivityAt: 0,
    ...over,
  };
}

/** Sort ids through the comparator — the shape every assertion below reads. */
function order(members: Member[], pinned: string[] = []): string[] {
  return [...members]
    .sort(compareMembers(pinIndexOf(pinned)))
    .map((m) => m.id);
}

describe("compareMembers — the four layers, one at a time", () => {
  it("L1 pinned members come first, in the STORED array order", () => {
    // The pinned pair is deliberately the LEAST urgent by every other rule:
    // no unread, no activity, no assistant role. Only the pin can lift them.
    const members = [
      mkMember({ id: "a-unread", unreadCount: 5, lastActivityAt: 900 }),
      mkMember({ id: "b-pin" }),
      mkMember({ id: "c-pin" }),
      mkMember({ id: "d-assistant", role: "assistant", lastActivityAt: 800 }),
    ];
    // Stored newest-pin-first: c was pinned after b, so c is on top.
    expect(order(members, ["c-pin", "b-pin"])).toEqual([
      "c-pin",
      "b-pin",
      "a-unread",
      "d-assistant",
    ]);
  });

  it("L2 unread beats recency (someone waiting outranks someone I just talked to)", () => {
    const members = [
      // Spoke seconds ago, nothing waiting.
      mkMember({ id: "fresh", lastActivityAt: 1000 }),
      // Spoke three days ago and is STILL waiting on an answer.
      mkMember({ id: "waiting", unreadCount: 1, lastActivityAt: 10 }),
    ];
    expect(order(members)).toEqual(["waiting", "fresh"]);
  });

  it("L2 does NOT rank by unread COUNT — only by whether any is waiting", () => {
    // Deliberately rejected in design: an unread count jumps around as the
    // other side keeps typing, and "more unread" is not "more urgent".
    // Within the unread group, RECENCY still decides.
    const members = [
      mkMember({ id: "many-old", unreadCount: 9, lastActivityAt: 10 }),
      mkMember({ id: "one-new", unreadCount: 1, lastActivityAt: 90 }),
    ];
    expect(order(members)).toEqual(["one-new", "many-old"]);
  });

  it("L3 recency sorts newest-first, and beats the role rule", () => {
    const members = [
      // The seed assistant, but silent for a week.
      mkMember({ id: "mira", role: "assistant", lastActivityAt: 100 }),
      mkMember({ id: "newer", lastActivityAt: 300 }),
      mkMember({ id: "newest", lastActivityAt: 500 }),
    ];
    expect(order(members)).toEqual(["newest", "newer", "mira"]);
  });

  it("L4 the assistant role wins only when everything above ties", () => {
    const members = [
      mkMember({ id: "a-plain" }),
      mkMember({ id: "z-assistant", role: "assistant" }),
    ];
    // Note the ids: pure id order would put a-plain first, so this asserts the
    // role layer really ran (and that it ran BEFORE the id tie-break).
    expect(order(members)).toEqual(["z-assistant", "a-plain"]);
  });

  it("L5a breaks a full tie by NAME, case-insensitively", () => {
    // The ids run the OTHER way (z-… before a-…), so this can only pass if the
    // name comparison ran before the id one. Mixed case on purpose: the compare
    // is on `toLowerCase()`, so "alice" must beat "Bob" — a raw code-unit
    // compare would put every capital letter ahead of every lowercase one.
    const members = [
      mkMember({ id: "z-1", name: "alice" }),
      mkMember({ id: "a-1", name: "Bob" }),
    ];
    expect(order(members)).toEqual(["z-1", "a-1"]);
  });

  it("L5b breaks a SAME-NAME tie by id, so the order is TOTAL", () => {
    const members = [
      mkMember({ id: "m-b", name: "Same" }),
      mkMember({ id: "m-a", name: "Same" }),
    ];
    expect(order(members)).toEqual(["m-a", "m-b"]);
  });
});

describe("compareMembers — 契約 2: a pinned group is never reshuffled", () => {
  it("keeps the stored pin order even when a later pin has unread AND newer activity", () => {
    // Everything the automatic rules care about points the other way: `second`
    // is the one with a waiting message and the fresher conversation. If any
    // layer below the pin ran, it would climb over `first`.
    const members = [
      mkMember({ id: "first", lastActivityAt: 1 }),
      mkMember({ id: "second", unreadCount: 3, lastActivityAt: 9999 }),
    ];
    expect(order(members, ["first", "second"])).toEqual(["first", "second"]);
  });

  it("the SAME two members DO reorder once they are not pinned", () => {
    // The control for the case above: without pins the automatic rules put
    // `second` on top. So the previous assertion is about the pin exemption,
    // not about the fixture happening to be in that order already.
    const members = [
      mkMember({ id: "first", lastActivityAt: 1 }),
      mkMember({ id: "second", unreadCount: 3, lastActivityAt: 9999 }),
    ];
    expect(order(members)).toEqual(["second", "first"]);
  });
});

describe("compareMembers — S2: the order is deterministic under shuffling", () => {
  it("returns an identical order for every permutation of the same roster", () => {
    // A stable sort only guarantees "same input order → same output order".
    // The roster's input order is NOT guaranteed (the server's SQL has no
    // secondary key), so the comparator itself must be total.
    // The fixture must actually REACH layer 5, in both of its halves —
    // otherwise it passes with layer 5 deleted (measured: it did):
    //   · m-2 / m-7 tie on every layer 1-4 and have DIFFERENT names → 5a;
    //   · m-8 / m-9 tie on 1-4 AND share a name → only the id separates them,
    //     so this half goes red if 5b stops deciding.
    const members = [
      mkMember({ id: "m-1", role: "assistant" }),
      mkMember({ id: "m-2" }),
      mkMember({ id: "m-7" }),
      mkMember({ id: "m-8", name: "Twin" }),
      mkMember({ id: "m-9", name: "Twin" }),
      mkMember({ id: "m-3", unreadCount: 2 }),
      mkMember({ id: "m-4", lastActivityAt: 500 }),
      mkMember({ id: "m-5", lastActivityAt: 500 }),
      mkMember({ id: "m-6", unreadCount: 1, lastActivityAt: 20 }),
    ];
    const expected = order(members, ["m-5"]);
    // Deterministic pseudo-shuffles (no Math.random — a flaky ordering test is
    // worse than none): rotate and reverse to cover many starting orders.
    for (let rot = 0; rot < members.length; rot++) {
      const rotated = [...members.slice(rot), ...members.slice(0, rot)];
      expect(order(rotated, ["m-5"])).toEqual(expected);
      expect(order([...rotated].reverse(), ["m-5"])).toEqual(expected);
    }
  });
});

describe("compareMembers — S5: fallback semantics", () => {
  it("an ABSENT lastActivityAt (older server) reads as 0 and retires the layer", () => {
    // No member carries the field at all — exactly the shape of a roster from
    // a server predating it. The result must be the PREVIOUS ordering (role,
    // then a deterministic tie-break), not a crash and not a random order.
    const members = [
      mkMember({ id: "b-plain", lastActivityAt: undefined }),
      mkMember({ id: "c-assistant", role: "assistant", lastActivityAt: undefined }),
      mkMember({ id: "a-plain", lastActivityAt: undefined }),
    ];
    expect(order(members)).toEqual(["c-assistant", "a-plain", "b-plain"]);
  });

  it("a REAL 0 (never talked) sorts after anyone who has — not bottom-pinned", () => {
    // "After the ones with activity" and "at the very bottom" differ: the
    // never-talked member must still be ranked by the layers BELOW recency.
    const members = [
      mkMember({ id: "talked", lastActivityAt: 50 }),
      mkMember({ id: "never-plain", lastActivityAt: 0 }),
      mkMember({ id: "never-assistant", role: "assistant", lastActivityAt: 0 }),
    ];
    expect(order(members)).toEqual([
      "talked",
      "never-assistant",
      "never-plain",
    ]);
  });

  it("an ABSENT unreadCount reads as 0 (no unread), never as 'has unread'", () => {
    const members = [
      mkMember({ id: "absent", unreadCount: undefined as unknown as number }),
      mkMember({ id: "real", unreadCount: 1 }),
    ];
    expect(order(members)).toEqual(["real", "absent"]);
  });

  it("an EMPTY pin list (a failed settings read degrades to []) changes nothing", () => {
    const members = [
      mkMember({ id: "b-plain" }),
      mkMember({ id: "a-assistant", role: "assistant" }),
    ];
    expect(order(members, [])).toEqual(["a-assistant", "b-plain"]);
  });
});

describe("pinIndexOf / splitPinned — orphan pins", () => {
  it("a pin for an unknown member is ignored: no ghost row, no crash", () => {
    const members = [mkMember({ id: "live" }), mkMember({ id: "other" })];
    // "dismissed" is pinned but no longer on the roster. The list must render
    // exactly the two live members — the server never cleans the stored array,
    // so the intersection has to happen here.
    const pinIndex = pinIndexOf(["dismissed", "live"]);
    const sorted = [...members].sort(compareMembers(pinIndex));
    expect(sorted.map((m) => m.id)).toEqual(["live", "other"]);
    const { pinned, rest } = splitPinned(sorted, pinIndex);
    expect(pinned.map((m) => m.id)).toEqual(["live"]);
    expect(rest.map((m) => m.id)).toEqual(["other"]);
  });

  it("splits into an empty pinned group when nothing is pinned", () => {
    const members = [mkMember({ id: "a" }), mkMember({ id: "b" })];
    const pinIndex = pinIndexOf([]);
    const { pinned, rest } = splitPinned(
      [...members].sort(compareMembers(pinIndex)),
      pinIndex,
    );
    expect(pinned).toEqual([]);
    expect(rest.map((m) => m.id)).toEqual(["a", "b"]);
  });

  it("splits into an empty REST group when everything is pinned", () => {
    const members = [mkMember({ id: "a" }), mkMember({ id: "b" })];
    const pinIndex = pinIndexOf(["b", "a"]);
    const { pinned, rest } = splitPinned(
      [...members].sort(compareMembers(pinIndex)),
      pinIndex,
    );
    expect(pinned.map((m) => m.id)).toEqual(["b", "a"]);
    expect(rest).toEqual([]);
  });
});
