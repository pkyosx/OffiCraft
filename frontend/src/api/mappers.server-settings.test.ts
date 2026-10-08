import { describe, it, expect } from "vitest";
import { toServerSettings } from "./mappers";
import type { WireServerSettings } from "./wire";

function settingsFromServer(extra: Record<string, unknown> = {}): WireServerSettings {
  return {
    owner_token_ttl: 86400,
    agent_token_ttl: 604800,
    handover_pct: 50,
    notice_pct: 40,
    codex_notice_round: 2,
    ...extra,
  } as unknown as WireServerSettings;
}

describe("toServerSettings", () => {
  it("under a server that omits the runtime login intervals, the check reads as the 300s and the recheck as the 30s shipped default; sent values are carried as is", () => {
    const omitted = toServerSettings(settingsFromServer());
    expect(omitted.runtimeLoginCheckIntervalSecs).toBe(300);
    expect(omitted.runtimeLoginRecheckIntervalSecs).toBe(30);
    const sent = toServerSettings(
      settingsFromServer({ runtime_login_check_interval_secs: 120, runtime_login_recheck_interval_secs: 90 }),
    );
    expect(sent.runtimeLoginCheckIntervalSecs).toBe(120);
    expect(sent.runtimeLoginRecheckIntervalSecs).toBe(90);
  });

  it("under a server that omits the disk usage interval, it reads as the 3600s shipped default; a sent value is carried as is", () => {
    expect(toServerSettings(settingsFromServer()).diskUsageIntervalSecs).toBe(3600);
    expect(toServerSettings(settingsFromServer({ disk_usage_interval_secs: 7200 })).diskUsageIntervalSecs).toBe(7200);
  });

  it("under a server that omits display_seasonal_theme, it reads as the shipped true; a sent false is carried as is", () => {
    expect(toServerSettings(settingsFromServer()).displaySeasonalTheme).toBe(true);
    expect(toServerSettings(settingsFromServer({ display_seasonal_theme: false })).displaySeasonalTheme).toBe(false);
  });

  it("under a server that omits the update check interval, it reads as the 300s shipped default; a sent value is carried as is", () => {
    expect(toServerSettings(settingsFromServer()).updaterCheckIntervalSecs).toBe(300);
    expect(toServerSettings(settingsFromServer({ updater_check_interval_secs: 900 })).updaterCheckIntervalSecs).toBe(900);
  });
});
