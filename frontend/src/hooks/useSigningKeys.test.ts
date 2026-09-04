// useSigningKeys — the hook's own tests, which the card's tests cannot stand in
// for because they `vi.mock` this module wholesale (T-62).
//
// 🔴 THIS FILE EXISTS BECAUSE OF A GUARD WIRED TO THE WRONG THING. The card had
// a test called "renders the server's refusal" that handed the component a
// hand-written English sentence and asserted the component rendered it. It was
// green while the real path rendered `http 409 for POST /api/auth/signing-keys/
// k-…/remove` — the ApiError LOG format — and threw the server's actual reason
// away. Nothing that mocks this hook can ever catch that; only driving the real
// error envelope can.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { mockApiError } from "../api/errorCodes";
import { serverMessageOf } from "../api/errors";
import { useSigningKeys } from "./useSigningKeys";
import { api } from "../api";

const FALLBACK = "fallback-copy";

const REFUSAL = mockApiError(
  "http 409 for POST /api/auth/signing-keys/k-old/remove",
  409,
  "key 'k-old' is the one currently signing and cannot be removed — rotate first, then remove it",
);

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("useSigningKeys · the failure path", () => {
  it("reports the SERVER'S reason, not the http log line", async () => {
    // The positive control for the whole file: these two really are different
    // strings, so an assertion on one cannot pass by accident on the other.
    expect(REFUSAL.message).toContain("http 409 for POST");
    expect(serverMessageOf(REFUSAL)).toContain("rotate first");
    expect(REFUSAL.message).not.toContain("rotate first");

    vi.spyOn(api, "getSigningKeys").mockResolvedValue([]);
    vi.spyOn(api, "removeSigningKey").mockRejectedValue(REFUSAL);

    const { result } = renderHook(() => useSigningKeys(FALLBACK));
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.remove("k-old"));
    await waitFor(() => expect(result.current.error).not.toBe(""));

    expect(result.current.error).toContain("rotate first");
    expect(result.current.error).not.toContain("http 409");
  });

  it("falls back to the caller's copy when the rejection carries no reason", async () => {
    // "" from serverMessageOf means "there was no reason to show" — an empty
    // error line reads as nothing having gone wrong, which is the failure this
    // whole hook header is about.
    vi.spyOn(api, "getSigningKeys").mockResolvedValue([]);
    vi.spyOn(api, "rotateSigningKey").mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useSigningKeys(FALLBACK));
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.rotate());
    await waitFor(() => expect(result.current.error).toBe(FALLBACK));
  });

  it("keeps the previous ring when an action fails — an emptied list reads as 'the keys are gone'", async () => {
    const ring = [{ keyId: "k-a", createdTs: null, isSigning: true }];
    vi.spyOn(api, "getSigningKeys").mockResolvedValue(ring);
    vi.spyOn(api, "rotateSigningKey").mockRejectedValue(REFUSAL);

    const { result } = renderHook(() => useSigningKeys(FALLBACK));
    await waitFor(() => expect(result.current.keys).toHaveLength(1));
    act(() => result.current.rotate());
    await waitFor(() => expect(result.current.error).not.toBe(""));
    expect(result.current.keys).toEqual(ring);
  });
});

describe("useSigningKeys · the happy path", () => {
  it("adopts the ring the action answers with, and clears the previous error", async () => {
    const before = [{ keyId: "k-a", createdTs: null, isSigning: true }];
    const after = [
      { keyId: "k-a", createdTs: null, isSigning: false },
      { keyId: "k-b", createdTs: 1788400000, isSigning: true },
    ];
    vi.spyOn(api, "getSigningKeys").mockResolvedValue(before);
    vi.spyOn(api, "rotateSigningKey").mockResolvedValue(after);

    const { result } = renderHook(() => useSigningKeys(FALLBACK));
    await waitFor(() => expect(result.current.keys).toHaveLength(1));
    act(() => result.current.rotate());
    await waitFor(() => expect(result.current.keys).toHaveLength(2));
    expect(result.current.error).toBe("");
    // The answer IS the new state: no second fetch is needed to learn it.
    expect(api.getSigningKeys).toHaveBeenCalledTimes(1);
  });
});

describe("mock parity · the mock must refuse the way the wire refuses", () => {
  // 🔴 A MOCK MORE GENEROUS THAN THE WIRE IS A LIE THAT PASSES. The first
  // version threw a bare `new Error(prose)`, so `e.message` carried the reason
  // and a caller reading the wrong field looked correct in mock mode while the
  // real server rendered `http 409 for POST …`. That is what this asserts:
  // the mock's rejection must carry the reason where the ADAPTERS put it, not
  // where a plain Error happens to.
  //
  // ⚠️ Scope, stated honestly: this pins the two signing-key refusals only.
  // "every mock rejection carries the wire's envelope" is a repo-wide property
  // and nothing holds it — dropping the envelope here was invisible to
  // errorCodes.test.ts, which checks that mock.ts IMPORTS mockApiError, a fact
  // the other call sites keep true on their own.
  it("carries the reason in serverMessage for both refusals", async () => {
    const { mockApi, __resetMock } = await import("../api/mock");
    __resetMock();

    // The signing key: 409, and the reason tells the caller what to do instead.
    const signing = (await mockApi.getSigningKeys()).find((k) => k.isSigning)!;
    await expect(mockApi.removeSigningKey(signing.keyId)).rejects.toSatisfy(
      (e: unknown) => serverMessageOf(e).includes("rotate first"),
    );

    // An unknown id: 404, and the reason names the id.
    await expect(mockApi.removeSigningKey("k-nope")).rejects.toSatisfy(
      (e: unknown) => serverMessageOf(e).includes("k-nope"),
    );
  });

  it("resets the ring between tests, so a rotation cannot leak into the next one", async () => {
    const { mockApi, __resetMock } = await import("../api/mock");
    __resetMock();
    const before = await mockApi.getSigningKeys();
    await mockApi.rotateSigningKey();
    expect(await mockApi.getSigningKeys()).toHaveLength(before.length + 1);
    __resetMock();
    expect(await mockApi.getSigningKeys()).toHaveLength(before.length);
  });
});
