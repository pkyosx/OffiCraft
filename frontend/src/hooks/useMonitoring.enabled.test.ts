// useMonitoring enabled-gating (T-ec2c) black-box pins.
//
// The monitoring fold is a large payload the server re-signals on EVERY agent
// telemetry heartbeat (topic "monitoring" ⊂ "monitor"). The office page only
// needs it while a member detail panel is open, so it passes enabled=false
// otherwise — and a disabled hook must make ZERO requests and hold NO
// subscription (merely being on the office page must not stream monitoring).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

const h = vi.hoisted(() => ({
  getMonitoring: vi.fn<() => Promise<unknown>>(),
  subscribed: 0,
  sseHandler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    getMonitoring: h.getMonitoring,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.subscribed += 1;
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useMonitoring } from "./useMonitoring";

beforeEach(() => {
  h.getMonitoring.mockReset().mockResolvedValue({ sessions: [] });
  h.subscribed = 0;
  h.sseHandler = null;
});

function emit(topic: string) {
  act(() => {
    h.sseHandler?.(topic);
  });
}

describe("useMonitoring enabled (default, e.g. Monitor page)", () => {
  it("fetches on mount and refetches on a monitor heartbeat", async () => {
    renderHook(() => useMonitoring());
    await waitFor(() => expect(h.getMonitoring).toHaveBeenCalledTimes(1));
    emit("monitoring");
    await waitFor(() => expect(h.getMonitoring).toHaveBeenCalledTimes(2));
  });
});

describe("useMonitoring disabled (T-ec2c, office page w/ no detail panel)", () => {
  it("makes NO request and holds NO subscription", async () => {
    const { result } = renderHook(() => useMonitoring({ enabled: false }));
    // Let any errant mount fetch settle, then assert none happened.
    await Promise.resolve();
    // LOAD-BEARING negatives. MUTANT: make the effect ignore `enabled` (always
    // fetch/subscribe) and BOTH of these go red.
    expect(h.getMonitoring).not.toHaveBeenCalled();
    expect(h.subscribed).toBe(0);
    // A telemetry heartbeat cannot reach a hook that never subscribed.
    emit("monitoring");
    expect(h.getMonitoring).not.toHaveBeenCalled();
    // Disabled must not hang on a spinner.
    expect(result.current.loading).toBe(false);
  });

  it("starts fetching when it flips enabled (detail panel opened)", async () => {
    const { rerender } = renderHook(
      ({ on }: { on: boolean }) => useMonitoring({ enabled: on }),
      { initialProps: { on: false } }
    );
    await Promise.resolve();
    expect(h.getMonitoring).not.toHaveBeenCalled();

    rerender({ on: true });
    await waitFor(() => expect(h.getMonitoring).toHaveBeenCalledTimes(1));
    expect(h.subscribed).toBe(1);
  });
});
