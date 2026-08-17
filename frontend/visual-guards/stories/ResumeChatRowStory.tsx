// CT story: the wake snapshot's 「近期聊天」 rows in their REAL production DOM.
//
// It mounts the REAL <ChatRow> (exported from ResumeSummaryCard for this
// purpose) inside the REAL section skeleton the card emits — the same
// `.mp-resume__section` / `.mp-resume__sectiontitle` wrapper — so the guard
// measures the shipped stylesheet rather than a hand-built lookalike. A copied
// skeleton would keep passing while the real row drifted, which is precisely
// the regression this guard is for.
//
// The fixture is chosen so every dimension the guard measures is EXERCISED:
//   - a SHORT-named row and a LONG-named row, because the timestamp's position
//     used to depend on how much room the names left (that inconsistency is
//     complaint ③).
//   - a row with `bodyOmittedChars` > 0, because the fold mark's position
//     relative to its own message is complaint ②.
//   - bodies long enough to wrap at 390px, so "the body starts at the section's
//     left edge" is a statement about a real paragraph and not a short word
//     that would sit at the left edge under almost any layout.
import { I18nProvider, useI18n } from "../../src/i18n";
import { ChatRow } from "../../src/components/ResumeSummaryCard";
import type { ChatMessage } from "../../src/api/adapter";

function mkMsg(over: Partial<ChatMessage> & { id: string }): ChatMessage {
  return {
    from: "m-planner",
    to: "m-exec",
    fromName: "小規",
    toName: "小執",
    body: "",
    ts: 1786000000,
    tsDisplay: "2026-08-13 09:47:11 +08:00",
    bodyOmittedChars: 0,
    attachments: [],
    ...over,
  } as ChatMessage;
}

const LONG_BODY =
  "這一則的內文刻意寫得夠長，長到在 390px 的視窗裡一定會折行，" +
  "這樣「內文的左邊界」量到的就是一個真正的段落左緣，而不是一個" +
  "短到隨便什麼版面都會貼齊左邊的單字。";

const MESSAGES: ChatMessage[] = [
  // Short names — the case where the old wrapping layout kept the timestamp
  // inline after the id.
  mkMsg({ id: "c-1", body: LONG_BODY }),
  // Long names — the case where the old layout pushed the timestamp onto its
  // own line. Same row type, different timestamp position: complaint ③.
  mkMsg({
    id: "c-2",
    from: "ow-longcodename-01",
    to: "m-exec",
    fromName: "外包代號很長的那一位同事",
    toName: "小執",
    body: LONG_BODY,
    tsDisplay: "2026-08-13 09:48:02 +08:00",
  }),
  // A FOLDED message — complaint ②. The mark must sit beside this message.
  mkMsg({
    id: "c-3",
    body: "這一則被折起來了，後面接的是折起記號。" + LONG_BODY,
    bodyOmittedChars: 462,
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

export function ResumeChatRowStory() {
  return (
    <I18nProvider>
      {/* The real card's own wrapper chain, so the section title is measured
        * from the same box the rows are measured from. */}
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
