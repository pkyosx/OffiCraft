import { describe, it, expect, vi, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useNowSeconds } from "./useNowSeconds";

const NOW = 1792807200;

afterEach(() => {
  vi.useRealTimers();
});

describe("useNowSeconds", () => {
  it("re-renders at wakeAt", () => {
    vi.useFakeTimers({ now: NOW * 1000 });
    let renders = 0;
    renderHook(() => {
      renders += 1;
      return useNowSeconds(NOW + 60);
    });
    const before = renders;
    act(() => {
      vi.advanceTimersByTime(59_999);
    });
    expect(renders).toBe(before);
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(renders).toBe(before + 1);
  });

  it("re-renders at once when wakeAt has already passed by the time it is armed", () => {
    vi.useFakeTimers({ now: NOW * 1000 });
    let renders = 0;
    renderHook(() => {
      renders += 1;
      return useNowSeconds(NOW - 0.001);
    });
    const before = renders;
    act(() => {
      vi.advanceTimersByTime(0);
    });
    expect(renders).toBe(before + 1);
  });
});
