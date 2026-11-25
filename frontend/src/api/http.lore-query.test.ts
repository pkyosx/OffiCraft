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
  used_chars: 0,
  first_dropped_chars: 0,
  budget_segments: [],
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
      entryIds: ["L-12", "L-30"],
      q: "審查 100%",
    });
    const u = lastUrl();
    expect(u.pathname).toBe("/api/lore");
    // The VALUES and their ORDER, not merely "a scope_kinds param exists" —
    // asking for the wrong two kinds is the same bug as asking for all of them.
    expect(u.searchParams.getAll("scope_kinds")).toEqual(["agent", "manual"]);
    expect(u.searchParams.getAll("scope_keys")).toEqual(["mira", "tm-review"]);
    expect(u.searchParams.getAll("states")).toEqual(["active", "pinned"]);
    expect(u.searchParams.getAll("author_ids")).toEqual(["mira", "nova"]);
    expect(u.searchParams.getAll("entry_ids")).toEqual(["L-12", "L-30"]);
    expect(u.searchParams.getAll("q")).toEqual(["審查 100%"]);
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
      q: "  ",
    });
    const u = lastUrl();
    expect(u.searchParams.has("q")).toBe(false);
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

describe("httpApi.setLoreEntryScope", () => {
  it("a scope switch posts only the target kind to the entry's scope route and resolves to nothing", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        id: "L-7",
        scope_kind: "everyone",
        scope_key: "",
        state: "active",
        effective_ts: 1788400000,
        updated_ts: 1788500000,
      }),
    );

    const result = await httpApi.setLoreEntryScope("L-7", "everyone");

    const calls = fetchMock.mock.calls as unknown as [Request][];
    expect(calls).toHaveLength(1);
    const req = calls[0][0];
    const url = new URL(req.url);
    expect({
      method: req.method,
      path: url.pathname,
      search: url.search,
      contentType: req.headers.get("Content-Type"),
      body: await req.text(),
    }).toEqual({
      method: "POST",
      path: "/api/lore/L-7/scope",
      search: "",
      contentType: "application/json",
      body: '{"scope_kind":"everyone"}',
    });
    expect(result).toBeUndefined();
  });

  it("a refused scope switch rejects with the server's status, code and reason", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(
        {
          error: {
            code: "forbidden",
            message: "admin only",
          },
        },
        403,
      ),
    );

    const err = await httpApi.setLoreEntryScope("L-7", "manual").then(
      () => null,
      (e: Record<string, unknown>) => e,
    );
    expect({
      name: err?.name,
      message: err?.message,
      status: err?.status,
      code: err?.code,
      serverMessage: err?.serverMessage,
      retryAfter: err?.retryAfter,
    }).toEqual({
      name: "ApiError",
      message: "http 403 for POST /api/lore/L-7/scope",
      status: 403,
      code: "forbidden",
      serverMessage: "admin only",
      retryAfter: null,
    });
  });
});

describe("httpApi lore_type on the wire", () => {
  it("sends one repeated lore_types parameter per ticked type and omits an empty set", async () => {
    await httpApi.listLoreEntries({ loreTypes: ["owner_decision", "other"] });
    expect(lastUrl().search).toBe("?lore_types=owner_decision&lore_types=other");

    await httpApi.listLoreEntries({ loreTypes: [] });
    expect(lastUrl().search).toBe("");
  });

  it("writes lore_type in the body", async () => {
    const receipt = {
      id: "L-9",
      seq: 9,
      scope_kind: "agent",
      scope_key: "mira",
      created_ts: 1,
      scope_note: "",
    };
    fetchMock.mockImplementation(async () => jsonResponse(receipt));
    const bodies: unknown[] = [];
    for (const entry of [
      { title: "t", body: "b", loreType: "owner_preference" as const },
      { title: "t", body: "b", loreType: "other" as const },
    ]) {
      await httpApi.writeLoreEntry(entry);
      const calls = fetchMock.mock.calls as unknown as [Request][];
      const req = calls[calls.length - 1][0];
      expect([req.method, new URL(req.url).pathname]).toEqual(["POST", "/api/lore"]);
      bodies.push(await req.json());
    }
    expect(bodies).toEqual([
      { title: "t", body: "b", lore_type: "owner_preference" },
      { title: "t", body: "b", lore_type: "other" },
    ]);
  });
});

describe("httpApi lore budgets and edit", () => {
  it("reads a page's used chars, the first dropped entry's size and the per-scope segments", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        ...emptyPage,
        cap_chars: 10000,
        first_dropped_id: "L-212",
        used_chars: 9640,
        first_dropped_chars: 520,
        budget_segments: [
          { scope_kind: "everyone", entry_count: 14, chars: 6240, fitted_count: 14, fitted_chars: 6240 },
          { scope_kind: "agent", entry_count: 11, chars: 5380, fitted_count: 7, fitted_chars: 3400 },
        ],
      }),
    );

    expect(await httpApi.listLoreEntries({ scopeKinds: ["agent"], scopeKeys: ["m-1"] })).toEqual({
      entries: [],
      limit: 30,
      offset: 0,
      capChars: 10000,
      firstDroppedId: "L-212",
      usedChars: 9640,
      firstDroppedChars: 520,
      budgetSegments: [
        { scopeKind: "everyone", entryCount: 14, chars: 6240, fittedCount: 14, fittedChars: 6240 },
        { scopeKind: "agent", entryCount: 11, chars: 5380, fittedCount: 7, fittedChars: 3400 },
      ],
    });
  });

  it("reads every target's budget from GET /api/lore/budgets", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse({
        targets: [
          { scope_kind: "everyone", scope_key: "", cap_chars: 10000, used_chars: 6240, entry_count: 14, dropped_count: 0 },
          { scope_kind: "agent", scope_key: "m-1", cap_chars: 10000, used_chars: 9640, entry_count: 11, dropped_count: 4 },
          { scope_kind: "manual", scope_key: "review-pr", cap_chars: 8000, used_chars: 3210, entry_count: 8, dropped_count: 0 },
        ],
      }),
    );

    const targets = await httpApi.listLoreBudgets();

    const req = (fetchMock.mock.calls as unknown as [Request][])[0][0];
    expect([req.method, new URL(req.url).pathname]).toEqual(["GET", "/api/lore/budgets"]);
    expect(targets).toEqual([
      { scopeKind: "everyone", scopeKey: "", capChars: 10000, usedChars: 6240, entryCount: 14, droppedCount: 0 },
      { scopeKind: "agent", scopeKey: "m-1", capChars: 10000, usedChars: 9640, entryCount: 11, droppedCount: 4 },
      { scopeKind: "manual", scopeKey: "review-pr", capChars: 8000, usedChars: 3210, entryCount: 8, droppedCount: 0 },
    ]);
  });

  it("an edit patches only the fields given, as snake_case, and resolves to nothing", async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ id: "L-7", updated_ts: 1788500000 }));

    const result = await httpApi.editLoreEntry("L-7", { body: "新內容", loreType: "owner_decision" });

    const req = (fetchMock.mock.calls as unknown as [Request][])[0][0];
    expect({
      method: req.method,
      path: new URL(req.url).pathname,
      body: await req.text(),
    }).toEqual({
      method: "PATCH",
      path: "/api/lore/L-7",
      body: '{"body":"新內容","lore_type":"owner_decision"}',
    });
    expect(result).toBeUndefined();
  });
});
