// 設定 › 軟體更新 版本顯示 (t-dc68 — ONE unified identity: an official
// package headlines its GitHub Release tag (`version` ≠ "0.0.0"); a
// self-build — the mock fixture — falls back to the composed label
// v<yymmdd>-<hhmm>-<shortsha> from git_sha + git_time). Locked here:
//   1. The self-build headline is the composed label (mock fixture: git_sha
//      f6f5e1c committed 2026-07-04T08:54 → v260704-0854-f6f5e1c).
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

  it("never shows a retired r-N serial", async () => {
    const utils = await openSoftware();
    // The r-N serial left the wire with the updater teardown (t-dc68) — the
    // card must not resurrect one from anywhere.
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
