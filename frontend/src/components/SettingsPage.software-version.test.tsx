// 設定 › 軟體更新 版本顯示 (T-e9d1 — owner final: ONE unified label
// v<yymmdd>-<hhmm>-<shortsha>, composed frontend-side from git_sha + git_time,
// and NOTHING else). Locked here:
//   1. The headline is the unified label (mock fixture: git_sha f6f5e1c
//      committed 2026-07-04T08:54 → v260704-0854-f6f5e1c) — no r-N anywhere.
//   2. No secondary rows at all — no human-readable commit-time row, no
//      separate sha row, no update-channel row.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

const s = zh.settings;

/** Render Settings and navigate landing → 軟體更新 (async version load). */
async function openSoftware() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.software));
  await utils.findByText(s.currentVersion);
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · 軟體更新 版本顯示", () => {
  it("headlines the unified v<yymmdd>-<hhmm>-<shortsha> label", async () => {
    const utils = await openSoftware();
    const headline = utils.container.querySelector(".sw-build__headline");
    expect(headline?.textContent).toBe("v260704-0854-f6f5e1c");
  });

  it("never shows the r-N serial", async () => {
    const utils = await openSoftware();
    // Mock fixture carries release_tag "r-7" on the wire — the UI must not
    // surface it anywhere on the card.
    expect(utils.container.textContent).not.toContain("r-7");
  });

  it("shows no secondary rows: no commit-time/sha/channel rows", async () => {
    const utils = await openSoftware();
    // Owner ruled the separate commit-time display out — the date-ish version
    // label alone is the build identity, and the removed rows never come back.
    expect(utils.container.querySelector(".sw-build__meta")).toBeNull();
    expect(
      utils.container.querySelectorAll(".sw-build__meta-row").length
    ).toBe(0);
    expect(utils.container.querySelector(".sw-channel")).toBeNull();
  });
});
