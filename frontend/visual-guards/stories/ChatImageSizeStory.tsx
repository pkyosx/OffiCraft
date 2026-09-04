// CT story: ONE chat message row carrying ONE image attachment, in the real
// production DOM shape, so the loaded office.css — not a mock — decides the
// geometry the guard measures.
//
// 🔴 THE IMAGE STARTS WITH NO `src`, ON PURPOSE. The contract under test is
// 「這一列的高度在圖片載入前後必須一樣」 (T-48, owner rc-7a6a159a102f), and the
// only way to measure "before" is to mount a row whose bytes have not arrived.
// The guard sets `src` itself, waits for `decode()`, and measures again.
//
// No props cross the mount bridge — the spec drives the `src` through the DOM,
// which is also closer to what the browser really does with a late blob.
export function ChatImageSizeStory() {
  return (
    <div className="chat__body">
      <div className="chat__messages" data-testid="chat-messages">
        <div className="chat__msg" data-testid="row-before">
          <div className="chat__msg-bubble">
            <div className="chat__msg-text">a text row above, for a stable neighbour</div>
          </div>
        </div>
        <div className="chat__msg" data-testid="image-row">
          <div className="chat__msg-bubble">
            <div className="chat__msg-attachments">
              <span className="chat__msg-attachment">
                {/* eslint-disable-next-line jsx-a11y/alt-text */}
                <img
                  className="chat__msg-image chat__msg-image--clickable"
                  data-testid="chat-image"
                  alt="attachment"
                />
              </span>
            </div>
          </div>
        </div>
        <div className="chat__msg" data-testid="row-after">
          <div className="chat__msg-bubble">
            <div className="chat__msg-text">a text row below — it must not be pushed</div>
          </div>
        </div>
      </div>
    </div>
  );
}
