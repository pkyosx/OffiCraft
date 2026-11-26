import { describe, it, expect } from "vitest";
import { formatDurationShort } from "./duration";

describe("formatDurationShort", () => {
  it.each([
    [0, "1m"],
    [59 * 60 + 59, "59m"],
    [3600, "1h"],
    [23 * 3600 + 59 * 60, "23h"],
    [86400, "1d"],
    [999 * 86400 + 23 * 3600, "999d"],
  ])("%i seconds → %s, one unit only", (seconds, expected) => {
    expect(formatDurationShort(seconds)).toBe(expected);
  });
});
