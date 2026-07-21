// Mock adapter parity for the owner nickname (settings; T-0b41): "" out of the
// box, a PATCH trims + persists it within the session (so a reload reads it
// back), an over-cap value 422s (writing nothing), and an empty value clears
// it. Mirrors the studio-name (org_name) mock parity. Unlike org_name the
// nickname never enters the agent read path, so there is no global-context leg.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { ApiError } from "./errors";

describe("mock settings — owner nickname (owner_name)", () => {
  beforeEach(() => __resetMock());

  it("defaults owner_name to \"\"", async () => {
    const s = await mockApi.getServerSettings();
    expect(s.ownerName).toBe("");
  });

  it("PATCHes owner_name (trimmed) and reads it back durably", async () => {
    const s = await mockApi.patchServerSettings({ ownerName: "  伊娃  " });
    expect(s.ownerName).toBe("伊娃");
    const again = await mockApi.getServerSettings();
    expect(again.ownerName).toBe("伊娃");
  });

  it("422s an owner_name over the 80-rune cap, writing nothing", async () => {
    const long = "水".repeat(81);
    await expect(
      mockApi.patchServerSettings({ ownerName: long })
    ).rejects.toBeInstanceOf(ApiError);
    const s = await mockApi.getServerSettings();
    expect(s.ownerName).toBe(""); // unchanged
  });

  it("clears owner_name back to \"\" on an empty patch value", async () => {
    await mockApi.patchServerSettings({ ownerName: "暫定暱稱" });
    const cleared = await mockApi.patchServerSettings({ ownerName: "" });
    expect(cleared.ownerName).toBe("");
  });
});
