// AuthGate — the login wall must know whether to ask for a TOTP code, on EVERY
// route into it.
//
// 🔴 THE BUG THIS PINS, because it was silent and shipped-looking. `mfaRequired`
// is written ONLY by the auth-status probe, and the probe runs only while the
// wall is "checking". A tab that began logged in starts at "app" and therefore
// never probes, so it carries `false`. Logging out used to jump straight to
// "login" with that stale `false`: the owner got a wall with no code field,
// typed their CORRECT password, received a flat 401, and was told "Incorrect
// password, try again". There is no affordance on that screen to enter a code —
// the only way out is a page reload, which nothing suggests. The first person
// to hit it is whoever turns MFA on and signs out to check that it worked.
//
// Every test here drives the REAL component against a stubbed adapter; none
// asserts on internal state.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";

// The gate only renders its wall in real-backend mode. `USE_MOCK` is computed
// at module-evaluation time from import.meta.env, and ESM imports are hoisted
// above any stubEnv call — so the switch has to be flipped with vi.mock, which
// IS hoisted. The adapter itself is left untouched (the tests spy on it).
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  USE_MOCK: false,
}));

import { AuthGate } from "./AuthGate";
import { api } from "./api";
import { I18nProvider } from "./i18n";
import { zh } from "./i18n/locales/zh";
import { TOKEN_KEY } from "./api/auth";

const L = zh.login;

/** The composed 429 sentence, assembled exactly as i18n/compose.ts does. Built
 * from the same overridable leaves rather than hardcoded, so a wording change
 * moves the test with the UI instead of breaking it. */
function throttledText(secs: number): string {
  return `${L.throttledLead} ${secs} ${L.throttledTail}`;
}

/** AuthGate reaches useI18n through the app it wraps, so it needs the provider. */
function renderGate() {
  return render(
    <I18nProvider>
      <AuthGate />
    </I18nProvider>,
  );
}

/** Stub the public probe. */
function stubProbe(passwordSet: boolean, mfaRequired: boolean) {
  return vi
    .spyOn(api, "getAuthStatus")
    .mockResolvedValue({ passwordSet, mfaRequired, passkeyLoginAvailable: false });
}

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("AuthGate — the wall knows about the second factor", () => {
  it("renders a code field on first paint when MFA is armed", async () => {
    stubProbe(true, true);
    const utils = renderGate();
    expect(await utils.findByLabelText(L.codePlaceholder)).toBeTruthy();
  });

  it("renders NO code field when MFA is off", async () => {
    stubProbe(true, false);
    const utils = renderGate();
    await utils.findByPlaceholderText(L.passwordPlaceholder);
    expect(utils.queryByLabelText(L.codePlaceholder)).toBeNull();
  });

  // 🔴 THE REGRESSION. Start with a token (so the gate boots straight to "app"
  // and never probes), then log out — the wall must still ask for the code.
  it("asks for a code after LOGGING OUT of a session that never probed", async () => {
    localStorage.setItem(TOKEN_KEY, "an-existing-owner-token");
    const probe = stubProbe(true, true);

    const utils = renderGate();
    // Booted into the app: no wall, and (the cause of the bug) no probe yet.
    await waitFor(() => expect(utils.queryByText(L.submit)).toBeNull());
    expect(probe).not.toHaveBeenCalled();

    // Log out the way an owner does: open the profile menu, click 登出.
    fireEvent.click(utils.container.querySelector(".profile-pill")!);
    fireEvent.click(await utils.findByText(zh.profile.logout));

    // The wall must come back WITH its code field, not with a password-only
    // form that will reject the right password and blame the password.
    expect(await utils.findByLabelText(L.codePlaceholder)).toBeTruthy();
  });
});

describe("AuthGate — a wall that is out of date recovers itself", () => {
  // Journey: the owner armed the factor on another device while this tab sat on
  // the login wall. The wall has no code field and cannot get one from a 401
  // alone — without a re-probe this is a permanent "wrong password" loop.
  it("grows a code field when a refused login turns out to need one", async () => {
    // First paint: MFA genuinely off.
    const probe = stubProbe(true, false);
    const utils = renderGate();
    await utils.findByPlaceholderText(L.passwordPlaceholder);
    expect(utils.queryByLabelText(L.codePlaceholder)).toBeNull();

    // Meanwhile, elsewhere, the factor is armed — and the login is refused.
    probe.mockResolvedValue({ passwordSet: true, mfaRequired: true, passkeyLoginAvailable: false });
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({ error: { code: "unauthorized", message: "x" } }),
            { status: 401, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );

    fireEvent.change(utils.getByPlaceholderText(L.passwordPlaceholder), {
      target: { value: "the-right-password" },
    });
    fireEvent.click(utils.getByText(L.submit));

    // It must explain WHY the field appeared…
    await utils.findByText(L.codeNowRequired);
    // …offer the field…
    expect(utils.getByLabelText(L.codePlaceholder)).toBeTruthy();
    // …and NOT accuse the password, which was never established as wrong.
    expect(utils.queryByText(L.error)).toBeNull();

    vi.unstubAllGlobals();
  });

  // Same recovery, different cause: the first-paint probe failed, so the wall
  // fell back to its default of "no code".
  it("recovers when the FIRST-PAINT probe failed and left the default", async () => {
    const probe = vi
      .spyOn(api, "getAuthStatus")
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValue({ passwordSet: true, mfaRequired: true, passkeyLoginAvailable: false });

    const utils = renderGate();
    await utils.findByPlaceholderText(L.passwordPlaceholder);
    expect(utils.queryByLabelText(L.codePlaceholder)).toBeNull();

    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({ error: { code: "unauthorized", message: "x" } }),
            { status: 401, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );
    fireEvent.change(utils.getByPlaceholderText(L.passwordPlaceholder), {
      target: { value: "the-right-password" },
    });
    fireEvent.click(utils.getByText(L.submit));

    await utils.findByText(L.codeNowRequired);
    expect(probe).toHaveBeenCalledTimes(2); // the failed one, then the recovery
    vi.unstubAllGlobals();
  });

  // A 429 must NOT be mistaken for a stale wall: the credential-attempt brake
  // says nothing about whether a code is required.
  it("does not re-probe or blame a code when the attempt was merely throttled", async () => {
    stubProbe(true, false);
    const utils = renderGate();
    await utils.findByPlaceholderText(L.passwordPlaceholder);

    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({ error: { code: "client_error", message: "x" } }),
            {
              status: 429,
              headers: {
                "Content-Type": "application/json",
                "Retry-After": "30",
              },
            },
          ),
      ),
    );
    fireEvent.change(utils.getByPlaceholderText(L.passwordPlaceholder), {
      target: { value: "pw" },
    });
    fireEvent.click(utils.getByText(L.submit));

    await utils.findByText(throttledText(30));
    expect(utils.queryByText(L.codeNowRequired)).toBeNull();
    vi.unstubAllGlobals();
  });
});
