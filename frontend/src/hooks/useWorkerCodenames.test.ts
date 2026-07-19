// useWorkerCodenames — the lazy released-worker codename cache (T-3ed8).
// GET /api/outsource-workers/{id} serves released rows, so an ow- id missing
// from every list the caller holds still resolves to its codename; a failed
// fetch is negative-cached (raw-id fallback, no refetch loop).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";

const getOutsourceWorker = vi.fn();
vi.mock("../api", () => ({
  api: { getOutsourceWorker: (id: string) => getOutsourceWorker(id) },
}));

import {
  useWorkerCodenames,
  __resetWorkerCodenameCache,
} from "./useWorkerCodenames";

describe("useWorkerCodenames", () => {
  beforeEach(() => {
    __resetWorkerCodenameCache();
    getOutsourceWorker.mockReset();
  });

  it("resolves an ow- id to its codename via the per-id read", async () => {
    getOutsourceWorker.mockResolvedValue({ id: "ow-abc", codename: "X-1" });
    const { result } = renderHook(() => useWorkerCodenames(["ow-abc"]));
    await waitFor(() => {
      expect(result.current.get("ow-abc")).toBe("X-1");
    });
    expect(getOutsourceWorker).toHaveBeenCalledTimes(1);
    expect(getOutsourceWorker).toHaveBeenCalledWith("ow-abc");
  });

  it("never fetches non-ow ids", async () => {
    const { result } = renderHook(() =>
      useWorkerCodenames(["m-123", "mira", "system", ""]),
    );
    expect(result.current.size).toBe(0);
    expect(getOutsourceWorker).not.toHaveBeenCalled();
  });

  it("negative-caches a failed fetch and keeps the raw-id fallback", async () => {
    getOutsourceWorker.mockRejectedValue(new Error("404"));
    const first = renderHook(() => useWorkerCodenames(["ow-gone"]));
    await waitFor(() => {
      expect(getOutsourceWorker).toHaveBeenCalledTimes(1);
    });
    expect(first.result.current.get("ow-gone")).toBeUndefined();
    // A second mount must NOT refetch the known-unresolvable id.
    const second = renderHook(() => useWorkerCodenames(["ow-gone"]));
    expect(second.result.current.get("ow-gone")).toBeUndefined();
    expect(getOutsourceWorker).toHaveBeenCalledTimes(1);
  });

  it("fetches each id once and shares the cache across mounts", async () => {
    getOutsourceWorker.mockResolvedValue({ id: "ow-abc", codename: "X-1" });
    const first = renderHook(() => useWorkerCodenames(["ow-abc"]));
    await waitFor(() => {
      expect(first.result.current.get("ow-abc")).toBe("X-1");
    });
    const second = renderHook(() => useWorkerCodenames(["ow-abc"]));
    expect(second.result.current.get("ow-abc")).toBe("X-1");
    expect(getOutsourceWorker).toHaveBeenCalledTimes(1);
  });
});
