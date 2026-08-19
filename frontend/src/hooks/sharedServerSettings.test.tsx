// T-8115 — 一次座艙載入只讀一份 /api/settings.
//
// MEASURED PREMISE: the production `GET /api/settings` body is 639,270 bytes
// uncompressed (373 kB gzipped; `custom_themes` is 626,721 of it — 98%, and
// the other fifteen fields add up to ~2.5 kB). FIVE independent consumers each
// mount-fetched it on one cockpit load — the topbar studio name, the topbar
// owner nickname, the display-pref login reconcile, the 外包 parallel cap, and
// the onboarding banner — for ~3.2 MB of identical bytes.
//
// (T-83ef has since moved `custom_themes` off settings entirely — that 98% is
// history, not the current body. What it did NOT change is the claim below: the
// remaining fields still have six consumers, and they still must cost ONE GET.)
//
// The subject here is the REQUEST COUNT and the value each consumer ends up
// showing. "getServerSettings was called" is true in both worlds and would pin
// nothing.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act, waitFor } from "@testing-library/react";

const h = vi.hoisted(() => ({
  getServerSettings: vi.fn(),
  patchServerSettings: vi.fn(),
  listOutsourceWorkers: vi.fn(),
  subscribeEvents: vi.fn(),
  // T-83ef: the i18n reconcile now reads BOTH faces — settings still owns WHICH
  // theme is active, `GET /api/themes` owns what themes there are. This fake
  // answers the narrow row the real endpoint answers (id + name, never a
  // bundle); `getTheme` is the only door to a bundle and there is none saved
  // here, so it rejects the way the server 404s rather than inventing one.
  listThemes: vi.fn(),
  getTheme: vi.fn(),
}));

vi.mock("../api", () => ({
  api: {
    getServerSettings: h.getServerSettings,
    patchServerSettings: h.patchServerSettings,
    listOutsourceWorkers: h.listOutsourceWorkers,
    subscribeEvents: h.subscribeEvents,
    listThemes: h.listThemes,
    getTheme: h.getTheme,
  },
}));

import { useOrgName } from "./useOrgName";
import { useOwnerName } from "./useOwnerName";
import { useOutsourceWorkers } from "./useOutsourceWorkers";
import { useServerSettings } from "./useServerSettings";
import { OnboardingBanner } from "../components/OnboardingBanner";
import { I18nProvider } from "../i18n";
import {
  loadServerSettings,
  refreshServerSettings,
  adoptServerSettings,
} from "./sharedServerSettings";
import { setToken } from "../api/auth";

const SETTINGS = {
  orgName: "貨運工作室",
  ownerName: "伊娃",
  outsourceMaxParallel: 4,
  docCapCharsDuty: 1000,
  docCapCharsInsight: 20000,
  docCapCharsLearning: 20000,
  docCapCharsManualSop: 20000,
  docCapCharsManualLearnings: 20000,
  tokenTTL: 86400,
  handoverPct: 70,
  displayTheme: "",
  displayLanguage: "",
  displayWide: false,
  pushContactEmail: "",
  onboarding: null,
};

/** The five real cockpit-load consumers, mounted together exactly as App +
 * OfficePage + SettingsPage mount them. I18nProvider brings the sixth
 * (the display-pref reconcile) with it once a token exists. */
function Cockpit() {
  const { orgName } = useOrgName("預設工作室");
  const { ownerName } = useOwnerName("使用者");
  const { maxParallel } = useOutsourceWorkers();
  const { settings } = useServerSettings();
  return (
    <div>
      <span data-testid="org">{orgName}</span>
      <span data-testid="owner">{ownerName}</span>
      <span data-testid="cap">{String(maxParallel)}</span>
      <span data-testid="cap-chars">{String(settings?.docCapCharsLearning)}</span>
      <OnboardingBanner />
    </div>
  );
}

function renderCockpit() {
  return render(
    <I18nProvider>
      <Cockpit />
    </I18nProvider>
  );
}

beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  h.getServerSettings.mockReset().mockResolvedValue(SETTINGS);
  h.patchServerSettings.mockReset();
  h.listOutsourceWorkers.mockReset().mockResolvedValue([]);
  h.subscribeEvents.mockReset().mockReturnValue(() => {});
  h.listThemes.mockReset().mockResolvedValue([]);
  h.getTheme.mockReset().mockRejectedValue(new Error("no such theme"));
});

describe("shared /api/settings snapshot", () => {
  it("serves one cockpit load from ONE request — and every consumer still shows its own field", async () => {
    // A token makes the i18n login-reconcile read fire too (it is gated on
    // hasToken) — the sixth caller, and the one most likely to be forgotten.
    setToken("owner-jwt");

    renderCockpit();

    await waitFor(() =>
      expect(screen.getByTestId("org").textContent).toBe("貨運工作室")
    );
    await waitFor(() =>
      expect(screen.getByTestId("cap").textContent).toBe("4")
    );

    // 🔴 THE assertion. Six consumers, one round trip. Before the merge this
    // read 6.
    expect(h.getServerSettings).toHaveBeenCalledTimes(1);
    // …and nobody lost their value to the sharing.
    expect(screen.getByTestId("owner").textContent).toBe("伊娃");
    expect(screen.getByTestId("cap-chars").textContent).toBe("20000");
  });

  it("does not re-download for a consumer that mounts after the answer landed", async () => {
    renderCockpit();
    await waitFor(() =>
      expect(screen.getByTestId("cap").textContent).toBe("4")
    );
    expect(h.getServerSettings).toHaveBeenCalledTimes(1);

    // A panel opening later (設定 › 版本紀錄, the 外包 gear…) asks again.
    await act(async () => {
      await loadServerSettings();
    });
    expect(h.getServerSettings).toHaveBeenCalledTimes(1);
  });

  it("adopts a save echo — the cache's ONE invalidation point", async () => {
    await act(async () => {
      await loadServerSettings();
    });
    expect(h.getServerSettings).toHaveBeenCalledTimes(1);

    adoptServerSettings({ ...SETTINGS, orgName: "改過的名字" } as never);

    // Still no new request, and the next reader sees the SAVED value, not the
    // pre-save one. A cache that kept serving 貨運工作室 here would be a screen
    // that lies.
    const seen = await loadServerSettings();
    expect(h.getServerSettings).toHaveBeenCalledTimes(1);
    expect(seen.orgName).toBe("改過的名字");
  });

  it("lets a save echo win over a GET that was already in flight (generation guard)", async () => {
    let releaseSlowGet: (v: unknown) => void = () => {};
    h.getServerSettings.mockImplementationOnce(
      () => new Promise((resolve) => (releaseSlowGet = resolve))
    );

    const slow = loadServerSettings();
    // The owner saves while that GET is still on the wire.
    adoptServerSettings({ ...SETTINGS, orgName: "剛存的名字" } as never);
    await act(async () => {
      releaseSlowGet({ ...SETTINGS, orgName: "存檔前的舊名字" });
      await slow;
    });

    // The stale answer reached its own caller but must NOT have overwritten the
    // newer truth — otherwise a save silently un-does itself moments later.
    const seen = await loadServerSettings();
    expect(seen.orgName).toBe("剛存的名字");
    expect(h.getServerSettings).toHaveBeenCalledTimes(1);
  });

  it("refresh() always goes to the server (the onboarding poll / 存檔測連通 path)", async () => {
    await act(async () => {
      await loadServerSettings();
    });
    expect(h.getServerSettings).toHaveBeenCalledTimes(1);

    await act(async () => {
      await refreshServerSettings();
      await refreshServerSettings();
    });
    expect(h.getServerSettings).toHaveBeenCalledTimes(3);
  });

  it("drops the cached snapshot when the session identity changes", async () => {
    await act(async () => {
      await loadServerSettings();
    });
    expect(h.getServerSettings).toHaveBeenCalledTimes(1);

    // A login mints a different identity (api/auth setToken fires oc-auth-login)
    // — the previous session's settings must not be inherited.
    await act(async () => {
      setToken("a-different-owner-jwt");
    });
    await act(async () => {
      await loadServerSettings();
    });
    expect(h.getServerSettings).toHaveBeenCalledTimes(2);
  });
});
