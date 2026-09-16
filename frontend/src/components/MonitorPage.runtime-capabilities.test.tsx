// Per-runtime version columns — Monitor §2 machine panel (T-674d, replacing
// the single ✓/✗ Runtimes column of T-90be ⑤ + T-b36a).
//
// Why the column changed: the old cell spent a full table column to say
// "claude ✓ · codex ✓" — two bits of information — while the version an
// operator actually wants was either in a different column (claude) or nowhere
// at all (codex). Claude and Codex now each print their probed version.
//
// What must NOT be lost in the trade: `machineSupportsRuntime` fail-closes when
// it cannot read `installed`/`logged_in`, so a machine whose codex probe says
// "not logged in" silently stops accepting codex work and its worker sits
// stamped machine_unavailable. That was the ✗'s whole job. A version-only cell
// would render that machine as a blank — which reads as "unknown" and is a
// different, wrong claim. So the refusal states are spelled out in words, and
// "signed out" rides alongside a perfectly good version number.
//
// And the age discipline is unchanged: telemetry is never cleared on
// disconnect, so these values outlive the machine that reported them. A
// capability rendered plain would be the same lie the hardware numbers used to
// tell — "as of some unknown moment" presented as "right now".

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, MonMachineView } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [],
  machines: [] as MonMachineView[],
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    getBackupHealth: () =>
      Promise.resolve({
        status: "healthy",
        code: "",
        detail: "",
        newestBackupTs: 1785600000,
        newestBackupAgeSecs: 3600,
        staleAfterSecs: 43200,
        sinceTs: null,
        checkedTs: 1785603600,
      }),
    subscribeEvents: () => () => {},
  },
}));

const registryRow = (
  id: string,
  claudeVersion: string | null = null
): MachineView => ({
  machineId: id,
  displayName: "seth-m5",
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion,
  claudeCredSource: null,
  claudeSubReadable: null,
});

/** One monitoring card. `stale` is the SERVER's verdict (the freshness window
 * lives there, not here) — null means the machine never probed. */
const card = (
  stale: boolean | null,
  capabilities: MonMachineView["runtimeCapabilities"]
): MonMachineView => ({
  machine: "m-server-self",
  displayName: "seth-m5",
  agents: 0,
  accounts: [],
  cpuPct: null,
  ramPct: null,
  batteryPct: null,
  acPower: null,
  hardwareTs: null,
  hardwareStale: null,
  hardwareInvalid: [],
  runtimeCapabilities: capabilities,
  runtimeCapabilitiesTs: stale == null ? null : 1_700_000_000,
  runtimeCapabilitiesStale: stale,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

function mount(
  machineCard: MonMachineView | null,
  row = registryRow("m-server-self")
) {
  listMembers.mockResolvedValue([
    {
      id: "m-server-self",
      name: "warden",
      kind: "warden",
      desiredMachineId: "",
    } as unknown as Member,
  ]);
  listMachines.mockResolvedValue([row]);
  getMonitoring.mockResolvedValue({
    accounts: [],
    sessions: [],
    machines: machineCard ? [machineCard] : [],
  });
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

const FRESH = {
  claude: { installed: true, loggedIn: true, version: "2.1.211" },
  codex: { installed: true, loggedIn: false, version: "0.52.0" },
};

describe("MonitorPage per-runtime version columns", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("prints each runtime's probed version in its own column", async () => {
    mount(card(false, FRESH));
    const claude = await screen.findByTestId("mon-claude-version");
    const codex = await screen.findByTestId("mon-codex-version");
    expect(claude.textContent).toContain("2.1.211");
    // The codex version is the whole point of the new column: it was collected
    // by the warden and carried by the server all along, and the old ✓/✗ cell
    // threw it away.
    expect(codex.textContent).toContain("0.52.0");
    // Fresh means fresh: no stale marker on either cell.
    expect(within(claude).queryByTestId("mon-claude-stale")).toBeNull();
    expect(within(codex).queryByTestId("mon-codex-stale")).toBeNull();
  });

  it("shows a signed-out runtime as signed out, alongside its version", async () => {
    mount(card(false, FRESH));
    const codex = await screen.findByTestId("mon-codex-version");
    // codex is INSTALLED and its version is known, but it is NOT logged in —
    // the exact state that makes placement refuse this machine. Printing only
    // "0.52.0" would read as a healthy runtime.
    expect(within(codex).getByTestId("mon-codex-logged-out")).toBeTruthy();
    // …and claude, which IS logged in, must not wear the mark.
    const claude = await screen.findByTestId("mon-claude-version");
    expect(within(claude).queryByTestId("mon-claude-logged-out")).toBeNull();
  });

  it("names a not-installed runtime instead of leaving the cell blank", async () => {
    mount(
      card(false, {
        claude: { installed: true, loggedIn: true, version: "2.1.211" },
        codex: { installed: false, loggedIn: null, version: null },
      })
    );
    const codex = await screen.findByTestId("mon-codex-version");
    // A reported false is an ANSWER. "—" would fold it back into "never told
    // us", which is the one thing this cell exists to distinguish.
    expect(codex.textContent).not.toBe("—");
    expect(codex.textContent).toContain("未安裝");
    // owner 2026-07-31 (rc-b7d1c642f2d2): ONE verb. The hover hint explaining
    // WHY the runtime is unusable described the same act as 啟動 — a third
    // word for it. It lives in a title attribute, so textContent misses it.
    expect(codex.querySelector(".mon-muted")?.getAttribute("title")).toContain(
      "無法在此喚醒"
    );
  });

  it("says 'installed' when the binary resolved but its version probe did not", async () => {
    mount(
      card(false, {
        codex: { installed: true, loggedIn: true, version: null },
      })
    );
    const codex = await screen.findByTestId("mon-codex-version");
    expect(codex.textContent).toContain("已安裝");
  });

  it("marks a stale capability map instead of showing it as current", async () => {
    mount(card(true, FRESH));
    const codex = await screen.findByTestId("mon-codex-version");
    // The values SURVIVE — they are the only diagnosis for a worker that will
    // not place on this box…
    expect(codex.textContent).toContain("0.52.0");
    // …but they are explicitly no longer presented as current.
    expect(within(codex).getByTestId("mon-codex-stale")).toBeTruthy();
  });

  it("does not present a map of unknown age as current", async () => {
    // stale=null with values present is the fail-closed case (the server could
    // not date the probe). Unknown age is not freshness.
    mount(card(null, FRESH));
    const codex = await screen.findByTestId("mon-codex-version");
    expect(within(codex).getByTestId("mon-codex-stale")).toBeTruthy();
  });

  it("shows an honest dash for codex when the machine never reported a capability map", async () => {
    mount(card(null, {}));
    const codex = await screen.findByTestId("mon-codex-version");
    expect(codex.textContent).toBe("—");
    // "Never told us" must not wear the stale badge either — nothing expired.
    expect(within(codex).queryByTestId("mon-codex-stale")).toBeNull();
  });

  it("falls back to the registry claude_version when there is no capability map", async () => {
    // An older warden reports `claude_version` on the registry row without any
    // capability probes. The Claude column has shown that since T-7c5b and must
    // keep showing it — the new column is a rename, not a narrowing.
    mount(card(null, {}), registryRow("m-server-self", "2.1.220"));
    const claude = await screen.findByTestId("mon-claude-version");
    expect(claude.textContent).toBe("2.1.220");
    // Registry data is not telemetry, so it carries no freshness mark.
    expect(within(claude).queryByTestId("mon-claude-stale")).toBeNull();
    // Codex has no registry twin: it stays an honest unknown rather than
    // borrowing claude's number.
    const codex = await screen.findByTestId("mon-codex-version");
    expect(codex.textContent).toBe("—");
  });
});
