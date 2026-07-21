// CT story (T-7fa1): the chat column in the exact state that broke on a phone —
// an OFFLINE peer with no messages (so `.chat__body` renders the `.chat__offline`
// empty state) while the composer carries the wake row AND the undispatched-wake
// DispatchAlert.
//
// Why the whole column and not just the empty state: the bug is a BUDGET bug.
// `.chat` is a flex column whose only shrinkable item is `.chat__body`
// (`flex:1; min-height:0`), so every pixel the composer gains the body loses.
// Measuring the empty state alone would never reproduce it — the squeeze only
// exists relative to a real header + a real composer inside a bounded column.
//
// The DispatchAlert is the REAL component (not a stand-in) on purpose: this
// guard must also fail if someone "fixes" the overlap by shrinking the notice,
// which is the one repair T-7fa1 forbids — the notice's job is to tell the owner
// what happened and what to do next.
//
// No function props cross the mount bridge (same rule as ChatWakeRowStory); the
// only prop is a plain number, the height the app shell gives `.chat`.
import { DispatchAlert } from "../../src/components/DispatchAlert";
import { BoltIcon, MoonIcon } from "../../src/components/icons";
import { I18nProvider } from "../../src/i18n";
import "../../src/components/office.css";

export function ChatOfflineNoticeStory({ chatHeight }: { chatHeight: number }) {
  return (
    <I18nProvider>
      {/* Stands in for the app shell's chat slot: `.chat` is `height: 100%`, so
          it needs a parent with a definite height exactly the way the real
          `.office__chat` column gives it one. */}
      <div style={{ height: chatHeight }} data-testid="chat-slot">
        <div className="chat">
          <header className="chat__header">
            <span
              className="avatar"
              style={{ width: 40, height: 40, flex: "none" }}
            />
            <div className="chat__header-text">
              <div className="chat__header-name">
                <span>Mira</span>
              </div>
              <div className="chat__header-sub">特助</div>
            </div>
          </header>

          <div className="chat__body" data-testid="chat-body">
            <div className="chat__offline" data-testid="offline-empty">
              <span className="chat__offline-icon">
                <MoonIcon size={26} />
              </span>
              <div className="chat__offline-title">Mira 目前離線</div>
              <div className="chat__offline-hint">
                你仍可在下方留言，Mira 上線後就會讀到。
              </div>
            </div>
          </div>

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
            <DispatchAlert kind="wake" testId="chat-wake-undispatched" />
            <div className="chat__composer-row">
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
      </div>
    </I18nProvider>
  );
}
