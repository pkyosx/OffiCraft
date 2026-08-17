// CT story for T-ee17 — the reply card's 任務資訊 row at a REAL card width.
//
// Two shapes, and they answer different questions:
//
// `ReplyTaskRefStory` mounts the row on its own, twice: an over-long task title
// (the shape that made the truncation guard necessary) and a short one. A guard
// that only mounts the long title cannot tell "the title is clipped" from
// "every title is clipped", and the second reading is a different, worse bug.
//
// `ReplyCardLeadRowStory` mounts the REAL ChatReplyCard against the mock api,
// so the ORDER it renders is production's order, not one hand-written here.
// That matters for the acceptance-round move (owner 2026-08-14:「這個不能夠放到
// 最一開始嗎？」): a story that placed the row itself would stay green with the
// row moved back to the bottom of the card, which is precisely the mutant this
// has to catch.
//
// The rows sit inside `.reply-card` on purpose: the row is a flex child of the
// card, so its available width — the whole thing this guard measures — comes
// from the card's own box and padding. Measuring the row on a bare page would
// hand it the full viewport and the overflow would simply not happen.
import { useEffect, useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { ReplyCardTaskRef } from "../../src/components/ReplyCardBody";
import { ChatReplyCard } from "../../src/components/ChatReplyCard";
import { __resetMock, __injectMockReplyCard } from "../../src/api/mock";
import type { ReplyCard } from "../../src/api/adapter";

// Long enough to overflow at DESKTOP width too, not just on a phone. A title
// that only bursts the row at 390px would leave the 1040px case asserting
// nothing — and real ticket titles do run this long (this one is a real one,
// with its own tail restored).
const LONG_TITLE =
  "[ACE-7580] SOC2 年度風險評估：review Google Drive 上的 ISMS 文件 + 產出風險評鑑清冊與處理計畫" +
  "，並對照去年度的缺失追蹤表逐項確認關閉狀態、補齊佐證，最後彙整成給稽核方的單一交付包";
const SHORT_TITLE = "補一把字數尺";

export function ReplyTaskRefStory() {
  return (
    <I18nProvider>
      <div className="replies">
        <article className="reply-card" data-testid="card-long">
          <ReplyCardTaskRef
            task={{ id: "t-long", typeKey: "review-pr", title: LONG_TITLE }}
            onJump={() => {}}
          />
        </article>
        <article className="reply-card" data-testid="card-short">
          <ReplyCardTaskRef
            task={{ id: "t-short", typeKey: "", title: SHORT_TITLE }}
            onJump={() => {}}
          />
        </article>
      </div>
    </I18nProvider>
  );
}

const LEAD_CARD: ReplyCard = {
  id: "rc-lead",
  from: "mira",
  kind: "decision",
  summary: "要現在把這份設定同步到 Jira 嗎？",
  // Long enough that a row sitting below it would be well past the fold of a
  // phone-height card — the very complaint that moved it.
  body: "同步之後那邊的欄位就會被覆寫，先確認一下再動。這段刻意寫長一點，讓正文佔掉整個卡片的高度。",
  options: ["核可，直接同步上去", "先不要"],
  status: "waiting",
  attachments: [],
  createdTs: Date.now() / 1000 - 600,
  answeredTs: null,
  chatMessageId: "msg-lead",
  answer: null,
  task: { id: "t-lead", typeKey: "sync-jira", title: LONG_TITLE },
};

export function ReplyCardLeadRowStory() {
  // The card is fetched by the component, so seed the mock before it mounts and
  // hold the first render back until the seed is in — otherwise the fetch races
  // an empty mock and the card renders as a load error.
  const [seeded, setSeeded] = useState(false);
  useEffect(() => {
    __resetMock();
    __injectMockReplyCard(LEAD_CARD);
    setSeeded(true);
  }, []);
  if (!seeded) return null;
  return (
    <I18nProvider>
      <div className="replies" data-testid="lead-host">
        <ChatReplyCard replyCardId="rc-lead" fallbackSummary="…" />
      </div>
    </I18nProvider>
  );
}
