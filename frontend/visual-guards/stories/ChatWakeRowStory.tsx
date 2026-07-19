// CT story: the T-94c1 offline composer — a wake row (queue notice + ⚡喚醒
// button) sitting ABOVE a FULL-WIDTH composer row (attach + input + send). This
// is the exact production DOM shape ChatArea renders while a member is
// offline/stopped; it passes NO function props across the mount bridge and uses
// the real class names so the loaded office.css governs the layout the guard
// measures.
//
// The guard (chat-wake-row.ct.spec.tsx) asserts the mockup's core win: because
// the wake button lives in its OWN row, the composer input stays wide (it is not
// crushed the way it would be if the button shared the input row) — and nothing
// overflows at phone width.
import { BoltIcon, MoonIcon } from "../../src/components/icons";

export function ChatWakeRowStory() {
  return (
    <div className="chat">
      <footer className="chat__composer">
        <div className="chat__wake-row" data-testid="wake-row">
          <span className="chat__wake-row__hint">
            <MoonIcon size={14} />
            Mira 目前離線中 — 訊息會排隊，或立即喚醒上線
          </span>
          <button
            type="button"
            className="chat__wake-btn"
            data-testid="wake-btn"
          >
            <BoltIcon size={15} />
            <span>喚醒</span>
          </button>
        </div>
        <div className="chat__composer-row" data-testid="composer-row">
          <button type="button" className="chat__attach" aria-label="附加檔案">
            <span style={{ width: 18, height: 18 }} />
          </button>
          <textarea
            className="chat__input"
            data-testid="composer-input"
            rows={1}
            placeholder="回覆 Mira…"
            readOnly
          />
          <button type="button" className="chat__send" aria-label="送出">
            <span style={{ width: 16, height: 16 }} />
          </button>
        </div>
      </footer>
    </div>
  );
}
