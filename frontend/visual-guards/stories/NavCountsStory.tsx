// CT story for the three nav counts (T-2658).
//
// Mounts the REAL `App` behind the same two providers production wires above it
// (main.tsx's I18nProvider, AuthGate's ReplyCardsProvider) against the REAL mock
// adapter — same reasoning as NavTabsNarrowStory: the thing under test is what
// the strip's own stylesheet and theme tokens paint, so a hand-built row of
// spans would measure colours that never ship.
//
// The mock store starts empty on purpose (an honest ✓ empty state), so the
// story injects one batch of open tasks, waiting cards and inbound messages —
// all three counts have to be on screen at once for the guard to compare the
// neutral one against the two red ones.
import { I18nProvider } from "../../src/i18n";
import { ReplyCardsProvider } from "../../src/hooks/useReplyCards";
import App from "../../src/App";
import {
  __injectMockChat,
  __injectMockReplyCard,
  __injectMockTask,
} from "../../src/api/mock";
import { mkTask } from "./taskFixtures";

/** What the strip shows: 辦公室 3 · 請示 2 · 任務 7. Two digits short of the
 *  99+ clamp, so the pills sit at their ordinary width. */
export const SEEDED = { chatUnread: 3, replies: 2, tasks: 7 };

let seeded = false;

function seed(): void {
  // Seeding is a side effect in a render body, so it has to be idempotent: the
  // mock store is module state and this function runs on every render of the
  // story. (A CT test does get a fresh page, and therefore a fresh module, per
  // test — the latch is about re-renders within one test, not about leaking
  // between them.)
  if (seeded) return;
  seeded = true;
  for (let i = 0; i < SEEDED.tasks; i++) {
    __injectMockTask(
      mkTask({ id: `t-nav${i}`, taskNo: `T-nav${i}`, title: `未結案任務 ${i}` })
    );
  }
  for (let i = 0; i < SEEDED.replies; i++) {
    __injectMockReplyCard({
      id: `rc-nav${i}`,
      from: "mira",
      kind: "decision",
      summary: "要幫你寄出這封信嗎？",
      body: "",
      options: ["寄出", "先不要"],
      status: "waiting",
      attachments: [],
      createdTs: 1000,
      answeredTs: null,
      chatMessageId: `msg-nav${i}`,
      answer: null,
    });
  }
  for (let i = 0; i < SEEDED.chatUnread; i++) {
    __injectMockChat({
      id: `c-nav${i}`,
      from: "mira",
      to: "owner",
      body: "未讀訊息",
      ts: 1000,
      attachments: [],
      replyCardId: null,
    });
  }
}

export function NavCountsStory() {
  seed();
  return (
    <I18nProvider>
      <ReplyCardsProvider>
        <App onLogout={() => {}} />
      </ReplyCardsProvider>
    </I18nProvider>
  );
}
