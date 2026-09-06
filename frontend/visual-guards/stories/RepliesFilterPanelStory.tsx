// CT stories for the 篩選面板 both list pages wear (T-93 round 2/3).
//
// 🔴 THE FIRST EXPORT MOUNTS THE REAL PAGE, ON PURPOSE. The version of this
// guard that these files replace built its own `.replies__filters` markup, and
// when round 2 deleted that row from every page the guard went on measuring it —
// green forever, against a layout the product does not render. A story that
// ASSEMBLES the thing under test can only ever be evidence about itself. So the
// panel here comes from `RepliesPage`, exactly as the owner meets it: delete the
// `<FilterPanel>` from that page and this guard has nothing to measure.
//
// What is stand-in is only the DATA: CT builds with the mock api seam
// (`src/api/index.ts`, USE_MOCK defaults on), so the cards behind the panel are
// the mock's. That is the right trade — the question here is "does this panel
// lay out, stay in the page, and stay findable", which lives in the CSS box tree
// rather than in the fetch.
//
// ⚠️ THE ANCESTOR CHAIN IS LOAD-BEARING (a lesson inherited from the story this
// replaces, and the reason it is repeated in both exports). A bare `width: 100%`
// mount lets the container grow to its content, so NO width mutant can ever
// overflow it and the whole guard goes green-by-construction. `.app > .app__main`
// with chrome.css loaded is where the 1040px cap and the 22px gutters live.
//
// `theme` is applied the way the app applies it — `data-theme` on the root
// element — so the guard can ask the same question twice under the two theme
// families without the story re-implementing theming.
import { I18nProvider } from "../../src/i18n";
import { RepliesPage } from "../../src/components/RepliesPage";
import { ReplyCardsProvider } from "../../src/hooks/useReplyCards";
import { FilterPanel } from "../../src/components/FilterPanel";
import { IdFilterInput } from "../../src/components/IdFilterInput";
import "../../src/components/chrome.css"; // .app / .app__main — the real width cap

/** The 請示 page itself, in the app's content column. The panel, its fields and
 * every label the guard measures come from the shipped page. */
export function RepliesPageStory({ theme }: { theme: "light" | "dark" }) {
  document.documentElement.setAttribute("data-theme", theme);
  return (
    <I18nProvider>
      <div className="app" style={{ width: "100vw", maxWidth: "100vw" }}>
        <main className="app__main">
          {/* RepliesPage reads its cards through this provider — App mounts it
            * above the router, so a bare page mount throws. */}
          <ReplyCardsProvider>
            <RepliesPage />
          </ReplyCardsProvider>
        </main>
      </div>
    </I18nProvider>
  );
}

/** The SHARED shell alone, with as many 已篩選 chips as asked for.
 *
 * Why this exists beside the real page: 請示 filters on ONE axis, so its page can
 * never produce the several-chip strip. The 任務頁 can (編號 + 執行者 + 狀態 +
 * 類型) and wears this same `FilterPanel` — but driving four dropdowns through
 * the real 任務頁 would make a wrapping guard depend on that page's data. The
 * component is the shipped one; only the chip list is dictated. */
export function FilterChipsStory({
  theme,
  chipLabels,
}: {
  theme: "light" | "dark";
  chipLabels: string[];
}) {
  document.documentElement.setAttribute("data-theme", theme);
  return (
    <I18nProvider>
      <div className="app" style={{ width: "100vw", maxWidth: "100vw" }}>
        <main className="app__main">
          <FilterPanel
            title="任務"
            count={chipLabels.length}
            open={false}
            onOpenChange={() => undefined}
            chips={chipLabels.map((label, i) => ({
              key: `${i}`,
              label,
              onRemove: () => undefined,
            }))}
            onClearAll={() => undefined}
            onApply={() => undefined}
            onCancel={() => undefined}
            testId="replies-filter"
          >
            <IdFilterInput
              value=""
              onChange={() => undefined}
              label="任務編號"
              testId="filter-task-id"
              widthCh={10}
            />
          </FilterPanel>
        </main>
      </div>
    </I18nProvider>
  );
}
