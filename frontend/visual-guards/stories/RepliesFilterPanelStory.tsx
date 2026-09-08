// CT stories for the 篩選 row both list pages wear (T-93 round 2/3 → T-118).
//
// 🔴 THE FIRST EXPORT MOUNTS THE REAL PAGE, ON PURPOSE. The version of this
// guard that these files replace built its own `.replies__filters` markup, and
// when round 2 deleted that row from every page the guard went on measuring it —
// green forever, against a layout the product does not render. A story that
// ASSEMBLES the thing under test can only ever be evidence about itself. So the
// panel here comes from `RepliesPage`, exactly as the owner meets it: delete the
// `<FilterPanel>` from that page and this guard has nothing to measure.
//
// 🔁 WHAT T-118 CHANGED IN THIS FILE. owner 2026-09-06 20:07 (c-c3d681fe05da),
// restated 20:19 (c-38c7759e6377):「不要多filter那一層了,全部拉出來…也不用再顯示
// 14筆已篩選跟那一行跟案件那個子標了,案件跟請示卡都一樣」. `FilterPanel` now
// takes ONLY `testId` + `children` — `title`/`count`/`open`/`onOpenChange`/
// `chips`/`onClearAll`/`onApply`/`onCancel` are gone, and so is the `FilterChip`
// type. The second export used to dictate a four-CHIP 已篩選 strip; there is no
// strip any more, so it dictates the four-FIELD row instead — the same question
// (does the shared shell WRAP its contents at a phone width, or run them out of
// the column?) asked about the thing that still exists. `IdFilterInput` also
// gained a required `onCommit`, so both exports pass one.
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
import App from "../../src/App";
import { ReplyCardsProvider } from "../../src/hooks/useReplyCards";
import { FilterPanel } from "../../src/components/FilterPanel";
import { IdFilterInput } from "../../src/components/IdFilterInput";
import { MultiSelectFilter } from "../../src/components/MultiSelectFilter";
import "../../src/components/chrome.css"; // .app / .app__main — the real width cap
// ⚠️ `MultiSelectFilter` wears `.tasks__filter` / `.tasks__ms-*` but imports NO
// stylesheet of its own — it relies on its host (`TasksPage`) having imported
// `tasks.css`. That is a live exception to frontend/.claude/rules/
// css-layout-traps.md 「用了哪份 CSS 的 class,就要自己 import 那份 CSS」, and it
// is why this line exists: without it the second export's dropdowns render with
// UA styling and the wrapping guard would measure boxes the product never
// paints — the exact green-by-construction failure this file was rewritten to
// end. If that component ever imports its own CSS, delete this line rather than
// leaving two importers.
import "../../src/components/tasks.css";

/** The 請示 page itself, in the app's content column. The panel, its field and
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

/** The production shell with the requested page selected, including topbar and
 * nav. The width preference is set before I18nProvider reads it. */
export function AppPageStory({
  page,
  theme,
  wide = false,
}: {
  page: "tasks" | "replies";
  theme: "light" | "dark";
  wide?: boolean;
}) {
  document.documentElement.setAttribute("data-theme", theme);
  window.localStorage.setItem("oc.wide", wide ? "true" : "false");
  window.location.hash = `#${page}`;
  return (
    <I18nProvider>
      <ReplyCardsProvider>
        <App onLogout={() => {}} />
      </ReplyCardsProvider>
    </I18nProvider>
  );
}

/** The SHARED shell alone, wearing the 任務頁's FOUR-field row.
 *
 * 🔁 REPLACES `FilterChipsStory`, which mounted the same shell with a dictated
 * list of 已篩選 chips. OVERTURNED BY owner 2026-09-06 (c-c3d681fe05da): the
 * strip and its chips no longer exist, so there is nothing left to lay one out
 * from. The QUESTION survives unchanged — several conditions on one shared row,
 * at the narrowest phone width, must WRAP inside the column rather than run out
 * of it — and the thing that now carries several conditions is the FIELDS.
 *
 * Why this exists beside the real page: 請示 filters on ONE axis, so its page can
 * never produce a multi-field row. The 任務頁 can (編號 + 執行者 + 狀態 + 類型)
 * and wears this same `FilterPanel` — but driving four dropdowns through the
 * real 任務頁 would make a wrapping guard depend on that page's data. Both
 * components are the shipped ones; only the option lists are dictated. */
export function FilterFieldsStory({ theme }: { theme: "light" | "dark" }) {
  document.documentElement.setAttribute("data-theme", theme);
  const noop = () => undefined;
  return (
    <I18nProvider>
      <div className="app" style={{ width: "100vw", maxWidth: "100vw" }}>
        <main className="app__main">
          <FilterPanel testId="replies-filter">
            <IdFilterInput
              value=""
              onChange={noop}
              onCommit={noop}
              label="任務編號"
              testId="filter-task-id"
              widthCh={10}
            />
            <MultiSelectFilter
              noun="負責人"
              allLabel="所有負責人"
              options={[
                { value: "mira", label: "陳小明" },
                { value: "kyle", label: "林大華" },
              ]}
              selected={new Set(["mira"])}
              onChange={noop}
              testId="filter-executor"
            />
            <MultiSelectFilter
              noun="類型"
              allLabel="所有類型"
              options={[{ value: "feature", label: "功能開發" }]}
              selected={new Set(["feature"])}
              onChange={noop}
              testId="filter-type"
            />
            <MultiSelectFilter
              noun="狀態"
              allLabel="所有狀態"
              options={[{ value: "in_progress", label: "進行中" }]}
              selected={new Set(["in_progress"])}
              onChange={noop}
              testId="filter-status"
            />
          </FilterPanel>
        </main>
      </div>
    </I18nProvider>
  );
}
