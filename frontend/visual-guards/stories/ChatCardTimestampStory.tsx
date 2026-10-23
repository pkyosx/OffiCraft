import { useEffect, useRef, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { ChatReplyCard } from "../../src/components/ChatReplyCard";
import {
  ChatSelectionReply,
  ReplyCardQuoteProvider,
} from "../../src/components/ChatSelectionReply";
import { __injectMockReplyCard, __resetMock } from "../../src/api/mock";
import type { ReplyCard } from "../../src/api/adapter";

const SUMMARY_LINE = "要現在把這份設定同步到 Jira 嗎？";

// A summary is agent-written markdown, and a fenced block in it is common; this
// line has no break opportunity, so it is wider than the card at every width.
const UNBREAKABLE_CODE =
  "officraft-sync--target=jira--project=OPS--fields=summary,description,labels,assignee,priority,components,fixVersions,duedate,reporter,watchers,attachments,subtasks,issuelinks,worklog--transition=in-review--notify=reporter,watchers--dry-run=false";

const CARD: ReplyCard = {
  id: "rc-chat-timestamp",
  from: "mira",
  kind: "decision",
  summary: `${SUMMARY_LINE}\n\n\`\`\`\n${UNBREAKABLE_CODE}\n\`\`\``,
  body: "同步之前先確認一次，這段內容讓卡片保持接近真實回覆卡的高度。",
  options: [{ text: "核可，同步上去", aiPick: true }, { text: "先不要", aiPick: false }],
  selectMode: "single",
  status: "waiting",
  attachments: [],
  createdTs: 1786000000,
  answeredTs: null,
  chatMessageId: "msg-chat-timestamp",
  answer: null,
};

function ChatCardMessage() {
  return (
    <div className="chat__msg chat__msg--card" data-testid="chat-card-row">
      <div className="chat__msg-meta">
        <span className="chat__msg-name">Mira</span>
      </div>
      <div className="chat__msg-line">
        <div className="chat__msg-content">
          <ChatReplyCard
            replyCardId={CARD.id}
            fallbackSummary={SUMMARY_LINE}
            initialStatus="waiting"
          />
        </div>
        <div className="chat__msg-sidemeta">
          <span className="chat__msg-time" data-testid="chat-card-time">
            01:11 AM
          </span>
        </div>
      </div>
    </div>
  );
}

export function ChatCardTimestampStory() {
  const [seeded, setSeeded] = useState(false);
  const messagesRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    __resetMock();
    __injectMockReplyCard(CARD);
    setSeeded(true);
  }, []);

  if (!seeded) return null;

  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main" data-testid="app-main">
          <div className="office" style={{ height: "100%" }}>
            <aside className="office__members" aria-hidden="true" />
            <section className="office__chat">
              <div className="chat">
                <ReplyCardQuoteProvider>
                  <ChatSelectionReply root={messagesRef} />
                  <div className="chat__body">
                    <div
                      className="chat__messages"
                      data-testid="chat-messages"
                      ref={messagesRef}
                    >
                      <ChatCardMessage />
                    </div>
                  </div>
                </ReplyCardQuoteProvider>
              </div>
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
