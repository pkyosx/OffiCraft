// ProfileDropdown → 兩步驟驗證: the owner-facing enrol → activate → disable
// ceremony, exercised against the mock adapter (which mirrors the server's
// check ORDER and refusals; it accepts one fixed code because computing a real
// TOTP here would be testing HMAC, not the UI).
//
// 🔴 WHAT THESE GUARD, all three silent if broken:
//   1. enroll must NOT arm the factor — only a proven code does. A UI that
//      flipped the row to "on" after enrolling would tell the owner they are
//      protected while the next login still takes the password alone.
//   2. the pending secret must leave the client the instant it is armed.
//   3. disable must demand BOTH the password and a code, because a factor a
//      stolen session can switch off protects nothing after the theft.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ProfileDropdown } from "./ProfileDropdown";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { clearToken } from "../api/auth";

const p = zh.profile;
/** The one code the mock accepts (see api/mock.ts MOCK_TOTP_CODE). */
const GOOD_CODE = "123456";
/** The password mock.ts boots with (api/mock.ts mockPassword). */
const MOCK_PASSWORD = "mock-password";

/** Turn the ship-dark rollout flag on — the precondition for the set-up path.
 * Verification is deliberately independent of it (see the dark-by-default test
 * at the bottom, which pins that). */
async function offerMfa() {
  await api.setMfaOffered(true);
}

async function openMfa() {
  const utils = render(
    <I18nProvider>
      <ProfileDropdown
        open
        onClose={vi.fn()}
        userName="使用者"
        setOwnerName={vi.fn()}
      />
    </I18nProvider>,
  );
  fireEvent.click(utils.getByText(p.accountSecurity));
  fireEvent.click(utils.getByText(p.mfa));
  return utils;
}

beforeEach(() => {
  __resetMock();
  clearToken();
  localStorage.clear();
});

describe("ProfileDropdown — second factor", () => {
  it("starts OFF and offers to set it up", async () => {
    await offerMfa();
    const utils = await openMfa();
    await utils.findByText(p.mfaEnrollStart);
    // The main-menu row's subtitle is the at-a-glance state.
    expect(utils.queryByText(p.mfaSubOn)).toBeNull();
  });

  // 🔴 Guard #1: enrolling shows the key but must NOT arm anything.
  it("enroll reveals the setup key WITHOUT arming the factor", async () => {
    await offerMfa();
    const utils = await openMfa();
    fireEvent.click(await utils.findByText(p.mfaEnrollStart));

    await utils.findByText(p.mfaScanHint);
    // The secret is on screen for the one moment it legitimately can be.
    expect(utils.getByText(/^[A-Z2-7]{16,}$/)).toBeTruthy();
    // …and the server still reports no factor required.
    await expect(api.getAuthStatus()).resolves.toMatchObject({
      mfaRequired: false,
    });
  });

  it("a wrong code is refused and the pending secret SURVIVES for a retry", async () => {
    await offerMfa();
    const utils = await openMfa();
    fireEvent.click(await utils.findByText(p.mfaEnrollStart));
    const codeInput = await utils.findByLabelText(p.mfaCodePlaceholder);

    fireEvent.change(
      utils.getByLabelText(p.currentPasswordPlaceholder),
      { target: { value: MOCK_PASSWORD } },
    );
    fireEvent.change(codeInput, { target: { value: "000000" } });
    fireEvent.click(utils.getByText(p.mfaActivate));
    await utils.findByText(p.mfaErrorActivate);
    await expect(api.getAuthStatus()).resolves.toMatchObject({
      mfaRequired: false,
    });

    // The SAME pending enrolment still activates — no re-scan needed. This is
    // what stops owners abandoning the ceremony half-done after one typo.
    fireEvent.change(codeInput, { target: { value: GOOD_CODE } });
    fireEvent.click(utils.getByText(p.mfaActivate));
    await utils.findByText(p.mfaActivated);
  });

  // 🔴 Guard #2: the secret must not linger on the client once armed.
  it("activate arms the factor and drops the secret from the screen", async () => {
    await offerMfa();
    const utils = await openMfa();
    fireEvent.click(await utils.findByText(p.mfaEnrollStart));

    const secret = (await utils.findByText(/^[A-Z2-7]{16,}$/)).textContent ?? "";
    expect(secret.length).toBeGreaterThan(15);

    fireEvent.change(
      utils.getByLabelText(p.currentPasswordPlaceholder),
      { target: { value: MOCK_PASSWORD } },
    );
    fireEvent.change(utils.getByLabelText(p.mfaCodePlaceholder), {
      target: { value: GOOD_CODE },
    });
    fireEvent.click(utils.getByText(p.mfaActivate));

    await utils.findByText(p.mfaActivated);
    expect(utils.queryByText(secret)).toBeNull();
    await expect(api.getAuthStatus()).resolves.toMatchObject({
      mfaRequired: true,
    });
  });

  it("an already-armed factor offers DISABLE, not another enrolment", async () => {
    // Arm it through the adapter, then open the view fresh — the state must be
    // read from the server, not remembered from this session.
    await offerMfa();
    await api.enrollMfa();
    await api.activateMfa(MOCK_PASSWORD, GOOD_CODE);

    await offerMfa();
    const utils = await openMfa();
    await utils.findByText(p.mfaDisable);
    expect(utils.queryByText(p.mfaEnrollStart)).toBeNull();
    // The recovery route for a lost authenticator is named in the hint, since
    // this endpoint deliberately cannot serve that case.
    expect(utils.getByText(/ocserverd mfa-disable/)).toBeTruthy();
  });

  // 🔴 Guard #3: both factors, and one indistinguishable refusal.
  it("disable requires BOTH the password and a code", async () => {
    await offerMfa();
    await api.enrollMfa();
    await api.activateMfa(MOCK_PASSWORD, GOOD_CODE);
    await offerMfa();
    const utils = await openMfa();
    await utils.findByText(p.mfaDisable);

    const submit = utils.getByText(p.mfaDisable) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    fireEvent.change(utils.getByLabelText(p.currentPasswordPlaceholder), {
      target: { value: "mock-password" },
    });
    expect(submit.disabled).toBe(true); // password alone is not enough

    fireEvent.change(utils.getByLabelText(p.mfaCodePlaceholder), {
      target: { value: "000000" },
    });
    expect(submit.disabled).toBe(false);

    // Right password, wrong code → refused, factor still armed.
    fireEvent.click(submit);
    await utils.findByText(p.mfaErrorDisable);
    await expect(api.getAuthStatus()).resolves.toMatchObject({
      mfaRequired: true,
    });

    fireEvent.change(utils.getByLabelText(p.mfaCodePlaceholder), {
      target: { value: GOOD_CODE },
    });
    fireEvent.click(utils.getByText(p.mfaDisable));
    await utils.findByText(p.mfaDisabled);
    await expect(api.getAuthStatus()).resolves.toMatchObject({
      mfaRequired: false,
    });
  });

  it("a wrong password with a good code is refused the same way", async () => {
    await offerMfa();
    await api.enrollMfa();
    await api.activateMfa(MOCK_PASSWORD, GOOD_CODE);
    await offerMfa();
    const utils = await openMfa();
    await utils.findByText(p.mfaDisable);

    fireEvent.change(utils.getByLabelText(p.currentPasswordPlaceholder), {
      target: { value: "not-the-password" },
    });
    fireEvent.change(utils.getByLabelText(p.mfaCodePlaceholder), {
      target: { value: GOOD_CODE },
    });
    fireEvent.click(utils.getByText(p.mfaDisable));

    await utils.findByText(p.mfaErrorDisable);
    await expect(api.getAuthStatus()).resolves.toMatchObject({
      mfaRequired: true,
    });
  });
});

describe("ProfileDropdown — the ship-dark rollout flag", () => {
  // The reason the flag exists: an existing studio that upgrades into this build
  // must see nothing new until its owner opts in.
  it("a dark server offers only the rollout switch, not the set-up path", async () => {
    const utils = await openMfa();

    await utils.findByText(p.mfaOfferOn);
    expect(utils.queryByText(p.mfaEnrollStart)).toBeNull();
    // The main-menu subtitle must say "not enabled on this server", NOT "off" —
    // they are different facts and only one of them has a button.
    expect(utils.queryByText(p.mfaSubOff)).toBeNull();
  });

  it("turning it on reveals the set-up path", async () => {
    const utils = await openMfa();
    fireEvent.click(await utils.findByText(p.mfaOfferOn));

    await utils.findByText(p.mfaEnrollStart);
    await expect(api.getMfaState()).resolves.toMatchObject({
      offered: true,
      enrolled: false,
    });
  });

  // 🔴 The safety property, at the UI seam: withdrawing the feature must not
  // disarm a factor, and must not take the off-switch away with it.
  it("withdrawing the feature leaves an armed factor armed and removable", async () => {
    await offerMfa();
    await api.enrollMfa();
    await api.activateMfa(MOCK_PASSWORD, GOOD_CODE);
    await api.setMfaOffered(false);

    // Still armed, and login still demands a code.
    await expect(api.getMfaState()).resolves.toMatchObject({
      offered: false,
      enrolled: true,
    });
    await expect(api.getAuthStatus()).resolves.toMatchObject({
      mfaRequired: true,
    });

    // And the owner can still take it off through the product.
    const utils = await openMfa();
    await utils.findByText(p.mfaDisable);
    fireEvent.change(utils.getByLabelText(p.currentPasswordPlaceholder), {
      target: { value: MOCK_PASSWORD },
    });
    fireEvent.change(utils.getByLabelText(p.mfaCodePlaceholder), {
      target: { value: GOOD_CODE },
    });
    fireEvent.click(utils.getByText(p.mfaDisable));
    await utils.findByText(p.mfaDisabled);
  });
});

describe("ProfileDropdown — the enrolment QR", () => {
  it("renders a QR built in the browser, alongside the key and the link", async () => {
    await offerMfa();
    const utils = await openMfa();
    fireEvent.click(await utils.findByText(p.mfaEnrollStart));
    await utils.findByText(p.mfaScanHint);

    const svg = utils.container.querySelector("svg[role='img']");
    expect(svg).toBeTruthy();
    // Nothing is fetched: a QR "service" would mean posting the TOTP secret to
    // a third party.
    expect(utils.container.innerHTML).not.toContain("<image");
    expect(utils.container.innerHTML).not.toContain("data:");
    // The key and the otpauth link remain the primary path.
    expect(utils.getByText(/^[A-Z2-7]{16,}$/)).toBeTruthy();
    expect(utils.getByText(p.mfaOpenInApp)).toBeTruthy();
  });

  // 🔴 The QR is an ENHANCEMENT and must fail like one. A throw during render
  // would unmount the dropdown and take the setup key, the otpauth link and the
  // disable button with it — exactly what an owner must not lose at that moment.
  // The payload is reachable-long in practice: issuer/account come from the
  // owner-settable org name and nickname.
  it("degrades to the key and link when the QR cannot be built", async () => {
    // The secret is a flat placeholder, not a plausible-looking key: nothing
    // here decodes it (enrollMfa is mocked and qrSvg just encodes the string),
    // and a high-entropy base32 literal in a test file is indistinguishable
    // from a real leaked one to a secret scanner. What actually drives this
    // test is `huge` — the oversized label that exceeds QR capacity.
    const secret = "AAAAAAAAAAAAAAAA";
    const huge = "y".repeat(4000);
    vi.spyOn(api, "enrollMfa").mockResolvedValue({
      secret,
      otpauthUri: `otpauth://totp/${huge}:owner?secret=${secret}`,
    });

    await offerMfa();
    const utils = await openMfa();
    fireEvent.click(await utils.findByText(p.mfaEnrollStart));

    // The panel still works…
    await utils.findByText(p.mfaScanHint);
    expect(utils.getByText(/^[A-Z2-7]{16,}$/)).toBeTruthy();
    expect(utils.getByText(p.mfaOpenInApp)).toBeTruthy();
    expect(utils.getByLabelText(p.mfaCodePlaceholder)).toBeTruthy();
    // …with no QR, and no scan-the-QR instruction pointing at a missing picture.
    expect(utils.container.querySelector("svg[role='img']")).toBeNull();
    expect(utils.queryByText(p.mfaScanQrHint)).toBeNull();
  });
});
