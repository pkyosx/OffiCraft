// CT story (T-48 / fix11): MEASUREMENT PROBE, not a guard.
//
// It mounts the REAL <ChatArea> + the REAL useChat against a mocked api seam
// that hands back N messages, so the question 「把 N 則訊息載進聊天畫面要多久、
// 吃多少記憶體、還能不能用」 can be answered with numbers instead of with a
// remembered figure nobody can point at.
//
// 🔴 NO PRODUCT CODE IS TOUCHED FOR THIS. Everything that makes the run
// measurable lives here or in the spec.
//
// The width knob is a GRID column, and that is deliberate: `.chat` declares no
// width and is not a flex-grow item, so a plain flex wrapper leaves it at its
// own max-content (measured by the forward-walk story: 273px at wrapper widths
// 321…900 alike). A grid item stretches to its column by default, so this is
// the one wrapper shape that actually pins the chat column at the width being
// asked about.
import { I18nProvider } from "../../src/i18n";
import { ChatArea } from "../../src/components/ChatArea";
import { api } from "../../src/api";
import type { ChatMessage } from "../../src/api/adapter";
import type { Member } from "../../src/types";
import "../../src/components/office.css";

const OWNER = "owner";
const PEER = "m-bbbbbbbbbbbb";

export type RenderCostVariant = "plain" | "cards" | "images";

const BODY =
  "第 %d 則訊息 —— 一句普通長度的聊天內容,好讓每一列的高度都是真的,不會全部擠在同一行。";

function buildLog(n: number, variant: RenderCostVariant): ChatMessage[] {
  const out: ChatMessage[] = [];
  for (let i = 0; i < n; i += 1) {
    const m: ChatMessage = {
      id: `r${i}`,
      from: i % 2 === 0 ? PEER : OWNER,
      to: i % 2 === 0 ? OWNER : PEER,
      body: BODY.replace("%d", String(i)),
      ts: 1000 + i,
      attachments: [],
      replyCardId: null,
    };
    // 每 5 則放一個「已回覆」的收合請示卡 —— 這一包剛做的就是「已回覆卡預設收合、
    // 不預抓」,所以它應該便宜。
    if (variant === "cards" && i % 5 === 0) {
      m.replyCardId = `card-${i}`;
      m.replyCardStatus = "answered";
      m.card = {
        options: [
          { text: "好", aiPick: false },
          { text: "不要", aiPick: true },
        ],
        answerOptionIdxs: [0],
        answerText: "",
        answeredTs: 1000 + i,
        answeredAtDisplay: "2026-09-04 12:00:00 +08:00",
      };
    }
    // 每 5 則放一張圖 —— office.css `.chat__msg-image` 給的是固定 220px 的框。
    if (variant === "images" && i % 5 === 0) {
      m.attachments = [
        {
          id: `att-${i}`,
          url: `/api/chat/attachments/att-${i}`,
          filename: `shot-${i}.png`,
          mime: "image/png",
          isImage: true,
        },
      ];
    }
    out.push(m);
  }
  return out;
}

let log: ChatMessage[] = [];
// 一次 loadOlder 要回幾列 —— 量「這條線已經有 N 列時,再 commit 一頁的成本」。
// mock 是我們自己的,所以它可以回得比 useChat 要的多。
let olderPageSize = 100;

api.listChat = async (
  _withId: string,
  _limit?: number,
  cursor?: { beforeTs: number; beforeId: string },
) => {
  if (cursor) {
    // 增量那一格:再給一頁「更舊的」,列數就是 olderPageSize。
    const older: ChatMessage[] = [];
    for (let i = 1; i <= olderPageSize; i += 1) {
      older.push({
        id: `o${cursor.beforeId}-${i}`,
        from: PEER,
        to: OWNER,
        body: BODY.replace("%d", `older ${i}`),
        ts: cursor.beforeTs - olderPageSize + i - 1,
        attachments: [],
        replyCardId: null,
      });
    }
    return older;
  }
  return log;
};
// 每一通 window 請求的延遲 —— 「使用者會盯著載入畫面幾秒」要把往返算進去。
let windowLatency = 0;
api.listChatWindow = async (
  _withId: string,
  anchor: { startId?: string; endId?: string },
  limit: number,
) => {
  if (windowLatency > 0) {
    await new Promise((r) => setTimeout(r, windowLatency));
  }
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

declare global {
  interface Window {
    __rc?: { t0: number; n: number };
  }
}

export function ChatRenderCostStory({
  n,
  widthPx,
  variant = "plain",
  anchorIndex,
  windowLatencyMs = 0,
}: {
  n: number;
  widthPx: number;
  variant?: RenderCostVariant;
  /** 有值 ⇒ 走「點原訊息」那個入口,錨點是第 anchorIndex 則。 */
  anchorIndex?: number;
  windowLatencyMs?: number;
}) {
  log = buildLog(n, variant);
  olderPageSize = 100;
  windowLatency = windowLatencyMs;
  if (typeof window !== "undefined" && !window.__rc) {
    // 這是第一次 render 的時刻:此時一列都還沒進 DOM(訊息在 api 的 microtask
    // 之後才回來),所以「N 列全部落地」減掉它,量到的就是這 N 列的成本。
    window.__rc = { t0: performance.now(), n };
  }
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
        <ChatArea
          key={peer.id}
          member={peer}
          members={[peer]}
          workers={[]}
          jumpToMsgId={anchorIndex === undefined ? undefined : `r${anchorIndex}`}
        />
      </div>
    </I18nProvider>
  );
}
