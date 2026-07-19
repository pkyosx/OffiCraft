// wire→view mapping of the monitoring account row's account_label (T-a9a7):
// present → carried verbatim; absent on the wire (the server's owner-only
// omitempty gate, or an older server) → honest null, never "".

import { describe, it, expect } from "vitest";
import { toMonitoring } from "./mappers";
import type { WireMonitoring } from "./wire";

type WireMonAccountRow = NonNullable<WireMonitoring["accounts"]>[number];

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
