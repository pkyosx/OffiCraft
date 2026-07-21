// T-ba62 — the in-place install log must survive a SUCCESSFUL install.
//
// The old success branch did `setBootstrapTarget(null)` and never stored the
// result, on the reasoning that the row flipping online is signal enough. But
// `ocwarden install` used to emit its most important output — "claude CLI not
// found — the warden will refuse every spawn" — as a WARNING while still
// exiting 0. So the one branch that discarded the log was exactly the branch
// that carried the warning, and "installed cleanly" rendered identically to
// "installed a warden that cannot spawn anything".
//
// Both edges are pinned so the pair is red/green: a mutant that shows nothing
// on success reddens the first test, one that labels everything a failure
// reddens the success-wording assertion.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
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
const bootstrapOnServer = vi.fn();

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    bootstrapOnServer: (id: string) => bootstrapOnServer(id),
    subscribeEvents: () => () => {},
  },
}));

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage bootstrap-on-server log retention", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    listMachines.mockResolvedValue([
      {
        machineId: "m-server-self",
        displayName: "本機",
        online: false,
        isSelf: true,
        binStatus: null,
        claudeVersion: null,
        claudeCredSource: null,
        claudeSubReadable: null,
      },
    ]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
  });

  it("keeps and shows the log of a SUCCESSFUL install", async () => {
    const log =
      "[ocwarden install] claude:   /opt/homebrew/bin/claude\n" +
      "[ocwarden install] SUCCESS: warden is running and STABLE";
    bootstrapOnServer.mockResolvedValue({
      machineId: "m-server-self",
      ok: true,
      exitCode: 0,
      log,
    });
    renderMonitor();
    fireEvent.click(await screen.findByTestId("mon-install-btn"));

    const shown = await screen.findByTestId("mon-bootstrap-log");
    expect(shown.textContent).toContain("SUCCESS: warden is running and STABLE");
    // and it is labelled as a success, not dressed up as a failure
    const block = await screen.findByTestId("mon-bootstrap-result-block");
    expect(block.textContent).toContain("安裝完成");
    expect(block.textContent).not.toContain("安裝失敗");
  });

  it("still shows the log — and the failure wording — on a FAILED install", async () => {
    bootstrapOnServer.mockResolvedValue({
      machineId: "m-server-self",
      ok: false,
      exitCode: 1,
      log: "[ocwarden install] FATAL: claude_bin_unresolved: no claude CLI on this host",
    });
    renderMonitor();
    fireEvent.click(await screen.findByTestId("mon-install-btn"));

    const shown = await screen.findByTestId("mon-bootstrap-log");
    expect(shown.textContent).toContain("claude_bin_unresolved");
    const block = await screen.findByTestId("mon-bootstrap-result-block");
    expect(block.textContent).toContain("安裝失敗");
  });
});
