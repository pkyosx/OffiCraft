// Story — the product-guide LIST page (使用說明 章節首頁) rendered against the REAL
// app shell + sheets, ALONGSIDE a bare `.settings` reference surface (the shape
// every OTHER settings page uses: 軟體更新 / 角色 / 參數 landing), so a CT can
// measure whether the guide list's content left edge is FLUSH with the others.
//
// Owner (desktop, v0.5.22 / T-9aa5): the 使用說明 LIST content sat ~48px to the
// RIGHT of every other settings page. Root cause (real-browser recon坐實): the
// v0.5.19/T-23df reading-measure block scoped its `900px cap + 20px reading
// gutters` to `.guide-view`, which BOTH the list and the doc view inherit — so
// the list menu got centred inside a 900px band (20px surface gutter + 28px
// centring margin = 48px). The fix scopes that measure to `.guide-view--doc`
// only; the bare-`.guide-view` list falls back to the plain `.settings` flex
// column and stretches flush-left, pixel-identical to the other pages.
//
// Faithfulness: BOTH surfaces live inside the SAME `.app › .app__main` column,
// so `.app__main` (max-width 1040, margin-inline auto) resolves to the exact
// same inner edge for each — any left-edge delta between the two set-entries is
// therefore the guide's OWN doing, not a shell difference. The list's DOM/class
// chain mirrors UserGuidePage.tsx's UserGuideList exactly (settings guide-view >
// settings__title--doc + set-entries); the reference mirrors SettingsPage.tsx's
// landing (settings > crumbs + settings__title + set-entries).
import { Breadcrumbs } from "../../src/components/Breadcrumbs";
import { ChevronRightIcon } from "../../src/components/icons";
import "../../src/styles/theme.css";
import "../../src/styles/global.css";
import "../../src/components/chrome.css";
import "../../src/components/settings.css";

const ENTRIES = ["為什麼", "安裝", "快速上手", "介面說明", "成員"];

function Entry({ label }: { label: string }) {
  return (
    <button type="button" className="set-entry">
      <span className="set-entry__name">{label}</span>
      <ChevronRightIcon size={18} className="set-entry__chev" />
    </button>
  );
}

export function GuideListFlushLeftStory() {
  return (
    <div className="app">
      <main className="app__main">
        {/* 使用說明 LIST — mirrors UserGuideList: `settings guide-view`, a
            `settings__title--doc`, then `set-entries`. NO breadcrumb (the real
            list drops it), and crucially NO `guide-view--doc`. */}
        <div className="settings guide-view" data-testid="guide-list">
          <h1 className="settings__title settings__title--doc">使用說明</h1>
          <div className="set-entries" data-testid="guide-set-entries">
            {ENTRIES.map((e) => (
              <Entry key={e} label={e} />
            ))}
          </div>
        </div>

        {/* REFERENCE — a bare `.settings` landing, the shape 軟體更新 / 角色 /
            參數 use. This is the pixel target the list must match. */}
        <div className="settings" data-testid="settings-ref">
          <Breadcrumbs items={[{ label: "設定" }]} />
          <h1 className="settings__title">設定</h1>
          <div className="set-entries" data-testid="ref-set-entries">
            {ENTRIES.map((e) => (
              <Entry key={e} label={e} />
            ))}
          </div>
        </div>
      </main>
    </div>
  );
}
