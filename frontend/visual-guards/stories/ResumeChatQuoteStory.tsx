// CT story: the wake snapshot's chat rows WITH the quote a reply carries
// (T-9871), in their real production DOM.
//
// It mounts the REAL <ChatRow> inside the REAL section skeleton, for the same
// reason ResumeChatRowStory does: a hand-copied skeleton keeps passing while
// the shipped stylesheet drifts, which is the regression a layout guard exists
// to catch.
//
// It is a SECOND story rather than three more rows on the first one because the
// existing guard counts the rows it measures (`toHaveCount(3)`), and growing
// that fixture would rewrite assertions that have nothing to do with quotes.
//
// The fixture is built to exercise the two ways this row can break a side panel
// that is NARROWER than the chat pane the quote line was designed in:
//   - PARTY NAMES LONG ENOUGH TO NOT FIT on one line at the narrow end. The
//     line has to wrap on its own.
//   - AN EXCERPT WITH NO BREAK OPPORTUNITY — 60 runes of unbroken latin, the
//     server's own cap. A quote that cannot shrink widens the whole card.
import { I18nProvider, useI18n } from "../../src/i18n";
import { ChatRow } from "../../src/components/ResumeSummaryCard";
import type { ChatMessage } from "../../src/api/adapter";

function mkMsg(over: Partial<ChatMessage> & { id: string }): ChatMessage {
  return {
    from: "m-planner",
    to: "owner",
    fromName: "小規",
    toName: "Seth",
    body: "",
    ts: 1786000000,
    tsDisplay: "2026-08-13 09:47:11 +08:00",
    bodyOmittedChars: 0,
    attachments: [],
    replyTo: null,
    replyToChat: null,
    ...over,
  } as ChatMessage;
}

/** Exactly the server's cap (chatReplyQuoteMaxChars = 60 runes) with no space
 *  in it: the widest excerpt the wire can ever deliver, and the one that has no
 *  break opportunity of its own. */
export const UNBREAKABLE_EXCERPT = "a".repeat(59) + "…";

const MESSAGES: ChatMessage[] = [
  // A reply whose quote resolved, with long names on BOTH sides and the
  // unbreakable excerpt.
  mkMsg({
    id: "q-1",
    from: "owner",
    to: "ow-longcodename-01",
    fromName: "Seth",
    toName: "外包代號很長的那一位同事",
    body: "這是回覆本體，內文照常畫在引用底下。",
    replyTo: "q-0",
    replyToChat: {
      id: "q-0",
      from: "ow-longcodename-01",
      fromName: "外包代號很長的那一位同事",
      to: "m-planner-with-a-long-id",
      toName: "另一位名字也很長的同事",
      content: UNBREAKABLE_EXCERPT,
    },
  }),
  // A reply whose original is gone: the fixed sentence, no parties.
  mkMsg({
    id: "q-2",
    body: "這一則回的是已經不存在的訊息。",
    replyTo: "q-vanished",
    tsDisplay: "2026-08-13 09:48:02 +08:00",
  }),
  // Not a reply at all — no quote element, so the guard can prove the strip is
  // not drawn on every row.
  mkMsg({
    id: "q-3",
    body: "這一則不是回覆。",
    tsDisplay: "2026-08-13 09:49:30 +08:00",
  }),
];

function Rows() {
  const { t, msg } = useI18n();
  return (
    <>
      {MESSAGES.map((m) => (
        <ChatRow key={m.id} m={m} t={t} msg={msg} />
      ))}
    </>
  );
}

export function ResumeChatQuoteStory() {
  return (
    <I18nProvider>
      <div className="mp-resume" style={{ padding: 12 }}>
        <div className="mp-resume__section" data-testid="story-section">
          <div className="mp-resume__sectionhead">
            <div
              className="mp-resume__sectiontitle"
              data-testid="story-section-title"
            >
              近期聊天
            </div>
          </div>
          <Rows />
        </div>
      </div>
    </I18nProvider>
  );
}
