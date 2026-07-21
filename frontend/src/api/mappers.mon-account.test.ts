// wire→view mapping of the monitoring account row's account_label (T-a9a7):
// present → carried verbatim; absent on the wire (the server's owner-only
// omitempty gate, or an older server) → honest null, never "".

import { describe, it, expect } from "vitest";
import { toMonitoring } from "./mappers";
import type { WireMonitoring, WireMonSession } from "./wire";

type WireMonAccountRow = NonNullable<WireMonitoring["accounts"]>[number];

const wireSession = (over: Partial<WireMonSession> = {}): WireMonSession => ({
  id: "mira",
  name: "Mira",
  role: "assistant",
  model: "claude-opus-4-8",
  effort: "",
  machine: "mbp5",
  account: "",
  presence: "online",
  context_pct: null,
  cost: null,
  banked_cost: null,
  tokens: null,
  stuck: true,
  idle_secs: 1200,
  ...over,
});

const wireAccount = (
  over: Partial<WireMonAccountRow> = {}
): WireMonAccountRow => ({
  account: "acct-123/9f8e-uuid",
  account_label: null,
  display_name: "acct-123/9f8e-uuid",
  machine: "mbp5",
  cost: null,
  five_hour: null,
  seven_day: null,
  ...over,
});

describe("toMonitoring account_label", () => {
  it("maps a present label verbatim", () => {
    const v = toMonitoring({
      sessions: [],
      machines: [],
      accounts: [wireAccount({ account_label: "eva@example.test(Example Org)" })],
    });
    expect(v.accounts[0].accountLabel).toBe("eva@example.test(Example Org)");
  });

  it("null label → null (owner saw no report)", () => {
    const v = toMonitoring({
      sessions: [],
      machines: [],
      accounts: [wireAccount()],
    });
    expect(v.accounts[0].accountLabel).toBeNull();
  });

  it("key ABSENT on the wire (non-owner / older server) → null, never ''", () => {
    const row = wireAccount();
    delete (row as Record<string, unknown>).account_label;
    const v = toMonitoring({ sessions: [], machines: [], accounts: [row] });
    expect(v.accounts[0].accountLabel).toBeNull();
  });
});

describe("toMonitoring session stuck/idle", () => {
  it("carries stuck=true and idleSecs through", () => {
    const v = toMonitoring({
      sessions: [wireSession({ stuck: true, idle_secs: 1200 })],
      machines: [],
      accounts: [],
    });
    expect(v.sessions[0].stuck).toBe(true);
    expect(v.sessions[0].idleSecs).toBe(1200);
  });

  it("stuck absent on the wire (older server) → false, idle_secs absent → null", () => {
    const row = wireSession();
    delete (row as Record<string, unknown>).stuck;
    delete (row as Record<string, unknown>).idle_secs;
    const v = toMonitoring({ sessions: [row], machines: [], accounts: [] });
    expect(v.sessions[0].stuck).toBe(false);
    expect(v.sessions[0].idleSecs).toBeNull();
  });
});
