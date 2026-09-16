// 設定 › 系統更新與備份 版本顯示 (t-dc68 — ONE unified identity: an official
// package headlines its GitHub Release tag (`version` ≠ "0.0.0"); a
// self-build — the mock fixture — falls back to the composed label
// v<yymmdd>-<hhmm>-<shortsha> from git_sha + git_time). Locked here: the
// self-build headline is the composed label (mock fixture: git_sha f6f5e1c
// committed 2026-07-04T08:54 → v260704-0854-f6f5e1c).

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

const s = zh.settings;

/** Render Settings and navigate landing → 系統更新與備份 (async version load). */
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

describe("SettingsPage · 系統更新與備份 版本顯示", () => {
  it("headlines the unified v<yymmdd>-<hhmm>-<shortsha> label", async () => {
    const utils = await openSoftware();
    // Assert on the element that CARRIES the label, not on the row: since the
    // owner round-2 move, .sw-build__headline is a two-item row (version +
    // the refresh button, whose accessible name is real — visually clipped —
    // text), so the row's textContent legitimately includes 檢查更新.
    const version = utils.container.querySelector(".sw-build__version");
    expect(version?.textContent).toBe("v260704-0854-f6f5e1c");
    // The label still headlines its row (it is the row's first child).
    const headline = utils.container.querySelector(".sw-build__headline");
    expect(headline?.firstElementChild).toBe(version);
  });
});
