// 🔴 THIS FILE EXISTS BECAUSE A NEW SCOPE WAS SILENTLY RENAMED INTO AN OLD ONE.
//
// `toLoreEntry` is the seam where a lore entry's WIRE `scope_kind` becomes the
// VIEW `scopeKind`. It used to end in a two-value narrowing:
//
//     scopeKind: w.scope_kind === "manual" ? "manual" : "role"
//                                            ^^^^^^^^   ^^^^^^
//                                            named      EVERYTHING ELSE
//
// The comment above it defended the fallback as crash-avoidance: a cockpit
// older than its server must not blank the whole 傳承 page when a scope it has
// never heard of appears. That reason is real and still holds. The coin it was
// bought with is what went wrong — the function did not TOLERATE an unknown
// kind, it RELABELLED it. When the server gained the third scope `agent`
// (outsource lore, which hangs off a member id because a contractor holds no
// role), every agent entry arrived here and left as a role entry: swept into a
// 角色 filter that never asked for it, counted as role lore, with no error
// anywhere to say so.
//
// NOTHING WOULD HAVE CAUGHT IT, and each miss has its own cause:
//
//   * `tsc --noEmit` could not: the generated schema types `scope_kind` as a
//     bare `string` (spec/openapi.json declares no enum), so collapsing an
//     unlisted value onto a listed one is well-typed.
//   * The mock could not: `mock.ts` stores entries already in the VIEW shape,
//     so mock-backed tests never execute this function.
//   * LorePage's tests could not: they stub `listLoreEntries` outright.
//   * The failure could not announce itself: a mislabelled row renders
//     perfectly — right title, right body, right author. A wrong answer that
//     is indistinguishable from a right one is worse than a blank row, which
//     is at least visibly missing something.
//
// So the case that matters is not "does role map to role". It is the FOURTH
// value: a kind this build has never heard of must come out as something that
// is not any of the three real ones.

import { describe, it, expect } from "vitest";
import { toLoreEntry } from "./mappers";
import type { components } from "./generated/schema";

/** One wire row, as a live server emits it. Only `scope_kind` varies per case;
 * everything else is held fixed so a failure can only be about the scope. */
function wireEntry(
  scopeKind: string,
  scopeKey: string,
): components["schemas"]["LoreEntryDTO"] {
  return {
    id: "L-1",
    seq: 1,
    scope_kind: scopeKind,
    scope_key: scopeKey,
    // `filed_note` used to sit here. It moved to the WRITE receipt as
    // `scope_note` (T-33): only a write can produce it, so on a row served by
    // GET /api/lore it was an always-empty column on every entry of every page.
    title: "交接路徑要寫絕對路徑",
    body: "對方在別的工作目錄下撲空，訊息跟那一輪沒跑一模一樣。",
    author_id: "ow-7d8ad859dd9b",
    source_task_id: "",
    state: "active",
    retire_reason: "",
    effective_ts: 1788460000,
    created_ts: 1788460000,
    updated_ts: 1788460000,
  };
}

describe("toLoreEntry scope_kind", () => {
  it("carries each of the two real scopes through under its own name", () => {
    // 🔴 `agent` IS THE ONE THIS FILE IS NAMED AFTER. Under the old narrowing
    // this line read "role" and every assertion about it passed elsewhere,
    // because nothing else looked.
    expect(toLoreEntry(wireEntry("agent", "ow-7d8ad859dd9b")).scopeKind).toBe(
      "agent",
    );
    expect(toLoreEntry(wireEntry("manual", "tm-mock")).scopeKind).toBe("manual");
  });

  it("maps the RETIRED `role` scope to unknown, and never to agent", () => {
    // 🔴 THIS ASSERTION REVERSED ON 2026-09-07, and the reversal is the change.
    // `role` was a real scope until the owner collapsed the three into two
    // (card rc-a43100fd0486 [0]); the server's migration rekeyed what it could
    // onto members and DELIBERATELY LEFT the rest at `role` rather than guess
    // an owner. So this value still arrives from live servers and this line is
    // about a row a reader will actually see, not about an old wire format.
    const legacy = toLoreEntry(wireEntry("role", "r-9f31c0d84a17"));
    // 🔴 `not.toBe("agent")` IS THE LOAD-BEARING HALF. The tempting "cleanup"
    // is to map `role` onto `agent` so the row renders with a kind word — which
    // would present an entry whose owner was explicitly undetermined as though
    // it belonged to a specific member, silently and irreversibly.
    expect(legacy.scopeKind).not.toBe("agent");
    expect(legacy.scopeKind).not.toBe("manual");
    expect(legacy.scopeKind).toBe("unknown");
    // The role_key survives verbatim: it is the only thing that says which
    // role this orphan came from, and it is what a person needs to place it.
    expect(legacy.scopeKey).toBe("r-9f31c0d84a17");
  });

  it("does not rename a scope it has never heard of into one it has", () => {
    // The next scope after `agent`, whatever it turns out to be, read by a
    // cockpit shipped before it existed. The point of the assertion is the
    // three `not` lines, not the "unknown" spelling: it must not be able to
    // pass by landing on a real kind.
    const future = toLoreEntry(wireEntry("station", "st-1")).scopeKind;
    expect(future).not.toBe("agent");
    expect(future).not.toBe("manual");
    expect(future).toBe("unknown");
  });

  it("still returns a whole, renderable row for an unknown scope", () => {
    // The property the old fallback was actually protecting, kept: no throw,
    // and the row keeps its own real text so the page shows the entry rather
    // than a hole. Losing this would trade one silent failure for another.
    const view = toLoreEntry(wireEntry("station", "st-1"));
    expect(view.id).toBe("L-1");
    expect(view.title).toBe("交接路徑要寫絕對路徑");
    expect(view.authorId).toBe("ow-7d8ad859dd9b");
    expect(view.state).toBe("active");
    // The key is carried VERBATIM even though the kind is unreadable — it is
    // the only thing that says which scope the entry belongs to, and dropping
    // it would make the row unrecoverable once the cockpit is upgraded.
    expect(view.scopeKey).toBe("st-1");
  });

  it("treats an empty scope_kind as unknown, not as a real scope", () => {
    // A truncated or defaulted row is the cheapest way to reintroduce the old
    // bug — `"" ? ... : "role"` is exactly the shape that was there.
    expect(toLoreEntry(wireEntry("", "")).scopeKind).toBe("unknown");
  });
});
