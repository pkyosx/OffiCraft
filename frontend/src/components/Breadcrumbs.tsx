// Breadcrumbs — the settings tree's unified top-of-page navigation (T-8f6e).
// Generalized from the 任務手冊 detail page's crumbs (the owner's reference
// style): every settings page heads with 「設定 › 子頁 › …」, each non-terminal
// segment clickable back up the tree, and the page title sits directly below.
// The trailing segment is the CURRENT page — rendered as plain text with
// aria-current, never a dead button. Jumps are the caller's: pages wire each
// segment's onClick to their navigation (hash writes ride lib/hashRoute.ts).
//
// T-68f1: no longer settings-only — the 使用說明 tab reuses it with its own
// root crumb (使用說明 › <doc>), and it is NOT under 設定. The `aria-label`
// below is still hardcoded to t.settings.title, so on that tab the landmark
// announces the wrong name; making it a prop is a known follow-up, not done
// here (this pack is doc/comment truth only).
// FOLLOW-UP TICKET: (to be filed by the coordinator — paste the id here; this
// line exists so the gap is addressable and not merely "someone knows").
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
