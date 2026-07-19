// useOrgName — the topbar studio name seam (T-d693). Black-box pins: the name
// is server-backed (mount-fetch /api/settings), resolves to the localized
// fallback until loaded / when unset, and a commit PATCHes then adopts the
// server's echoed value (reverting on failure).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";

const h = vi.hoisted(() => ({
  getServerSettings: vi.fn<() => Promise<{ orgName: string }>>(),
  patchServerSettings: vi.fn<(p: { orgName?: string }) => Promise<{ orgName: string }>>(),
}));

vi.mock("../api", () => ({
  api: {
    getServerSettings: h.getServerSettings,
    patchServerSettings: h.patchServerSettings,
  },
}));

import { useOrgName } from "./useOrgName";

beforeEach(() => {
  h.getServerSettings.mockReset().mockResolvedValue({ orgName: "" });
  h.patchServerSettings.mockReset();
});

describe("useOrgName", () => {
  it("shows the fallback until the fetch lands, then the server value", async () => {
    h.getServerSettings.mockResolvedValueOnce({ orgName: "伊娃工作室" });
    const { result } = renderHook(() => useOrgName("AI 工作室"));
    // Before the fetch resolves: the localized default.
    expect(result.current.orgName).toBe("AI 工作室");
    await waitFor(() => expect(result.current.orgName).toBe("伊娃工作室"));
  });

  it("keeps the fallback when the stored name is empty", async () => {
    h.getServerSettings.mockResolvedValueOnce({ orgName: "" });
    const { result } = renderHook(() => useOrgName("AI 工作室"));
    await waitFor(() => expect(h.getServerSettings).toHaveBeenCalled());
    expect(result.current.orgName).toBe("AI 工作室");
  });

  it("keeps the fallback (never a phantom value) when the load fails", async () => {
    h.getServerSettings.mockRejectedValueOnce(new Error("boom"));
    const { result } = renderHook(() => useOrgName("AI 工作室"));
    await waitFor(() => expect(h.getServerSettings).toHaveBeenCalled());
    expect(result.current.orgName).toBe("AI 工作室");
  });

  it("PATCHes a commit (trimmed) and adopts the server's echoed value", async () => {
    h.getServerSettings.mockResolvedValueOnce({ orgName: "" });
    h.patchServerSettings.mockResolvedValueOnce({ orgName: "新工作室" });
    const { result } = renderHook(() => useOrgName("AI 工作室"));
    await waitFor(() => expect(h.getServerSettings).toHaveBeenCalled());

    act(() => result.current.setOrgName("  新工作室  "));
    expect(h.patchServerSettings).toHaveBeenCalledWith({ orgName: "新工作室" });
    await waitFor(() => expect(result.current.orgName).toBe("新工作室"));
  });

  it("reverts the optimistic edit when the PATCH rejects", async () => {
    h.getServerSettings.mockResolvedValueOnce({ orgName: "原名" });
    h.patchServerSettings.mockRejectedValueOnce(new Error("nope"));
    const { result } = renderHook(() => useOrgName("AI 工作室"));
    await waitFor(() => expect(result.current.orgName).toBe("原名"));

    act(() => result.current.setOrgName("改壞了"));
    // Optimistic value shows first, then snaps back to the last confirmed one.
    await waitFor(() => expect(result.current.orgName).toBe("原名"));
  });
});
