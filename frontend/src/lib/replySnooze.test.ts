import { describe, expect, it } from "vitest";
import { snoozeUntil } from "./replySnooze";

const local = (y: number, mo: number, d: number, h: number, mi = 0) =>
  new Date(y, mo - 1, d, h, mi).getTime() / 1000;

describe("snoozeUntil", () => {
  // 2026-11-05 is a Thursday.
  const now = new Date(2026, 10, 5, 22, 30);

  it("counts hours from now", () => {
    expect(snoozeUntil("1h", now)).toBe(local(2026, 11, 5, 23, 30));
    expect(snoozeUntil("3h", now)).toBe(local(2026, 11, 6, 1, 30));
  });

  it("puts 明天 09:00 on the next calendar day in local time", () => {
    expect(snoozeUntil("tomorrow", now)).toBe(local(2026, 11, 6, 9));
    expect(snoozeUntil("tomorrow", new Date(2026, 11, 31, 8))).toBe(local(2027, 1, 1, 9));
  });

  it("puts 下週一 09:00 on the first Monday after today", () => {
    expect(snoozeUntil("nextMonday", now)).toBe(local(2026, 11, 9, 9));
    expect(snoozeUntil("nextMonday", new Date(2026, 10, 9, 8))).toBe(local(2026, 11, 16, 9));
    expect(snoozeUntil("nextMonday", new Date(2026, 10, 8, 20))).toBe(local(2026, 11, 9, 9));
  });
});
