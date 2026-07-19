// Mock adapter parity for the studio name (settings; T-d693): "" out of the
// box, a PATCH trims + persists it within the session, an over-cap value 422s
// (writing nothing), and an empty value clears it. (The agent read path —
// get_global_context carrying org_name — is server-side; the frontend view
// model deliberately omits it, so that leg is pinned by the Go tests.)

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { ApiError } from "./errors";

describe("mock settings — studio name (org_name)", () => {
  beforeEach(() => __resetMock());

  it("defaults org_name to \"\"", async () => {
    const s = await mockApi.getServerSettings();
    expect(s.orgName).toBe("");
  });

  it("PATCHes org_name (trimmed) and reads it back durably", async () => {
    const s = await mockApi.patchServerSettings({ orgName: "  伊娃工作室  " });
    expect(s.orgName).toBe("伊娃工作室");
    const again = await mockApi.getServerSettings();
    expect(again.orgName).toBe("伊娃工作室");
  });

  it("422s an org_name over the 80-rune cap, writing nothing", async () => {
    const long = "水".repeat(81);
    await expect(
      mockApi.patchServerSettings({ orgName: long })
    ).rejects.toBeInstanceOf(ApiError);
    const s = await mockApi.getServerSettings();
    expect(s.orgName).toBe(""); // unchanged
  });

  it("clears org_name back to \"\" on an empty patch value", async () => {
    await mockApi.patchServerSettings({ orgName: "暫定名" });
    const cleared = await mockApi.patchServerSettings({ orgName: "" });
    expect(cleared.orgName).toBe("");
  });
});
