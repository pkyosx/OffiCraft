// Story — the 機器資訊 table in its three looks: a plain row, a row whose
// Claude cell carries the 版本太舊 and 未登入 chips, and a row whose telemetry
// went stale (過期 on every cell). Mounted through the real MachinesTable with
// hand-built rows (no api), inside a 1000px box — the content width of the
// monitor page at a 1500px desktop viewport.
import { I18nProvider } from "../../src/i18n";
import { MachinesTable } from "../../src/components/MonitorPage";
import type { MachineView, MonMachineView } from "../../src/types";
import "../../src/components/monitor.css";

const machine: MachineView = {
  machineId: "m-server-self",
  displayName: "伺服器這一台",
  online: true,
  isSelf: true,
  binStatus: "current",
  wardenShape: "anchor",
  cutoverEffect: "effective",
  claudeVersion: "2.1.287",
  claudeCredSource: "keychain",
  claudeSubReadable: true,
};

const hardware: MonMachineView = {
  machine: "m-server-self",
  displayName: "伺服器這一台",
  agents: 1,
  accounts: [],
  cpuPct: 12,
  ramPct: 63,
  batteryPct: 80,
  acPower: true,
  binStatus: "current",
  wardenShape: "anchor",
  cutoverEffect: "effective",
  claudeVersion: "2.1.287",
  runtimeCapabilities: {
    claude: { installed: true, loggedIn: true, version: "2.1.287", belowNotifyMinimum: false },
    codex: { installed: true, loggedIn: true, version: "0.159.2" },
  },
  runtimeCapabilitiesTs: 1,
  runtimeCapabilitiesStale: false,
  hardwareTs: 1,
  hardwareStale: false,
  hardwareInvalid: [],
  claudeCredSource: "keychain",
  claudeSubReadable: true,
};

export type MachinesLayoutState = "normal" | "chips" | "stale";

const rows: Record<MachinesLayoutState, { machine: MachineView; hw: MonMachineView }> = {
  normal: { machine, hw: hardware },
  chips: {
    machine: { ...machine, claudeVersion: "2.1.286" },
    hw: {
      ...hardware,
      cpuPct: 16,
      runtimeCapabilities: {
        claude: { installed: true, loggedIn: false, version: "2.1.286", belowNotifyMinimum: true },
        codex: { installed: true, loggedIn: true, version: "0.159.2" },
      },
    },
  },
  stale: {
    machine: { ...machine, online: false, claudeVersion: "2.1.286" },
    hw: {
      ...hardware,
      cpuPct: null,
      ramPct: null,
      batteryPct: null,
      acPower: null,
      runtimeCapabilities: {
        claude: { installed: true, loggedIn: false, version: "2.1.286", belowNotifyMinimum: true },
        codex: { installed: true, loggedIn: true, version: "0.159.2" },
      },
      runtimeCapabilitiesStale: true,
      hardwareStale: true,
    },
  },
};

const noop = () => {};

export function MonitorMachinesLayoutStory({
  states = ["normal", "chips", "stale"],
  width = 1000,
}: {
  states?: MachinesLayoutState[];
  width?: number;
}) {
  return (
    <I18nProvider>
      <div className="monitor" style={{ width, maxWidth: "100%", margin: "0 auto" }}>
        {states.map((state) => (
          <section className="mon-section" key={state} data-state={state}>
            <MachinesTable
              machines={[rows[state].machine]}
              hwByHost={new Map([[rows[state].hw.machine, rows[state].hw]])}
              bootstrapBusy={false}
              uninstalling={() => false}
              onRename={noop}
              onLogin={noop}
              onUpgrade={noop}
              onInstall={noop}
              onUninstall={noop}
              onDelete={noop}
            />
          </section>
        ))}
      </div>
    </I18nProvider>
  );
}
