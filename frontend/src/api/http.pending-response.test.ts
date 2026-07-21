// httpApi · the *_pending RESPONSE contract (T-7fa1).
//
// 🔴 WHY THIS FILE EXISTS SEPARATELY FROM THE UI TESTS. The component tests
// drive a handler THEY supply, so they prove the UI reacts correctly to a
// verdict — they say nothing about whether the adapter ever produces one. A
// `activateMember` that always answered `{activationPending: false}` (the exact
// shape of the original bug, just typed) would leave every one of them green.
// This pins the other end: the wire field is actually READ.
//
// Locked here, for BOTH members of the family:
//   1. `activation_pending: true` in the 200 body → `{activationPending: true}`.
//   2. the field ABSENT (the server omits it via omitempty on the success
//      shape) → `{activationPending: false}` — absence is success, never
//      undefined leaking into a `?.activationPending` branch.
//   3. `null` → false as well (the OpenAPI type is `boolean | null`).
//   4. the same three for relocate's `relocation_pending`.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

/** The activate/relocate 200 body is a MemberDTO; only the pending flag matters
 * here, so the rest is the minimum the mapper tolerates. */
function memberBody(extra: Record<string, unknown> = {}) {
  return {
    id: "m-1",
    name: "Mira",
    role: "assistant",
    online: false,
    presence: "offline",
    status: "active",
    ...extra,
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn(async () => jsonResponse(memberBody()));

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("httpApi.activateMember reads activation_pending", () => {
  it("true → activationPending true", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(memberBody({ activation_pending: true })),
    );
    expect(await httpApi.activateMember("m-1")).toEqual({
      activationPending: true,
    });
  });

  it("absent → activationPending false (omitempty = the START went out)", async () => {
    fetchMock.mockImplementation(async () => jsonResponse(memberBody()));
    expect(await httpApi.activateMember("m-1")).toEqual({
      activationPending: false,
    });
  });

  it("null → activationPending false (the wire type is boolean | null)", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(memberBody({ activation_pending: null })),
    );
    expect(await httpApi.activateMember("m-1")).toEqual({
      activationPending: false,
    });
  });
});

describe("httpApi.relocateMember reads relocation_pending", () => {
  it("true → relocationPending true", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(memberBody({ relocation_pending: true })),
    );
    expect(await httpApi.relocateMember("m-1", "mach-a")).toEqual({
      relocationPending: true,
    });
  });

  it("absent → relocationPending false (the move landed)", async () => {
    fetchMock.mockImplementation(async () => jsonResponse(memberBody()));
    expect(await httpApi.relocateMember("m-1", "mach-a")).toEqual({
      relocationPending: false,
    });
  });

  it("null → relocationPending false", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(memberBody({ relocation_pending: null })),
    );
    expect(await httpApi.relocateMember("m-1", "mach-a")).toEqual({
      relocationPending: false,
    });
  });
});
