import { describe, expect, it } from "vitest";
import { composeSeedFor } from "./composeSeed";

describe("composeSeedFor", () => {
  it("names a 傳承 id as LoreID and leaves a task number bare", () => {
    expect(composeSeedFor("L-212")).toBe("[LoreID=L-212] ");
    expect(composeSeedFor("T-725")).toBe("[T-725] ");
  });
});
