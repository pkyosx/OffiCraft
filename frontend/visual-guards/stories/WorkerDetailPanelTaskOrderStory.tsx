// CT story for T-b0e3/T-cd6f: mounts the REAL WorkerDetailPanel (not a hand-built
// AgentDetailPanel slot stub) with a bound task, so the paired visual guard
// measures the ACTUAL 委託任務-vs-模型/機器 card order and the ACTUAL header
// (short task-type label) — both of which only exist once WorkerDetailPanel's
// own slot-wiring and identity JSX run, not the shared-panel plumbing alone.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { WorkerDetailPanel } from "../../src/components/WorkerDetailPanel";
import type { OutsourceWorkerView } from "../../src/api/adapter";

const worker: OutsourceWorkerView = {
  id: "ow-1",
  avatarIndex: 14,
  codename: "O-19",
  model: "claude-opus-4-8",
  effort: "high",
  status: "active",
  taskId: "t-1",
  taskTitle: "拆發包 per-agent 白名單閘:發包全員可用,成本控制只靠 outsourceParallelCap(owner 2026-07-20 裁決)",
  taskStatus: "in_progress",
  taskNo: "T-23cf",
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

export function WorkerDetailPanelTaskOrderStory() {
  const [avatarIndex, setAvatarIndex] = useState(worker.avatarIndex);
  return (
    <I18nProvider>
      <WorkerDetailPanel
        worker={{ ...worker, avatarIndex }}
        onBack={() => {}}
        onUpdateAvatarIndex={async (next) => setAvatarIndex(next)}
      />
    </I18nProvider>
  );
}
