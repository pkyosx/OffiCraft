// Mock adapter parity for the dual-channel updater toggles (settings):
// both OFF out of the box (mirroring the server default), a PATCH flips them
// durably within the session, and each toggle patches independently (PATCH
// semantics — an omitted field never changes).

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";

describe("mock settings — updater dual-channel toggles", () => {
  beforeEach(() => __resetMock());

  it("defaults both toggles OFF", async () => {
    const s = await mockApi.getServerSettings();
    expect(s.updaterReceiveBeta).toBe(false);
    expect(s.updaterAutoUpdate).toBe(false);
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
