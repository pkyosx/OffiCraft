import { describe, expect, it } from "vitest";
import { REPLY_SNOOZE_MAX_SECONDS, SNOOZE_PRESETS, snoozeDraft, snoozeUntil } from "./replySnooze";

const NOW = 1_793_000_000.6;

describe("snoozeUntil", () => {
  it("is whole seconds from now: the four presets → +1h, +3h, +1d, +3d", () => {
    expect(SNOOZE_PRESETS.map((p) => snoozeUntil(p.amount, p.unit, NOW) - 1_793_000_000)).toEqual([
      3600, 10_800, 86_400, 259_200,
    ]);
  });
});

describe("snoozeDraft", () => {
  it("a whole number of hours or days up to 90 days → the time it ends at", () => {
    expect(snoozeDraft("5", "hour", NOW)).toEqual({ ok: true, untilTs: 1_793_000_000 + 5 * 3600 });
    expect(snoozeDraft(" 90 ", "day", NOW)).toEqual({
      ok: true,
      untilTs: 1_793_000_000 + REPLY_SNOOZE_MAX_SECONDS,
    });
    expect(snoozeDraft("2160", "hour", NOW)).toEqual({
      ok: true,
      untilTs: 1_793_000_000 + REPLY_SNOOZE_MAX_SECONDS,
    });
  });

  it("past 90 days → tooFar; empty → empty; 0, a fraction or text → invalid", () => {
    expect(
      [
        snoozeDraft("91", "day", NOW),
        snoozeDraft("2161", "hour", NOW),
        snoozeDraft("", "day", NOW),
        snoozeDraft("0", "hour", NOW),
        snoozeDraft("1.5", "hour", NOW),
        snoozeDraft("-2", "day", NOW),
        snoozeDraft("e", "day", NOW),
      ].map((d) => (d.ok ? "ok" : d.reason))
    ).toEqual(["tooFar", "tooFar", "empty", "invalid", "invalid", "invalid", "invalid"]);
  });
});
