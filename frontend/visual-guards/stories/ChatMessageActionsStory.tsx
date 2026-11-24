// CT story: the REAL <ChatArea> in the app's own ancestor chain, for what only
// the live component does — the toolbar's hover reveal over real bubbles, the
// long-press menu, 選取文字 and the reply-card row. The thread mixes the
// shapes whose widths differ: a long incoming answer with a link, a one-word
// incoming line, your own reply with a quote (still deletable), your own
// one-word line, your own line with a mention and a link, a waiting reply card
// on a task with a question attachment, and an answered one on a task.
import { useEffect } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import type { ThemeBundle } from "../../src/lib/themeBundle";
import thanksgiving from "../../../themes/thanksgiving-2026-day.theme.json";
import christmas from "../../../themes/christmas-2026-day.theme.json";
import { ChatArea } from "../../src/components/ChatArea";
import { api } from "../../src/api";
import type { ChatMessage, ReplyCard } from "../../src/api/adapter";
import type { Member } from "../../src/types";
import "../../src/components/chrome.css";
import "../../src/components/office.css";

const PEER = "m-cccccccccccc";
const now = Math.floor(Date.now() / 1000);

const LONG =
  "今天的部署整理如下，請你過目：\n\n- 前端：聊天訊息加上「複製訊息」，試用站已經換版\n- 後端：[migration 0142](https://example.com/m0142) 會改 chat_messages 的索引，上線前要先備份\n- 文件：使用說明第 7 節補上複製與回覆的操作\n\n有問題再跟我說。";

// The same answer with a list longer than a phone's room above the selection
// bar, still ending on its one-line closing paragraph.
const TALL_LONG = LONG.replace(
  "\n\n有問題再跟我說。",
  Array.from({ length: 14 }, (_, i) => `\n- 第 ${i + 1} 項檢查：確認備份、索引與回歸測試都完成`).join("") +
    "\n\n有問題再跟我說。",
);

const msg = (over: Partial<ChatMessage> & { id: string }): ChatMessage => ({
  from: PEER,
  to: "owner",
  body: "",
  ts: now - 300,
  attachments: [],
  replyCardId: null,
  replyCardStatus: null,
  replyTo: null,
  replyToChat: null,
  ...over,
});

const log: ChatMessage[] = [
  msg({ id: "m-long", body: LONG, ts: now - 600 }),
  msg({ id: "m-short", body: "好", ts: now - 500 }),
  msg({
    id: "m-mine",
    from: "owner",
    to: PEER,
    body: "第二點的 migration 先等我確認備份再跑。",
    ts: now - 400,
    replyTo: "m-long",
    replyToChat: {
      id: "m-long",
      from: PEER,
      fromName: "",
      to: "owner",
      toName: "",
      content: "今天的部署整理如下，請你過目：前端：聊天訊息加上「複製訊息」…",
    },
  }),
  msg({ id: "m-mine-short", from: "owner", to: PEER, body: "收到", ts: now - 300 }),
  msg({
    id: "m-mention",
    from: "owner",
    to: PEER,
    body: `請 [@Mira](/#office/member/${PEER}) 先看 [這份計畫](https://example.com/plan) 再回我`,
    ts: now - 250,
  }),
  msg({
    id: "m-card",
    body: "要不要先在試用站備份？",
    ts: now - 200,
    replyCardId: "rc-1",
    replyCardStatus: "waiting",
  }),
  msg({
    id: "m-card-answered",
    body: "週五要不要上線？",
    ts: now - 100,
    replyCardId: "rc-2",
    replyCardStatus: "answered",
  }),
];

const card = {
  id: "rc-1",
  from: PEER,
  to: "owner",
  status: "waiting",
  kind: "decision",
  selectMode: "single",
  summary: "要不要先在試用站備份？",
  body: "migration 0142 會改索引。",
  options: [
    { text: "先備份", aiPick: true },
    { text: "直接跑", aiPick: false },
  ],
  attachments: [
    {
      id: "att-q",
      url: "data:application/pdf;base64,",
      filename: "備份計畫.pdf",
      mime: "application/pdf",
      isImage: false,
    },
  ],
  answer: null,
  task: { id: "t-0142", typeKey: "dev", title: "chat_messages 索引調整" },
  createdTs: now - 200,
  chatMessageId: "m-card",
} as unknown as ReplyCard;

const tallLog = log.map((m) => (m.id === "m-long" ? { ...m, body: TALL_LONG } : m));
api.listChatWindow = async () => [];
api.listChatReads = async () => [];
api.markChatRead = async () => undefined as never;
api.subscribeEvents = () => () => {};
const answered = {
  ...card,
  id: "rc-2",
  status: "answered",
  summary: "週五要不要上線？",
  body: "上線前要先跑完回歸測試。",
  answer: { optionIdxs: [0], text: "", attachments: [] },
  attachments: [],
  task: { id: "t-0150", typeKey: "dev", title: "週五上線" },
  answeredTs: now - 50,
  chatMessageId: "m-card-answered",
} as unknown as ReplyCard;

const readCard = async (id: string) => (id === "rc-2" ? answered : card);
const readCardWithoutTask = async (id: string) => ({ ...(await readCard(id)), task: null });
const failCard = async (): Promise<ReplyCard> => {
  throw new Error("offline");
};

function mkMember(id: string, name: string): Member {
  return {
    id,
    name,
    role: "assistant",
    roleName: "助理",
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
    terminalAttachCommand: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

const peer = mkMember(PEER, "Mira");

// A seasonal bundle under a custom id (the seasonal ids are reserved and CT
// runs with no seasonal schedule), worn through the provider like any saved
// theme.
const SEASONS: Record<string, ThemeBundle> = {
  thanksgiving: { ...(thanksgiving as unknown as ThemeBundle), id: "thanksgiving-preview" },
  christmas: { ...(christmas as unknown as ThemeBundle), id: "christmas-preview" },
};

function SeasonDriver({ season }: { season: string }) {
  const { theme, setTheme, saveTheme } = useI18n();
  const bundle = SEASONS[season];
  useEffect(() => {
    setTheme(bundle.id);
  }, [setTheme, bundle.id]);
  useEffect(() => {
    if (theme === bundle.id) void saveTheme(bundle);
  }, [theme, saveTheme, bundle]);
  return null;
}

export function ChatMessageActionsStory({
  withRoster,
  cardReadFails = false,
  cardsWithoutTask = false,
  tallMessage = false,
  season,
}: {
  withRoster: boolean;
  /** The card read behind 複製 on the reply-card row fails. */
  cardReadFails?: boolean;
  /** The cards ask about no task, so the card's title leads it. */
  cardsWithoutTask?: boolean;
  /** The long answer runs taller than a phone's room above the selection bar. */
  tallMessage?: boolean;
  season?: "thanksgiving" | "christmas";
}) {
  api.listChat = async () => (tallMessage ? tallLog : log);
  api.getReplyCard = cardReadFails ? failCard : cardsWithoutTask ? readCardWithoutTask : readCard;
  return (
    <I18nProvider>
      {season && <SeasonDriver season={season} />}
      <div className="app" style={{ height: "100vh" }}>
        <main className="app__main">
          <div className="office">
            {withRoster && <aside className="office__members" />}
            <section className="office__chat" data-testid="chat-pane">
              <ChatArea key={peer.id} member={peer} members={[peer]} rosterSettled />
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
