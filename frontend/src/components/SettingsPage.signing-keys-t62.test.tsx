// 設定 › 系統更新與備份 · 簽章金鑰 (T-62).
//
// The card's job beyond listing is to make the ASYMMETRY between its two
// buttons visible, so that is what this file asserts as RENDERED TEXT:
//
//   - rotate is safe and needs no confirmation;
//   - remove is a revocation with no undo whose blast radius is WIDER than
//     anyone would guess (it kills file share links too, and warden credentials
//     never expire on their own), so it must not be reachable in one click and
//     the confirmation must say both things.
//
// It also pins the two ways this card could quietly lie: the key that is
// SIGNING must have no remove button at all (a button that exists in order to
// fail is worse than no button), and a key whose creation time was never
// recorded must not render as a date.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import type { SigningKeyView } from "../types";
import { __resetMock } from "../api/mock";

const state = {
  keys: [] as SigningKeyView[],
  error: "",
  rotate: vi.fn(),
  remove: vi.fn(),
};
vi.mock("../hooks/useSigningKeys", () => ({
  useSigningKeys: () => ({
    keys: state.keys,
    loading: false,
    busy: false,
    error: state.error,
    rotate: state.rotate,
    remove: state.remove,
  }),
}));

import { SettingsPage } from "./SettingsPage";

const s = zh.settings;
const k = zh.signingKeys;

/** A ring mid-transition: one retired key, one signing. */
function transitioningRing(): SigningKeyView[] {
  return [
    { keyId: "k-old", createdTs: null, isSigning: false },
    { keyId: "k-new", createdTs: 1788400000, isSigning: true },
  ];
}

async function openSection() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>,
  );
  fireEvent.click(utils.getByText(s.software));
  await utils.findByText(s.currentVersion);
  return utils;
}

beforeEach(() => {
  __resetMock();
  state.keys = transitioningRing();
  state.error = "";
  state.rotate = vi.fn();
  state.remove = vi.fn();
});

describe("簽章金鑰卡 · 看得出現況", () => {
  it("shows how many keys there are, when each was made, and which one signs", async () => {
    await openSection();
    const card = await waitFor(() => screen.getByTestId("set-signing-keys"));

    // The three facts the ticket asks the settings page to make visible.
    expect(screen.getByTestId("set-signing-keys-count").textContent).toContain("2");
    expect(card.textContent).toContain("k-old");
    expect(card.textContent).toContain("k-new");

    const signing = screen.getByTestId("set-signing-key-k-new");
    const retired = screen.getByTestId("set-signing-key-k-old");
    expect(signing.getAttribute("data-signing")).toBe("yes");
    expect(retired.getAttribute("data-signing")).toBe("no");
    // Rendered wording, not a dict key: "which one signs" is the deliverable.
    expect(signing.textContent).toContain(k.signingBadge);
    expect(retired.textContent).toContain(k.retiredBadge);
    expect(k.signingBadge).not.toBe(k.retiredBadge);
  });

  it("says a never-recorded creation time in words, never as a date", async () => {
    await openSection();
    const retired = await waitFor(() =>
      screen.getByTestId("set-signing-key-k-old"),
    );
    expect(retired.textContent).toContain(k.createdUnknown);
    // 0 → null happens in the mapper precisely so this cannot render as 1970;
    // assert the wrong fact is absent rather than trusting the layer below.
    expect(retired.textContent).not.toContain("1970");
    expect(retired.textContent).not.toContain("1/1");
  });
});

describe("簽章金鑰卡 · 兩顆按鈕的不對稱", () => {
  it("rotates in ONE click — it logs nobody out, so it asks nothing", async () => {
    await openSection();
    const btn = await waitFor(() =>
      screen.getByTestId("set-signing-keys-rotate"),
    );
    fireEvent.click(btn);
    expect(state.rotate).toHaveBeenCalledTimes(1);
    // And no confirmation appeared: a dialog here would train people to click
    // through the one that matters.
    expect(screen.queryByTestId("set-signing-keys-confirm")).toBeNull();
  });

  it("never offers to remove the key that is SIGNING", async () => {
    await openSection();
    const signing = await waitFor(() =>
      screen.getByTestId("set-signing-key-k-new"),
    );
    expect(signing.querySelector("button")).toBeNull();
    // The retired row DOES have one — the positive control, without which the
    // assertion above would also pass on a card that renders no buttons at all.
    const retired = screen.getByTestId("set-signing-key-k-old");
    expect(retired.querySelector("button")).not.toBeNull();
  });

  it("removal takes a confirmation, and the confirmation says what dies", async () => {
    await openSection();
    const retired = await waitFor(() =>
      screen.getByTestId("set-signing-key-k-old"),
    );
    fireEvent.click(retired.querySelector("button")!);

    // Nothing has happened yet — the first click only opens the question.
    expect(state.remove).not.toHaveBeenCalled();
    const modal = screen.getByTestId("set-signing-keys-confirm");

    // 🔴 The two consequences nobody would guess, as RENDERED TEXT. If either
    // sentence is dropped or softened this test is what notices.
    expect(modal.textContent).toContain(k.removeConfirmBody);
    expect(modal.textContent).toContain(k.removeConfirmWarden);
    expect(k.removeConfirmBody).toContain("分享連結");
    expect(k.removeConfirmWarden).toContain("機器");

    fireEvent.click(screen.getByTestId("set-signing-keys-confirm-ok"));
    expect(state.remove).toHaveBeenCalledWith("k-old");
  });

  it("cancelling removes nothing", async () => {
    await openSection();
    const retired = await waitFor(() =>
      screen.getByTestId("set-signing-key-k-old"),
    );
    fireEvent.click(retired.querySelector("button")!);
    fireEvent.click(screen.getByText(k.removeConfirmCancel));
    expect(state.remove).not.toHaveBeenCalled();
    expect(screen.queryByTestId("set-signing-keys-confirm")).toBeNull();
  });
});

describe("簽章金鑰卡 · 失敗不可以長得像成功", () => {
  it("renders whatever the hook reports as the failure", async () => {
    // ⚠️ This row only proves the card DISPLAYS an error string. It cannot prove
    // the string is the server's reason, because the hook is mocked here — and
    // the first version of this file stopped at exactly this assertion while the
    // real path rendered "http 409 for POST /api/…". That the string is the
    // SERVER'S is pinned where it can be: useSigningKeys.test.ts, which drives
    // the real adapter error envelope.
    state.error =
      "key 'k-new' is the one currently signing and cannot be removed — rotate first, then remove it";
    await openSection();
    const err = await waitFor(() =>
      screen.getByTestId("set-signing-keys-error"),
    );
    expect(err.textContent).toContain("rotate first");
  });
});

describe("簽章金鑰卡 · 英文那一份也要是真的字", () => {
  it("carries the same two warnings in English", () => {
    // A half-translated destructive confirmation is a wrong string with legs:
    // the English reader would get the button without the blast radius.
    expect(en.signingKeys.removeConfirmBody).toContain("share links");
    expect(en.signingKeys.removeConfirmWarden).toContain("warden");
    expect(en.signingKeys.removeConfirmBody).not.toBe(zh.signingKeys.removeConfirmBody);
  });
});
