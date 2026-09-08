// T-33 傳承 — what the lore list fetch actually puts ON THE WIRE.
//
// The ruling (owner rc-0376bf875757 [1]) made the 清單頁's three filters
// multi-select, so each axis gained a REPEATABLE plural parameter beside its
// frozen singular one. What has to be pinned here is the REQUEST URL, not "the
// adapter was called with an options object": the page can hold exactly the
// right multi-select state and still ask the server for one value, or for
// everything, and nothing on screen would say which.
//
// So these tests read the real `Request` openapi-fetch builds, which puts the
// SERIALISATION under test too — one repeated `?states=` per value, form/explode,
// the shape the spec declares — rather than assuming it.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const emptyPage = {
  entries: [],
  limit: 30,
  offset: 0,
  cap_chars: 0,
  first_dropped_id: "",
};

const fetchMock = vi.fn(async () => jsonResponse(emptyPage));

function lastUrl(): URL {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  return new URL(calls[calls.length - 1][0].url);
}

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(async () => jsonResponse(emptyPage));
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("httpApi.listLoreEntries · the multi-select sets go on the wire (T-33)", () => {
  it("sends one repeated parameter per ticked value, with the EXACT values", async () => {
    await httpApi.listLoreEntries({
      scopeKinds: ["agent", "manual"],
      scopeKeys: ["mira", "tm-review"],
      states: ["active", "pinned"],
      authorIds: ["mira", "nova"],
    });
    const u = lastUrl();
    expect(u.pathname).toBe("/api/lore");
    // The VALUES and their ORDER, not merely "a scope_kinds param exists" —
    // asking for the wrong two kinds is the same bug as asking for all of them.
    expect(u.searchParams.getAll("scope_kinds")).toEqual(["agent", "manual"]);
    expect(u.searchParams.getAll("scope_keys")).toEqual(["mira", "tm-review"]);
    expect(u.searchParams.getAll("states")).toEqual(["active", "pinned"]);
    expect(u.searchParams.getAll("author_ids")).toEqual(["mira", "nova"]);
    // Repeated, NOT comma-joined into one value — the server binds these as an
    // array and a single "agent,manual" would be one unknown kind, a 400.
    expect(u.search).toContain("scope_kinds=agent&scope_kinds=manual");
  });

  it("omits an empty set entirely rather than sending an empty parameter", async () => {
    // An empty multi-select means 所有, and sending nothing is exactly that.
    // An empty `?states=` would be a value the server has to decide the meaning
    // of, on an axis the user did not narrow.
    await httpApi.listLoreEntries({
      scopeKinds: [],
      scopeKeys: [],
      states: [],
      authorIds: [],
    });
    const u = lastUrl();
    expect(u.searchParams.has("scope_kinds")).toBe(false);
    expect(u.searchParams.has("scope_keys")).toBe(false);
    expect(u.searchParams.has("states")).toBe(false);
    expect(u.searchParams.has("author_ids")).toBe(false);
  });

  it("still sends the singular spelling on its own, unchanged", async () => {
    // The frozen wire is untouched: a caller written before the ruling sends
    // exactly what it always sent, and sends none of the plural names.
    await httpApi.listLoreEntries({
      scopeKind: "agent",
      scopeKey: "mira",
      state: "active",
      authorId: "mira",
    });
    const u = lastUrl();
    expect(u.searchParams.get("scope_kind")).toBe("agent");
    expect(u.searchParams.get("scope_key")).toBe("mira");
    expect(u.searchParams.get("state")).toBe("active");
    expect(u.searchParams.get("author_id")).toBe("mira");
    expect(u.searchParams.has("states")).toBe(false);
  });

  it("forwards BOTH spellings when both are given, and resolves neither", async () => {
    // 🔴 THE PRECEDENCE IS THE SERVER'S. This seam must not drop the singular,
    // and must not drop the plural either: a second copy of "plural wins" here
    // could drift from the one place the rule is actually enforced, and the two
    // would then disagree about a request neither of them refused.
    await httpApi.listLoreEntries({
      state: "active",
      states: ["pinned", "retired"],
    });
    const u = lastUrl();
    expect(u.searchParams.get("state")).toBe("active");
    expect(u.searchParams.getAll("states")).toEqual(["pinned", "retired"]);
  });
});
