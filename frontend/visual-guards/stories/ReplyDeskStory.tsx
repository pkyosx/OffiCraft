// The 請示 page itself, in the app's content column (`.app > .app__main` with
// chrome.css — the real width cap and gutters), over mock cards that carry the
// longest things a card head and its task row hold: an outsource asker, a high
// priority overdue task with a long title, a timed due date, waited stamps from
// the shortest (5m) to the longest (12d 23h) shape, and a snoozed card whose
// stamp is a full return date. `questions` adds the newest waiting card, whose
// question has a body with a mention and a link and runs taller than a phone's
// room above the selection bar, a waiting secret card, and an answered card in
// 近期已處理.
import { I18nProvider } from "../../src/i18n";
import { RepliesPage } from "../../src/components/RepliesPage";
import { ReplyCardsProvider } from "../../src/hooks/useReplyCards";
import { __injectMockReplyCard, __injectMockMember } from "../../src/api/mock";
import { api } from "../../src/api";
import { QUESTION_BODY, QUESTION_SUMMARY } from "./replyDeskQuestion";
import type { ReplyCard } from "../../src/api/adapter";
import "../../src/components/chrome.css";

const NOW = Date.now() / 1000;

function card(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要現在把這批更新同步到 Jira 嗎？",
    body: "",
    options: [{ text: "好", aiPick: true }],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: NOW - 25 * 60,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

let seeded = false;
function seed(questions: boolean) {
  if (seeded) return;
  seeded = true;
  if (questions) {
    __injectMockReplyCard(
      card({
        id: "rc-desk-q",
        createdTs: NOW - 60,
        summary: QUESTION_SUMMARY,
        body: QUESTION_BODY,
        task: { id: "T-77", typeKey: "", title: "部署", priority: "high" },
      })
    );
    __injectMockReplyCard(
      card({
        id: "rc-desk-secret",
        kind: "secret",
        createdTs: NOW - 30 * 86_400,
        summary: "部署金鑰",
        body: "放到 deploy 主機",
        options: [],
        task: null,
        secret: { state: "waiting", providedTs: null, retrievedTs: null, expiresTs: null },
      })
    );
    __injectMockReplyCard(
      card({
        id: "rc-desk-done",
        createdTs: NOW - 3600,
        summary: "週報要附上監控截圖嗎？",
        body: "附上的話我會放在第二節。",
        status: "answered",
        answeredTs: NOW - 600,
        answer: { optionIdxs: [0], text: "", attachments: [] },
        task: null,
      })
    );
  }
  __injectMockMember({ id: "ow-desk", kind: "outsource", name: "O-161" });
  __injectMockReplyCard(
    card({
      id: "rc-desk-1",
      from: "ow-desk",
      createdTs: NOW - 5 * 60 - 30,
      summary: "這個版本的設定頁要不要先上線，還是等下一輪一起？",
      task: {
        id: "T-58412",
        typeKey: "",
        title: "請示頁改版：任務列、稍後再看、優先序與期限篩選，以及手機版的排版修正",
        priority: "high",
        dueTs: NOW - 2 * 86_400,
      },
    })
  );
  __injectMockReplyCard(
    card({
      id: "rc-desk-2",
      createdTs: NOW - (2 * 3600 + 57 * 60 + 30),
      task: {
        id: "T-9",
        typeKey: "",
        title: "短標題",
        priority: "frozen",
        dueTs: NOW + 3 * 3600 + 17 * 60,
      },
    })
  );
  __injectMockReplyCard(
    card({ id: "rc-desk-3", createdTs: NOW - (12 * 86_400 + 23 * 3600 + 30 * 60), task: null })
  );
  __injectMockReplyCard(
    card({
      id: "rc-desk-snoozed",
      createdTs: NOW - 86_400,
      snoozedUntilTs: NOW + 40 * 86_400,
      task: null,
    })
  );
}

const failRead = () => Promise.reject(new Error("read failed"));
const hangRead = () => new Promise<never>(() => {});

export function ReplyDeskStory({
  questions = false,
  cardReadFails = false,
  cardReadHangs = false,
}: {
  questions?: boolean;
  /** Every one-card read fails: opening a card and 複製 alike. */
  cardReadFails?: boolean;
  /** Every one-card read stays in flight. */
  cardReadHangs?: boolean;
}) {
  seed(questions);
  if (cardReadFails) api.getReplyCard = failRead;
  if (cardReadHangs) api.getReplyCard = hangRead;
  // Another window's 稍後再看, for a guard that needs a card to change panes
  // under the page.
  (window as unknown as { __ocSnooze?: (id: string, untilTs: number) => void }).__ocSnooze = (
    id,
    untilTs,
  ) => void api.snoozeReplyCard(id, untilTs);
  return (
    <I18nProvider>
      <div className="app" style={{ width: "100vw", maxWidth: "100vw" }}>
        <main className="app__main">
          <ReplyCardsProvider>
            <RepliesPage />
          </ReplyCardsProvider>
        </main>
      </div>
    </I18nProvider>
  );
}
