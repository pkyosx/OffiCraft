// CT story (T-48 fix12): the REAL <ChatArea> + the REAL useChat, entered either
// ORDINARILY or AT AN ANCHOR, against an api seam whose pages can be held in the
// air — so the loading state has a duration to be observed in.
//
// 🔴 WHY BOTH ENTRANCES LIVE IN ONE STORY. The claim under guard is not 「a
// spinner appears」 but 「there is ONE state behind it」 (owner c-d24ebd7f8d78:
// 「照理說應該只有改一個地方吧?就會都有作用?」). A story per entrance would let
// each be satisfied by its own flag, which is the shape being refused. One
// story, one prop, two entrances.
import { I18nProvider } from "../../src/i18n";
import { ChatArea } from "../../src/components/ChatArea";
import { api } from "../../src/api";
import type { ChatMessage } from "../../src/api/adapter";
import type { Member } from "../../src/types";
import { LOADING_TOTAL, LOADING_ANCHOR } from "./chatThreadLoadingFixtures";
import "../../src/components/office.css";

const OWNER = "owner";
const PEER = "m-cccccccccccc";

const log: ChatMessage[] = [];
for (let i = 0; i < LOADING_TOTAL; i += 1) {
  log.push({
    id: `L${i}`,
    from: i % 2 === 0 ? PEER : OWNER,
    to: i % 2 === 0 ? OWNER : PEER,
    body: `第 ${i} 則訊息 —— 一句普通長度的聊天內容,好讓每一列都有真的高度。`,
    ts: 1000 + i,
    attachments: [],
    replyCardId: null,
  });
}

// Every page waits this long. It is a KNOB and not a constant of the product:
// the loading state's whole point is that it covers a wait, so a guard needs a
// wait long enough to sample. 120ms per page × 3 walk pages puts the anchor
// entrance well past the show-after delay without making the run slow.
let latency = 120;

api.listChat = async (
  _withId: string,
  limit?: number,
  cursor?: { beforeTs: number; beforeId: string },
) => {
  await new Promise((r) => setTimeout(r, latency));
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
api.listChatWindow = async (
  _withId: string,
  anchor: { startId?: string; endId?: string },
  limit: number,
) => {
  await new Promise((r) => setTimeout(r, latency));
  const at = log.findIndex((m) => m.id === (anchor.endId ?? anchor.startId));
  if (at < 0) return [];
  return anchor.endId
    ? log.slice(Math.max(0, at - limit + 1), at + 1)
    : log.slice(at, at + limit);
};
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

export function ChatThreadLoadingStory({
  entrance,
  widthPx,
  latencyMs,
}: {
  /** 「進聊天室」 or 「點選原訊息」 — the two doors owner c-de666642e77b named. */
  entrance: "plain" | "anchor";
  widthPx: number;
  latencyMs?: number;
}) {
  latency = latencyMs ?? 120;
  return (
    <I18nProvider>
      {/* A GRID column, so `.chat` (which declares no width and is not a
        * flex-grow item) actually stretches to the width under test. */}
      <div
        style={{
          width: widthPx,
          height: 720,
          display: "grid",
          gridTemplateColumns: "1fr",
          background: "var(--color-main-bg)",
        }}
      >
        <ChatArea
          key={peer.id}
          member={peer}
          members={[peer]}
          workers={[]}
          jumpToMsgId={entrance === "anchor" ? LOADING_ANCHOR : undefined}
        />
      </div>
    </I18nProvider>
  );
}
