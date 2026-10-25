import { describe, it, expect } from "vitest";
import {
  dueState,
  dueTsFromInputs,
  effectiveLeadDays,
  isDueSoon,
  nextDueChangeTs,
  toTimeInputValue,
} from "./dueDate";

// 2026-10-24 10:00:00 Asia/Taipei (the suite's TZ).
const NOW = 1792807200;
const OCT_30_END = 1793375999;
const OCT_30_1430 = 1793341800;
const OCT_22_1430 = 1792650600;

describe("effectiveLeadDays", () => {
  it("answers the task's own lead when it has one", () => {
    expect(effectiveLeadDays(7, 2)).toBe(7);
  });

  it("answers the workspace lead for a task without one", () => {
    expect(effectiveLeadDays(null, 2)).toBe(2);
    expect(effectiveLeadDays(undefined, 2)).toBe(2);
  });
});

describe("isDueSoon", () => {
  it("is true from exactly the lead's days before the due date, and false a second earlier", () => {
    expect(isDueSoon(NOW + 3 * 86_400, NOW, 3)).toBe(true);
    expect(isDueSoon(NOW + 3 * 86_400 + 1, NOW, 3)).toBe(false);
  });
});

describe("dueState", () => {
  it("is soon inside the lead, later outside it, and counts calendar days once overdue", () => {
    expect(dueState(OCT_30_END, NOW, 7)).toEqual({ kind: "soon" });
    expect(dueState(OCT_30_END, NOW, 3)).toEqual({ kind: "later" });
    expect(dueState(OCT_30_END, OCT_30_END + 2 * 86_400, 3)).toEqual({ kind: "overdue", days: 2 });
    expect(dueState(OCT_22_1430, NOW, 3)).toEqual({ kind: "overdue", days: 2 });
  });
});

describe("nextDueChangeTs", () => {
  it("is the start of the lead window, then the due moment", () => {
    expect(nextDueChangeTs(OCT_30_END, NOW, 3)).toBe(OCT_30_END - 3 * 86_400);
    expect(nextDueChangeTs(OCT_30_END, OCT_30_END - 60, 3)).toBe(OCT_30_END);
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
