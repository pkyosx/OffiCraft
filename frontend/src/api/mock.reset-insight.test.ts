// The mock adapter's reset_insight path (T-6501). The mock is what every
// cockpit test runs against, so a mock that is wrong in the same way as a buggy
// server would let those tests go green over the defect — which is the reason
// the existing insight seed code in mock.ts carries that warning verbatim.
//
// 🔴 EVERY ASSERTION BELOW WOULD BE TAUTOLOGICALLY TRUE if the overlay happened
// to equal the seed. That is not a hypothetical: T-6501 exists because a
// written overlay that was byte-identical to the shipped seed (317 == 317) let
// two people misread this document's state on 2026-08-04. So each test asserts
// the overlay DIFFERS from the seed before it asserts the reset moved anything.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { isHttpStatus } from "./errors";
import { documentRevisions } from "../test/documentHistory";

const WRITTEN = "# Insight — 這一份是角色自己寫的，不是出廠版。\n";

beforeEach(() => {
  __resetMock();
});

describe("mockApi · resetInsight", () => {
  it("puts the per-role factory seed back and flips isDefault", async () => {
    const seed = await mockApi.getInsight("assistant");
    expect(seed.isDefault).toBe(true);
    // ANTI-TAUTOLOGY: a seed that is empty, or an overlay equal to it, would
    // satisfy everything below for the wrong reason.
    expect(seed.text.trim()).not.toBe("");
    expect(WRITTEN).not.toBe(seed.text);

    await mockApi.saveInsight("assistant", WRITTEN);
    const written = await mockApi.getInsight("assistant");
    expect(written.text).toBe(WRITTEN);
    expect(written.isDefault).toBe(false);

    const reset = await mockApi.resetInsight("assistant");
    expect(reset.text).toBe(seed.text);
    expect(reset.isDefault).toBe(true);
    expect(reset.sizeChars).toBe(seed.sizeChars);
    // The read face agrees — the response is not a one-off projection.
    expect(await mockApi.getInsight("assistant")).toEqual(reset);
  });

  it("retains the overlay it discarded, not the seed it restored", async () => {
    const seed = await mockApi.getInsight("assistant");
    await mockApi.saveInsight("assistant", WRITTEN);
    // The first write replaced a default document, which retains nothing (the
    // server skips the empty snapshot) — so anything found below came from the
    // reset itself.
    expect(await documentRevisions(mockApi, "insight", "assistant")).toEqual(
      []
    );

    await mockApi.resetInsight("assistant");

    const [newest] = await documentRevisions(mockApi, "insight", "assistant");
    expect(newest).toBeDefined();
    expect(newest.content.text).not.toBe(seed.text);
    expect(newest.content.text).toBe(WRITTEN);
  });

  it("404s for a role with no factory insight of its own", async () => {
    // POSITIVE CONTROL: the seeded role must succeed here, or a mock where
    // seeds simply do not resolve would satisfy the rejection below.
    await expect(mockApi.resetInsight("assistant")).resolves.toBeDefined();

    await expect(mockApi.resetInsight("r-tester")).rejects.toSatisfy(
      (e: unknown) => isHttpStatus(e, 404)
    );
  });

  it("is idempotent", async () => {
    const seed = await mockApi.getInsight("assistant");
    await mockApi.saveInsight("assistant", WRITTEN);
    for (let i = 0; i < 3; i++) await mockApi.resetInsight("assistant");
    const after = await mockApi.getInsight("assistant");
    expect(after.text).toBe(seed.text);
    expect(after.isDefault).toBe(true);
  });
});
