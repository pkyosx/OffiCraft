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
// What is stand-in is only the DATA: CT builds with the mock api seam
// (`src/api/index.ts`, USE_MOCK defaults on), so the cards behind the panel are
// the mock's. That is the right trade — the question here is "does this panel
// lay out, stay in the page, and stay findable", which lives in the CSS box tree
// rather than in the fetch.
//
// ⚠️ THE ANCESTOR CHAIN IS LOAD-BEARING (a lesson inherited from the story this
// replaces, and the reason it is repeated in each story). A bare `width: 100%`
// mount lets the container grow to its content, so NO width mutant can ever
// overflow it and the whole guard goes green-by-construction. `.app > .app__main`
// with chrome.css loaded is where the 1040px cap and the 22px gutters live.
//
// `theme` is a built-in, selected the way the app selects one — the stored
// theme id I18nProvider reads, which it applies as `data-theme` on the root —
// so the guard can ask the same question under each built-in without the story
// re-implementing theming.
import { I18nProvider } from "../../src/i18n";
import { RepliesPage } from "../../src/components/RepliesPage";
import App from "../../src/App";
import { ReplyCardsProvider } from "../../src/hooks/useReplyCards";
import { LS_THEME } from "../../src/lib/themePaint";
import type { BuiltinThemeId } from "../../src/lib/themeBundleCore";
import "../../src/components/chrome.css"; // .app / .app__main — the real width cap
// ⚠️ `MultiSelectFilter` wears `.tasks__filter` / `.tasks__ms-*` but imports NO
// stylesheet of its own — it relies on its host (`TasksPage`) having imported
// `tasks.css`. That is a live exception to frontend/.claude/rules/
// css-layout-traps.md 「用了哪份 CSS 的 class,就要自己 import 那份 CSS」, and it
// is why this line exists: without it the 請示頁 story's dropdowns render with
// UA styling and the wrapping guard would measure boxes the product never
// paints — the exact green-by-construction failure this file was rewritten to
// end. If that component ever imports its own CSS, delete this line rather than
// leaving two importers.
import "../../src/components/tasks.css";
import { api } from "../../src/api";
import { __injectMockReplyCard } from "../../src/api/mock";
import type { ReplyCard } from "../../src/api/adapter";

let seeded = false;
let held = false;
function seedCards([waiting, snoozed, answered]: readonly [number, number, number]) {
  if (seeded) return;
  seeded = true;
  const now = Date.now() / 1000;
  const card = (i: number, over: Partial<ReplyCard>): ReplyCard => ({
    id: `rc-tabs-${i}`,
    from: "mira",
    kind: "decision",
    summary: `請示 ${i}`,
    body: "",
    options: [{ text: "好", aiPick: true }],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: now - 600 - i,
    answeredTs: null,
    chatMessageId: `msg-tabs-${i}`,
    answer: null,
    task: null,
    ...over,
  });
  let i = 0;
  for (let n = 0; n < waiting; n++) __injectMockReplyCard(card(i++, {}));
  for (let n = 0; n < snoozed; n++) __injectMockReplyCard(card(i++, { snoozedUntilTs: now + 3600 }));
  for (let n = 0; n < answered; n++) {
    __injectMockReplyCard(
      card(i++, {
        status: "answered",
        answeredTs: now - 300 - n,
        answer: { optionIdxs: [0], text: "", attachments: [] },
      })
    );
  }
}

/** The 請示 page itself, in the app's content column. The panel, its field and
 * every label the guard measures come from the shipped page. `cards` seeds
 * that many waiting / snoozed / answered cards; `holdCounts` holds the
 * count read until the page calls `window.__releaseReplyCounts()`, so no count
 * is known before then. */
export function RepliesPageStory({
  theme,
  cards,
  holdCounts = false,
}: {
  theme: BuiltinThemeId;
  cards?: readonly [number, number, number];
  holdCounts?: boolean;
}) {
  window.localStorage.setItem(LS_THEME, theme);
  document.documentElement.setAttribute("data-theme", theme);
  if (cards) seedCards(cards);
  if (holdCounts && !held) {
    held = true;
    const real = api.getReplyCardCount.bind(api);
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    (window as unknown as { __releaseReplyCounts: () => void }).__releaseReplyCounts = release;
    api.getReplyCardCount = () => gate.then(real);
  }
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
 * nav. The width preference is set before I18nProvider reads it. `lore` opens
 * Mira's section page. */
export function AppPageStory({
  page,
  theme,
  wide = false,
}: {
  page: "tasks" | "replies" | "lore";
  theme: BuiltinThemeId;
  wide?: boolean;
}) {
  window.localStorage.setItem(LS_THEME, theme);
  document.documentElement.setAttribute("data-theme", theme);
  window.localStorage.setItem("oc.wide", wide ? "true" : "false");
  window.location.hash = page === "lore" ? "#lore/agent/mira" : `#${page}`;
  return (
    <I18nProvider>
      <ReplyCardsProvider>
        <App onLogout={() => {}} />
      </ReplyCardsProvider>
    </I18nProvider>
  );
}
