import { describe, it, expect } from "vitest";
import {
  dueRemaining,
  dueState,
  dueTsFromInputs,
  effectiveLeadMinutes,
  isDueSoon,
  leadMinutesFromDraft,
  nextDueChangeTs,
  nextDueRemainingChangeTs,
  nextDueWake,
  splitLeadMinutes,
  toTimeInputValue,
} from "./dueDate";

// 2026-10-24 10:00:00 Asia/Taipei (the suite's TZ).
const NOW = 1792807200;
const OCT_30_END = 1793375999;
const OCT_30_1430 = 1793341800;
const OCT_22_1430 = 1792650600;

describe("effectiveLeadMinutes", () => {
  it("answers the task's own lead when it has one", () => {
    expect(effectiveLeadMinutes(10080, 2880)).toBe(10080);
  });

  it("answers the workspace lead for a task without one", () => {
    expect(effectiveLeadMinutes(null, 2880)).toBe(2880);
    expect(effectiveLeadMinutes(undefined, 2880)).toBe(2880);
  });
});

describe("splitLeadMinutes", () => {
  it("answers the largest unit that divides the lead exactly", () => {
    expect(splitLeadMinutes(1440)).toEqual({ amount: 1, unit: "day" });
    expect(splitLeadMinutes(43200)).toEqual({ amount: 30, unit: "day" });
    expect(splitLeadMinutes(120)).toEqual({ amount: 2, unit: "hour" });
    expect(splitLeadMinutes(1500)).toEqual({ amount: 25, unit: "hour" });
    expect(splitLeadMinutes(90)).toEqual({ amount: 90, unit: "minute" });
  });
});

describe("leadMinutesFromDraft", () => {
  it("converts a whole amount in its unit to minutes and an empty draft to null", () => {
    expect(leadMinutesFromDraft("2", "hour")).toBe(120);
    expect(leadMinutesFromDraft("1", "day")).toBe(1440);
    expect(leadMinutesFromDraft("30", "day")).toBe(43200);
    expect(leadMinutesFromDraft("1", "minute")).toBe(1);
    expect(leadMinutesFromDraft("", "day")).toBeNull();
  });

  it("refuses 0, a fraction, anything past 30 days and unparseable text", () => {
    expect(leadMinutesFromDraft("0", "minute")).toBeUndefined();
    expect(leadMinutesFromDraft("1.5", "hour")).toBeUndefined();
    expect(leadMinutesFromDraft("43201", "minute")).toBeUndefined();
    expect(leadMinutesFromDraft("31", "day")).toBeUndefined();
    expect(leadMinutesFromDraft("721", "hour")).toBeUndefined();
    expect(leadMinutesFromDraft("e", "day")).toBeUndefined();
  });
});

describe("isDueSoon", () => {
  it("is true from exactly the lead's minutes before the due date, and false a second earlier", () => {
    expect(isDueSoon(NOW + 90 * 60, NOW, 90)).toBe(true);
    expect(isDueSoon(NOW + 90 * 60 + 1, NOW, 90)).toBe(false);
  });
});

describe("dueState", () => {
  it("is soon inside the lead, later outside it, and counts calendar days once overdue", () => {
    expect(dueState(OCT_30_END, NOW, 10080)).toEqual({ kind: "soon" });
    expect(dueState(OCT_30_END, NOW, 4320)).toEqual({ kind: "later" });
    expect(dueState(OCT_30_END, OCT_30_END + 2 * 86_400, 4320)).toEqual({ kind: "overdue", days: 2 });
    expect(dueState(OCT_22_1430, NOW, 4320)).toEqual({ kind: "overdue", days: 2 });
  });
});

describe("nextDueChangeTs", () => {
  it("is the start of the lead window, then the due moment", () => {
    expect(nextDueChangeTs(OCT_30_END, NOW, 4320)).toBe(OCT_30_END - 3 * 86_400);
    expect(nextDueChangeTs(OCT_30_END, OCT_30_END - 60, 4320)).toBe(OCT_30_END);
  });
});

describe("dueRemaining", () => {
  const left = (amount: number, unit: string) => ({ overdue: false, amount, unit });
  const over = (amount: number, unit: string) => ({ overdue: true, amount, unit });

  it("counts calendar days from a full day out, whole hours under a day and whole minutes under an hour", () => {
    expect(dueRemaining(OCT_30_END, NOW)).toEqual(left(6, "day"));
    expect(dueRemaining(NOW + 86_400, NOW)).toEqual(left(1, "day"));
    expect(dueRemaining(NOW + 86_399, NOW)).toEqual(left(23, "hour"));
    expect(dueRemaining(NOW + 3600, NOW)).toEqual(left(1, "hour"));
    expect(dueRemaining(NOW + 3599, NOW)).toEqual(left(59, "minute"));
    expect(dueRemaining(NOW + 30, NOW)).toEqual(left(1, "minute"));
  });

  it("counts calendar days once overdue past local midnight, and hours or minutes earlier the same day", () => {
    expect(dueRemaining(OCT_22_1430, NOW)).toEqual(over(2, "day"));
    expect(dueRemaining(NOW - 5 * 3600 - 1, NOW)).toEqual(over(5, "hour"));
    expect(dueRemaining(NOW - 25 * 60, NOW)).toEqual(over(25, "minute"));
    expect(dueRemaining(NOW, NOW)).toEqual(over(1, "minute"));
  });
});

describe("nextDueRemainingChangeTs", () => {
  it("is the first second the remaining time reads differently", () => {
    expect(nextDueRemainingChangeTs(OCT_30_END, NOW)).toBe(OCT_30_END - 86_400 + 1);
    expect(nextDueRemainingChangeTs(NOW + 5 * 3600 + 10, NOW)).toBe(NOW + 11);
    expect(nextDueRemainingChangeTs(NOW + 25 * 60 + 10, NOW)).toBe(NOW + 11);
    expect(nextDueRemainingChangeTs(NOW + 90, NOW)).toBe(NOW + 90);
    expect(nextDueRemainingChangeTs(NOW - 30, NOW)).toBe(NOW + 90);
    expect(nextDueRemainingChangeTs(NOW - 25 * 60 - 10, NOW)).toBe(NOW + 50);
    expect(nextDueRemainingChangeTs(NOW - 5 * 3600 - 10, NOW)).toBe(NOW + 3590);
  });

  it("is null once overdue past local midnight, which useNowSeconds already wakes for", () => {
    expect(nextDueRemainingChangeTs(OCT_22_1430, NOW)).toBeNull();
  });
});

describe("nextDueWake", () => {
  it("is the earlier of the colour change and the remaining-time change", () => {
    expect(nextDueWake(OCT_30_END, NOW, 4320)).toBe(OCT_30_END - 3 * 86_400);
    expect(nextDueWake(OCT_30_END, NOW, 60)).toBe(OCT_30_END - 86_400 + 1);
    expect(nextDueWake(OCT_22_1430, NOW, 60)).toBeNull();
  });
});

describe("dueTsFromInputs", () => {
  it("is 23:59:59 local of the date without a time, and that local minute with one", () => {
    expect(dueTsFromInputs("2026-10-30", "")).toBe(OCT_30_END);
    expect(dueTsFromInputs("2026-10-30", "14:30")).toBe(OCT_30_1430);
    expect(dueTsFromInputs("", "14:30")).toBeNull();
  });
});

describe("toTimeInputValue", () => {
  it("is empty for 23:59:59 local and HH:MM otherwise", () => {
    expect(toTimeInputValue(OCT_30_END)).toBe("");
    expect(toTimeInputValue(OCT_30_1430)).toBe("14:30");
    expect(toTimeInputValue(OCT_30_END - 59)).toBe("23:59");
  });
});
