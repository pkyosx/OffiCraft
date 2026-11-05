// The 請示 page itself, in the app's content column (`.app > .app__main` with
// chrome.css — the real width cap and gutters), over mock cards that carry the
// longest things a card head and its task row hold: an outsource asker, a high
// priority overdue task with a long title, and a timed due date.
import { I18nProvider } from "../../src/i18n";
import { RepliesPage } from "../../src/components/RepliesPage";
import { ReplyCardsProvider } from "../../src/hooks/useReplyCards";
import { __injectMockReplyCard, __injectMockMember } from "../../src/api/mock";
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
function seed() {
  if (seeded) return;
  seeded = true;
  __injectMockMember({ id: "ow-desk", kind: "outsource", name: "O-161" });
  __injectMockReplyCard(
    card({
      id: "rc-desk-1",
      from: "ow-desk",
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
      createdTs: NOW - 3 * 3600,
      task: {
        id: "T-9",
        typeKey: "",
        title: "短標題",
        priority: "frozen",
        dueTs: NOW + 3 * 3600 + 17 * 60,
      },
    })
  );
  __injectMockReplyCard(card({ id: "rc-desk-3", createdTs: NOW - 4 * 3600, task: null }));
}

export function ReplyDeskStory() {
  seed();
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
