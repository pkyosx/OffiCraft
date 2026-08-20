// CT stories for the T-ba6b detail-panel convergence: BOTH the member and the
// outsource worker now render through the ONE AgentDetailPanel, so the shared
// info card (LEFT 模型/思考強度 · RIGHT 機器/Claude Account) is a `.mp-info2`
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
  notHere,
  slot,
  type AgentDetailSlots,
  type AgentDetailVM,
} from "../../src/components/AgentDetailPanel";

/** Every slot declined — these stories isolate the SHARED cards, so nothing
 * kind-specific belongs on screen. Since T-0b4f the panel takes the whole slot
 * map, so "this story wants none of them" has to be said, not left out. */
const NO_SLOTS: AgentDetailSlots = {
  overlays: notHere("此故事只量共用卡片的版面"),
  afterIdentityCards: notHere("此故事只量共用卡片的版面"),
  afterInfoCards: notHere("此故事只量共用卡片的版面"),
  extraExpandCards: notHere("此故事只量共用卡片的版面"),
  afterPromptCards: notHere("此故事只量共用卡片的版面"),
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
        slots={NO_SLOTS}
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
        // The worker's own extra card is a single 委託人 field since T-7526
        // (its 狀態 half retired — owner 2026-07-31), so it is a plain .mp-card:
        // the assertion below deliberately reads the SHARED grid via .first(),
        // and a second .mp-info2 here would let a regression hide behind it.
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
        }}
      />
    </I18nProvider>
  );
}
