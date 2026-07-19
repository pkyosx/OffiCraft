// CT stories for the T-ba6b detail-panel convergence: BOTH the member and the
// outsource worker now render through the ONE AgentDetailPanel, so the shared
// info card (LEFT 模型/投入度 · RIGHT 機器/Claude Account) is a `.mp-info2`
// `grid-template-columns: 1fr 1fr` two-column layout for both identities. jsdom
// resolves no grid (it reports the two `.mp-field`s stacked), so the "are they
// really side by side, for both kinds?" question can only be answered in a real
// browser — that is what the paired visual guard measures.
//
// These stories feed AgentDetailPanel a hand-built view model directly (no
// hooks / no api), isolating the shared-component layout from the wrappers.
import { I18nProvider } from "../../src/i18n";
import {
  AgentDetailPanel,
  type AgentDetailVM,
} from "../../src/components/AgentDetailPanel";

const NOOP_LABEL = (s: string) => s;

const baseVM: Omit<AgentDetailVM, "testIdPrefix"> = {
  online: true,
  model: "claude-opus-4-8",
  effort: "high",
  modelEffortNote: "note",
  machineText: "MBP 5",
  accountText: "shawn-claude",
  contextPct: 42,
  cost: 7,
  refocusSince: null,
  refocusSubmittedNote: "sent",
  refocusSinceLabel: NOOP_LABEL,
  lastOp: "",
  lastOpVerb: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpReason: "",
  lastOpAt: null,
  tmuxSession: "member-mira",
  terminalHint: "hint",
};

export function MemberDetailConvergenceStory() {
  return (
    <I18nProvider>
      <AgentDetailPanel
        onBack={() => {}}
        identity={<div className="mp-card mp-identity">member</div>}
        vm={{ ...baseVM, testIdPrefix: "mp" }}
      />
    </I18nProvider>
  );
}

export function WorkerDetailConvergenceStory() {
  return (
    <I18nProvider>
      <AgentDetailPanel
        onBack={() => {}}
        identity={<div className="mp-card mp-identity">worker</div>}
        afterInfoCards={
          <div className="mp-card mp-info2">
            <div className="mp-field">status</div>
            <div className="mp-field mp-field--divider">delegator</div>
          </div>
        }
        vm={{
          ...baseVM,
          testIdPrefix: "worker-detail",
          tmuxSession: "worker-ow-1",
        }}
      />
    </I18nProvider>
  );
}
