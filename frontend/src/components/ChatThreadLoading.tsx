// ChatThreadLoading — 「這條對話的內容還沒到」, on screen (T-48 fix12).
//
// 🔴 ONE COMPONENT, ONE STATE, EVERY ENTRANCE (owner c-de666642e77b「不管是進聊
// 天室,或點選元訊息都是這樣」; c-d24ebd7f8d78「照理說應該只有改一個地方吧?就會
// 都有作用?」).
//
// It is rendered from exactly ONE place in `ChatArea` — the thread body's empty
// branch — off exactly ONE input, `useChat`'s `initialLoading`. Neither this
// component nor that flag knows or asks which door the reader came through, so
// a third entrance added later gets the spinner with no new wiring. Two copies,
// one per entrance, is the thing this shape exists to prevent: two flags that
// have to agree are two flags that can disagree, and the disagreement is
// invisible — a spinner that never stops looks exactly like a slow network.
//
// 🔴 WHY IT COULD NOT STAY AN ABSENCE. Before fix12 an anchor entry showed the
// jump target after two round trips. It now shows nothing until EVERYTHING from
// the anchor to the live tail has been fetched (owner c-6a973512ed77「我是指整個
// 訊息撈完才 render」), which on a long thread is many round trips. An empty
// room is what a broken room looks like too.
import { useEffect, useState } from "react";
import { useI18n } from "../i18n";

// 🔴 A WAIT NOBODY NOTICED IS NOT A WAIT WORTH DRAWING. Showing instantly means
// a fast conversation paints a spinner and erases it inside one or two frames —
// a flash, i.e. noise that reads as a glitch rather than as progress. So the
// element does not exist for the first stretch of the wait; past it, it is
// always there.
//
// ⚠️ IT IS A DELAY, NOT A MINIMUM DISPLAY TIME. Once the content lands this
// unmounts immediately — no 「keep it up for at least X so it does not flicker」
// rule, because that one trades the reader's time for the animation's dignity.
export const CHAT_LOADING_DELAY_MS = 150;

export function ChatThreadLoading() {
  const { t } = useI18n();
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => setShown(true), CHAT_LOADING_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, []);
  if (!shown) return null;
  return (
    <div className="chat__loading" role="status" aria-live="polite">
      <span className="chat__loading-spinner" aria-hidden />
      <span className="chat__loading-label">{t.chat.threadLoading}</span>
    </div>
  );
}
