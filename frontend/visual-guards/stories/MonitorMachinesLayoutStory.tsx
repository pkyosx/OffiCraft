// Story — the 機器資訊 table in its looks: a plain row, a row whose
// Claude and Codex cells carry the 版本太舊 and 未登入 chips and whose disk was
// never measured, and a row whose telemetry went stale (過期 on every cell) on
// an offline machine whose cutover is not in effect (the exclamation beside the
// dot) — the widest 機器 cell of those three. Codex's 版本太舊 is not something
// the server sends today (below_notify_minimum is Claude's alone); it is here
// because both columns render the same fields the same way and must fit them.
// More on request: a row whose Claude carries 版本太舊 alone, rows where only
// Claude or only Codex carries both chips, one whose Codex was never probed,
// a remote machine
// with a real-length name and id, and one with a name longer than 機器's cap,
// alone or with two marks in Claude and Codex. Mounted through the real MachinesTable with
// hand-built rows (no api), each state its own table unless `together` puts
// them in one, inside a 1000px box (the monitor page content is 996px at any desktop
// viewport from 1100px up).
import { useEffect, useMemo } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import { MachinesTable } from "../../src/components/MonitorPage";
import type { MachineDiskUsageView, MachineView, MonMachineView } from "../../src/types";
import { LONG_NAME } from "./monitorMachinesLayoutNames";
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

const cat = (key: string, bytes: number) => ({ key, parentKey: null, bytes, inRoot: true });

/** The widest total the 磁碟 column has to hold: "1023.9 GB". */
const diskUsage: MachineDiskUsageView = {
  measuredAt: Math.floor(Date.now() / 1000) - 12 * 60,
  totalBytes: Math.round(1023.9 * GIB),
  databaseMeasuredAt: Math.floor(Date.now() / 1000) - 5 * 60,
  categories: [
    // Each category a share the bar draws as more than a sliver.
    cat("database", Math.round(20 * GIB)),
    cat("backups", Math.round(100 * GIB)),
    cat("workspaces", Math.round(300 * GIB)),
    cat("logs", Math.round(50 * GIB)),
    cat("old_version_backups", Math.round(150 * GIB)),
    cat("other", Math.round(403.9 * GIB)),
  ],
  members: [
    { memberId: "mira", name: "Mira", rosterStatus: "active", workspaceBytes: Math.round(7.9 * GIB) },
    { memberId: "ow-151", name: "O-151", rosterStatus: "removed", workspaceBytes: Math.round(3.9 * GIB) },
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

export type MachinesLayoutState =
  | "normal"
  | "old"
  | "chips"
  | "stale"
  | "named"
  | "long"
  | "longmarks"
  | "claude2"
  | "codex2"
  | "nocodex"
  | "tiny"
  | "zerologs";

const rows: Record<MachinesLayoutState, { machine: MachineView; hw: MonMachineView }> = {
  normal: { machine, hw: hardware },
  // No logs at all: a top-level row with nothing to colour.
  zerologs: {
    machine,
    hw: {
      ...hardware,
      diskUsage: {
        ...diskUsage,
        categories: diskUsage.categories.map((c) =>
          c.key === "logs" ? { ...c, bytes: 0 } : c.key === "other" ? { ...c, bytes: Math.round(453.9 * GIB) } : c,
        ),
        // Long enough to wrap on a phone.
        members: [
          ...diskUsage.members,
          {
            memberId: "ow-long",
            name: "Seth 的 Mac Studio 上試用中的外包成員（夜間建置與發版測試）",
            rosterStatus: "active",
            workspaceBytes: Math.round(1.2 * GIB),
          },
        ],
      },
    },
  },
  // Slivers of the total at the bar's rounded start (the database, 424 KB as
  // on a real station) and in its middle (backups).
  tiny: {
    machine,
    hw: {
      ...hardware,
      diskUsage: {
        ...diskUsage,
        categories: diskUsage.categories.map((c) =>
          c.key === "database"
            ? { ...c, bytes: 424 * 1024 }
            : c.key === "backups"
              ? { ...c, bytes: Math.round(0.5 * GIB) }
              : c.key === "other"
                ? { ...c, bytes: Math.round(1023.9 * GIB) - 424 * 1024 - Math.round(0.5 * GIB) - Math.round(500 * GIB) }
                : c,
        ),
      },
    },
  },
  // A remote machine named the way machines are actually named: a short host
  // name and a full-length 12-hex id.
  named: {
    machine: { ...machine, machineId: "m-11b2e651011e", displayName: "eva-m5", isSelf: false },
    hw: { ...hardware, machine: "m-11b2e651011e", displayName: "eva-m5" },
  },
  // A name longer than 機器's cap: it wraps inside the column.
  long: {
    machine: { ...machine, machineId: "m-c9479bc2d696", displayName: LONG_NAME, isSelf: false },
    hw: { ...hardware, machine: "m-c9479bc2d696", displayName: LONG_NAME },
  },
  // The same name with two marks in Claude and in Codex: the row that still
  // scrolls at 1280px.
  longmarks: {
    machine: { ...machine, machineId: "m-c9479bc2d696", displayName: LONG_NAME, isSelf: false, claudeVersion: "2.1.286" },
    hw: {
      ...hardware,
      machine: "m-c9479bc2d696",
      displayName: LONG_NAME,
      runtimeCapabilities: {
        claude: { installed: true, loggedIn: false, version: "2.1.286", belowNotifyMinimum: true },
        codex: { installed: true, loggedIn: false, version: "0.159.2", belowNotifyMinimum: true },
      },
    },
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
  claude2: {
    machine: { ...machine, claudeVersion: "2.1.286" },
    hw: {
      ...hardware,
      runtimeCapabilities: {
        ...hardware.runtimeCapabilities,
        claude: { installed: true, loggedIn: false, version: "2.1.286", belowNotifyMinimum: true },
      },
    },
  },
  codex2: {
    machine,
    hw: {
      ...hardware,
      runtimeCapabilities: {
        ...hardware.runtimeCapabilities,
        codex: { installed: true, loggedIn: false, version: "0.159.2", belowNotifyMinimum: true },
      },
    },
  },
  // Codex never probed: its cell is a plain dash, not the menu's trigger.
  nocodex: {
    machine,
    hw: { ...hardware, runtimeCapabilities: { claude: hardware.runtimeCapabilities!.claude } },
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

/** Switches the mounted story's language in place, as the profile menu does. */
function LanguageSwitch({ language }: { language?: "zh" | "en" }) {
  const { language: current, setLanguage } = useI18n();
  useEffect(() => {
    if (language && language !== current) setLanguage(language);
  }, [language, current, setLanguage]);
  return null;
}

export function MonitorMachinesLayoutStory({
  states = ["normal", "chips", "stale"],
  width = 1000,
  empty = false,
  name,
  together = false,
  language,
}: {
  states?: MachinesLayoutState[];
  width?: number;
  /** Every table with no machine (the 尚無機器 row), keeping each section's
   * table mounted so a test can remove the rows of a measured table. */
  empty?: boolean;
  /** Every machine renamed to this, as a server-sent name (not an edit). */
  name?: string;
  /** One table holding a row per state, in order, the n-th row with machine id
   * `m-<n>` so each joins its own telemetry. The machine list keeps its
   * identity while its contents are equal, as the page's does, so a state
   * change that is telemetry alone (normal → codex2) re-renders with the same
   * `machines`. */
  together?: boolean;
  /** Changes the language without remounting anything. */
  language?: "zh" | "en";
}) {
  const machineOf = (state: MachinesLayoutState, id: string) => {
    const m = { ...rows[state].machine, machineId: id };
    return name === undefined ? m : { ...m, displayName: name };
  };
  const togetherMachines = empty ? [] : states.map((s, i) => machineOf(s, `m-${i}`));
  const togetherKey = JSON.stringify(togetherMachines);
  const stableTogether = useMemo(() => togetherMachines, [togetherKey]);
  const table = (key: string, list: MachinesLayoutState[], ids: (s: MachinesLayoutState, i: number) => string) => (
    <section className="mon-section" key={key} data-state={key}>
      <MachinesTable
        machines={empty ? [] : together ? stableTogether : list.map((s, i) => machineOf(s, ids(s, i)))}
        hwByHost={new Map(list.map((s, i) => [ids(s, i), { ...rows[s].hw, machine: ids(s, i) }]))}
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
  );
  return (
    <I18nProvider>
      <LanguageSwitch language={language} />
      <div className="monitor" style={{ width, maxWidth: "100%", margin: "0 auto" }}>
        {together
          ? table("together", states, (_, i) => `m-${i}`)
          : states.map((state) => table(state, [state], (s) => rows[s].machine.machineId))}
      </div>
    </I18nProvider>
  );
}
