// CT stories: the @mention picker in the two composers that sit in the middle
// of a scrolling page — the task card's message box and the reply card's
// answer box — each inside the app's own ancestor chain (.app > .app__main >
// page > section > list), so the list is measured against the padding the
// shipped page gives it.
import type { ReactNode } from "react";
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { ReplyCardWaitingBody } from "../../src/components/ReplyCardBody";
import { MentionRosterContext, useMentionRoster } from "../../src/hooks/useMentionComposer";
import type { OutsourceWorkerView, ReplyCard } from "../../src/api/adapter";
import type { Member } from "../../src/types";
import { mkTask, NOOP } from "./taskFixtures";
import "../../src/components/chrome.css";
import "../../src/components/office.css";
import "../../src/components/replies.css";
import "../../src/components/tasks.css";

function mkMember(id: string, name: string, role: string, roleName: string): Member {
  return {
    id,
    name,
    role,
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

const MEMBERS = [
  mkMember("mira", "Mira", "assistant", "助理"),
  mkMember("m-eva", "Family Money Eva", "custom", "財務與帳務整理"),
];
const WORKERS: OutsourceWorkerView[] = [
  {
    id: "ow-7",
    codename: "O-7",
    model: "",
    effort: "",
    status: "active",
    taskId: "t-492",
    taskNo: "T-492",
    // Wider than a 1280 desktop row too: these boxes span the whole page.
    taskTitle:
      "任務卡與請示卡的回覆框裡的 @ 提及清單在手機與桌面寬度下都要完整顯示、可點選，而且長長的任務標題必須被截斷而不是把整張卡片或整個頁面撐寬，就算標題長到超過一整個桌面寬度也一樣",
  },
];

function Page({ children }: { children: ReactNode }) {
  return (
    <I18nProvider>
      <div className="app" style={{ height: 844 }}>
        <main className="app__main">{children}</main>
      </div>
    </I18nProvider>
  );
}

const TASK = mkTask({
  id: "t-mention",
  taskNo: "t-mention",
  title: "在任務卡上點名同事",
  status: "in_progress",
});

export function TaskCardMentionStory() {
  return (
    <Page>
      <div className="tasks">
        <section className="tasks__section">
          <div className="tasks__list">
            <TaskCard
              task={TASK}
              allTasks={[TASK]}
              members={MEMBERS}
              workers={WORKERS}
              nowTs={3000}
              onTerminate={NOOP as never}
              onMarkDuplicate={NOOP as never}
              onSetPriority={NOOP as never}
              onSendMessage={NOOP as never}
              onReassign={NOOP as never}
              onHydrate={(async () => TASK) as never}
            />
          </div>
        </section>
      </div>
    </Page>
  );
}

const CARD: ReplyCard = {
  id: "rc-mention",
  from: "mira",
  kind: "decision",
  summary: "這件事要交給誰？",
  body: "",
  options: [{ text: "我自己來", aiPick: false }],
  selectMode: "single",
  status: "waiting",
  attachments: [],
  createdTs: Date.now() / 1000 - 600,
  answeredTs: null,
  chatMessageId: "msg-mention",
  answer: null,
  task: null,
};

function Roster({ children }: { children: ReactNode }) {
  const roster = useMentionRoster(MEMBERS, WORKERS, { backTo: "replies" });
  return <MentionRosterContext.Provider value={roster}>{children}</MentionRosterContext.Provider>;
}

export function ReplyCardMentionStory() {
  return (
    <Page>
      <div className="replies">
        <section className="replies__section">
          <div className="replies__list">
            <article className="reply-card">
              <div className="reply-card__summary">{CARD.summary}</div>
              <Roster>
                <ReplyCardWaitingBody card={CARD} onAnswer={async () => {}} />
              </Roster>
            </article>
          </div>
        </section>
      </div>
    </Page>
  );
}
