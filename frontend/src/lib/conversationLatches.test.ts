// The lease record behind every per-conversation latch in useChat. These pin
// the properties the rest of the fix RESTS on — the ones that used to be
// comments, and that four reviews in a row found somebody had stopped obeying.

import { describe, it, expect } from "vitest";
import { openLatches } from "./conversationLatches";

describe("openLatches", () => {
  it("two records share nothing — one conversation's latch is not another's", () => {
    // 🔴 THE RECORD IS THE BOUNDARY (T-48, R13-5). This used to assert that a
    // record REFUSED a caller naming a different peer, because one `useChat`
    // instance was swapped between rooms and could be holding a record from a
    // room the owner had left. Rooms are separate MOUNTS now, so each has its
    // own record and the peer stamp had become a comparison that could not
    // fail. What still has to be true — and would break if the state were ever
    // hoisted back into the module — is that the two records are independent.
    const a = openLatches(true);
    const b = openLatches(false);
    expect(a.isHeld("entryAnchor")).toBe(true);
    expect(b.isHeld("entryAnchor")).toBe(false);

    const mine = a.acquire("loadingOlder");
    expect(mine).not.toBe(null);
    expect(a.isHeld("loadingOlder")).toBe(true);
    expect(b.isHeld("loadingOlder"), "B's mutex is not A's").toBe(false);
    expect(b.acquire("loadingOlder"), "and B can still take its own").not.toBe(
      null,
    );

    mine?.();
    expect(a.isHeld("loadingOlder")).toBe(false);
    expect(b.isHeld("loadingOlder"), "A's release did not free B's").toBe(true);
  });

  it("entering WITHOUT an anchor holds nothing at all", () => {
    const l = openLatches(false);
    for (const name of [
      "entryAnchor",
      "anchorFetch",
      "loadStale",
      "loadingOlder",
      "loadingNewer",
    ] as const) {
      expect(l.isHeld(name)).toBe(false);
    }
  });

  it("a same-direction mutex refuses the second holder and frees on the handle", () => {
    const l = openLatches(false);
    const first = l.acquire("loadingOlder");
    expect(first).not.toBe(null);
    expect(l.acquire("loadingOlder")).toBe(null);
    // The other direction is a different latch.
    expect(l.acquire("loadingNewer")).not.toBe(null);
    first!();
    expect(l.isHeld("loadingOlder")).toBe(false);
    expect(l.acquire("loadingOlder")).not.toBe(null);
  });

  it("dropping an anchor lease ends the entry-anchor window, on every ending", () => {
    const l = openLatches(true);
    const release = l.acquire("anchorFetch")!;
    expect(l.isHeld("anchorFetch")).toBe(true);
    expect(l.isHeld("entryAnchor")).toBe(true);
    release();
    expect(l.isHeld("anchorFetch")).toBe(false);
    // R3-3: the superseded branch used to keep this set "because the caller
    // re-schedules", and the caller only cleared it on an EMPTY thread.
    expect(l.isHeld("entryAnchor")).toBe(false);
  });

  it("anchor leases nest — the COUNT clears on the last one out, the entry window on the FIRST", () => {
    // ⚠️ The name used to say "only the last one out ends the window", which is
    // the opposite of what this record does and of what the test asserted
    // (R5-4: it never mentioned `entryAnchor` at all). "The window" in this
    // codebase's vocabulary is the ENTRY-ANCHOR window, and that one ends with
    // the FIRST lease dropped — deliberately, because `load()`'s gate is
    // `entryAnchor OR anchorFetch > 0`, so a still-held count keeps the door
    // shut anyway and nothing observes the difference. Both halves are pinned
    // here so the next reader does not "fix" the implementation to match a name.
    const l = openLatches(true);
    const first = l.acquire("anchorFetch")!;
    const second = l.acquire("anchorFetch")!;
    first();
    expect(l.isHeld("anchorFetch")).toBe(true);
    expect(l.isHeld("entryAnchor")).toBe(false);
    second();
    expect(l.isHeld("anchorFetch")).toBe(false);
    expect(l.isHeld("entryAnchor")).toBe(false);
  });

  it("a handle is spent once — a double release cannot take the count below zero", () => {
    // 🔴 R4-1's damage, made unreachable. Releasing a lease this call never
    // took drove `anchorFetching` to -1, which disabled the `> 0` gate for the
    // rest of the session. A handle is idempotent, and there is no other door.
    const l = openLatches(false);
    const outer = l.acquire("anchorFetch")!;
    const inner = l.acquire("anchorFetch")!;
    inner();
    inner();
    inner();
    expect(l.isHeld("anchorFetch")).toBe(true);
    outer();
    expect(l.isHeld("anchorFetch")).toBe(false);
    // Still zero, not negative: one more acquire is still visible.
    const again = l.acquire("anchorFetch")!;
    expect(l.isHeld("anchorFetch")).toBe(true);
    again();
  });

  it("the load debt is re-statable, and settling it clears it once", () => {
    // Not a mutex: the holder is the load that FAILED, the payer is the next
    // load that LANDS, and a second failure must not be refused.
    const l = openLatches(false);
    const first = l.acquire("loadStale")!;
    const second = l.acquire("loadStale")!;
    expect(l.isHeld("loadStale")).toBe(true);
    second();
    expect(l.isHeld("loadStale")).toBe(false);
    first();
    expect(l.isHeld("loadStale")).toBe(false);
  });
});
