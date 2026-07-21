// useOwnerName — the topbar profile-pill nickname seam (T-0b41). Black-box
// pins: the name is server-backed (mount-fetch /api/settings), resolves to the
// localized fallback until loaded / when unset, and a commit PATCHes then
// adopts the server's echoed value (reverting on failure).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

const h = vi.hoisted(() => ({
  getServerSettings: vi.fn<() => Promise<{ ownerName: string }>>(),
  patchServerSettings: vi.fn<(p: { ownerName?: string }) => Promise<{ ownerName: string }>>(),
}));

vi.mock("../api", () => ({
  api: {
    getServerSettings: h.getServerSettings,
    patchServerSettings: h.patchServerSettings,
  },
}));

import { useOwnerName } from "./useOwnerName";

beforeEach(() => {
  h.getServerSettings.mockReset().mockResolvedValue({ ownerName: "" });
  h.patchServerSettings.mockReset();
});

describe("useOwnerName", () => {
  it("shows the fallback until the fetch lands, then the server value", async () => {
    h.getServerSettings.mockResolvedValueOnce({ ownerName: "伊娃" });
    const { result } = renderHook(() => useOwnerName("使用者"));
    expect(result.current.ownerName).toBe("使用者");
    await waitFor(() => expect(result.current.ownerName).toBe("伊娃"));
  });

  it("keeps the fallback when the stored name is empty", async () => {
    h.getServerSettings.mockResolvedValueOnce({ ownerName: "" });
    const { result } = renderHook(() => useOwnerName("使用者"));
    await waitFor(() => expect(h.getServerSettings).toHaveBeenCalled());
    expect(result.current.ownerName).toBe("使用者");
  });

  it("keeps the fallback (never a phantom value) when the load fails", async () => {
    h.getServerSettings.mockRejectedValueOnce(new Error("boom"));
    const { result } = renderHook(() => useOwnerName("使用者"));
    await waitFor(() => expect(h.getServerSettings).toHaveBeenCalled());
    expect(result.current.ownerName).toBe("使用者");
  });

  it("PATCHes a commit (trimmed) and adopts the server's echoed value", async () => {
    h.getServerSettings.mockResolvedValueOnce({ ownerName: "" });
    h.patchServerSettings.mockResolvedValueOnce({ ownerName: "新暱稱" });
    const { result } = renderHook(() => useOwnerName("使用者"));
    await waitFor(() => expect(h.getServerSettings).toHaveBeenCalled());

    act(() => result.current.setOwnerName("  新暱稱  "));
    expect(h.patchServerSettings).toHaveBeenCalledWith({ ownerName: "新暱稱" });
    await waitFor(() => expect(result.current.ownerName).toBe("新暱稱"));
  });

  it("reverts the optimistic edit when the PATCH rejects", async () => {
    h.getServerSettings.mockResolvedValueOnce({ ownerName: "原名" });
    h.patchServerSettings.mockRejectedValueOnce(new Error("nope"));
    const { result } = renderHook(() => useOwnerName("使用者"));
    await waitFor(() => expect(result.current.ownerName).toBe("原名"));

    act(() => result.current.setOwnerName("改壞了"));
    await waitFor(() => expect(result.current.ownerName).toBe("原名"));
  });
});
