// 設定 › 系統更新與備份 · 備份健康 (T-5e71).
//
// The backup verdict used to live in two places the owner did not want: a card
// at the top of the monitor page and a permanently-mounted topbar light. He
// ruled (rc-5ef6f1319f27) that it belongs under the existing 軟體更新 section,
// renamed 系統更新與備份, and that the topbar icon goes away.
//
// So this file asserts PLACEMENT and WORDING as rendered — the landing row is
// named 系統更新與備份, and the backup verdict is reachable there — plus the
// distinguishability the original card was built for: a block that renders the
// same thing for "broken", "cannot tell" and "fine" has reintroduced the
// original defect while looking implemented.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import type { BackupHealthView } from "../types";
import { __resetMock } from "../api/mock";

const state = { health: null as BackupHealthView | null, error: false };
vi.mock("../hooks/useBackupHealth", () => ({
  useBackupHealth: () => ({
    health: state.health,
    loading: false,
    error: state.error,
    refresh: () => {},
  }),
}));

import { SettingsPage } from "./SettingsPage";

const s = zh.settings;
const b = zh.backupHealth;

function healthy(): BackupHealthView {
  return {
    status: "healthy",
    code: "",
    detail: "",
    newestBackupTs: 1785600000,
    newestBackupAgeSecs: 3600,
    staleAfterSecs: 43200,
    sinceTs: null,
    checkedTs: 1785603600,
  };
}

function renderSettings() {
  return render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>,
  );
}

/** Render Settings and navigate landing → 系統更新與備份. */
async function openSection() {
  const utils = renderSettings();
  fireEvent.click(utils.getByText(s.software));
  await utils.findByText(s.currentVersion);
  return utils;
}

async function status(): Promise<HTMLElement> {
  return await waitFor(() => screen.getByTestId("set-backup-status"));
}

beforeEach(() => {
  __resetMock();
  state.health = healthy();
  state.error = false;
});

describe("設定 landing · 區塊名稱", () => {
  it("names the row 系統更新與備份 — the owner's wording, rendered", () => {
    const utils = renderSettings();
    // Assert the TEXT that actually reaches the screen, not that a dict key
    // exists: the rename is the deliverable.
    expect(utils.getByText("系統更新與備份")).toBeTruthy();
  });

  it("renames the English copy too — a half-renamed section is a wrong string with legs", () => {
    // The language toggle persists to localStorage and the provider reads it on
    // mount, so this renders the REAL English dict rather than asserting a key.
    window.localStorage.setItem("oc.language", "en");
    try {
      const utils = renderSettings();
      expect(utils.getByText(en.settings.software)).toBeTruthy();
      expect(en.settings.software).toBe("System update & backup");
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });

  it("titles the section page 系統更新與備份 too", async () => {
    const utils = await openSection();
    const title = utils.container.querySelector(".settings__title--doc");
    expect(title?.textContent).toBe("系統更新與備份");
  });
});

describe("設定 › 系統更新與備份 · 備份健康", () => {
  it("shows the backup verdict inside this section", async () => {
    const utils = await openSection();
    // The block is HERE, under the software-update section — that is the whole
    // ticket. Its heading is the backup title, and a verdict is rendered.
    expect(utils.getByText(b.title)).toBeTruthy();
    const el = await status();
    expect(el.dataset.backupState).toBe("healthy");
    // A green light has no failure to explain — filler there would dilute the
    // red case it exists to make noticeable.
    expect(screen.queryByTestId("set-backup-reason")).toBeNull();
    // But it must name the backup it is standing on, or it is a bare assertion.
    expect(screen.getByTestId("set-backup-newest").textContent).not.toBe("—");
  });

  it("shows a STALE backup — a previous one exists, but the schedule went quiet", async () => {
    state.health = {
      ...healthy(),
      status: "unhealthy",
      code: "stale",
      detail: "newest scheduled backup is 30h0m0s old (alarm after 12h0m0s)",
      newestBackupAgeSecs: 108000,
      sinceTs: Math.floor(Date.now() / 1000) - 7200,
    };
    await openSection();
    const el = await status();
    expect(el.dataset.backupState).toBe("unhealthy");
    expect(screen.getByTestId("set-backup-reason").textContent?.trim()).not.toBe(
      "",
    );
    // How long it has been broken must be visible: an outage that reads as
    // brand new every time nobody can judge.
    expect(screen.getByTestId("set-backup-since")).toBeTruthy();
  });

  it("says NEVER, in words, when no scheduled backup has ever landed", async () => {
    state.health = {
      ...healthy(),
      status: "unhealthy",
      code: "never_ran",
      detail: "no scheduled backup has ever landed (watching for 30h0m0s)",
      newestBackupTs: null,
      newestBackupAgeSecs: null,
      sinceTs: Math.floor(Date.now() / 1000) - 3600,
    };
    await openSection();
    const el = await status();
    expect(el.dataset.backupState).toBe("unhealthy");
    // "never" is a FACT, not a missing value — rendering it as the same dash
    // used for "not measured" is exactly how this alarm would go unnoticed.
    const newest = screen.getByTestId("set-backup-newest").textContent ?? "";
    expect(newest.trim()).not.toBe("—");
    expect(newest.trim()).not.toBe("");
  });

  it("renders a server that cannot tell as UNKNOWN, never as healthy", async () => {
    state.health = {
      ...healthy(),
      status: "unknown",
      code: "",
      detail: "the backup watchdog has not reported yet",
      newestBackupTs: null,
      newestBackupAgeSecs: null,
    };
    await openSection();
    const el = await status();
    expect(el.dataset.backupState).toBe("unknown");
    expect(screen.getByTestId("set-backup-reason").textContent?.trim()).not.toBe(
      "",
    );
  });

  it("renders a FAILED load as unknown too — an unanswerable question is not good news", async () => {
    state.health = null;
    state.error = true;
    await openSection();
    const el = await status();
    expect(el.dataset.backupState).toBe("unknown");
  });

  it("shows the server's diagnostic as secondary text, not as the headline", async () => {
    state.health = {
      ...healthy(),
      status: "unhealthy",
      code: "failed",
      detail: "scheduled backup failed: disk went away",
    };
    await openSection();
    await status();
    const reason = screen.getByTestId("set-backup-reason").textContent ?? "";
    // The sentence the owner reads is translated, derived from the code; the
    // server's English diagnostic sits in its own labelled row so a wording
    // change on the server can never silently rewrite the UI.
    expect(reason).not.toContain("disk went away");
    expect(screen.getByTestId("set-backup-detail").textContent).toContain(
      "disk went away",
    );
  });
});
