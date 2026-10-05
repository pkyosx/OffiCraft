import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import type { Dict } from "../i18n/locales/zh";
import { formatBytes } from "../lib/bytes";
import { formatDuration } from "../lib/duration";
import type { MachineDiskUsageView } from "../types";
import { ChevronDownIcon } from "./icons";
import "./disk-usage.css";

const TOP_MEMBERS = 5;

type Segment = "database" | "backups" | "workspaces" | "conversations" | "other";

export interface DiskUsageCellProps {
  /** The machine's `diskUsage`; null or undefined reads as never measured. */
  usage: MachineDiskUsageView | null | undefined;
  /** Opens the machine's detail dialog, where the breakdown lives. */
  onOpen: () => void;
}

interface Row {
  key: string;
  label: ReactNode;
  value: string;
  sub?: boolean;
  section?: boolean;
  segment?: Segment;
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
  const rows = breakdownRows(usage, d, t.monitor.machineCol, msg.monitorDiskOtherMembers);
  const segments = barSegments(usage, d);
  const measuredAt = usage.measuredAt ?? usage.databaseMeasuredAt;
  const measuredText =
    measuredAt === null
      ? null
      : msg.monitorMeasuredAgo(formatDuration(Math.max(0, Date.now() / 1000 - measuredAt)));
  const shown = new Set(segments.map((s) => s.segment));

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
              key={s.segment}
              className={`disk-usage__seg disk-usage__seg--${s.segment}`}
              data-segment={s.segment}
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
                (r.segment && shown.has(r.segment) ? (
                  <span
                    className={`disk-usage__swatch disk-usage__seg--${r.segment}`}
                    data-segment={r.segment}
                    aria-hidden="true"
                  />
                ) : (
                  <span className="disk-usage__swatch" aria-hidden="true" />
                ))}
              <span className="disk-usage__name">{r.label}</span>
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

/** The bar's segments with their share of the total, in percent. Members and
 * the Claude/Codex split are parts of a category, so they are not segments.
 * No total, no bar: a share of an unknown whole would be invented. The
 * categories are measured separately and can add up to a little more than the
 * total; the larger of the two is the whole then, so the bar never overflows. */
function barSegments(u: MachineDiskUsageView, d: Dict["monitor"]["diskUsage"]) {
  if (u.totalBytes === null) return [];
  const parts = (
    [
      ["database", d.database, u.databaseBytes],
      ["backups", d.backups, u.backupsBytes],
      ["workspaces", d.workspaces, u.workspaceBytes],
      ["conversations", d.conversations, u.conversationBytes],
      ["other", d.other, u.otherBytes],
    ] as [Segment, string, number | null][]
  ).filter((p): p is [Segment, string, number] => p[2] !== null && p[2] > 0);
  const whole = Math.max(
    u.totalBytes,
    parts.reduce((sum, [, , bytes]) => sum + bytes, 0),
  );
  if (whole <= 0) return [];
  return parts.map(([segment, label, bytes]) => ({ segment, label, bytes, share: (bytes / whole) * 100 }));
}

function breakdownRows(
  u: MachineDiskUsageView,
  d: Dict["monitor"]["diskUsage"],
  col: Dict["monitor"]["machineCol"],
  otherMembers: (count: number) => string,
): Row[] {
  const rows: Row[] = [];
  const push = (key: string, label: ReactNode, bytes: number | null, extra?: Partial<Row>) => {
    if (bytes !== null) rows.push({ key, label, value: formatBytes(bytes), ...extra });
  };
  push("database", d.database, u.databaseBytes, { segment: "database" });
  push("backups", d.backups, u.backupsBytes, { segment: "backups" });
  const workspaceAt = rows.length;
  push("workspaces", d.workspaces, u.workspaceBytes, { segment: "workspaces" });

  const members = [...u.members].sort((a, b) => b.totalBytes - a.totalBytes);
  members.slice(0, TOP_MEMBERS).forEach((m) => {
    const label = (
      <>
        {m.name ?? m.memberId}
        {m.rosterStatus !== "active" && (
          <span className="disk-usage__left" data-testid="disk-usage-left">
            {d.memberLeft}
          </span>
        )}
      </>
    );
    push(`top:${m.memberId}`, label, m.totalBytes, { sub: true });
  });
  const rest = members.slice(TOP_MEMBERS);
  if (rest.length > 0) {
    push("members-rest", otherMembers(rest.length), rest.reduce((sum, m) => sum + m.totalBytes, 0), { sub: true });
  }

  // Without a workspace total the members open the section themselves, so
  // they do not read as part of 備份 above them.
  if (rows[workspaceAt]) rows[workspaceAt].section = true;

  const conversationAt = rows.length;
  push("conversations", d.conversations, u.conversationBytes, { segment: "conversations" });
  push("claude", col.claude, u.claudeConversationBytes, { sub: true });
  push("codex", col.codex, u.codexConversationBytes, { sub: true });
  if (rows[conversationAt]) rows[conversationAt].section = true;

  push("other", d.other, u.otherBytes, { segment: "other" });
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
