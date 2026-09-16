// CT stories for the 「設定改了但還沒生效」 hint lines (T-7f28) — the four
// `PendingHint` lines AgentDetailPanel renders under 執行環境 / 模型 / 思考強度
// (left cell) and 機器 (right cell).
//
// WHY THESE EXIST AT ALL: T-57 found two guards still feeding the panel a
// `modelEffortNote`, a field of the OLD in-place model/effort editor that
// c-era T-7f28 deleted outright. Deleting that residue would have left the
// mechanism that REPLACED it with no real-browser guard whatsoever, so the
// owner ruled 「這一包就把新機制的檢查一起補上」 and this is that half.
//
// 🔴 SCOPE — read before trusting a green run here.
// These stories hand the panel a `pending: { runtime, model, effort, machine }`
// object of FINISHED STRINGS. That is deliberate (it is the only way to stage
// all four cells lit at once, at chosen lengths, with no api and no member
// fixture), but it means the DECISION RULE is NOT under test here:
// `pendingChangeHint()` in `src/lib/pendingChange.ts` — 「回報值未知一律不標」,
// 「相等一律不標」 — is bypassed entirely. Rewrite `pendingChangeHint` to
// `return label(configured)` unconditionally and every test in
// `pending-change-hints.ct.spec.tsx` still passes.
//   · The RULE is guarded by `src/components/AgentDetailPanel.pending-change.test.tsx`
//     (jsdom, via the real MemberDetailPanel and a real member fixture).
//   · THIS pair guards the RENDERING and the LAYOUT — that the hint is its own
//     line under its own value rather than a tail on it, that four lit cells
//     survive a phone width, that an unlit panel really renders no node, and
//     that the line is visually subordinate to the value it annotates.
// Do not describe this guard as covering the rule. It does not.
//
// The hint TEXT comes from the real `msg.*` composers rather than literals, so
// the wording the owner sees and the wording asserted cannot drift apart — but
// note that is a wording tie, not a rule tie (see above).
import { useI18n, I18nProvider } from "../../src/i18n";
import {
  AgentDetailPanel,
  notHere,
  runtimeLabel,
  type AgentDetailSlots,
  type AgentDetailVM,
} from "../../src/components/AgentDetailPanel";

const NO_SLOTS: AgentDetailSlots = {
  overlays: notHere("此故事只量 pending 提示行的版面"),
  afterIdentityCards: notHere("此故事只量 pending 提示行的版面"),
  afterInfoCards: notHere("此故事只量 pending 提示行的版面"),
  extraExpandCards: notHere("此故事只量 pending 提示行的版面"),
  afterPromptCards: notHere("此故事只量 pending 提示行的版面"),
};

const NOOP_LABEL = (s: string) => s;

/** Deliberately long, and deliberately REAL in shape: model ids in this fleet
 * are dated slugs and machine ids are owner-set hostnames, both unbreakable
 * single tokens. A hint line is the worst case for the info card because it
 * sits in a `grid-template-columns: 1fr 1fr` half — i.e. roughly 160px at
 * 375px wide — so if anything in this card can push the page sideways, it is
 * one of these four lines. */
const REPORTED_MODEL = "claude-sonnet-4-7-20260514-legacy-long-identifier";
const CONFIGURED_MODEL = "claude-opus-4-8-thinking-20260901-preview-extended";
const REPORTED_MACHINE = "eva-m5-mac-studio-primary-worker-node-long-owner-label";
const CONFIGURED_MACHINE = "seth-m1-mac-mini-spare-runner-node-equally-long-label";

/** The steady, nothing-outstanding half of the view model — identical in both
 * arms, so the ONLY difference between the lit story and the dark one is the
 * `pending` object. Anything else that differed would make the height
 * comparison measure two things at once. */
const baseVM: Omit<AgentDetailVM, "testIdPrefix"> = {
  online: true,
  runtime: "codex",
  reportedRuntime: "claude",
  model: REPORTED_MODEL,
  effort: "medium",
  machineText: REPORTED_MACHINE,
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
  terminalAttachCommand: "tmux -L officraft attach -t member-mira",
  terminalHint: "hint",
  terminalUnavailable: "",
};

/** Build the four hint strings the way the REAL wrappers build them —
 * `msg.agentPendingChange` for the three value cells, `msg.memberMachineMovingTo`
 * for the machine cell (a place, not a value: 要換到 vs 要換成). Hook-based
 * because the composers live on the i18n context. */
function useAllFourPending(): NonNullable<AgentDetailVM["pending"]> {
  const { msg } = useI18n();
  return {
    runtime: msg.agentPendingChange(runtimeLabel("codex")),
    model: msg.agentPendingChange(CONFIGURED_MODEL),
    effort: msg.agentPendingChange("xhigh"),
    machine: msg.memberMachineMovingTo(CONFIGURED_MACHINE),
  };
}

function Panel({
  prefix,
  pending,
}: {
  prefix: string;
  pending?: NonNullable<AgentDetailVM["pending"]>;
}) {
  return (
    <AgentDetailPanel
      onBack={() => {}}
      identity={<div className="mp-card mp-identity">agent</div>}
      slots={NO_SLOTS}
      vm={{ ...baseVM, testIdPrefix: prefix, ...(pending ? { pending } : {}) }}
    />
  );
}

function LitPanel({ prefix }: { prefix: string }) {
  return <Panel prefix={prefix} pending={useAllFourPending()} />;
}

/** All four cells outstanding at once — the densest state the panel can reach,
 * and the one the owner accepted the feature on condition of (「又不想要畫面太
 * 雜亂」). */
export function PendingChangeMemberStory() {
  return (
    <I18nProvider>
      <div className="app__main">
        <LitPanel prefix="mp" />
      </div>
    </I18nProvider>
  );
}

/** The outsource arm of the SAME component — one implementation, both kinds.
 * Only `testIdPrefix` differs, which is the point: if the hints were ever wired
 * per-page instead of in the shared panel, this arm would go dark. */
export function PendingChangeWorkerStory() {
  return (
    <I18nProvider>
      <div className="app__main">
        <LitPanel prefix="worker-detail" />
      </div>
    </I18nProvider>
  );
}

/** NEGATIVE CONTROL — nothing pending, so `PendingHint` must return null on all
 * four cells: no node, no placeholder, no spacer. The panel has to be the panel
 * that existed before this feature did. */
export function PendingChangeNoneStory() {
  return (
    <I18nProvider>
      <div className="app__main">
        <Panel prefix="mp" />
      </div>
    </I18nProvider>
  );
}

/** Both arms in ONE mount, stacked so they render at the SAME width — this is
 * how the guard compares the lit card's height against the dark card's by
 * MEASURING BOTH, instead of pinning a pixel constant that would encode
 * today's font metrics as a requirement. Scope every locator by the
 * `data-surface` wrapper: the two panels share a testId prefix on purpose (so
 * the comparison is like-for-like) and their testIds therefore collide at
 * document scope. */
export function PendingChangeHeightPairStory() {
  return (
    <I18nProvider>
      <div className="app__main">
        <div data-surface="lit">
          <LitPanel prefix="mp" />
        </div>
        <div data-surface="dark">
          <Panel prefix="mp" />
        </div>
      </div>
    </I18nProvider>
  );
}
