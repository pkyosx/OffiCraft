// The mock's 強制停止 must pass through the window the real server shows: the
// force-stop is published while the session is still connected (presence
// `stopping`, refocus_op cleared, forced_stop_live true), and only later does
// the session drop. A mock that jumped straight to the end would hide that
// window from every panel test that drives the mock.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  mockApi,
  __resetMock,
  __injectMockMember,
  __injectMockOutsourceWorker,
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

  it("a wake pressed inside the window is not overwritten by the old session's disconnect", async () => {
    __injectMockMember({
      id: "kip",
      kind: "staff",
      presence: "online",
      desired_state: "online",
      forced_stop_live: false,
    });

    await mockApi.forceStopMember("kip");
    await mockApi.activateMember("kip");
    vi.advanceTimersByTime(MOCK_FORCED_STOP_DISCONNECT_MS);
    expect(ladderFacts(await mockApi.getMember("kip"))).toEqual({
      lifecycle: "waking",
      desiredState: "online",
      refocusOp: "",
      forcedStopLive: false,
    });
  });

  it("加速停止 inside the window answers the server's 409", async () => {
    __injectMockMember({
      id: "kip",
      kind: "staff",
      presence: "stopping",
      desired_state: "offline",
      refocus_op: "accelerated_stop",
      forced_stop_live: false,
    });

    await mockApi.forceStopMember("kip");
    await expect(mockApi.acceleratedStopMember("kip")).rejects.toMatchObject({
      message: "http 409 for POST /api/members/{id}/accelerated-stop",
      status: 409,
      code: "conflict",
      serverMessage:
        "加速停止 has nothing to escalate — this member was already force-stopped (強制停止): its session was cut off and no wind-down is open",
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

describe("mock forceStopOutsourceMember", () => {
  it("holds a connected worker in stopping with forced_stop_live, then drops it, and a wake clears it", async () => {
    __injectMockOutsourceWorker({
      id: "ow-1",
      codename: "O-1",
      model: "opus",
      effort: "medium",
      status: "active",
      taskId: "t-1",
      taskTitle: "",
      taskStatus: "in_progress",
      createdTs: 1_790_000_000,
      presence: "online",
      desiredState: "online",
      refocusOp: "accelerated_stop",
      forcedStopLive: false,
      machine: "",
      desiredMachineId: "",
      account: null,
      contextPct: null,
      cost: null,
      bankedCost: null,
      lastOp: "",
      lastOpOk: null,
      lastOpLog: "",
      lastOpReason: "",
      lastOpAt: null,
      creatorId: "",
      delegatedBy: "",
    });
    const facts = async () => {
      const w = await mockApi.getOutsourceWorker("ow-1");
      return {
        presence: w.presence,
        desiredState: w.desiredState,
        refocusOp: w.refocusOp ?? "",
        forcedStopLive: w.forcedStopLive,
      };
    };

    await mockApi.forceStopMember("ow-1");
    expect(await facts()).toEqual({
      presence: "stopping",
      desiredState: "offline",
      refocusOp: "",
      forcedStopLive: true,
    });
    await expect(mockApi.acceleratedStopMember("ow-1")).rejects.toMatchObject({
      message: "http 409 for POST /api/members/ow-1/accelerated-stop",
      status: 409,
      code: "conflict",
      serverMessage:
        "加速停止 has nothing to escalate — this member was already force-stopped (強制停止): its session was cut off and no wind-down is open",
    });

    vi.advanceTimersByTime(MOCK_FORCED_STOP_DISCONNECT_MS);
    expect(await facts()).toEqual({
      presence: "stopped",
      desiredState: "offline",
      refocusOp: "",
      forcedStopLive: true,
    });

    await mockApi.activateMember("ow-1");
    expect(await facts()).toEqual({
      presence: "waking",
      desiredState: "online",
      refocusOp: "",
      forcedStopLive: false,
    });
  });
});
