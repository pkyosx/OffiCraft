import { describe, it, expect } from "vitest";
import {
  durationSecsFromDraft,
  splitDurationSecs,
  SECRET_CARD_RETRIEVED_RETENTION_SECS_DEFAULT,
  SECRET_CARD_RETRIEVED_RETENTION_SECS_MAX,
  SECRET_CARD_RETRIEVED_RETENTION_SECS_MIN,
  SECRET_CARD_UNRETRIEVED_TTL_SECS_DEFAULT,
  SECRET_CARD_UNRETRIEVED_TTL_SECS_MAX,
  SECRET_CARD_UNRETRIEVED_TTL_SECS_MIN,
} from "./secretCardSettings";

describe("secretCardSettings", () => {
  it("names what the server ships", () => {
    expect([
      SECRET_CARD_UNRETRIEVED_TTL_SECS_DEFAULT,
      SECRET_CARD_UNRETRIEVED_TTL_SECS_MIN,
      SECRET_CARD_UNRETRIEVED_TTL_SECS_MAX,
      SECRET_CARD_RETRIEVED_RETENTION_SECS_DEFAULT,
      SECRET_CARD_RETRIEVED_RETENTION_SECS_MIN,
      SECRET_CARD_RETRIEVED_RETENTION_SECS_MAX,
    ]).toEqual([86400, 60, 2592000, 600, 0, 86400]);
  });

  it.each([
    [86400, { amount: 1, unit: "day" }],
    [7200, { amount: 2, unit: "hour" }],
    [600, { amount: 10, unit: "minute" }],
    [90, { amount: 90, unit: "second" }],
    [0, { amount: 0, unit: "minute" }],
  ])("splits %d seconds", (secs, want) => {
    expect(splitDurationSecs(secs)).toEqual(want);
  });

  it.each([
    ["2", "hour", 7200],
    [" 1 ", "day", 86400],
    ["0", "minute", 0],
    ["", "day", null],
    ["1.5", "hour", null],
    ["abc", "hour", null],
    ["2", "day", null],
  ] as const)("reads %j %s against a 0..86400 range", (amount, unit, want) => {
    expect(durationSecsFromDraft(amount, unit, 0, 86400)).toBe(want);
  });
});
