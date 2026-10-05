// Story — the 機器資訊 table in its looks: a plain row, a row whose
// Claude and Codex cells carry the 版本太舊 and 未登入 chips and whose disk was
// never measured, and a row whose telemetry went stale (過期 on every cell) on
// an offline machine whose cutover is 未生效 — the widest 機器 cell of those three. Codex's 版本太舊 is not something
// the server sends today (below_notify_minimum is Claude's alone); it is here
// because both columns render the same fields the same way and must fit them.
// More on request: a row whose Claude carries 版本太舊 alone, a remote machine
// with a real-length name and id, and one with a very long name. Mounted through the real MachinesTable with
// hand-built rows (no api), inside a 1000px box (the monitor page content is 996px at any desktop
// viewport from 1100px up).
import { I18nProvider } from "../../src/i18n";
import { MachinesTable } from "../../src/components/MonitorPage";
import type { MachineDiskUsageView, MachineView, MonMachineView } from "../../src/types";
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

const GIB = 1024 ** 3;

/** The widest total the 磁碟 column has to hold: "1023.9 GB". */
const diskUsage: MachineDiskUsageView = {
  measuredAt: Math.floor(Date.now() / 1000) - 12 * 60,
  totalBytes: Math.round(1023.9 * GIB),
  databaseBytes: Math.round(2.3 * GIB),
  backupsBytes: Math.round(12.7 * GIB),
  databaseMeasuredAt: Math.floor(Date.now() / 1000) - 5 * 60,
  workspaceBytes: Math.round(15.6 * GIB),
  conversationBytes: Math.round(7.7 * GIB),
  claudeConversationBytes: Math.round(3.5 * GIB),
  codexConversationBytes: Math.round(4.2 * GIB),
  otherBytes: Math.round(985.6 * GIB),
  members: [
    { memberId: "mira", name: "Mira", rosterStatus: "active", workspaceBytes: null, conversationBytes: null, totalBytes: Math.round(7.9 * GIB) },
    { memberId: "ow-151", name: "O-151", rosterStatus: "removed", workspaceBytes: null, conversationBytes: null, totalBytes: Math.round(3.9 * GIB) },
  ],
  diskFreeBytes: Math.round(384 * GIB),
  diskTotalBytes: Math.round(931.5 * GIB),
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
  diskUsage,
};

export type MachinesLayoutState = "normal" | "old" | "chips" | "stale" | "named" | "long";

const rows: Record<MachinesLayoutState, { machine: MachineView; hw: MonMachineView }> = {
  normal: { machine, hw: hardware },
  // A remote machine named the way machines are actually named: a short host
  // name and a full-length 12-hex id.
  named: {
    machine: { ...machine, machineId: "m-11b2e651011e", displayName: "eva-m5", isSelf: false },
    hw: { ...hardware, machine: "m-11b2e651011e", displayName: "eva-m5" },
  },
  // A name far longer than the frame leaves 機器 at 1280px.
  long: {
    machine: { ...machine, machineId: "m-c9479bc2d696", displayName: "Seth 的 Mac Studio（辦公室三樓靠窗）", isSelf: false },
    hw: { ...hardware, machine: "m-c9479bc2d696", displayName: "Seth 的 Mac Studio（辦公室三樓靠窗）" },
  },
  old: {
    machine: { ...machine, claudeVersion: "2.1.286" },
    hw: {
      ...hardware,
      runtimeCapabilities: {
        ...hardware.runtimeCapabilities,
        claude: { installed: true, loggedIn: true, version: "2.1.286", belowNotifyMinimum: true },
      },
    },
  },
  chips: {
    machine: { ...machine, claudeVersion: "2.1.286" },
    hw: {
      ...hardware,
      cpuPct: 16,
      diskUsage: null,
      runtimeCapabilities: {
        claude: { installed: true, loggedIn: false, version: "2.1.286", belowNotifyMinimum: true },
        codex: { installed: true, loggedIn: false, version: "0.159.2", belowNotifyMinimum: true },
      },
    },
  },
  stale: {
    machine: { ...machine, online: false, claudeVersion: "2.1.286", cutoverEffect: "not_effective" },
    hw: {
      ...hardware,
      cpuPct: null,
      ramPct: null,
      batteryPct: null,
      acPower: null,
      runtimeCapabilities: {
        claude: { installed: true, loggedIn: false, version: "2.1.286", belowNotifyMinimum: true },
        codex: { installed: true, loggedIn: false, version: "0.159.2", belowNotifyMinimum: true },
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
