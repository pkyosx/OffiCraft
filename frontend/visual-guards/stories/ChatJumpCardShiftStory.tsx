// CT story (T-48): the REAL <ChatArea> + the REAL useChat + the REAL
// <ChatReplyCard>, driven through a JUMP onto a message that has a WAITING
// 請示卡 sitting above it.
//
// 🔴 WHY THE WHOLE COMPONENT AND NOT A HAND-WRITTEN SHELL. What is being
// measured is a SHIFT — the jump target's y before and after the window in
// which the card's own fetch would have landed — and what decides whether there
// is one is that `ChatReplyCard` renders COLLAPSED and therefore never asks
// (T-48, owner 2026-09-04). A shell would have to fake that, i.e. fake the
// answer. jsdom cannot measure it at all (no layout engine, every rect is 0),
// which is why this is a CT guard and not a vitest one.

//
// The api seam is patched in place (the house pattern — see
// ScheduledMessagesClampStory / SoftwareUpdateStory): `api` IS `mockApi` under
// CT's default VITE_USE_MOCK, so assigning its methods swaps the backend without
// touching a line of product code.
import { I18nProvider } from "../../src/i18n";
import { ChatArea } from "../../src/components/ChatArea";
import { api } from "../../src/api";
import type { ChatMessage, ReplyCard } from "../../src/api/adapter";
import type { Member } from "../../src/types";
import {
  OWNER,
  PEER,
  TARGET_ID,
  CARD_ROW_ID,
  CARD_ID,
  CARD_LATENCY_MS,
} from "./chatJumpCardShiftFixtures";
import "../../src/components/office.css";

const CARD: ReplyCard = {
  id: CARD_ID,
  from: PEER,
  kind: "decision",
  summary: "要用哪一個方案把這批貨走掉?",
  body: "三家都回了報價,價差不大但時效差很多。",
  options: [
    { text: "走 A 家,最快但最貴", aiPick: true },
    { text: "走 B 家,便宜兩天到", aiPick: false },
  ],
  selectMode: "single",
  status: "waiting",
  createdTs: 1,
  attachments: [],
  answeredTs: null,
  expiredTs: null,
  chatMessageId: CARD_ROW_ID,
  answer: null,
  task: null,
};

const log: ChatMessage[] = [];
for (let i = 0; i < 80; i += 1) {
  log.push({
    id: `a${i}`,
    from: PEER,
    to: OWNER,
    body: `第 ${i} 則訊息 —— 一句普通長度的聊天內容,好讓每一列的高度都是真的。`,
    ts: 100 + i,
    attachments: [],
    replyCardId: null,
  });
}
log.push({
  id: CARD_ROW_ID,
  from: PEER,
  to: OWNER,
  body: CARD.summary,
  ts: 139.5,
  attachments: [],
  replyCardId: CARD_ID,
  replyCardStatus: "waiting",
});
log.sort((a, b) => a.ts - b.ts);

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

api.listChat = async (
  withId: string,
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
api.listChatWindow = async (
  _withId: string,
  anchor: { startId?: string; endId?: string },
  limit: number,
) => {
  const at = log.findIndex((m) => m.id === (anchor.endId ?? anchor.startId));
  if (at < 0) return [];
  return anchor.endId
    ? log.slice(Math.max(0, at - limit + 1), at + 1)
    : log.slice(at, at + limit);
};
api.getReplyCard = async (id: string): Promise<ReplyCard> => {
  await sleep(CARD_LATENCY_MS);
  return { ...CARD, id };
};
api.listChatReads = async () => [];
api.markChatRead = async () => undefined as never;
api.subscribeEvents = () => () => {};

const peer: Member = {
  id: PEER,
  name: "Alice",
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

export function ChatJumpCardShiftStory() {
  return (
    <I18nProvider>
      {/* `.chat` is a height:100% flex column and the CT mount point has no
        * height of its own, so the scroller would be unbounded and NOTHING
        * would ever be clipped — i.e. a shift would have nowhere to show. */}
      <div style={{ height: 720, display: "flex", background: "var(--color-main-bg)" }}>
        <ChatArea
          key={peer.id}
          member={peer}
          members={[peer]}
          workers={[]}
          jumpToMsgId={TARGET_ID}
        />
      </div>
    </I18nProvider>
  );
}
