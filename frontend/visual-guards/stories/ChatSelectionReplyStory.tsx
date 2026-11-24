// CT story: the REAL <ChatSelectionReply> over two incoming bubbles and one own
// bubble in the production thread skeleton, plus the composer banner it fills.
// The banner is markup (as in ChatReplyToStory) driven by the story's own
// state, so no function prop crosses the mount bridge.
import { useRef, useState } from "react";
import type { ReactNode } from "react";
import { I18nProvider } from "../../src/i18n";
import { ChatSelectionReply } from "../../src/components/ChatSelectionReply";

const LONG_BODY =
  "這是一段很長的原文，長到在手機寬度下會折成好幾行，用來確認選取不會把版面撐開";

// On a touch screen a message selects only after its 選取文字; `selecting`
// renders every row in that state.
const rowClass = (base: string, selecting?: boolean) =>
  base + (selecting ? " chat__msg--selecting" : "");

function MineRow({ id, body, selecting }: { id: string; body: string; selecting?: boolean }) {
  return (
    <div className={rowClass("chat__msg chat__msg--me", selecting)} data-msg-id={id}>
      <div className="chat__msg-line">
        <div className="chat__msg-sidemeta">
          <span className="chat__msg-time">10:03</span>
        </div>
        <div className="chat__msg-content">
          <div className="chat__msg-bubble">
            <div className="chat__msg-text doc-md">
              <p>{body}</p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function Row({
  id,
  body,
  selecting,
  children,
}: {
  id: string;
  body?: string;
  selecting?: boolean;
  children?: ReactNode;
}) {
  return (
    <div className={rowClass("chat__msg", selecting)} data-msg-id={id}>
      <div className="chat__msg-meta">
        <span className="chat__msg-name">Mira</span>
      </div>
      <div className="chat__msg-line">
        <div className="chat__msg-content">
          <div className="chat__msg-bubble">
            <div className="chat__msg-text doc-md">
              {children ?? <p>{body}</p>}
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

export function ChatSelectionReplyStory({ selecting = false }: { selecting?: boolean } = {}) {
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
            <Row id="c-1" selecting={selecting} body={LONG_BODY} />
            <Row id="c-2" selecting={selecting} body="第二則訊息" />
            <MineRow id="c-3" selecting={selecting} body="好的我確認這兩個字" />
            <Row id="c-4" selecting={selecting}>
              <p>第一段開場白</p>
              <p>中間這一段是重點</p>
              <p>最後一段收尾</p>
            </Row>
            <Row id="c-5" selecting={selecting}>
              <ul>
                <li>第一項待辦</li>
                <li>第二項待辦</li>
              </ul>
            </Row>
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
