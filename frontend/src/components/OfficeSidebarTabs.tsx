// T-66a8 辦公室側欄 正職／外包 tab 切換 (owner mockup 2026-07-18) — replaces the
// old two-stacked-groups rail (正職 collapse header + 外包 panel head) with a
// TOP text-tab switcher: 「正職」「外包」side by side, the selected tab carrying
// a blue underline indicator. Each tab shows a red unread-count badge (that
// area's members' total unread; 0 → hidden) beside its label, and a small
// count sub-line underneath — 正職 「N 人」, 外包 「N 人 · 上限 M」. The old
// per-group 「上線/總」 and 「N / 上限」 counts are gone (the sub-lines carry
// the count now). The recruit button and the outsource cap popover live in
// OfficePage (below the switched content); this component is JUST the header.

import { useI18n } from "../i18n";

interface SidebarTabProps {
  tab: "staff" | "outsource";
  active: boolean;
  onSelect: (tab: "staff" | "outsource") => void;
  label: string;
  /** area unread total; 0 → the badge is not rendered (mockup rule). */
  unread: number;
  /** the count sub-line under the label; "" → suppressed (honest: a failed /
   * still-loading fetch never fabricates a "0 人"). */
  sub: string;
  testid: string;
  unreadTestid: string;
  subTestid: string;
}

function SidebarTab({
  tab,
  active,
  onSelect,
  label,
  unread,
  sub,
  testid,
  unreadTestid,
  subTestid,
}: SidebarTabProps) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      className={`office__tab${active ? " office__tab--active" : ""}`}
      data-testid={testid}
      onClick={() => onSelect(tab)}
    >
      <span className="office__tab-head">
        <span className="office__tab-label">{label}</span>
        {/* Red count pill — the SAME recipe as .member-card__unread /
         * .nav-tab__badge (>99 → "99+"; 0 → not rendered). */}
        {unread > 0 && (
          <span className="office__tab-badge" data-testid={unreadTestid}>
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </span>
      {/* The small count line under the label; empty string renders an empty
       * (zero-height-content) span so the two tabs stay vertically aligned. */}
      <span className="office__tab-sub" data-testid={subTestid}>
        {sub}
      </span>
    </button>
  );
}

export function OfficeSidebarTabs({
  activeTab,
  onSelect,
  staffCount,
  staffUnread,
  staffReady,
  outsourceCount,
  outsourceUnread,
  outsourceReady,
  capText,
}: {
  activeTab: "staff" | "outsource";
  onSelect: (tab: "staff" | "outsource") => void;
  staffCount: number;
  staffUnread: number;
  /** false while the roster is loading or its fetch rejected → suppress the
   * 正職 count sub-line (never render a dead load as 「0 人」). */
  staffReady: boolean;
  outsourceCount: number;
  outsourceUnread: number;
  /** false when the outsource fetch rejected → suppress the 外包 count. */
  outsourceReady: boolean;
  /** the cap display ("∞" for 無限, "N" for a finite cap); null when settings
   * are not loaded → the 「· 上限 M」 suffix is omitted (no fabricated limit). */
  capText: string | null;
}) {
  const { t } = useI18n();
  const outsourceSub = outsourceReady
    ? t.office.outsource.workerSub(outsourceCount) +
      (capText !== null ? t.office.outsource.capSuffix(capText) : "")
    : "";
  return (
    <div className="office__tabs" role="tablist" data-testid="office-tabs">
      <SidebarTab
        tab="staff"
        active={activeTab === "staff"}
        onSelect={onSelect}
        label={t.office.staffTitle}
        unread={staffUnread}
        sub={staffReady ? t.office.staffSub(staffCount) : ""}
        testid="office-tab-staff"
        unreadTestid="staff-tab-unread"
        subTestid="staff-tab-sub"
      />
      <SidebarTab
        tab="outsource"
        active={activeTab === "outsource"}
        onSelect={onSelect}
        label={t.office.outsource.title}
        unread={outsourceUnread}
        sub={outsourceSub}
        testid="office-tab-outsource"
        unreadTestid="outsource-tab-unread"
        subTestid="outsource-tab-sub"
      />
    </div>
  );
}
