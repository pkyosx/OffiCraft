// CT story for DiffView (T-1f39) — the diff surface against the REAL sheets.
//
// Two facts jsdom cannot see are staged here:
//   ① an added row and a removed row must land on VISIBLY different
//      backgrounds. Both are `color-mix(... var(--token) 18%, transparent)`,
//      which jsdom returns verbatim as source text — only a real engine
//      resolves them to the rgb() pair the guard compares.
//   ② a line with no break opportunity must scroll inside .diff-view__scroll
//      and leave the PAGE unscrollable. That is layout, so jsdom is blind.
//
// The host is deliberately NARROW (360px, owner's phone class) inside the
// normal page flow — the same shape the pending history modal will give it.
import { I18nProvider } from "../../src/i18n";
import { DiffView } from "../../src/components/DiffView";

/** 300 chars, no space, no hyphen — the shape a pasted token/URL takes. */
export const LONG_LINE =
  "sha256:" +
  "0123456789abcdef".repeat(18) +
  "/twin(desired_state/desired_machine_id/refocus_since)";

const SHORT_BEFORE = [
  "# 角色定義",
  "",
  "你是 PR review 的第一關。",
  "",
  "## SOP",
  "1. 看 PR 狀態",
  "2. 確認是否 review 過",
  "3. 觸發 review",
  "4. 等 webhook",
  "5. 依 verdict 決定",
  "6. 收尾",
];

const SHORT_AFTER = [
  "# 角色定義",
  "",
  "你是 PR review 的第一關,也是最後一關。",
  "",
  "## SOP",
  "1. 看 PR 狀態",
  "2. 確認是否 review 過",
  "3. 觸發 review",
  "4. 等 webhook",
  "5. 依 verdict 決定",
  "6. 收尾並回報",
];

/** One line that WRAPS several times in a split half, paired with a one-word
 * replacement. It exists so the row-alignment guard has a row whose two sides
 * are genuinely different HEIGHTS: on matched short lines the two gutters sit
 * level whatever the CSS says, so an assertion measured there passes on a
 * broken sheet as readily as on a correct one (measured: a
 * `vertical-align: bottom` mutant on the split gutters left all five tests
 * green). */
const LONG_CJK_BEFORE =
  "這一行刻意寫得很長，長到在兩欄對照的半邊裡一定會折成好幾行，" +
  "因為只有折過的那一列才問得出「行號有沒有跟著它的文字一起往下走」這個問題；" +
  "沒有折行的列，兩側本來就一樣高，量了也證明不了任何事情。";
const LONG_CJK_AFTER = "改成一句短的。";

/** `longLine` is an INPUT dimension, not decoration: the overflow guard needs
 * an unbreakable line, and the full-width-tint guard needs its absence (with
 * a long line present the table is wide by force and would pass vacuously).
 * `asymmetric` is the same kind of dimension for row ALIGNMENT — see the pair
 * above. */
export function DiffViewStory({
  width = 360,
  longLine = true,
  asymmetric = false,
}: {
  width?: number;
  longLine?: boolean;
  asymmetric?: boolean;
}) {
  const tail = longLine ? ["", LONG_LINE] : [];
  return (
    <I18nProvider>
      <div style={{ width, padding: 12 }} data-surface="diff">
        <DiffView
          before={[
            ...SHORT_BEFORE,
            ...(asymmetric ? ["", LONG_CJK_BEFORE] : []),
            ...tail,
          ].join("\n")}
          after={[
            ...SHORT_AFTER,
            ...(asymmetric ? ["", LONG_CJK_AFTER] : []),
            ...tail,
          ].join("\n")}
        />
      </div>
    </I18nProvider>
  );
}
