// CT stories for T-53 成本歸零: the destructive button that sits in the 估計$
// cell head, and the confirm it opens.
//
// jsdom cannot answer either question these stories exist for. (1) The button
// shares a row with 換手 in the SIBLING cell head — two pills in two separate
// grid cells — so "do they line up?" needs a real layout engine; a unit test
// sees both at x=0. (2) The confirm is `position: fixed` over the panel, and
// whether it actually COVERS the figure it is about to destroy is likewise a
// real-browser question.
//
// The stories feed AgentDetailPanel a hand-built view model directly (no hooks,
// no api), so what is measured is the shared component's own layout.
import { I18nProvider } from "../../src/i18n";
import {
  AgentDetailPanel,
  notHere,
  slot,
  type AgentDetailSlots,
  type AgentDetailVM,
} from "../../src/components/AgentDetailPanel";

const NO_SLOTS: AgentDetailSlots = {
  overlays: notHere("此故事只量成本歸零那一格"),
  afterIdentityCards: notHere("此故事只量成本歸零那一格"),
  afterInfoCards: notHere("此故事只量成本歸零那一格"),
  extraExpandCards: notHere("此故事只量成本歸零那一格"),
  afterPromptCards: notHere("此故事只量成本歸零那一格"),
};

const NOOP_LABEL = (s: string) => s;

const baseVM: Omit<AgentDetailVM, "testIdPrefix"> = {
  online: true,
  model: "claude-opus-4-8",
  effort: "high",
  modelEffortNote: "note",
  machineText: "MBP 5",
  accountText: "shawn-claude",
  contextPct: 42,
  cost: 37,
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

/** The steady state: a member with spend on the clock, so the button is live. */
export function CostResetButtonStory() {
  return (
    <I18nProvider>
      <AgentDetailPanel
        onBack={() => {}}
        identity={<div className="mp-card mp-identity">member</div>}
        slots={NO_SLOTS}
        vm={{
          ...baseVM,
          testIdPrefix: "mp",
          onResetCost: async () => {},
        }}
      />
    </I18nProvider>
  );
}

/** Nothing measured — the cell reads the dash and the button must be dead,
 * which is the pair that can never be allowed to disagree. */
export function CostResetNothingToClearStory() {
  return (
    <I18nProvider>
      <AgentDetailPanel
        onBack={() => {}}
        identity={<div className="mp-card mp-identity">member</div>}
        slots={NO_SLOTS}
        vm={{
          ...baseVM,
          testIdPrefix: "mp",
          cost: null,
          onResetCost: async () => {},
        }}
      />
    </I18nProvider>
  );
}

/** The outsource arm of the same shared component — one implementation, both
 * kinds (DoD #4). Its extra card mirrors the convergence story's. */
export function CostResetWorkerStory() {
  return (
    <I18nProvider>
      <AgentDetailPanel
        onBack={() => {}}
        identity={<div className="mp-card mp-identity">worker</div>}
        slots={{
          ...NO_SLOTS,
          afterInfoCards: slot(
            <div className="mp-card">
              <div className="mp-field">delegator</div>
            </div>,
          ),
        }}
        vm={{
          ...baseVM,
          testIdPrefix: "worker-detail",
          tmuxSession: "worker-ow-1",
          onResetCost: async () => {},
        }}
      />
    </I18nProvider>
  );
}
