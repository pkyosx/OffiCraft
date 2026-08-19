// T-8115 — 「永久是空的」不再被當成「還沒寫好、再等等」.
//
// MEASURED PREMISE — dated, because half of it has since been made false ON
// PURPOSE. In 2026-07, GET /api/settings on the production install measured
// 639,270 bytes uncompressed / 373 kB gzipped, and `onboarding` was null —
// which the DTO itself declares NORMAL ("Null on the settings read when
// onboarding never ran (an install that predates it, or a database that already
// had a password)"). The banner treated null as non-terminal, so every cockpit
// open polled that payload once every 3 s for the full 180 s ceiling: ~61
// downloads, ~22 MB, for a row that no code path on that install will ever
// write.
//
// 🔴 T-83ef took the themes out of that payload, so the three SIZE figures above
// describe 2026-07 and nothing else — settings is a few hundred bytes now. They
// are kept with their date rather than restated, because that pair went stale
// once already without anyone noticing (see frontend/src/lib/sharedSnapshot.ts,
// which carries the same history). WHAT SURVIVES INTACT is the reason this file
// exists: null was being treated as "not written yet", so the poll ran its full
// ceiling for a row nobody will ever write. That is a bug about a TERMINAL STATE
// being read as a pending one, and it costs ~61 pointless round trips whether
// each one weighs 373 kB or 400 bytes.
//
// 🔴 THIS FILE IS ONE HALF OF A PAIRED CONTRACT. Treating null as terminal is
// only honest because the server persists the `running` report BEFORE the
// set-password 200 can reach any client (kickFirstRunOnboardingWith claims the
// slot synchronously, so it is done before the handler returns). The server
// half is pinned by TestOnboardingClaimIsPersistedBeforeKickReturns and
// TestSetPasswordLeavesNoNullOnboardingWindow in
// server/ocserverd/onboarding_contract_test.go. Break one side and the other
// side's guard is what tells you.
//
// The fix must not buy the saving with silence: the ONLY timeline this banner
// exists for is the fresh install whose verdict lands ~30 s after the password
// is set, and that arrives as `running` → `failed`, never as null. Both halves
// are pinned here, and the counts are the assertions — "was called" would pass
// either way.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
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

const runningReport = {
  state: "running",
  startedAt: 1,
  finishedAt: 0,
  steps: [],
};

const failedReport = {
  state: "failed",
  startedAt: 1,
  finishedAt: 2,
  steps: [
    {
      name: "install_warden",
      ok: false,
      reason: "installing this machine's warden failed (exit 1)",
      detail: "",
    },
  ],
};

function renderBanner() {
  return render(
    <I18nProvider>
      <OnboardingBanner />
    </I18nProvider>
  );
}

/** Drive the poll past its own ceiling. Each step flushes the microtask queue
 * so a settled fetch can schedule the next timer before the clock moves on. */
async function runPastCeiling() {
  const steps = Math.ceil(ONBOARDING_POLL_CEILING_MS / ONBOARDING_POLL_MS) + 5;
  for (let i = 0; i < steps; i++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS);
    });
  }
}

describe("OnboardingBanner — a null report is terminal", () => {
  beforeEach(() => {
    sessionStorage.clear();
    getServerSettings.mockReset();
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  // ── half 1: the production install. Exactly ONE read, for the whole three
  // minutes the poll used to run.
  it("stops after ONE read when onboarding never ran", async () => {
    getServerSettings.mockResolvedValue(settingsWith(null));

    renderBanner();
    await act(async () => {});
    expect(getServerSettings).toHaveBeenCalledTimes(1);

    await runPastCeiling();

    // Before the fix this was ~61. The number is the point: a "was called"
    // assertion is satisfied by the broken behaviour too.
    expect(getServerSettings).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  // ── half 2: THE case the banner exists for, and the reason this is still not
  // a one-shot fetch. A real first run reports `running` from the very first
  // read (the server claims the row before the set-password 200 is written),
  // and the verdict lands tens of seconds later with nobody reloading.
  it("keeps polling through `running` and shows the ~30s verdict without a reload", async () => {
    getServerSettings
      .mockResolvedValueOnce(settingsWith(runningReport))
      .mockResolvedValueOnce(settingsWith(runningReport))
      .mockResolvedValueOnce(settingsWith(runningReport))
      .mockResolvedValueOnce(settingsWith(runningReport))
      .mockResolvedValue(settingsWith(failedReport));

    renderBanner();
    await act(async () => {});
    expect(getServerSettings).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();

    for (let i = 0; i < 5; i++) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS);
      });
    }

    expect(getServerSettings.mock.calls.length).toBeGreaterThanOrEqual(5);
    expect(screen.getByTestId("onboarding-banner").textContent).toContain(
      "installing this machine's warden failed (exit 1)"
    );
  });

  // A read that FAILED is not a null report, and must not be collapsed into
  // one: a transient blip during first-run boot is exactly when this banner is
  // trying to be useful. (Without this the "null is terminal" rule is one
  // careless edit away from swallowing errors too.)
  it("keeps polling when the settings READ fails — a failure is not a null report", async () => {
    getServerSettings
      .mockRejectedValueOnce(new Error("network down"))
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValue(settingsWith(failedReport));

    renderBanner();
    await act(async () => {});
    expect(getServerSettings).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();

    for (let i = 0; i < 3; i++) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS);
      });
    }

    expect(screen.getByTestId("onboarding-banner").textContent).toContain(
      "installing this machine's warden failed (exit 1)"
    );
  });
});
