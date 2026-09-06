// CT story for T-122 — the 建議回覆 row at a real card width, in the two shapes
// the owner's setting can actually take.
//
// The sentences are OWNER FREE TEXT, so the two extremes are not hypothetical:
// one entry can be a whole paragraph, and there can be a lot of them. They
// break the layout in opposite directions — a long one widens the row, many of
// them make it tall — so both are on screen here and both are measured.
//
// The row is mounted inside `.reply-card` / `.reply-composer`, the real width
// chain it hangs in on 等我回覆; a bare mount would give it the viewport's width
// and certify nothing about fitting a card.
import { SuggestedReplies } from "../../src/components/SuggestedReplies";

/** One entry long enough that a row which refuses to wrap will burst the card
 * at BOTH widths, not only the phone one. */
const LONG = [
  "收到，這個我看過了。先照你說的做，但倉庫那邊的位子要等他們週一回覆才能確定，所以這一批我會先押著不出，等確認再一次送。",
];

/** Enough entries to need several lines — and to overrun the row's height cap,
 * which is what turns the row into its own scroll box instead of pushing the
 * composer down the page. */
const MANY = [
  "收到",
  "照這樣做",
  "先擱著，這週不碰",
  "我要再想一下",
  "找銀月確認",
  "這個交給外包",
  "先看成本",
  "改天再說",
  "同意，直接上",
  "不同意，退回",
  "補一張票",
  "我來處理",
];

export function SuggestedRepliesStory() {
  return (
    <div className="replies">
      <article className="reply-card" data-testid="card-long">
        <div className="reply-composer">
          <div className="chat__composer-row">
            <textarea className="chat__input" rows={1} readOnly />
          </div>
          <SuggestedReplies replies={LONG} onPick={() => {}} testId="row-long" />
        </div>
      </article>
      <article className="reply-card" data-testid="card-many">
        <div className="reply-composer">
          <div className="chat__composer-row">
            <textarea className="chat__input" rows={1} readOnly />
          </div>
          <SuggestedReplies replies={MANY} onPick={() => {}} testId="row-many" />
        </div>
      </article>
    </div>
  );
}
