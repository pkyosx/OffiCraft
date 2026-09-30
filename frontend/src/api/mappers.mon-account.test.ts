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

// ── T-3b90: the seam between the server's stamp and what the card renders ────
//
// This block exists because independent verification found the join UNGUARDED:
// replacing both `measuredAt:` lines in `toMonAccount` with `null` left the
// whole suite (205 files / 1724 tests) green, even though the BE half and the
// render half each had their own passing mutants. The entire user-visible fix
// could therefore have gone dead silently — the exact failure shape
// frontend/AGENTS.md already records under "a fake api must not be more
// generous than the real server".
describe("toMonitoring usage-window measured_at (T-3b90)", () => {
  const withWindows = (
    fiveHour: Record<string, unknown> | null,
    sevenDay: Record<string, unknown> | null
  ) =>
    toMonitoring({
      sessions: [],
      machines: [],
      accounts: [
        wireAccount({
          five_hour: fiveHour as never,
          seven_day: sevenDay as never,
        }),
      ],
    }).accounts[0];

  it("carries each window's measured_at through to the view model", () => {
    const a = withWindows(
      { used_pct: 10, elapsed_pct: 40, pace: "ok", measured_at: 1_700_000_000 },
      { used_pct: 43, elapsed_pct: 15.15, pace: null, measured_at: 1_699_000_000 }
    );
    // Asserted as exact numbers, per window. "Not undefined" would pass on a
    // mapper that collapsed both windows onto one account-wide stamp — which
    // is precisely the confusion this ticket was filed about.
    expect(a.fiveHour?.measuredAt).toBe(1_700_000_000);
    expect(a.sevenDay?.measuredAt).toBe(1_699_000_000);
  });

  it("honest-null when the server did not stamp the sample", () => {
    // Never back-filled: an unstamped snapshot dressed up as a live one would
    // answer "is this number old?" with "no" every single time.
    const a = withWindows(null, {
      used_pct: 43,
      elapsed_pct: 15.15,
      pace: null,
      measured_at: null,
    });
    expect(a.sevenDay?.measuredAt).toBeNull();
  });

  it("a withheld pace verdict maps to not-overheated", () => {
    // The other half of the same seam: the card must take the BE's refusal to
    // judge as "no badge", never re-derive heat from the two percentages.
    const a = withWindows(null, {
      used_pct: 43,
      elapsed_pct: 15.15,
      pace: null,
      measured_at: 1_699_000_000,
    });
    expect(a.sevenDay?.overheated).toBe(false);

    // Positive control, so "always false" cannot pass.
    const hot = withWindows(null, {
      used_pct: 80,
      elapsed_pct: 20,
      pace: "hot",
      measured_at: 1_700_000_000,
    });
    expect(hot.sevenDay?.overheated).toBe(true);
  });
});
