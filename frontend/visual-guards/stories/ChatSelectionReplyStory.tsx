// CT story: the REAL <ChatSelectionReply> over two incoming bubbles in the
// production thread skeleton, plus the composer banner it fills. The banner is
// markup (as in ChatReplyToStory) driven by the story's own state, so no
// function prop crosses the mount bridge.
import { useRef, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { ChatSelectionReply } from "../../src/components/ChatSelectionReply";

const LONG_BODY =
  "這是一段很長的原文，長到在手機寬度下會折成好幾行，選取它最右邊的幾個字時浮動按鈕必須夾在視窗裡面而不是跑出右邊界";

function Row({ id, body }: { id: string; body: string }) {
  return (
    <div className="chat__msg" data-msg-id={id} data-testid={`sel-row-${id}`}>
      <div className="chat__msg-meta">
        <span className="chat__msg-name">Mira</span>
      </div>
      <div className="chat__msg-line">
        <div className="chat__msg-content">
          <div className="chat__msg-bubble chat__msg-bubble--expandable chat__msg-bubble--acts2">
            <div className="chat__msg-actions" />
            <div className="chat__msg-text doc-md">
              <p>{body}</p>
            </div>
          </div>
        </div>
        <div className="chat__msg-sidemeta">
          <span className="chat__msg-time">10:02</span>
        </div>
      </div>
    </div>
  );
}

export function ChatSelectionReplyStory() {
  const messagesRef = useRef<HTMLDivElement>(null);
  const [picked, setPicked] = useState<{ id: string; excerpt: string } | null>(
    null,
  );
  return (
    <I18nProvider>
      <div className="chat">
        <ChatSelectionReply
          root={messagesRef}
          onReply={(id, excerpt) => setPicked({ id, excerpt })}
        />
        <div className="chat__body">
          <div className="chat__messages" ref={messagesRef}>
            <Row id="c-1" body={LONG_BODY} />
            <Row id="c-2" body="第二則訊息" />
          </div>
        </div>
        <footer className="chat__composer">
          {picked && (
            <div
              className="chat__reply-banner"
              data-testid="chat-reply-banner"
              data-reply-to={picked.id}
            >
              <span className="chat__reply-banner__text">
                <span className="chat__reply-banner__who">正在回覆 Mira → 韓立</span>
                <span className="chat__reply-banner__body">{picked.excerpt}</span>
              </span>
              <button type="button" className="chat__reply-banner__x" aria-label="取消回覆" />
            </div>
          )}
          <div className="chat__composer-row">
            <textarea className="chat__input" rows={1} defaultValue="" />
          </div>
        </footer>
      </div>
    </I18nProvider>
  );
}
