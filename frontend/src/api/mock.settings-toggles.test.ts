// Mock adapter parity for the dual-channel updater toggles (settings):
// both OFF out of the box (mirroring the server default), a PATCH flips them
// durably within the session, and each toggle patches independently (PATCH
// semantics — an omitted field never changes).

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";

describe("mock settings — updater dual-channel toggles", () => {
  beforeEach(() => __resetMock());

  it("defaults both toggles OFF and the check interval to 300s", async () => {
    const s = await mockApi.getServerSettings();
    expect(s.updaterReceiveBeta).toBe(false);
    expect(s.updaterAutoUpdate).toBe(false);
    expect(s.updaterCheckIntervalSecs).toBe(300);
  });

  it("under a check interval outside 60..3600 the PATCH is refused with the server's 422 and nothing changes", async () => {
    for (const secs of [59, 3601]) {
      await expect(mockApi.patchServerSettings({ updaterCheckIntervalSecs: secs })).rejects.toMatchObject({
        status: 422,
        serverMessage: "updater_check_interval_secs must be between 60 and 3600 seconds",
      });
    }
    expect((await mockApi.getServerSettings()).updaterCheckIntervalSecs).toBe(300);
    const s = await mockApi.patchServerSettings({ updaterCheckIntervalSecs: 3600 });
    expect(s.updaterCheckIntervalSecs).toBe(3600);
  });

  it("PATCHes each toggle independently and reads it back", async () => {
    let s = await mockApi.patchServerSettings({ updaterReceiveBeta: true });
    expect(s.updaterReceiveBeta).toBe(true);
    expect(s.updaterAutoUpdate).toBe(false); // untouched by the partial patch

    s = await mockApi.patchServerSettings({ updaterAutoUpdate: true });
    expect(s.updaterReceiveBeta).toBe(true); // still on
    expect(s.updaterAutoUpdate).toBe(true);

    s = await mockApi.patchServerSettings({
      updaterReceiveBeta: false,
      updaterAutoUpdate: false,
    });
    expect(s.updaterReceiveBeta).toBe(false);
    expect(s.updaterAutoUpdate).toBe(false);

    // Durable within the session: a fresh GET agrees with the last PATCH.
    const again = await mockApi.getServerSettings();
    expect(again.updaterReceiveBeta).toBe(false);
    expect(again.updaterAutoUpdate).toBe(false);
  });
});
