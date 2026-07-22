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
// That move BROKE the landmark name, and this comment used to describe the
// break as an open defect. It is fixed now, and the history is worth keeping
// because of how it was caught. The `aria-label` was hardcoded to
// `t.settings.title`, which was TRUE while the guide lived under Settings
// (SettingsPage passed `[crumbRoot, {label: t.settings.guide}]`) and became
// FALSE the moment this pack promoted it to a tab: a screen-reader user on 使用說明
// was told they were in 設定. Same class of falsehood as the stale comments
// this pack spent four rounds removing — just written in an aria attribute
// instead of prose, which is exactly why it outlived them.
//
// Nothing caught it: the suites address this trail by class (`nav.crumbs`,
// `.crumbs__seg`) and by segment TEXT, and an aria-label is not a text node —
// so even GuidePage's `queryByText("設定") === null` assertion, written by this
// pack to prove the guide had left Settings, passed straight over the string
// "設定" sitting in the landmark. Breadcrumbs.test.tsx now reads the accessible
// name directly, on both surfaces and in all three locales.
//
// The name is now DERIVED from items[0] (see below) rather than passed in, so
// there is no per-caller decision to get wrong and no default to fall back to.

export interface Crumb {
  label: string;
  /** Jump up the tree. Omitted on the terminal (current-page) segment. */
  onClick?: () => void;
  /** Monospace styling for machine-ish labels (e.g. a manual type_key). */
  mono?: boolean;
}

export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    // The landmark is named by the trail's OWN root, not by a hardcoded
    // constant: items[0] is the region this trail lives in (設定 for every
    // settings page, 使用說明 for the guide tab), already localized because it
    // is the same string the reader sees as the first segment. That makes the
    // accessible name incapable of drifting away from the visible trail — the
    // failure this replaced. No items → no name at all, which is honest;
    // an unnamed landmark beats a wrongly-named one.
    <nav className="crumbs" aria-label={items[0]?.label}>
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
