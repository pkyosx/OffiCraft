import { describe, it, expect } from "vitest";
import { shareExpiryDraft, SHARE_LINK_MAX_SECONDS } from "./shareLinkExpiry";

const NOW = 1792650600.7;

describe("shareExpiryDraft", () => {
  it.each([
    ["7", "day", Math.floor(NOW) + 7 * 86_400],
    ["3", "hour", Math.floor(NOW) + 3 * 3600],
    ["365", "day", Math.floor(NOW) + SHARE_LINK_MAX_SECONDS],
    ["8760", "hour", Math.floor(NOW) + SHARE_LINK_MAX_SECONDS],
  ] as const)("%s %s → an absolute time", (amount, unit, expiresTs) => {
    expect(shareExpiryDraft(amount, unit, NOW)).toEqual({ ok: true, expiresTs });
  });

  it("is null for 永久, whatever the number field holds", () => {
    expect(shareExpiryDraft("", "forever", NOW)).toEqual({ ok: true, expiresTs: null });
  });

  it.each([
    ["", "day", "empty"],
    ["0", "day", "invalid"],
    ["1.5", "day", "invalid"],
    ["-1", "hour", "invalid"],
    ["366", "day", "tooFar"],
    ["8761", "hour", "tooFar"],
  ] as const)("refuses %j %s as %s", (amount, unit, reason) => {
    expect(shareExpiryDraft(amount, unit, NOW)).toEqual({ ok: false, reason });
  });
});
