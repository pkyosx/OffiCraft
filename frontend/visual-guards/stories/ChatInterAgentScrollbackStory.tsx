// T-124 repro story: the REAL <ChatArea> + the REAL useChat against an api seam
// whose OLDER history is entirely 成員間對話 (agent↔agent), so every prepended
// page folds into ONE collapsed block instead of adding real rows.
//
// 🔴 WHY IT MUST BE A BROWSER. The claim under test is 「往上捲載不到更舊的訊
// 息」 — a claim about scroll events that a real scroller does or does not emit
// when it is pinned at its top limit, and about how much HEIGHT a prepend adds.
// jsdom has neither: every box is 0px tall and a test writes scrollTop by hand,
// so the stall is structurally invisible there.
import { I18nProvider } from "../../src/i18n";
import { ChatArea } from "../../src/components/ChatArea";
import { api } from "../../src/api";
import type { ChatMessage } from "../../src/api/adapter";
import type { Member } from "../../src/types";
import "../../src/components/office.css";
import { OLDER_PAGES, TOTAL_MESSAGES } from "./chatInterAgentScrollbackFixtures";

const OWNER = "owner";
const PEER = "m-cccccccccccc";
const OTHER = "m-dddddddddddd";

// All timestamps land in the same local day so `splitByDay` cannot break the
// inter-agent run into several blocks by accident — the run under test is ONE
// contiguous run, which is what makes a prepend fold into the block already on
// screen and add no height at all.
const BASE_TS = Math.floor(Date.now() / 1000) - TOTAL_MESSAGES - 60;

// The stream, oldest→newest. Its NEWEST page is deliberately the shape owner
// photographed in c-fc47d568cc14: 30 messages of which 28 are 成員間對話 folded
// into three one-line blocks, and only 2 are owner↔peer bubbles. Rendered, that
// whole page is a few hundred pixels tall — SHORTER THAN THE PANE.
const log: ChatMessage[] = [];
let seq = 0;
function pushInter(n: number) {
  for (let i = 0; i < n; i += 1) {
    const k = seq++;
    log.push({
      id: `I${String(k).padStart(3, "0")}`,
      from: k % 2 === 0 ? PEER : OTHER,
      to: k % 2 === 0 ? OTHER : PEER,
      body: `成員間第 ${k} 則 —— 一句普通長度的內容,好讓展開後每一列都有真的高度。`,
      ts: BASE_TS + k,
      attachments: [],
      replyCardId: null,
    });
  }
}
function pushNormal(n: number) {
  for (let i = 0; i < n; i += 1) {
    const k = seq++;
    log.push({
      id: `N${String(k).padStart(3, "0")}`,
      from: k % 2 === 0 ? PEER : OWNER,
      to: k % 2 === 0 ? OWNER : PEER,
      body: `第 ${k} 則訊息 —— 一句普通長度的聊天內容,好讓每一列都有真的高度。`,
      ts: BASE_TS + k,
      attachments: [],
      replyCardId: null,
    });
  }
}
// One page of the screenshot's own composition: 28 成員間對話 in three runs,
// two owner↔peer bubbles between them.
function pushPage() {
  pushInter(14);
  pushNormal(1);
  pushInter(12);
  pushNormal(1);
  pushInter(2);
}
// The page the thread opens on, plus OLDER_PAGES more of the same shape above
// it — every page a scrollback pulls is again mostly collapsed.
for (let page = 0; page < OLDER_PAGES + 1; page += 1) pushPage();

api.listChat = async (
  _withId: string,
  limit?: number,
  cursor?: { beforeTs: number; beforeId: string },
) => {
  const size = limit ?? 30;
  if (cursor) {
    return log
      .filter(
        (m) =>
          m.ts < cursor.beforeTs ||
          (m.ts === cursor.beforeTs && m.id < cursor.beforeId),
      )
      .slice(-size);
  }
  return log.slice(-size);
};
api.listChatWindow = async () => [];
api.listChatReads = async () => [];
api.markChatRead = async () => undefined as never;
api.subscribeEvents = () => () => {};

const peer: Member = {
  id: PEER,
  name: "Cara",
  role: "assistant",
  status: "online",
  lifecycle: "online",
  model: "opus",
  effort: "medium",
  kind: "staff",
  desiredMachineId: "",
  machine: null,
  account: null,
  contextPct: null,
  estimatedCost: null,
  bankedCost: null,
  tmuxSession: "",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
};

export function ChatInterAgentScrollbackStory({
  widthPx = 1280,
}: { widthPx?: number } = {}) {
  return (
    <I18nProvider>
      <div
        style={{
          width: widthPx,
          height: 720,
          display: "grid",
          gridTemplateColumns: "1fr",
          background: "var(--color-main-bg)",
        }}
      >
        <ChatArea key={peer.id} member={peer} members={[peer]} workers={[]} />
      </div>
    </I18nProvider>
  );
}
