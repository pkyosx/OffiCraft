// The mock's 強制停止 must pass through the window the real server shows: the
// force-stop is published while the session is still connected (presence
// `stopping`, refocus_op cleared, forced_stop_live true), and only later does
// the session drop. A mock that jumped straight to the end hid that window from
// every panel test, which is how the ladder falling back to 加速停止 shipped.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  mockApi,
  __resetMock,
  __injectMockMember,
  MOCK_FORCED_STOP_DISCONNECT_MS,
} from "./mock";

beforeEach(() => {
  __resetMock();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

function ladderFacts(m: Awaited<ReturnType<typeof mockApi.getMember>>) {
  return {
    lifecycle: m.lifecycle,
    desiredState: m.desiredState,
    refocusOp: m.refocusOp,
    forcedStopLive: m.forcedStopLive,
  };
}

describe("mock forceStopMember (staff)", () => {
  it("holds a connected session in stopping with forced_stop_live, then drops it", async () => {
    __injectMockMember({
      id: "kip",
      kind: "staff",
      presence: "stopping",
      desired_state: "offline",
      refocus_op: "accelerated_stop",
      forced_stop_live: false,
    });

    await mockApi.forceStopMember("kip");
    expect(ladderFacts(await mockApi.getMember("kip"))).toEqual({
      lifecycle: "stopping",
      desiredState: "offline",
      refocusOp: "",
      forcedStopLive: true,
    });

    vi.advanceTimersByTime(MOCK_FORCED_STOP_DISCONNECT_MS);
    expect(ladderFacts(await mockApi.getMember("kip"))).toEqual({
      lifecycle: "offline",
      desiredState: "offline",
      refocusOp: "",
      forcedStopLive: true,
    });
  });

  it("a member with no session lands offline at once and a wake clears forced_stop_live", async () => {
    __injectMockMember({
      id: "kip",
      kind: "staff",
      presence: "offline",
      desired_state: "online",
      forced_stop_live: false,
    });

    await mockApi.forceStopMember("kip");
    expect(ladderFacts(await mockApi.getMember("kip"))).toEqual({
      lifecycle: "offline",
      desiredState: "offline",
      refocusOp: "",
      forcedStopLive: true,
    });

    await mockApi.activateMember("kip");
    expect(ladderFacts(await mockApi.getMember("kip"))).toEqual({
      lifecycle: "waking",
      desiredState: "online",
      refocusOp: "",
      forcedStopLive: false,
    });
  });
});
