// lib/runtime — the ONE join from live monitoring telemetry onto an agent.
//
// The effort half is the reason this file exists: a member DTO has no reported
// effort at all, so the panel showing `member.effort` (the owner's launch
// intent) looked exactly like a real readout and nobody could tell it was never
// the running value. The join below is where "reported" is separated from
// "configured", and `isReportingTelemetry` is what lets a blank readout say
// which of the two blanks it is.

import { describe, it, expect } from "vitest";
import { joinSessionRuntime, findSessionFor, isReportingTelemetry } from "./runtime";
import type { Member, MonSessionView } from "../types";

function mkSession(over: Partial<MonSessionView> = {}): MonSessionView {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    model: "opus",
    effort: "",
    machine: "mbp5",
    account: "",
    runtime: "claude",
    status: "online",
    contextPct: null,
    compactionCount: null,
    cost: null,
    bankedCost: null,
    ...over,
  };
}

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

describe("joinSessionRuntime", () => {
  it("folds the session's reported effort onto actualEffort, leaving the configured effort alone", () => {
    const joined = joinSessionRuntime(
      mkMember({ effort: "medium" }),
      [mkSession({ effort: "high", contextPct: 42, cost: 3.5 })]
    );
    expect(joined.actualEffort).toBe("high");
    expect(joined.effort).toBe("medium");
    expect(joined.contextPct).toBe(42);
    expect(joined.estimatedCost).toBe(3.5);
  });

  it("leaves actualEffort absent when the session reported none — never the configured effort", () => {
    const joined = joinSessionRuntime(
      mkMember({ effort: "medium" }),
      [mkSession({ effort: "" })]
    );
    expect(joined.actualEffort).toBeUndefined();
  });

  it("returns the member untouched when no session carries its id", () => {
    const member = mkMember();
    expect(joinSessionRuntime(member, [mkSession({ id: "other" })])).toBe(member);
  });
});

describe("findSessionFor", () => {
  it("resolves a worker's row out of the same array the member rows come from", () => {
    const worker = mkSession({ id: "ow-1", model: "sonnet" });
    const sessions = [mkSession(), worker];
    expect(findSessionFor("ow-1", sessions)).toBe(worker);
    expect(findSessionFor("ow-missing", sessions)).toBeUndefined();
  });
});

describe("isReportingTelemetry", () => {
  it("is true only when an online session has landed some other telemetry value", () => {
    expect(isReportingTelemetry(mkSession({ contextPct: 42 }))).toBe(true);
    expect(isReportingTelemetry(mkSession({ cost: 0.5 }))).toBe(true);
    expect(isReportingTelemetry(mkSession({ account: "eva@example.test" }))).toBe(true);
  });

  it("is false for an offline session, a bare online one, and no session at all", () => {
    expect(isReportingTelemetry(mkSession({ status: "offline", contextPct: 42 }))).toBe(false);
    expect(isReportingTelemetry(mkSession())).toBe(false);
    expect(isReportingTelemetry(undefined)).toBe(false);
  });
});
