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
  it("under a server that omits runtime_login_check_interval_secs, the login check interval reads as the 300s shipped default; a sent value is carried as is", () => {
    expect(toServerSettings(settingsFromServer()).runtimeLoginCheckIntervalSecs).toBe(300);
    expect(
      toServerSettings(settingsFromServer({ runtime_login_check_interval_secs: 120 }))
        .runtimeLoginCheckIntervalSecs
    ).toBe(120);
  });
});
