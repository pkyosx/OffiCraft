// CT story: mounts the REAL WorkerDetailPanel (not a hand-built AgentDetailPanel
// slot stub) with a bound task whose title is long, so the paired guards
// measure the ACTUAL identity card: codename, the task line (short task-type
// label) and the full task title, as WorkerDetailPanel's own JSX renders them.
import { I18nProvider } from "../../src/i18n";
import { WorkerDetailPanel } from "../../src/components/WorkerDetailPanel";
import type { OutsourceWorkerView } from "../../src/api/adapter";

const worker: OutsourceWorkerView = {
  id: "ow-1",
  avatarUrl:
    "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGM4EcUFAAMaAS0191t5AAAAAElFTkSuQmCC",
  codename: "O-19",
  model: "claude-opus-4-8",
  effort: "high",
  status: "active",
  taskId: "t-23cf5291a001",
  taskTitle: "拆發包 per-agent 白名單閘:發包全員可用,成本控制只靠 outsourceParallelCap(owner 2026-07-20 裁決)",
  taskStatus: "in_progress",
  taskNo: "t-23cf5291a001",
  taskTypeKey: "tm-05f7c776d6ff",
  taskTypeName: "OffiCraft 開發",
  presence: "online",
  machine: "Warden · mbp5",
  desiredMachineId: "",
  account: "shawn-claude",
  contextPct: 42,
  cost: 7,
  bankedCost: 0,
  creatorId: "owner",
  delegatedBy: "",
};

export function WorkerDetailPanelIdentityStory() {
  return (
    <I18nProvider>
      <WorkerDetailPanel
        worker={worker}
        onBack={() => {}}
        onUpdateAvatar={async () => {}}
        onRemoveAvatar={async () => {}}
      />
    </I18nProvider>
  );
}
