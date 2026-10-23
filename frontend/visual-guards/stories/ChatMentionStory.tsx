// CT story: the REAL <ChatArea> inside the app's own ancestor chain
// (.app > .app__main > .office > .office__chat), so the @mention picker is
// measured against the padding and grid the shipped page gives it — a bare
// mount has more room than the real pane and would pass a list that overflows.
import { I18nProvider } from "../../src/i18n";
import { ChatArea } from "../../src/components/ChatArea";
import { api } from "../../src/api";
import type { ChatMessage, OutsourceWorkerView } from "../../src/api/adapter";
import type { Member } from "../../src/types";
import "../../src/components/chrome.css";
import "../../src/components/office.css";

const PEER = "m-cccccccccccc";

const log: ChatMessage[] = [0, 1, 2, 3].map((i) => ({
  id: `L${i}`,
  from: i % 2 === 0 ? PEER : "owner",
  to: i % 2 === 0 ? "owner" : PEER,
  body: `第 ${i} 則訊息`,
  ts: 1000 + i,
  attachments: [],
  replyCardId: null,
}));

api.listChat = async () => log;
api.listChatWindow = async () => [];
api.listChatReads = async () => [];
api.markChatRead = async () => undefined as never;
api.subscribeEvents = () => () => {};

function mkMember(id: string, name: string, roleName: string): Member {
  return {
    id,
    name,
    role: "assistant",
    roleName,
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

const peer = mkMember(PEER, "Cara", "助理");
const members = [peer, mkMember("m-eva", "Family Money Eva", "財務與帳務整理")];
const workers: OutsourceWorkerView[] = [
  {
    id: "ow-7",
    codename: "O-7",
    model: "",
    effort: "",
    status: "active",
    taskId: "t-492",
    taskNo: "T-492",
    taskTitle:
      "聊天輸入框的 @ 提及清單在手機寬度下要完整顯示、可點選，而且長長的任務標題必須被截斷而不是把整個頁面撐寬",
  },
];

export function ChatMentionStory({ withRoster }: { withRoster: boolean }) {
  return (
    <I18nProvider>
      <div className="app" style={{ height: 844 }}>
        <main className="app__main">
          <div className="office">
            {withRoster && <aside className="office__members" />}
            <section className="office__chat" data-testid="chat-pane">
              <ChatArea
                key={peer.id}
                member={peer}
                members={members}
                workers={workers}
                rosterSettled
              />
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
