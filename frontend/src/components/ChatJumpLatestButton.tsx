// ① T-48 回到底部箭頭 — the round arrow floating over the bottom-right of the
// message pane, just above the composer.
//
// WHEN IT SHOWS is not this component's business (see lib/chatBottomAffordance):
// the owner's rule is "the newest message is not in the viewport", which is
// neither "scrolled more than a screen" nor "a new message arrived". It hands
// its place to the new-message preview strip whenever one is up.
//
// The visual language is deliberately the retired 「有新訊息」 pill's — card
// surface, hairline border, soft shadow, every colour a theme token — because
// the owner's ruling on the styling was 「跟現在的風格差不多」. What changed is
// the SHAPE (round) and the SIDE (right, not centre).
import { ArrowDownIcon } from "./icons";
import { useI18n } from "../i18n";

export function ChatJumpLatestButton({ onClick }: { onClick: () => void }) {
  const { t } = useI18n();
  return (
    <button
      type="button"
      className="chat__jump-latest"
      data-testid="chat-jump-latest"
      aria-label={t.chat.jumpToLatest}
      title={t.chat.jumpToLatest}
      onClick={onClick}
    >
      <ArrowDownIcon size={18} />
    </button>
  );
}
