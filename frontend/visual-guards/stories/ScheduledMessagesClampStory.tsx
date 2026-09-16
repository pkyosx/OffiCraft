// CT story (T-f059): the REAL <ScheduledMessagesCard> rows against the REAL
// member-detail.css, so the three-line collapse can be MEASURED.
//
// The card reads its rows through useScheduledMessages → api.listScheduledMessages,
// so the one thing faked here is that call: the mock adapter's own rows are not
// long enough to clamp, and the whole question is what a long body does. `api` is
// a plain object export, which is the same seam SoftwareUpdateStory patches and
// the vitest suite spies on.
//
// Two bodies, because the guard has to separate "the clamp works" from "the
// clamp is offered at all":
//   • LONG  — many lines at EVERY panel width, so 收合 really hides something
//   • SHORT — one line at the NARROWEST width under test, so it must carry no
//             展開 control and no hidden overflow. It doubles as the guard's
//             line-height ruler: its own box IS one line.
import { I18nProvider } from "../../src/i18n";
import { ScheduledMessagesCard } from "../../src/components/ScheduledMessagesCard";
import { api } from "../../src/api";
import type { ScheduledMessage } from "../../src/api/adapter";

export const LONG_ID = "sch-long";
export const SHORT_ID = "sch-short";

// One paragraph, no newlines: the number of lines it occupies must come from
// WRAPPING, which is what makes the narrow and wide measurements different
// questions rather than the same one asked twice.
//
// Long enough to exceed three lines at the WIDEST panel under test, which is the
// binding constraint: a body that wraps to many lines at 320px can still be a
// tidy three at 900px, and a fixture that lands there tests nothing. The spec
// re-measures this at both widths and fails loudly rather than passing vacuously
// if this text is ever shortened past that point.
const LONG_BODY =
  "每天早上請先看一遍昨天沒有結束的任務,把還在等我回覆的那幾張標出來,再確認今天要交付" +
  "的東西有沒有卡在別人身上;如果有,直接在群組裡點名,不要等到下午才說。接著看一次昨晚" +
  "跑的排程有沒有漏送,尤其是跨時區的那幾條,漏送不會有錯誤訊息,只能自己對。再來把昨天" +
  "收到的回覆卡整理過一遍,已經有答案的就收掉,還沒有答案的標上還在等誰,不要讓它一直掛" +
  "在那裡佔位子。接著確認機器上跑著的那幾個分身有沒有卡住,卡住的先看它最後一次說了什麼" +
  "再決定要不要重開,重開之前把現場留下來,不然下次還是一樣。然後看一次今天要開的會,能" +
  "取消的取消,不能取消的先把要問的問題寫下來,會議上再想就來不及了。最後把今天要做的三" +
  "件事寫在卡片上,寫不出三件就代表今天的優先順序還沒想清楚,先想清楚再開始動手,不要一" +
  "邊做一邊想,那樣一天結束的時候什麼都只做了一半。";

const SHORT_BODY = "早安,今天也順利";

const ROWS: ScheduledMessage[] = [
  {
    id: LONG_ID,
    memberId: "mira",
    label: "晨間巡檢",
    body: LONG_BODY,
    cadence: "daily",
    dayOfWeek: 1,
    dayOfMonth: 1,
    hour: 9,
    minute: 0,
    customDays: [],
    customHours: [],
    customMinutes: [],
    customMonths: [],
    timezone: "Asia/Taipei",
    status: "enabled",
    lastFiredSlot: "",
    lastFiredTs: 0,
    createdTs: 1754800000,
  },
  {
    id: SHORT_ID,
    memberId: "mira",
    label: "打招呼",
    body: SHORT_BODY,
    cadence: "daily",
    dayOfWeek: 1,
    dayOfMonth: 1,
    hour: 8,
    minute: 30,
    customDays: [],
    customHours: [],
    customMinutes: [],
    customMonths: [],
    timezone: "Asia/Taipei",
    status: "enabled",
    lastFiredSlot: "",
    lastFiredTs: 0,
    createdTs: 1754800001,
  },
];

/** `width` is the PANEL's width, not the viewport's — the card lives in the
 * detail column, and how many lines a body wraps to is a property of that
 * column. The spec drives it at both ends of the range the column actually
 * takes. */
export function ScheduledMessagesClampStory({ width }: { width: number }) {
  // Patched before the first render commits, so the card's mount fetch already
  // resolves to these rows.
  api.listScheduledMessages = async () => ROWS;

  return (
    <I18nProvider>
      <div className="mp" style={{ width, height: 900 }}>
        <ScheduledMessagesCard memberId="mira" />
      </div>
    </I18nProvider>
  );
}
