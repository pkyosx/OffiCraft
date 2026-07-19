// formatCost (cost.ts) — the single token-cost money formatter (T-075a). Pins
// the owner rule: whole dollars, no decimals, thousands-separated.

import { describe, it, expect } from "vitest";
import { formatCost } from "./cost";

describe("formatCost", () => {
  it("drops the decimals — the old toFixed(4) figure now reads whole-dollar", () => {
    expect(formatCost(12.3456)).toBe("$12");
  });

  it("rounds half-up to the nearest dollar", () => {
    expect(formatCost(0.5)).toBe("$1");
    expect(formatCost(1.49)).toBe("$1");
    expect(formatCost(1.5)).toBe("$2");
  });

  it("shows a sub-50¢ cost as $0 (coarse by design, not a dash)", () => {
    expect(formatCost(0.0034)).toBe("$0");
    expect(formatCost(0)).toBe("$0");
  });

  it("thousands-separates a large cumulative cost", () => {
    expect(formatCost(1234)).toBe("$1,234");
    expect(formatCost(1234567.89)).toBe("$1,234,568");
  });
});
