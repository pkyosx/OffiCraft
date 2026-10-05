import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import type { Dict } from "../i18n/locales/zh";
import { formatBytes } from "../lib/bytes";
import { formatDuration } from "../lib/duration";
import type { MachineDiskUsageCategoryView, MachineDiskUsageView } from "../types";
import { ChevronDownIcon } from "./icons";
import "./disk-usage.css";

const TOP_MEMBERS = 5;

type Dicts = Dict["monitor"]["diskUsage"];
type KnownKey = keyof Dicts["desc"];

/** The category keys the page has words and a colour for, by the server's
 * key. Any other key a warden reports is shown as the key itself, in the
 * neutral colour (segment "unknown"). */
const KNOWN: Record<string, KnownKey> = {
  database: "database",
  backups: "backups",
  workspaces: "workspaces",
  logs: "logs",
  old_version_backups: "oldVersionBackups",
  old_database_copies: "oldDatabaseCopies",
  other: "other",
};

/** A known key's colour class, "unknown" for the rest. */
function segmentOf(key: string): string {
  return KNOWN[key] ? key : "unknown";
}

export interface DiskUsageCellProps {
  /** The machine's `diskUsage`; null or undefined reads as never measured. */
  usage: MachineDiskUsageView | null | undefined;
  /** Opens the machine's detail dialog, where the breakdown lives. */
  onOpen: () => void;
}

interface Row {
  key: string;
  label: ReactNode;
  /** A grey line under the label saying what the row holds. */
  desc?: string;
  value: string;
  sub?: boolean;
  section?: boolean;
  /** Its colour class: a known key, or "unknown". */
  segment?: string;
}

/** A machine's OffiCraft disk total in the 磁碟 column. Clicking it opens the
 * machine's detail dialog; never measured reads 尚未量測 and opens nothing. */
export function DiskUsageCell({ usage, onOpen }: DiskUsageCellProps) {
  const { t } = useI18n();
  const d = t.monitor.diskUsage;

  if (!usage) {
    return (
      <span className="disk-usage__unmeasured" data-testid="disk-usage-unmeasured">
        {d.notMeasured}
      </span>
    );
  }

  const totalText = usage.totalBytes === null ? t.monitor.dash : formatBytes(usage.totalBytes);
  return (
    <span className="disk-usage">
      <button
        type="button"
        className="disk-usage__trigger"
        aria-label={`${d.open} ${totalText}`}
        aria-haspopup="dialog"
        data-testid="disk-usage-trigger"
        onClick={(e) => {
          e.stopPropagation();
          onOpen();
        }}
      >
        {totalText}
        <span className="disk-usage__chevron" aria-hidden="true">
          <ChevronDownIcon size={12} />
        </span>
      </button>
    </span>
  );
}

/** The breakdown of one measurement: a bar of the top-level categories, each
 * segment as wide as its share of the OffiCraft total, then every row with its
 * number and when it was measured. */
export function DiskUsageBreakdown({ usage }: { usage: MachineDiskUsageView }) {
  const { t, msg } = useI18n();
  const d = t.monitor.diskUsage;
  const rows = breakdownRows(usage, d, t.monitor.dash, msg.monitorDiskOtherMembers);
  const segments = barSegments(usage, d);
  const measuredAt = usage.measuredAt ?? usage.databaseMeasuredAt;
  const measuredText =
    measuredAt === null
      ? null
      : msg.monitorMeasuredAgo(formatDuration(Math.max(0, Date.now() / 1000 - measuredAt)));
  const shown = new Set(segments.map((s) => s.key));

  return (
    <div className="disk-usage-breakdown" data-testid="disk-usage-breakdown">
      {segments.length > 0 && (
        <div
          className="disk-usage__bar"
          role="img"
          aria-label={
            d.barLabel +
            segments.map((s) => `${s.label} ${formatBytes(s.bytes)} (${percentText(s.share)})`).join(d.listSep)
          }
          data-testid="disk-usage-bar"
        >
          {segments.map((s) => (
            <span
              key={s.key}
              className={`disk-usage__seg disk-usage__seg--${s.segment}`}
              data-segment={s.key}
              data-testid="disk-usage-seg"
              style={{ flexBasis: `${s.share}%` }}
            />
          ))}
        </div>
      )}
      <ul className="disk-usage__rows">
        {rows.map((r) => (
          <li
            key={r.key}
            className={
              "disk-usage__row" +
              (r.sub ? " disk-usage__row--sub" : "") +
              (r.section ? " disk-usage__row--section" : "")
            }
            data-testid="disk-usage-row"
          >
            <span className="disk-usage__label">
              {!r.sub &&
                (r.segment && shown.has(r.key) ? (
                  <span
                    className={`disk-usage__swatch disk-usage__seg--${r.segment}`}
                    data-segment={r.key}
                    aria-hidden="true"
                  />
                ) : (
                  <span className="disk-usage__swatch" aria-hidden="true" />
                ))}
              <span className="disk-usage__text">
                <span className="disk-usage__name">{r.label}</span>
                {r.desc && <span className="disk-usage__desc">{r.desc}</span>}
              </span>
            </span>
            <span className="disk-usage__value">{r.value}</span>
          </li>
        ))}
      </ul>
      {measuredText && (
        <div className="disk-usage__measured" data-testid="disk-usage-measured">
          {measuredText}
        </div>
      )}
    </div>
  );
}

function percentText(share: number): string {
  return share > 0 && share < 1 ? "<1%" : `${Math.round(share)}%`;
}

/** The bar's segments with their share of the total, in percent: one per
 * top-level category with something in it. Members and a category's parts are
 * parts of a category, so they are not segments. No total, no bar: a share of
 * an unknown whole would be invented. The categories are measured separately
 * and can add up to a little more than the total; the larger of the two is the
 * whole then, so the bar never overflows. */
function barSegments(u: MachineDiskUsageView, d: Dicts) {
  if (u.totalBytes === null) return [];
  const parts = u.categories
    .filter((c): c is MachineDiskUsageCategoryView & { bytes: number } => c.parentKey === null && c.bytes !== null && c.bytes > 0)
    .map((c) => ({ key: c.key, segment: segmentOf(c.key), label: labelOf(c.key, d), bytes: c.bytes }));
  const whole = Math.max(
    u.totalBytes,
    parts.reduce((sum, p) => sum + p.bytes, 0),
  );
  if (whole <= 0) return [];
  return parts.map((p) => ({ ...p, share: (p.bytes / whole) * 100 }));
}

function labelOf(key: string, d: Dicts): string {
  const known = KNOWN[key];
  return known ? d[known] : key;
}

function breakdownRows(
  u: MachineDiskUsageView,
  d: Dicts,
  dash: string,
  otherMembers: (count: number) => string,
): Row[] {
  const rows: Row[] = [];
  const value = (bytes: number | null) => (bytes === null ? dash : formatBytes(bytes));
  for (const c of u.categories) {
    if (c.parentKey !== null) continue;
    const known = KNOWN[c.key];
    rows.push({
      key: c.key,
      label: labelOf(c.key, d),
      desc: known ? d.desc[known] : undefined,
      value: value(c.bytes),
      segment: segmentOf(c.key),
      // A row with its own sub rows opens a section, so they do not read as
      // part of the row above.
      section: c.key === "workspaces" || u.categories.some((p) => p.parentKey === c.key),
    });
    for (const p of u.categories) {
      if (p.parentKey === c.key) rows.push({ key: `${c.key}/${p.key}`, label: labelOf(p.key, d), value: value(p.bytes), sub: true });
    }
    if (c.key === "workspaces") rows.push(...memberRows(u, d, dash, otherMembers));
  }
  // A top-level row right after a group's sub rows would read as one more of
  // them without a divider.
  rows.forEach((r, i) => {
    if (i > 0 && !r.sub && rows[i - 1].sub) r.section = true;
  });
  if (u.diskFreeBytes !== null && u.diskTotalBytes !== null) {
    rows.push({
      key: "disk",
      label: d.disk,
      value: `${formatBytes(u.diskFreeBytes)} / ${formatBytes(u.diskTotalBytes)}`,
      section: true,
    });
  }
  return rows;
}

/** The largest members' workspaces, then the rest as one row. */
function memberRows(u: MachineDiskUsageView, d: Dicts, dash: string, otherMembers: (count: number) => string): Row[] {
  const members = [...u.members].sort((a, b) => (b.workspaceBytes ?? -1) - (a.workspaceBytes ?? -1));
  const rows: Row[] = members.slice(0, TOP_MEMBERS).map((m) => ({
    key: `top:${m.memberId}`,
    label: (
      <>
        {m.name ?? m.memberId}
        {m.rosterStatus !== "active" && (
          <span className="disk-usage__left" data-testid="disk-usage-left">
            {d.memberLeft}
          </span>
        )}
      </>
    ),
    value: m.workspaceBytes === null ? dash : formatBytes(m.workspaceBytes),
    sub: true,
  }));
  const rest = members.slice(TOP_MEMBERS);
  if (rest.length > 0) {
    rows.push({
      key: "members-rest",
      label: otherMembers(rest.length),
      value: formatBytes(rest.reduce((sum, m) => sum + (m.workspaceBytes ?? 0), 0)),
      sub: true,
    });
  }
  return rows;
}
