import { beforeEach, describe, expect, it } from "vitest";
import { __resetMock, mockApi } from "./mock";

// T-fc53 第二段: warden credentials expire again, on the org setting
// `auth.warden_credential_lifetime_secs`. This file used to assert the opposite
// (`expiresIn === 0`, the permanent-credential sentinel) and is renamed with the
// contract — the thing it has ALWAYS guarded is that the legacy `ttlDays`
// request option does not reach the warden credential's lifetime, which is still
// true and is still the only reason the option is passed here at all.
describe("mock warden onboarding credential", () => {
  beforeEach(() => __resetMock());

  it("expires on the org setting, and ttlDays does not move it", async () => {
    const ordinary = await mockApi.onboardMachine("Mock One", { ttlDays: 1 });
    const formerlyClamped = await mockApi.onboardMachine("Mock Two", {
      ttlDays: 401,
    });

    const settings = await mockApi.getServerSettings();
    const lifetime = settings.wardenCredentialLifetimeSecs;

    expect(lifetime).toBeGreaterThan(0);
    expect(ordinary.expiresIn).toBe(lifetime);
    expect(formerlyClamped.expiresIn).toBe(lifetime);
  });
});
