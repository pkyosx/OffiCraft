// 設定 › 軟體更新 — invite-link paste (task: the updater operator hands out a
// one-line link `http://host:8790?code=<邀請碼>`; pasting it into the
// 更新伺服器位址 field must AUTO-SPLIT: the code lands in the invite-code
// setting, the URL is stored clean, and both fields remain independently
// editable for the manual path).

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage, splitInviteLink } from "./SettingsPage";
import { __resetMock } from "../api/mock";
import { api } from "../api";

const s = zh.settings;

/** Render Settings and open 軟體更新 (async settings load). */
async function openSoftware() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.software));
  await utils.findByLabelText(s.updaterUrl); // settings loaded
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("splitInviteLink", () => {
  it("splits a bare-origin invite link into clean URL + code", () => {
    expect(splitInviteLink("http://updates.example:8790?code=ocu-inv-abc")).toEqual({
      url: "http://updates.example:8790",
      code: "ocu-inv-abc",
    });
  });

  it("keeps other query params / path while stripping only the code", () => {
    expect(
      splitInviteLink("https://updates.example/base?channel=beta&code=ocu-inv-abc")
    ).toEqual({
      url: "https://updates.example/base?channel=beta",
      code: "ocu-inv-abc",
    });
  });

  it("tolerates surrounding whitespace (a sloppy paste)", () => {
    expect(splitInviteLink("  http://h:8790?code=xyz  ")).toEqual({
      url: "http://h:8790",
      code: "xyz",
    });
  });

  it("answers null for a plain URL, a blank code, a non-URL, non-http", () => {
    expect(splitInviteLink("http://updates.example:8790")).toBeNull();
    expect(splitInviteLink("http://updates.example:8790?code=")).toBeNull();
    expect(splitInviteLink("not a url ?code=x")).toBeNull();
    expect(splitInviteLink("ftp://h?code=x")).toBeNull();
  });
});

describe("SettingsPage · 軟體更新 · 邀請連結貼上", () => {
  it("pasting an invite link fills BOTH the clean URL and the invite code", async () => {
    const utils = await openSoftware();
    const url = utils.getByLabelText(s.updaterUrl) as HTMLInputElement;

    fireEvent.change(url, {
      target: { value: "http://updates.example:8790?code=ocu-inv-secret" },
    });
    fireEvent.blur(url);

    await waitFor(async () => {
      const settings = await api.getServerSettings();
      expect(settings.updaterUrl).toBe("http://updates.example:8790");
      expect(settings.updaterInviteCodeSet).toBe(true);
    });
    // The field mirrors the CLEAN url — the secret never lingers on screen.
    expect(url.value).toBe("http://updates.example:8790");
    // The invite field shows its "already set" placeholder (write-only secret).
    const invite = utils.getByLabelText(s.updaterInvite) as HTMLInputElement;
    expect(invite.placeholder).toBe(s.updaterInviteSet);
  });

  it("a plain URL paste keeps the old single-field behaviour", async () => {
    const utils = await openSoftware();
    const url = utils.getByLabelText(s.updaterUrl) as HTMLInputElement;

    fireEvent.change(url, { target: { value: "https://plain.example" } });
    fireEvent.blur(url);

    await waitFor(async () => {
      const settings = await api.getServerSettings();
      expect(settings.updaterUrl).toBe("https://plain.example");
      expect(settings.updaterInviteCodeSet).toBe(false);
    });
  });
});
