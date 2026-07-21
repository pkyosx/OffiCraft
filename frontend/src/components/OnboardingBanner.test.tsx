// T-ba62 — the first-run onboarding banner is the ONLY place a fresh install
// can read WHY it is not working. These tests pin both directions:
//   - onboarding failed → the banner names the step AND its reason;
//   - onboarding ok / never ran / still running → nothing renders at all.
// The negative cases are not decoration: a banner that renders unconditionally
// would satisfy the failure test on its own, and a "why is my studio broken"
// nag on a perfectly healthy install is its own regression.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import {
  OnboardingBanner,
  ONBOARDING_POLL_MS,
  ONBOARDING_POLL_CEILING_MS,
} from "./OnboardingBanner";

const getServerSettings = vi.fn();

vi.mock("../api", () => ({
  api: { getServerSettings: () => getServerSettings() },
}));

function settingsWith(onboarding: unknown) {
  return { outsourceMaxParallel: 0, onboarding };
}

function renderBanner() {
  return render(
    <I18nProvider>
      <OnboardingBanner />
    </I18nProvider>
  );
}

describe("OnboardingBanner", () => {
  beforeEach(() => {
    sessionStorage.clear();
    getServerSettings.mockReset();
  });

  it("shows the failed step and its REASON", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "failed",
        startedAt: 1,
        finishedAt: 2,
        steps: [
          {
            name: "install_warden",
            ok: false,
            reason: "installing this machine's warden failed (exit 1)",
            detail: "[ocwarden install] FATAL: claude_bin_unresolved",
          },
        ],
      })
    );
    renderBanner();
    const banner = await screen.findByTestId("onboarding-banner");
    // The step is named…
    expect(banner.textContent).toContain("安裝這台機器");
    // …and, crucially, WHY. A step name with no cause is the same silence.
    expect(banner.textContent).toContain(
      "installing this machine's warden failed (exit 1)"
    );
  });

  it("hides the raw tool log behind a toggle, then reveals it", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "failed",
        startedAt: 1,
        finishedAt: 2,
        steps: [
          {
            name: "install_warden",
            ok: false,
            reason: "installing this machine's warden failed (exit 1)",
            detail: "[ocwarden install] FATAL: claude_bin_unresolved",
          },
        ],
      })
    );
    renderBanner();
    const toggle = await screen.findByTestId("onboarding-detail-toggle");
    expect(screen.queryByTestId("onboarding-detail")).toBeNull();
    toggle.click();
    await waitFor(() => {
      expect(screen.getByTestId("onboarding-detail").textContent).toContain(
        "claude_bin_unresolved"
      );
    });
  });

  it("renders NOTHING when onboarding succeeded", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "ok",
        startedAt: 1,
        finishedAt: 2,
        steps: [{ name: "install_warden", ok: true, reason: "installed", detail: "" }],
      })
    );
    const { container } = renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
    expect(container.textContent).toBe("");
  });

  it("renders NOTHING while onboarding is still running", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({ state: "running", startedAt: 1, finishedAt: 0, steps: [] })
    );
    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  // The STATE gate, isolated. The case above cannot pin it: an unfinished run
  // has no steps yet, so "no failed steps" alone would already suppress the
  // banner and a mutant that dropped the state check entirely would stay green
  // (it did — this test exists because that mutant survived). Here the report
  // is mid-run AND already carries a failed step: the banner must still hold
  // its tongue, because a run in progress can still recover, and a warning
  // that appears and then vanishes on its own teaches the owner to ignore it.
  it("renders NOTHING mid-run even when a step has already failed", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "running",
        startedAt: 1,
        finishedAt: 0,
        steps: [
          { name: "install_warden", ok: false, reason: "still retrying", detail: "" },
        ],
      })
    );
    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  it("renders NOTHING when onboarding never ran (null report)", async () => {
    getServerSettings.mockResolvedValue(settingsWith(null));
    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  it("renders NOTHING when the settings read itself fails (asserts no fiction)", async () => {
    getServerSettings.mockRejectedValue(new Error("network down"));
    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  it("stays dismissed for the session once dismissed", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "failed",
        startedAt: 1,
        finishedAt: 2,
        steps: [{ name: "wake_assistant", ok: false, reason: "no warden yet", detail: "" }],
      })
    );
    const first = renderBanner();
    (await screen.findByTestId("onboarding-dismiss")).click();
    await waitFor(() => expect(screen.queryByTestId("onboarding-banner")).toBeNull());
    first.unmount();

    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalledTimes(2));
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });
});

// ── 🔴 the transition, which is the ONLY timeline that actually happens ──────
//
// Every test above is a static snapshot: the report already reads its final
// value at mount. The real first run does not look like that — the cockpit
// mounts while onboarding is still running, and the report turns "failed" tens
// of seconds later. A mount-only fetch passes all eight snapshots and still
// never shows the owner anything, which is how this shipped the first time.
describe("OnboardingBanner — running → failed transition", () => {
  beforeEach(() => {
    sessionStorage.clear();
    getServerSettings.mockReset();
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const failedReport = {
    state: "failed",
    startedAt: 1,
    finishedAt: 2,
    steps: [
      {
        name: "install_warden",
        ok: false,
        reason: "installing this machine's warden failed (exit 1)",
        detail: "[ocwarden install] FATAL: claude_bin_unresolved",
      },
    ],
  };

  it("appears once a still-running onboarding turns failed — with no reload", async () => {
    getServerSettings
      .mockResolvedValueOnce(settingsWith({ state: "running", startedAt: 1, finishedAt: 0, steps: [] }))
      .mockResolvedValueOnce(settingsWith({ state: "running", startedAt: 1, finishedAt: 0, steps: [] }))
      .mockResolvedValue(settingsWith(failedReport));

    render(
      <I18nProvider>
        <OnboardingBanner />
      </I18nProvider>
    );
    // mount read: still running → correctly silent
    await act(async () => {});
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();

    // ...and it keeps asking until the answer changes.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS * 3);
    });
    const banner = screen.getByTestId("onboarding-banner");
    expect(banner.textContent).toContain(
      "installing this machine's warden failed (exit 1)"
    );
  });

  it("stops polling once the report is terminal (no unbounded traffic)", async () => {
    getServerSettings.mockResolvedValue(settingsWith(failedReport));
    render(
      <I18nProvider>
        <OnboardingBanner />
      </I18nProvider>
    );
    await act(async () => {});
    expect(getServerSettings).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS * 5);
    });
    // Still one: a terminal answer ends the loop.
    expect(getServerSettings).toHaveBeenCalledTimes(1);
  });

  it("gives up after the ceiling instead of polling a wedged report forever", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({ state: "running", startedAt: 1, finishedAt: 0, steps: [] })
    );
    render(
      <I18nProvider>
        <OnboardingBanner />
      </I18nProvider>
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_CEILING_MS + ONBOARDING_POLL_MS * 5);
    });
    const atCeiling = getServerSettings.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS * 10);
    });
    expect(getServerSettings.mock.calls.length).toBe(atCeiling);
  });

  it("keeps polling through a transient settings-read failure", async () => {
    getServerSettings
      .mockRejectedValueOnce(new Error("server still booting"))
      .mockResolvedValue(settingsWith(failedReport));
    render(
      <I18nProvider>
        <OnboardingBanner />
      </I18nProvider>
    );
    await act(async () => {});
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS * 2);
    });
    expect(screen.getByTestId("onboarding-banner")).toBeTruthy();
  });
});
