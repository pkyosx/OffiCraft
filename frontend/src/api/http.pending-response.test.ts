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
//   3. `null` → false as well. T-91 narrowed the RECEIPT schemas to plain
//      optional `boolean` (MemberDTO, which these routes no longer answer, is
//      what typed them `boolean | null`), so a null is no longer a shape the
//      server can send — the case stays pinned because `=== true` is what makes
//      absent/null/false one answer, and a mapper rewritten to `?? false` or
//      `!!` would still pass every other case here.
//   4. the same three for relocate's `relocation_pending`.
//   5. relocate's companion `relocation_deferred` (T-927a) maps the same way, and
//      INDEPENDENTLY of pending: the panel suppresses its alert on deferred, so a
//      mapper that dropped it would turn a normal wind-down into a false alarm.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

/** The activate/relocate 200 body is a RECEIPT since T-91 —
 * MemberActivateReceiptDTO / AgentRelocateReceiptDTO, `id` plus the pending
 * flags — not the MemberDTO it used to be. `id` alone is the whole of the rest. */
function receiptBody(extra: Record<string, unknown> = {}) {
  return { id: "m-1", ...extra };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn(async () => jsonResponse(receiptBody()));

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
      jsonResponse(receiptBody({ activation_pending: true })),
    );
    expect(await httpApi.activateMember("m-1")).toEqual({
      activationPending: true,
    });
  });

  it("absent → activationPending false (omitempty = the START went out)", async () => {
    fetchMock.mockImplementation(async () => jsonResponse(receiptBody()));
    expect(await httpApi.activateMember("m-1")).toEqual({
      activationPending: false,
    });
  });

  it("null → activationPending false (the wire type is boolean | null)", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(receiptBody({ activation_pending: null })),
    );
    expect(await httpApi.activateMember("m-1")).toEqual({
      activationPending: false,
    });
  });
});

describe("httpApi.relocateMember reads relocation_pending", () => {
  it("true → relocationPending true", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(receiptBody({ relocation_pending: true })),
    );
    expect(await httpApi.relocateMember("m-1", "mach-a")).toEqual({
      relocationPending: true,
      relocationDeferred: false,
    });
  });

  it("absent → relocationPending false (the move landed)", async () => {
    fetchMock.mockImplementation(async () => jsonResponse(receiptBody()));
    expect(await httpApi.relocateMember("m-1", "mach-a")).toEqual({
      relocationPending: false,
      relocationDeferred: false,
    });
  });

  it("relocation_deferred true → relocationDeferred true (a deliberate deferral)", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(
        receiptBody({ relocation_pending: true, relocation_deferred: true }),
      ),
    );
    expect(await httpApi.relocateMember("m-1", "mach-a")).toEqual({
      relocationPending: true,
      relocationDeferred: true,
    });
  });

  it("relocation_deferred null → relocationDeferred false", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(
        receiptBody({ relocation_pending: true, relocation_deferred: null }),
      ),
    );
    expect(await httpApi.relocateMember("m-1", "mach-a")).toEqual({
      relocationPending: true,
      relocationDeferred: false,
    });
  });

  it("null → relocationPending false", async () => {
    fetchMock.mockImplementation(async () =>
      jsonResponse(receiptBody({ relocation_pending: null })),
    );
    expect(await httpApi.relocateMember("m-1", "mach-a")).toEqual({
      relocationPending: false,
      relocationDeferred: false,
    });
  });
});
