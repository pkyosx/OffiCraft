import { describe, expect, it } from "vitest";
import { localTestWorkers } from "./local-test-workers";

describe("localTestWorkers", () => {
  it("defaults to 4 when unset or empty", () => {
    expect(localTestWorkers({})).toBe(4);
    expect(localTestWorkers({ OC_LOCAL_TEST_WORKERS: "" })).toBe(4);
  });

  it("uses an explicit positive integer", () => {
    expect(localTestWorkers({ OC_LOCAL_TEST_WORKERS: "2" })).toBe(2);
  });

  it.each(["abc", "0", "-1", "4.5"])("rejects %j instead of running uncapped", (raw) => {
    expect(() => localTestWorkers({ OC_LOCAL_TEST_WORKERS: raw })).toThrow(
      "OC_LOCAL_TEST_WORKERS must be a positive integer",
    );
  });

  it("leaves CI at the runner's default, ignoring the variable", () => {
    expect(localTestWorkers({ CI: "true" })).toBeUndefined();
    expect(localTestWorkers({ CI: "true", OC_LOCAL_TEST_WORKERS: "abc" })).toBeUndefined();
  });
});
