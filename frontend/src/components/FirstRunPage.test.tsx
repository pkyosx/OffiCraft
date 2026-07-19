// First-run setup page (B3): the claim-token + set-password wall a fresh
// install boots into. Exercised against the mock adapter in its first-run
// shape (__setMockFirstRun) — the same check order the server applies.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { FirstRunPage } from "./FirstRunPage";
import { __resetMock, __setMockFirstRun } from "../api/mock";
import { api } from "../api";

const f = zh.firstRun;
let claimToken = "";

function renderPage(onSuccess = vi.fn(), onGotoLogin = vi.fn()) {
  const utils = render(
    <I18nProvider>
      <FirstRunPage onSuccess={onSuccess} onGotoLogin={onGotoLogin} />
    </I18nProvider>
  );
  return { ...utils, onSuccess, onGotoLogin };
}

function fill(
  utils: ReturnType<typeof renderPage>,
  claim: string,
  pwd: string,
  confirm: string
) {
  fireEvent.change(utils.getByLabelText(f.claimPlaceholder), {
    target: { value: claim },
  });
  fireEvent.change(utils.getByLabelText(f.passwordPlaceholder), {
    target: { value: pwd },
  });
  fireEvent.change(utils.getByLabelText(f.confirmPlaceholder), {
    target: { value: confirm },
  });
}

beforeEach(() => {
  __resetMock();
  claimToken = __setMockFirstRun();
});

afterEach(() => {
  window.history.replaceState(null, "", "/");
});

describe("FirstRunPage", () => {
  it("claims the server and logs in with a valid claim token + password", async () => {
    const utils = renderPage();
    fill(utils, claimToken, "brand-new-pass", "brand-new-pass");
    fireEvent.click(utils.getByText(f.submit));
    await waitFor(() => expect(utils.onSuccess).toHaveBeenCalled());
    // The mock now reports the password as set (the server-side flip).
    await expect(api.getAuthStatus()).resolves.toBe(true);
  });

  it("surfaces a wrong claim token as an inline error, no entry", async () => {
    const utils = renderPage();
    fill(utils, "not-the-token", "brand-new-pass", "brand-new-pass");
    fireEvent.click(utils.getByText(f.submit));
    await utils.findByText(f.errorClaim);
    expect(utils.onSuccess).not.toHaveBeenCalled();
    await expect(api.getAuthStatus()).resolves.toBe(false);
  });

  it("rejects a short password and a mismatched confirmation locally", async () => {
    const utils = renderPage();
    fill(utils, claimToken, "short", "short");
    fireEvent.click(utils.getByText(f.submit));
    await utils.findByText(f.errorTooShort);

    fill(utils, claimToken, "brand-new-pass", "different-pass");
    fireEvent.click(utils.getByText(f.submit));
    await utils.findByText(f.errorMismatch);
    expect(utils.onSuccess).not.toHaveBeenCalled();
  });

  it("prefills the claim token from ?code=, focuses the password field, and scrubs the URL", async () => {
    window.history.replaceState(null, "", `/?code=${claimToken}`);
    const utils = renderPage();
    expect(
      (utils.getByLabelText(f.claimPlaceholder) as HTMLInputElement).value
    ).toBe(claimToken);
    expect(document.activeElement).toBe(utils.getByLabelText(f.passwordPlaceholder));
    await waitFor(() => expect(window.location.search).not.toContain("code="));

    fireEvent.change(utils.getByLabelText(f.passwordPlaceholder), {
      target: { value: "brand-new-pass" },
    });
    fireEvent.change(utils.getByLabelText(f.confirmPlaceholder), {
      target: { value: "brand-new-pass" },
    });
    fireEvent.click(utils.getByText(f.submit));
    await waitFor(() => expect(utils.onSuccess).toHaveBeenCalled());
  });

  it("keeps the empty claim field focused when the URL carries no code", () => {
    const utils = renderPage();
    expect(
      (utils.getByLabelText(f.claimPlaceholder) as HTMLInputElement).value
    ).toBe("");
    expect(document.activeElement).toBe(utils.getByLabelText(f.claimPlaceholder));
  });

  it("offers the login wall when a password is already set (409)", async () => {
    __resetMock(); // back to the installed shape: password set
    const utils = renderPage();
    fill(utils, claimToken, "brand-new-pass", "brand-new-pass");
    fireEvent.click(utils.getByText(f.submit));
    await utils.findByText(f.errorTaken);
    fireEvent.click(utils.getByText(f.gotoLogin));
    expect(utils.onGotoLogin).toHaveBeenCalled();
    expect(utils.onSuccess).not.toHaveBeenCalled();
  });
});
