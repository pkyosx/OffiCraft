// CT story for T-d593 — one element per unread-badge CSS rule.
//
// Three elements, not seven: the seven render sites (App.tsx ×3,
// OfficeSidebarTabs ×2, MemberCard, OutsourcePanel) resolve to exactly THREE
// CSS rules, and which site uses which class is pinned separately by
// src/components/badgeRing.test.ts (a source scan that runs in vitest, i.e. in
// the cloud gate too). This story's job is the other half: what the browser
// ACTUALLY paints for those three rules.
//
// No providers, no fixtures — the classes are all that matter, and the real
// stylesheets are loaded globally by playwright/index.ts.
export function BadgeRingStory() {
  return (
    <div>
      <span className="nav-tab__badge" data-testid="ring-nav">3</span>
      <span className="office__tab-badge" data-testid="ring-tab">2</span>
      <span className="member-card__unread" data-testid="ring-card">24</span>
    </div>
  );
}
