import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";
import { toMachine, toMonitoring } from "./mappers";
import type { WireMachine } from "./wire";

const WIRE_UPGRADE = {
  upgrade_id: "ru-1",
  machine_id: "m-box",
  runtime: "claude",
  state: "running",
  from_version: "2.1.200",
  to_version: null,
  reason: null,
  started_ts: 1800000000,
  updated_ts: 1800000003,
};

const fetchMock = vi.fn(
  async () => new Response(JSON.stringify(WIRE_UPGRADE), { status: 200, headers: { "Content-Type": "application/json" } })
);

async function requests(): Promise<{ method: string; url: string; body: string }[]> {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  return Promise.all(
    calls.map(async ([req]) => ({
      method: req.method,
      url: new URL(req.url).pathname,
      body: await req.clone().text(),
    }))
  );
}

beforeEach(() => {
  fetchMock.mockClear();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => vi.unstubAllGlobals());

describe("httpApi runtime upgrade", () => {
  it("under a start, it posts {runtime} to the machine's runtime-upgrade path and answers the upgrade in camelCase", async () => {
    const got = await httpApi.startRuntimeUpgrade("m-box", "claude");
    expect(await requests()).toEqual([
      { method: "POST", url: "/api/machines/m-box/runtime-upgrade", body: '{"runtime":"claude"}' },
    ]);
    expect(got).toEqual({
      upgradeId: "ru-1",
      machineId: "m-box",
      runtime: "claude",
      state: "running",
      fromVersion: "2.1.200",
      toVersion: null,
      reason: null,
      startedTs: 1800000000,
      updatedTs: 1800000003,
    });
  });

  it("under a read, it gets the upgrade by machine and upgrade id with no body", async () => {
    await httpApi.getRuntimeUpgrade("m-box", "ru-1");
    expect(await requests()).toEqual([{ method: "GET", url: "/api/machines/m-box/runtime-upgrade/ru-1", body: "" }]);
  });
});

describe("runtime capability below_notify_minimum", () => {
  const caps = {
    claude: { installed: true, logged_in: true, version: "2.1.200", below_notify_minimum: true },
    codex: { installed: true, version: "0.52.0" },
  };
  const want = {
    claude: { installed: true, loggedIn: true, version: "2.1.200", belowNotifyMinimum: true },
    codex: { installed: true, loggedIn: null, version: "0.52.0", belowNotifyMinimum: null },
  };

  it("under a reported flag, both the machine row and the monitoring projection carry it, and an absent one reads null", () => {
    const machine: WireMachine = {
      machine_id: "m-box",
      display_name: "工作站",
      online: true,
      is_self: false,
      bin_status: null,
      runtime_capabilities: caps,
    };
    expect(toMachine(machine).runtimeCapabilities).toEqual(want);
    expect(
      toMonitoring({
        sessions: [],
        accounts: [],
        machines: [{ machine: "m-box", display_name: "工作站", agents: 0, runtime_capabilities: caps }],
      }).machines[0].runtimeCapabilities
    ).toEqual(want);
  });
});
