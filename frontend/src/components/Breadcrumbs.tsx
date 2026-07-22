// Breadcrumbs — the settings tree's unified top-of-page navigation (T-8f6e).
// Generalized from the 任務手冊 detail page's crumbs (the owner's reference
// style): every settings page heads with 「設定 › 子頁 › …」, each non-terminal
// segment clickable back up the tree, and the page title sits directly below.
// The trailing segment is the CURRENT page — rendered as plain text with
// aria-current, never a dead button. Jumps are the caller's: pages wire each
// segment's onClick to their navigation (hash writes ride lib/hashRoute.ts).
//
// T-68f1: no longer settings-only — the 使用說明 tab reuses it with its own
// root crumb (使用說明 › <doc>), and it is NOT under 設定.
//
// 🔴 OPEN DEFECT, deliberately not fixed here. The `aria-label` below is
// hardcoded to `t.settings.title`, so on the 使用說明 tab the navigation
// landmark announces itself as 設定 — a screen-reader user is told they are in
// Settings on a page that is not under Settings. It is wrong TODAY, in shipped
// code; it is not a nicety.
//
// Why not fixed here: this pack's authority was doc/comment truth, and the fix
// is a behaviour change with a design decision inside it — every caller must
// then say what its landmark is called, which means picking a source for that
// name (a new required prop? a default that keeps 設定 for the settings tree?).
// Widening a fourth-round pack to make that call was judged the worse risk.
//
// What the next person has to do: give the landmark name an owner. Make it a
// prop, pass 使用說明's own title from UserGuidePage and 設定's from the
// settings pages, and pin it with a test that reads the rendered aria-label on
// BOTH surfaces. Note WHY nothing catches it today: GuidePage.test.tsx really
// does mount this component outside the settings tree, but every suite —
// including Breadcrumbs.test.tsx — addresses the trail by class (`nav.crumbs`,
// `.crumbs__seg`) and by its segment TEXT. Not one assertion anywhere reads the
// accessible name, so the wrong landmark passes straight through a green suite.
// No ticket is tracking this; this comment is the only record.
import { useI18n } from "../i18n";

export interface Crumb {
  label: string;
  /** Jump up the tree. Omitted on the terminal (current-page) segment. */
  onClick?: () => void;
  /** Monospace styling for machine-ish labels (e.g. a manual type_key). */
  mono?: boolean;
}

export function Breadcrumbs({ items }: { items: Crumb[] }) {
  const { t } = useI18n();
  return (
    <nav className="crumbs" aria-label={t.settings.title}>
      {items.map((c, i) => {
        const mono = c.mono ? " manual-key" : "";
        const last = i === items.length - 1;
        return (
          // Position is identity here (labels may repeat, e.g. 設定 › 設定 is
          // impossible but a role named 角色誌 is not) — index keys are safe
          // because the list is render-static per page.
          <span className="crumbs__seg" key={i}>
            {i > 0 && <span className="crumbs__sep">›</span>}
            {!last && c.onClick ? (
              <button
                type="button"
                className={`crumbs__link${mono}`}
                onClick={c.onClick}
              >
                {c.label}
              </button>
            ) : (
              <span
                className={`crumbs__here${mono}`}
                aria-current={last ? "page" : undefined}
              >
                {c.label}
              </span>
            )}
          </span>
        );
      })}
    </nav>
  );
}
