import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { ChatReplyCard } from "../../src/components/ChatReplyCard";
import { __injectMockReplyCard, __resetMock } from "../../src/api/mock";
import type { ReplyCard } from "../../src/api/adapter";

const CARD: ReplyCard = {
  id: "rc-chat-timestamp",
  from: "mira",
  kind: "decision",
  summary: "要現在把這份設定同步到 Jira 嗎？",
  body: "同步之前先確認一次，這段內容讓卡片保持接近真實回覆卡的高度。",
  options: ["核可，同步上去", "先不要"],
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
            fallbackSummary={CARD.summary}
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
                <div className="chat__body">
                  <div className="chat__messages" data-testid="chat-messages">
                    <ChatCardMessage />
                  </div>
                </div>
              </div>
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
